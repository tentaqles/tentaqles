package eventlog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

// DropNoExit is the drop reason of a shell call whose output had no exit code the lobe could find.
// It is the only reason in this first form of the drops file; later features add the others.
const DropNoExit = "no-exit"

// dropsMax is the size at which the drops file stops growing: 64 KiB (proposed). At or above it,
// AppendDrop writes nothing.
const dropsMax = 64 << 10

// errUnknownDrop is returned by AppendDrop for a reason that is not a known drop reason. It is a
// string type so the value is a constant and no package-level error is made at start-up.
const errUnknownDrop = dropError("eventlog: unknown drop reason")

// dropError is an error whose value is a constant.
type dropError string

func (e dropError) Error() string { return string(e) }

// dropLine is the record of one drop. Both fields are fixed metadata: no content of a call reaches it.
type dropLine struct {
	At  string `json:"at"`
	Why string `json:"why"`
}

// DropsFileFor is the store-relative drops file of a time: events/drops-YYYYMMDD.jsonl, named by the
// UTC date of at. It sits beside the day files of FileFor.
func DropsFileFor(at time.Time) string {
	return "events/drops-" + at.UTC().Format("20060102") + ".jsonl"
}

// AppendDrop writes one line, {"at":"<RFC 3339 UTC time>","why":"<why>"}, to the drops file of at.
// The reason must be a known one (only DropNoExit for now); any other reason is refused with an error
// and nothing is written. When the file is already dropsMax bytes or more, nothing is written and the
// result is nil. The line goes through scrub.Line and the store's whole-line Append, as an event line
// does.
//
// It waits up to LockWait for the store lock, as Append does, and appends without the lock when another
// process holds it. So the caller need not hold the lock. The size check runs under the same hold as the
// write. Errors are returned; the caller ignores them, since a lost drop costs one count.
func AppendDrop(ctx context.Context, s *store.Store, why string, at time.Time) error {
	if why != DropNoExit {
		return errUnknownDrop
	}
	file := DropsFileFor(at)
	line, err := scrub.Line(dropLine{At: at.UTC().Format(time.RFC3339), Why: why})
	if err != nil {
		return err
	}
	lctx, cancel := context.WithTimeout(ctx, LockWait)
	unlock, err := s.Lock(lctx)
	cancel()
	switch {
	case err == nil:
	case errors.Is(err, store.ErrBusy):
		unlock = nil
	default:
		return err
	}
	if unlock != nil {
		defer unlock()
	}
	fi, err := os.Stat(filepath.Join(s.Dir(), filepath.FromSlash(file)))
	switch {
	case err == nil:
		if fi.Size() >= dropsMax {
			return nil
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return err
	}
	return s.Append(file, line)
}
