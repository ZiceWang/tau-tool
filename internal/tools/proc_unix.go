//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

// setProcAttr configures the child process: start it in its own process group
// so the whole tree can be killed together.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessTree kills a process and all its children by sending SIGKILL to
// the process group.
func killProcessTree(pid int) {
	// Kill the process group; fall back to killing just the child.
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
