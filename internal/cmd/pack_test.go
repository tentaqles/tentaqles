package cmd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// Example checks, flags, guards and a command registered only in this test
// binary: the CLI and the hook are proven end to end without a real pack.
func init() {
	packs.RegisterPack(packs.Pack{
		Name: "data",
		Flags: []packs.Flag{
			{Name: "example_cmd_sql", Reviewers: []string{"pipeline-reviewer"}, Raise: func(in *packs.Input) bool { return in.AnyChanged("*.sql") }},
		},
		Commands: func() []*cobra.Command {
			return []*cobra.Command{{Use: "example-cmd", RunE: func(c *cobra.Command, _ []string) error { c.Print("example-cmd ran"); return nil }}}
		},
	})
	packs.RegisterPack(packs.Pack{Name: "db", Flags: []packs.Flag{{Name: "migration", Reviewers: []string{"db-reviewer"}}}})
	packs.Register(packs.Check{ID: "data/example-cmd-star", Pack: "data", Kind: packs.Builtin,
		Applies: func(in *packs.Input) bool { return in.AnyChanged("*.sql") },
		Run: func(ctx context.Context, in *packs.Input) packs.Result {
			var fs []packs.Finding
			for _, p := range in.ChangedPaths() {
				b, _ := in.ReadNew(p, 0)
				if strings.Contains(strings.ToLower(string(b)), "select *") {
					fs = append(fs, packs.Finding{File: p, Line: 1, Level: packs.LevelError, Rule: "EX01", Message: "SELECT *"})
				}
			}
			return packs.Result{Status: packs.OK, Detail: "checked", Findings: fs}
		}})
	packs.Register(packs.Check{ID: "data/example-cmd-tool", Pack: "data", Kind: packs.External, Tool: "tq-fake-tool-xyz",
		Run: func(ctx context.Context, in *packs.Input) packs.Result { return packs.Result{Status: packs.OK} }})
	packs.RegisterGuard(packs.Guard{ID: "tq/example-cmd-apply", Pack: "cloud", Events: []string{"Bash", "PowerShell"},
		Supersedes: []string{"tq/iac-apply"},
		Check: func(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
			c := call.Command
			switch {
			case !strings.Contains(c, "terraform apply"):
				return policy.Allow, "", false
			case strings.Contains(c, "panic.tfplan"):
				panic("example guard exploded")
			case strings.Contains(c, "approved.tfplan"):
				g.Consume("d-example")
				return policy.Allow, "approved by the human", true
			}
			return policy.Deny, "no approval for this plan", true
		}})
}

func clearAgentMarkers(t *testing.T) {
	for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK", "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN"} {
		t.Setenv(k, "")
	}
}

func packGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-c", "user.email=tester@acme.test", "-c", "user.name=tester", "-c", "commit.gpgsign=false"}, args...)...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

