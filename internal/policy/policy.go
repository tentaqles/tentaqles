// Package policy decides whether one Claude Code tool call is allowed, needs
// the user's confirmation ("ask"), or is refused ("deny"), from declarative
// rules. It is pure: the caller parses the hook payload into a ToolCall and
// gathers every fact (rule sets, whether a file exists) first.
//
// Rules come in layers. Built-ins ship with tq; a trusted manifest
// (.tentaqles.yaml, hash-pinned by `tq allow`) may add rules and disable
// built-ins; a project file (.claude/tq-rules.yaml, repo content, therefore
// untrusted) may only ADD rules. Nothing a project ships can loosen the guard.
package policy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Action is a rule's verdict.
type Action string

const (
	Allow Action = ""
	Ask   Action = "ask"
	Deny  Action = "deny"
)

func (a Action) rank() int {
	switch a {
	case Deny:
		return 2
	case Ask:
		return 1
	}
	return 0
}

// ToolCall is the normalized view of one PreToolUse payload.
type ToolCall struct {
	Tool    string // tool_name as Claude Code sends it
	Command string // Bash / PowerShell command
	Path    string // file path (Read/Edit/Write/NotebookEdit/Grep), slash-normalized
	Content string // text being written (Write content, Edit new_string, …) or the raw MCP input
	Exists  bool   // Path exists on disk before the call
}

// IsShell reports whether the call runs a shell command.
func (c ToolCall) IsShell() bool { return c.Tool == "Bash" || c.Tool == "PowerShell" }

// IsMCP reports whether the call targets an MCP server tool.
func (c ToolCall) IsMCP() bool { return strings.HasPrefix(c.Tool, "mcp__") }

// MCPServer returns the server segment of an mcp__<server>__<tool> name.
func (c ToolCall) MCPServer() string {
	rest, ok := strings.CutPrefix(c.Tool, "mcp__")
	if !ok {
		return ""
	}
	server, _, _ := strings.Cut(rest, "__")
	return server
}

// NormalizePath turns a Windows path into the forward-slash form every path
// rule is written against.
func NormalizePath(p string) string {
	return strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
}

// Rule is one declarative check. Every non-empty matcher must match (AND).
// All regexes are case-insensitive. Except, when set, is cut out of every
// subject (command, path, content) before matching, so `.env.example` can be
// exempted without also exempting a `.env` in the same command.
type Rule struct {
	ID           string `yaml:"id"`
	Action       Action `yaml:"action"`
	Tool         string `yaml:"tool,omitempty"`    // regex on the tool name, anchored
	Command      string `yaml:"command,omitempty"` // regex on the shell command
	Path         string `yaml:"path,omitempty"`    // regex on the normalized path
	Content      string `yaml:"content,omitempty"` // regex on written content / MCP input
	Except       string `yaml:"except,omitempty"`
	ExistingOnly bool   `yaml:"existing_only,omitempty"` // only when Path already exists
	Reason       string `yaml:"reason"`
	Source       string `yaml:"-"` // "builtin", the manifest path, or the project rules path

	tool, command, path, content, except *regexp.Regexp
}

// Compile validates r and prepares its regexes.
func (r *Rule) Compile() error {
	if r.ID == "" {
		return fmt.Errorf("rule without id")
	}
	if r.Action != Ask && r.Action != Deny {
		return fmt.Errorf("rule %s: action must be ask or deny, got %q", r.ID, r.Action)
	}
	if r.Command == "" && r.Path == "" && r.Content == "" && r.Tool == "" {
		return fmt.Errorf("rule %s: needs at least one of tool, command, path, content", r.ID)
	}
	var err error
	comp := func(field, src string, anchored bool) *regexp.Regexp {
		if src == "" || err != nil {
			return nil
		}
		if anchored {
			src = "^(?:" + src + ")$"
		}
		re, e := regexp.Compile("(?i)" + src)
		if e != nil {
			err = fmt.Errorf("rule %s: bad %s regex: %w", r.ID, field, e)
		}
		return re
	}
	r.tool = comp("tool", r.Tool, true)
	r.command = comp("command", r.Command, false)
	r.path = comp("path", r.Path, false)
	r.content = comp("content", r.Content, false)
	r.except = comp("except", r.Except, false)
	return err
}

