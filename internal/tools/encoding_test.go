package tools

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- read/write/edit encoding round-trips ----

func TestWriteAndReadGBK(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	handler := CreateWriteTool()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, WriteInput{
		Path:     "gbk.txt",
		Content:  "中文测试",
		Encoding: "gbk",
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if result.IsError {
		t.Fatalf("write failed: %s", textOf(result))
	}

	// File on disk must be GBK bytes, not UTF-8.
	data, _ := os.ReadFile("gbk.txt")
	if string(data) == "中文测试" {
		t.Errorf("file was written as UTF-8, expected GBK bytes")
	}
	if !bytes.Contains(gbkBytes("中文测试"), data) {
		t.Errorf("file bytes not GBK: %x", data)
	}

	readHandler := CreateReadTool()
	readRes, _, err := readHandler(context.Background(), &mcp.CallToolRequest{}, ReadInput{
		Path:     "gbk.txt",
		Encoding: "gbk",
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if readRes.IsError {
		t.Fatalf("read failed: %s", textOf(readRes))
	}
	if got := textOf(readRes); got != "中文测试" {
		t.Errorf("read content = %q, want 中文测试", got)
	}
}

func TestEditGBK(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Write a GBK file.
	handler := CreateWriteTool()
	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, WriteInput{
		Path:     "gbk.txt",
		Content:  "第一行\n第二行\n",
		Encoding: "gbk",
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	editHandler := CreateEditTool()
	result, _, err := editHandler(context.Background(), &mcp.CallToolRequest{}, EditInput{
		Path:     "gbk.txt",
		Edits:    []Edit{{OldText: "第一行", NewText: "修改后"}},
		Encoding: "gbk",
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if result.IsError {
		t.Fatalf("edit failed: %s", textOf(result))
	}

	// Read back with gbk.
	readHandler := CreateReadTool()
	readRes, _, err := readHandler(context.Background(), &mcp.CallToolRequest{}, ReadInput{
		Path:     "gbk.txt",
		Encoding: "gbk",
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := textOf(readRes); got != "修改后\n第二行\n" {
		t.Errorf("read content = %q, want 修改后\\n第二行\\n", got)
	}
}

func TestReadGBKWithoutEncodingIsGarbled(t *testing.T) {
	// Sanity check that without encoding=gbk, GBK bytes are NOT clean UTF-8
	// (proving the encoding param is what makes it work).
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("g.txt", gbkBytes("中文测试"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	readHandler := CreateReadTool()
	res, _, err := readHandler(context.Background(), &mcp.CallToolRequest{}, ReadInput{Path: "g.txt"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if textOf(res) == "中文测试" {
		t.Errorf("unexpectedly decoded as UTF-8")
	}
}

// ---- bash output encoding via settings ----

// TestBashShellEncodingSetting verifies the bash tool decodes output using the
// configured shellEncoding. It pipes a pre-written GBK file through cmd's
// `type`, which streams raw bytes without transcoding, so the output encoding
// is deterministic regardless of the shared (and mutable) Windows console code
// page.
func TestBashShellEncodingSetting(t *testing.T) {
	bin, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Skip("cmd.exe not found")
	}

	dir := t.TempDir()
	t.Setenv(EnvCwd, dir)
	if err := os.WriteFile(filepath.Join(dir, "gbk.txt"), gbkBytes("中文测试"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	store := NewSettingsStoreWithPath(filepath.Join(t.TempDir(), "settings.json"))
	if err := store.Set("shellPath", bin); err != nil {
		t.Fatalf("set shellPath: %v", err)
	}
	if err := store.Set("shellEncoding", "gbk"); err != nil {
		t.Fatalf("set shellEncoding: %v", err)
	}

	deps := bashDeps{settings: store}
	result, _, err := deps.handle(context.Background(), &mcp.CallToolRequest{}, BashInput{Command: "type gbk.txt"})
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if result.IsError {
		t.Fatalf("bash failed: %s", textOf(result))
	}
	text := textOf(result)
	if strings.TrimRight(text, "\r\n") != "中文测试" {
		t.Errorf("output = %q, want 中文测试", text)
	}
}

// TestDecodedReaderGBK verifies the pipe decoder used by the bash tool turns
// GBK bytes into correct UTF-8 text, deterministically (no shell involved).
func TestDecodedReaderGBK(t *testing.T) {
	acc := NewOutputAccumulator(OutputAccumulatorOptions{})
	reader, err := decodedReader(bytes.NewReader(gbkBytes("中文测试\n第二行")), "gbk")
	if err != nil {
		t.Fatalf("decodedReader: %v", err)
	}
	buf := make([]byte, 4) // tiny buffer to force partial reads across GBK boundaries
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			_ = acc.Append(buf[:n])
		}
		if readErr != nil {
			break
		}
	}
	_ = acc.Finish()
	snap, _ := acc.Snapshot(false)
	if snap.Content != "中文测试\n第二行" {
		t.Errorf("content = %q, want 中文测试\\n第二行", snap.Content)
	}
}

// gbkBytes encodes s to GBK for test fixtures.
func gbkBytes(s string) []byte {
	data, err := encodeBytes(s, "gbk")
	if err != nil {
		panic(err)
	}
	return data
}

func TestDecodeBytesRoundTrip(t *testing.T) {
	cases := []string{"中文测试", "こんにちは", "안녕하세요", "héllo", "繁體中文"}
	encs := []string{"gbk", "shift-jis", "euc-kr", "latin1", "big5"}
	for _, enc := range encs {
		for _, s := range cases {
			data, err := encodeBytes(s, enc)
			if err != nil {
				t.Logf("encode %s in %s: %v (skipping)", s, enc, err)
				continue
			}
			got, err := decodeBytes(data, enc)
			if err != nil {
				t.Errorf("decode %s in %s: %v", s, enc, err)
				continue
			}
			if got != s {
				t.Errorf("%s round-trip in %s = %q", s, enc, got)
			}
		}
	}
}

func TestUnsupportedEncoding(t *testing.T) {
	if _, err := decodeBytes([]byte("x"), "klingon"); err == nil {
		t.Errorf("expected error for unsupported encoding")
	}
}
