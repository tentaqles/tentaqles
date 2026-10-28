//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// prepareChild puts the hub in its own process group, so the force stop reaches everything it
// started (its runs' agents) and not this app.
func prepareChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// procTree is the hub's process group.
type procTree struct{ pgid int }

func attachTree(cmd *exec.Cmd) *procTree { return &procTree{pgid: cmd.Process.Pid} }

// interruptProcess asks the hub to shut down the way Ctrl-C would: it drains HTTP, stops its
// runners and removes its serve record. (Closing its stdin asks the same.)
func interruptProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
	}
}

// killTree force-ends the whole group.
func (t *procTree) killTree() { _ = syscall.Kill(-t.pgid, syscall.SIGKILL) }

// release runs once the hub has exited: anything left in its group goes too.
func (t *procTree) release() { _ = syscall.Kill(-t.pgid, syscall.SIGKILL) }
