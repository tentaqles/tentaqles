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
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/zalando/go-keyring"
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
	clearPackAgentMarkers(t)
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
		{Tool: "Bash", Command: `tq pack cloud apply d123`},
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

// The state-dir rule covers only the protected parts (hub, packs,
// build-gate, trust, guard.yaml); the agents' working area stays open.
func TestHookStateDirOnlyProtectedParts(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	home := paths.Home()
	mem := filepath.Join(home, "identities", "x", "claude", "projects", "p", "memory", "a.md")
	allowed := []policy.ToolCall{
		{Tool: "Write", Path: mem},
		{Tool: "Write", Path: `~/.tentaqles/identities/x/claude/projects/p/memory/a.md`},
		{Tool: "Bash", Command: `cat ~/.tentaqles/identities/x/claude/settings.json`},
		{Tool: "PowerShell", Command: `Get-Content $HOME\.tentaqles\identities\x\claude\settings.json`},
		{Tool: "Bash", Command: `git commit -m "docs: mention ~/.tentaqles in the setup guide"`},
		{Tool: "Bash", Command: `ls ~/.tentaqles/bundles`},
		{Tool: "Bash", Command: `cat ~/.tentaqles/identities/x/claude/projects/hub-notes.md`},
		// A manifest named .tentaqles.yaml is not the state root, even with
		// a packs path elsewhere in the command.
		{Tool: "Bash", Command: `grep -rn x internal/packs/*.go; cat /c/repos/personal/.tentaqles.yaml`},
		{Tool: "PowerShell", Command: `Get-Content C:\repos\acme\.tentaqles.yaml; Select-String x internal\packs\doctor.go`},
		{Tool: "Bash", Command: `ls ~/.tentaqles-old/packs`},
		{Tool: "Bash", Command: `ls ~/.tentaqles.y*ml internal/packs/*.go`},
		{Tool: "Bash", Command: `git add .tentaqles.yaml internal/packs/doctor.go && git status`},
		// Likely false positives of the clear-cut fixes (prose that runs
		// nothing, a plain copy, carets, a test run with its own TQ_HOME).
		{Tool: "Bash", Command: `git commit -m "docs: mention ~/.tentaqles"`},
		{Tool: "Bash", Command: "git commit -m \"$(cat <<'EOF'\ndocs: the guard keeps ~/.tentaqles/packs safe\nEOF\n)\""},
		{Tool: "Bash", Command: `echo hello`},
		{Tool: "Bash", Command: `cp -r src dst`},
		{Tool: "Bash", Command: `cp -r ~/.tentaqles/bundles /tmp/bundles`},
		{Tool: "PowerShell", Command: `$env:TQ_HOME = "$env:TEMP\x"; go test ./...`},
		{Tool: "PowerShell", Command: `New-Item -ItemType Directory -Path $HOME\.tentaqles\bundles\x`},
		{Tool: "PowerShell", Command: `cmd /c dir %USERPROFILE%\.tentaqles\bundles ^| findstr x`},
	}
	for _, c := range allowed {
		if code, out := hookDecision(t, ws, c); code != 0 || strings.Contains(out, "tq/hub-store-write") {
			t.Errorf("denied (%d): %s%s\n%s", code, c.Command, c.Path, out)
		}
	}
	denied := []policy.ToolCall{
		{Tool: "Bash", Command: `cd ~/.tentaqles && echo x >> packs/acme/ml/ledger.jsonl`},
		{Tool: "Bash", Command: `cd ~/.tentaqles; cd packs; echo x >> acme/ml/ledger.jsonl`},
		{Tool: "PowerShell", Command: `Set-Location $HOME\.tentaqles; Add-Content .\hub\acme\x x`},
		{Tool: "Bash", Command: `cat ~/.tentaqles/*/acme/ml/ledger.jsonl`},
		{Tool: "Bash", Command: `echo k > ~/.tentaqles/build-gate/hmac.key`},
		{Tool: "Bash", Command: `cp t ~/.tentaqles/trust/abc`},
		{Tool: "Bash", Command: `ln -s ~/.tentaqles /tmp/t`},
		// Windows drops trailing dots from a component: still the root.
		{Tool: "Bash", Command: `echo x >> ~/.tentaqles./packs/acme/ml/ledger.jsonl`},
		{Tool: "Bash", Command: `cat ~/.tentaqles../packs/acme/ml/ledger.jsonl`},
		{Tool: "Bash", Command: `D=~/.tentaqles.; echo x >> $D/packs/acme/ml/ledger.jsonl`},
		// a manifest beside a real root mention does not hide it
		{Tool: "Bash", Command: `cat .tentaqles.yaml ~/.tentaqles/packs/acme/ml/ledger.jsonl`},
		// A longer name counts as the root when a later step can cut the
		// suffix back off (security review of the name-boundary fix).
		{Tool: "Bash", Command: `echo ~/.tentaqles.x | sed 's/.x/\/packs/' | xargs rm -rf`},
		{Tool: "Bash", Command: `p=~/.tentaqles.yaml; rm -rf ${p%.yaml}/packs`},
		{Tool: "PowerShell", Command: `$p="$HOME/.tentaqles".Trim(); Remove-Item -Recurse -Force "$p/packs"`},
		{Tool: "PowerShell", Command: `$p=$env:TQ_HOME-replace'zz',''; Remove-Item -Recurse "$p/packs"`},
		{Tool: "PowerShell", Command: `set p=%USERPROFILE%\.tentaqles.x & rd /s /q %p:~0,-2%\packs`},
		{Tool: "Bash", Command: `node -e "require('fs').rmSync(require('os').homedir()+'/.tentaqles'.concat('/packs'),{recursive:true})"`},
		{Tool: "PowerShell", Command: `New-Item -ItemType Junction -Path j -Target "$HOME/.tentaqles".Trim()`},
		// a glob class reads as a longer name once normShell drops the brackets
		{Tool: "Bash", Command: `rm -rf ~/.tentaqle[sz]/packs`},
		{Tool: "PowerShell", Command: `Remove-Item -Recurse -Force ~/.tentaqle[sz]/packs`},
		// an interpreter shortens a string without any refused character
		{Tool: "Bash", Command: `ruby -rfileutils -e "FileUtils.rm_rf Dir.home+'/.tentaqlesX'.chop+'/packs'"`},
		{Tool: "Bash", Command: `perl -MFile::Path -e "chdir; rmtree pack q/A10A7/, q/.tentaqlesX/, q:/packs/:"`},
		// a lone CR ends a PowerShell statement: the second one is no reader
		{Tool: "PowerShell", Command: "gc README.md\rruby -rfileutils -e \"FileUtils.rm_rf Dir.home+'/.tentaqlesX'.chop+'/packs'\""},
		{Tool: "Bash", Command: "gc README.md\rruby -rfileutils -e \"FileUtils.rm_rf Dir.home+'/.tentaqlesX'.chop+'/packs'\""},
		{Tool: "PowerShell", Command: "cat README.md ruby -rfileutils -e \"FileUtils.rm_rf Dir.home+'/.tentaqlesX'.chop+'/packs'\""},
		// not only readers: judged as before (fail closed), though no root
		{Tool: "Bash", Command: `rm -rf ~/.tentaqles.x/packs; cat ~/.tentaqles.x`},
		// cmd caret escapes hide a name from the matcher.
		{Tool: "PowerShell", Command: `cmd /c del %USERPROFILE%\.tentaqle^s\packs\acme\ml\ledger.jsonl`},
		{Tool: "PowerShell", Command: `cmd /c del %USERPROFILE%\.tentaqles\pa^cks\acme\ml\ledger.jsonl`},
		// prose that runs something is not prose
		{Tool: "Bash", Command: `echo $(icacls ~/.tentaqles/trust /grant x:F)`},
		{Tool: "Bash", Command: `git commit -m "$(icacls ~/.tentaqles/trust /grant x:F)"`},
		{Tool: "Bash", Command: "echo `icacls ~/.tentaqles/trust /grant x:F`"},
		// link creation spelled other ways
		{Tool: "PowerShell", Command: `New-Item -Type Junction -Path C:\t -Target $HOME\.tentaqles`},
		{Tool: "PowerShell", Command: `New-Item -ItemType:SymbolicLink -Path C:\t -Target $HOME\.tentaqles`},
		{Tool: "PowerShell", Command: `New-Item -It HardLink -Path C:\t -Value $HOME\.tentaqles\x`},
		{Tool: "PowerShell", Command: `ni -ItemType Junction -Path C:\t -Value $HOME\.tentaqles`},
		{Tool: "PowerShell", Command: `fsutil hardlink create C:\t\x %USERPROFILE%\.tentaqles\x`},
		{Tool: "Bash", Command: `cp -rs ~/.tentaqles /tmp/t`},
		{Tool: "Bash", Command: `cp -al ~/.tentaqles /tmp/t`},
		{Tool: "Bash", Command: `cp --symbolic-link ~/.tentaqles/x /tmp/t`},
		{Tool: "Bash", Command: `cp -r --link ~/.tentaqles /tmp/t`},
		{Tool: "Write", Path: filepath.Join(home, "trust", "abc")},
		{Tool: "Write", Path: filepath.Join(home, "build-gate", "hmac.key")},
		{Tool: "Write", Path: filepath.Join(home, "guard.yaml")},
	}
	for _, c := range denied {
		if code, out := hookDecision(t, ws, c); code != 2 {
			t.Errorf("not denied (%d): %s%s\n%s", code, c.Command, c.Path, out)
		}
	}
}

