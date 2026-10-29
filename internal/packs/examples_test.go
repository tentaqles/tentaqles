package packs

// Example checks, flags, hints, guards and commands registered only in this
// test binary: they prove the framework end to end without any real pack.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"github.com/tentaqles/tentaqles/internal/policy"
	"gopkg.in/yaml.v3"
)

func init() {
	RegisterPack(Pack{
		Name: "data",
		ConfigKeys: map[string]func(yaml.Node) error{
			"example_paths": func(n yaml.Node) error {
				if n.Kind != yaml.SequenceNode {
					return errors.New("must be a list")
				}
				return nil
			},
			"example_cap":     nil,
			"example_floor":   nil,
			"example_command": nil,
			"example_secret":  nil,
			"example_block":   nil,
			"example_extra":   nil,
			"x":               nil,
		},
		Merge: map[string]MergeRule{
			"example_paths":          Union(),
			"example_cap":            {Kind: MergeMin, Default: "50GB", ZeroUnlimited: true},
			"example_floor":          Max("0.5"),
			"example_command":        RepoValue(),
			"example_extra":          TrustedAdd(),
			"example_block.*.limit":  Min("100"),
			"example_block.*.strict": Or(),
			"example_block.*.apply":  Ordered("ask", "auto", "ask", "approve", "deny"),
		},
		Flags: []Flag{
			{Name: "example_sql", Reviewers: []string{"pipeline-reviewer"}, Raise: func(in *Input) bool { return in.AnyChanged("*.sql") }},
			{Name: "example_question", Reviewers: []string{"pipeline-reviewer"}, Question: "Does this change alter an example pipeline?"},
			{Name: "migration", Reviewers: []string{"db-reviewer"}},
			{Name: FlagBaselineChange, Reviewers: []string{"pipeline-reviewer"}},
		},
		Hints: []Hint{{Glob: "dbt_project.yml", Why: "a dbt project"}},
		Doctor: func(in *Input) []doctor.Finding {
			return []doctor.Finding{{Level: "warn", Code: "data-example", Msg: "example probe for " + in.Pack()}}
		},
		Commands: func() []*cobra.Command {
			return []*cobra.Command{{Use: "example", RunE: func(c *cobra.Command, _ []string) error { c.Println("example ran"); return nil }}}
		},
	})
	RegisterPack(Pack{Name: "ml", Hints: []Hint{{Glob: "*.py", Contains: `from sklearn`, Why: "scikit-learn code"}}})
	Register(Check{ID: "data/example-select-star", Pack: "data", Kind: Builtin,
		Applies: func(in *Input) bool { return in.AnyChanged("*.sql") },
		Run: func(ctx context.Context, in *Input) Result {
			var fs []Finding
			n := 0
			for _, p := range in.ChangedPaths() {
				if !MatchGlob("*.sql", p) {
					continue
				}
				n++
				b, err := in.ReadNew(p, 0)
				if err != nil {
					return Result{Status: Fail, Detail: "cannot read " + p}
				}
				for _, st := range scan.SQLStatements(b) {
					for _, l := range st.SelectStars() {
						fs = append(fs, Finding{File: p, Line: l, Level: LevelError, Rule: "SQL001", Message: "SELECT * in a model"})
					}
				}
			}
			return Result{Status: OK, Detail: plural(n, "SQL file"), Findings: fs}
		}})
	Register(Check{ID: "data/example-warn", Pack: "data", Kind: Builtin,
		Applies: func(in *Input) bool { return in.AnyChanged("*.sql") },
		Run: func(ctx context.Context, in *Input) Result {
			return Result{Status: Fail, Detail: "style notes", Findings: []Finding{{File: "models/orders.sql", Line: 1, Level: LevelWarn, Rule: "STY01", Message: "lower-case keywords"}}}
		}})
	Register(Check{ID: "data/example-panic", Pack: "data", Kind: Builtin,
		Applies: func(in *Input) bool { return in.AnyChanged("panic.marker") },
		Run:     func(ctx context.Context, in *Input) Result { panic("boom") }})
	Register(Check{ID: "data/example-slow", Pack: "data", Kind: Builtin,
		Applies: func(in *Input) bool { return in.AnyChanged("slow.marker") },
		Run: func(ctx context.Context, in *Input) Result {
			<-ctx.Done()
			return Result{Status: OK}
		}})
	Register(Check{ID: "data/example-sqlfluff", Pack: "data", Kind: External, Tool: "sqlfluff", Timeout: 300 * time.Second,
		Applies: func(in *Input) bool { return in.AnyChanged("*.sql") },
		Run: func(ctx context.Context, in *Input) Result {
			out, err := in.Tools.Run(ctx, toolrun.Spec{Pack: "data", Tool: "sqlfluff", Args: []string{"lint", "--format", "json"}})
			if err != nil {
				return Result{Status: Fail, Detail: err.Error()}
			}
			r := Result{Status: OK, Detail: "1 file linted", Tool: &ToolRun{Name: "sqlfluff", Version: out.Version, ExitCode: out.ExitCode, DurationMS: out.Duration.Milliseconds()}}
			for _, l := range strings.Split(strings.TrimSpace(string(out.Stdout)), "\n") {
				if l != "" {
					r.Findings = append(r.Findings, Finding{File: "models/orders.sql", Line: 2, Level: LevelError, Rule: "LT01", Message: l})
				}
			}
			return r
		}})
	Register(Check{ID: "ml/example-ruff", Pack: "ml", Kind: External, Tool: "ruff", MinVersion: "0.5.0",
		Run: func(ctx context.Context, in *Input) Result { return Result{Status: OK, Detail: "ruff clean"} }})
	Register(Check{ID: "ml/example-dvc", Pack: "ml", Kind: External, Tool: "dvc",
		Run: func(ctx context.Context, in *Input) Result { return Result{Status: OK} }})

	RegisterGuard(Guard{ID: "tq/example-bq", Pack: "data", Events: []string{"Bash", "PowerShell"}, Supersedes: []string{"tq/iac-apply"},
		Check: func(call policy.ToolCall, g GuardCtx) (policy.Action, string, bool) {
			c := call.Command
			switch {
			case strings.Contains(c, "guard-panic"):
				panic("guard exploded")
			case strings.Contains(c, "guard-slow"):
				time.Sleep(300 * time.Millisecond)
				return policy.Allow, "", false
			case strings.Contains(c, "guard-consume"):
				g.Consume("d-example")
				g.Count("apply_allowed:approve", 1)
				return policy.Allow, "approved", true
			case strings.Contains(c, "guard-ask-consume"):
				g.Consume("d-ask")
				return policy.Ask, "confirm", true
			case bqUnbounded.MatchString(c):
				return policy.Deny, "bq query needs --maximum_bytes_billed", true
			}
			return policy.Allow, "", false
		}})
	RegisterGuard(Guard{ID: "tq/example-mcp", Pack: "ml", Events: []string{`mcp__.*__promote`},
		Check: func(call policy.ToolCall, g GuardCtx) (policy.Action, string, bool) {
			return policy.Deny, "promotion through MCP", true
		}})
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return strings.TrimSpace(strings.Join([]string{itoa(n), what + "s"}, " "))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// fakeTools is a ToolRunner over canned probes and outputs.
type fakeTools struct {
	probes map[string]toolrun.Probe
	outs   map[string]toolrun.Output
	slow   map[string]bool
	seen   map[string]time.Duration // tool -> Spec.Timeout it was run with
	probed map[string]int
}

func (f fakeTools) Probe(pack, tool string) toolrun.Probe {
	if f.probed != nil {
		f.probed[tool]++
	}
	if p, ok := f.probes[tool]; ok {
		return p
	}
	return toolrun.Probe{Tool: tool}
}

func (f fakeTools) Run(ctx context.Context, s toolrun.Spec) (toolrun.Output, error) {
	if f.seen != nil {
		f.seen[s.Tool] = s.Timeout
	}
	if f.slow[s.Tool] {
		<-ctx.Done()
		return toolrun.Output{TimedOut: true}, nil
	}
	return f.outs[s.Tool], nil
}

func isolate(t testing.TB) string {
	t.Helper()
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func packsOf(t testing.TB, body string) map[string]manifest.PackConfig {
	t.Helper()
	var m map[string]manifest.PackConfig
	if err := yaml.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func writeFile(t testing.TB, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
