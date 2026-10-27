package runner

import (
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

const relocatePrefix = "TQ_HUB_RELOCATE "

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type Relocate struct {
	Worktree string `json:"worktree"`
	Slug     string `json:"slug"`
}

func lastLine(text string) (string, int) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return strings.TrimSpace(lines[i]), i
		}
	}
	return "", -1
}

// ParseRelocate reads the hand-off line of spec §13.4 from the last non-empty line only.
func ParseRelocate(text string) (*Relocate, bool) {
	line, _ := lastLine(text)
	if !strings.HasPrefix(line, relocatePrefix) {
		return nil, false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, relocatePrefix))
	i := strings.LastIndexByte(rest, ' ')
	if i <= 0 {
		return nil, false
	}
	wt, slug := fromMSYS(strings.TrimSpace(rest[:i])), rest[i+1:]
	if !slugRe.MatchString(slug) || !filepath.IsAbs(wt) {
		return nil, false
	}
	return &Relocate{Worktree: filepath.Clean(wt), Slug: slug}, true
}

// fromMSYS turns Git Bash's /c/Users/... into C:/Users/... on Windows (the skill prints a native
// path, but a session that used pwd in Git Bash prints the MSYS form).
func fromMSYS(p string) string {
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == '/' && unicode.IsLetter(rune(p[1])) {
		return strings.ToUpper(p[1:2]) + ":" + p[2:]
	}
	return p
}

func StripRelocate(text string) string {
	if _, ok := ParseRelocate(text); !ok {
		return strings.TrimSpace(text)
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	_, idx := lastLine(text)
	return strings.TrimSpace(strings.Join(lines[:idx], "\n"))
}

// StateKey fingerprints what a round can change, to tell "state unchanged" from progress.
func StateKey(st *gate.State) string {
	if st == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "a=%v f=%v rb=%v", st.Approved, st.Finished, st.RolledBack)
	if st.Halted != nil {
		fmt.Fprintf(&b, " h=%s", st.Halted.Reason)
	}
	for _, c := range st.Checkpoints {
		gates := make([]string, 0, len(c.Gates))
		for k, v := range c.Gates {
			gates = append(gates, k+":"+v)
		}
		sort.Strings(gates)
		fmt.Fprintf(&b, " cp%s=%s/%s/%s", c.ID, c.Status, c.Evidence, strings.Join(gates, ","))
	}
	for _, d := range st.Decisions {
		fmt.Fprintf(&b, " d%s=%s", d.ID, d.Status)
	}
	for _, d := range st.PendingDecisions {
		fmt.Fprintf(&b, " p%s", d.ID)
	}
	if st.DoD != nil {
		fmt.Fprintf(&b, " dod=%v/%s", st.DoD.OK, st.DoD.Tree)
	}
	return b.String()
}

type Observation struct {
	ExitCode       int
	Subtype        string
	FinalText      string
	State          *gate.State // nil when the slug is unknown or the gate has no plan yet
	PrevStateKey   string
	Relocations    []string
	PermissionTool string
	Retried        int
	PreSlug        bool // the run has no build slug yet (no worktree, no gate record possible)
}

type Verdict string

const (
	VerdictRelocate     Verdict = "relocate"
	VerdictRelocateLoop Verdict = "relocate-loop"
	VerdictRolledBack   Verdict = "rolled-back"
	VerdictHalted       Verdict = "halted"
	VerdictWaiting      Verdict = "waiting"
	VerdictFinished     Verdict = "finished"
	VerdictPermission   Verdict = "permission"
	VerdictMaxTurns     Verdict = "max-turns"
	VerdictRetry        Verdict = "retry"
	VerdictExitFailed   Verdict = "exit-failed"
	VerdictNoStop       Verdict = "no-stop"
	VerdictUsageLimit   Verdict = "usage-limit"
	VerdictQuestion     Verdict = "question"
)

// usageLimitMax: a usage-limit stop is a short message the CLI prints in place of the agent's
// result; a longer final text is the agent's own summary, which may well discuss rate limits.
const usageLimitMax = 600

