//go:build !windows

package toolrun

import (
	"os/exec"
	"syscall"
)

// procTree is the child's process group. It is signalled only on a timeout,
// before tq reaps the leader (so the group id cannot have been reused); a
// normal exit never signals a group.
type procTree struct{ pid int }

func prepareTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attachTree(cmd *exec.Cmd) (*procTree, error) { return &procTree{pid: cmd.Process.Pid}, nil }

func (t *procTree) kill() {
	if t.pid > 0 {
		_ = syscall.Kill(-t.pid, syscall.SIGKILL)
	}
}

func (t *procTree) close() {}
