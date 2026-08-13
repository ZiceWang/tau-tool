package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxTimeoutMs      = 2147483647
	maxTimeoutSeconds = maxTimeoutMs / 1000
	// Grace period after the child exits while descendant processes may still
	// be writing to inherited stdout/stderr pipes.
	exitStdioGraceMs = 100
)

func resolveTimeoutMs(timeout *int) (int, error) {
	if timeout == nil {
		return 0, nil
	}
	if *timeout <= 0 {
		return 0, fmt.Errorf("Invalid timeout: must be a finite number of seconds")
	}
	timeoutMs := *timeout * 1000
	if timeoutMs > maxTimeoutMs {
		return 0, fmt.Errorf("Invalid timeout: maximum is %d seconds", maxTimeoutSeconds)
	}
	return timeoutMs, nil
}

// BashInput matches pi's bash schema: command, timeout (seconds).
type BashInput struct {
	Command string `json:"command" jsonschema:"Bash command to execute"`
	Timeout *int   `json:"timeout,omitempty" jsonschema:"Timeout in seconds (optional, no default timeout)"`
}

// BashToolDescription is pi's exact bash tool description.
const BashToolDescription = "Execute a bash command in the current working directory. Returns stdout and stderr. Output is truncated to last 2000 lines or 50KB (whichever is hit first). If truncated, full output is saved to a temp file. Optionally provide a timeout in seconds."

// BashToolPromptSnippet and guidelines mirror pi's system prompt contributions.
const (
	BashToolPromptSnippet = "Execute bash commands (ls, grep, find, etc.)"
	BashToolGuideline     = "You can inspect PI_* environment variables for current model and session details."
)

type bashDeps struct {
	shellPath   string
	commandPrefix string
	settings    *SettingsStore
}

// CreateBashTool returns the bash tool handler.
func CreateBashTool(options ...BashToolOptions) mcp.ToolHandlerFor[BashInput, any] {
	deps := bashDeps{}
	if len(options) > 0 {
		deps.shellPath = options[0].ShellPath
		deps.commandPrefix = options[0].CommandPrefix
		deps.settings = options[0].Settings
	}
	return deps.handle
}

// BashToolOptions mirrors pi's BashToolOptions.
type BashToolOptions struct {
	CommandPrefix string
	ShellPath     string
	Settings      *SettingsStore
}

// ResolveShell picks the shell executable and its args, following pi's
// resolution order. The bool reports whether the command should be transported
// via stdin (legacy WSL bash) instead of argv.
func ResolveShell(customShellPath string) (shell string, args []string, commandFromStdin bool, err error) {
	return resolveShell(customShellPath)
}

// resolveShell is the internal implementation of ResolveShell.
func resolveShell(customShellPath string) (shell string, args []string, commandFromStdin bool, err error) {
	if customShellPath != "" {
		if pathExists(customShellPath) {
			shell, args, stdin := bashShellConfig(customShellPath)
			return shell, args, stdin, nil
		}
		// Allow bare command names (e.g. "pwsh", "nu", "cmd.exe") by resolving
		// them against PATH.
		if resolved, err := exec.LookPath(customShellPath); err == nil {
			shell, args, stdin := bashShellConfig(resolved)
			return shell, args, stdin, nil
		}
		return "", nil, false, fmt.Errorf("Custom shell path not found: %s", customShellPath)
	}

	if os.PathSeparator == '\\' {
		// Windows: try Git Bash in known locations, then bash on PATH.
		var candidates []string
		if pf := os.Getenv("ProgramFiles"); pf != "" {
			candidates = append(candidates, filepathJoin(pf, "Git", "bin", "bash.exe"))
		}
		if pfx := os.Getenv("ProgramFiles(x86)"); pfx != "" {
			candidates = append(candidates, filepathJoin(pfx, "Git", "bin", "bash.exe"))
		}
		for _, p := range candidates {
			if pathExists(p) {
				shell, args, stdin := bashShellConfig(p)
				return shell, args, stdin, nil
			}
		}
		if bash := findBashOnPath(); bash != "" {
			shell, args, stdin := bashShellConfig(bash)
			return shell, args, stdin, nil
		}
		list := strings.Join(candidates, "\n")
		return "", nil, false, fmt.Errorf("No bash shell found. Options:\n  1. Install Git for Windows: https://git-scm.com/download/win\n  2. Add your bash to PATH (Cygwin, MSYS2, etc.)\n  3. Set shellPath in settings.json\n\nSearched Git Bash in:\n%s", list)
	}

	// Unix: /bin/bash, bash on PATH, then sh.
	if pathExists("/bin/bash") {
		shell, args, stdin := bashShellConfig("/bin/bash")
		return shell, args, stdin, nil
	}
	if bash := findBashOnPath(); bash != "" {
		shell, args, stdin := bashShellConfig(bash)
		return shell, args, stdin, nil
	}
	return "sh", []string{"-c"}, false, nil
}

