//go:build !windows

package termopen

import "os"

// killProcess ends pid. A terminal emulator closes its window when the shell goes away.
func killProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
