//go:build !windows && !linux && !darwin

package humanonly

// processTreeMethod is empty: this OS has no walker, and tq doctor says so.
const processTreeMethod = ""

// parentOf is stubbed: the process-tree detector never fires here, and the
// TTY and marker detectors carry the check.
func parentOf(int) (int, string, string, bool) { return 0, "", "", false }
