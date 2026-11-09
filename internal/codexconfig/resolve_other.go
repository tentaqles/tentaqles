//go:build !windows

package codexconfig

import "path/filepath"

// resolveFinal returns the path of an existing file with its symlinks resolved.
func resolveFinal(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
