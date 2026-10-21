//go:build !windows

package adapter

import (
	"os"
	"syscall"
	"time"
)

// terminate sends SIGTERM now and SIGKILL after grace.
func terminate(p *os.Process, grace time.Duration) error {
	if p == nil {
		return nil
	}
	_ = p.Signal(syscall.SIGTERM)
	go func() {
		time.Sleep(grace)
		_ = p.Kill()
	}()
	return nil
}
