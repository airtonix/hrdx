//go:build !windows

package plugin

import (
	"os/exec"
	"syscall"
)

func startProcess(command *exec.Cmd) (func(), error) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return nil, err
	}
	return func() { _ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }, nil
}
