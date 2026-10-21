//go:build !windows

package adapter

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// prepare puts the child in its own process group so the whole tree can be signalled.
func prepare(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// terminate sends SIGTERM to the process group now and SIGKILL after grace, unless done
// (closed once the child was reaped) comes first.
func terminate(p *os.Process, grace time.Duration, done <-chan struct{}) error {
	if p == nil {
		return nil
	}
	_ = syscall.Kill(-p.Pid, syscall.SIGTERM)
	go func() {
		t := time.NewTimer(grace)
		defer t.Stop()
		select {
		case <-done:
		case <-t.C:
			_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
			_ = p.Kill()
		}
	}()
	return nil
}
