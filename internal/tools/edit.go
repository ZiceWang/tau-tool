package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Edit is a single targeted replacement (matches pi's edit schema entry).
type Edit struct {
	OldText string `json:"oldText" jsonschema:"Exact text for one targeted replacement. It must be unique in the original file and must not overlap with any other edits[].oldText in the same call."`
	NewText string `json:"newText" jsonschema:"Replacement text for this targeted edit."`
}

// EditInput matches pi's edit schema: path + edits[].
type EditInput struct {
	Path     string `json:"path" jsonschema:"Path to the file to edit (relative or absolute)"`
	Edits    []Edit `json:"edits" jsonschema:"One or more targeted replacements. Each edit is matched against the original file, not incrementally. Do not include overlapping or nested edits. If two changes touch the same block or nearby lines, merge them into one edit instead."`
	Encoding string `json:"encoding,omitempty" jsonschema:"Text encoding of the file: utf-8 (default), gbk, gb18030, big5, shift-jis, euc-jp, euc-kr, latin1, windows-1252"`
}

// EditToolDescription is pi's exact edit tool description.
const EditToolDescription = "Edit a single file using exact text replacement. Every edits[].oldText must match a unique, non-overlapping region of the original file. If two changes affect the same block or nearby lines, merge them into one edit instead of emitting overlapping edits. Do not include large unchanged regions just to connect distant changes."

// EditToolPromptSnippet and guidelines mirror pi's system prompt contributions.
const (
	EditToolPromptSnippet = "Make precise file edits with exact text replacement, including multiple disjoint edits in one call"
	EditToolGuidelines    = "Use edit for precise changes (edits[].oldText must match exactly)\n" +
		"When changing multiple separate locations in one file, use one edit call with multiple entries in edits[] instead of multiple edit calls\n" +
		"Each edits[].oldText is matched against the original file, not after earlier edits are applied. Do not emit overlapping or nested edits. Merge nearby changes into one edit.\n" +
		"Keep edits[].oldText as small as possible while still being unique in the file. Do not pad with large unchanged regions."
)

// EditToolGuidelineList exposes pi's edit guidelines as a list.
func EditToolGuidelineList() []string {
	return strings.Split(EditToolGuidelines, "\n")
}

// UnmarshalJSON normalizes the many input shapes some models emit:
//   - edits as a JSON array
//   - edits as a JSON-encoded string that itself parses to an array
//   - legacy top-level oldText/newText fields
func (in *EditInput) UnmarshalJSON(data []byte) error {
	var raw struct {
		Path    string          `json:"path"`
		Edits   json.RawMessage `json:"edits"`
		OldText string          `json:"oldText"`
		NewText string          `json:"newText"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	in.Path = raw.Path

	var edits []Edit
	if len(raw.Edits) > 0 {
		var s string
		if err := json.Unmarshal(raw.Edits, &s); err == nil {
			// edits came in as a JSON string; try to parse it as an array
			var parsed []Edit
			if err := json.Unmarshal([]byte(s), &parsed); err == nil {
				edits = parsed
			}
		} else {
			var arr []Edit
			if err := json.Unmarshal(raw.Edits, &arr); err == nil {
				edits = arr
			}
		}
	}

	if raw.OldText != "" || raw.NewText != "" {
		edits = append(edits, Edit{OldText: raw.OldText, NewText: raw.NewText})
	}

	in.Edits = edits
	return nil
}

type editDeps struct {
	ops EditOperations
}

// EditOperations lets the edit tool delegate file editing to remote systems.
type EditOperations interface {
	ReadFile(absolutePath string) ([]byte, error)
	WriteFile(absolutePath string, data []byte) error
	Access(absolutePath string) error
}

type localEditOperations struct{}

func (localEditOperations) ReadFile(absolutePath string) ([]byte, error) {
	return os.ReadFile(absolutePath)
}

func (localEditOperations) WriteFile(absolutePath string, data []byte) error {
	return os.WriteFile(absolutePath, data, 0o644)
}

func (localEditOperations) Access(absolutePath string) error {
	// pi checks R_OK | W_OK; just attempt to open for read-write.
	f, err := os.OpenFile(absolutePath, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	return f.Close()
}

// CreateEditTool returns the edit tool handler.
func CreateEditTool(ops ...EditOperations) mcp.ToolHandlerFor[EditInput, any] {
	deps := editDeps{}
	if len(ops) > 0 && ops[0] != nil {
		deps.ops = ops[0]
	} else {
		deps.ops = localEditOperations{}
	}
	return deps.handle
}

func (d editDeps) handle(_ context.Context, _ *mcp.CallToolRequest, in EditInput) (*mcp.CallToolResult, any, error) {
	if in.Path == "" {
		return toolError("edit: path is required"), nil, nil
	}
	if len(in.Edits) == 0 {
		return toolError("Edit tool input is invalid. edits must contain at least one replacement."), nil, nil
	}

	absolutePath := resolveToCwd(in.Path)

	var resultContent string
	var diff, patch string
	var firstChangedLine int

	err := withFileMutationQueue(absolutePath, func() error {
		// Check if file exists and is writable.
		if err := d.ops.Access(absolutePath); err != nil {
			errText := err.Error()
			if os.IsNotExist(err) {
				errText = err.Error()
			}
			return fmt.Errorf("Could not edit file: %s. %s.", in.Path, errText)
		}

		buffer, err := d.ops.ReadFile(absolutePath)
		if err != nil {
			return err
		}
		rawContent, err := decodeBytes(buffer, in.Encoding)
		if err != nil {
			return err
		}

		// Strip BOM before matching.
		bom, content := stripBom(rawContent)
		originalEnding := detectLineEnding(content)
		normalizedContent := normalizeToLF(content)
		baseContent, newContent, err := applyEditsToNormalizedContent(normalizedContent, in.Edits, in.Path)
		if err != nil {
			return err
		}

		finalContent := bom + restoreLineEndings(newContent, originalEnding)
		finalBytes, err := encodeBytes(finalContent, in.Encoding)
		if err != nil {
			return err
		}
		if err := d.ops.WriteFile(absolutePath, finalBytes); err != nil {
			return err
		}

		diffStr, first := generateDiffString(baseContent, newContent, 4)
		diff = diffStr
		firstChangedLine = first
		patch = generateUnifiedPatch(in.Path, baseContent, newContent, 4)
		resultContent = fmt.Sprintf("Successfully replaced %d block(s) in %s.", len(in.Edits), in.Path)
		return nil
	})
	if err != nil {
		return toolError(err.Error()), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: resultContent}},
		StructuredContent: map[string]any{
			"diff":             diff,
			"patch":            patch,
			"firstChangedLine": firstChangedLine,
		},
	}, nil, nil
}

// ---- line ending / normalization helpers (port of edit-diff.ts) ----

func detectLineEnding(content string) string {
	crlfIdx := strings.Index(content, "\r\n")
	lfIdx := strings.Index(content, "\n")
	switch {
	case lfIdx == -1, crlfIdx == -1:
		return "\n"
	default:
		if crlfIdx < lfIdx {
			return "\r\n"
		}
		return "\n"
	}
}

func normalizeToLF(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

func restoreLineEndings(text, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}

// stripBom returns the BOM (if any) and the text without it.
func stripBom(content string) (string, string) {
	if strings.HasPrefix(content, "\uFEFF") {
		return "\uFEFF", content[len("\uFEFF"):]
	}
	return "", content
}

// ---- fuzzy matching (port of edit-diff.ts) ----

// normalizeForFuzzyMatch applies progressive transformations:
// strip trailing whitespace per line, normalize smart quotes, dashes, and
// special spaces to ASCII equivalents.
func normalizeForFuzzyMatch(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	text = strings.Join(lines, "\n")
	text = strings.NewReplacer(
		"\u2018", "'", "\u2019", "'", "\u201A", "'", "\u201B", "'",
		"\u201C", "\"", "\u201D", "\"", "\u201E", "\"", "\u201F", "\"",
		"\u2010", "-", "\u2011", "-", "\u2012", "-", "\u2013", "-",
		"\u2014", "-", "\u2015", "-", "\u2212", "-",
		"\u00A0", " ", "\u2002", " ", "\u2003", " ", "\u2004", " ", "\u2005", " ",
		"\u2006", " ", "\u2007", " ", "\u2008", " ", "\u2009", " ", "\u200A", " ",
		"\u202F", " ", "\u205F", " ", "\u3000", " ",
	).Replace(text)
	return text
}

type fuzzyMatchResult struct {
	found                bool
	index                int
	matchLength          int
	usedFuzzyMatch       bool
	contentForReplacement string
}

func fuzzyFindText(content, oldText string) fuzzyMatchResult {
	// Try exact match first.
	exactIndex := strings.Index(content, oldText)
	if exactIndex != -1 {
		return fuzzyMatchResult{
			found:                 true,
			index:                 exactIndex,
			matchLength:           len(oldText),
			usedFuzzyMatch:        false,
			contentForReplacement: content,
		}
	}

	// Try fuzzy match in normalized space.
	fuzzyContent := normalizeForFuzzyMatch(content)
	fuzzyOldText := normalizeForFuzzyMatch(oldText)
	fuzzyIndex := strings.Index(fuzzyContent, fuzzyOldText)
	if fuzzyIndex == -1 {
		return fuzzyMatchResult{found: false, index: -1, matchLength: 0, usedFuzzyMatch: false, contentForReplacement: content}
	}

	return fuzzyMatchResult{
		found:                 true,
		index:                 fuzzyIndex,
		matchLength:           len(fuzzyOldText),
		usedFuzzyMatch:        true,
		contentForReplacement: fuzzyContent,
	}
}

func countOccurrences(content, oldText string) int {
	fuzzyContent := normalizeForFuzzyMatch(content)
	fuzzyOldText := normalizeForFuzzyMatch(oldText)
	if fuzzyOldText == "" {
		return 0
	}
	return strings.Count(fuzzyContent, fuzzyOldText)
}

func getNotFoundError(path string, editIndex, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("Could not find the exact text in %s. The old text must match exactly including all whitespace and newlines.", path)
	}
	return fmt.Errorf("Could not find edits[%d] in %s. The oldText must match exactly including all whitespace and newlines.", editIndex, path)
}

func getDuplicateError(path string, editIndex, totalEdits, occurrences int) error {
	if totalEdits == 1 {
		return fmt.Errorf("Found %d occurrences of the text in %s. The text must be unique. Please provide more context to make it unique.", occurrences, path)
	}
	return fmt.Errorf("Found %d occurrences of edits[%d] in %s. Each oldText must be unique. Please provide more context to make it unique.", occurrences, editIndex, path)
}

func getEmptyOldTextError(path string, editIndex, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("oldText must not be empty in %s.", path)
	}
	return fmt.Errorf("edits[%d].oldText must not be empty in %s.", editIndex, path)
}

func getNoChangeError(path string, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("No changes made to %s. The replacement produced identical content. This might indicate an issue with special characters or the text not existing as expected.", path)
	}
	return fmt.Errorf("No changes made to %s. The replacements produced identical content.", path)
}

type matchedEdit struct {
	editIndex   int
	matchIndex  int
	matchLength int
	newText     string
}

type textReplacement struct {
	matchIndex  int
	matchLength int
	newText     string
}

// applyEditsToNormalizedContent applies exact-text replacements to
// LF-normalized content. All edits are matched against the same original
// content; replacements are applied in reverse order so offsets remain stable.
func applyEditsToNormalizedContent(normalizedContent string, edits []Edit, path string) (string, string, error) {
	normalizedEdits := make([]Edit, len(edits))
	for i, e := range edits {
		normalizedEdits[i] = Edit{OldText: normalizeToLF(e.OldText), NewText: normalizeToLF(e.NewText)}
	}

	for i := range normalizedEdits {
		if len(normalizedEdits[i].OldText) == 0 {
			return "", "", getEmptyOldTextError(path, i, len(normalizedEdits))
		}
	}

	initialMatches := make([]fuzzyMatchResult, len(normalizedEdits))
	usedFuzzyMatch := false
	for i, e := range normalizedEdits {
		initialMatches[i] = fuzzyFindText(normalizedContent, e.OldText)
		if initialMatches[i].usedFuzzyMatch {
			usedFuzzyMatch = true
		}
	}

	replacementBaseContent := normalizedContent
	if usedFuzzyMatch {
		replacementBaseContent = normalizeForFuzzyMatch(normalizedContent)
	}

	matched := make([]matchedEdit, 0, len(normalizedEdits))
	for i, e := range normalizedEdits {
		matchResult := fuzzyFindText(replacementBaseContent, e.OldText)
		if !matchResult.found {
			return "", "", getNotFoundError(path, i, len(normalizedEdits))
		}

		occurrences := countOccurrences(replacementBaseContent, e.OldText)
		if occurrences > 1 {
			return "", "", getDuplicateError(path, i, len(normalizedEdits), occurrences)
		}

		matched = append(matched, matchedEdit{
			editIndex:   i,
			matchIndex:  matchResult.index,
			matchLength: matchResult.matchLength,
			newText:     e.NewText,
		})
	}

	// Sort by match index and check overlap.
	sortEditsByIndex(matched)
	for i := 1; i < len(matched); i++ {
		prev := matched[i-1]
		cur := matched[i]
		if prev.matchIndex+prev.matchLength > cur.matchIndex {
			return "", "", fmt.Errorf("edits[%d] and edits[%d] overlap in %s. Merge them into one edit or target disjoint regions.", prev.editIndex, cur.editIndex, path)
		}
	}

	baseContent := normalizedContent
	var newContent string
	if usedFuzzyMatch {
		repls := make([]textReplacement, len(matched))
		for i, m := range matched {
			repls[i] = textReplacement{matchIndex: m.matchIndex, matchLength: m.matchLength, newText: m.newText}
		}
		var err error
		newContent, err = applyReplacementsPreservingUnchangedLines(normalizedContent, replacementBaseContent, repls)
		if err != nil {
			return "", "", err
		}
	} else {
		repls := make([]textReplacement, len(matched))
		for i, m := range matched {
			repls[i] = textReplacement{matchIndex: m.matchIndex, matchLength: m.matchLength, newText: m.newText}
		}
		newContent = applyReplacements(replacementBaseContent, repls)
	}

	if baseContent == newContent {
		return "", "", getNoChangeError(path, len(normalizedEdits))
	}

	return baseContent, newContent, nil
}

func sortEditsByIndex(edits []matchedEdit) {
	for i := 1; i < len(edits); i++ {
		for j := i; j > 0 && edits[j-1].matchIndex > edits[j].matchIndex; j-- {
			edits[j-1], edits[j] = edits[j], edits[j-1]
		}
	}
}

func applyReplacements(content string, replacements []textReplacement, offset ...int) string {
	off := 0
	if len(offset) > 0 {
		off = offset[0]
	}
	result := content
	for i := len(replacements) - 1; i >= 0; i-- {
		r := replacements[i]
		matchIndex := r.matchIndex - off
		result = result[:matchIndex] + r.newText + result[matchIndex+r.matchLength:]
	}
	return result
}

// ---- line-span preservation for fuzzy matching ----

type lineSpan struct {
	start, end int
}

var editLineSplitterRe = regexp.MustCompile(`[^\n]*\n|[^\n]+`)

func splitLinesWithEndings(content string) []string {
	return editLineSplitterRe.FindAllString(content, -1)
}

func getLineSpans(content string) []lineSpan {
	offset := 0
	lines := splitLinesWithEndings(content)
	spans := make([]lineSpan, len(lines))
	for i, line := range lines {
		spans[i] = lineSpan{start: offset, end: offset + len(line)}
		offset = spans[i].end
	}
	return spans
}

func getReplacementLineRange(lines []lineSpan, r textReplacement) (int, int) {
	replacementStart := r.matchIndex
	replacementEnd := r.matchIndex + r.matchLength

	startLine := -1
	for i, l := range lines {
		if replacementStart >= l.start && replacementStart < l.end {
			startLine = i
			break
		}
	}
	if startLine == -1 {
		return -1, -1
	}

	endLine := startLine
	for endLine < len(lines) && lines[endLine].end < replacementEnd {
		endLine++
	}
	if endLine >= len(lines) {
		return -1, -1
	}

	return startLine, endLine + 1
}

// applyReplacementsPreservingUnchangedLines rewrites only the touched lines
// from the normalized base, copying all other lines back from the original.
func applyReplacementsPreservingUnchangedLines(originalContent, baseContent string, replacements []textReplacement) (string, error) {
	originalLines := splitLinesWithEndings(originalContent)
	baseLines := getLineSpans(baseContent)
	if len(originalLines) != len(baseLines) {
		return "", fmt.Errorf("Cannot preserve unchanged lines because the base content has a different line count.")
	}

	type group struct {
		startLine, endLine int
		replacements       []textReplacement
	}
	var groups []group
	sorted := make([]textReplacement, len(replacements))
	copy(sorted, replacements)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j-1].matchIndex > sorted[j].matchIndex; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}

	for _, r := range sorted {
		start, end := getReplacementLineRange(baseLines, r)
		if start == -1 || end == -1 {
			return "", fmt.Errorf("Replacement range is outside the base content.")
		}
		if len(groups) > 0 {
			cur := &groups[len(groups)-1]
			if start < cur.endLine {
				if end > cur.endLine {
					cur.endLine = end
				}
				cur.replacements = append(cur.replacements, r)
				continue
			}
		}
		groups = append(groups, group{startLine: start, endLine: end, replacements: []textReplacement{r}})
	}

	originalLineIndex := 0
	var result strings.Builder
	for _, g := range groups {
		result.WriteString(strings.Join(originalLines[originalLineIndex:g.startLine], ""))
		groupStartOffset := baseLines[g.startLine].start
		groupEndOffset := baseLines[g.endLine-1].end
		result.WriteString(applyReplacements(baseContent[groupStartOffset:groupEndOffset], g.replacements, groupStartOffset))
		originalLineIndex = g.endLine
	}
	result.WriteString(strings.Join(originalLines[originalLineIndex:], ""))
	return result.String(), nil
}