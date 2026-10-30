//go:build !windows

package adapter

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// processRunning reports whether pid can still be signalled. A killed
// process waiting to be reaped (a zombie, Linux) counts as exited.
func processRunning(pid int) bool {
	if raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		if i := strings.LastIndexByte(string(raw), ')'); i > 0 && i+2 < len(raw) && raw[i+2] == 'Z' {
			return false
		}
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
