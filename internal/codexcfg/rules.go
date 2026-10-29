package codexcfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Codex exec-policy decisions (most restrictive wins: forbidden > prompt >
// allow).
const (
	DecisionPrompt    = "prompt"
	DecisionForbidden = "forbidden"
)

// RulesFile is tq's exec-policy file, relative to CODEX_HOME. A user's own
// rules/*.rules files are never touched.
const RulesFile = "rules/tq.rules"

const (
	rulesBegin = "# >>> managed by tq (guard mode): edit with tq mode, not by hand"
	rulesEnd   = "# <<< managed by tq (guard mode)"
)

// PrefixRule is one prefix_rule tq writes.
type PrefixRule struct {
	ID            string // the tq rule id the pattern stands for
	Decision      string // DecisionPrompt | DecisionForbidden
	Pattern       Pattern
	Justification string
}

// ascii keeps s on one line of printable ASCII, so the Go quoting below is
// also a valid Starlark string.
func ascii(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e {
			return ' '
		}
		return r
	}, s)
}

func renderPattern(p Pattern) string {
	parts := make([]string, len(p))
	for i, tok := range p {
		if len(tok) == 1 {
			parts[i] = strconv.Quote(tok[0])
			continue
		}
		alts := make([]string, len(tok))
		for j, w := range tok {
			alts[j] = strconv.Quote(w)
		}
		parts[i] = "[" + strings.Join(alts, ", ") + "]"
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// RenderRules returns the content of rules/tq.rules for mode.
func RenderRules(mode string, rules []PrefixRule) string {
	var b strings.Builder
	b.WriteString(rulesBegin + "\n")
	fmt.Fprintf(&b, "# tq guard mode: %s. tq writes this file; your own rules/*.rules stay yours.\n", ascii(mode))
	for _, r := range rules {
		just := ascii(r.ID)
		if r.Justification != "" {
			just += ": " + ascii(r.Justification)
		}
		fmt.Fprintf(&b, "prefix_rule(pattern = %s, decision = %q, justification = %s)\n", renderPattern(r.Pattern), r.Decision, strconv.Quote(just))
	}
	b.WriteString(rulesEnd + "\n")
	return b.String()
}

var ruleLineRe = regexp.MustCompile(`^prefix_rule\(pattern = (\[.*\]), decision = "(prompt|forbidden)", justification = ("(?:[^"\\]|\\.)*")\)$`)

// ParseRules reads a file RenderRules wrote. Anything else (no markers, a
// line tq would not write) is an error: the file is not tq's.
func ParseRules(s string) ([]PrefixRule, error) {
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(s, "\r\n", "\n"), "\n"), "\n")
	if len(lines) < 2 || lines[0] != rulesBegin || lines[len(lines)-1] != rulesEnd {
		return nil, errors.New("not tq's rules file (markers missing)")
	}
	var out []PrefixRule
	for _, l := range lines[1 : len(lines)-1] {
		if strings.HasPrefix(l, "#") || strings.TrimSpace(l) == "" {
			continue
		}
		m := ruleLineRe.FindStringSubmatch(l)
		if m == nil {
			return nil, fmt.Errorf("not a tq rule line: %q", l)
		}
		var raw []any
		if err := json.Unmarshal([]byte(m[1]), &raw); err != nil || len(raw) == 0 {
			return nil, fmt.Errorf("bad pattern: %q", m[1])
		}
		var p Pattern
		for _, e := range raw {
			switch v := e.(type) {
			case string:
				p = append(p, Token{v})
			case []any:
				var tok Token
				for _, a := range v {
					s, ok := a.(string)
					if !ok {
						return nil, fmt.Errorf("bad pattern: %q", m[1])
					}
					tok = append(tok, s)
				}
				if len(tok) == 0 {
					return nil, fmt.Errorf("bad pattern: %q", m[1])
				}
				p = append(p, tok)
			default:
				return nil, fmt.Errorf("bad pattern: %q", m[1])
			}
		}
		just, err := strconv.Unquote(m[3])
		if err != nil {
			return nil, fmt.Errorf("bad justification: %q", m[3])
		}
		id, rest, _ := strings.Cut(just, ": ")
		out = append(out, PrefixRule{ID: id, Decision: m[2], Pattern: p, Justification: rest})
	}
	return out, nil
}

// ReadRules reads and parses codexHome/rules/tq.rules. A missing or foreign
// file is an error.
func ReadRules(codexHome string) ([]PrefixRule, error) {
	b, err := os.ReadFile(filepath.Join(codexHome, filepath.FromSlash(RulesFile)))
	if err != nil {
		return nil, err
	}
	return ParseRules(string(b))
}

var plainWordRe = regexp.MustCompile(`^[A-Za-z0-9_./:=,@%+-]+$`)

var shellKeywords = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "fi": true, "for": true, "while": true,
	"until": true, "do": true, "done": true, "case": true, "esac": true, "function": true, "select": true,
}

// Segments splits command the way Codex's exec policy does for a shell
// script (spec E §12): a linear chain of plain words joined by &&, ||, ; or |
// is evaluated command by command. ok is false for anything else
// (redirection, substitution, variables, quotes, wildcards, control flow, a
// leading VAR=value): Codex then matches the whole script as one command,
// which no tq prefix matches. It errs on the side of "not simple".
func Segments(command string) (segs [][]string, ok bool) {
	if strings.ContainsAny(command, "\n\r") {
		return nil, false
	}
	var cur []string
	flush := func() bool {
		if len(cur) == 0 {
			return false
		}
		segs = append(segs, cur)
		cur = nil
		return true
	}
	for _, f := range strings.Fields(command) {
		switch f {
		case "&&", "||", ";", "|":
			if !flush() {
				return nil, false
			}
			continue
		}
		// A word glued to an operator ("a;" or "b&&c") is split too.
		for f != "" {
			i := strings.IndexAny(f, ";|&")
			word := f
			op := ""
			if i >= 0 {
				word = f[:i]
				rest := f[i:]
				switch {
				case strings.HasPrefix(rest, "&&"), strings.HasPrefix(rest, "||"):
					op = rest[:2]
				case rest[0] == ';' || rest[0] == '|':
					op = rest[:1]
				default:
					return nil, false // a lone & (background)
				}
				f = rest[len(op):]
			} else {
				f = ""
			}
			if word != "" {
				if !plainWordRe.MatchString(word) {
					return nil, false
				}
				if len(cur) == 0 && (strings.Contains(word, "=") || shellKeywords[word]) {
					return nil, false
				}
				cur = append(cur, word)
			}
			if op != "" && !flush() {
				return nil, false
			}
		}
	}
	if !flush() {
		return nil, false
	}
	return segs, true
}

// PromptCovers reports whether Codex will itself prompt for command because
// of rules (tq's own file): every id in ids must have one of tq's curated
// patterns that rules carries as a prompt and that one of command's segments
// matches. A pattern in rules that is not in tq's table never counts, so a
// hand-planted broad rule cannot make the hook let a call through.
func PromptCovers(rules []PrefixRule, ids []string, command string) bool {
	if len(ids) == 0 {
		return false
	}
	segs, ok := Segments(command)
	if !ok {
		return false
	}
	for _, id := range ids {
		covered := false
		for _, p := range prefixes[id] {
			inFile := false
			for _, r := range rules {
				if r.ID == id && r.Decision == DecisionPrompt && r.Pattern.equal(p) {
					inFile = true
					break
				}
			}
			if !inFile {
				continue
			}
			for _, s := range segs {
				if p.Match(s) {
					covered = true
					break
				}
			}
			if covered {
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}
