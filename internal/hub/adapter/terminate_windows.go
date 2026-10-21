//go:build windows

package adapter

import (
	"os"
	"os/exec"
	"strconv"
	"time"
)

func prepare(*exec.Cmd) {}

// terminate asks the process tree to close (taskkill /T), then forces it after grace (taskkill /T /F)
// unless done (closed once the child was reaped) comes first: claude on Windows is a .cmd shim with a
// node child, so the tree must go, not only the shim, and an exited PID must never be targeted.
func terminate(p *os.Process, grace time.Duration, done <-chan struct{}) error {
	if p == nil {
		return nil
	}
	pid := strconv.Itoa(p.Pid)
	_ = exec.Command("taskkill", "/PID", pid, "/T").Run()
	go func() {
		t := time.NewTimer(grace)
		defer t.Stop()
		select {
		case <-done:
		case <-t.C:
			_ = exec.Command("taskkill", "/PID", pid, "/T", "/F").Run()
			_ = p.Kill()
		}
	}()
	return nil
}
