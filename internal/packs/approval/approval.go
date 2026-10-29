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

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
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
	ErrBinding  = errors.New("the approval is for another pack, action, subject or target")
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
	// Keep the ledger under the guards' 1 MiB read cap (finished decisions go).
	// Best-effort: a busy ledger is compacted next time; only the read cap is
	// a hard stop, and it fails closed.
	_ = ledger.CompactIfLarge(ws, r.Pack, Now())
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

// trustFor is the workspace's public key and pack watermarks, or an empty
// Trust when the keychain has none or cannot be read (fail closed: no
// answered line then counts).
func trustFor(ws string, packs ...string) ledger.Trust {
	t, err := ledger.LoadTrust(ws, packs...)
	if err != nil {
		return ledger.Trust{}
	}
	return t
}

// Binding is what an approval must be for. Verify compares all four
// fields; Find compares Pack, Action and Target, and Subject when set.
type Binding struct {
	Pack, Action, Subject, Target string
}

// check verifies one resolved state against the binding and the evidence
// recomputed now. claimed reports an existing claim (used).
func check(s ledger.State, want Binding, evidence map[string]string, now time.Time, claimed bool) (Approval, error) {
	a := Approval{Decision: s.Decision, Card: s.Card, Pack: s.Pack, Action: s.Action, Subject: s.Subject, Target: s.Target,
		Evidence: s.Evidence, ExpiresAt: s.ExpiresAt, AnsweredAt: s.AnsweredAt, Choice: s.Choice, ActorKind: s.ActorKind}
	switch {
	case s.AskedAt.IsZero():
		return a, fmt.Errorf("%s: %w", s.Decision, ErrUnknown)
	case s.Pack != want.Pack || s.Action != want.Action || s.Target != want.Target || (want.Subject != "" && s.Subject != want.Subject):
		return a, fmt.Errorf("%s: %w (it is for %s %s %s -> %s)", s.Decision, ErrBinding, s.Pack, s.Action, s.Subject, s.Target)
	case s.Used() || claimed:
		return a, fmt.Errorf("%s: %w", s.Decision, ErrUsed)
	case s.AnsweredAt.IsZero():
		if s.Expired(now) {
			return a, fmt.Errorf("%s: %w", s.Decision, ErrExpired)
		}
		return a, fmt.Errorf("%s: %w", s.Decision, ErrPending)
	case !ledger.IsHuman(s.ActorKind):
		return a, fmt.Errorf("%s: %w", s.Decision, ErrAgent)
	case s.Choice != s.Grant:
		return a, fmt.Errorf("%s: %w (%s)", s.Decision, ErrRejected, s.Choice)
	case s.Expired(now):
		return a, fmt.Errorf("%s: %w", s.Decision, ErrExpired)
	case s.EvidenceHash != "" && ledger.EvidenceHash(evidence) != s.EvidenceHash,
		s.EvidenceHash == "" && !ledger.EqualEvidence(s.Evidence, evidence):
		return a, fmt.Errorf("%s: %w", s.Decision, ErrEvidence)
	}
	return a, nil
}

func validBinding(want Binding) error {
	if _, ok := PackLimits[want.Pack]; !ok || want.Action == "" || want.Target == "" {
		return fmt.Errorf("approval: a binding needs a known pack, an action and a target")
	}
	return nil
}

// Verify succeeds only for decision id of want's pack, for exactly want's
// action, subject and target, answered by a human with the granting
// option, unexpired, unused (no claim) and with evidence equal to the map
// recomputed now. It reads only that pack's ledger; a corrupt ledger fails.
func Verify(ws, id string, want Binding, evidence map[string]string) (Approval, error) {
	if err := validBinding(want); err != nil {
		return Approval{}, err
	}
	if want.Subject == "" {
		return Approval{}, errors.New("approval: Verify needs the subject")
	}
	entries, err := ledger.Read(ws, want.Pack)
	if err != nil {
		return Approval{}, err
	}
	return verifyEntries(entries, trustFor(ws, want.Pack), id, want, evidence, Now(), func(d string) bool { return ledger.Claimed(ws, want.Pack, d) })
}

// VerifyEntries is Verify over entries already read, with the workspace's
// Trust (no key: no answer counts); claimed reports a claim for a decision
// id (nil: none known).
func VerifyEntries(entries []ledger.Entry, t ledger.Trust, id string, want Binding, evidence map[string]string, now time.Time, claimed func(string) bool) (Approval, error) {
	if err := validBinding(want); err != nil {
		return Approval{}, err
	}
	return verifyEntries(entries, t, id, want, evidence, now, claimed)
}

func verifyEntries(entries []ledger.Entry, t ledger.Trust, id string, want Binding, evidence map[string]string, now time.Time, claimed func(string) bool) (Approval, error) {
	s, ok := ledger.ResolveSigned(entries, t)[id]
	if !ok {
		return Approval{Decision: id}, fmt.Errorf("%s: %w", id, ErrUnknown)
	}
	return check(s, want, evidence, now, claimed != nil && claimed(id))
}

// Find returns the one usable approval for want whose evidence equals
// evidence (Subject "" matches any subject). When none is usable it
// returns the error of the most recent candidate (so a guard can say
// "expired" or "evidence changed"), or ErrUnknown when there is none.
func Find(entries []ledger.Entry, t ledger.Trust, want Binding, evidence map[string]string, now time.Time, claimed func(string) bool) (Approval, error) {
	if err := validBinding(want); err != nil {
		return Approval{}, err
	}
	var lastErr error
	for _, s := range ledger.Sorted(ledger.ResolveSigned(entries, t)) {
		if s.Pack != want.Pack || s.Action != want.Action || s.Target != want.Target || (want.Subject != "" && s.Subject != want.Subject) {
			continue
		}
		a, err := check(s, want, evidence, now, claimed != nil && claimed(s.Decision))
		if err == nil {
			return a, nil
		}
		if lastErr == nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("%s %s -> %s: %w", want.Pack, want.Action, want.Target, ErrUnknown)
	}
	return Approval{}, lastErr
}

