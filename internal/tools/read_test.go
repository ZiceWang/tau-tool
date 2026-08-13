package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}

func readToolResult(t *testing.T, path string, offset, limit *int) (*mcp.CallToolResult, error) {
	t.Helper()
	handler := CreateReadTool()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ReadInput{Path: path, Offset: offset, Limit: limit})
	return result, err
}

func TestReadFullFile(t *testing.T) {
	path := writeTempFile(t, "a.txt", "line1\nline2\nline3")
	result, err := readToolResult(t, path, nil, nil)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}
	if got := textOf(result); got != "line1\nline2\nline3" {
		t.Errorf("content = %q", got)
	}
}

func TestReadOffsetLimit(t *testing.T) {
	path := writeTempFile(t, "a.txt", "l1\nl2\nl3\nl4\nl5")
	limit := 2
	offset := 2
	result, err := readToolResult(t, path, &offset, &limit)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := textOf(result); got != "l2\nl3\n\n[2 more lines in file. Use offset=4 to continue.]" {
		t.Errorf("content = %q", got)
	}
}

func TestReadOffsetBeyondEOF(t *testing.T) {
	path := writeTempFile(t, "a.txt", "l1\nl2")
	offset := 10
	result, err := readToolResult(t, path, &offset, nil)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error")
	}
	want := "Offset 10 is beyond end of file (2 lines total)"
	if got := textOf(result); got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

func TestReadMissingFile(t *testing.T) {
	dir := t.TempDir()
	result, err := readToolResult(t, filepath.Join(dir, "nope.txt"), nil, nil)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error")
	}
}

func TestReadRelativePath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rel.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Chdir(dir)

	result, err := readToolResult(t, "rel.txt", nil, nil)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := textOf(result); got != "hello world" {
		t.Errorf("content = %q", got)
	}
}

func TestReadTruncationNotice(t *testing.T) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line")
	}
	path := writeTempFile(t, "big.txt", strings.Join(lines, "\n"))
	limit := 5
	result, err := readToolResult(t, path, nil, &limit)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := textOf(result)
	if !strings.Contains(text, "[95 more lines in file. Use offset=6 to continue.]") {
		t.Errorf("missing continuation notice: %q", text)
	}
}

func TestReadImage(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(img, []byte("\x89PNG\r\n\x1a\n"+"data"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	result, err := readToolResult(t, img, nil, nil)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}
	foundImage := false
	foundNote := false
	for _, c := range result.Content {
		if _, ok := c.(*mcp.ImageContent); ok {
			foundImage = true
		}
		if tc, ok := c.(*mcp.TextContent); ok && strings.HasPrefix(tc.Text, "Read image file [image/png]") {
			foundNote = true
		}
	}
	if !foundImage || !foundNote {
		t.Errorf("image content missing: image=%v note=%v", foundImage, foundNote)
	}
}
