package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/hooks"
	"github.com/tentaqles/tentaqles/internal/migrate"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// ------------------------------------------------------------------ fixture

// migEnv is a whole synthetic machine: its own HOME (with its own .gitconfig
// and shell profiles), a TQ_HOME *outside* that home so journal writes do not
// show up in a home-tree snapshot, and a workspace base with one trusted
// workspace whose identity directory is a link to a legacy location.
//
// Every external seam the migrate command reaches through is replaced:
// migrateProcesses, migrateReg, migrateNow and hooks.ProfilesFn. Nothing here
// may touch the developer's real gitconfig, identity directories, profiles or
// registry.
type migEnv struct {
	t        *testing.T
	root     string
	home     string
	tqHome   string
	base     string
	gitcfg   string
	profiles hooks.Profiles

	// procs is what the fake process lister returns; procsErr overrides it.
	procs    []string
	procsErr error
	// reg is the fake registry hive: `KEY\NAME` -> value.
	reg map[string]migrate.RegValue
	ts  string
}

const migTestTS = "20260830T101112Z"

func newMigEnv(t *testing.T) *migEnv {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := testutil.TempDir(t)
	e := &migEnv{
		t:      t,
		root:   root,
		home:   filepath.Join(root, "home"),
		tqHome: filepath.Join(root, "tqhome"),
		base:   filepath.Join(root, "home", "repos"),
		reg:    map[string]migrate.RegValue{},
		ts:     migTestTS,
	}
	migMkdir(t, e.home, e.tqHome, e.base)
	e.gitcfg = filepath.Join(e.home, ".gitconfig")

	t.Setenv("HOME", e.home)
	t.Setenv("USERPROFILE", e.home)
	t.Setenv("TQ_HOME", e.tqHome)
	t.Setenv("GIT_CONFIG_GLOBAL", e.gitcfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"__TQ_STATE", "TQ_WS", "TQ_WS_ROOT", "CLAUDE_CONFIG_DIR", "GH_CONFIG_DIR", "XDG_CONFIG_HOME"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}

	// No shell profile exists unless a test writes one, so the shell step is
	// silent by default and can never reach the developer's real profiles.
	e.profiles = hooks.Profiles{
		"bash":       filepath.Join(e.home, ".bashrc"),
		"zsh":        filepath.Join(e.home, ".zshrc"),
		"fish":       filepath.Join(e.home, ".config", "fish", "config.fish"),
		"pwsh":       filepath.Join(e.home, "ps", "pwsh_profile.ps1"),
		"powershell": filepath.Join(e.home, "ps", "wps_profile.ps1"),
	}
	prevProfiles := hooks.ProfilesFn
	hooks.ProfilesFn = func() hooks.Profiles { return e.profiles }
	t.Cleanup(func() { hooks.ProfilesFn = prevProfiles })

	prevProcs, prevReg, prevNow, prevGit := migrateProcesses, migrateReg, migrateNow, migrateGit
	migrateProcesses = func() ([]string, error) {
		if e.procsErr != nil {
			return nil, e.procsErr
		}
		return append([]string(nil), e.procs...), nil
	}
	migrateReg = e.fakeReg
	migrateNow = func() string { return e.ts }
	migrateGit = gitcfg.RunGit
	t.Cleanup(func() {
		migrateProcesses, migrateReg, migrateNow, migrateGit = prevProcs, prevReg, prevNow, prevGit
	})

	prevRunner := uninstallRunner
	uninstallRunner = func() migrate.Runner {
		return migrate.Runner{Git: gitcfg.RunGit, Reg: e.fakeReg}
	}
	t.Cleanup(func() { uninstallRunner = prevRunner })

	// Register the base so registry.Load() inside the command sees it.
	cfg := &registry.Config{}
	if _, err := cfg.AddBase(e.base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	return e
}

// fakeReg is an in-memory reg.exe.
func (e *migEnv) fakeReg(action, key, name string, v migrate.RegValue) (string, error) {
	k := key + `\` + name
	switch action {
	case "query":
		cur, ok := e.reg[k]
		if !ok {
			return "", fmt.Errorf("reg query %s: ERROR: The system was unable to find the specified registry key or value", k)
		}
		return fmt.Sprintf("\r\n%s\r\n    %s    %s    %s\r\n", key, name, cur.Type, cur.Data), nil
	case "set":
		e.reg[k] = v
		return "The operation completed successfully.", nil
	case "delete":
		delete(e.reg, k)
		return "The operation completed successfully.", nil
	}
	return "", fmt.Errorf("reg: unknown action %q", action)
}

func (e *migEnv) addWorkspace(name, gitName, gitEmail string, identities ...string) string {
	e.t.Helper()
	wsRoot := filepath.Join(e.base, name)
	migMkdir(e.t, wsRoot)
	var b strings.Builder
	b.WriteString("schema: tentaqles-client-v2\n")
	fmt.Fprintf(&b, "client: %s\n", name)
	b.WriteString("git:\n")
	fmt.Fprintf(&b, "  name: %s\n", gitName)
	fmt.Fprintf(&b, "  email: %s\n", gitEmail)
	b.WriteString("identities:\n")
	for _, id := range identities {
		fmt.Fprintf(&b, "  %s: {}\n", id)
	}
	mp := filepath.Join(wsRoot, ".tentaqles.yaml")
	migWrite(e.t, mp, b.String())
	h, err := trust.HashFile(mp)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := trust.Allow(h); err != nil {
		e.t.Fatal(err)
	}
	return wsRoot
}

// linkIdentity makes paths.IdentityDir(ws, id) a link to legacy, which it
// creates with a marker file so a move can be proved.
func (e *migEnv) linkIdentity(ws, id, legacy string) string {
	e.t.Helper()
	migMkdir(e.t, legacy)
	migWrite(e.t, filepath.Join(legacy, "marker.txt"), ws+"/"+id)
	dir := paths.IdentityDir(ws, id)
	migMkdir(e.t, filepath.Dir(dir))
	if err := migrate.MakeLink(dir, legacy); err != nil {
		e.t.Skipf("MakeLink(%s -> %s): %v (this platform cannot create links unprivileged)", dir, legacy, err)
	}
	return legacy
}

func migMkdir(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func migWrite(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runTQ drives the root command with buffers, capturing the exit code the
// command would have handed os.Exit.
func runTQ(t *testing.T, args ...string) (code int, out, errOut string, err error) {
	t.Helper()
	prevExit := exitFunc
	exitFunc = func(c int) { code = c }
	t.Cleanup(func() { exitFunc = prevExit })

	var stdout, stderr bytes.Buffer
	root := NewRoot()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err = root.Execute()
	return code, stdout.String(), stderr.String(), err
}

// snapshot records every path under dir with its kind and, for files, a digest.
// Links are recorded by target, not by what they point at, so a directory that
// became a junction (or the reverse) shows up as a difference.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if linked, target := migrate.IsLink(p); linked {
			out[rel] = "link->" + filepath.ToSlash(strings.ToLower(filepath.Clean(target)))
			return fs.SkipDir
		}
		if d.IsDir() {
			out[rel] = "dir"
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			out[rel] = "file?"
			return nil
		}
		sum := sha256.Sum256(b)
		out[rel] = "file:" + hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func diffSnapshots(a, b map[string]string) []string {
	var out []string
	for k, v := range a {
		if w, ok := b[k]; !ok {
			out = append(out, "removed "+k)
		} else if w != v {
			out = append(out, fmt.Sprintf("changed %s: %s -> %s", k, v, w))
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			out = append(out, "added "+k)
		}
	}
	sort.Strings(out)
	return out
}

// legacyBashrc is the shape of a hand-installed tq block, copied from
// internal/hooks/testdata/real_bashrc.
const legacyBashrc = "alias claude='claude --dangerously-skip-permissions'\n" +
	"\n" +
	"# --- tq (managed by Tentaqles; TQ_ENABLED=0 disables) ---\n" +
	"if [ \"${TQ_ENABLED:-1}\" != \"0\" ]; then\n" +
	"  case \":$PATH:\" in *\"/tentaqles/bin:\"*) ;; *) PATH=\"$PATH:$LOCALAPPDATA/tentaqles/bin\";; esac\n" +
	"  command -v tq >/dev/null 2>&1 && eval \"$(tq activate bash)\"\n" +
	"fi\n"

// ------------------------------------------------------------------ dry run

func TestMigrate_DryRunWritesNothing(t *testing.T) {
	e := newMigEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude", "gh")
	e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))
	e.linkIdentity("alpha", "gh", filepath.Join(e.home, ".cli-identities", "alpha"))
	migWrite(t, e.gitcfg, "[user]\n\tname = Old Name\n\temail = old@example.com\n")
	migWrite(t, e.profiles["bash"], legacyBashrc)

	before := snapshot(t, e.root)
	code, out, errOut, err := runTQ(t, "migrate")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v stderr=%q", code, err, errOut)
	}
	if d := diffSnapshots(before, snapshot(t, e.root)); len(d) != 0 {
		t.Fatalf("a dry run changed the tree:\n  %s", strings.Join(d, "\n  "))
	}
	if !strings.Contains(out, "identity: 6 changes") {
		t.Errorf("missing identity header in:\n%s", out)
	}
	if !strings.Contains(out, "! move-dir") {
		t.Errorf("move-dir is not marked as dangerous in:\n%s", out)
	}
	if !strings.Contains(out, "~ remove-link") {
		t.Errorf("missing remove-link line in:\n%s", out)
	}
	if !strings.Contains(out, "~ unset-global       user.email -> \"old@example.com\"") {
		t.Errorf("missing git unset-global line in:\n%s", out)
	}
	if !strings.Contains(out, "~ adopt-hook") {
		t.Errorf("missing shell adopt-hook line in:\n%s", out)
	}
	if !strings.HasSuffix(out, "dry run — nothing changed. Re-run with --apply.\n") {
		t.Errorf("wrong final line in:\n%s", out)
	}
	// The default step set does not include cmd.
	if strings.Contains(out, "cmd:") {
		t.Errorf("cmd ran without being asked for:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(e.tqHome, "backups")); err == nil {
		t.Error("a dry run created a journal directory")
	}
}

func TestMigrate_DryRunStepOrderIsIdentityGitShell(t *testing.T) {
	e := newMigEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))
	migWrite(t, e.gitcfg, "[user]\n\temail = old@example.com\n")

	// Ask for them backwards: Steps() must still run identity, git, shell.
	_, out, _, err := runTQ(t, "migrate", "--steps", "shell,git,identity")
	if err != nil {
		t.Fatal(err)
	}
	iIdent := strings.Index(out, "identity:")
	iGit := strings.Index(out, "git:")
	iShell := strings.Index(out, "shell:")
	if iIdent < 0 || iGit < 0 || iShell < 0 || !(iIdent < iGit && iGit < iShell) {
		t.Fatalf("step order wrong (identity=%d git=%d shell=%d):\n%s", iIdent, iGit, iShell, out)
	}
}

func TestMigrate_UnknownStep(t *testing.T) {
	newMigEnv(t)
	_, _, _, err := runTQ(t, "migrate", "--steps", "identity,banana")
	if err == nil {
		t.Fatal("expected an error for an unknown step")
	}
	if !strings.Contains(err.Error(), `unknown step "banana"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestMigrate_EmptyStepsIsAnError(t *testing.T) {
	newMigEnv(t)
	_, _, _, err := runTQ(t, "migrate", "--steps", " , ")
	if err == nil || !strings.Contains(err.Error(), "at least one step") {
		t.Fatalf("error = %v", err)
	}
}

// ------------------------------------------------------------------ JSON

func TestMigrate_JSONShape(t *testing.T) {
	e := newMigEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))
	migWrite(t, e.gitcfg, "[user]\n\temail = old@example.com\n")

	_, out, _, err := runTQ(t, "migrate", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		TS    string `json:"ts"`
		Steps map[string]struct {
			Changes []struct {
				Step   string `json:"step"`
				Kind   string `json:"kind"`
				Path   string `json:"path"`
				Detail string `json:"detail"`
				Danger bool   `json:"danger"`
			} `json:"changes"`
			Warnings []string `json:"warnings"`
			Skipped  []string `json:"skipped"`
		} `json:"steps"`
		Applied bool `json:"applied"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if doc.Applied {
		t.Error("applied should be false on a dry run")
	}
	if doc.TS != "" {
		t.Errorf(`ts = %q, want "" on a dry run (no journal exists to restore)`, doc.TS)
	}
	for _, want := range []string{"identity", "git", "shell"} {
		if _, ok := doc.Steps[want]; !ok {
			t.Fatalf("steps is missing %q: %s", want, out)
		}
	}
	id := doc.Steps["identity"]
	if len(id.Changes) != 3 {
		t.Fatalf("identity changes = %d, want 3: %s", len(id.Changes), out)
	}
	if id.Changes[1].Kind != "move-dir" || !id.Changes[1].Danger {
		t.Errorf("second identity change = %+v, want a dangerous move-dir", id.Changes[1])
	}
	if id.Warnings == nil || id.Skipped == nil {
		t.Error("warnings/skipped must marshal as [] not null")
	}
	// Nothing capitalised leaks out of the Go structs.
	if strings.Contains(out, `"Changes"`) || strings.Contains(out, `"Kind"`) {
		t.Errorf("JSON uses Go field names: %s", out)
	}
}

func TestMigrate_JSONApplyCarriesTS(t *testing.T) {
	e := newMigEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))

	code, out, errOut, err := runTQ(t, "migrate", "--apply", "--steps", "identity", "--json")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v stderr=%q out=%s", code, err, errOut, out)
	}
	var doc struct {
		TS      string `json:"ts"`
		Applied bool   `json:"applied"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if doc.TS != migTestTS || !doc.Applied {
		t.Fatalf("ts=%q applied=%v", doc.TS, doc.Applied)
	}
}

// ------------------------------------------------------------------ apply

func TestMigrate_ApplyOnSyntheticTree(t *testing.T) {
	e := newMigEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	legacy := e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))
	migWrite(t, e.gitcfg, "[user]\n\tname = Old Name\n\temail = old@example.com\n")
	dir := paths.IdentityDir("alpha", "claude")

	code, out, errOut, err := runTQ(t, "migrate", "--apply")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v stderr=%q out=%s", code, err, errOut, out)
	}
	wantJournal := filepath.Join(e.tqHome, "backups", migTestTS)
	if first := strings.SplitN(out, "\n", 2)[0]; first != "journal: "+wantJournal {
		t.Fatalf("first line = %q, want %q", first, "journal: "+wantJournal)
	}
	if !strings.Contains(out, "applied. undo with: tq uninstall --restore "+migTestTS) {
		t.Errorf("missing the undo hint:\n%s", out)
	}
	// The data moved into tq, and a junction was left behind.
	if linked, _ := migrate.IsLink(dir); linked {
		t.Fatalf("%s is still a link", dir)
	}
	if b, rerr := os.ReadFile(filepath.Join(dir, "marker.txt")); rerr != nil || string(b) != "alpha/claude" {
		t.Fatalf("marker at the new location: %q %v", b, rerr)
	}
	if linked, _ := migrate.IsLink(legacy); !linked {
		t.Fatalf("%s should be a junction back to %s", legacy, dir)
	}
	// The global email is gone and tq's include file is wired in.
	if v, present, gerr := gitcfg.GetGlobal(gitcfg.RunGit, "user.email"); gerr != nil || present {
		t.Fatalf("global user.email still set to %q (err=%v)", v, gerr)
	}
	if _, serr := os.Stat(filepath.Join(wantJournal, "journal.json")); serr != nil {
		t.Fatalf("journal.json: %v", serr)
	}
}

