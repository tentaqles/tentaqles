package cmd

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// The third guard category, self-protection (PRD v2 §8.2, ADR-0005):
// tq/protected-path denies every read, write, copy, move or delete of a
// protected path in a file tool, an MCP input or a shell command, and
// tq/human-only-command denies a human-only tq verb in the shell. Rules
// are evaluated blocked_commands -> identity -> secrets -> self-protection,
// and every deny is one line in the denial log.

// protectedFixture is a trusted workspace in the protected layout
// (ADR-0002) under an isolated home, with every protected file of §8.2
// present: the manifest, the trust record, the registry, the identity's
// settings.json, the installed plugin (its hooks.json and tq_hook.sh), a
// skill of the plugin, Claude's own memory file, and the marker.
type protectedFixture struct {
	root, home, marker, manifest, trustRecord, config, settings string
	pluginHooks, tqHook, skill, memory                          string
}

// setupProtected builds the fixture; extra is appended to the manifest.
func setupProtected(t *testing.T, extra string) protectedFixture {
	t.Helper()
	name := "acme"
	root := mkUntrusted(t, name)
	body := "schema: tentaqles-client-v2\nclient: " + name + "\ngit: { email: tester@example.com }\nidentities: { claude: {}, codex: {} }\n" + extra
	marker := filepath.Join(root, manifest.FileName)
	if err := os.WriteFile(marker, manifest.MarkerBytes(name), 0o644); err != nil {
		t.Fatal(err)
	}
	mp := paths.ManifestPath(name)
	if err := os.MkdirAll(filepath.Dir(mp), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := trust.HashFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TQ_WS", name)

	claude := paths.IdentityDir(name, "claude")
	plugin := filepath.Join(claude, "plugins", "cache", "tentaqles", "tentaqles", "0.16.0")
	f := protectedFixture{
		root: root, home: paths.Home(), marker: marker, manifest: mp,
		trustRecord: trust.RecordPath(name, h), config: paths.Config(),
		settings:    filepath.Join(claude, "settings.json"),
		pluginHooks: filepath.Join(plugin, "hooks", "hooks.json"),
		tqHook:      filepath.Join(plugin, "scripts", "tq_hook.sh"),
		skill:       filepath.Join(plugin, "skills", "ship", "SKILL.md"),
		memory:      filepath.Join(claude, "projects", "C--repos-acme", "memory", "MEMORY.md"),
	}
	for _, p := range []string{f.settings, f.pluginHooks, f.tqHook, f.skill, f.memory} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Signed last: the integrity record (ADR-0004) covers the identity's
	// settings and the installed plugin, so a workspace signed before they
	// were written would be an integrity mismatch, not a trusted one.
	if err := trust.Allow(name, h); err != nil {
		t.Fatal(err)
	}
	return f
}

// hookDecision runs pre-tool-use --json and parses the decision.
func hookDecision(t *testing.T, cwd, tool, input string, extra ...string) (code int, category, rule, errOut string) {
	t.Helper()
	args := append([]string{"claude-hook", "pre-tool-use", "--json"}, extra...)
	code, out, errOut := runHook(t, args, toolPayload(t, cwd, tool, input))
	var d struct {
		Block    bool   `json:"block"`
		Category string `json:"category"`
		Rule     string `json:"rule"`
	}
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("%s %s: not a decision: %q (%v)", tool, input, out, err)
	}
	if d.Block != (code == 2) {
		t.Fatalf("%s %s: block=%v code=%d", tool, input, d.Block, code)
	}
	return code, d.Category, d.Rule, errOut
}

type protectCase struct {
	name, tool, input string
	rule              string // the denying rule; "" = allowed
}

func pathIn(key, p string) string { return `{"` + key + `":` + jsonPathLit(p) + `}` }

func jsonPathLit(p string) string {
	b, _ := json.Marshal(p)
	return string(b)
}

func cmdIn(cmd string) string { return `{"command":` + jsonPathLit(cmd) + `}` }

