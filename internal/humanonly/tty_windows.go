//go:build windows

package humanonly

import "os"

// openTTY opens the console input, whatever stdin is. Go reads a console
// handle through ReadConsole, so the typed line arrives as UTF-8.
func openTTY() (*os.File, error) { return os.OpenFile("CONIN$", os.O_RDWR, 0) }