// Fix round 3 (R2-2, R2-3, R2-4, R2-7): the reviewer's bypass table is
// denied; normal agent work and prose stay allowed.
func TestHookRound3Bypasses(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	home := paths.Home()
	cfgDir := filepath.Join(home, "identities", "acme", "claude")
	os.MkdirAll(cfgDir, 0o700)
	t.Setenv("CLAUDE_CONFIG_DIR", cfgDir)
	os.MkdirAll(filepath.Join(home, "build-gate"), 0o700)
	deny := []policy.ToolCall{
		{Tool: "Bash", Command: `echo x >> ~/.tentaqles/identities/../packs/acme/ml/ledger.jsonl`},
		{Tool: "PowerShell", Command: `Add-Content $HOME\.tentaqles\identities\..\packs\acme\ml\ledger.jsonl x`},
		{Tool: "Bash", Command: `echo x >> $CLAUDE_CONFIG_DIR/../../../packs/acme/ml/ledger.jsonl`},
		{Tool: "PowerShell", Command: `Add-Content "$env:CLAUDE_CONFIG_DIR\..\..\..\packs\acme\ml\ledger.jsonl" x`},
		{Tool: "Bash", Command: `D=~/.tentaqles/identities; echo x >> $D/../packs/acme/ml/ledger.jsonl`},
		// R5-1: a quote or backtick between the dots folds back to `..`.
		{Tool: "Bash", Command: `D=~/.tentaqles/identities; echo x >> $D/.''./packs/acme/ml/ledger.jsonl`},
		{Tool: "Bash", Command: `D=~/.tentaqles/identities; echo x >> "$D/."".""/packs/acme/ml/ledger.jsonl"`},
		{Tool: "Bash", Command: `D=~/.tentaqles/identities; echo x >> $D/.` + "``" + `./trust/x`},
		{Tool: "PowerShell", Command: `$D="$HOME\.tentaqles\identities"; Add-Content "$D\.` + "`" + `.\packs\acme\ml\ledger.jsonl" x`},
		{Tool: "PowerShell", Command: `$D="$HOME\.tentaqles\identities"; Set-Content ($D+'\.'+'.\config.yaml') x`},
		{Tool: "Bash", Command: `cmd /c "set D=%USERPROFILE%\.tentaqles\identities&& echo x >> %D%\."".\guard.yaml"`},
		{Tool: "PowerShell", Command: `cmd /c 'set D=%USERPROFILE%\.tentaqles\identities&& echo x >> %D%\.''.\build-gate\k'`},
		{Tool: "Bash", Command: `cd ~/.tentaqles/identities/acme && cd ../.. && echo x >> packs/acme/ml/ledger.jsonl`},
		{Tool: "Bash", Command: `cd ~/.tentaqles/identities; cd ..; echo x >> packs/acme/ml/ledger.jsonl`},
		{Tool: "Bash", Command: `cd $CLAUDE_CONFIG_DIR/../../.. && echo x >> packs/acme/ml/ledger.jsonl`},
		{Tool: "Bash", Command: `python -c "import os;open(os.path.expanduser('~/.tenta'+'qles/packs/acme/ml/ledger.jsonl'),'a').write('x')"`},
		{Tool: "Bash", Command: `python -c "import os;open(os.environ['CLAUDE_CONFIG_DIR']+'/../../../packs/acme/ml/ledger.jsonl','a').write('x')"`},
		{Tool: "Bash", Command: `ln -s ~/.tentaqles/identities/.. /tmp/t && echo x >> /tmp/t/packs/acme/ml/ledger.jsonl`},
		{Tool: "PowerShell", Command: `New-Item -ItemType Junction -Path C:\t -Target $HOME\.tentaqles\identities\..`},
		{Tool: "PowerShell", Command: `cmd /c mklink /J C:\t $env:CLAUDE_CONFIG_DIR\..\..\..`},
		{Tool: "Bash", Command: `cat ~/.tentaqles/identities/../hub/s*.json`},
		{Tool: "Bash", Command: `echo 'bases: []' > ~/.tentaqles/config.yaml`},
		{Tool: "Bash", Command: `echo x >> ~/.tentaqles/audit.jsonl`},
		{Tool: "Bash", Command: `echo x >> ~/.tentaqles/guard/unattended.jsonl`},
		{Tool: "Write", Path: filepath.Join(home, "config.yaml")},
		{Tool: "Bash", Command: `tq hub --workspace acme answer c1 d1 --choice approve`},
		{Tool: "Bash", Command: `t''q hub answer c1 d1 --choice approve`},
		{Tool: "Bash", Command: `T=tq; $T hub answer c1 d1 --choice approve`},
		{Tool: "PowerShell", Command: `& (Get-Command tq).Source hub answer c1 d1 --choice approve`},
		{Tool: "Bash", Command: `python -c "import subprocess;subprocess.run(['tq','hub','answer','c1','d1','--choice','approve'])"`},
		{Tool: "Bash", Command: `curl -b j -H 'Origin: http://127.0.0.1:8765' -d choice=approve 'http://127.0.0.1:8765/cards/c1/%64ecisions/d1/%61nswer?ws=acme'`},
		{Tool: "Bash", Command: `curl -d choice=approve "http://127.0.0.1:8765/cards/c1/deci""sions/d1/answer"`},
		{Tool: "PowerShell", Command: `$u='http://127.0.0.1:8765/cards/c1/deci'+'sions/d1/answer'; Invoke-WebRequest -Method Post $u`},
		{Tool: "PowerShell", Command: `cmdkey /list:tentaqles-packs`},
		{Tool: "Bash", Command: `secret-tool lookup service tentaqles-packs username acme`},
		{Tool: "Bash", Command: `security find-generic-password -s tentaqles-packs -a acme -w`},
		{Tool: "PowerShell", Command: `[Windows.Security.Credentials.PasswordVault]::new().Retrieve('tentaqles-packs','acme')`},
		// cmd caret escapes
		{Tool: "PowerShell", Command: `cmd /c t^q hub an^swer c1 d1 --choice approve`},
	}
	if s := shortPath(filepath.Join(home, "build-gate")); s != "" && !strings.EqualFold(filepath.Base(s), "build-gate") {
		deny = append(deny, policy.ToolCall{Tool: "Bash", Command: `echo k > ~/.tentaqles/` + filepath.Base(s) + `/hmac.key`})
	}
	for _, c := range deny {
		if code, out := hookDecision(t, ws, c); code != 2 {
			t.Errorf("not denied (%d): %s%s\n%s", code, c.Command, c.Path, out)
		}
	}
	allowed := []policy.ToolCall{
		// Round 5 (spec D §9.8): approve and promote request only ask.
		{Tool: "Bash", Command: `tq pack cloud approve --plan p1`},
		{Tool: "Bash", Command: `tq pack db approve m1`},
		{Tool: "Bash", Command: `tq pack ml promote request churn@7`},
		{Tool: "Bash", Command: `git commit -m "docs: the state dir ~/.tentaqles keeps packs and hub data"`},
		{Tool: "Bash", Command: `echo "the hub keeps packs under ~/.tentaqles"`},
		{Tool: "Write", Path: filepath.Join(cfgDir, "projects", "p", "memory", "MEMORY.md")},
		{Tool: "Bash", Command: `ls $CLAUDE_CONFIG_DIR/projects`},
		{Tool: "Bash", Command: `grep -r hub ~/.tentaqles/identities/acme/claude/projects`},
		{Tool: "Bash", Command: `tq hub list`},
		{Tool: "Bash", Command: `tq hub add "answer the question about packs"`},
		{Tool: "Bash", Command: `tq pack approvals --json`},
	}
	for _, c := range allowed {
		if code, out := hookDecision(t, ws, c); code != 0 {
			t.Errorf("denied (%d): %s%s\n%s", code, c.Command, c.Path, out)
		}
	}
}

