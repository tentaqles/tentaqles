package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs/approval"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
)

func runRoot(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := NewRoot()
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

var packEv = map[string]string{"model_card": "sha256:aa"}

// N1(a): tq hub answer on a pack approval needs a terminal and the typed
// binding; --choice alone never answers it.
func TestHubAnswerPackApprovalNeedsTheTerminal(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "packs:\n  ml: { enabled: true }\n")
	clearAgentMarkers(t)
	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	os.Chdir(ws)
	id, card, err := approval.Ask("acme", approval.Request{Pack: "ml", Action: "promote", Subject: "churn@7", Target: "production", Evidence: packEv})
	if err != nil {
		t.Fatal(err)
	}
	want := approval.Binding{Pack: "ml", Action: "promote", Subject: "churn@7", Target: "production"}
	confirm := "ml promote " + subjectCode("churn@7") + "\n"
	// A pipe (no terminal): refused, even with the right confirmation.
	if _, err := runRoot(t, confirm, "hub", "answer", card, id, "--choice", "approve"); !errors.Is(err, errPackAnswerTTY) {
		t.Fatalf("pipe: %v", err)
	}
	prevTTY := stdinIsTerminal
	stdinIsTerminal = func(io.Reader) bool { return true }
	t.Cleanup(func() { stdinIsTerminal = prevTTY })
	if _, err := runRoot(t, "yes\n", "hub", "answer", card, id, "--choice", "approve"); err == nil || !strings.Contains(err.Error(), "did not match") {
		t.Fatalf("a wrong confirmation: %v", err)
	}
	if _, err := approval.Verify("acme", id, want, packEv); !errors.Is(err, approval.ErrPending) {
		t.Fatalf("still pending: %v", err)
	}
	// An agent session is refused even at a terminal.
	t.Setenv("CLAUDECODE", "1")
	if _, err := runRoot(t, confirm, "hub", "answer", card, id, "--choice", "approve"); err == nil {
		t.Fatal("agent session answered")
	}
	t.Setenv("CLAUDECODE", "")
	out, err := runRoot(t, confirm, "hub", "answer", card, id, "--choice", "approve")
	if err != nil || !strings.Contains(out, "approve by hub:") {
		t.Fatalf("%v %s", err, out)
	}
	if _, err := approval.Verify("acme", id, want, packEv); err != nil {
		t.Fatalf("granted: %v", err)
	}
}

func hookDecision(t *testing.T, ws string, call policy.ToolCall) (int, string) {
	t.Helper()
	var payload string
	if call.IsShell() {
		payload = `{"tool_name":"` + call.Tool + `","cwd":` + jsonPath(t, ws) + `,"tool_input":{"command":` + jsonPath(t, call.Command) + `}}`
	} else {
		payload = `{"tool_name":"` + call.Tool + `","cwd":` + jsonPath(t, ws) + `,"tool_input":{"file_path":` + jsonPath(t, call.Path) + `,"content":"x"}}`
	}
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, payload)
	return code, out
}