// psUnderHome spells a path under the home folder as PowerShell does on this OS: only Windows
// has USERPROFILE and backslash separators.
func psUnderHome(rel string) string {
	if runtime.GOOS == "windows" {
		return `$env:USERPROFILE\` + strings.ReplaceAll(rel, "/", `\`)
	}
	return "$env:HOME/" + rel
}

// protectedCases is the acceptance table of §8.2: one row per write route
// and per protected path kind, the reads that are denied, and what stays
// allowed.
func protectedCases(f protectedFixture) []protectCase {
	const pp = policy.ProtectedPathID
	const ho = policy.HumanOnlyCommandID
	writeContent := `{"file_path":` + jsonPathLit(f.manifest) + `,"content":"SECRET-CONTENT-MARKER"}`
	return []protectCase{
		// file tools: every write route to every kind
		{"Write manifest", "Write", writeContent, pp},
		{"Edit manifest", "Edit", `{"file_path":` + jsonPathLit(f.manifest) + `,"old_string":"a","new_string":"b"}`, pp},
		{"MultiEdit manifest", "MultiEdit", `{"file_path":` + jsonPathLit(f.manifest) + `,"edits":[{"old_string":"a","new_string":"b"}]}`, pp},
		{"NotebookEdit manifest", "NotebookEdit", pathIn("notebook_path", f.manifest), pp},
		{"Write trust record", "Write", pathIn("file_path", f.trustRecord), pp},
		{"Write config.yaml", "Write", pathIn("file_path", f.config), pp},
		{"Write settings.json", "Write", pathIn("file_path", f.settings), pp},
		{"Write plugin hooks.json", "Write", pathIn("file_path", f.pluginHooks), pp},
		{"Edit tq_hook.sh", "Edit", `{"file_path":` + jsonPathLit(f.tqHook) + `,"old_string":"a","new_string":"b"}`, pp},
		{"Write marker", "Write", pathIn("file_path", f.marker), pp},
		{"Edit marker", "Edit", `{"file_path":` + jsonPathLit(f.marker) + `,"old_string":"a","new_string":"b"}`, pp},
		{"Write a tq beside the binary", "Write", pathIn("file_path", filepath.Join(filepath.Dir(testExecutable()), "tq.exe")), pp},
		{"Write a skill of the plugin", "Write", pathIn("file_path", f.skill), pp},
		// shell: redirects, copies, moves, deletes, permissions, links, in-place edits, interpreters, cd, git
		{"redirect to config.yaml", "Bash", cmdIn("echo x > ~/.tentaqles/config.yaml"), pp},
		{"append with $HOME", "Bash", cmdIn("echo x >> $HOME/.tentaqles/config.yaml"), pp},
		{"cp manifest out", "Bash", cmdIn("cp " + f.manifest + " /tmp/m.yaml"), pp},
		{"mv manifest", "Bash", cmdIn("mv " + f.manifest + " " + f.manifest + ".bak"), pp},
		{"rm trust", "Bash", cmdIn("rm -rf ~/.tentaqles/trust"), pp},
		{"rm the state folder", "Bash", cmdIn("rm -rf ~/.tentaqles"), pp},
		{"rm glob", "Bash", cmdIn("rm -rf ~/.tentaqles/*"), pp},
		{"Remove-Item trust", "PowerShell", cmdIn("Remove-Item -Recurse -Force " + psUnderHome(".tentaqles/trust")), pp},
		{"Set-Content config", "PowerShell", cmdIn(`Set-Content -Path "$HOME/.tentaqles/config.yaml" -Value "x"`), pp},
		{"Out-File settings", "PowerShell", cmdIn(`"x" | Out-File -FilePath ` + f.settings), pp},
		{"tee config", "Bash", cmdIn("echo x | tee ~/.tentaqles/config.yaml"), pp},
		{"icacls the state folder", "PowerShell", cmdIn(`icacls "` + f.home + `" /grant Everyone:F`), pp},
		{"chmod trust record", "Bash", cmdIn("chmod 777 " + f.trustRecord), pp},
		{"mklink to config", "PowerShell", cmdIn(`cmd /c mklink C:\tmp\cfg.yaml ` + f.config), pp},
		{"mklink over config", "PowerShell", cmdIn(`mklink ` + f.config + ` C:\tmp\x`), pp},
		{"ln -s config", "Bash", cmdIn("ln -s " + f.config + " ./cfg"), pp},
		{"truncate config", "Bash", cmdIn("truncate -s 0 " + f.config), pp},
		{"sed -i config", "Bash", cmdIn("sed -i 's/a/b/' " + f.config), pp},
		{"python -c open for write", "Bash", cmdIn(`python -c "open('` + filepath.ToSlash(f.config) + `','w').write('x')"`), pp},
		{"node -e writeFileSync", "Bash", cmdIn(`node -e "require('fs').writeFileSync('` + filepath.ToSlash(f.manifest) + `','x')"`), pp},
		{"cd then rm", "Bash", cmdIn("cd ~/.tentaqles && rm config.yaml"), pp},
		{"git -C the state folder", "Bash", cmdIn("git -C ~/.tentaqles init"), pp},
		{"nested shell rm", "Bash", cmdIn(`bash -c "rm -rf ~/.tentaqles"`), pp},
		{"sudo rm", "Bash", cmdIn("sudo rm -rf ~/.tentaqles"), pp},
		{"redirect to the marker", "Bash", cmdIn("echo x > " + f.marker), pp},
		{"cp over the plugin hooks.json", "Bash", cmdIn("cp mine.json " + f.pluginHooks), pp},
		{"rm tq_hook.sh", "Bash", cmdIn("rm " + f.tqHook), pp},
		{"cp a tq beside the binary", "Bash", cmdIn("cp evil.exe " + filepath.Join(filepath.Dir(testExecutable()), "tq.exe")), pp},
		{"IO.File write", "PowerShell", cmdIn(`[IO.File]::WriteAllText('` + f.manifest + `', 'x')`), pp},
		// reads of the state are denied; the marker, the plugin and the binary may be read
		{"Read manifest", "Read", pathIn("file_path", f.manifest), pp},
		{"Read trust record", "Read", pathIn("file_path", f.trustRecord), pp},
		{"Read config.yaml", "Read", pathIn("file_path", f.config), pp},
		{"Read settings.json", "Read", pathIn("file_path", f.settings), pp},
		{"Grep the state folder", "Grep", `{"pattern":"x","path":` + jsonPathLit(f.home) + `}`, pp},
		{"cat manifest", "Bash", cmdIn("cat " + f.manifest), pp},
		{"type manifest", "PowerShell", cmdIn("type " + f.manifest), pp},
		{"Get-Content trust record", "PowerShell", cmdIn("Get-Content " + f.trustRecord), pp},
		{"head config", "Bash", cmdIn("head -c 10 " + f.config), pp},
		{"grep -r the state folder", "Bash", cmdIn("grep -r x ~/.tentaqles"), pp},
		{"stdin redirect from config", "Bash", cmdIn("cat < " + f.config), pp},
		{"cd then cat", "Bash", cmdIn("cd ~/.tentaqles && cat config.yaml"), pp},
		{"python -c open for read", "Bash", cmdIn(`python -c "print(open('` + filepath.ToSlash(f.manifest) + `').read())"`), pp},
		{"IO.File read", "PowerShell", cmdIn(`[IO.File]::ReadAllText('` + f.manifest + `')`), pp},
		// the integrity key: its plain-file fallback and its keychain entry
		{"Write integrity.key", "Write", pathIn("file_path", filepath.Join(f.home, "integrity.key")), pp},
		{"Read integrity.key", "Read", pathIn("file_path", filepath.Join(f.home, "integrity.key")), pp},
		{"cat integrity.key", "Bash", cmdIn("cat ~/.tentaqles/integrity.key"), pp},
		{"cmdkey /generic tentaqles", "PowerShell", cmdIn(`cmdkey /generic:tentaqles:integrity /user:tq /pass:x`), pp},
		{"cmdkey /delete tentaqles", "PowerShell", cmdIn(`cmdkey /delete:tentaqles:integrity`), pp},
		{"security add-generic-password", "Bash", cmdIn("security add-generic-password -U -s tentaqles -a integrity -w x"), pp},
		{"security delete-generic-password", "Bash", cmdIn("security delete-generic-password -s tentaqles -a integrity"), pp},
		{"secret-tool store", "Bash", cmdIn("echo x | secret-tool store --label=tq service tentaqles username integrity"), pp},
		{"secret-tool clear", "Bash", cmdIn("secret-tool clear service tentaqles username integrity"), pp},
		{"cmdkey /list", "PowerShell", cmdIn("cmdkey /list"), ""},
		{"cmdkey another target", "PowerShell", cmdIn("cmdkey /generic:github.com /user:x /pass:y"), ""},
		{"security another service", "Bash", cmdIn("security add-generic-password -s other -a x -w y"), ""},
		{"Read marker", "Read", pathIn("file_path", f.marker), ""},
		{"cat marker", "Bash", cmdIn("cat " + f.marker), ""},
		{"Read a skill of the plugin", "Read", pathIn("file_path", f.skill), ""},
		{"cat tq_hook.sh", "Bash", cmdIn("cat " + f.tqHook), ""},
		{"Read the binary", "Read", pathIn("file_path", testExecutable()), ""},
		// Claude's own working data in the identity's config home is not tq's
		{"Write a memory file", "Write", pathIn("file_path", f.memory), ""},
		{"Edit a memory file", "Edit", `{"file_path":` + jsonPathLit(f.memory) + `,"old_string":"a","new_string":"b"}`, ""},
		// what stays allowed
		{"tq doctor", "Bash", cmdIn("tq doctor"), ""},
		{"tq list", "Bash", cmdIn("tq list --json"), ""},
		{"tq env", "Bash", cmdIn("tq env"), ""},
		{"tq migrate dry run", "Bash", cmdIn("tq migrate"), ""},
		{"tq migrate manifests dry run", "Bash", cmdIn("tq migrate manifests"), ""},
		{"tq settings render --dry-run", "Bash", cmdIn("tq settings render --dry-run acme"), ""},
		{"tq hooks verify", "Bash", cmdIn("tq hooks verify"), ""},
		{"tq secret status", "Bash", cmdIn("tq secret status acme jira"), ""},
		{"tq allow --help", "Bash", cmdIn("tq allow --help"), ""},
		{"ls the state folder", "Bash", cmdIn("ls -la ~/.tentaqles"), ""},
		{"redirect elsewhere", "Bash", cmdIn("echo x > /tmp/notes.txt"), ""},
		{"cat elsewhere", "Bash", cmdIn("cat README.md"), ""},
		{"Write in the workspace", "Write", pathIn("file_path", filepath.Join(f.root, "notes.md")), ""},
		{"git status", "Bash", cmdIn("git status"), ""},
		{"cp a template", "Bash", cmdIn("cp .env.example .env"), ""},
		{"python a script", "Bash", cmdIn("python scripts/build.py --out dist"), ""},
		{"a note mentioning the folder", "Bash", cmdIn("cat > notes.md <<'EOF'\nrm -rf ~/.tentaqles\nEOF"), ""},
		// human-only tq commands
		{"tq allow", "Bash", cmdIn("tq allow acme"), ho},
		{"tq deny", "Bash", cmdIn("tq deny acme"), ho},
		{"tq.exe by path", "PowerShell", cmdIn(`C:\tools\tq.exe hooks install`), ho},
		{"./tq init", "Bash", cmdIn("./tq init C:/work"), ho},
		{"absolute tq add", "Bash", cmdIn("/usr/local/bin/tq add beta --base /work"), ho},
		{"tq migrate --apply", "Bash", cmdIn("tq migrate --apply"), ho},
		{"tq migrate manifests --apply", "Bash", cmdIn("tq migrate manifests --apply"), ho},
		{"go run ./cmd/tq allow", "Bash", cmdIn("go run ./cmd/tq allow acme"), ho},
		{"sudo tq deny", "Bash", cmdIn("sudo tq deny acme"), ho},
		{"env tq allow", "Bash", cmdIn("env X=1 tq allow acme"), ho},
		{"tq secret set", "Bash", cmdIn("tq secret set acme jira"), ho},
		{"tq run --off", "Bash", cmdIn("tq run --off -- claude"), ho},
		{"tq hooks verify --accept", "Bash", cmdIn("tq hooks verify --accept"), ho},
		{"tq settings render", "Bash", cmdIn("tq settings render acme"), ho},
		{"tq settings anchors add", "Bash", cmdIn("tq settings anchors add foo-term"), ho},
		{"tq bundle sync", "Bash", cmdIn("tq bundle sync acme"), ho},
		{"tq providers add", "Bash", cmdIn("tq providers add widget --name Widget"), ho},
		{"tq worktrees prune", "Bash", cmdIn("tq worktrees prune --yes"), ho},
		{"tq uninstall", "Bash", cmdIn("tq uninstall --restore latest"), ho},
		{"tq setup", "Bash", cmdIn("tq setup"), ho},
		{"tq onboard", "Bash", cmdIn("tq onboard acme"), ho},
		{"tq login", "Bash", cmdIn("tq login acme gh"), ho},
		{"tq off", "Bash", cmdIn("tq off"), ho},
		{"tq edit", "Bash", cmdIn("tq edit acme"), ho},
		{"tq trust", "Bash", cmdIn("tq trust acme"), ho},
		{"nested tq allow", "Bash", cmdIn(`bash -c "tq allow acme"`), ho},
		{"cmd /c tq allow", "PowerShell", cmdIn("cmd /c tq allow acme"), ho},
		{"tq allow after &&", "Bash", cmdIn("go test ./... && tq allow acme"), ho},
	}
}

// testExecutable is the running test binary: the hook treats it as the tq
// binary (hookExecutable), so the binary rows need no real install.
func testExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe
}

func TestProtectedPaths_Table(t *testing.T) {
	f := setupProtected(t, "")
	t.Setenv("TQ_TEST_CANARY", "canary-value-xyz")
	denied := 0
	for _, c := range protectedCases(f) {
		t.Run(c.name, func(t *testing.T) {
			code, category, rule, errOut := hookDecision(t, f.root, c.tool, c.input)
			switch {
			case c.rule == "":
				if code != 0 || rule != "" || errOut != "" {
					t.Fatalf("want allow, got code=%d rule=%q stderr=%q", code, rule, errOut)
				}
			default:
				if code != 2 || rule != c.rule || category != policy.CategorySelfProtection {
					t.Fatalf("want deny by %s, got code=%d category=%q rule=%q stderr=%q", c.rule, code, category, rule, errOut)
				}
				if !strings.HasPrefix(errOut, "BLOCKED [self-protection/"+c.rule+"]: ") {
					t.Fatalf("the deny names the category and the rule: %q", errOut)
				}
				if strings.Contains(errOut, "SECRET-CONTENT-MARKER") {
					t.Fatalf("the deny echoes the content: %q", errOut)
				}
			}
		})
		if c.rule != "" {
			denied++
		}
	}

	// The denial log: one line per deny, with the category, the layer, the
	// tool, the command or path, the rule and the workspace; never the
	// content written or the environment.
	ds := readDenials(t)
	if len(ds) != denied {
		t.Fatalf("denials logged = %d, want %d", len(ds), denied)
	}
	for _, d := range ds {
		wantLayer := audit.LayerHook
		if d.Rule == policy.ProtectedPathID {
			wantLayer = audit.LayerFilesystem
		}
		if d.Category != policy.CategorySelfProtection || d.Layer != wantLayer || d.Tool == "" || d.Command == "" ||
			(d.Rule != policy.ProtectedPathID && d.Rule != policy.HumanOnlyCommandID) || d.Identity != "acme" || d.Reason == "" || d.Cwd == "" {
			t.Fatalf("denial %+v", d)
		}
	}
	raw, err := os.ReadFile(audit.DenialsPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"SECRET-CONTENT-MARKER", "canary-value-xyz"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("the denial log carries %s", leak)
		}
	}
}

// Rules are evaluated blocked_commands -> identity -> secrets ->
// self-protection; the first category that matches is the one reported.
func TestProtectedPaths_RuleOrder(t *testing.T) {
	f := setupProtected(t, "blocked_commands: [\"rm -rf\"]\ncloud:\n  provider: azure\n")
	cases := []struct {
		name, tool, cmd, category, rule string
	}{
		{"blocked command before the protected path", "Bash", "rm -rf ~/.tentaqles", "identity", "blocked-command"},
		{"identity before the protected path", "Bash", "aws s3 cp " + f.manifest + " s3://bucket/m.yaml", "identity", "wrong-cloud"},
		{"identity before secrets", "Bash", "cat .env && aws s3 ls", "identity", "wrong-cloud"},
		{"secrets before the protected path", "Bash", "cat ~/.tentaqles/bundles/catalog.yaml", "secrets", "tq/secret-store-shell"},
		{"secrets before the protected path (file tool)", "Read", filepath.Join(f.home, "bundles", "catalog.yaml"), "secrets", "tq/secret-store-read"},
		{"the protected path when nothing else matches", "Bash", "cp " + f.manifest + " /tmp/m.yaml", "self-protection", "tq/protected-path"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			input := cmdIn(c.cmd)
			if c.tool == "Read" {
				input = pathIn("file_path", c.cmd)
			}
			code, category, rule, errOut := hookDecision(t, f.root, c.tool, input)
			if code != 2 || rule != c.rule || category != c.category {
				t.Fatalf("got code=%d category=%q rule=%q stderr=%q", code, category, rule, errOut)
			}
			if !strings.HasPrefix(errOut, "BLOCKED ["+c.category+"/"+c.rule+"]: ") {
				t.Fatalf("prefix: %q", errOut)
			}
		})
	}
	ds := readDenials(t)
	if len(ds) != len(cases) {
		t.Fatalf("denials logged = %d, want %d", len(ds), len(cases))
	}
	for i, d := range ds {
		wantLayer := audit.LayerHook
		if d.Rule == policy.ProtectedPathID {
			wantLayer = audit.LayerFilesystem
		}
		if d.Category != cases[i].category || d.Rule != cases[i].rule || d.Layer != wantLayer || d.Identity != "acme" {
			t.Fatalf("denial %d: %+v", i, d)
		}
	}
}

// The identity guard's blocks and the secret rules name their category too.
func TestDenyPrefixNamesTheCategory(t *testing.T) {
	isolateHome(t)
	dir := testutil.TempDir(t)
	_, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, dir, "git push"))
	if !strings.HasPrefix(errOut, "BLOCKED [identity/neutral-remote]: ") {
		t.Fatalf("identity: %q", errOut)
	}
	_, _, errOut = runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, dir, "printenv"))
	if !strings.HasPrefix(errOut, "BLOCKED [secrets/tq/env-dump]: ") {
		t.Fatalf("secrets: %q", errOut)
	}
	ds := readDenials(t)
	// The log carries the redacted command (ADR-0006).
	if len(ds) != 2 || ds[0].Category != "identity" || ds[0].Rule != "neutral-remote" || ds[0].Command != "git push" ||
		ds[1].Category != "secrets" || ds[1].Rule != "tq/env-dump" || ds[1].Command != "printenv" || ds[1].Tool != "Bash" || ds[0].Layer != audit.LayerHook {
		t.Fatalf("denials %+v", ds)
	}
}

// The same rules run for Codex: a patch that updates the manifest and a
// human-only tq command are Codex denies.
func TestProtectedPaths_Codex(t *testing.T) {
	f := setupProtected(t, "")
	patch := "*** Begin Patch\n*** Update File: " + f.manifest + "\n@@\n-a\n+b\n*** End Patch\n"
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, applyPatchPayload(t, f.root, patch))
	if dec, reason := codexAnswerJSON(t, out); code != 2 || dec != "deny" || !strings.Contains(reason, "self-protection/"+policy.ProtectedPathID) {
		t.Fatalf("apply_patch on the manifest: code %d out %q", code, out)
	}
	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, hookPayloadFor(t, f.root, "tq allow acme"))
	if dec, reason := codexAnswerJSON(t, out); code != 2 || dec != "deny" || !strings.Contains(reason, "self-protection/"+policy.HumanOnlyCommandID) {
		t.Fatalf("tq allow under codex: code %d out %q", code, out)
	}
	ok := "*** Begin Patch\n*** Add File: notes.md\n+hello\n*** End Patch\n"
	if code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, applyPatchPayload(t, f.root, ok)); code != 0 || out != "" {
		t.Fatalf("a plain patch: code %d out %q", code, out)
	}
	ds := readDenials(t)
	// The log records the tool as the command and "apply_patch <file>" as
	// the subject, never the patch text.
	if len(ds) != 2 || ds[0].Command != "apply_patch" || ds[0].Subject != "apply_patch "+f.manifest || ds[0].Tool != "apply_patch" || strings.Contains(ds[0].Subject, "Begin Patch") {
		t.Fatalf("the log records the file, never the patch: %+v", ds)
	}
	if ds[1].Subject != "tq allow" || ds[1].Command != "tq allow …" {
		t.Fatalf("the human-only line: subject %q command %q", ds[1].Subject, ds[1].Command)
	}
}

// An MCP input that names a protected path is denied, whatever the server.
func TestProtectedPaths_MCPInput(t *testing.T) {
	f := setupProtected(t, "")
	code, category, rule, _ := hookDecision(t, f.root, "mcp__filesystem__write_file", `{"path":`+jsonPathLit(f.manifest)+`,"content":"x"}`)
	if code != 2 || rule != policy.ProtectedPathID || category != policy.CategorySelfProtection {
		t.Fatalf("code=%d category=%q rule=%q", code, category, rule)
	}
	if code, _, rule, _ := hookDecision(t, f.root, "mcp__filesystem__read_file", `{"path":`+jsonPathLit(filepath.Join(f.root, "README.md"))+`}`); code != 0 || rule != "" {
		t.Fatalf("a workspace file: code=%d rule=%q", code, rule)
	}
	ds := readDenials(t)
	if len(ds) != 1 || ds[0].Command != "mcp__filesystem__write_file" {
		t.Fatalf("the log records the tool, never the input: %+v", ds)
	}
}

// The marker of any registered workspace is protected, not only the
// resolved one's, and the state folder is protected from a neutral cwd.
func TestProtectedPaths_OtherWorkspaceAndNeutralCwd(t *testing.T) {
	f := setupProtected(t, "")
	cfg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(cfg.Bases[0], "beta")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if code, _, rule, _ := hookDecision(t, f.root, "Write", pathIn("file_path", filepath.Join(other, manifest.FileName))); code != 2 || rule != policy.ProtectedPathID {
		t.Fatalf("another workspace's marker: code=%d rule=%q", code, rule)
	}
	neutral := testutil.TempDir(t)
	if code, _, rule, _ := hookDecision(t, neutral, "Write", pathIn("file_path", f.config)); code != 2 || rule != policy.ProtectedPathID {
		t.Fatalf("the registry from a neutral cwd: code=%d rule=%q", code, rule)
	}
	if code, _, rule, _ := hookDecision(t, neutral, "Bash", cmdIn("tq allow acme")); code != 2 || rule != policy.HumanOnlyCommandID {
		t.Fatalf("tq allow from a neutral cwd: code=%d rule=%q", code, rule)
	}
}

// The denial log records the command redacted (PRD v2 §7.3, ADR-0006) and,
// as subject, the verb (after sudo/env/assignments are stripped) plus the
// matched path (ADR-0005). A here-doc body, an assignment value, a
// credential flag's value or a quoted secret never reaches the log, in
// every category's line.
func TestDenialLogRedaction(t *testing.T) {
	f := setupProtected(t, "blocked_commands: [\"gh \"]\n")
	cfgPath := filepath.Join(f.home, "config.yaml")
	cases := []struct {
		name, tool, cmd, wantPrefix, wantRule, wantKind string
	}{
		{"heredoc password", "Bash", "cat > ~/.tentaqles/config.yaml <<'EOF'\npassword: HUNTER2-PASTED\nEOF", "cat ", policy.ProtectedPathID, "self-protection"},
		{"leading assignment", "Bash", "GH_TOKEN=tok-INLINE-9 rm ~/.tentaqles/config.yaml", "rm ", policy.ProtectedPathID, "self-protection"},
		{"export then rm", "Bash", "export GH_TOKEN=tok-EXPORT-9; rm ~/.tentaqles/config.yaml", "", policy.ProtectedPathID, "self-protection"},
		{"ps env assignment then remove", "PowerShell", "$env:GH_TOKEN='tok-PS-9'; Remove-Item ~/.tentaqles/config.yaml", "remove-item ", policy.ProtectedPathID, "self-protection"},
		{"auth header", "Bash", "curl -H 'Authorization: Bearer tok-HEADER-9' -o ~/.tentaqles/config.yaml https://x", "curl ", policy.ProtectedPathID, "self-protection"},
		{"identity blocked", "Bash", "gh pr merge 1", "gh", "blocked-command", "identity"},
	}
	secrets := []string{"HUNTER2-PASTED", "tok-INLINE-9", "tok-EXPORT-9", "tok-PS-9", "tok-HEADER-9"}
	for _, c := range cases {
		if code, _, rule, _ := hookDecision(t, f.root, c.tool, cmdIn(c.cmd)); code != 2 || rule != c.wantRule {
			t.Fatalf("%s: code=%d rule=%q", c.name, code, rule)
		}
	}
	ds := readDenials(t)
	if len(ds) != len(cases) {
		t.Fatalf("want %d denials, got %d", len(cases), len(ds))
	}
	for i, c := range cases {
		d := ds[i]
		if d.Category != c.wantKind {
			t.Errorf("%s: kind=%q want %q", c.name, d.Category, c.wantKind)
		}
		if c.wantPrefix != "" && !strings.HasPrefix(d.Subject, c.wantPrefix) && !strings.HasPrefix(d.Command, c.wantPrefix) {
			t.Errorf("%s: subject %q command %q, want prefix %q", c.name, d.Subject, d.Command, c.wantPrefix)
		}
		// Every protected-path line's subject names the matched config path.
		if c.wantRule == policy.ProtectedPathID && !strings.Contains(strings.ToLower(d.Subject), strings.ToLower(cfgPath)) {
			t.Errorf("%s: subject %q should name the matched path", c.name, d.Subject)
		}
	}
	raw, err := os.ReadFile(audit.DenialsPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range secrets {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("the denial log carries %q", leak)
		}
	}
}

// Grep and Glob rooted at the home folder (an ancestor of the state folder)
// are denied — the recursive search would read tq's state — while a search
// inside the workspace is allowed (ADR-0005 round 3).
func TestProtectedPaths_AncestorSearch(t *testing.T) {
	f := setupProtected(t, "")
	home := filepath.Dir(f.home) // the user home, parent of ~/.tentaqles
	for _, tc := range []struct {
		name, tool, input string
		deny              bool
	}{
		{"Grep at home", "Grep", `{"pattern":"email","path":` + jsonPathLit(home) + `}`, true},
		{"Glob at home", "Glob", `{"pattern":"**/manifest.yaml","path":` + jsonPathLit(home) + `}`, true},
		{"grep -r at home", "Bash", cmdIn("grep -r email " + home), true},
		{"Grep in the workspace", "Grep", `{"pattern":"x","path":` + jsonPathLit(f.root) + `}`, false},
		{"grep -r in a project", "Bash", cmdIn("grep -r foo src"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _, rule, _ := hookDecision(t, f.root, tc.tool, tc.input)
			if tc.deny && (code != 2 || rule != policy.ProtectedPathID) {
				t.Fatalf("want deny, code=%d rule=%q", code, rule)
			}
			if !tc.deny && code != 0 {
				t.Fatalf("want allow, code=%d rule=%q", code, rule)
			}
		})
	}
}

// The Glob tool is guarded like Grep: a pattern that resolves under tq's
// state is a denied read; one in the workspace is allowed.
func TestProtectedPaths_GlobTool(t *testing.T) {
	f := setupProtected(t, "")
	if code, _, rule, _ := hookDecision(t, f.root, "Glob", pathIn("pattern", filepath.Join(f.home, "**", "*.yaml"))); code != 2 || rule != policy.ProtectedPathID {
		t.Fatalf("glob into the state folder: code=%d rule=%q", code, rule)
	}
	if code, _, rule, _ := hookDecision(t, f.root, "Glob", `{"pattern":"**/*.go"}`); code != 0 || rule != "" {
		t.Fatalf("glob in the workspace: code=%d rule=%q", code, rule)
	}
}

// The tentaqles marketplace clone (P1.4's trust anchor for batch accepts) is
// write-protected like the plugin cache: writes, deletes and git -C into it
// are denied, reads are allowed. The plugin's pycache sits inside the state
// folder, so a Write there is denied too.
func TestProtectedPaths_MarketplaceCloneAndPycache(t *testing.T) {
	f := setupProtected(t, "")
	clone := filepath.Join(paths.IdentityDir("acme", "claude"), "plugins", "marketplaces", "tentaqles")
	hooksFile := filepath.Join(clone, "plugin", "hooks", "hooks.json")
	pyc := filepath.Join(f.home, "pycache", "tentaqles", "x.cpython-312.pyc")
	for _, tc := range []struct {
		name, tool, input string
		deny              bool
	}{
		{"Write in the clone", "Write", pathIn("file_path", hooksFile), true},
		{"Edit in the clone", "Edit", `{"file_path":` + jsonPathLit(hooksFile) + `,"old_string":"a","new_string":"b"}`, true},
		{"rm in the clone", "Bash", cmdIn("rm -rf " + clone), true},
		{"git -C the clone", "Bash", cmdIn("git -C " + clone + " checkout main"), true},
		{"git -C the clone, read", "Bash", cmdIn("git -C " + clone + " log -1"), true},
		{"Read in the clone", "Read", pathIn("file_path", hooksFile), false},
		{"Write in pycache", "Write", pathIn("file_path", pyc), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _, rule, _ := hookDecision(t, f.root, tc.tool, tc.input)
			if tc.deny && (code != 2 || rule != policy.ProtectedPathID) {
				t.Fatalf("want deny, code=%d rule=%q", code, rule)
			}
			if !tc.deny && code != 0 {
				t.Fatalf("want allow, code=%d rule=%q", code, rule)
			}
		})
	}
}

// A PowerShell -EncodedCommand reaches the hook decoded (ADR-0005).
func TestProtectedPaths_EncodedCommand(t *testing.T) {
	f := setupProtected(t, "")
	ps := "Remove-Item " + filepath.Join(f.home, "config.yaml")
	u := utf16.Encode([]rune(ps))
	raw := make([]byte, len(u)*2)
	for i, r := range u {
		raw[2*i] = byte(r)
		raw[2*i+1] = byte(r >> 8)
	}
	b64 := base64.StdEncoding.EncodeToString(raw)
	if code, _, rule, _ := hookDecision(t, f.root, "PowerShell", cmdIn("powershell -EncodedCommand "+b64)); code != 2 || rule != policy.ProtectedPathID {
		t.Fatalf("encoded protected write: code=%d rule=%q", code, rule)
	}
}
