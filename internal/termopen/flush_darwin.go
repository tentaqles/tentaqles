//go:build darwin

package termopen

import (
	"os"
	"syscall"
	"unsafe"
)

// FlushInput discards the input typed on f's terminal and not yet read: a key pressed while reach was
// installing is not an answer to a later prompt (ADR-0019). Best effort: a pipe is left as it is.
func FlushInput(f *os.File) {
	const fread = 0x1 // FREAD: the read queue, the one TIOCFLUSH takes
	what := int32(fread)
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCFLUSH), uintptr(unsafe.Pointer(&what)))
}
