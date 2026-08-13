package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func shellForTest(t *testing.T) string {
	// Prefer an explicit override.
	if p := os.Getenv("TAU_BASH_PATH"); p != "" && pathExists(p) {
		return p
	}
	// First try the same resolution the tool uses at runtime.
	if shell, args, stdin, err := resolveShell(""); err == nil && bashWorks(shell, args, stdin) {
		return ""
	}
	// On Windows the default resolution may land on WSL's launcher
	// (system32\bash.exe), which only works when WSL is installed. Probe
	// common real bash installations before giving up.
	if os.PathSeparator == '\\' {
		var candidates []string
		if pf := os.Getenv("ProgramFiles"); pf != "" {
			candidates = append(candidates, filepathJoin(pf, "Git", "bin", "bash.exe"))
		}
		if pfx := os.Getenv("ProgramFiles(x86)"); pfx != "" {
			candidates = append(candidates, filepathJoin(pfx, "Git", "bin", "bash.exe"))
		}
		for _, p := range candidates {
			if pathExists(p) && bashWorks(p, []string{"-c"}, false) {
				return p
			}
		}
	}
	t.Skip("no usable bash shell available for tests")
	return ""
}

// bashWorks verifies a shell can actually run a command using the same
// invocation shape the tool uses (argv or stdin transport). A WSL launcher
// with no WSL distro installed fails this probe.
func bashWorks(shell string, args []string, commandFromStdin bool) bool {
	cmd := exec.Command(shell, args...)
	if commandFromStdin {
		cmd.Stdin = strings.NewReader("echo ok")
	} else {
		cmd.Args = append(cmd.Args, "echo ok")
	}
	out, err := cmd.CombinedOutput()
	return err == nil && strings.Contains(string(out), "ok")
}

func bashHandler(t *testing.T) bashDeps {
	deps := bashDeps{}
	if shell := shellForTest(t); shell != "" {
		deps.shellPath = shell
	}
	return deps
}

func TestBashEcho(t *testing.T) {
	deps := bashHandler(t)
	result, _, err := deps.handle(context.Background(), &mcp.CallToolRequest{}, BashInput{Command: "echo hello"})
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}
	if got := textOf(result); !strings.Contains(got, "hello") {
		t.Errorf("output = %q", got)
	}
}

func TestBashStdoutAndStderr(t *testing.T) {
	deps := bashHandler(t)
	result, _, err := deps.handle(context.Background(), &mcp.CallToolRequest{}, BashInput{Command: "echo out; echo err >&2"})
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}
	text := textOf(result)
	if !strings.Contains(text, "out") || !strings.Contains(text, "err") {
		t.Errorf("output = %q", text)
	}
}

func TestBashNonZeroExit(t *testing.T) {
	deps := bashHandler(t)
	result, _, err := deps.handle(context.Background(), &mcp.CallToolRequest{}, BashInput{Command: "exit 3"})
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result")
	}
	if !strings.Contains(textOf(result), "Command exited with code 3") {
		t.Errorf("error = %q", textOf(result))
	}
}

func TestBashTimeout(t *testing.T) {
	deps := bashHandler(t)
	timeout := 1
	result, _, err := deps.handle(context.Background(), &mcp.CallToolRequest{}, BashInput{
		Command: "sleep 30",
		Timeout: &timeout,
	})
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected timeout error")
	}
	if !strings.Contains(textOf(result), "Command timed out after 1 seconds") {
		t.Errorf("error = %q", textOf(result))
	}
}

func TestBashInvalidTimeout(t *testing.T) {
	deps := bashHandler(t)
	timeout := -5
	result, _, err := deps.handle(context.Background(), &mcp.CallToolRequest{}, BashInput{
		Command: "echo x",
		Timeout: &timeout,
	})
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result")
	}
	if !strings.Contains(textOf(result), "Invalid timeout") {
		t.Errorf("error = %q", textOf(result))
	}
}

func TestBashContextCancellation(t *testing.T) {
	deps := bashHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	go cancel()
	result, _, err := deps.handle(ctx, &mcp.CallToolRequest{}, BashInput{Command: "sleep 10"})
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected abort error")
	}
	if !strings.Contains(textOf(result), "Command aborted") {
		t.Errorf("error = %q", textOf(result))
	}
}

func TestBashWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cwd.txt")
	_ = os.WriteFile(path, []byte("in-cwd"), 0o644)
	t.Chdir(dir)

	deps := bashDeps{}
	if shell := shellForTest(t); shell != "" {
		deps.shellPath = shell
	}
	result, _, err := deps.handle(context.Background(), &mcp.CallToolRequest{}, BashInput{Command: "cat cwd.txt"})
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}
	if got := textOf(result); !strings.Contains(got, "in-cwd") {
		t.Errorf("output = %q", got)
	}
}

func TestBashUsesEnvCwd(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "env.txt"), []byte("via-env"), 0o644)
	t.Setenv(EnvCwd, dir)

	deps := bashDeps{}
	if shell := shellForTest(t); shell != "" {
		deps.shellPath = shell
	}
	result, _, err := deps.handle(context.Background(), &mcp.CallToolRequest{}, BashInput{Command: "cat env.txt"})
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", textOf(result))
	}
	if got := textOf(result); !strings.Contains(got, "via-env") {
		t.Errorf("output = %q", got)
	}
}

func TestBashMissingCwd(t *testing.T) {
	t.Setenv(EnvCwd, filepath.Join(t.TempDir(), "does-not-exist"))
	deps := bashDeps{}
	result, _, err := deps.handle(context.Background(), &mcp.CallToolRequest{}, BashInput{Command: "echo x"})
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error")
	}
	if !strings.Contains(textOf(result), "Working directory does not exist") {
		t.Errorf("error = %q", textOf(result))
	}
}