func TestMigrate_ApplyWithCmdStepClearsAutoRun(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the cmd step only runs on Windows")
	}
	e := newMigEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	key := `HKCU\Software\Microsoft\Command Processor`
	e.reg[key+`\AutoRun`] = migrate.RegValue{Type: "REG_EXPAND_SZ", Data: `"%LOCALAPPDATA%\tentaqles\shims\autorun.cmd"`}

	code, out, errOut, err := runTQ(t, "migrate", "--apply", "--steps", "cmd")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v stderr=%q out=%s", code, err, errOut, out)
	}
	if !strings.Contains(out, "! clear-autorun") {
		t.Errorf("clear-autorun should be marked dangerous:\n%s", out)
	}
	if _, still := e.reg[key+`\AutoRun`]; still {
		t.Fatal("AutoRun survived the cmd step; Deps.Reg was probably not wired")
	}
}

// A nil Deps.Reg makes the cmd step skip silently. Proving the command wires
// one is the difference between "clears AutoRun" and "does nothing, quietly".
func TestMigrate_CmdStepHasARegistryRunner(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the cmd step only runs on Windows")
	}
	e := newMigEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	_, out, _, err := runTQ(t, "migrate", "--steps", "cmd")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "no registry runner configured") {
		t.Fatalf("Deps.Reg is nil:\n%s", out)
	}
}

