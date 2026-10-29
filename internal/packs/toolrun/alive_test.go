package toolrun

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// alive reports whether pid still runs.
func alive(pid int) bool {
	if runtime.GOOS == "windows" {
		out, _ := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH").Output()
		return strings.Contains(string(out), " "+strconv.Itoa(pid)+" ")
	}
	// A killed process waiting to be reaped (zombie) counts as dead.
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
