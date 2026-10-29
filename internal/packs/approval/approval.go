// Package approval turns a gated pack action (an ML promotion, an apply or a
// destroy) into a hub decision a human answers (spec C §2.11). The decision
// is the hub's existing kind `permission` with data.pack set, so the hub
// store schema does not change. The binding (evidence hashes, expiry,
// granting option) and every state change live in the pack ledger
// ($TQ_HOME/packs/<ws>/<pack>/ledger.jsonl), which Verify and the guards
// read without opening the hub store.
package approval

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
	"github.com/tentaqles/tentaqles/internal/redact"
)

// Request asks a human to approve one gated action.
type Request struct {
	Pack, Action, Subject, Target string            // "ml", "promote", "churn@7", "production"
	Evidence                      map[string]string // name -> sha256 (files, results, git HEAD)
	Summary                       string            // redacted before storage
	Options                       []string          // first = the granting choice; default ["approve","reject"]
	ValidFor                      time.Duration     // 0 = the pack default; over the cap is an error
	CardID                        string            // the run's card; "" = TQ_HUB_TASK's card, else a new card
	ProjectDir                    string
}

// Limits are a pack's ValidFor default and cap.
type Limits struct{ Default, Max time.Duration }

// PackLimits: ml and data 7 days (max 30), cloud and db 60 minutes (max 24 h).
var PackLimits = map[string]Limits{
	"ml":    {7 * 24 * time.Hour, 30 * 24 * time.Hour},
	"data":  {7 * 24 * time.Hour, 30 * 24 * time.Hour},
	"cloud": {60 * time.Minute, 24 * time.Hour},
	"db":    {60 * time.Minute, 24 * time.Hour},
}

// Approval is a granted, unexpired, unused decision.
type Approval struct {
	Decision, Card, Pack, Action, Subject, Target string
	Evidence                                      map[string]string
	ExpiresAt, AnsweredAt                         time.Time
	Choice, ActorKind                             string
}

