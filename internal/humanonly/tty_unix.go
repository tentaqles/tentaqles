//go:build !windows

package humanonly

import (
	"io"
	"os"

	"github.com/charmbracelet/x/term"
)

// isTerminal reports whether f is an interactive terminal.
func isTerminal(f *os.File) bool { return f != nil && term.IsTerminal(f.Fd()) }

// openTTY opens the controlling terminal, whatever stdin is.
func openTTY() (io.Reader, func(), error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
}
