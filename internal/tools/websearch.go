package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/net/html"
)

const (
	defaultMaxResults = 8
	defaultTimeoutMs  = 12_000
	minTimeoutMs      = 1_000
	maxTimeoutMsWeb   = 60_000
	maxResultsLimit   = 20
)

const (
	defaultUserAgent   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	ddgCommonUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36"
)

// WebSearchInput is the input schema for the websearch tool (mirrors the
// tau-agent Rust tool).
type WebSearchInput struct {
	Query      string `json:"query" jsonschema:"The search query"`
	MaxResults *int   `json:"maxResults,omitempty" jsonschema:"Maximum number of results (1-20, default 8)"`
	TimeoutMs  *int   `json:"timeoutMs,omitempty" jsonschema:"Timeout in milliseconds (1000-60000, default 12000)"`
}

// WebSearchToolDescription mirrors the tau-agent websearch template.
const WebSearchToolDescription = "Search the web using a no-key DuckDuckGo fallback.\n\nBackend order:\n1. duck-duck-scrape.\n2. html.duckduckgo.com.\n3. lite.duckduckgo.com.\n\nOutput:\n- title\n- url\n- abstract"

// WebSearchToolPromptSnippet is the one-line system prompt contribution.
const WebSearchToolPromptSnippet = "Search the web for information"

// WebSearchResult is one search hit.
type WebSearchResult struct {
	Title        string `json:"title"`
	URL          string `json:"url"`
	AbstractText string `json:"abstract"`
}

// webFetch is the injectable HTTP layer (testable without network).
type webFetch func(ctx context.Context, rawURL string) (string, error)

// ---- pure helpers (unit-testable) ----

func normalizeWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// jitterMs is a pseudo-random 0..120ms jitter (matches TS Math.random()*120).
func jitterMs() uint64 {
	return uint64(time.Now().UnixNano() % 120)
}

// isTransientDdgError mirrors the TS isTransientDdgError predicate.
func isTransientDdgError(message string) bool {
	lower := strings.ToLower(message)
	needles := []string{
		"anomaly",
		"too quickly",
		"server error",
		"timeout",
		"network",
		"fetch failed",
		"econnreset",
		"etimedout",
	}
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

// extractURLFromDDGHref prefers the uddg= redirect param, then a raw http(s)
// url, then a //-prefixed protocol-relative url.
func extractURLFromDDGHref(href string) (string, bool) {
	if href == "" {
		return "", false
	}
	if i := strings.LastIndex(href, "?"); i >= 0 {
		for _, pair := range strings.Split(href[i+1:], "&") {
			if !strings.HasPrefix(pair, "uddg=") {
				continue
			}
			encoded := pair[len("uddg="):]
			if decoded, err := url.QueryUnescape(encoded); err == nil {
				return decoded, true
			}
			return "", false
		}
	}
	lower := strings.ToLower(href)
	if strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") {
		return href, true
	}
	if strings.HasPrefix(href, "//") {
		return "https:" + href, true
	}
	return "", false
}

