package decide

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// JudgeRule is a judgment rule: a question Jev answers about one tool call,
// asked only when its cheap regex pre-filter matches. Every rule is a
// yes-means-risky noul question.
type JudgeRule struct {
	ID string `yaml:"id"`
	// Tool is an anchored regex on the tool name.
	Tool string `yaml:"tool"`
	// Path, Command and Content are regex pre-filters (case-insensitive);
	// every non-empty one must match. They keep Jev off the hot path for
	// the vast majority of calls.
	Path     string `yaml:"path,omitempty"`
	Command  string `yaml:"command,omitempty"`
	Content  string `yaml:"content,omitempty"`
	Question string `yaml:"question"`
	// Max caps what the rule can do in enforce mode: "ask" (the default)
	// or "deny". Jev is mid-tier on accuracy; most rules should only ask.
	Max    Action `yaml:"max,omitempty"`
	Reason string `yaml:"reason,omitempty"`

	tool, path, command, content *regexp.Regexp
}

// Subject is what a judgment rule looks at: the normalized tool call.
type Subject struct {
	Tool, Path, Command, Content string
}

func (s Subject) state() map[string]string {
	m := map[string]string{"tool": s.Tool}
	if s.Path != "" {
		m["path"] = s.Path
	}
	if s.Command != "" {
		m["command"] = s.Command
	}
	if s.Content != "" {
		m["content"] = s.Content
	}
	return m
}

const editTools = `Edit|Write|MultiEdit|NotebookEdit`

// BuiltinJudgeRules are tq's judgment rules. Each family has a seed eval set
// in docs/decide/seed-cases.jsonl; none may be enforced before its family
// clears `tq decide eval` at precision >= 0.9 on ~100 cases.
func BuiltinJudgeRules() []JudgeRule {
	return []JudgeRule{
		{
			ID: "jev/destructive-sql", Tool: editTools + `|Bash|PowerShell|mcp__.*`,
			Content:  `\b(drop\s+(table|schema|database|column|view|index)|truncate\s+(table\s+)?\w|delete\s+from|alter\s+table\s+\S+\s+drop)\b`,
			Question: "Does this change permanently destroy existing data (drop, truncate, or delete rows without a backup or migration path)?",
			Reason:   "Jev judged this as destroying data",
		},
		{
			ID: "jev/rls-weakened", Tool: editTools + `|mcp__.*`,
			Content:  `\b(row\s+level\s+security|create\s+policy|alter\s+policy|grant\s+\w+\s+on|using\s*\(\s*true\s*\))`,
			Question: "Does this change weaken row-level security or let a user read or write rows that belong to other users?",
			Reason:   "Jev judged this as weakening row-level security",
		},
		{
			ID: "jev/service-role-client", Tool: editTools,
			Path:     `\.(tsx?|jsx?|vue|svelte|astro)$`,
			Content:  `service[_-]?role|secret[_-]?key|admin[_-]?(key|token)`,
			Question: "Does this code expose a privileged server credential (service role key, admin token) to browser or client-side code?",
			Reason:   "Jev judged this as exposing a server credential to client code",
		},
		{
			// Live n8n writes only: reads and exports are not judged.
			ID: "jev/n8n-inline-credentials", Tool: `mcp__.*n8n.*__(?:\w*?_)?(create|update|publish|restore|execute|test)\w*`,
			Content:  `authorization|bearer|api[_-]?key|password|token|secret|connectionString`,
			Question: "Does this n8n node embed a credential directly in its parameters instead of using an n8n credential reference?",
			Reason:   "Jev judged this n8n change as inlining a credential",
		},
	}
}

// compile prepares the regexes; a broken rule is reported, never matched.
func (r *JudgeRule) compile() error {
	c := func(expr string, anchored bool) (*regexp.Regexp, error) {
		if expr == "" {
			return nil, nil
		}
		if anchored {
			expr = `^(?:` + expr + `)$`
		}
		return regexp.Compile(`(?i)` + expr)
	}
	var err error
	if r.ID == "" || r.Question == "" || r.Tool == "" {
		return fmt.Errorf("judge rule %q: id, tool and question are required", r.ID)
	}
	if r.Max != "" && r.Max != Ask && r.Max != Deny {
		return fmt.Errorf("judge rule %s: max must be ask or deny", r.ID)
	}
	if r.tool, err = c(r.Tool, true); err != nil {
		return fmt.Errorf("judge rule %s tool: %w", r.ID, err)
	}
	if r.path, err = c(r.Path, false); err != nil {
		return fmt.Errorf("judge rule %s path: %w", r.ID, err)
	}
	if r.command, err = c(r.Command, false); err != nil {
		return fmt.Errorf("judge rule %s command: %w", r.ID, err)
	}
	if r.content, err = c(r.Content, false); err != nil {
		return fmt.Errorf("judge rule %s content: %w", r.ID, err)
	}
	return nil
}

func (r *JudgeRule) matches(s Subject) bool {
	if !r.tool.MatchString(s.Tool) {
		return false
	}
	if r.path != nil && !r.path.MatchString(s.Path) {
		return false
	}
	if r.command != nil && !r.command.MatchString(s.Command) {
		return false
	}
	// Content rules also look at the command, so a psql -c "DROP ..." in a
	// Bash call is judged like the same SQL in an edit.
	if r.content != nil && !r.content.MatchString(s.Content) && !r.content.MatchString(s.Command) {
		return false
	}
	return true
}

