package eventlog

import (
	"context"
	"errors"
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
