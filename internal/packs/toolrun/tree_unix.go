//go:build !windows

package toolrun

import (
	"os/exec"
	"syscall"
)

// procTree is the child's process group: killing -pgid reaches every
// descendant that did not leave the group.
type procTree struct{ pid int }

func prepareTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attachTree(cmd *exec.Cmd) *procTree { return &procTree{pid: cmd.Process.Pid} }

func (t *procTree) kill() {
	if t.pid > 0 {
		_ = syscall.Kill(-t.pid, syscall.SIGKILL)
	}
}

func (t *procTree) close() {
	// A grandchild may outlive a parent that exited normally; the group is
	// killed so it cannot hold the pipes or keep running.
	t.kill()
}
