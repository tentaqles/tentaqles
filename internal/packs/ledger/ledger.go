// Package ledger is the pack approvals ledger (spec C §2.11):
// $TQ_HOME/packs/<ws>/<pack>/ledger.jsonl, append-only, one JSON line per
// event (asked, answered, consumed, expired). The latest line per decision
// wins for its status; the asked line holds the binding (evidence, expiry,
// granting option). No line carries the note text.
//
// It imports neither the hub store nor internal/packs, so the store's
// answer path can append the signed answered line without an import cycle.
package ledger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	// EvidenceHash, AskedTS and Sig sign an answered line (sign.go); an
	// answered line also repeats Grant and ExpiresAt, under the signature.
	EvidenceHash string `json:"evidence_hash,omitempty"`
	AskedTS      string `json:"asked_ts,omitempty"`
	Sig          string `json:"sig,omitempty"`
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
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	release, err := lockPack(filepath.Dir(p))
	if err != nil {
		return err
	}
	defer release()
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

// ErrCorrupt is a ledger that cannot be read completely: a malformed or
// oversized line. Readers fail closed on it (a guard denies, Verify fails).
var ErrCorrupt = errors.New("ledger: corrupt or truncated")

// Parse reads ledger bytes. Blank lines are skipped; a malformed line, a
// line without a decision id or an oversized line is ErrCorrupt (the
// entries before it are returned with the error).
func Parse(raw []byte) ([]Entry, error) {
	var out []Entry
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil || e.Decision == "" || e.Event == "" {
			return out, fmt.Errorf("%w: line %d", ErrCorrupt, n)
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	return out, nil
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
	return Parse(raw)
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
	// EvidenceHash is the signed evidence hash (ResolveSigned); empty for an
	// unsigned resolution, which compares Evidence instead.
	EvidenceHash string
	Card         string
	AskedAt      time.Time
	// Status is the latest event: asked (pending), answered, consumed or expired.
	Status    string
	Choice    string
	ActorKind string
	AnsweredAt,
	ConsumedAt time.Time
}

// HumanKinds are the actor kinds that count as a human answer: an answer
// through the hub (hub:<user>) or the terminal. Anything else is an agent.
var HumanKinds = []string{"hub", "terminal"}

// IsHuman reports whether an actor kind is a human one (allow-list).
func IsHuman(kind string) bool {
	for _, k := range HumanKinds {
		if kind == k {
			return true
		}
	}
	return false
}

// Granted reports whether a human answered with the granting option.
func (s State) Granted() bool {
	return s.Choice != "" && s.Choice == s.Grant && IsHuman(s.ActorKind)
}

// Expired reports whether the approval is past its expiry at now. An asked
// line without a readable expiry counts as expired (fail closed).
func (s State) Expired(now time.Time) bool {
	return s.Status == Expired || s.ExpiresAt.IsZero() || !now.Before(s.ExpiresAt)
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

// ClaimsDir is the claim folder of a pack: one empty file per used approval.
func ClaimsDir(ws, pack string) (string, error) {
	p, err := Path(ws, pack)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "claims"), nil
}

// ErrClaimed is returned when an approval was already claimed (used).
var ErrClaimed = errors.New("the approval was already used")

var claimRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Claim marks decision as used, atomically across processes: it creates
// claims/<decision> with O_CREATE|O_EXCL. Exactly one claimer succeeds; the
// others get ErrClaimed. Call it before the allow (a guard's hook) or before
// running the command (a human-run apply), then Append the consumed line.
func Claim(ws, pack, decision string) error {
	if !claimRe.MatchString(decision) {
		return fmt.Errorf("ledger: bad decision id %q", decision)
	}
	dir, err := ClaimsDir(ws, pack)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, decision), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s: %w", decision, ErrClaimed)
	}
	if err != nil {
		return err
	}
	return f.Close()
}

// ClaimName is the claim file's path relative to the pack's state directory
// (for GuardCtx.ReadState).
func ClaimName(decision string) string { return "claims/" + decision }

// Claimed reports whether decision was claimed. A stat error other than
// "not there" counts as claimed (fail closed).
func Claimed(ws, pack, decision string) bool {
	if !claimRe.MatchString(decision) {
		return true
	}
	dir, err := ClaimsDir(ws, pack)
	if err != nil {
		return true
	}
	_, err = os.Lstat(filepath.Join(dir, decision))
	return !errors.Is(err, os.ErrNotExist)
}

// CompactAt is the ledger size past which Compact rewrites it (ReadState
// reads at most 1 MiB).
const CompactAt = 512 << 10

// LockWait bounds the wait for the ledger lock.
var LockWait = 2 * time.Second

// ErrLocked is returned when the ledger lock was not taken within LockWait.
var ErrLocked = errors.New("ledger: locked by another tq process")

// lockPack takes the pack's OS file lock (<pack>/ledger.lock: LockFileEx on
// Windows, flock elsewhere), across processes, waiting at most LockWait.
func lockPack(dir string) (func(), error) {
	appendMu.Lock()
	f, err := os.OpenFile(filepath.Join(dir, "ledger.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		appendMu.Unlock()
		return nil, err
	}
	deadline := time.Now().Add(LockWait)
	for !tryLock(f) {
		if time.Now().After(deadline) {
			f.Close()
			appendMu.Unlock()
			return nil, ErrLocked
		}
		time.Sleep(5 * time.Millisecond)
	}
	return func() {
		unlock(f)
		f.Close()
		appendMu.Unlock()
	}, nil
}

// ErrCompactSkipped is returned when the ledger could not be replaced this
// time (Windows: another process has it open). Nothing is lost.
var ErrCompactSkipped = errors.New("ledger: compaction skipped (the ledger is in use)")

// Compact rewrites the pack ledger without the decisions that are finished
// (consumed, or expired at now), so it never reaches the read cap. A
// finished decision cannot be granted again: its claim file stays, and a
// decision without an asked line is unknown. It holds the pack lock while it
// reads and replaces the file, so no concurrent Append is lost; it writes a
// fresh temp file, fsyncs it and renames it over the ledger (retrying a
// Windows sharing violation for about 50 ms, then skipping).
func Compact(ws, pack string, now time.Time) error {
	p, err := Path(ws, pack)
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return nil
	}
	release, err := lockPack(dir)
	if err != nil {
		return err
	}
	defer release()
	raw, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	es, err := Parse(raw)
	if err != nil {
		return err
	}
	st := Resolve(es)
	var buf bytes.Buffer
	kept := 0
	for _, e := range es {
		s := st[e.Decision]
		if s.Used() || s.Expired(now) {
			continue
		}
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf.Write(append(b, '\n'))
		kept++
	}
	if kept == len(es) {
		return nil
	}
	tmp, err := os.CreateTemp(dir, "ledger-*.compact")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	for i := 0; ; i++ {
		err = os.Rename(name, p)
		if err == nil {
			return syncDir(dir)
		}
		if !renameRetryable(err) || i >= 10 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if renameRetryable(err) {
		return ErrCompactSkipped
	}
	return err
}

// CompactIfLarge compacts the ledger when it is over CompactAt.
func CompactIfLarge(ws, pack string, now time.Time) error {
	p, err := Path(ws, pack)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(p); err == nil && fi.Size() > CompactAt {
		return Compact(ws, pack, now)
	}
	return nil
}

// Packs lists the packs of workspace ws that have a state directory.
func Packs(ws string) ([]string, error) {
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
	var out []string
	for _, d := range dirs {
		if d.IsDir() && plain(d.Name()) {
			out = append(out, d.Name())
		}
	}
	return out, nil
}
