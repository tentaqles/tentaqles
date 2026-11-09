package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
)

// Append adds line and a newline to the store-relative file rel with one write. It does not take the
// lock; the caller decides. The parent folder is created when missing, one level only, so a removed
// store is never recreated.
func (s *Store) Append(rel string, line scrub.Clean) error {
	full, err := s.resolve(rel)
	if err != nil {
		return err
	}
	text := line.String()
	if strings.Contains(text, "\n") {
		return errLineBreak
	}
	if err := os.Mkdir(filepath.Dir(full), 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	f, err := os.OpenFile(full, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, werr := f.Write([]byte(text + "\n"))
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}