// -------------------------------------------------------- in-use refusal

func TestMigrate_RefusesWhenAProcessHoldsTheDirectory(t *testing.T) {
	e := newMigEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	legacy := e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))
	e.procs = []string{"node.exe C:\\node\\node.exe " + legacy + "\\cli.js"}

	code, out, errOut, _ := runTQ(t, "migrate", "--apply", "--steps", "identity")
	if code != 1 {
		t.Fatalf("code=%d, want 1\nstdout=%s\nstderr=%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "stopped at identity:") {
		t.Fatalf("stderr=%q", errOut)
	}
	if !strings.Contains(errOut, "refusing to move identity directories while they are in use") {
		t.Fatalf("stderr=%q", errOut)
	}
	if !strings.Contains(errOut, "restore with: tq uninstall --restore "+migTestTS) {
		t.Fatalf("stderr is missing the restore hint: %q", errOut)
	}
	// Nothing moved.
	if linked, _ := migrate.IsLink(paths.IdentityDir("alpha", "claude")); !linked {
		t.Fatal("the identity directory was moved despite the refusal")
	}
	// The dry run warned about it up front.
	if !strings.Contains(out, "applying will refuse") {
		t.Errorf("the plan did not warn about the blocker:\n%s", out)
	}
}

func TestMigrate_ForceOverridesTheInUseRefusal(t *testing.T) {
	e := newMigEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	legacy := e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))
	e.procs = []string{"claude.exe " + legacy}

	code, out, errOut, err := runTQ(t, "migrate", "--apply", "--steps", "identity", "--force")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v stderr=%q out=%s", code, err, errOut, out)
	}
	if linked, _ := migrate.IsLink(paths.IdentityDir("alpha", "claude")); linked {
		t.Fatal("--force did not let the move through")
	}
}

