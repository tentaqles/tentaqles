//go:build windows

package browser

import (
	"os/exec"
	"syscall"
)

func start(path string, args []string) error {
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200} // CREATE_NEW_PROCESS_GROUP
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