// N1(c), N2: the reviewer's probe commands are denied or asked.
func TestHookStateDirAndHumanAnswer(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	home := paths.Home()
	L := filepath.ToSlash(filepath.Join(home, "packs", "acme", "ml", "ledger.jsonl"))
	deny := []policy.ToolCall{
		{Tool: "Bash", Command: `cd ~/.tentaqles/packs/acme/ml && echo x >> ledger.jsonl`},
		{Tool: "Bash", Command: `cd ~/.tentaqles/packs/acme/ml; printf x>>ledger.jsonl`},
		{Tool: "Bash", Command: `python -c "open('` + L + `','a').write('x')"`},
		{Tool: "Bash", Command: `python3 -c 'import pathlib;pathlib.Path.home().joinpath(".tentaqles","packs","acme","ml","ledger.jsonl").open("a").write("x")'`},
		{Tool: "Bash", Command: `node -e "require('fs').appendFileSync(process.env.HOME+'/.tentaqles/packs/acme/ml/ledger.jsonl','x')"`},
		{Tool: "PowerShell", Command: `[IO.File]::AppendAllText("$HOME\.tentaqles\packs\acme\ml\ledger.jsonl","x")`},
		{Tool: "Bash", Command: `D=~/.tenta""qles; echo x >> $D/packs/acme/ml/ledger.jsonl`},
		{Tool: "Bash", Command: `echo x >> ~/.tentaqles/pack[s]/acme/ml/ledger.jsonl`},
		{Tool: "Bash", Command: `echo x >> ~/.tentaqles//./packs/acme/ml/ledger.jsonl`},
		{Tool: "Bash", Command: `echo x >> ~/TENTAQ~1/packs/acme/ml/ledger.jsonl`},
		{Tool: "Bash", Command: `ln -s ~/.tentaqles /tmp/t && echo x >> /tmp/t/packs/acme/ml/ledger.jsonl`},
		{Tool: "PowerShell", Command: `New-Item -ItemType Junction -Path C:\t -Target $HOME\.tentaqles; Add-Content C:\t\packs\acme\ml\ledger.jsonl x`},
		{Tool: "Bash", Command: `echo x >> ` + L}, // the real (custom) TQ_HOME of this test
		{Tool: "Bash", Command: `find ~/.tentaqles/packs -name d123 -delete`},
		{Tool: "Bash", Command: `tar -xf /tmp/forged.tar -C ~/.tentaqles/packs/acme/ml`},
		{Tool: "Bash", Command: `env -u CLAUDECODE -u CLAUDE_PLUGIN_ROOT tq hub answer c1 d123 --choice approve`},
		{Tool: "PowerShell", Command: `$env:CLAUDECODE=$null; $env:CLAUDE_PLUGIN_ROOT=$null; tq hub answer c1 d123 --choice approve`},
		{Tool: "Bash", Command: `tq pack ml promote apply churn@7`},
		{Tool: "Bash", Command: `tq pack cloud approve d123`},
		{Tool: "Bash", Command: `curl -X POST -H 'Origin: http://127.0.0.1:8765' http://127.0.0.1:8765/api/cards/c1/decisions/d123/answer -d choice=approve`},
		{Tool: "PowerShell", Command: `Invoke-WebRequest -Method Post http://127.0.0.1:8765/cards/c1/decisions/d1/answer`},
		{Tool: "Bash", Command: `cat ~/.tentaqles/hub/serve-8765.json`},
		{Tool: "Write", Path: filepath.Join(home, "packs", "acme", "ml", "ledger.jsonl")},
		{Tool: "Read", Path: filepath.Join(home, "hub", "serve-8765.json")},
	}
	for _, c := range deny {
		if code, out := hookDecision(t, ws, c); code != 2 {
			t.Errorf("not denied (%d): %s%s\n%s", code, c.Command, c.Path, out)
		}
	}
	ask := []policy.ToolCall{
		{Tool: "Bash", Command: `python -c "open('tq-packs.yaml','w').write('x')"`},
		{Tool: "Bash", Command: `git checkout evil -- tq-packs.yaml`},
		{Tool: "Bash", Command: `git restore --source evil tq-packs.yaml`},
		{Tool: "PowerShell", Command: `Set-Content tq-packs.yaml 'x'`},
		{Tool: "Write", Path: filepath.Join(ws, "tq-packs.yaml")},
	}
	for _, c := range ask {
		if code, out := hookDecision(t, ws, c); code != 0 || !strings.Contains(out, "tq/pack-config-change") {
			t.Errorf("not asked (%d): %s%s\n%s", code, c.Command, c.Path, out)
		}
	}
	for _, c := range []policy.ToolCall{
		{Tool: "Bash", Command: `tq pack approvals --json`},
		{Tool: "Bash", Command: `cat tq-packs.yaml`},
		{Tool: "Bash", Command: `ls`},
	} {
		if code, out := hookDecision(t, ws, c); code != 0 || strings.Contains(out, "tq/hub-store-write") || strings.Contains(out, "tq/human-answer") || strings.Contains(out, "pack-config-change") {
			t.Errorf("flagged (%d): %s\n%s", code, c.Command, out)
		}
	}
}

// N2(c): an Edit or Write through a directory link into the state dir is
// caught on its canonical path.
func TestHookCanonicalisesWrittenPaths(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	os.MkdirAll(filepath.Join(paths.Home(), "packs"), 0o700)
	link := filepath.Join(ws, "lnk")
	if err := makeDirLink(filepath.Join(paths.Home(), "packs"), link); err != nil {
		t.Skipf("no directory links: %v", err)
	}
	code, out := hookDecision(t, ws, policy.ToolCall{Tool: "Write", Path: filepath.Join(link, "acme", "ml", "ledger.jsonl")})
	if code != 2 || !strings.Contains(out, "tq/hub-store-write") {
		t.Fatalf("through a link: %d %s", code, out)
	}
	if s := shortPath(paths.Home()); s != "" && !strings.EqualFold(s, paths.Home()) {
		code, out = hookDecision(t, ws, policy.ToolCall{Tool: "Write", Path: filepath.Join(s, "packs", "x.jsonl")})
		if code != 2 {
			t.Fatalf("short name %s: %d %s", s, code, out)
		}
	}
}

// R9: an untrusted manifest raises packs_config_invalid in triage, and a
// registry that cannot be read is a config error for tq pack.
func TestTriageUntrustedManifestAndBadRegistry(t *testing.T) {
	ws := packRepo(t, "packs:\n  data: { enabled: true }\n")
	os.WriteFile(filepath.Join(ws, "orders.sql"), []byte("select 1\n"), 0o644)
	packGit(t, ws, "add", "orders.sql")
	mp := filepath.Join(ws, ".tentaqles.yaml")
	raw, _ := os.ReadFile(mp)
	os.WriteFile(mp, append(raw, []byte("\n# edited\n")...), 0o600)
	code, out, _ := runHook(t, []string{"decide", "triage", "--staged"}, "")
	if code != 3 || !strings.Contains(out, "packs_config_invalid") {
		t.Fatalf("untrusted: %d %q", code, out)
	}
	os.WriteFile(filepath.Join(paths.Home(), "config.yaml"), []byte("bases: [unterminated\n"), 0o600)
	code, _, errOut := runHook(t, []string{"pack", "check"}, "")
	if code != 2 || !strings.Contains(errOut, "registry") {
		t.Fatalf("bad registry: %d %q", code, errOut)
	}
}

// N1 (nit): baseline_change keeps its spec name; --detail limits the crew.
func TestCrewDetail(t *testing.T) {
	packRepo(t, "packs:\n  data: { enabled: true }\n  db: { enabled: true }\n")
	_, out, _ := runHook(t, []string{"pack", "crew", "--flags", "migration", "--detail", "baseline_change=cloud,db"}, "")
	if strings.TrimSpace(out) != "db-reviewer" {
		t.Fatalf("%q", out)
	}
}
