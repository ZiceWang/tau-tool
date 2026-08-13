package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestWriteCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nested", "deep", "file.txt")
	t.Chdir(dir)

	handler := CreateWriteTool()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, WriteInput{
		Path:    "nested/deep/file.txt",
		Content: "hello",
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q", data)
	}
	want := "Successfully wrote 5 bytes to nested/deep/file.txt"
	if got := textOf(result); got != want {
		t.Errorf("result = %q, want %q", got, want)
	}
}

func TestWriteOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Chdir(dir)

	handler := CreateWriteTool()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, WriteInput{
		Path:    "f.txt",
		Content: "new content",
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}

	data, _ := os.ReadFile(path)
	if string(data) != "new content" {
		t.Errorf("content = %q", data)
	}
}

func TestWriteByteCountMatchesJSLength(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	handler := CreateWriteTool()
	content := "héllo\nworld" // JS content.length counts runes, not bytes
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, WriteInput{
		Path:    "unicode.txt",
		Content: content,
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	want := "Successfully wrote 11 bytes to unicode.txt"
	if got := textOf(result); got != want {
		t.Errorf("result = %q, want %q", got, want)
	}
}

func TestWriteRequiresPath(t *testing.T) {
	handler := CreateWriteTool()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, WriteInput{Content: "x"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if !result.IsError || !strings.Contains(textOf(result), "path is required") {
		t.Errorf("unexpected result: %q", textOf(result))
	}
}

func TestEnvCwdOverridesWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	// Relative paths must resolve against TAU_TOOL_CWD, not the process cwd.
	t.Setenv(EnvCwd, dir)

	handler := CreateWriteTool()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, WriteInput{
		Path:    "env.txt",
		Content: "via env",
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}
	data, err := os.ReadFile(filepath.Join(dir, "env.txt"))
	if err != nil {
		t.Fatalf("file not written to env cwd: %v", err)
	}
	if string(data) != "via env" {
		t.Errorf("content = %q", data)
	}
}