// buildSnippetFromAnchorText is the parent container's text minus the anchor
// text, truncated to 280 chars.
func buildSnippetFromAnchorText(anchorText, containerText string) string {
	normalizedAnchor := normalizeWhitespace(anchorText)
	normalizedContainer := normalizeWhitespace(containerText)
	if normalizedContainer == "" {
		return ""
	}
	if normalizedAnchor == "" {
		return truncateRunes(normalizedContainer, 280)
	}
	return truncateRunes(normalizeWhitespace(strings.ReplaceAll(normalizedContainer, normalizedAnchor, "")), 280)
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

func elementAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func elementText(n *html.Node) string {
	var sb strings.Builder
	var visit func(*html.Node)
	visit = func(m *html.Node) {
		if m.Type == html.TextNode {
			sb.WriteString(m.Data)
		}
		for c := m.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(n)
	return sb.String()
}

func containerText(anchor *html.Node) string {
	if anchor.Parent != nil && anchor.Parent.Type == html.ElementNode {
		return elementText(anchor.Parent)
	}
	if anchor.Parent != nil && anchor.Parent.Parent != nil && anchor.Parent.Parent.Type == html.ElementNode {
		return elementText(anchor.Parent.Parent)
	}
	return ""
}

// extractResultsFromDDGHTML parses a DDG HTML result page (html/ or lite/),
// walking all <a> elements, validating the redirect url, deduping, and
// building snippets from the anchor's container text.
func extractResultsFromDDGHTML(page string, count int) []WebSearchResult {
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return nil
	}

	var results []WebSearchResult
	seen := map[string]struct{}{}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if len(results) >= count {
			return
		}
		if n.Type == html.ElementNode && n.Data == "a" {
			if r, ok := buildResultFromAnchor(n, seen); ok {
				results = append(results, r)
			}
		}
		for c := n.FirstChild; c != nil && len(results) < count; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return results
}

func buildResultFromAnchor(anchor *html.Node, seen map[string]struct{}) (WebSearchResult, bool) {
	href := elementAttr(anchor, "href")
	decoded, ok := extractURLFromDDGHref(href)
	if !ok {
		return WebSearchResult{}, false
	}
	parsed, err := url.Parse(decoded)
	if err != nil {
		return WebSearchResult{}, false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return WebSearchResult{}, false
	}
	normalized := parsed.String()
	if _, dup := seen[normalized]; dup {
		return WebSearchResult{}, false
	}
	seen[normalized] = struct{}{}

	title := normalizeWhitespace(elementText(anchor))
	if title == "" {
		return WebSearchResult{}, false
	}
	snippet := buildSnippetFromAnchorText(title, containerText(anchor))
	return WebSearchResult{Title: title, URL: normalized, AbstractText: snippet}, true
}

// ---- retry + fetch ----

var vqdRegexp = regexp.MustCompile(`vqd=['"](\d+-\d+(?:-\d+)?)['"]`)
var djsRegexp = regexp.MustCompile(`(?s)DDG\.pageLayout\.load\('d',\s*(\[.*?\])\s*\);`)

func ddgError(msg string) error {
	return fmt.Errorf("%s", msg)
}

func withRetry(ctx context.Context, retries int, op func(ctx context.Context) ([]WebSearchResult, error)) ([]WebSearchResult, error) {
	var last error
	for attempt := 0; attempt <= retries; attempt++ {
		results, err := op(ctx)
		if err == nil {
			return results, nil
		}
		last = err
		if attempt >= retries || !isTransientDdgError(err.Error()) {
			return nil, err
		}
		backoff := 250 * (1 << attempt)
		timer := time.NewTimer(time.Duration(backoff+int(jitterMs())) * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}
	return nil, last
}

// searchDDGApi is the primary path: links.duckduckgo.com/d.js gated by a vqd
// token, wrapped in withRetry.
func searchDDGApi(ctx context.Context, fetch webFetch, query string, count int) ([]WebSearchResult, error) {
	return withRetry(ctx, 2, func(ctx context.Context) ([]WebSearchResult, error) {
		// 1. Resolve the vqd token.
		tokenURL := "https://duckduckgo.com/?q=" + url.QueryEscape(query) + "&ia=web"
		tokenBody, err := fetch(ctx, tokenURL)
		if err != nil {
			return nil, err
		}
		m := vqdRegexp.FindStringSubmatch(tokenBody)
		if len(m) < 2 {
			return nil, ddgError("Failed to get the VQD for query.")
		}
		vqd := m[1]

		// 2. Build the d.js query object (matches duck-duck-scrape moderate).
		params := []struct{ k, v string }{
			{"q", query}, {"t", "D"}, {"l", "en-us"}, {"kl", "wt-wt"},
			{"s", "0"}, {"dl", "en"}, {"ct", "US"}, {"bing_market", "en-US"},
			{"df", "a"}, {"vqd", vqd}, {"ex", "-1"}, {"sp", "1"}, {"bpa", "1"},
			{"biaexp", "b"}, {"msvrtexp", "b"}, {"nadse", "b"}, {"eclsexp", "b"},
			{"tjsexp", "b"},
		}
		parts := make([]string, 0, len(params))
		for _, p := range params {
			parts = append(parts, p.k+"="+url.QueryEscape(p.v))
		}
		dURL := "https://links.duckduckgo.com/d.js?" + strings.Join(parts, "&")

		body, err := fetch(ctx, dURL)
		if err != nil {
			return nil, err
		}
		if strings.Contains(body, "DDG.deep.is506") {
			return nil, ddgError("A server error occurred!")
		}
		if strings.Contains(body, "DDG.deep.anomalyDetectionBlock") {
			return nil, ddgError("DDG detected an anomaly in the request, you are likely making requests too quickly.")
		}

		match := djsRegexp.FindStringSubmatch(body)
		if len(match) < 2 {
			return nil, ddgError("No DuckDuckGo results")
		}
		jsonText := strings.ReplaceAll(match[1], "\t", "    ")
		var entries []map[string]json.RawMessage
		if err := json.Unmarshal([]byte(jsonText), &entries); err != nil {
			return nil, ddgError(fmt.Sprintf("d.js parse error: %v", err))
		}

		var results []WebSearchResult
		for _, entry := range entries {
			if len(results) >= count {
				break
			}
			if _, hasN := entry["n"]; hasN {
				continue
			}
			title := rawString(entry["t"])
			rawURL := rawString(entry["u"])
			abstract := rawString(entry["a"])
			if title == "" && rawURL == "" {
				continue
			}
			results = append(results, WebSearchResult{Title: title, URL: rawURL, AbstractText: abstract})
		}
		if len(results) == 0 {
			return nil, ddgError("No DuckDuckGo results")
		}
		return results, nil
	})
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func searchDDGHtml(ctx context.Context, fetch webFetch, query string, count int) ([]WebSearchResult, error) {
	urlStr := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	body, err := fetch(ctx, urlStr)
	if err != nil {
		return nil, err
	}
	if body == "" {
		return nil, ddgError("DDG HTML returned empty body")
	}
	results := extractResultsFromDDGHTML(body, count)
	if len(results) == 0 {
		return nil, ddgError("No results parsed from DDG HTML")
	}
	return results, nil
}

func searchDDGLite(ctx context.Context, fetch webFetch, query string, count int) ([]WebSearchResult, error) {
	urlStr := "https://lite.duckduckgo.com/lite/?q=" + url.QueryEscape(query)
	body, err := fetch(ctx, urlStr)
	if err != nil {
		return nil, err
	}
	if body == "" {
		return nil, ddgError("DDG Lite returned empty body")
	}
	results := extractResultsFromDDGHTML(body, count)
	if len(results) == 0 {
		return nil, ddgError("No results parsed from DDG Lite HTML")
	}
	return results, nil
}

// searchDuckDuckGo runs the 3-endpoint fallback chain, collecting a failure
// path so the final error lists every endpoint tried.
func searchDuckDuckGo(ctx context.Context, fetch webFetch, query string, count int) ([]WebSearchResult, error) {
	var failures []string
	if results, err := searchDDGApi(ctx, fetch, query, count); err == nil {
		return results, nil
	} else {
		failures = append(failures, "ddg_primary:"+err.Error())
	}
	if results, err := searchDDGHtml(ctx, fetch, query, count); err == nil {
		return results, nil
	} else {
		failures = append(failures, "ddg_html:"+err.Error())
	}
	if results, err := searchDDGLite(ctx, fetch, query, count); err == nil {
		return results, nil
	} else {
		failures = append(failures, "ddg_lite:"+err.Error())
	}
	return nil, ddgError("All DuckDuckGo endpoints failed: " + strings.Join(failures, "; "))
}

func httpWebFetch(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", ddgError("network: " + err.Error())
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "text/html")
	resp, err := client.Do(req)
	if err != nil {
		return "", ddgError("network: " + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", ddgError(fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", ddgError("network: " + err.Error())
	}
	return string(body), nil
}

type webSearchDeps struct{}

// CreateWebSearchTool returns the websearch tool handler.
func CreateWebSearchTool() mcp.ToolHandlerFor[WebSearchInput, any] {
	return webSearchDeps{}.handle
}

func (webSearchDeps) handle(ctx context.Context, _ *mcp.CallToolRequest, in WebSearchInput) (*mcp.CallToolResult, any, error) {
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return toolError("query is required"), nil, nil
	}
	count := defaultMaxResults
	if in.MaxResults != nil {
		count = clampInt(*in.MaxResults, 1, maxResultsLimit)
	}
	timeout := defaultTimeoutMs
	if in.TimeoutMs != nil {
		timeout = clampInt(*in.TimeoutMs, minTimeoutMs, maxTimeoutMsWeb)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	client := &http.Client{}
	fetch := func(ctx context.Context, u string) (string, error) {
		return httpWebFetch(ctx, client, u)
	}

	results, err := searchDuckDuckGo(ctx, fetch, query, count)
	if err != nil {
		return toolError(err.Error()), nil, nil
	}

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return toolError(err.Error()), nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
