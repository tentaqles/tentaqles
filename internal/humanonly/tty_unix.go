//go:build !windows

package humanonly

import "os"

// openTTY opens the controlling terminal, whatever stdin is.
func openTTY() (*os.File, error) { return os.OpenFile("/dev/tty", os.O_RDWR, 0) }
