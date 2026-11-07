//go:build !windows

package install

import (
	"errors"
	"os/exec"
	"syscall"
)

// tree stops a process and its descendants. On Unix the child leads its own process group and
// the group is killed.
type tree struct{}

func prepareTree(c *exec.Cmd) *tree {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	return &tree{}
}

func (t *tree) attach(c *exec.Cmd) {}

func (t *tree) release() {}
