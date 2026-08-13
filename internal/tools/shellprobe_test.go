package tools

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestBashToolWorksWithCommonShells verifies the bash tool can drive shells
// found on PATH (pwsh, nu) via the shellPath override, without hardcoding
// absolute paths.
func TestBashToolWorksWithCommonShells(t *testing.T) {
	shells := []string{"pwsh", "nu", "bash"}
	for _, name := range shells {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Logf("%s not on PATH, skipping", name)
			continue
		}
		deps := bashDeps{shellPath: path}
		result, _, err := deps.handle(context.Background(), &mcp.CallToolRequest{}, BashInput{Command: "echo probe-ok"})
		if err != nil {
			t.Errorf("%s: err %v", name, err)
			continue
		}
		if result.IsError || !strings.Contains(textOf(result), "probe-ok") {
			t.Errorf("%s: IsError=%v output=%q", name, result.IsError, textOf(result))
		}
	}
}
