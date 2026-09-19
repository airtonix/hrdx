//go:build !windows

package plugin

import (
	"os"
	"syscall"
)

func processAlive(process *os.Process) bool {
	return process.Signal(syscall.Signal(0)) == nil
}
