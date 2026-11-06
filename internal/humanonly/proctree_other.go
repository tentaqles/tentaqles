//go:build !windows && !linux && !darwin

package humanonly

// processTreeMethod is empty: this OS has no walker. The walk cannot
// establish a human ancestry here, so every human-only command refuses, and
// tq doctor says so.
const processTreeMethod = ""

// parentOf is stubbed: no parent can be read.
func parentOf(int) (Parent, bool) { return Parent{}, false }
