//go:build windows

package termopen

import "syscall"

// killProcess ends pid with exit code 0. Windows Terminal closes a tab whose shell exits cleanly, and
// keeps it on "[process exited with code 1]" after a forced exit (ADR-0019).
func killProcess(pid int) error {
	h, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	return syscall.TerminateProcess(h, 0)
}
