package packs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// EventsFile is $TQ_HOME/packs/events.jsonl: aggregate-only lines that
// tq insights reads. A line never holds a file name, a message or a value.
func EventsFile() string { return filepath.Join(paths.Home(), "packs", "events.jsonl") }

// event is one line: a check result, or (Counter set) a pack counter.
type event struct {
	TS         string         `json:"ts"`
	Workspace  string         `json:"workspace,omitempty"`
	Check      string         `json:"check,omitempty"`
	Pack       string         `json:"pack"`
	Status     string         `json:"status,omitempty"`
	Required   bool           `json:"required,omitempty"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Missing    bool           `json:"declared_missing,omitempty"`
	Counts     map[string]int `json:"findings,omitempty"`
	Counter    string         `json:"counter,omitempty"`
	N          int            `json:"n,omitempty"`
}

var (
	eventsMu  sync.Mutex
	currentWS string
)

// SetEventWorkspace names the workspace Count lines are attributed to. The
// CLI and the hook set it once they resolved the workspace.
func SetEventWorkspace(ws string) {
	eventsMu.Lock()
	currentWS = ws
	eventsMu.Unlock()
}

// counterRe keeps counter names to names: no paths, no values.
var counterRe = regexp.MustCompile(`^[a-z0-9_]+(:[a-z0-9_.-]+)?$`)

// Count appends a pack counter (spec C §2.13), for example
// Count("cloud", "apply_denied:expired", 1). Counter names are
// [a-z0-9_]+(:[a-z0-9_.-]+)?; anything else is dropped. Best-effort: an
// error is ignored. Guards must use GuardCtx.Count instead, which the hook
// writes after deciding.
func Count(pack, counter string, n int) {
	if !knownPack(pack) || !counterRe.MatchString(counter) {
		return
	}
	eventsMu.Lock()
	ws := currentWS
	eventsMu.Unlock()
	appendEvent(event{Workspace: ws, Pack: pack, Counter: counter, N: n})
}

func appendEvent(e event) {
	e.TS = time.Now().UTC().Format(time.RFC3339)
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	eventsMu.Lock()
	defer eventsMu.Unlock()
	p := EventsFile()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()
}
