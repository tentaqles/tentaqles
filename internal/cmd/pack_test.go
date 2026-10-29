package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/approval"
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
	packs.RegisterGuard(packs.Guard{ID: "tq/example-cmd-find", Pack: "db", Events: []string{"Bash"},
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
	packs.RegisterGuard(packs.Guard{ID: "tq/example-cmd-plan", Pack: "cloud", Events: []string{"Bash", "PowerShell"},
		Supersedes: []string{"tq/iac-apply"},
		Check: func(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
			if !strings.Contains(call.Command, "tofu apply") && !strings.Contains(call.Command, "pulumi up") {
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
					if err := g.ReadArgs(rel); err != nil {
						return policy.Deny, err.Error(), true
					}
					return policy.Allow, "plan verified", true
				}
			}
			return policy.Deny, "no stored plan", true
		}})
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
	signedApproval(t, "acme", "cloud", "d-example")
	return runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, cmd))
}

// signedApproval writes an asked line and a human answer signed with the
// workspace key (the mock keychain), as tq hub answer would.
func signedApproval(t *testing.T, ws, pack, id string) {
	t.Helper()
	exp := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	a := ledger.Entry{Event: ledger.Asked, Decision: id, Pack: pack, Action: "apply", Subject: "s", Target: "prod", Evidence: map[string]string{"plan": "sha256:aa"}, ExpiresAt: exp, Grant: "apply"}
	if err := ledger.Append(ws, a); err != nil {
		t.Fatal(err)
	}
	priv, err := ledger.SigningKey(ws)
	if err != nil {
		t.Fatal(err)
	}
	e, err := ledger.AnswerSigner(ws, priv)(ledger.Shown{Pack: pack, Action: "apply", Subject: "s", Target: "prod", ExpiresAt: exp, Grant: "apply", Evidence: a.Evidence}, id, "apply", "hub", time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Append(ws, e); err != nil {
		t.Fatal(err)
	}
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
	if err != nil || len(es) != 3 || es[2].Event != ledger.Consumed || es[2].Decision != "d-example" {
		t.Fatalf("consumed line: %v %+v", err, es)
	}
	if m, err := ledger.LoadHighWater("acme", "cloud"); err != nil || m.Decision != "d-example" {
		t.Fatalf("high-water mark: %v %+v", err, m)
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
	clearAgentMarkers(t)
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
	code, hout, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "terraform apply prod.tfplan"))
	if code != 0 || !strings.Contains(hout, "tq/iac-apply") || strings.Contains(hout, "example-cmd-apply") {
		t.Fatalf("a repo-enabled pack guard ran or superseded: %d %s", code, hout)
	}
}

// C1: with no trusted workspace, no pack guard runs and the repo file is ignored.
func TestHookNoWorkspaceNoPackGuards(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tq-packs.yaml"), []byte("cloud: { enabled: true }\n"), 0o644)
	os.Mkdir(filepath.Join(dir, ".git"), 0o755)
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, dir, "terraform apply approved.tfplan"))
	if code != 0 || !strings.Contains(out, "tq/iac-apply") {
		t.Fatalf("%d %s", code, out)
	}
}

// M2: the hook claims the approval; a second hook with the same approval is denied.
func TestHookClaimIsSingleUse(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "packs:\n  cloud: { enabled: true }\n")
	signedApproval(t, "acme", "cloud", "d-example")
	code, _, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "terraform apply approved.tfplan"))
	if code != 0 || !ledger.Claimed("acme", "cloud", "d-example") {
		t.Fatalf("first use: %d", code)
	}
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "terraform apply approved.tfplan"))
	if code != 2 || !strings.Contains(out, "already used") {
		t.Fatalf("second use: %d %s", code, out)
	}
}

// C1(d), M4: the core rules are on with no pack enabled, and unattended
// mode or guard.actions cannot soften them.
func TestHookCoreFrameworkRules(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "guard: { actions: { tq/hub-store-write: ask }, disable: [tq/pack-config-change] }\n")
	payload := `{"tool_name":"Write","cwd":` + jsonPath(t, ws) + `,"tool_input":{"file_path":` + jsonPath(t, filepath.Join(ws, "tq-packs.yaml")) + `,"content":"cloud: {}"}}`
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, payload)
	if code != 0 || !strings.Contains(out, "tq/pack-config-change") {
		t.Fatalf("pack-config-change: %d %s", code, out)
	}
	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, `echo x >> ~/.tentaqles/packs/acme/ml/ledger.jsonl`))
	if code != 2 || !strings.Contains(out, "tq/hub-store-write") {
		t.Fatalf("hub-store-write stays a deny: %d %s", code, out)
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

// Round 5: a stored plan the cloud guard verified is a plain argument, not a
// state-dir write; every bypass is still denied.
func TestHookStoredPlanReadArg(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "packs:\n  cloud: { enabled: true }\n")
	dir := packs.StateDir("acme", "cloud")
	os.MkdirAll(filepath.Join(dir, "plans"), 0o700)
	plan := filepath.Join(dir, "plans", "p1.tfplan")
	os.WriteFile(plan, []byte("plan"), 0o600)
	os.WriteFile(filepath.Join(dir, "plans", "p2.tfplan"), []byte("plan"), 0o600)
	led := filepath.Join(dir, "ledger.jsonl")
	q := func(s string) string { return `"` + s + `"` }
	for _, c := range []string{
		"tofu apply " + plan,
		"tofu apply " + q(plan),
		"pulumi up --plan " + plan,
		"pulumi up --plan=" + plan,
		"cd infra && tofu apply " + plan,
	} {
		code, out := hookDecision(t, ws, policy.ToolCall{Tool: "Bash", Command: c})
		if code != 0 || strings.Contains(out, "tq/hub-store-write") {
			t.Fatalf("allowed form %q: %d %s", c, code, out)
		}
	}
	for name, c := range map[string]string{
		"extra segment, same path":  "tofu apply " + plan + "; cat " + plan,
		"extra segment, state dir":  "tofu apply " + plan + " && ls " + filepath.Dir(plan),
		"redirect":                  "tofu apply " + plan + " > " + plan,
		"cp to the same path":       "tofu apply " + plan + " && cp x " + plan,
		"tee":                       "tofu apply " + plan + " | tee " + plan,
		"another protected path":    "tofu apply " + plan + " " + led,
		"a path not verified":       "tofu apply " + plan + " " + filepath.Join(dir, "plans", "p2.tfplan"),
		"a .. spelling":             "tofu apply " + filepath.Join(dir, "plans", "x") + "/../p1.tfplan",
		"substitution":              "tofu apply " + plan + " $(cat " + led + ")",
		"never hashed (liar guard)": "tofu apply liar " + plan,
		"newline segment":           "tofu apply " + plan + "\ncat " + plan,
	} {
		code, out := hookDecision(t, ws, policy.ToolCall{Tool: "Bash", Command: c})
		if code != 2 || !strings.Contains(out, "tq/hub-store-write") {
			t.Fatalf("%s: %d %s", name, code, out)
		}
	}
}
