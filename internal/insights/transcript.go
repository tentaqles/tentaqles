package insights

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// maxLine bounds one transcript line. Longer lines (huge tool outputs) are
// skipped, so a single record never costs more than this much memory.
var maxLine = 32 << 20

// ContextLimit is the "session too big" threshold the plan targets.
const ContextLimit = 400_000

// Session classes.
const (
	ClassInteractive    = "interactive"
	ClassSecurityReview = "security_review"
	ClassProbe          = "probe"
	ClassSDKOther       = "sdk_other"
)

// FileStats is everything one transcript file contributes. It holds counts
// and ids only, never transcript content.
type FileStats struct {
	SessionID string
	Project   string
	Subagent  bool
	ParentID  string // for subagent files: the session they belong to

	Start, End  time.Time
	Class       string
	Model       string // most used model by assistant messages
	PeakContext int64
	CostUSD     float64
	Recorded    float64 // Claude Code's own cost-state figure, summed over runs started in the window
	Tokens      Tokens
	CostByTier  map[string]float64
	Messages    int // assistant messages (deduplicated)

	Shell ShellStats
	Guard GuardStats
	Hooks map[string][]float64 // hook event -> durations (ms)
}

// Tokens are summed token counts.
type Tokens struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

func (t *Tokens) add(o Tokens) {
	t.Input += o.Input
	t.Output += o.Output
	t.CacheRead += o.CacheRead
	t.CacheWrite += o.CacheWrite
}

// ToolStats are calls and failed results of one tool.
type ToolStats struct {
	Calls  int `json:"calls"`
	Errors int `json:"errors"`
}

// ShellStats is shell health.
type ShellStats struct {
	Bash             ToolStats `json:"bash"`
	PowerShell       ToolStats `json:"powershell"`
	HeredocEOF       int       `json:"heredoc_eof"`
	WorktreeRefusals int       `json:"worktree_refusals"`
	Exit127          int       `json:"exit_127"`
}

func (s *ShellStats) add(o ShellStats) {
	s.Bash.Calls += o.Bash.Calls
	s.Bash.Errors += o.Bash.Errors
	s.PowerShell.Calls += o.PowerShell.Calls
	s.PowerShell.Errors += o.PowerShell.Errors
	s.HeredocEOF += o.HeredocEOF
	s.WorktreeRefusals += o.WorktreeRefusals
	s.Exit127 += o.Exit127
}

// GuardStats counts tq hook outcomes seen in transcripts.
type GuardStats struct {
	Deny         map[string]int `json:"deny"`
	Ask          map[string]int `json:"ask"`
	StopBlocks   int            `json:"stop_blocks"`
	HookErrors   map[string]int `json:"hook_errors"`   // non-blocking hook failures by event
	HookTimeouts map[string]int `json:"hook_timeouts"` // cancelled hooks by event
}

func newGuard() GuardStats {
	return GuardStats{Deny: map[string]int{}, Ask: map[string]int{}, HookErrors: map[string]int{}, HookTimeouts: map[string]int{}}
}

func (g *GuardStats) add(o GuardStats) {
	for k, v := range o.Deny {
		g.Deny[k] += v
	}
	for k, v := range o.Ask {
		g.Ask[k] += v
	}
	for k, v := range o.HookErrors {
		g.HookErrors[k] += v
	}
	for k, v := range o.HookTimeouts {
		g.HookTimeouts[k] += v
	}
	g.StopBlocks += o.StopBlocks
}

// Rule ids look like "tq/env-file-read" or "jev/destructive-sql"; the
// pattern is strict so nothing but an id is ever captured.
var (
	reBlockedRule = regexp.MustCompile(`BLOCKED \[([a-z0-9_.-]+/[a-z0-9_.-]+)\]`)
	reConfirmRule = regexp.MustCompile(`CONFIRM \[([a-z0-9_.-]+/[a-z0-9_.-]+)\]`)
)

// identityRules maps the identity guard's "BLOCKED: <message>" texts (no
// rule id in the message) to its rule names.
var identityRules = []struct{ marker, rule string }{
	{"is blocked by client manifest", "blocked-command"},
	{"This workspace uses ", "wrong-cloud"},
	{"Cloud identity mismatch", "wrong-cloud"},
	{"cwd is not inside a truste", "neutral-remote"},
	{"is not trusted; git is refused", "untrusted"},
	{"shell identity (TQ_WS)", "env-drift"},
	{"CLAUDE_CONFIG_DIR drift", "claude-config-drift"},
	{"inline git identity", "git-email-drift"},
	{"Git email mismatch", "git-email-drift"},
	{"GitHub user mismatch", "gh-user"},
	{"tq is not installed", "tq-missing"},
}

// GuardRules extracts the deny rule ids from a failed tool result's text.
func GuardRules(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range reBlockedRule.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	if i := strings.Index(text, "BLOCKED: "); i >= 0 {
		rest := text[i:]
		rule := "identity/other"
		for _, r := range identityRules {
			if strings.Contains(rest, r.marker) {
				rule = "identity/" + r.rule
				break
			}
		}
		if !seen[rule] {
			out = append(out, rule)
		}
	}
	sort.Strings(out)
	return out
}

