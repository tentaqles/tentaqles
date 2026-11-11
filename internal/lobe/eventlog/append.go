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

// LockWait is how long a hook waits for the store lock before it appends without it.
const LockWait = 50 * time.Millisecond

// The limits of S-07 (all proposed). lineLimit is the most bytes a line may take with its line break;
// a longer one has its input and error halved, at most cutMax times. A day file at fullOK or more takes
// no ok event; at fullAll or more it takes no event. The fill levels are checked only under the store lock.
const (
	lineLimit = 32 << 10
	cutMax    = 4
	fullOK    = 8 << 20
	fullAll   = 16 << 20
)

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
// appends with or without it: a lost event costs more than a line a reader may have to skip. The limits
// of S-07 apply (see appendEvent). A dropped event is counted in the drops file and Append returns nil;
// a write error is returned.
func Append(ctx context.Context, s *store.Store, e Event) error {
	_, _, err := appendEvent(ctx, s, e, false)
	return err
}

// buildRefLine builds the reference line of a repeat. It is a variable so a test can make it fail; the
// real builder cannot fail on an Event (its strings always marshal).
var buildRefLine = ReferenceLine

// AppendChecked is Append with the repeat check of S-06 (steps 2 to 6) and the limits of S-07. It waits
// up to LockWait for the store lock. With the lock, it reads the tail of the event's day file and decides
// under the same hold: a replay writes nothing; a repeat writes the reference line Reference(e, firstID);
// a new event writes the full line. Without the lock (another process holds it) the check and the fill
// levels are skipped and the full line is appended, as Append does. So is the check when e has no session
// or the tail cannot be read. When the reference line cannot be built, the full line is appended instead:
// a lost event costs more than a duplicate.
//
// It returns the verdict, firstID (the id of the first line, for VerdictRepeat only) and the error of the
// write, if any. VerdictDropped means the event was not written and a drops line was written, best effort,
// for its reason (full, big or disk). A disk drop returns the write error too; a full or big drop does not.
func AppendChecked(ctx context.Context, s *store.Store, e Event) (Verdict, string, error) {
	return appendEvent(ctx, s, e, true)
}

// appendEvent is the one append of an event. The line limit is checked first, since it needs no file.
// Then, under the store lock (when it can be taken), it checks the repeat (when repeatCheck) and the fill
// levels of the day file, and writes the line with one write (store.Append repairs a torn line in that
// write). A drop is written under the same hold, through writeDrop; with no lock it is written as
// AppendDrop writes it when the lock is busy.
func appendEvent(ctx context.Context, s *store.Store, e Event, repeatCheck bool) (Verdict, string, error) {
	file, err := FileFor(e.At)
	if err != nil {
		return VerdictNew, "", err
	}
	at, err := time.Parse(time.RFC3339, e.At)
	if err != nil {
		return VerdictNew, "", err
	}
	line, fits, err := fitLine(e)
	if err != nil {
		return VerdictNew, "", err
	}
	if !fits {
		_ = AppendDrop(ctx, s, DropBig, at)
		return VerdictDropped, "", nil
	}

	lctx, cancel := context.WithTimeout(ctx, LockWait)
	unlock, err := s.Lock(lctx)
	cancel()
	held := true
	switch {
	case err == nil:
		defer unlock()
	case errors.Is(err, store.ErrBusy):
		held = false
	default:
		return VerdictNew, "", err
	}

	v, first := VerdictNew, ""
	if repeatCheck && held {
		v, first = checkTail(s, file, e)
	}
	if v == VerdictReplay {
		return v, "", nil
	}
	if held && refused(s, file, e.Kind) {
		dropBest(s, DropFull, at)
		return VerdictDropped, "", nil
	}
	if v == VerdictRepeat {
		if ref, err := buildRefLine(Reference(e, first)); err == nil {
			return write(s, file, ref, at, VerdictRepeat, first)
		}
		// A lost event costs more than a duplicate: the full line is written instead.
	}
	return write(s, file, line, at, VerdictNew, "")
}

// write appends one line to file. A write error is a drop with the reason disk: the drop is written best
// effort and VerdictDropped is returned with the write error. Otherwise the verdict given is returned.
func write(s *store.Store, file string, line scrub.Clean, at time.Time, ok Verdict, firstID string) (Verdict, string, error) {
	if err := s.Append(file, line); err != nil {
		dropBest(s, DropDisk, at)
		return VerdictDropped, "", err
	}
	return ok, firstID, nil
}

// refused reports whether the day file at file is at its fill level for an event of kind: at fullAll or
// more every event is refused; at fullOK or more an ok event is. A file that cannot be stat'd is not full.
// It must run under the store lock.
func refused(s *store.Store, file, kind string) bool {
	fi, err := os.Stat(filepath.Join(s.Dir(), filepath.FromSlash(file)))
	if err != nil {
		return false
	}
	switch {
	case fi.Size() >= fullAll:
		return true
	case fi.Size() >= fullOK:
		return kind == KindOK
	}
	return false
}

// cutEvent is the shape of Event with input and error as text, for a line that is cut. Its fields, their
// JSON names and their order are Event's, so a cut line reads as any other line.
type cutEvent struct {
	V       int    `json:"v"`
	ID      string `json:"id"`
	At      string `json:"at"`
	Session string `json:"session"`
	Host    string `json:"host"`
	Tool    string `json:"tool"`
	Kind    string `json:"kind"`
	Repo    string `json:"repo,omitempty"`
	Cwd     string `json:"cwd"`
	Input   string `json:"input,omitzero"`
	Error   string `json:"error,omitzero"`
	Exit    *int   `json:"exit,omitempty"`
	FP      string `json:"fp,omitempty"`
	Dup     string `json:"dup,omitempty"`
}

// fitLine builds the line of e with its break and reports whether it is at most lineLimit bytes. When it is
// not, input and error are halved (by runes, keeping the first half) and the line is built again, up to
// cutMax halvings. It returns false when no halving fits.
func fitLine(e Event) (scrub.Clean, bool, error) {
	line, err := scrub.Line(e)
	if err != nil {
		return scrub.Clean{}, false, err
	}
	if len(line.String())+1 <= lineLimit {
		return line, true, nil
	}
	in, errText := e.Input.String(), e.Error.String()
	for cut := 1; cut <= cutMax; cut++ {
		in, errText = halfRunes(in), halfRunes(errText)
		c := cutEvent{
			V: e.V, ID: e.ID, At: e.At, Session: e.Session, Host: e.Host, Tool: e.Tool, Kind: e.Kind,
			Repo: e.Repo, Cwd: e.Cwd, Input: in, Error: errText, Exit: e.Exit, FP: e.FP, Dup: e.Dup,
		}
		line, err := scrub.Line(c)
		if err != nil {
			return scrub.Clean{}, false, err
		}
		if len(line.String())+1 <= lineLimit {
			return line, true, nil
		}
	}
	return scrub.Clean{}, false, nil
}

// halfRunes returns the first half of s by runes, so the result is valid UTF-8 whenever s is.
func halfRunes(s string) string {
	r := []rune(s)
	return string(r[:len(r)/2])
}

// checkTail is the S-06 check of e against the tail of the store-relative day file. A missing session or a
// tail that cannot be read gives VerdictNew: the check is skipped and the full line is written. It must run
// under the store lock.
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