// Verify failures. errors.Is tells them apart.
var (
	ErrUnknown  = errors.New("no such pack approval in the ledger")
	ErrPending  = errors.New("the approval has not been answered yet")
	ErrRejected = errors.New("the approval was not granted")
	ErrAgent    = errors.New("the approval was not answered by a human")
	ErrExpired  = errors.New("the approval has expired")
	ErrUsed     = errors.New("the approval was already used")
	ErrEvidence = errors.New("the evidence changed since the approval was asked")
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_.:@/+-]{1,200}$`)

// Now is the clock (tests replace it).
var Now = func() time.Time { return time.Now().UTC() }

func validate(r *Request) error {
	lim, ok := PackLimits[r.Pack]
	if !ok {
		return fmt.Errorf("approval: unknown pack %q", r.Pack)
	}
	for k, v := range map[string]string{"action": r.Action, "subject": r.Subject, "target": r.Target} {
		if !nameRe.MatchString(v) {
			return fmt.Errorf("approval: %s %q must be a short name ([A-Za-z0-9_.:@/+-])", k, v)
		}
	}
	if len(r.Evidence) == 0 {
		return errors.New("approval: evidence is required (name -> sha256)")
	}
	for k, v := range r.Evidence {
		if !nameRe.MatchString(k) || v == "" || len(v) > 200 {
			return fmt.Errorf("approval: bad evidence entry %q", k)
		}
	}
	if len(r.Options) == 0 {
		r.Options = []string{"approve", "reject"}
	}
	if len(r.Options) < 2 {
		return errors.New("approval: options need the granting choice and at least one other")
	}
	if r.ValidFor == 0 {
		r.ValidFor = lim.Default
	}
	if r.ValidFor < 0 || r.ValidFor > lim.Max {
		return fmt.Errorf("approval: valid_for %s is over the %s pack's cap %s", r.ValidFor, r.Pack, lim.Max)
	}
	return nil
}

// taskCard reads the card of the hub run this session belongs to.
func taskCard() string {
	var t struct {
		Hub string `json:"hub"`
	}
	raw := strings.TrimSpace(os.Getenv("TQ_HUB_TASK"))
	if raw != "" && json.Unmarshal([]byte(raw), &t) == nil {
		return t.Hub
	}
	return ""
}

// OpenStore opens the workspace's hub store from the CLI (tests replace it).
var OpenStore = func(ws string) (*store.Store, error) { return store.Open(ws, nil) }

// Ask records the request as a hub permission decision (on the run's card,
// or on a new chore card in todo tagged <pack>-approval) and appends the
// asked line to the ledger. Agents may ask; only a human answers.
func Ask(ws string, r Request) (decisionID, cardID string, err error) {
	if err := validate(&r); err != nil {
		return "", "", err
	}
	st, err := OpenStore(ws)
	if err != nil {
		return "", "", err
	}
	defer st.Close()
	cardID = r.CardID
	if cardID == "" {
		cardID = taskCard()
	}
	if cardID != "" {
		if _, err := st.Card(cardID); err != nil {
			return "", "", fmt.Errorf("approval: card %s: %w", cardID, err)
		}
	} else {
		lane, err := st.FirstLane(store.LaneTodo)
		if err != nil {
			return "", "", err
		}
		c, err := st.AddCard(store.NewCard{Title: fmt.Sprintf("%s approval: %s %s -> %s", r.Pack, r.Action, r.Subject, r.Target),
			Kind: "chore", Lane: lane.Name, Tags: []string{r.Pack + "-approval"}, ProjectDir: r.ProjectDir}, "tq pack")
		if err != nil {
			return "", "", err
		}
		cardID = c.ID
	}
	expires := Now().Add(r.ValidFor).Format(time.RFC3339)
	ev := map[string]any{}
	for k, v := range r.Evidence {
		ev[k] = v
	}
	summary := redact.Redact(strings.TrimSpace(r.Summary), []string{"all"})
	text := fmt.Sprintf("%s approval: %s %s -> %s (expires %s)", r.Pack, r.Action, r.Subject, r.Target, expires)
	if summary != "" {
		text += "\n\n" + summary
	}
	d, err := st.AskHub(cardID, "", "", text, r.Options, map[string]any{
		"pack": r.Pack, "action": r.Action, "subject": r.Subject, "target": r.Target, "evidence": ev, "expires_at": expires,
	})
	if err != nil {
		return "", cardID, err
	}
	if err := ledger.Append(ws, ledger.Entry{Event: ledger.Asked, Decision: d.ID, Pack: r.Pack, Action: r.Action, Subject: r.Subject,
		Target: r.Target, Evidence: r.Evidence, ExpiresAt: expires, Grant: r.Options[0], Card: cardID}); err != nil {
		return d.ID, cardID, fmt.Errorf("approval: decision %s was asked but the ledger write failed (%v); it cannot be granted, ask again", d.ID, err)
	}
	return d.ID, cardID, nil
}

// check verifies one resolved state against the evidence recomputed now.
func check(s ledger.State, evidence map[string]string, now time.Time) (Approval, error) {
	a := Approval{Decision: s.Decision, Card: s.Card, Pack: s.Pack, Action: s.Action, Subject: s.Subject, Target: s.Target,
		Evidence: s.Evidence, ExpiresAt: s.ExpiresAt, AnsweredAt: s.AnsweredAt, Choice: s.Choice, ActorKind: s.ActorKind}
	switch {
	case s.AskedAt.IsZero():
		return a, fmt.Errorf("%s: %w", s.Decision, ErrUnknown)
	case s.Used():
		return a, fmt.Errorf("%s: %w", s.Decision, ErrUsed)
	case s.AnsweredAt.IsZero():
		if s.Expired(now) {
			return a, fmt.Errorf("%s: %w", s.Decision, ErrExpired)
		}
		return a, fmt.Errorf("%s: %w", s.Decision, ErrPending)
	case s.ActorKind == "" || s.ActorKind == "agent":
		return a, fmt.Errorf("%s: %w", s.Decision, ErrAgent)
	case s.Choice != s.Grant:
		return a, fmt.Errorf("%s: %w (%s)", s.Decision, ErrRejected, s.Choice)
	case s.Expired(now):
		return a, fmt.Errorf("%s: %w", s.Decision, ErrExpired)
	case !ledger.EqualEvidence(s.Evidence, evidence):
		return a, fmt.Errorf("%s: %w", s.Decision, ErrEvidence)
	}
	return a, nil
}

// Verify succeeds only for a decision a human answered with the granting
// option, unexpired, unused, whose evidence equals the map recomputed now.
// It reads only the ledger.
func Verify(ws, id string, evidence map[string]string) (Approval, error) {
	entries, err := ledger.ReadAll(ws)
	if err != nil {
		return Approval{}, err
	}
	return VerifyEntries(entries, id, evidence, Now())
}

// VerifyEntries is Verify over entries already read (a guard reads them with
// GuardCtx.ReadState(ledger.FileName)).
func VerifyEntries(entries []ledger.Entry, id string, evidence map[string]string, now time.Time) (Approval, error) {
	s, ok := ledger.Resolve(entries)[id]
	if !ok {
		return Approval{Decision: id}, fmt.Errorf("%s: %w", id, ErrUnknown)
	}
	return check(s, evidence, now)
}

// Find returns the one usable approval for (action, target) whose evidence
// equals evidence. When none is usable it returns the error of the most
// recent candidate (so a guard can say "expired" or "evidence changed"), or
// ErrUnknown when there is no candidate at all.
func Find(entries []ledger.Entry, action, target string, evidence map[string]string, now time.Time) (Approval, error) {
	var lastErr error
	for _, s := range ledger.Sorted(ledger.Resolve(entries)) {
		if s.Action != action || s.Target != target {
			continue
		}
		a, err := check(s, evidence, now)
		if err == nil {
			return a, nil
		}
		if lastErr == nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("%s -> %s: %w", action, target, ErrUnknown)
	}
	return Approval{}, lastErr
}

// FindGuard is Find for a decision guard: it reads the pack ledger through
// GuardCtx.ReadState (1 MiB, bounded). A read error is returned: deny.
func FindGuard(g packs.GuardCtx, action, target string, evidence map[string]string) (Approval, error) {
	raw, err := g.ReadState(ledger.FileName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Approval{}, fmt.Errorf("%s -> %s: %w", action, target, ErrUnknown)
		}
		return Approval{}, err
	}
	return Find(ledger.Parse(raw), action, target, evidence, Now())
}

// MarkUsed appends the consumed line (a human-run apply calls it after the
// command ran).
func MarkUsed(ws, id string) error {
	entries, err := ledger.ReadAll(ws)
	if err != nil {
		return err
	}
	s, ok := ledger.Resolve(entries)[id]
	if !ok {
		return fmt.Errorf("%s: %w", id, ErrUnknown)
	}
	return ledger.Append(ws, ledger.Entry{Event: ledger.Consumed, Decision: id, Pack: s.Pack, Action: s.Action, Subject: s.Subject, Target: s.Target})
}

// Status is one row of tq pack approvals.
type Status struct {
	Decision  string    `json:"decision"`
	Card      string    `json:"card,omitempty"`
	Pack      string    `json:"pack"`
	Action    string    `json:"action"`
	Subject   string    `json:"subject"`
	Target    string    `json:"target"`
	State     string    `json:"state"` // pending | approved | rejected | used | expired
	Choice    string    `json:"choice,omitempty"`
	ActorKind string    `json:"actor_kind,omitempty"`
	AskedAt   time.Time `json:"asked_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Evidence  []string  `json:"evidence"` // names only
}

