//go:build !windows

package packs

import (
	"os"
	"syscall"
)

// openNoBlock opens for reading without blocking on a FIFO.
func openNoBlock(p string) (*os.File, error) {
	return os.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
