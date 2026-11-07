//go:build !windows && !linux && !darwin

package termopen

import "os"

// FlushInput does nothing on this OS: the pause may take a key typed during the installs.
func FlushInput(f *os.File) {}
