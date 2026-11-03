package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/approval"
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
			return []*cobra.Command{{Use: "example-cmd", RunE: func(c *cobra.Command, _ []string) error { c.Print("example-cmd ran"); return nil }},
				{Use: "example-ctx", RunE: func(c *cobra.Command, _ []string) error {
					in, err := packs.FromCommand(c)
					if err != nil {
						return err
					}
					c.Print(in.Workspace() + " " + in.Pack() + " " + fmt.Sprint(in.Settings().Enabled))
					return nil
				}}}
		},
	})
	packs.RegisterPack(packs.Pack{Name: "db", Flags: []packs.Flag{{Name: "migration", Reviewers: []string{"db-reviewer"}}},
		// R5-4: an approve command that only asks, and an executor that declares itself.
		Commands: func() []*cobra.Command {
			approve := &cobra.Command{Use: "approve", Annotations: map[string]string{packs.AnnotationHumanOnly: packs.ApprovalAsk},
				RunE: func(c *cobra.Command, _ []string) error {
					in, err := packs.FromCommand(c)
					if err != nil {
						return err
					}
					id, _, err := approval.Ask(in.Workspace(), approval.Request{Pack: "db", Action: "migrate", Subject: "m1", Target: "prod", Evidence: map[string]string{"plan": "sha256:aa"}})
					c.Print(id)
					return err
				}}
			apply := &cobra.Command{Use: "apply", Annotations: map[string]string{packs.AnnotationHumanOnly: packs.HumanOnlyExecute},
				RunE: func(c *cobra.Command, args []string) error {
					if err := packs.HumanOnly(); err != nil {
						return err
					}
					in, err := packs.FromCommand(c)
					if err != nil {
						return err
					}
					_, err = approval.Claim(c, in.Workspace(), args[0], approval.Binding{Pack: "db", Action: "migrate", Subject: "m1", Target: "prod"}, map[string]string{"plan": "sha256:aa"})
					return err
				}}
			return []*cobra.Command{approve, apply}
		}})
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
	packs.RegisterGuard(packs.Guard{Category: guardcfg.CatPacks, ID: "tq/example-cmd-find", Pack: "db", Events: []string{"Bash"},
		Check: func(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
			if !strings.Contains(call.Command, "find-approval") {
				return policy.Allow, "", false
			}
			a, err := approval.FindGuard(g, approval.Binding{Pack: "db", Action: "apply", Target: "prod"}, map[string]string{"plan": "sha256:aa"})
			if err != nil {
				return policy.Deny, err.Error(), true
			}
			g.Consume(a.Decision)
			return policy.Allow, "approved", true
		}})
	// Round 5: a guard that verifies a stored plan and names it as a read arg.
	packs.RegisterGuard(packs.Guard{Category: guardcfg.CatPacks, ID: "tq/example-cmd-plan", Pack: "cloud", Events: []string{"Bash", "PowerShell"},
		Supersedes: []string{"tq/iac-apply"},
		Check: func(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
			if !strings.Contains(call.Command, "tofu apply") && !strings.Contains(call.Command, "pulumi up") && !strings.Contains(call.Command, "terraform apply -") {
				return policy.Allow, "", false
			}
			for _, f := range strings.Fields(strings.NewReplacer("=", " ", ";", " ", "&", " ").Replace(call.Command)) {
				f = strings.ReplaceAll(strings.Trim(f, `\"'`), `\`, "/")
				if i := strings.Index(f, "/plans/"); i >= 0 {
					rel := f[i+1:]
					if strings.Contains(call.Command, "liar") {
						rel = "plans/never-hashed.tfplan"
					} else if _, err := g.HashState(rel); err != nil {
						return policy.Deny, err.Error(), true
					}
					spec := packs.ReadSpec{Program: "tofu", BoolFlags: []string{"-auto-approve", "-no-color"}}
					switch {
					case strings.Contains(call.Command, "pulumi up"):
						spec = packs.ReadSpec{Program: "pulumi", Flag: "--plan", BoolFlags: []string{"--yes", "-y", "--skip-preview"}}
					case strings.Contains(call.Command, "terraform apply"):
						spec.Program = "terraform"
					}
					if err := g.ReadArgs(spec, rel); err != nil {
						return policy.Deny, err.Error(), true
					}
					return policy.Allow, "plan verified", true
				}
			}
			return policy.Deny, "no stored plan", true
		}})
	packs.RegisterGuard(packs.Guard{Category: guardcfg.CatPacks, ID: "tq/example-cmd-apply", Pack: "cloud", Events: []string{"Bash", "PowerShell"},
		Supersedes: []string{"tq/iac-apply"},
		Check: func(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
			c := call.Command
			switch {
			case !strings.Contains(c, "terraform apply"), strings.Contains(strings.ReplaceAll(c, `\`, "/"), "/plans/"):
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

func clearPackAgentMarkers(t *testing.T) {
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
	clearPackAgentMarkers(t)
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

// --- fix round 1 -----------------------------------------------------------

// F5: an invalid or untrusted manifest is a config error (exit 2), never
// "no packs enabled".
func TestPackInvalidOrUntrustedManifestExits2(t *testing.T) {
	ws := packRepo(t, "packs:\n  data: { enabled: true }\n")
	// Edited after tq allow: untrusted.
	mp := filepath.Join(ws, ".tentaqles.yaml")
	raw, _ := os.ReadFile(mp)
	os.WriteFile(mp, append(raw, []byte("\n# edited\n")...), 0o600)
	for _, args := range [][]string{{"pack", "check"}, {"pack", "list", "--json"}, {"pack", "doctor"}, {"pack", "approvals"}, {"pack", "data", "example-cmd"}} {
		code, out, errOut := runHook(t, args, "")
		if code != 2 || !strings.Contains(errOut, "not trusted") {
			t.Fatalf("%v untrusted: %d %q %q", args, code, out, errOut)
		}
	}
	// Invalid packs block.
	os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: acme\npacks: { data: { timeouts: { dbt: 3600s } } }\n"), 0o600)
	code, _, errOut := runHook(t, []string{"pack", "check"}, "")
	if code != 2 || !strings.Contains(errOut, "manifest invalid") {
		t.Fatalf("invalid: %d %q", code, errOut)
	}
}

// F12: cobra flag errors exit 2; approvals outside a workspace exit 0.
func TestPackFlagErrorsAndApprovalsOutside(t *testing.T) {
	packRepo(t, "packs:\n  data: { enabled: true }\n")
	for _, args := range [][]string{{"pack", "check", "--budget", "600"}, {"pack", "check", "--nope"}, {"pack", "list", "--bogus"}} {
		code, _, _ := runHook(t, args, "")
		if code != 2 {
			t.Fatalf("%v: %d", args, code)
		}
	}
	isolateHome(t)
	clearPackAgentMarkers(t)
	dir := t.TempDir()
	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	os.Chdir(dir)
	code, out, _ := runHook(t, []string{"pack", "approvals"}, "")
	if code != 0 || !strings.Contains(out, "not in a workspace") {
		t.Fatalf("%d %q", code, out)
	}
}

// F10, F12: --paths takes a directory and keeps commas.
func TestPackCheckPathsDirectoryAndComma(t *testing.T) {
	ws := packRepo(t, "packs:\n  data: { enabled: true }\n")
	os.MkdirAll(filepath.Join(ws, "models"), 0o755)
	os.WriteFile(filepath.Join(ws, "models", "a,b.sql"), []byte("select * from t\n"), 0o644)
	code, out, _ := runHook(t, []string{"pack", "check", "--json", "--paths", "models"}, "")
	if code != 1 || !strings.Contains(out, `"models/a,b.sql"`) {
		t.Fatalf("directory with a comma file: %d %s", code, out)
	}
}

// C1: the repo layer can never enable a pack (and so never a guard).
func TestRepoLayerCannotEnable(t *testing.T) {
	ws := packRepo(t, "")
	os.WriteFile(filepath.Join(ws, "tq-packs.yaml"), []byte("cloud: { enabled: true }\ndata: { enabled: true }\n"), 0o644)
	_, out, _ := runHook(t, []string{"pack", "list", "--enabled", "--json"}, "")
	var lr packs.ListReport
	if err := json.Unmarshal([]byte(out), &lr); err != nil || len(lr.Packs) != 0 {
		t.Fatalf("%v %s", err, out)
	}
}

// F6: an invalid tq-packs.yaml keeps the manifest flags and raises
// packs_config_invalid.
func TestTriageInvalidRepoFileKeepsFlags(t *testing.T) {
	ws := packRepo(t, "packs:\n  data: { enabled: true }\n")
	os.WriteFile(filepath.Join(ws, "tq-packs.yaml"), []byte("web: {}\n"), 0o644)
	os.WriteFile(filepath.Join(ws, "orders.sql"), []byte("select 1\n"), 0o644)
	packGit(t, ws, "add", "orders.sql")
	code, out, _ := runHook(t, []string{"decide", "triage", "--staged"}, "")
	if code != 3 || !strings.Contains(out, "example_cmd_sql") || !strings.Contains(out, "packs_config_invalid") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

// F8: a pack subcommand gets its run context from FromCommand.
func TestPackSubcommandFromCommand(t *testing.T) {
	packRepo(t, "packs:\n  data: { enabled: true }\n")
	_, out, _ := runHook(t, []string{"pack", "data", "example-ctx"}, "")
	if out != "acme data true" {
		t.Fatalf("%q", out)
	}
}

func TestPackBadBaseAndBaselineRefusal(t *testing.T) {
	packRepo(t, "packs:\n  data: { tools: [tq-fake-tool-xyz], baseline: .tq/base.json }\n")
	code, _, _ := runHook(t, []string{"pack", "check", "--base", "no-such-ref"}, "")
	if code != 2 {
		t.Fatalf("bad --base: %d", code)
	}
	code, _, errOut := runHook(t, []string{"pack", "baseline"}, "")
	if code != 2 || !strings.Contains(errOut, "did not run cleanly") {
		t.Fatalf("baseline with a missing declared tool: %d %q", code, errOut)
	}
}
