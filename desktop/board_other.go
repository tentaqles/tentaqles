//go:build !windows

package main

import (
	"os"
	"os/exec"
)

func hideConsole(*exec.Cmd) {}

// interruptProcess asks the hub to shut down the way Ctrl-C would: it drains HTTP, stops its
// runners and removes its serve record.
func interruptProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
	}
}
