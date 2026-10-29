//go:build !windows

package packs

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// finalPath returns the symlink-free absolute path of the open file and its
// hard-link count (1 for a directory). Any EvalSymlinks error, or a resolved
// path that is not the open file, is an error.
func finalPath(f *os.File, p string) (string, int, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", 0, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", 0, err
	}
	fi, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	ri, err := os.Stat(real)
	if err != nil {
		return "", 0, err
	}
	if !os.SameFile(fi, ri) {
		return "", 0, errors.New("the path changed while it was read")
	}
	links := 1
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && !fi.IsDir() {
		links = int(st.Nlink)
	}
	return real, links, nil
}
