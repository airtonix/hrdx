//go:build windows

package plugin

import (
	"os"

	"golang.org/x/sys/windows"
)

func processAlive(process *os.Process) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(process.Pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if windows.GetExitCodeProcess(handle, &code) != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}
