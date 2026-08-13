//go:build windows

package tools

import (
	"os/exec"
	"syscall"
)

// setProcAttr configures the child process: hide its window on Windows.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// killProcessTree kills a process and all its children via taskkill.
func killProcessTree(pid int) {
	exec.Command("taskkill", "/F", "/T", "/PID", itoa(pid)).Run()
}