// packRepo makes a trusted workspace acme whose root is a git repo with one
// commit, and chdirs there.
func packRepo(t *testing.T, packsBlock string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	clearAgentMarkers(t)
	ws := setupTrustedWorkspaceWithManifest(t, "acme", packsBlock)
	packGit(t, ws, "init", "-q")
	os.WriteFile(filepath.Join(ws, "README.md"), []byte("acme\n"), 0o644)
	packGit(t, ws, "add", "README.md")
	packGit(t, ws, "commit", "-q", "-m", "init")
	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(ws); err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestPackCheckExitCodesAndJSON(t *testing.T) {
	ws := packRepo(t, "packs:\n  data: { enabled: true }\n")
	code, out, _ := runHook(t, []string{"pack", "check", "--json"}, "")
	var rep packs.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil || code != 0 || rep.Schema != "tq.packs/v1" || rep.Workspace != "acme" || rep.Base != "HEAD" {
		t.Fatalf("clean tree: code %d %v %s", code, err, out)
	}
	os.WriteFile(filepath.Join(ws, "orders.sql"), []byte("select * from t\n"), 0o644)
	code, out, _ = runHook(t, []string{"pack", "check"}, "")
	if code != 1 || !strings.Contains(out, "fail data/example-cmd-star: checked") || !strings.Contains(out, "orders.sql:1 error EX01 SELECT *") {
		t.Fatalf("untracked SQL file: code %d\n%s", code, out)
	}
	code, _, _ = runHook(t, []string{"pack", "check", "--staged"}, "")
	if code != 0 {
		t.Fatalf("--staged sees nothing staged: %d", code)
	}
	code, out, _ = runHook(t, []string{"pack", "check", "--paths", "orders.sql", "--json"}, "")
	json.Unmarshal([]byte(out), &rep)
	if code != 1 || rep.Summary.RequiredFail != 1 {
		t.Fatalf("--paths: %d %s", code, out)
	}
	code, _, errOut := runHook(t, []string{"pack", "check", "--staged", "--all"}, "")
	if code != 2 || !strings.Contains(errOut, "only one of") {
		t.Fatalf("usage error: %d %q", code, errOut)
	}
	code, _, _ = runHook(t, []string{"pack", "check", "--budget", "2h"}, "")
	if code != 2 {
		t.Fatalf("budget cap: %d", code)
	}
	os.WriteFile(filepath.Join(ws, "tq-packs.yaml"), []byte("web: {}\n"), 0o644)
	code, _, errOut = runHook(t, []string{"pack", "check"}, "")
	if code != 2 || !strings.Contains(errOut, "tq-packs.yaml") {
		t.Fatalf("config error: %d %q", code, errOut)
	}
}

func TestPackCheckDeclaredMissingTool(t *testing.T) {
	packRepo(t, "packs:\n  data: { tools: [tq-fake-tool-xyz] }\n")
	code, out, _ := runHook(t, []string{"pack", "check", "--json"}, "")
	var rep packs.Report
	json.Unmarshal([]byte(out), &rep)
	found := false
	for _, r := range rep.Results {
		if r.ID == "data/example-cmd-tool" {
			found = r.Status == packs.Fail && r.Required && r.Detail == packs.MissingDetail("data")
		}
	}
	if code != 1 || !found {
		t.Fatalf("declared but missing must fail: %d %s", code, out)
	}
	code, out, _ = runHook(t, []string{"pack", "check", "--builtin-only"}, "")
	if code != 0 {
		t.Fatalf("--builtin-only skips external checks: %d %s", code, out)
	}
	code, out, _ = runHook(t, []string{"pack", "doctor", "--json"}, "")
	if code != 1 || !strings.Contains(out, "packs-tool-missing") {
		t.Fatalf("doctor: %d %s", code, out)
	}
}

func TestPackNoPacksEnabled(t *testing.T) {
	packRepo(t, "")
	code, out, _ := runHook(t, []string{"pack", "check"}, "")
	if code != 0 || strings.TrimSpace(out) != "no packs enabled" {
		t.Fatalf("%d %q", code, out)
	}
}

func TestPackListAndCrew(t *testing.T) {
	packRepo(t, "packs:\n  data: { enabled: true }\n  db: { enabled: true }\n")
	_, out, _ := runHook(t, []string{"pack", "list", "--enabled", "--json"}, "")
	var lr packs.ListReport
	if err := json.Unmarshal([]byte(out), &lr); err != nil || len(lr.Packs) != 2 || lr.Packs[0].Name != "data" || !strings.HasPrefix(lr.ConfigHash, "sha256:") {
		t.Fatalf("%v %s", err, out)
	}
	_, cout, _ := runHook(t, []string{"pack", "check", "--json"}, "")
	var rep packs.Report
	json.Unmarshal([]byte(cout), &rep)
	if rep.ConfigHash != lr.ConfigHash {
		t.Fatal("list and check config_hash differ")
	}
	_, out, _ = runHook(t, []string{"pack", "crew", "--flags", "migration,example_cmd_sql,auth"}, "")
	if strings.TrimSpace(out) != "db-reviewer\npipeline-reviewer" {
		t.Fatalf("crew %q", out)
	}
	_, out, _ = runHook(t, []string{"pack", "data", "example-cmd"}, "")
	if out != "example-cmd ran" {
		t.Fatalf("pack subcommand: %q", out)
	}
}

func TestPackBaselineRefusesAgents(t *testing.T) {
	packRepo(t, "packs:\n  data: { enabled: true, baseline: .tq/base.json }\n")
	t.Setenv("CLAUDECODE", "1")
	root := NewRoot()
	root.SetArgs([]string{"pack", "baseline"})
	root.SetOut(&strings.Builder{})
	root.SetErr(&strings.Builder{})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "human") {
		t.Fatalf("agent session must be refused: %v", err)
	}
	t.Setenv("CLAUDECODE", "")
	code, out, _ := runHook(t, []string{"pack", "baseline"}, "")
	if code != 0 || !strings.Contains(out, "data: 0 entries -> .tq/base.json") {
		t.Fatalf("%d %s", code, out)
	}
}

