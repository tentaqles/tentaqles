//go:build windows

package adapter

import (
	"os"
	"os/exec"
	"strconv"
	"time"
)

// terminate asks the process tree to close (taskkill /T), then forces it after grace (taskkill /T /F):
// claude on Windows is a .cmd shim with a node child, so the tree must go, not only the shim.
func terminate(p *os.Process, grace time.Duration) error {
	if p == nil {
		return nil
	}
	pid := strconv.Itoa(p.Pid)
	_ = exec.Command("taskkill", "/PID", pid, "/T").Run()
	go func() {
		time.Sleep(grace)
		_ = exec.Command("taskkill", "/PID", pid, "/T", "/F").Run()
		_ = p.Kill()
	}()
	return nil
}
