package codexcfg

import (
	"regexp"
	"strings"
	"testing"
)

func sampleRules() []PrefixRule {
	return []PrefixRule{
		{ID: "tq/gh-repo-delete", Decision: DecisionForbidden, Pattern: Pattern{{"gh"}, {"repo"}, {"delete"}}, Justification: `deleting a "repo"`},
		{ID: "tq/iac-apply", Decision: DecisionPrompt, Pattern: Pattern{{"terraform", "tofu"}, {"apply"}}, Justification: "applying infrastructure"},
	}
}

func TestRenderRulesRoundTrip(t *testing.T) {
	s := RenderRules("default", sampleRules())
	if !strings.HasPrefix(s, rulesBegin+"\n") || !strings.HasSuffix(s, rulesEnd+"\n") {
		t.Fatalf("markers:\n%s", s)
	}
	checkStarlark(t, s)
	got, err := ParseRules(s)
	if err != nil {
		t.Fatal(err)
	}
	want := sampleRules()
	if len(got) != len(want) {
		t.Fatalf("got %d rules:\n%s", len(got), s)
	}
	for i := range want {
		if got[i].Decision != want[i].Decision || !got[i].Pattern.equal(want[i].Pattern) || got[i].ID != want[i].ID {
			t.Errorf("rule %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestParseRulesRejectsForeignFiles(t *testing.T) {
	for _, s := range []string{
		"",
		"prefix_rule(pattern = [\"git\"], decision = \"prompt\")\n",
		rulesBegin + "\nprefix_rule(pattern = [\"git\"], decision = \"prompt\")\n", // no end marker
	} {
		if _, err := ParseRules(s); err == nil {
			t.Errorf("must refuse:\n%s", s)
		}
	}
	// A hand-added line in tq's file is not one of tq's rules.
	s := strings.Replace(RenderRules("default", sampleRules()), rulesEnd, "prefix_rule(pattern = [\"git\"], decision = \"prompt\")\n"+rulesEnd, 1)
	if _, err := ParseRules(s); err == nil {
		t.Error("an unparseable line must make the file foreign")
	}
}

func TestSegments(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		want string // segments joined by " | ", words by " "; "!" = not simple
	}{
		{"terraform apply", "terraform apply"},
		{"  terraform   apply -auto-approve ", "terraform apply -auto-approve"},
		{"cd infra && terraform apply", "cd infra | terraform apply"},
		{"a || b; c | d", "a | b | c | d"},
		{"terraform apply > out.txt", "!"},
		{"terraform apply $(cat x)", "!"},
		{"FOO=1 terraform apply", "!"},
		{"terraform apply 'x'", "!"},
		{"terraform apply \"x\"", "!"},
		{"terraform apply *", "!"},
		{"terraform apply &", "!"},
		{"if true; then terraform apply; fi", "!"},
		{"terraform apply\nls", "!"},
		{"echo `id`", "!"},
		{"terraform apply ~/x", "!"},
		{"a && && b", "!"},
		{"", "!"},
	} {
		segs, ok := Segments(tc.cmd)
		got := "!"
		if ok {
			var parts []string
			for _, s := range segs {
				parts = append(parts, strings.Join(s, " "))
			}
			got = strings.Join(parts, " | ")
		}
		if got != tc.want {
			t.Errorf("%q: got %q want %q", tc.cmd, got, tc.want)
		}
	}
}

func TestPromptCovers(t *testing.T) {
	rules := []PrefixRule{
		{ID: "tq/iac-apply", Decision: DecisionPrompt, Pattern: Prefixes("tq/iac-apply")[0]},
		{ID: "tq/gh-repo-delete", Decision: DecisionForbidden, Pattern: Prefixes("tq/gh-repo-delete")[0]},
		// A planted pattern that is not in tq's table never counts.
		{ID: "tq/force-push", Decision: DecisionPrompt, Pattern: Pattern{{"git"}}},
	}
	for _, tc := range []struct {
		cmd  string
		ids  []string
		want bool
	}{
		{"terraform apply", []string{"tq/iac-apply"}, true},
		{"cd x && terraform apply -auto-approve", []string{"tq/iac-apply"}, true},
		{"terraform -chdir=x apply", []string{"tq/iac-apply"}, false}, // no prefix form: Codex would not prompt
		{"terraform apply > log", []string{"tq/iac-apply"}, false},    // not split by Codex
		{"terraform apply", []string{"tq/iac-apply", "tq/cloud-delete"}, false},
		{"gh repo delete x", []string{"tq/gh-repo-delete"}, false}, // forbidden, not prompt
		{"git push --force x", []string{"tq/force-push"}, false},   // planted pattern
		{"terraform apply", nil, false},
		{"kubectl apply -f x", []string{"tq/iac-apply"}, false}, // kubectl pattern is not in the file
	} {
		if got := PromptCovers(rules, tc.ids, tc.cmd); got != tc.want {
			t.Errorf("%q %v: got %v", tc.cmd, tc.ids, got)
		}
	}
}

// checkStarlark parses s with a small Starlark subset parser: comments, and
// prefix_rule(...) calls whose keyword arguments are strings or (nested)
// lists of strings. It fails on anything else, so Codex would load the file.
func checkStarlark(t *testing.T, s string) {
	t.Helper()
	p := &starlark{s: s}
	for {
		p.skip()
		if p.eof() {
			return
		}
		if !p.call(t) {
			t.Fatalf("not a prefix_rule call at %d:\n%s", p.i, s[p.i:])
		}
	}
}

type starlark struct {
	s string
	i int
}

func (p *starlark) eof() bool { return p.i >= len(p.s) }

func (p *starlark) skip() {
	for !p.eof() {
		switch c := p.s[p.i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			p.i++
		case c == '#':
			for !p.eof() && p.s[p.i] != '\n' {
				p.i++
			}
		default:
			return
		}
	}
}

func (p *starlark) lit(x string) bool {
	p.skip()
	if strings.HasPrefix(p.s[p.i:], x) {
		p.i += len(x)
		return true
	}
	return false
}

var identRe = regexp.MustCompile(`^[a-z_]+`)

func (p *starlark) call(t *testing.T) bool {
	if !p.lit("prefix_rule") || !p.lit("(") {
		return false
	}
	seen := map[string]bool{}
	for {
		if p.lit(")") {
			if !seen["pattern"] {
				t.Fatal("prefix_rule without pattern")
			}
			return true
		}
		p.skip()
		name := identRe.FindString(p.s[p.i:])
		switch name {
		case "pattern", "decision", "justification", "match", "not_match":
		default:
			t.Fatalf("unknown argument %q", name)
		}
		if seen[name] {
			t.Fatalf("duplicate argument %q", name)
		}
		seen[name] = true
		p.i += len(name)
		if !p.lit("=") {
			t.Fatal("missing =")
		}
		v := p.value(t)
		if name == "decision" {
			if v != `"allow"` && v != `"prompt"` && v != `"forbidden"` {
				t.Fatalf("bad decision %s", v)
			}
		}
		if !p.lit(",") {
			if !p.lit(")") {
				t.Fatalf("expected , or ) at %d", p.i)
			}
			return true
		}
	}
}

func (p *starlark) value(t *testing.T) string {
	p.skip()
	start := p.i
	switch {
	case p.lit("["):
		n := 0
		for !p.lit("]") {
			p.value(t)
			n++
			if !p.lit(",") {
				if !p.lit("]") {
					t.Fatalf("bad list at %d", p.i)
				}
				break
			}
		}
		if n == 0 {
			t.Fatal("empty list")
		}
	case p.lit(`"`):
		for {
			if p.eof() || p.s[p.i] == '\n' {
				t.Fatal("unterminated string")
			}
			c := p.s[p.i]
			p.i++
			if c == '\\' {
				p.i++
				continue
			}
			if c == '"' {
				break
			}
		}
	default:
		t.Fatalf("bad value at %d: %q", p.i, p.s[p.i:min(len(p.s), p.i+20)])
	}
	return p.s[start:p.i]
}