var legacyWslBashRe = regexp.MustCompile(`^[a-z]:\\windows\\(?:system32|sysnative)\\bash\.exe$`)

// bashShellConfig returns the shell args; legacy WSL bash uses stdin transport
// and cmd.exe uses /c instead of -c.
func bashShellConfig(shell string) (string, []string, bool) {
	base := strings.ToLower(filepath.Base(shell))
	if base == "cmd" || base == "cmd.exe" {
		return shell, []string{"/c"}, false
	}
	normalized := strings.ReplaceAll(shell, "/", "\\")
	lower := strings.ToLower(normalized)
	if legacyWslBashRe.MatchString(lower) {
		return shell, []string{"-s"}, true // stdin transport
	}
	return shell, []string{"-c"}, false
}

// findBashOnPath locates bash via PATH lookup.
func findBashOnPath() string {
	if os.PathSeparator == '\\' {
		// Windows: use where and verify the file exists.
		out, err := exec.Command("where", "bash.exe").Output()
		if err == nil {
			first := firstLine(string(out))
			if first != "" && pathExists(strings.TrimSpace(first)) {
				return strings.TrimSpace(first)
			}
		}
		return ""
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		return ""
	}
	return bash
}

func filepathJoin(parts ...string) string {
	return strings.Join(parts, string(os.PathSeparator))
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

func (d bashDeps) handle(ctx context.Context, _ *mcp.CallToolRequest, in BashInput) (*mcp.CallToolResult, any, error) {
	timeoutMs, err := resolveTimeoutMs(in.Timeout)
	if err != nil {
		return toolError(err.Error()), nil, nil
	}

	// Settings can override the shell, command prefix, and output encoding;
	// explicit options passed at server construction take precedence over
	// settings.
	shellPath := d.shellPath
	commandPrefix := d.commandPrefix
	var outputEncoding string
	if d.settings != nil {
		settingsShell, settingsPrefix, settingsEncoding := d.settings.EffectiveShell()
		if shellPath == "" && settingsShell != "" {
			shellPath = settingsShell
		}
		if commandPrefix == "" && settingsPrefix != "" {
			commandPrefix = settingsPrefix
		}
		outputEncoding = settingsEncoding
	}

	shell, shellArgs, commandFromStdin, err := resolveShell(shellPath)
	if err != nil {
		return toolError(err.Error()), nil, nil
	}

	workDir := workDir()
	if !pathExists(workDir) {
		return toolError(fmt.Sprintf("Working directory does not exist: %s\nCannot execute bash commands.", workDir)), nil, nil
	}

	command := in.Command
	if commandPrefix != "" {
		command = commandPrefix + "\n" + command
	}

	acc := NewOutputAccumulator(OutputAccumulatorOptions{TempFilePrefix: "pi-bash"})

	cmd := exec.Command(shell, shellArgs...)
	if commandFromStdin {
		cmd.Stdin = strings.NewReader(command)
	} else {
		cmd.Args = append(cmd.Args, command)
	}
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	setProcAttr(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return toolError(err.Error()), nil, nil
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return toolError(err.Error()), nil, nil
	}

	// Decode shell output with the configured encoding so non-UTF-8 shells
	// (e.g. PowerShell/cmd on a GBK system) do not garble text.
	stdoutReader, err := decodedReader(stdout, outputEncoding)
	if err != nil {
		return toolError(err.Error()), nil, nil
	}
	stderrReader, err := decodedReader(stderr, outputEncoding)
	if err != nil {
		return toolError(err.Error()), nil, nil
	}

	if err := cmd.Start(); err != nil {
		return toolError(err.Error()), nil, nil
	}
	childPID := cmd.Process.Pid

	// Stream stdout and stderr into the accumulator until EOF, tracking pipe
	// completion. We must NOT call cmd.Wait() here: on Windows it closes the
	// pipe read ends, dropping output that the readers have not consumed yet.
	// Instead we reap with cmd.Process.Wait(), which leaves the pipes intact so
	// the readers reach a natural EOF.
	donePipes := make(chan struct{}, 2)
	pipeDone := func(r io.Reader) {
		defer func() { donePipes <- struct{}{} }()
		buf := make([]byte, 32*1024)
		for {
			n, readErr := r.Read(buf)
			if n > 0 {
				_ = acc.Append(buf[:n])
			}
			if readErr != nil {
				return
			}
		}
	}
	go pipeDone(stdoutReader)
	go pipeDone(stderrReader)

	// Reap the process and get its exit code.
	waitCh := make(chan error, 1)
	var processState *os.ProcessState
	go func() {
		state, err := cmd.Process.Wait()
		processState = state
		waitCh <- err
	}()

	var timeoutCh <-chan time.Time
	if timeoutMs > 0 {
		timer := time.NewTimer(time.Duration(timeoutMs) * time.Millisecond)
		defer timer.Stop()
		timeoutCh = timer.C
	}

	var aborted, timedOut bool
	select {
	case <-waitCh:
	case <-ctx.Done():
		aborted = true
		killProcessTree(childPID)
		<-waitCh
	case <-timeoutCh:
		timedOut = true
		killProcessTree(childPID)
		<-waitCh
	}

	exitCode := -1
	if processState != nil {
		exitCode = processState.ExitCode()
	}
	_ = cmd.Process.Release()

	// Give descendant processes a grace window to finish writing output.
	waitForPipeGrace(donePipes, 2, exitStdioGraceMs)
	lastLineBytes := acc.GetLastLineBytes()
	_ = acc.Finish()
	snapshot, _ := acc.Snapshot(true)
	_ = acc.CloseTempFile()

	text, details := formatBashOutput(snapshot, lastLineBytes)

	if aborted {
		return toolError(appendStatus(text, "Command aborted")), nil, nil
	}
	if timedOut {
		return toolError(appendStatus(text, fmt.Sprintf("Command timed out after %d seconds", *in.Timeout))), nil, nil
	}
	if exitCode != 0 {
		return toolError(appendStatus(text, fmt.Sprintf("Command exited with code %d", exitCode))), nil, nil
	}

	result := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
	if details.FullOutputPath != "" {
		result.StructuredContent = map[string]any{
			"fullOutputPath": details.FullOutputPath,
			"truncated":      details.Truncation.Truncated,
		}
	}
	return result, nil, nil
}

// waitForPipeGrace waits for all pipes to finish, or until the idle grace
// elapses with no new output.
func waitForPipeGrace(donePipes <-chan struct{}, remaining int, graceMs int) {
	deadline := time.Now().Add(time.Duration(graceMs) * time.Millisecond)
	for remaining > 0 {
		if time.Now().After(deadline) {
			return
		}
		select {
		case <-donePipes:
			remaining--
		case <-time.After(5 * time.Millisecond):
		}
	}
}

type bashOutputDetails struct {
	Truncation     TruncationResult
	FullOutputPath string
}

func formatBashOutput(snapshot OutputSnapshot, lastLineBytes int) (string, bashOutputDetails) {
	truncation := snapshot.Truncation
	text := snapshot.Content
	if text == "" {
		text = "(no output)"
	}
	details := bashOutputDetails{}
	if truncation.Truncated {
		details.Truncation = truncation
		details.FullOutputPath = snapshot.FullOutputPath
		startLine := truncation.TotalLines - truncation.OutputLines + 1
		endLine := truncation.TotalLines
		if truncation.LastLinePartial {
			lastLineSize := FormatSize(lastLineBytes)
			text += fmt.Sprintf("\n\n[Showing last %s of line %d (line is %s). Full output: %s]",
				FormatSize(truncation.OutputBytes), endLine, lastLineSize, snapshot.FullOutputPath)
		} else if truncation.TruncatedBy == "lines" {
			text += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Full output: %s]",
				startLine, endLine, truncation.TotalLines, snapshot.FullOutputPath)
		} else {
			text += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Full output: %s]",
				startLine, endLine, truncation.TotalLines, FormatSize(DEFAULT_MAX_BYTES), snapshot.FullOutputPath)
		}
	}
	return text, details
}

func appendStatus(text, status string) string {
	if text == "" {
		return status
	}
	return text + "\n\n" + status
}