// AskRules extracts the ask rule ids from a PreToolUse hook's stdout.
func AskRules(stdout string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range reConfirmRule.FindAllStringSubmatch(stdout, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// ClassifyPrompt maps a session's first typed prompt to a class. Only a
// prefix check is made; the text is never kept.
func ClassifyPrompt(first, entrypoint string) string {
	p := strings.TrimSpace(first)
	switch {
	case strings.HasPrefix(p, "Review this change for security vulnerabilities"),
		strings.HasPrefix(p, "You previously flagged these candidate vulnerabilities"):
		return ClassSecurityReview
	case strings.HasPrefix(strings.ToLower(p), "reply with exactly"):
		return ClassProbe
	case strings.HasPrefix(entrypoint, "sdk"):
		return ClassSDKOther
	}
	return ClassInteractive
}

type record struct {
	Type        string `json:"type"`
	Subtype     string `json:"subtype"`
	Timestamp   string `json:"timestamp"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	IsCompact   bool   `json:"isCompactSummary"`
	Entrypoint  string `json:"entrypoint"`
	Message     *struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Usage   *Usage          `json:"usage"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Attachment *struct {
		Type       string   `json:"type"`
		HookEvent  string   `json:"hookEvent"`
		DurationMs *float64 `json:"durationMs"`
		Stdout     string   `json:"stdout"`
	} `json:"attachment"`
	TotalCostUSD float64 `json:"totalCostUSD"`
	StartTime    int64   `json:"startTime"`
}

type block struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Text      string          `json:"text"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

// resultText returns at most 16 KiB of a tool_result's text, for matching.
func resultText(raw json.RawMessage) string {
	const cap = 16 << 10
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if len(s) > cap {
			s = s[:cap]
		}
		return s
	}
	var parts []block
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
			b.WriteByte('\n')
		}
		if b.Len() > cap {
			break
		}
	}
	s = b.String()
	if len(s) > cap {
		s = s[:cap]
	}
	return s
}

// readLine reads one line, discarding (and reporting skip=true for) lines
// longer than maxLine.
func readLine(r *bufio.Reader, buf []byte) (line []byte, skip bool, err error) {
	buf = buf[:0]
	for {
		chunk, e := r.ReadSlice('\n')
		if len(buf)+len(chunk) <= maxLine {
			buf = append(buf, chunk...)
		} else {
			skip = true
		}
		if e == bufio.ErrBufferFull {
			continue
		}
		if e != nil && !(errors.Is(e, io.EOF) && len(buf) > 0) {
			return buf, skip, e
		}
		return buf, skip, nil
	}
}

// ScanTranscript streams one transcript, counting only records at or after
// since. It never keeps more than one line in memory, plus a map of
// message and tool-use ids.
func ScanTranscript(r io.Reader, since time.Time) (*FileStats, error) {
	sc := &scanner{
		fs:     &FileStats{CostByTier: map[string]float64{}, Guard: newGuard(), Hooks: map[string][]float64{}},
		since:  since,
		uses:   map[string]msgUse{},
		tools:  map[string]string{},
		models: map[string]int{},
		runs:   map[int64]float64{},
	}
	fs := sc.fs
	br := bufio.NewReaderSize(r, 1<<20)
	var buf []byte
	for {
		line, skip, err := readLine(br, buf)
		if len(line) > 0 && !skip {
			buf = line
			sc.consume(line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
	}
	for _, c := range sc.runs {
		fs.Recorded += c
	}
	for _, mu := range sc.uses {
		u := mu.u
		fs.Tokens.add(Tokens{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite})
		c := CostUSD(mu.model, u)
		fs.CostUSD += c
		if t := TierOf(mu.model); t != "" {
			fs.CostByTier[t] += c
		}
		fs.Messages++
	}
	best := 0
	for m, n := range sc.models {
		if n > best || (n == best && m < fs.Model) {
			fs.Model, best = m, n
		}
	}
	if fs.Class == "" {
		fs.Class = ClassifyPrompt("", sc.entry)
	}
	return fs, nil
}

// msgUse is the last usage seen for one message id.
type msgUse struct {
	model string
	u     Usage
}

// scanner is the per-file state of ScanTranscript.
type scanner struct {
	fs          *FileStats
	since       time.Time
	uses        map[string]msgUse // message id -> last usage (lines repeat per content block)
	anon        int
	tools       map[string]string // tool_use id -> tool name
	models      map[string]int
	firstPrompt bool
	entry       string
	runs        map[int64]float64 // cost-state startTime -> max totalCostUSD
}

func (sc *scanner) consume(line []byte) {
	fs, since := sc.fs, sc.since
	var rec record
	if json.Unmarshal(bytes.TrimSpace(line), &rec) != nil {
		return
	}
	if rec.Timestamp != "" {
		t, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
		if err == nil {
			if t.Before(since) {
				return
			}
			if fs.Start.IsZero() || t.Before(fs.Start) {
				fs.Start = t
			}
			if t.After(fs.End) {
				fs.End = t
			}
		}
	}
	if sc.entry == "" && rec.Entrypoint != "" {
		sc.entry = rec.Entrypoint
	}
	switch rec.Type {
	case "cost-state":
		// Claude Code restarts the counter when a session is resumed, so
		// each run (startTime) keeps its own maximum and runs are summed.
		if rec.StartTime > 0 && time.UnixMilli(rec.StartTime).Before(since) {
			return
		}
		if rec.TotalCostUSD > sc.runs[rec.StartTime] {
			sc.runs[rec.StartTime] = rec.TotalCostUSD
		}
	case "attachment":
		a := rec.Attachment
		if a == nil || !strings.HasPrefix(a.Type, "hook") {
			return
		}
		if a.DurationMs != nil && a.HookEvent != "" && (a.Type == "hook_success" || a.Type == "hook_non_blocking_error") {
			fs.Hooks[a.HookEvent] = append(fs.Hooks[a.HookEvent], *a.DurationMs)
		}
		switch a.Type {
		case "hook_blocking_error":
			if a.HookEvent == "Stop" || a.HookEvent == "SubagentStop" {
				fs.Guard.StopBlocks++
			}
		case "hook_non_blocking_error":
			fs.Guard.HookErrors[a.HookEvent]++
		case "hook_cancelled":
			fs.Guard.HookTimeouts[a.HookEvent]++
		case "hook_success":
			if a.HookEvent == "PreToolUse" && strings.Contains(a.Stdout, "CONFIRM [") {
				for _, id := range AskRules(a.Stdout) {
					fs.Guard.Ask[id]++
				}
			}
		}
	case "assistant":
		m := rec.Message
		if m == nil {
			return
		}
		if m.Model != "" && m.Model != "<synthetic>" {
			sc.models[m.Model]++
		}
		if m.Usage != nil && m.Model != "<synthetic>" {
			if !rec.IsSidechain {
				if c := m.Usage.Context(); c > fs.PeakContext {
					fs.PeakContext = c
				}
			}
			id := m.ID
			if id == "" {
				sc.anon++
				id = "anon-" + strconv.Itoa(sc.anon)
			}
			sc.uses[id] = msgUse{m.Model, *m.Usage}
		}
		var blocks []block
		if json.Unmarshal(m.Content, &blocks) != nil {
			return
		}
		for _, b := range blocks {
			if b.Type != "tool_use" {
				continue
			}
			sc.tools[b.ID] = b.Name
			switch b.Name {
			case "Bash":
				fs.Shell.Bash.Calls++
			case "PowerShell":
				fs.Shell.PowerShell.Calls++
			}
		}
	case "user":
		m := rec.Message
		if m == nil || len(m.Content) == 0 {
			return
		}
		var s string
		if json.Unmarshal(m.Content, &s) == nil {
			sc.notePrompt(s, rec)
			return
		}
		var blocks []block
		if json.Unmarshal(m.Content, &blocks) != nil {
			return
		}
		hasResult := false
		for _, b := range blocks {
			if b.Type != "tool_result" {
				continue
			}
			hasResult = true
			if !b.IsError {
				continue
			}
			fs.toolError(sc.tools[b.ToolUseID], resultText(b.Content))
		}
		if !hasResult {
			for _, b := range blocks {
				if b.Type == "text" {
					sc.notePrompt(b.Text, rec)
					break
				}
			}
		}
	}
}

func (sc *scanner) notePrompt(text string, rec record) {
	if sc.firstPrompt || rec.IsMeta || rec.IsSidechain || rec.IsCompact {
		return
	}
	t := strings.TrimSpace(text)
	if t == "" || strings.HasPrefix(t, "<") || strings.HasPrefix(t, "[Request interrupted") || strings.HasPrefix(t, "Caveat") {
		return
	}
	sc.firstPrompt = true
	entry := sc.entry
	if entry == "" {
		entry = rec.Entrypoint
	}
	sc.fs.Class = ClassifyPrompt(t, entry)
}

func (fs *FileStats) toolError(tool, text string) {
	switch tool {
	case "Bash":
		fs.Shell.Bash.Errors++
	case "PowerShell":
		fs.Shell.PowerShell.Errors++
	}
	if strings.Contains(text, "unexpected EOF while looking for matching") {
		fs.Shell.HeredocEOF++
	}
	if strings.Contains(text, "This agent is isolated in the worktree") {
		fs.Shell.WorktreeRefusals++
	}
	if strings.Contains(text, "Exit code 127") {
		fs.Shell.Exit127++
	}
	if strings.Contains(text, "BLOCKED") {
		for _, id := range GuardRules(text) {
			fs.Guard.Deny[id]++
		}
	}
}