// A process list tq cannot read is itself a blocker: silently proceeding would
// move a directory out from under a live process.
func TestMigrate_ProcessListErrorBlocksApply(t *testing.T) {
	e := newMigEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))
	e.procsErr = fmt.Errorf("powershell exploded")

	code, _, errOut, _ := runTQ(t, "migrate", "--apply", "--steps", "identity")
	if code != 1 {
		t.Fatalf("code=%d, want 1 (stderr=%q)", code, errOut)
	}
	if !strings.Contains(errOut, "could not list running processes") {
		t.Fatalf("stderr=%q", errOut)
	}
	if linked, _ := migrate.IsLink(paths.IdentityDir("alpha", "claude")); !linked {
		t.Fatal("the identity directory was moved despite an unreadable process list")
	}
}

// The command must supply Deps.Processes. With a nil one the in-use check is a
// no-op and the refusal above never happens.
func TestMigrate_ProcessesIsWired(t *testing.T) {
	e := newMigEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))
	called := 0
	migrateProcesses = func() ([]string, error) { called++; return nil, nil }

	if _, _, _, err := runTQ(t, "migrate", "--steps", "identity"); err != nil {
		t.Fatal(err)
	}
	if called == 0 {
		t.Fatal("Deps.Processes was never called; the in-use check is dead")
	}
}

