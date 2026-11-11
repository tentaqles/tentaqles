package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Remove deletes the store-relative file rel. It refuses a path that leaves the store (checkRel), a parent
// folder that is a link or not a folder (ErrLinked), and a file that is a link or not a regular file
// (ErrLinked). A file that does not exist is not an error. It does not take the lock; the caller decides.
func (s *Store) Remove(rel string) error {
	full, err := s.resolve(rel)
	if err != nil {
		return err
	}
	if err := rejectLink(filepath.Dir(full)); err != nil {
		return err
	}
	fi, err := os.Lstat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return ErrLinked
	}
	return os.Remove(full)
}
