package server

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"tau-tool/internal/tools"
)

func TestStdioServerE2E(t *testing.T) {
	binary := buildServerBinary(t)
	t.Chdir(t.TempDir())

	// Point the spawned server at a temp settings file so the test never
	// touches the user's real ~/.tau-tool/settings.json.
	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(),
		tools.EnvSettings+"="+filepath.Join(t.TempDir(), "settings.json"),
		tools.EnvCwd+"="+t.TempDir(),
	)

	transport := &mcp.CommandTransport{Command: cmd}
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-client"}, nil)
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connect to stdio server: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	// write a file
	write, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "write",
		Arguments: map[string]any{"path": "e2e.txt", "content": "e2e content"},
	})
	if err != nil {
		t.Fatalf("CallTool write: %v", err)
	}
	if write.IsError {
		t.Fatalf("write failed: %s", textOf(write))
	}

	// read it back
	read, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "read",
		Arguments: map[string]any{"path": "e2e.txt"},
	})
	if err != nil {
		t.Fatalf("CallTool read: %v", err)
	}
	if read.IsError {
		t.Fatalf("read failed: %s", textOf(read))
	}
	if got := textOf(read); got != "e2e content" {
		t.Errorf("read content = %q, want %q", got, "e2e content")
	}

	// edit it
	edit, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "edit",
		Arguments: map[string]any{"path": "e2e.txt", "edits": []map[string]any{{"oldText": "e2e content", "newText": "edited content"}}},
	})
	if err != nil {
		t.Fatalf("CallTool edit: %v", err)
	}
	if edit.IsError {
		t.Fatalf("edit failed: %s", textOf(edit))
	}
	structured, err := json.Marshal(edit.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if !strings.Contains(string(structured), "patch") || !strings.Contains(string(structured), "diff") {
		t.Errorf("edit structured content missing diff/patch: %s", structured)
	}

	// bash
	if _, _, _, err := tools.ResolveShell(""); err == nil {
		bash, err := session.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "bash",
			Arguments: map[string]any{"command": "echo e2e-bash"},
		})
		if err != nil {
			t.Fatalf("CallTool bash: %v", err)
		}
		if bash.IsError {
			t.Fatalf("bash failed: %s", textOf(bash))
		}
		if !strings.Contains(textOf(bash), "e2e-bash") {
			t.Errorf("bash output = %q", textOf(bash))
		}
	}

	// settings set + get roundtrip
	setRes, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "settings",
		Arguments: map[string]any{"operation": "set", "key": "shellPath", "value": "pwsh"},
	})
	if err != nil {
		t.Fatalf("CallTool settings set: %v", err)
	}
	if setRes.IsError {
		t.Fatalf("settings set failed: %s", textOf(setRes))
	}
	if !strings.Contains(textOf(setRes), "shellPath = \"pwsh\"") {
		t.Errorf("settings set output = %q", textOf(setRes))
	}
	getRes, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "settings",
		Arguments: map[string]any{"operation": "get", "key": "shellPath"},
	})
	if err != nil {
		t.Fatalf("CallTool settings get: %v", err)
	}
	if getRes.IsError || !strings.Contains(textOf(getRes), "pwsh") {
		t.Errorf("settings get output = %q", textOf(getRes))
	}
}

func buildServerBinary(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "tau-tool"+exeSuffix())
	cmd := exec.Command("go", "build", "-o", bin, "tau-tool/cmd/server")
	cmd.Dir = repoRoot(t)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build: %v", err)
	}
	return bin
}

func exeSuffix() string {
	if os.PathSeparator == '\\' {
		return ".exe"
	}
	return ""
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod from %s", dir)
		}
		dir = parent
	}
}
