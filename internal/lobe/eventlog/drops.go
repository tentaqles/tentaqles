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

// The drop reasons. Each is the reason of one event that was not written, and a drops line carries it.
const (
	// DropNoExit is the reason of a shell call whose output had no exit code the lobe could find.
	DropNoExit = "no-exit"
	// DropFull is the reason of an event refused by a day file at its fill level (8 MiB for ok, 16 MiB
	// for every event).
	DropFull = "full"
	// DropBig is the reason of a line still over the line limit after its cuts.
	DropBig = "big"
	// DropDisk is the reason of an event whose write failed (a disk full, a file that cannot be written).
	DropDisk = "disk"
)

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

// knownDrop reports whether why is one of the four drop reasons.
func knownDrop(why string) bool {
	switch why {
	case DropNoExit, DropFull, DropBig, DropDisk:
		return true
	}
	return false
}

// buildDrop is the drops line of one reason at a time, scrubbed as an event line is.
func buildDrop(why string, at time.Time) (scrub.Clean, error) {
	return scrub.Line(dropLine{At: at.UTC().Format(time.RFC3339), Why: why})
}

// AppendDrop writes one line, {"at":"<RFC 3339 UTC time>","why":"<why>"}, to the drops file of at.
// The reason must be one of the four drop reasons; any other is refused with an error and nothing is
// written. When the file is already dropsMax bytes or more, nothing is written and the result is nil.
// The line goes through scrub.Line and the store's whole-line Append, as an event line does.
//
// It waits up to LockWait for the store lock, as Append does, and appends without the lock when another
// process holds it. So the caller need not hold the lock. The size check runs under the same hold as the
// write. Errors are returned; the caller ignores them, since a lost drop costs one count.
func AppendDrop(ctx context.Context, s *store.Store, why string, at time.Time) error {
	if !knownDrop(why) {
		return errUnknownDrop
	}
	line, err := buildDrop(why, at)
	if err != nil {
		return err
	}
	lctx, cancel := context.WithTimeout(ctx, LockWait)
	unlock, err := s.Lock(lctx)
	cancel()
	switch {
	case err == nil:
		defer unlock()
	case errors.Is(err, store.ErrBusy):
	default:
		return err
	}
	return writeDrop(s, line, at)
}

// writeDrop appends a built drop line to the drops file of at, when that file is under dropsMax. It takes
// no lock: the caller holds one, or has none by choice. It is the write AppendDrop and AppendChecked share.
func writeDrop(s *store.Store, line scrub.Clean, at time.Time) error {
	file := DropsFileFor(at)
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

// dropBest writes the drop of why at at, best effort: a drop that cannot be written costs one count, so
// its error is not returned. It takes no lock; see writeDrop.
func dropBest(s *store.Store, why string, at time.Time) {
	line, err := buildDrop(why, at)
	if err != nil {
		return
	}
	_ = writeDrop(s, line, at)
}