// usageLimitRe matches the Claude CLI's credit, usage and rate limit messages at the start of a line.
var usageLimitRe = regexp.MustCompile(`(?i)^(?:` +
	`you(?:'|’)re out of (?:usage )?credits` +
	`|(?:claude(?: ai)? )?usage limit (?:reached|exceeded)` +
	`|you(?:'|’)ve (?:hit|reached) your (?:\w+ )?limit` + // "your limit · resets 3pm", "your weekly limit"
	`|(?:(?:claude|opus|sonnet|haiku|fable) )?(?:\d+-hour|weekly|daily|session) limit reached` + // "Opus weekly limit reached ∙ resets …"
	`|rate limit (?:reached|exceeded)` +
	`|credit balance is too low` +
	`|api error: 429\b.*rate_limit_error` +
	`)`)

// UsageLimited reports whether a round's final text is a usage, credit or rate limit stop. It
// matches conservatively: the text is short and its last non-empty line is the limit message.
func UsageLimited(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > usageLimitMax {
		return false
	}
	line, _ := lastLine(text)
	return usageLimitRe.MatchString(line)
}

type Outcome struct {
	Verdict    Verdict
	Status     string
	StopReason string
	Relocate   *Relocate
}

// Classify is the outcome table of spec §8.4.
func Classify(o Observation) Outcome {
	if r, ok := ParseRelocate(o.FinalText); ok {
		if slices.Contains(o.Relocations, r.Slug) {
			return Outcome{Verdict: VerdictRelocateLoop, Status: store.StatusFailed, StopReason: "relocate loop", Relocate: r}
		}
		return Outcome{Verdict: VerdictRelocate, Status: store.StatusRunning, Relocate: r}
	}
	if st := o.State; st != nil {
		if st.RolledBack {
			return Outcome{Verdict: VerdictRolledBack, Status: store.StatusFailed, StopReason: "rolled back"}
		}
		if st.Halted != nil && strings.TrimSpace(st.Halted.Reason) != "" {
			return Outcome{Verdict: VerdictHalted, Status: store.StatusFailed, StopReason: st.Halted.Reason}
		}
		if len(st.PendingDecisions) > 0 {
			return Outcome{Verdict: VerdictWaiting, Status: store.StatusWaiting}
		}
		if st.Finished && ((st.DoD != nil && st.DoD.OK) || (st.DoD == nil && st.Workflow == "build")) {
			return Outcome{Verdict: VerdictFinished, Status: store.StatusFinished}
		}
	}
	if o.PermissionTool != "" {
		return Outcome{Verdict: VerdictPermission, Status: store.StatusWaiting, StopReason: "needs permission: " + o.PermissionTool}
	}
	if UsageLimited(o.FinalText) {
		return Outcome{Verdict: VerdictUsageLimit, Status: store.StatusWaiting, StopReason: "usage limit"}
	}
	if o.Subtype == "error_max_turns" {
		return Outcome{Verdict: VerdictMaxTurns, Status: store.StatusQueued}
	}
	if o.ExitCode != 0 && StateKey(o.State) == o.PrevStateKey {
		if o.Retried == 0 {
			return Outcome{Verdict: VerdictRetry, Status: store.StatusQueued}
		}
		return Outcome{Verdict: VerdictExitFailed, Status: store.StatusFailed, StopReason: fmt.Sprintf("exit %d", o.ExitCode)}
	}
	// A skill that needs the human before it has a build slug (feature, kickoff and fix step 1)
	// cannot write a gate record: in headless mode its final message is the question.
	if o.PreSlug && o.State == nil && o.ExitCode == 0 && (o.Subtype == "success" || o.Subtype == "exit 0") && strings.TrimSpace(o.FinalText) != "" {
		return Outcome{Verdict: VerdictQuestion, Status: store.StatusWaiting, StopReason: "question"}
	}
	return Outcome{Verdict: VerdictNoStop, Status: store.StatusFailed, StopReason: "ended without a stop"}
}