// Match reports whether r applies to call. Compile must have succeeded.
func (r *Rule) Match(call ToolCall) bool {
	if r.tool != nil && !r.tool.MatchString(call.Tool) {
		return false
	}
	if r.ExistingOnly && !call.Exists {
		return false
	}
	type subject struct {
		re   *regexp.Regexp
		text string
	}
	var subjects []subject
	if r.command != nil {
		if !call.IsShell() {
			return false
		}
		subjects = append(subjects, subject{r.command, call.Command})
	}
	if r.path != nil {
		subjects = append(subjects, subject{r.path, NormalizePath(call.Path)})
	}
	if r.content != nil {
		subjects = append(subjects, subject{r.content, call.Content})
	}
	for _, s := range subjects {
		text := s.text
		if r.except != nil {
			text = r.except.ReplaceAllString(text, " ")
		}
		if strings.TrimSpace(text) == "" || !s.re.MatchString(text) {
			return false
		}
	}
	return true
}

// Set is the effective, compiled rule list for one workspace.
type Set struct {
	Rules []*Rule
}

// Layers are the raw inputs to Build.
type Layers struct {
	Builtin  []Rule
	Manifest []Rule   // trusted: may add rules
	Disable  []string // trusted: built-in ids to switch off
	Project  []Rule   // untrusted: may only add rules
}

// Build compiles the layers into a Set. A rule that fails to compile is
// skipped and reported; the remaining rules still apply, so one typo in a
// project file never switches the whole guard off.
func Build(l Layers) (*Set, []error) {
	var errs []error
	off := map[string]bool{}
	for _, id := range l.Disable {
		off[strings.TrimSpace(id)] = true
	}
	s := &Set{}
	add := func(rs []Rule, src string, canDisable bool) {
		for i := range rs {
			r := rs[i]
			if r.Source == "" {
				r.Source = src
			}
			if canDisable && off[r.ID] {
				continue
			}
			if err := r.Compile(); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", r.Source, err))
				continue
			}
			s.Rules = append(s.Rules, &r)
		}
	}
	add(l.Builtin, "builtin", true)
	add(l.Manifest, "manifest", false)
	add(l.Project, "project", false)
	return s, errs
}

// Decision is the outcome for one call: the strictest matching rule wins,
// and every rule that matched at that level is listed.
type Decision struct {
	Action  Action
	Matched []*Rule
}

// Reason renders the decision for the hook's stderr / permissionDecisionReason.
func (d Decision) Reason() string {
	if d.Action == Allow {
		return ""
	}
	var b strings.Builder
	verb := "BLOCKED"
	if d.Action == Ask {
		verb = "CONFIRM"
	}
	for i, r := range d.Matched {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s [%s]: %s (source: %s)", verb, r.ID, r.Reason, r.Source)
	}
	return b.String()
}

// Evaluate applies every rule in s to call.
func (s *Set) Evaluate(call ToolCall) Decision {
	var d Decision
	for _, r := range s.Rules {
		if !r.Match(call) {
			continue
		}
		switch {
		case r.Action.rank() > d.Action.rank():
			d.Action = r.Action
			d.Matched = []*Rule{r}
		case r.Action.rank() == d.Action.rank():
			d.Matched = append(d.Matched, r)
		}
	}
	sort.SliceStable(d.Matched, func(i, j int) bool { return d.Matched[i].ID < d.Matched[j].ID })
	return d
}

// Merge combines two decisions, keeping the stricter one (both lists when tied).
func Merge(a, b Decision) Decision {
	switch {
	case a.Action.rank() > b.Action.rank():
		return a
	case b.Action.rank() > a.Action.rank():
		return b
	}
	return Decision{Action: a.Action, Matched: append(append([]*Rule{}, a.Matched...), b.Matched...)}
}
