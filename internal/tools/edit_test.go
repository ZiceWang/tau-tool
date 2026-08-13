package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func editToolResult(t *testing.T, cwd, path string, edits []Edit) *mcp.CallToolResult {
	t.Helper()
	t.Chdir(cwd)
	handler := CreateEditTool()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, EditInput{Path: path, Edits: edits})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	return result
}

func TestEditSingleReplacement(t *testing.T) {
	path := writeTempFile(t, "f.go", "package main\n\nfunc foo() {}\n")
	dir := filepath.Dir(path)

	result := editToolResult(t, dir, "f.go", []Edit{{OldText: "func foo() {}", NewText: "func bar() {}"}})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}
	want := "Successfully replaced 1 block(s) in f.go."
	if got := textOf(result); got != want {
		t.Errorf("result = %q, want %q", got, want)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "func bar() {}") {
		t.Errorf("file not updated: %q", data)
	}
	// Patch and diff should be present in structured content.
	if result.StructuredContent == nil {
		t.Errorf("missing structured content")
	}
}

func TestEditMultipleDisjoint(t *testing.T) {
	path := writeTempFile(t, "f.txt", "a\nb\nc\nd\ne\n")
	dir := filepath.Dir(path)

	result := editToolResult(t, dir, "f.txt", []Edit{
		{OldText: "b", NewText: "B"},
		{OldText: "d", NewText: "D"},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}
	data, _ := os.ReadFile(path)
	want := "a\nB\nc\nD\ne\n"
	if string(data) != want {
		t.Errorf("file = %q, want %q", data, want)
	}
	if got := textOf(result); !strings.Contains(got, "Successfully replaced 2 block(s) in f.txt.") {
		t.Errorf("result = %q", got)
	}
}

func TestEditNotFound(t *testing.T) {
	path := writeTempFile(t, "f.txt", "hello world\n")
	dir := filepath.Dir(path)

	result := editToolResult(t, dir, "f.txt", []Edit{{OldText: "goodbye", NewText: "bye"}})
	if !result.IsError {
		t.Fatalf("expected error")
	}
	want := "Could not find the exact text in f.txt. The old text must match exactly including all whitespace and newlines."
	if got := textOf(result); got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

func TestEditNotFoundMulti(t *testing.T) {
	path := writeTempFile(t, "f.txt", "hello\n")
	dir := filepath.Dir(path)

	result := editToolResult(t, dir, "f.txt", []Edit{
		{OldText: "hello", NewText: "hi"},
		{OldText: "nope", NewText: "x"},
	})
	if !result.IsError {
		t.Fatalf("expected error")
	}
	want := "Could not find edits[1] in f.txt. The oldText must match exactly including all whitespace and newlines."
	if got := textOf(result); got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

func TestEditDuplicate(t *testing.T) {
	path := writeTempFile(t, "f.txt", "x\ny\nx\n")
	dir := filepath.Dir(path)

	result := editToolResult(t, dir, "f.txt", []Edit{{OldText: "x", NewText: "z"}})
	if !result.IsError {
		t.Fatalf("expected error")
	}
	want := "Found 2 occurrences of the text in f.txt. The text must be unique. Please provide more context to make it unique."
	if got := textOf(result); got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

func TestEditEmptyOldText(t *testing.T) {
	path := writeTempFile(t, "f.txt", "hello\n")
	dir := filepath.Dir(path)

	result := editToolResult(t, dir, "f.txt", []Edit{{OldText: "", NewText: "z"}})
	if !result.IsError {
		t.Fatalf("expected error")
	}
	if got := textOf(result); got != "oldText must not be empty in f.txt." {
		t.Errorf("error = %q", got)
	}
}

func TestEditOverlap(t *testing.T) {
	path := writeTempFile(t, "f.txt", "abcdef\n")
	dir := filepath.Dir(path)

	result := editToolResult(t, dir, "f.txt", []Edit{
		{OldText: "abc", NewText: "ABC"},
		{OldText: "bcd", NewText: "xyz"},
	})
	if !result.IsError {
		t.Fatalf("expected error")
	}
	if !strings.Contains(textOf(result), "overlap in f.txt.") {
		t.Errorf("error = %q", textOf(result))
	}
}

func TestEditNoChange(t *testing.T) {
	path := writeTempFile(t, "f.txt", "hello\n")
	dir := filepath.Dir(path)

	result := editToolResult(t, dir, "f.txt", []Edit{{OldText: "hello", NewText: "hello"}})
	if !result.IsError {
		t.Fatalf("expected error")
	}
	if !strings.Contains(textOf(result), "No changes made to f.txt.") {
		t.Errorf("error = %q", textOf(result))
	}
}

func TestEditPreservesCRLF(t *testing.T) {
	path := writeTempFile(t, "f.txt", "a\r\nb\r\nc\r\n")
	dir := filepath.Dir(path)

	result := editToolResult(t, dir, "f.txt", []Edit{{OldText: "b", NewText: "B"}})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}
	data, _ := os.ReadFile(path)
	want := "a\r\nB\r\nc\r\n"
	if string(data) != want {
		t.Errorf("file = %q, want %q", data, want)
	}
}

func TestEditPreservesBOM(t *testing.T) {
	path := writeTempFile(t, "f.txt", "\uFEFFa\nb\n")
	dir := filepath.Dir(path)

	result := editToolResult(t, dir, "f.txt", []Edit{{OldText: "a", NewText: "A"}})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "\uFEFFA\nb\n") {
		t.Errorf("file = %q", data)
	}
}

func TestEditLegacyOldNewText(t *testing.T) {
	path := writeTempFile(t, "f.txt", "hello\n")
	dir := filepath.Dir(path)
	t.Chdir(dir)

	handler := CreateEditTool()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, EditInput{})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	// Legacy form arrives as oldText/newText at top level via UnmarshalJSON.
	raw := `{"path":"f.txt","oldText":"hello","newText":"hi"}`
	var in EditInput
	if err := in.UnmarshalJSON([]byte(raw)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	result, _, err = handler(context.Background(), &mcp.CallToolRequest{}, in)
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hi\n" {
		t.Errorf("file = %q", data)
	}
}

func TestEditEditsAsJSONString(t *testing.T) {
	path := writeTempFile(t, "f.txt", "hello\n")
	dir := filepath.Dir(path)
	t.Chdir(dir)

	raw := `{"path":"f.txt","edits":"[{\"oldText\":\"hello\",\"newText\":\"hi\"}]"}`
	var in EditInput
	if err := in.UnmarshalJSON([]byte(raw)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(in.Edits) != 1 {
		t.Fatalf("edits = %d, want 1", len(in.Edits))
	}
	handler := CreateEditTool()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, in)
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}
}

