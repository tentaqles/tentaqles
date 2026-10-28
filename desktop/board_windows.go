//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// createNoWindow keeps the child hub from opening a console window of its own.
const createNoWindow = 0x08000000

func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// interruptProcess: Windows has no signal to send a console-less child, so the stop is a kill.
// The hub's store is SQLite (crash-safe) and a stale serve record is ignored by discovery.
func interruptProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
