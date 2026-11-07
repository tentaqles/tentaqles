//go:build !windows

package install

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

// processAlive reports whether the process is still running. A zombie (killed, not yet reaped
// by an init that is slow to do it) is gone: on Linux its state in /proc says so.
func processAlive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	if raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		if i := strings.LastIndexByte(string(raw), ')'); i >= 0 && len(raw) > i+2 && raw[i+2] == 'Z' {
			return false
		}
	}
	return true
}
