//go:build !windows

package install

import "syscall"

// processAlive reports whether the process is still running.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
