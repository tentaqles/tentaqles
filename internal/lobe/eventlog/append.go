package eventlog

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

// LockWait is how long a hook waits for the store lock before it appends without it.
const LockWait = 50 * time.Millisecond

// FileFor is the store-relative day file of an event time: events/events-YYYYMMDD.jsonl, named by
// the UTC date of at (RFC 3339).
func FileFor(at string) (string, error) {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return "", err
	}
	return "events/events-" + t.UTC().Format("20060102") + ".jsonl", nil
}

// Append writes e as one line of its day file. It waits up to LockWait for the store lock and then
// appends with or without it: a lost event costs more than a line a reader may have to skip.
func Append(ctx context.Context, s *store.Store, e Event) error {
	file, err := FileFor(e.At)
	if err != nil {
		return err
	}
	line, err := scrub.Line(e)
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
	err = s.Append(file, line)
	if unlock != nil {
		unlock()
	}
	return err
}

// buildRefLine builds the reference line of a repeat. It is a variable so a test can make it fail; the
// real builder cannot fail on an Event (its strings always marshal).
var buildRefLine = ReferenceLine

// AppendChecked is Append with the repeat check of S-06 (steps 2 to 6). It waits up to LockWait for
// the store lock. With the lock, it reads the tail of the event's day file and decides under the same
// hold: a replay writes nothing; a repeat writes the reference line Reference(e, firstID); a new event
// writes the full line. Without the lock (another process holds it) the check is skipped and the full
// line is appended, as Append does. So is the check when e has no session or the tail cannot be read.
// When the reference line cannot be built, the full line is appended instead: a lost event costs more
// than a duplicate.
//
// It returns the verdict (VerdictNew when the check was skipped, the line is new, or the full line was
// written after a failed reference), firstID (the id of the first line, for VerdictRepeat only) and the
// error of the write, if any.
func AppendChecked(ctx context.Context, s *store.Store, e Event) (Verdict, string, error) {
	file, err := FileFor(e.At)
	if err != nil {
		return VerdictNew, "", err
	}
	line, err := scrub.Line(e)
	if err != nil {
		return VerdictNew, "", err
	}
	lctx, cancel := context.WithTimeout(ctx, LockWait)
	unlock, err := s.Lock(lctx)
	cancel()
	switch {
	case err == nil:
	case errors.Is(err, store.ErrBusy):
		return VerdictNew, "", s.Append(file, line)
	default:
		return VerdictNew, "", err
	}
	defer unlock()

	v, first := checkTail(s, file, e)
	switch v {
	case VerdictReplay:
		return v, "", nil
	case VerdictRepeat:
		ref, err := buildRefLine(Reference(e, first))
		if err != nil {
			// A lost event costs more than a duplicate: the full line is written instead.
			return VerdictNew, "", s.Append(file, line)
		}
		return v, first, s.Append(file, ref)
	}
	return VerdictNew, "", s.Append(file, line)
}

// checkTail is the S-06 check of e against the tail of the store-relative day file. A missing session
// or a tail that cannot be read gives VerdictNew: the check is skipped and the full line is written.
// It must run under the store lock.
func checkTail(s *store.Store, file string, e Event) (Verdict, string) {
	if e.Session == "" {
		return VerdictNew, ""
	}
	tail, err := TailEvents(filepath.Join(s.Dir(), filepath.FromSlash(file)), EventTailBytes)
	if err != nil {
		return VerdictNew, ""
	}
	return Check(tail, e)
}
