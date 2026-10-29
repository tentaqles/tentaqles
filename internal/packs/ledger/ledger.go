// Package ledger is the pack approvals ledger (spec C §2.11):
// $TQ_HOME/packs/<ws>/<pack>/ledger.jsonl, append-only, one JSON line per
// event (asked, answered, consumed, expired). The latest line per decision
// wins for its status; the asked line holds the binding (evidence, expiry,
// granting option). No line carries the note text.
//
// It imports neither the hub store nor internal/packs, so the store's
// answer path can call RecordAnswer without an import cycle.
package ledger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// Events.
const (
	Asked    = "asked"
	Answered = "answered"
	Consumed = "consumed"
	Expired  = "expired"
)

// FileName is the ledger's name inside a pack's state directory.
const FileName = "ledger.jsonl"

// Entry is one ledger line.
type Entry struct {
	Event     string            `json:"event"`
	Decision  string            `json:"decision"`
	Pack      string            `json:"pack"`
	Action    string            `json:"action,omitempty"`
	Subject   string            `json:"subject,omitempty"`
	Target    string            `json:"target,omitempty"`
	Evidence  map[string]string `json:"evidence,omitempty"`
	ExpiresAt string            `json:"expires_at,omitempty"`
	// Grant is the granting option (the request's first option), on the asked line.
	Grant     string `json:"grant,omitempty"`
	Choice    string `json:"choice,omitempty"`
	ActorKind string `json:"actor_kind,omitempty"` // human | agent | hub | terminal | other
	Card      string `json:"card,omitempty"`
	TS        string `json:"ts"`
}

// Path is the ledger of pack in workspace ws.
func Path(ws, pack string) (string, error) {
	if !plain(ws) || !plain(pack) {
		return "", fmt.Errorf("ledger: invalid workspace or pack name")
	}
	return filepath.Join(paths.Home(), "packs", ws, pack, FileName), nil
}

func plain(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\:`) && strings.TrimSpace(s) == s
}

var appendMu sync.Mutex

// Append writes one line (dir 0700, file 0600) and syncs it.
func Append(ws string, e Entry) error {
	p, err := Path(ws, e.Pack)
	if err != nil {
		return err
	}
	if e.Decision == "" || e.Event == "" {
		return errors.New("ledger: an entry needs an event and a decision")
	}
	if e.TS == "" {
		e.TS = time.Now().UTC().Format(time.RFC3339)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	appendMu.Lock()
	defer appendMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// RecordAnswer appends the answered line for a hub decision whose data marks
// a pack approval (data.pack set). Decisions without data.pack are ignored
// (nil error). The hub store calls it before acknowledging the answer; an
// error must fail the answer.
func RecordAnswer(ws, decisionID string, data map[string]any, choice, actorKind string, at time.Time) error {
	pack, _ := data["pack"].(string)
	if pack == "" {
		return nil
	}
	e := Entry{Event: Answered, Decision: decisionID, Pack: pack, Choice: choice, ActorKind: actorKind, TS: at.UTC().Format(time.RFC3339)}
	e.Action, _ = data["action"].(string)
	e.Subject, _ = data["subject"].(string)
	e.Target, _ = data["target"].(string)
	e.ExpiresAt, _ = data["expires_at"].(string)
	return Append(ws, e)
}

// Parse reads ledger bytes; malformed lines are skipped.
func Parse(raw []byte) []Entry {
	var out []Entry
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Decision != "" {
			out = append(out, e)
		}
	}
	return out
}

// Read reads one pack's ledger (missing: empty).
func Read(ws, pack string) ([]Entry, error) {
	p, err := Path(ws, pack)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(raw), nil
}

// ReadAll reads every pack ledger of workspace ws.
func ReadAll(ws string) ([]Entry, error) {
	if !plain(ws) {
		return nil, fmt.Errorf("ledger: invalid workspace name")
	}
	dirs, err := os.ReadDir(filepath.Join(paths.Home(), "packs", ws))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, d := range dirs {
		if !d.IsDir() || !plain(d.Name()) {
			continue
		}
		es, err := Read(ws, d.Name())
		if err != nil {
			return out, err
		}
		out = append(out, es...)
	}
	return out, nil
}

// State is one decision resolved from the ledger.
type State struct {
	Decision, Pack, Action, Subject, Target string
	Evidence                                map[string]string
	ExpiresAt                               time.Time
	Grant                                   string
	Card                                    string
	AskedAt                                 time.Time
	// Status is the latest event: asked (pending), answered, consumed or expired.
	Status    string
	Choice    string
	ActorKind string
	AnsweredAt,
	ConsumedAt time.Time
}

// Granted reports whether the human answered with the granting option.
func (s State) Granted() bool {
	return s.Choice != "" && s.Choice == s.Grant && s.ActorKind != "" && s.ActorKind != "agent"
}

// Expired reports whether the approval is past its expiry at now.
func (s State) Expired(now time.Time) bool {
	return s.Status == Expired || (!s.ExpiresAt.IsZero() && !now.Before(s.ExpiresAt))
}

// Used reports whether the approval was consumed.
func (s State) Used() bool { return s.Status == Consumed || !s.ConsumedAt.IsZero() }

// Resolve folds the entries into one State per decision. The asked line
// gives the binding; later lines give the status. An answered line never
// changes the binding.
func Resolve(entries []Entry) map[string]State {
	out := map[string]State{}
	for _, e := range entries {
		s, ok := out[e.Decision]
		if !ok {
			s = State{Decision: e.Decision, Pack: e.Pack}
		}
		ts, _ := time.Parse(time.RFC3339, e.TS)
		switch e.Event {
		case Asked:
			if s.AskedAt.IsZero() {
				s.Action, s.Subject, s.Target, s.Grant, s.Card = e.Action, e.Subject, e.Target, e.Grant, e.Card
				s.Evidence = e.Evidence
				s.ExpiresAt, _ = time.Parse(time.RFC3339, e.ExpiresAt)
				s.AskedAt = ts
			}
		case Answered:
			if s.AnsweredAt.IsZero() {
				s.Choice, s.ActorKind, s.AnsweredAt = e.Choice, e.ActorKind, ts
			}
		case Consumed:
			if s.ConsumedAt.IsZero() {
				s.ConsumedAt = ts
			}
		}
		if e.Event == Asked || e.Event == Answered || e.Event == Consumed || e.Event == Expired {
			s.Status = e.Event
		}
		if !s.ConsumedAt.IsZero() {
			s.Status = Consumed // nothing un-consumes an approval
		}
		out[e.Decision] = s
	}
	return out
}

// Sorted returns the states ordered by asked time, newest first.
func Sorted(m map[string]State) []State {
	out := make([]State, 0, len(m))
	for _, s := range m {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].AskedAt.Equal(out[j].AskedAt) {
			return out[i].AskedAt.After(out[j].AskedAt)
		}
		return out[i].Decision < out[j].Decision
	})
	return out
}

// EqualEvidence compares two evidence maps exactly.
func EqualEvidence(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
