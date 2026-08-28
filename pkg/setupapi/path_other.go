//go:build !windows

package setupapi

// setUserPathOS is a no-op on non-Windows platforms: there's no single
// registry-backed user PATH to edit. Callers should print a PATH hint
// themselves (e.g. "add ~/.local/bin to your PATH").
func setUserPathOS(dir string) error {
	return nil
}
