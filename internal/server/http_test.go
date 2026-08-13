package server

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"tau-tool/internal/tools"
)

func TestHTTPModeE2E(t *testing.T) {
	binary := buildServerBinary(t)
	port := freePort(t)

	var stdout bytes.Buffer
	cmd := exec.Command(binary, "http", "--port", strconv.Itoa(port))
	cmd.Env = append(os.Environ(),
		tools.EnvSettings+"="+filepath.Join(t.TempDir(), "settings.json"),
		tools.EnvCwd+"="+t.TempDir(),
	)
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start http server: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	waitForHTTPServer(ctx, port)

	transport := &mcp.StreamableClientTransport{
		Endpoint:             fmt.Sprintf("http://localhost:%d/mcp", port),
		DisableStandaloneSSE: true,
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "http-e2e"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect to http server: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	// The startup line should announce the listening URL.
	url := fmt.Sprintf("http://localhost:%d/mcp", port)
	if !strings.Contains(stdout.String(), url) {
		t.Errorf("startup output %q missing %q", stdout.String(), url)
	}

	list, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 6 {
		t.Errorf("expected 6 tools, got %d", len(list.Tools))
	}

	// Call a tool end to end: write a file into the env cwd, then read it back.
	write, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "write",
		Arguments: map[string]any{"path": "http.txt", "content": "hello-http"},
	})
	if err != nil {
		t.Fatalf("CallTool write: %v", err)
	}
	if write.IsError {
		t.Fatalf("write failed: %s", textOf(write))
	}

	read, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "read",
		Arguments: map[string]any{"path": "http.txt"},
	})
	if err != nil {
		t.Fatalf("CallTool read: %v", err)
	}
	if read.IsError {
		t.Fatalf("read failed: %s", textOf(read))
	}
	if got := textOf(read); got != "hello-http" {
		t.Errorf("read content = %q", got)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitForHTTPServer(ctx context.Context, port int) {
	for {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), 300*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
}