// FindGuard is Find for a decision guard of want.Pack: it reads that pack's
// ledger and claims through GuardCtx.ReadState (1 MiB, bounded). Any read
// error, a corrupt ledger or a guard of another pack is an error: deny. On
// success the guard calls g.Consume(a.Decision); the hook claims it
// atomically before the allow.
func FindGuard(g packs.GuardCtx, want Binding, evidence map[string]string) (Approval, error) {
	if want.Pack != g.Pack() {
		return Approval{}, fmt.Errorf("approval: a %s guard cannot use a %s approval", g.Pack(), want.Pack)
	}
	raw, err := g.ReadState(ledger.FileName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Approval{}, fmt.Errorf("%s -> %s: %w", want.Action, want.Target, ErrUnknown)
		}
		return Approval{}, err
	}
	entries, err := ledger.Parse(raw)
	if err != nil {
		return Approval{}, err
	}
	claimed := func(d string) bool {
		_, err := g.ReadState(ledger.ClaimName(d))
		return !errors.Is(err, os.ErrNotExist) // a claim, or an error: used (fail closed)
	}
	return Find(entries, trustFor(g.Workspace(), want.Pack), want, evidence, Now(), claimed)
}

// Claim is the human-run apply's single-use step, after Confirm and just
// before the command runs: it verifies the approval, claims it atomically
// (exactly one caller wins) and raises the pack's high-water mark (a failed
// write is an error: do not run the command). Call MarkUsed after the
// command. The order of a human-run apply is FromCommand, HumanOnly,
// Verify (to show what will run), Confirm, Claim, run, MarkUsed, Comment.
func Claim(c *cobra.Command, ws, id string, want Binding, evidence map[string]string) (Approval, error) {
	if err := executor(c); err != nil {
		return Approval{}, err
	}
	a, err := Verify(ws, id, want, evidence)
	if err != nil {
		return a, err
	}
	if err := ledger.Claim(ws, want.Pack, id); err != nil {
		if errors.Is(err, ledger.ErrClaimed) {
			return a, fmt.Errorf("%s: %w", id, ErrUsed)
		}
		return a, err
	}
	if err := ledger.SetHighWater(ws, want.Pack, id); err != nil {
		return a, fmt.Errorf("approval: %s was claimed but its use could not be recorded in the keychain (%v); do not run the command", id, err)
	}
	return a, nil
}

// MarkUsed appends the consumed line of decision id of pack (claiming it
// first when nobody did).
func MarkUsed(ws, pack, id string) error {
	entries, err := ledger.Read(ws, pack)
	if err != nil {
		return err
	}
	s, ok := ledger.Resolve(entries)[id]
	if !ok {
		return fmt.Errorf("%s: %w", id, ErrUnknown)
	}
	if err := ledger.Claim(ws, pack, id); err != nil && !errors.Is(err, ledger.ErrClaimed) {
		return err
	}
	if err := ledger.SetHighWater(ws, pack, id); err != nil {
		return err
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
	names, err := ledger.Packs(ws)
	if err != nil {
		return nil, err
	}
	now := Now()
	out := []Status{}
	var states []ledger.State
	for _, p := range names {
		if pack != "" && p != pack {
			continue
		}
		// Per pack: decisions are keyed by (pack, id), never mixed across packs.
		entries, err := ledger.Read(ws, p)
		if err != nil {
			return nil, err
		}
		states = append(states, ledger.Sorted(ledger.ResolveSigned(entries, trustFor(ws, p)))...)
	}
	sort.SliceStable(states, func(i, j int) bool { return states[i].AskedAt.After(states[j].AskedAt) })
	for _, s := range states {
		if ledger.Claimed(ws, s.Pack, s.Decision) {
			s.ConsumedAt = s.AskedAt // claimed: used
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
		case s.Choice == s.Grant && ledger.IsHuman(s.ActorKind):
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

// isTerminal tells a terminal from a pipe or a file (tests replace it).
var isTerminal = func(f *os.File) bool { return f != nil && term.IsTerminal(f.Fd()) }

// ErrNotExecutor refuses Confirm and Claim from a command that does not
// declare itself an executor (R6-1): tq/human-answer denies agents exactly
// the commands that declare it, so an undeclared caller would slip past it.
var ErrNotExecutor = errors.New("approval: only a pack command annotated " + packs.AnnotationHumanOnly + ": " + packs.HumanOnlyExecute + " (named ... apply) may confirm or claim an approval")

// executor checks that c declares packs.HumanOnlyExecute.
func executor(c *cobra.Command) error {
	if c == nil || c.Annotations[packs.AnnotationHumanOnly] != packs.HumanOnlyExecute {
		return ErrNotExecutor
	}
	return nil
}

// Confirm shows the exact command a human-run apply will run and asks for a
// typed "yes". c is the running pack command, which must declare
// packs.HumanOnlyExecute. It refuses unless in is a terminal (it checks
// itself: an apply never runs from a pipe), and in an agent session.
func Confirm(c *cobra.Command, in *os.File, out io.Writer, command string) error {
	if err := executor(c); err != nil {
		return err
	}
	if err := packs.HumanOnly(); err != nil {
		return err
	}
	if !isTerminal(in) {
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
