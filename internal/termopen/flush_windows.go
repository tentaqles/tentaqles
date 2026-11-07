//go:build windows

package termopen

import (
	"os"
	"syscall"
)

var procFlushConsoleInputBuffer = syscall.NewLazyDLL("kernel32.dll").NewProc("FlushConsoleInputBuffer")

// FlushInput discards the keys typed on f's console before this call: a key pressed while reach was
// installing is not an answer to a later prompt (ADR-0019). Best effort: a pipe is left as it is.
func FlushInput(f *os.File) {
	procFlushConsoleInputBuffer.Call(f.Fd())
}
