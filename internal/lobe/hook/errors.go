package hook

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

// Error is a sentinel error of this package. It is a string type so the sentinels can be constants.
type Error string

func (e Error) Error() string { return string(e) }

const (
	// ErrOversize is an output too large to record.
	ErrOversize = Error("lobe hook: the output is too large")
	// ErrDetach is a child process that could not be started.
	ErrDetach = Error("lobe hook: a child process could not be started")
)

// The seven classes an error line can name. They are the only words an error line holds about a failure.
const (
	classDeadline = "deadline"
	classPanic    = "panic"
	classBusy     = "busy"
	classOversize = "oversize"
	classDetach   = "detach"
	classIO       = "io"
	classError    = "error"
)

// errorFile is the store-relative file of the error lines.
const errorFile = "stats/hook-errors.jsonl"

// classify names the class of a step's error.
func classify(err error) string {
	var pe *fs.PathError
	var le *os.LinkError
	var se *os.SyscallError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return classDeadline
	case errors.Is(err, store.ErrBusy):
		return classBusy
	case errors.Is(err, ErrOversize):
		return classOversize
	case errors.Is(err, ErrDetach):
		return classDetach
	case errors.As(err, &pe), errors.As(err, &le), errors.As(err, &se):
		return classIO
	}
	return classError
}

// errorLine is one line of the error file: the keys in this order, and nothing else.
type errorLine struct {
	At    string `json:"at"`
	Host  string `json:"host"`
	Event string `json:"event"`
	Step  string `json:"step"`
	Class string `json:"class"`
}

// writeError appends the error line of a run to the store's error file. It takes no lock and gives up
// silently: a line that cannot be written is dropped. The line holds no payload text, path, tool name,
// session id or error text.
func writeError(st *store.Store, host, event, step, class string, now time.Time) {
	line, err := scrub.Line(errorLine{
		At:    now.UTC().Format(time.RFC3339),
		Host:  host,
		Event: event,
		Step:  step,
		Class: class,
	})
	if err != nil {
		return
	}
	_ = st.Append(errorFile, line)
}