// F2: a rewritten ledger asked line makes the terminal answer refuse, and the
// decision stays pending for both the old and the new target.
func TestHubAnswerRefusesARewrittenLedger(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "packs:\n  ml: { enabled: true }\n")
	clearPackAgentMarkers(t)
	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	os.Chdir(ws)
	id, card, err := approval.Ask("acme", approval.Request{Pack: "ml", Action: "promote", Subject: "churn@7", Target: "staging", Evidence: packEv})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := ledger.Path("acme", "ml")
	raw, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(raw), `"target":"staging"`, `"target":"production"`, 1)), 0o600)
	prevTTY := stdinIsTerminal
	stdinIsTerminal = func(io.Reader) bool { return true }
	t.Cleanup(func() { stdinIsTerminal = prevTTY })
	if _, err := runRoot(t, "ml promote "+subjectCode("churn@7")+"\n", "hub", "answer", card, id, "--choice", "approve"); !errors.Is(err, ledger.ErrShownMismatch) {
		t.Fatalf("answer over a rewritten ledger: %v", err)
	}
	for _, target := range []string{"staging", "production"} {
		if _, err := approval.Verify("acme", id, approval.Binding{Pack: "ml", Action: "promote", Subject: "churn@7", Target: target}, packEv); err == nil {
			t.Fatalf("%s granted", target)
		}
	}
}

