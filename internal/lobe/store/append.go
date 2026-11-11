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
//
// When the file exists and its last byte is not a line break (a crash cut the last line short), a line
// break goes first, in the same write. The torn line is then a line of its own: readers skip it and the
// new line is whole.
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
	f, err := os.OpenFile(full, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	prefix := ""
	if torn(f) {
		prefix = "\n"
	}
	_, werr := f.Write([]byte(prefix + text + "\n"))
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// torn reports whether the file is non-empty and its last byte is not a line break. A file that cannot
// be read is not torn: the line is appended as it is.
func torn(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil || fi.Size() == 0 {
		return false
	}
	var last [1]byte
	if _, err := f.ReadAt(last[:], fi.Size()-1); err != nil {
		return false
	}
	return last[0] != '\n'
}
