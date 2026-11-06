//go:build windows

package humanonly

import (
	"encoding/binary"
	"errors"
	"io"
	"os"

	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/windows"
)

// isTerminal reports a console handle, or a Cygwin / MSYS2 pty: mintty (Git
// Bash, MSYS2) gives its shells named pipes instead of a console, so the
// pipe's name decides (cygwinPtyName).
func isTerminal(f *os.File) bool {
	return f != nil && (term.IsTerminal(f.Fd()) || cygwinPty(f))
}

// cygwinPty reports whether f is one end of a Cygwin or MSYS2 pty.
func cygwinPty(f *os.File) bool {
	name, ok := pipeName(f)
	return ok && cygwinPtyName(name)
}

// pipeName is the name of the pipe behind f (GetFileInformationByHandleEx,
// FileNameInfo); ok is false for anything that is not a pipe.
func pipeName(f *os.File) (string, bool) {
	if f == nil {
		return "", false
	}
	h := windows.Handle(f.Fd())
	ft, err := windows.GetFileType(h)
	if err != nil || ft != windows.FILE_TYPE_PIPE {
		return "", false
	}
	// FILE_NAME_INFO: a uint32 byte length, then the UTF-16 name.
	var buf [2048]byte
	if err := windows.GetFileInformationByHandleEx(h, windows.FileNameInfo, &buf[0], uint32(len(buf))); err != nil {
		return "", false
	}
	n := int(binary.LittleEndian.Uint32(buf[:4]))
	if n < 0 || n > len(buf)-4 {
		n = len(buf) - 4
	}
	u16 := make([]uint16, n/2)
	for i := range u16 {
		u16[i] = binary.LittleEndian.Uint16(buf[4+2*i:])
	}
	return windows.UTF16ToString(u16), true
}

// openTTY opens the console input, whatever stdin is; Go reads a console
// handle through ReadConsole, so the typed line arrives as UTF-8. With no
// console (mintty) the Cygwin pty on stdin is the terminal, and is read
// without being closed. A plain pipe or file on stdin is no terminal.
func openTTY() (io.Reader, func(), error) {
	if f, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		return f, func() { _ = f.Close() }, nil
	}
	if cygwinPty(os.Stdin) {
		return os.Stdin, func() {}, nil
	}
	return nil, nil, errors.New("no console and no Cygwin pty on stdin")
}
