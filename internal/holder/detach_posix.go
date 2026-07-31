//go:build !windows

package holder

import (
	"os/exec"
	"syscall"
)

// detachProcess puts the holder into its own session so it survives the
// TUI's terminal closing.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
