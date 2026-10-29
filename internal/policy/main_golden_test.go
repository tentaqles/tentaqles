package policy_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// writeCorpus writes the corpus calls to testdata/default_corpus.json, the
// input of the main-verdicts generator (see TestDefaultModeMatchesMain).
var writeCorpus = flag.Bool("write-corpus", false, "write testdata/default_corpus.json")

// credURL is a URL with user:password in it.
var credURL = regexp.MustCompile(`://[^/\s:@"]+:[^/\s@"]+@`)

// mainVerdict is what main (before spec E) decided for one corpus call.
type mainVerdict struct {
	Call   policy.ToolCall `json:"call"`
	Action policy.Action   `json:"action"`
	IDs    []string        `json:"ids"`
}

// newInSpecE are the rules spec E added or widened, which default applies
// too (MIGRATE.md "Behaviour changes"): a corpus call any of them matches is
// not compared with main.
var newInSpecE = map[string]bool{
	"tq/nested-agent-loosened": true, // §9
	"tq/system-dir-write":      true, // §5.3
	"tq/elevation":             true, // §5.3
	"tq/mode-change":           true, // §10.1
	"tq/trust-store-write":     true, // widened: identity files, CLAUDE_CONFIG_DIR/CODEX_HOME, .codex
	"tq/settings-widen":        true, // widened: enabledPlugins, env, defaultMode
}

// Guard mode default decides what main decided before spec E, over the
// built-in decision corpus, except where a rule spec E added or widened
// matches (final review m3). testdata/main_verdicts.json holds main's
// verdicts; regenerate it after a corpus change with
//
//	go test ./internal/policy -run TestDefaultModeMatchesMain -args -write-corpus
//
// then, in a worktree of main, evaluate testdata/default_corpus.json with
// main's policy.Builtin() set and write {call, action, ids} per call.
func TestDefaultModeMatchesMain(t *testing.T) {
	var calls []policy.ToolCall
	for _, c := range policy.BuiltinCorpus() {
		// Fake credentials in a URL (tq:allow-secret in the Go fixtures)
		// cannot carry that marker in JSON, so the goldens leave them out.
		if !credURL.MatchString(c.Command + " " + c.Content + " " + c.Path) {
			calls = append(calls, c)
		}
	}
	if *writeCorpus {
		b, err := json.MarshalIndent(calls, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join("testdata", "default_corpus.json"), append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Skip("corpus written")
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "main_verdicts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var verdicts []mainVerdict
	if err := json.Unmarshal(raw, &verdicts); err != nil {
		t.Fatal(err)
	}
	byCall := map[policy.ToolCall]mainVerdict{}
	for _, v := range verdicts {
		byCall[v.Call] = v
	}
	s, errs := policy.Build(policy.Layers{Builtin: policy.Builtin()})
	if len(errs) > 0 {
		t.Fatalf("builtin rules do not compile: %v", errs)
	}
	def := guardcfg.DefaultResolved()
	compared := 0
	for _, call := range calls {
		got, _ := s.EvaluateMapped(call, def.Effective)
		ids := matchedIDs(got)
		if slices.ContainsFunc(ids, func(id string) bool { return newInSpecE[id] }) {
			continue
		}
		want, ok := byCall[call]
		if !ok {
			continue // a corpus case added after the golden was made
		}
		wantIDs := slices.Clone(want.IDs)
		slices.Sort(wantIDs)
		if slices.ContainsFunc(wantIDs, func(id string) bool { return newInSpecE[id] }) {
			continue
		}
		compared++
		if got.Action != want.Action || !slices.Equal(ids, wantIDs) {
			t.Errorf("%+v: default %s %v, main %s %v", call, got.Action, ids, want.Action, wantIDs)
		}
	}
	if compared < 50 {
		t.Fatalf("only %d corpus calls compared with main", compared)
	}
}
