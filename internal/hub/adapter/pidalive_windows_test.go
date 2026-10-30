//go:build windows

package adapter

import "syscall"

const (
	synchronize                    = 0x00100000
	processQueryLimitedInformation = 0x1000
	waitTimeout                    = 0x00000102
)

// processRunning reports whether pid is a process that has not exited. A
// process that exited stays openable while anyone (taskkill, the exec
// package, an antivirus) still holds a handle to it, so a successful
// OpenProcess alone is not "alive": the process object must also be
// unsignalled.
func processRunning(pid int) bool {
	h, err := syscall.OpenProcess(synchronize|processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	ev, err := syscall.WaitForSingleObject(h, 0)
	return err == nil && ev == waitTimeout
}
