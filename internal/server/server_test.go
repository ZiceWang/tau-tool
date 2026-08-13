package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"tau-tool/internal/tools"
)

func newTestServer(t *testing.T) *mcp.ClientSession {
	// Isolate from the user's real settings so the generated description and
	// tool behavior are deterministic.
	t.Setenv(tools.EnvSettings, filepath.Join(t.TempDir(), "settings.json"))

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	srv := New()
	ctx := context.Background()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return session
}

func TestServerListTools(t *testing.T) {
	session := newTestServer(t)

	list, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 6 {
		t.Fatalf("expected 6 tools, got %d", len(list.Tools))
	}

	want := map[string]string{
		"read":      tools.ReadToolDescription,
		"write":     tools.WriteToolDescription,
		"edit":      tools.EditToolDescription,
		"bash":      tools.BashToolDescription,
		"settings":  tools.SettingsToolDescription(nil),
		"websearch": tools.WebSearchToolDescription,
	}
	got := map[string]string{}
	for _, tool := range list.Tools {
		got[tool.Name] = tool.Description
	}
	for name, desc := range want {
		if got[name] != desc {
			t.Errorf("tool %s description mismatch:\n got %q\nwant %q", name, got[name], desc)
		}
	}
}

func TestServerCallWriteAndRead(t *testing.T) {
	t.Chdir(t.TempDir())
	session := newTestServer(t)

	write, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "write",
		Arguments: map[string]any{"path": "hello.txt", "content": "hello mcp"},
	})
	if err != nil {
		t.Fatalf("CallTool write: %v", err)
	}
	if write.IsError {
		t.Fatalf("write failed: %s", textOf(write))
	}

	read, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "read",
		Arguments: map[string]any{"path": "hello.txt"},
	})
	if err != nil {
		t.Fatalf("CallTool read: %v", err)
	}
	if read.IsError {
		t.Fatalf("read failed: %s", textOf(read))
	}
	if got := textOf(read); got != "hello mcp" {
		t.Errorf("read content = %q", got)
	}
}

func TestServerCallBash(t *testing.T) {
	if _, _, _, err := tools.ResolveShell(""); err != nil {
		t.Skip("no bash available")
	}

	session := newTestServer(t)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "bash",
		Arguments: map[string]any{"command": "echo hi-from-mcp"},
	})
	if err != nil {
		t.Fatalf("CallTool bash: %v", err)
	}
	if result.IsError {
		t.Fatalf("bash failed: %s", textOf(result))
	}
	if !strings.Contains(textOf(result), "hi-from-mcp") {
		t.Errorf("bash output = %q", textOf(result))
	}
}

func TestServerCallEdit(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	session := newTestServer(t)
	path := filepath.Join(dir, "e.txt")
	os.WriteFile(path, []byte("old value\n"), 0o644)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "edit",
		Arguments: map[string]any{"path": "e.txt", "edits": []map[string]any{{"oldText": "old value", "newText": "new value"}}},
	})
	if err != nil {
		t.Fatalf("CallTool edit: %v", err)
	}
	if result.IsError {
		t.Fatalf("edit failed: %s", textOf(result))
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new value\n" {
		t.Errorf("file = %q", data)
	}
}

func textOf(result *mcp.CallToolResult) string {
	var b []byte
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			if len(b) > 0 {
				b = append(b, '\n')
			}
			b = append(b, tc.Text...)
		}
	}
	return string(b)
}
