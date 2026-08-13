package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func fakeFetch(fixtures map[string]string) webFetch {
	return func(ctx context.Context, rawURL string) (string, error) {
		for key, body := range fixtures {
			if strings.Contains(rawURL, key) {
				return body, nil
			}
		}
		return "", fmt.Errorf("no fixture for %s", rawURL)
	}
}

func alwaysErrFetch(msg string) webFetch {
	return func(ctx context.Context, rawURL string) (string, error) {
		return "", fmt.Errorf("%s", msg)
	}
}

func TestNormalizeWhitespace(t *testing.T) {
	if got := normalizeWhitespace("  a\n b \t c  "); got != "a b c" {
		t.Errorf("got %q", got)
	}
	if got := normalizeWhitespace(""); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestExtractURLFromDDGHref(t *testing.T) {
	cases := []struct {
		href string
		want string
		ok   bool
	}{
		{"/l/?uddg=https%3A%2F%2Fexample.com%2Fp&rut=1", "https://example.com/p", true},
		{"https://example.com/raw", "https://example.com/raw", true},
		{"//example.com/proto-relative", "https://example.com/proto-relative", true},
		{"javascript:void(0)", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := extractURLFromDDGHref(c.href)
		if ok != c.ok || got != c.want {
			t.Errorf("extractURLFromDDGHref(%q) = %q,%v want %q,%v", c.href, got, ok, c.want, c.ok)
		}
	}
}

func TestBuildSnippetFromAnchorText(t *testing.T) {
	got := buildSnippetFromAnchorText("Title", "Title and some surrounding context here")
	if got != "and some surrounding context here" {
		t.Errorf("got %q", got)
	}
	long := buildSnippetFromAnchorText("T", fmt.Sprintf("T %s", strings.Repeat("x", 400)))
	if len([]rune(long)) > 280 {
		t.Errorf("snippet too long: %d", len([]rune(long)))
	}
	if got := buildSnippetFromAnchorText("", ""); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestExtractResultsFromDDGHTML(t *testing.T) {
	htmlStr := `
	<html><body>
	  <div class="result">
	    <a class="result__a" href="/l/?uddg=https%3A%2F%2Fexample.com%2Fa&rut=1">Example A</a>
	    <div class="result__snippet">snippet text for a</div>
	  </div>
	  <div class="result">
	    <a class="result__a" href="/l/?uddg=https%3A%2F%2Fexample.com%2Fa&rut=2">Example A Dup</a>
	  </div>
	  <div class="result">
	    <a href="javascript:void(0)">Skip me</a>
	  </div>
	  <div class="result">
	    <a href="//b.example.com/b">Title B</a>
	    <span>container text b</span>
	  </div>
	</body></html>`
	results := extractResultsFromDDGHTML(htmlStr, 8)
	if len(results) != 2 {
		t.Fatalf("got %d results", len(results))
	}
	if results[0].URL != "https://example.com/a" || results[0].Title != "Example A" {
		t.Errorf("result0 = %+v", results[0])
	}
	if results[1].URL != "https://b.example.com/b" || results[1].Title != "Title B" {
		t.Errorf("result1 = %+v", results[1])
	}
}

func TestIsTransientDdgError(t *testing.T) {
	for _, msg := range []string{
		"DDG detected an anomaly",
		"you are making requests too quickly",
		"network: ETIMEDOUT",
	} {
		if !isTransientDdgError(msg) {
			t.Errorf("expected transient: %q", msg)
		}
	}
	if isTransientDdgError("query is required") {
		t.Errorf("unexpected transient")
	}
}

func TestSearchDDGApiParsesFixture(t *testing.T) {
	djs := `DDG.pageLayout.load('d', [{"t":"First","u":"https://first.example.com","a":"first abstract"},{"t":"Second","u":"https://second.example.com","a":"second abstract"}]);DDG.duckbar.load('images', {});`
	tokenBody := `<html><script>vqd="4-5-6";</script></html>`
	fetch := fakeFetch(map[string]string{
		"duckduckgo.com/?q=": tokenBody,
		"links.duckduckgo.com": djs,
	})
	results, err := searchDDGApi(context.Background(), fetch, "test", 8)
	if err != nil {
		t.Fatalf("searchDDGApi: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results", len(results))
	}
	if results[0].Title != "First" || results[0].URL != "https://first.example.com" || results[0].AbstractText != "first abstract" {
		t.Errorf("result0 = %+v", results[0])
	}
}

func TestSearchDDGApiReportsAnomaly(t *testing.T) {
	fetch := fakeFetch(map[string]string{
		"duckduckgo.com/?q=": `<html>vqd="1-2-3";</html>`,
		"links.duckduckgo.com": "DDG.deep.anomalyDetectionBlock = true;",
	})
	_, err := searchDDGApi(context.Background(), fetch, "test", 8)
	if err == nil {
		t.Fatal("expected anomaly error")
	}
	if !strings.Contains(err.Error(), "anomaly") || !isTransientDdgError(err.Error()) {
		t.Errorf("err = %q", err)
	}
}

func TestSearchDDGHtmlParsesViaDOM(t *testing.T) {
	fetch := fakeFetch(map[string]string{
		"html.duckduckgo.com": `<a class="result__a" href="//example.com/x">X</a>`,
	})
	results, err := searchDDGHtml(context.Background(), fetch, "x", 8)
	if err != nil {
		t.Fatalf("searchDDGHtml: %v", err)
	}
	if results[0].URL != "https://example.com/x" {
		t.Errorf("url = %q", results[0].URL)
	}
}

func TestFallbackChainCollectsAllFailures(t *testing.T) {
	fetch := alwaysErrFetch("network: ETIMEDOUT")
	_, err := searchDuckDuckGo(context.Background(), fetch, "q", 8)
	if err == nil {
		t.Fatal("expected all-fail error")
	}
	for _, part := range []string{"ddg_primary:", "ddg_html:", "ddg_lite:", "All DuckDuckGo endpoints failed"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("missing %q in %q", part, err)
		}
	}
}

func TestFallbackChainStopsAtFirstSuccess(t *testing.T) {
	var calls []string
	htmlBody := `<a href="//example.com/win">Win</a>`
	fetch := func(ctx context.Context, rawURL string) (string, error) {
		calls = append(calls, rawURL)
		if strings.Contains(rawURL, "html.duckduckgo.com") {
			return htmlBody, nil
		}
		return "", fmt.Errorf("network: boom")
	}
	results, err := searchDuckDuckGo(context.Background(), fetch, "q", 8)
	if err != nil {
		t.Fatalf("searchDuckDuckGo: %v", err)
	}
	if results[0].URL != "https://example.com/win" {
		t.Errorf("url = %q", results[0].URL)
	}
	joined := strings.Join(calls, " ")
	if !strings.Contains(joined, "duckduckgo.com/?q=") {
		t.Errorf("api not tried: %s", joined)
	}
	if !strings.Contains(joined, "html.duckduckgo.com") {
		t.Errorf("html not tried: %s", joined)
	}
	if strings.Contains(joined, "lite.duckduckgo.com") {
		t.Errorf("lite must not run: %s", joined)
	}
}

func TestWithRetryRetriesTransient(t *testing.T) {
	attempts := 0
	op := func(ctx context.Context) ([]WebSearchResult, error) {
		attempts++
		if attempts < 3 {
			return nil, fmt.Errorf("network: ETIMEDOUT")
		}
		return nil, fmt.Errorf("No DuckDuckGo results")
	}
	_, err := withRetry(context.Background(), 2, op)
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestWithRetryDoesNotRetryPersistent(t *testing.T) {
	attempts := 0
	op := func(ctx context.Context) ([]WebSearchResult, error) {
		attempts++
		return nil, fmt.Errorf("No DuckDuckGo results")
	}
	_, err := withRetry(context.Background(), 2, op)
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
}