// The lister is memoised per invocation: Plan and Apply each ask for it, and
// enumerating processes costs seconds on Windows.
func TestMigrate_ProcessListIsFetchedOnce(t *testing.T) {
	e := newMigEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))
	calls := 0
	migrateProcesses = func() ([]string, error) { calls++; return nil, nil }

	if _, _, errOut, err := runTQ(t, "migrate", "--apply", "--steps", "identity"); err != nil {
		t.Fatalf("%v %s", err, errOut)
	}
	if calls != 1 {
		t.Fatalf("the process list was enumerated %d times, want 1", calls)
	}
}

// ------------------------------------------------------- real process lister

func TestListProcesses_SeesThisTest(t *testing.T) {
	lines, err := listProcesses()
	if err != nil {
		t.Fatalf("listProcesses: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("listProcesses returned nothing; an empty list would silently pass the in-use check")
	}
	// The test binary itself must show up, or the check cannot see `claude`.
	self := strings.ToLower(filepath.Base(os.Args[0]))
	self = strings.TrimSuffix(self, ".exe")
	found := false
	for _, l := range lines {
		if strings.Contains(strings.ToLower(l), self) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the running test binary %q is not in the %d-line process list; sample: %q",
			self, len(lines), lines[:min(5, len(lines))])
	}
}
