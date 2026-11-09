package store

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
)

// Replace takes the lock and writes data over the store-relative file rel. Nothing is written when
// the lock is busy.
func (s *Store) Replace(ctx context.Context, rel string, data scrub.Clean) error {
	full, err := s.resolve(rel)
	if err != nil {
		return err
	}
	unlock, err := s.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return writeFile(full, data.String())
}

// Update takes the lock, reads rel (a missing file gives fn a nil slice), and replaces the file with
// what fn returns. An error from fn aborts with nothing written.
func (s *Store) Update(ctx context.Context, rel string, fn func(old []byte) (scrub.Clean, error)) error {
	full, err := s.resolve(rel)
	if err != nil {
		return err
	}
	unlock, err := s.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	old, err := os.ReadFile(full)
	if errors.Is(err, fs.ErrNotExist) {
		old = nil
	} else if err != nil {
		return err
	}
	next, err := fn(old)
	if err != nil {
		return err
	}
	return writeFile(full, next.String())
}

// writeFile puts content into full through a temporary file in the same folder, synced and then renamed
// over full. The folder is created when missing, one level only. The caller holds the store lock, or
// the store is being created and has no other writer yet.
func writeFile(full, content string) error {
	dir := filepath.Dir(full)
	if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(full)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		return fail(err)
	}
	if err := renameOver(tmpName, full); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// renameOver renames from over to. A rename that fails because another process has the file open is
// retried: up to 5 times, 10 milliseconds apart.
func renameOver(from, to string) error {
	var err error
	for attempt := 0; attempt <= 5; attempt++ {
		if attempt > 0 {
			time.Sleep(10 * time.Millisecond)
		}
		if err = os.Rename(from, to); err == nil {
			return nil
		}
	}
	return err
}