func TestEditFuzzyMatchTrailingWhitespace(t *testing.T) {
	path := writeTempFile(t, "f.txt", "const x = 1;   \nother line\n")
	dir := filepath.Dir(path)

	// The model omits trailing whitespace; fuzzy match must still apply.
	result := editToolResult(t, dir, "f.txt", []Edit{{OldText: "const x = 1;", NewText: "const x = 2;"}})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "const x = 2;") {
		t.Errorf("file = %q", data)
	}
}

func TestEditMissingFile(t *testing.T) {
	dir := t.TempDir()
	result := editToolResult(t, dir, "nope.txt", []Edit{{OldText: "a", NewText: "b"}})
	if !result.IsError {
		t.Fatalf("expected error")
	}
	if !strings.Contains(textOf(result), "Could not edit file: nope.txt.") {
		t.Errorf("error = %q", textOf(result))
	}
}

func TestEditUnifiedPatchPresent(t *testing.T) {
	path := writeTempFile(t, "f.txt", "a\nb\nc\n")
	dir := filepath.Dir(path)

	result := editToolResult(t, dir, "f.txt", []Edit{{OldText: "b", NewText: "B"}})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}
	patch, ok := result.StructuredContent.(map[string]any)["patch"].(string)
	if !ok || !strings.Contains(patch, "--- f.txt") || !strings.Contains(patch, "@@") {
		t.Errorf("patch missing or malformed: %q", patch)
	}
}