// List returns the workspace's pack approvals (pack "" = all), newest first.
func List(ws, pack string) ([]Status, error) {
	entries, err := ledger.ReadAll(ws)
	if err != nil {
		return nil, err
	}
	now := Now()
	out := []Status{}
	for _, s := range ledger.Sorted(ledger.Resolve(entries)) {
		if pack != "" && s.Pack != pack {
			continue
		}
		row := Status{Decision: s.Decision, Card: s.Card, Pack: s.Pack, Action: s.Action, Subject: s.Subject, Target: s.Target,
			Choice: s.Choice, ActorKind: s.ActorKind, AskedAt: s.AskedAt, ExpiresAt: s.ExpiresAt, Evidence: []string{}}
		for k := range s.Evidence {
			row.Evidence = append(row.Evidence, k)
		}
		sort.Strings(row.Evidence)
		switch {
		case s.Used():
			row.State = "used"
		case s.Expired(now):
			row.State = "expired"
		case s.AnsweredAt.IsZero():
			row.State = "pending"
		case s.Choice == s.Grant && s.ActorKind != "agent":
			row.State = "approved"
		default:
			row.State = "rejected"
		}
		out = append(out, row)
	}
	return out, nil
}

// ErrNotConfirmed is returned when the human did not type yes.
var ErrNotConfirmed = errors.New("not confirmed")

// Confirm shows the exact command a human-run apply will run and asks for a
// typed "yes" on a terminal. isTTY false refuses (an apply never runs from a
// pipe).
func Confirm(in io.Reader, out io.Writer, isTTY bool, command string) error {
	if !isTTY {
		return errors.New("this command needs a terminal: type yes to confirm the command it runs")
	}
	fmt.Fprintf(out, "tq will run:\n\n  %s\n\nType yes to run it: ", command)
	line, _ := bufio.NewReader(in).ReadString('\n')
	if strings.TrimSpace(line) != "yes" {
		return ErrNotConfirmed
	}
	return nil
}

// Comment posts a redacted result line on the approval's card.
func Comment(ws, cardID, body string) error {
	if cardID == "" {
		return nil
	}
	st, err := OpenStore(ws)
	if err != nil {
		return err
	}
	defer st.Close()
	_, err = st.AddComment(cardID, store.AuthorSystem, "tq pack", redact.Redact(body, []string{"all"}), "")
	return err
}
