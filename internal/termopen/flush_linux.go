//go:build linux

package termopen

import (
	"os"

	"golang.org/x/sys/unix"
)

// FlushInput discards the input typed on f's terminal and not yet read: a key pressed while reach was
// installing is not an answer to a later prompt (ADR-0019). Best effort: a pipe is left as it is.
func FlushInput(f *os.File) {
	_ = unix.IoctlSetInt(int(f.Fd()), unix.TCFLSH, unix.TCIFLUSH)
}