func TestPackApprovalsNeedsWorkspace(t *testing.T) {
	packRepo(t, "packs:\n  ml: { enabled: true }\n")
	code, out, _ := runHook(t, []string{"pack", "approvals"}, "")
	if code != 0 || !strings.Contains(out, "no pack approvals") {
		t.Fatalf("%d %s", code, out)
	}
}

func TestTriageAddsPackFlags(t *testing.T) {
	ws := packRepo(t, "packs:\n  data: { enabled: true }\n")
	os.WriteFile(filepath.Join(ws, "orders.sql"), []byte("select 1\n"), 0o644)
	packGit(t, ws, "add", "orders.sql")
	code, out, _ := runHook(t, []string{"decide", "triage", "--staged"}, "")
	if code != 3 || !strings.Contains(out, "example_cmd_sql") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

// --- hook wiring -----------------------------------------------------------

func cloudHook(t *testing.T, cmd string) (int, string, string) {
	t.Helper()
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "packs:\n  cloud: { enabled: true }\n")
	return runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, cmd))
}

func TestHookPackGuardDeniesAndSupersedes(t *testing.T) {
	code, out, _ := cloudHook(t, "terraform apply prod.tfplan")
	if code != 2 || !strings.Contains(out, `"rule":"tq/example-cmd-apply"`) {
		t.Fatalf("deny: %d %s", code, out)
	}
	if strings.Contains(out, "tq/iac-apply]") {
		t.Fatalf("the handled guard must supersede tq/iac-apply: %s", out)
	}
	// Approved: the guard allows, tq/iac-apply's ask is dropped, and the
	// approval is marked consumed in the ledger before the allow.
	code, out, _ = cloudHook(t, "terraform apply approved.tfplan")
	if code != 0 || strings.Contains(out, "iac-apply") {
		t.Fatalf("allow: %d %s", code, out)
	}
	es, err := ledger.Read("acme", "cloud")
	if err != nil || len(es) != 1 || es[0].Event != ledger.Consumed || es[0].Decision != "d-example" {
		t.Fatalf("consumed line: %v %+v", err, es)
	}
}

func TestHookPackGuardPanicDenies(t *testing.T) {
	code, out, _ := cloudHook(t, "terraform apply panic.tfplan")
	if code != 2 || !strings.Contains(out, "example guard exploded") {
		t.Fatalf("a guard panic must deny: %d %s", code, out)
	}
}

func TestHookConsumeFailureDenies(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "packs:\n  cloud: { enabled: true }\n")
	p, _ := ledger.Path("acme", "cloud")
	os.MkdirAll(p, 0o700) // a directory where the ledger file must be
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "terraform apply approved.tfplan"))
	if code != 2 || !strings.Contains(out, "could not be marked used") {
		t.Fatalf("a failed consumed append must deny: %d %s", code, out)
	}
}

func TestHookPackGuardOnlyWhenEnabledAndHonoursDisable(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "terraform apply prod.tfplan"))
	if code != 0 || !strings.Contains(out, "tq/iac-apply") {
		t.Fatalf("pack off: the core ask applies: %d %s", code, out)
	}
	ws = setupTrustedWorkspaceWithManifest(t, "acme", "packs:\n  cloud: { enabled: true }\nguard: { disable: [tq/example-cmd-apply] }\n")
	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "terraform apply prod.tfplan"))
	if code != 0 || !strings.Contains(out, "tq/iac-apply") {
		t.Fatalf("guard.disable: %d %s", code, out)
	}
	ws = setupTrustedWorkspaceWithManifest(t, "acme", "packs:\n  cloud: { enabled: true }\nguard: { actions: { tq/example-cmd-apply: ask } }\n")
	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "terraform apply prod.tfplan"))
	if code != 0 || !strings.Contains(out, `"rule":"tq/example-cmd-apply"`) {
		t.Fatalf("guard.actions: %d %s", code, out)
	}
}

func TestGuardListShowsPackGuards(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	_ = ws
	rows, _, _ := guardRows(nil)
	for _, r := range rows {
		if r.ID == "tq/example-cmd-apply" {
			if r.Kind != "pack" || r.Pack != "cloud" {
				t.Fatalf("%+v", r)
			}
			return
		}
	}
	t.Fatal("pack guard not listed")
}

func TestPackParentsMounted(t *testing.T) {
	root := NewRoot()
	for _, p := range []string{"data", "ml", "cloud", "db"} {
		if c, _, err := root.Find([]string{"pack", p}); err != nil || c.Name() != p {
			t.Fatalf("tq pack %s: %v", p, err)
		}
	}
}