// JudgeSet is the compiled rule set for one workspace.
type JudgeSet struct {
	Rules  []*JudgeRule
	Errors []error
}

// BuildJudgeSet layers built-ins, then manifest additions, minus disabled ids.
func BuildJudgeSet(extra []JudgeRule, disable []string) JudgeSet {
	off := map[string]bool{}
	for _, id := range disable {
		off[strings.TrimSpace(id)] = true
	}
	var set JudgeSet
	for _, r := range append(BuiltinJudgeRules(), extra...) {
		if off[r.ID] {
			continue
		}
		r := r
		if err := r.compile(); err != nil {
			set.Errors = append(set.Errors, err)
			continue
		}
		set.Rules = append(set.Rules, &r)
	}
	return set
}

// Candidates returns the rules whose pre-filter matches s.
func (s JudgeSet) Candidates(sub Subject) []*JudgeRule {
	var out []*JudgeRule
	for _, r := range s.Rules {
		if r.matches(sub) {
			out = append(out, r)
		}
	}
	return out
}

// Verdict is one rule's outcome for one call.
type Verdict struct {
	Rule       *JudgeRule
	Escalation Escalation
}

// Judge asks Jev every candidate rule in one batched call. It never returns
// an error: a failed call yields verdicts whose Escalation carries the
// error and applies nothing.
func Judge(ctx context.Context, c *Client, p Policy, sub Subject, rules []*JudgeRule) []Verdict {
	if len(rules) == 0 {
		return nil
	}
	qs := make(map[string]Question, len(rules))
	for _, r := range rules {
		qs[r.ID] = Noul(r.Question)
	}
	resp, err := c.Ask(ctx, sub.state(), qs)
	out := make([]Verdict, 0, len(rules))
	for _, r := range rules {
		var prob float64
		e := err
		if e == nil {
			prob, e = resp.NoulOf(r.ID)
		}
		esc := Escalate(p, prob, e)
		max := r.Max
		if max == "" {
			max = Ask
		}
		if esc.Apply == Deny && max == Ask {
			esc.Apply = Ask
		}
		out = append(out, Verdict{Rule: r, Escalation: esc})
	}
	return out
}

// Breaker skips Jev for a cool-down after a failure, so an outage costs one
// deadline, not one per tool call. State is a file holding the time of the
// last failure.
type Breaker struct {
	Path     string
	CoolDown time.Duration
}

// Open reports whether calls should be skipped right now.
func (b Breaker) Open() bool {
	fi, err := os.Stat(b.Path)
	return err == nil && time.Since(fi.ModTime()) < b.CoolDown
}

// Trip records a failure now.
func (b Breaker) Trip() {
	_ = os.MkdirAll(filepath.Dir(b.Path), 0o700)
	_ = os.WriteFile(b.Path, []byte(time.Now().UTC().Format(time.RFC3339)), 0o600)
}

// Reset clears a recorded failure.
func (b Breaker) Reset() { _ = os.Remove(b.Path) }

// JudgmentLog records what Jev judged per tool call: rule ids,
// probabilities, what it would do and what was applied. Never the content.
type JudgmentLog struct {
	Path string
	mu   sync.Mutex
}

// Judgment is one log line.
type Judgment struct {
	Time      time.Time          `json:"ts"`
	Workspace string             `json:"ws,omitempty"`
	Kind      string             `json:"kind"` // guard | route | triage | stop | skill
	Mode      string             `json:"mode"`
	Tool      string             `json:"tool,omitempty"`
	Path      string             `json:"path,omitempty"`
	P         map[string]float64 `json:"p,omitempty"`
	Would     map[string]string  `json:"would,omitempty"`
	Applied   map[string]string  `json:"applied,omitempty"`
	Error     string             `json:"error,omitempty"`
	Extra     map[string]string  `json:"extra,omitempty"`
}

// Write appends one line; failures are ignored (logging never blocks).
func (l *JudgmentLog) Write(j Judgment) {
	if l == nil {
		return
	}
	if j.Time.IsZero() {
		j.Time = time.Now().UTC()
	}
	line, err := json.Marshal(j)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = os.MkdirAll(filepath.Dir(l.Path), 0o700)
	f, err := os.OpenFile(l.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// VerdictsJudgment summarises verdicts for the log.
func VerdictsJudgment(ws, mode string, sub Subject, vs []Verdict) Judgment {
	j := Judgment{Workspace: ws, Kind: "guard", Mode: mode, Tool: sub.Tool, Path: sub.Path,
		P: map[string]float64{}, Would: map[string]string{}, Applied: map[string]string{}}
	var errs []string
	for _, v := range vs {
		id := v.Rule.ID
		if v.Escalation.Err != nil {
			errs = append(errs, v.Escalation.Err.Error())
			continue
		}
		j.P[id] = v.Escalation.P
		if v.Escalation.Would != None {
			j.Would[id] = string(v.Escalation.Would)
		}
		if v.Escalation.Apply != None {
			j.Applied[id] = string(v.Escalation.Apply)
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		j.Error = errs[0]
	}
	return j
}