// F6: a guard allow merged with another rule's ask consumes the approval now
// (the hook cannot know whether the call runs) and says so in the prompt.
func TestHookAskConsumesAndSaysSo(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "packs:\n  cloud: { enabled: true }\nguard: { rules: [ { id: x/ask, action: ask, command: approved, reason: confirm } ] }\n")
	signedApproval(t, "acme", "cloud", "d-example")
	code, out := hookDecision(t, ws, policy.ToolCall{Tool: "Bash", Command: "terraform apply approved.tfplan"})
	if code != 0 || !strings.Contains(out, "consumed now, even if you decline") || !ledger.Claimed("acme", "cloud", "d-example") {
		t.Fatalf("%d %s", code, out)
	}
}

// F9: a keychain error makes a pack guard that looks up its approval deny.
func TestHookKeychainErrorDenies(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "packs:\n  db: { enabled: true }\n")
	signedApproval(t, "acme", "db", "d-find")
	keyring.MockInitWithError(errors.New("no secret service"))
	defer keyring.MockInit()
	code, out := hookDecision(t, ws, policy.ToolCall{Tool: "Bash", Command: "find-approval migrate prod"})
	if code != 2 || !strings.Contains(out, "tq/example-cmd-find") {
		t.Fatalf("%d %s", code, out)
	}
}

// R4-3: the hook denies a call that would consume an approval when the
// keychain reads but cannot record the use.
func TestHookDeniesWhenTheMarkCannotBeWritten(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "packs:\n  cloud: { enabled: true }\n")
	signedApproval(t, "acme", "cloud", "d-example")
	restore := ledger.FailKeychainWritesForTest(errors.New("read-only keychain"))
	defer restore()
	code, out := hookDecision(t, ws, policy.ToolCall{Tool: "Bash", Command: "terraform apply approved.tfplan"})
	if code != 2 || !strings.Contains(out, "read-only keychain") {
		t.Fatalf("%d %s", code, out)
	}
}
