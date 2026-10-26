package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tentaqles/tentaqles/internal/insights"
	"github.com/tentaqles/tentaqles/internal/redact"
)

// DefaultProgressEvery is the least time between two running cost lines in the run log.
const DefaultProgressEvery = 30 * time.Second

const (
	maxStreamLine = 16 << 20 // a longer line (a huge tool result) is skipped, not buffered
	clipText      = 200
)

// streamEvent is the part of a `--output-format stream-json` line the hub reads.
type streamEvent struct {
	Type            string  `json:"type"`
	Subtype         string  `json:"subtype"`
	SessionID       string  `json:"session_id"`
	Model           string  `json:"model"`
	ParentToolUseID *string `json:"parent_tool_use_id"`
	Message         *struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"` // a string on some user events
		Usage   *insights.Usage `json:"usage"`
	} `json:"message"`
}

type contentBlock struct {
	Type  string         `json:"type"`
	Text  string         `json:"text"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// stream reads claude's stream-json stdout as it arrives: it writes masked progress lines to the
// run log, adds up each assistant message's usage, and calls stop once that spend reaches limit.
type stream struct {
	mu      sync.Mutex
	log     io.Writer
	classes []string
	dir     string
	limit   Limit
	every   time.Duration
	now     func() time.Time
	stop    func()
	raw     *capped

	buf      []byte
	skipping bool

	sawEvent   bool
	result     []byte
	sessionID  string
	model      string
	msgs       map[string]insights.Usage
	anon       int
	usage      Usage
	cost       float64
	unpriced   bool
	lastReport time.Time
	over       string
	exitedRun  bool

	// The round's last words when the stream ends without a result event: the last top-level
	// assistant text, or the non-JSON stdout lines, whichever came last (a usage-limit message).
	lastText  string
	plain     []byte
	plainLast bool
}

// transcriptTail bounds the plain-output transcript, as ParseResult bounds unparseable output.
const transcriptTail = 4000

func newStream(log io.Writer, req RunRequest, every time.Duration, stop func()) *stream {
	if every <= 0 {
		every = DefaultProgressEvery
	}
	s := &stream{log: log, classes: req.Classes, dir: req.Dir, limit: req.Limit, every: every, now: time.Now, stop: stop,
		raw: &capped{max: 16 << 20}, msgs: map[string]insights.Usage{}}
	s.lastReport = s.now()
	return s
}

func (s *stream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(p)
	s.raw.Write(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			if !s.skipping {
				s.buf = append(s.buf, p...)
				if len(s.buf) > maxStreamLine {
					s.buf, s.skipping = nil, true
					s.line("tq hub: skipped an output line longer than 16 MiB")
				}
			}
			break
		}
		if !s.skipping {
			s.buf = append(s.buf, p[:i]...)
			s.handle(s.buf)
		}
		s.buf, s.skipping = s.buf[:0], false
		p = p[i+1:]
	}
	return n, nil
}

// flush handles a last line without a newline; call it once the process has exited.
func (s *stream) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buf) > 0 && !s.skipping {
		s.handle(s.buf)
	}
	s.buf = nil
}

// line writes one masked line to the run log.
func (s *stream) line(text string) {
	fmt.Fprintln(s.log, redact.Redact(text, s.classes))
}

func (s *stream) handle(raw []byte) {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return
	}
	var ev streamEvent
	if text[0] != '{' || json.Unmarshal([]byte(text), &ev) != nil || ev.Type == "" {
		s.line(clip(text, 4000)) // not an event: shown as claude printed it
		s.plain = tailBytes(append(s.plain, text+"\n"...), transcriptTail)
		s.plainLast = true
		return
	}
	s.sawEvent = true
	if ev.SessionID != "" {
		s.sessionID = ev.SessionID
	}
	switch ev.Type {
	case "system":
		if ev.Subtype == "init" {
			if ev.Model != "" {
				s.model = ev.Model
			}
			msg := "→ session started"
			if ev.Model != "" {
				msg += " (model " + ev.Model + ")"
			}
			s.line(msg)
		}
	case "assistant":
		s.assistant(ev)
	case "result":
		s.result = []byte(text)
		var d resultDoc
		if json.Unmarshal([]byte(text), &d) == nil {
			msg := "→ result " + d.Subtype
			if d.TotalCost != nil {
				msg += fmt.Sprintf(", $%.2f", *d.TotalCost)
			}
			s.line(msg + fmt.Sprintf(", %d turns", d.NumTurns))
		}
	}
}

func (s *stream) assistant(ev streamEvent) {
	if ev.Message == nil {
		return
	}
	indent := ""
	if ev.ParentToolUseID != nil && *ev.ParentToolUseID != "" {
		indent = "  " // a subagent's step
	}
	var blocks []contentBlock
	_ = json.Unmarshal(ev.Message.Content, &blocks)
	for _, b := range blocks {
		if l := s.describe(b); l != "" {
			s.line(indent + l)
		}
		if b.Type == "text" && indent == "" && strings.TrimSpace(b.Text) != "" {
			s.lastText, s.plainLast = strings.TrimSpace(b.Text), false
		}
	}
	if u := ev.Message.Usage; u != nil {
		model := ev.Message.Model
		if model == "" {
			model = s.model
		}
		s.account(ev.Message.ID, model, *u)
		if t := s.now(); t.Sub(s.lastReport) >= s.every {
			s.lastReport = t
			s.line(s.spent())
		}
		s.check()
	}
}

func (s *stream) describe(b contentBlock) string {
	str := func(k string) string { v, _ := b.Input[k].(string); return v }
	switch b.Type {
	case "text":
		first, _, _ := strings.Cut(strings.TrimSpace(b.Text), "\n")
		if first == "" {
			return ""
		}
		return "→ " + clip(first, clipText)
	case "tool_use":
		switch b.Name {
		case "Edit", "Write", "MultiEdit", "NotebookEdit":
			p := str("file_path")
			if p == "" {
				p = str("notebook_path")
			}
			return "→ editing " + s.rel(p)
		case "Task", "Agent":
			kind := str("subagent_type")
			if kind == "" {
				kind = "agent"
			}
			return "→ subagent " + kind + ": " + clip(firstLine(str("description")), clipText)
		}
		arg := ""
		for _, k := range []string{"command", "file_path", "pattern", "path", "url", "query", "skill", "description"} {
			if v := str(k); v != "" {
				arg = v
				if k == "file_path" || k == "path" {
					arg = s.rel(v)
				}
				break
			}
		}
		if arg == "" {
			return "→ tool " + b.Name
		}
		return "→ tool " + b.Name + ": " + clip(firstLine(arg), clipText)
	}
	return ""
}

// rel shows a path inside the round's folder relative to it.
func (s *stream) rel(p string) string {
	if p == "" || s.dir == "" || !filepath.IsAbs(p) {
		return p
	}
	if r, err := filepath.Rel(s.dir, p); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return p
}

// account adds one assistant event's usage. Claude repeats a message's usage on every content
// block of that message, so usage is kept per message id and only the growth is added.
func (s *stream) account(id, model string, u insights.Usage) {
	if id == "" {
		s.anon++
		id = fmt.Sprintf("#%d", s.anon)
	}
	old := s.msgs[id]
	nu := insights.Usage{Input: max(old.Input, u.Input), Output: max(old.Output, u.Output),
		CacheRead: max(old.CacheRead, u.CacheRead), CacheWrite: max(old.CacheWrite, u.CacheWrite)}
	s.msgs[id] = nu
	d := insights.Usage{Input: nu.Input - old.Input, Output: nu.Output - old.Output,
		CacheRead: nu.CacheRead - old.CacheRead, CacheWrite: nu.CacheWrite - old.CacheWrite}
	s.usage = Usage{s.usage.Input + d.Input, s.usage.Output + d.Output, s.usage.CacheRead + d.CacheRead, s.usage.CacheWrite + d.CacheWrite}
	if _, ok := insights.LookupPrice(model); ok {
		s.cost += insights.CostUSD(model, d)
	} else {
		s.unpriced = true
	}
}

func (s *stream) tokens() int64 { return s.usage.Input + s.usage.Output + s.usage.CacheWrite }

func (s *stream) spent() string {
	cost := fmt.Sprintf("$%.2f", s.cost)
	if s.unpriced {
		cost += " (some usage unpriced)"
	}
	return fmt.Sprintf("→ spent so far this round: %s, %d tokens over %d messages", cost, s.tokens(), len(s.msgs))
}

// check stops the round once its spend reaches what is left of the run's budget.
func (s *stream) check() {
	switch {
	case s.limit.Tokens > 0 && s.tokens() >= s.limit.Tokens:
		s.breach("tokens", fmt.Sprintf("%d tokens of the %d left", s.tokens(), s.limit.Tokens))
	case s.limit.USD > 0 && s.cost >= s.limit.USD:
		s.breach("usd", fmt.Sprintf("$%.2f of the $%.2f left", s.cost, s.limit.USD))
	}
}

// breach records the first limit reached and stops the process. Caller holds s.mu.
func (s *stream) breach(limit, detail string) {
	if s.over != "" {
		return
	}
	s.over = limit
	s.line(s.spent())
	s.line("tq hub: stopping the round inside it: it reached the run's " + limit + " budget (" + detail + ")")
	if s.stop != nil {
		s.stop()
	}
}

// breachMinutes is the minutes timer firing. It counts only while the process runs: a timer that
// fires after cmd.Run returned would turn a finished round into an over-budget one.
func (s *stream) breachMinutes(minutes float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exitedRun {
		return
	}
	s.breach("minutes", fmt.Sprintf("%.1f minutes left", minutes))
}

// exited marks the process as ended; call it as soon as cmd.Run returns.
func (s *stream) exited() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exitedRun = true
}

// outcome is the round's result: the stream's result event when there is one, else the old
// single-document parse of the whole output. A round stopped at its budget, or one whose stream
// ended without a result, keeps the spend and session the stream showed. clean: the process
// exited 0; a budget stop that came too late to cut a round which then exited 0 with its result
// does not count, the finished work is classified as usual (the spend still counts between rounds).
func (s *stream) outcome(clean bool) RunResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	over := s.over
	if over != "" && clean && s.result != nil {
		s.line("tq hub: the round finished before the stop took effect; its result is kept")
		over = ""
	}
	var res RunResult
	switch {
	case s.result != nil:
		res = ParseResult(s.result)
	case s.sawEvent:
		res = RunResult{StopReason: "unparseable output", SessionID: s.sessionID, Model: s.model, Usage: s.usage,
			UsageKnown: len(s.msgs) > 0, CostUSD: s.cost, CostKnown: len(s.msgs) > 0 && !s.unpriced}
		if s.plainLast || s.lastText == "" {
			res.Transcript = string(s.plain)
		} else {
			res.Transcript = string(tailBytes([]byte(s.lastText), transcriptTail))
		}
		if over != "" {
			res.StopReason = "over budget: " + over
		}
	default:
		res = ParseResult(s.raw.b.Bytes())
	}
	if res.SessionID == "" {
		res.SessionID = s.sessionID
	}
	res.OverBudget = over
	return res
}

// tailBytes keeps at most the last n bytes of b, cut at a rune boundary.
func tailBytes(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	b = b[len(b)-n:]
	for len(b) > 0 && !utf8.RuneStart(b[0]) {
		b = b[1:]
	}
	return append([]byte(nil), b...)
}

func firstLine(s string) string {
	f, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return f
}

// clip shortens s to at most n runes, marking the cut.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}
