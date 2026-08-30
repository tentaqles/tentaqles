package migrate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// ---------------------------------------------------------------- fixture

// env is one migrated machine, built entirely out of temp directories: a home
// with its own ~/.gitconfig, a tq home outside it, and a workspace base.
//
// TQ_HOME lives *outside* HOME on purpose. The journal writes its backups under
// TQ_HOME, and the restore assertions hash the whole home tree; a journal
// inside it would change the hash for reasons that have nothing to do with the
// migration.
type stepEnv struct {
	t      *testing.T
	root   string
	home   string
	tqHome string
	base   string
	gitcfg string
	cfg    *registry.Config
}

func newStepEnv(t *testing.T) *stepEnv {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := stTmpDir(t)
	e := &stepEnv{
		t:      t,
		root:   root,
		home:   filepath.Join(root, "home"),
		tqHome: filepath.Join(root, "tqhome"),
		base:   filepath.Join(root, "home", "repos"),
	}
	stMkdir(t, e.home, e.tqHome, e.base)
	e.gitcfg = filepath.Join(e.home, ".gitconfig")

	t.Setenv("HOME", e.home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", e.home)
	}
	t.Setenv("TQ_HOME", e.tqHome)
	t.Setenv("GIT_CONFIG_GLOBAL", e.gitcfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	// Nothing below should ever reach the developer's real state.
	for _, k := range []string{"__TQ_STATE", "TQ_WS", "TQ_WS_ROOT", "CLAUDE_CONFIG_DIR", "GH_CONFIG_DIR"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	e.cfg = &registry.Config{Bases: []string{stNormalize(t, e.base)}}
	return e
}

func stTmpDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if r, err := filepath.EvalSymlinks(d); err == nil {
		d = r
	}
	return d
}

func stNormalize(t *testing.T, p string) string {
	t.Helper()
	n, err := registry.Normalize(p)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func stMkdir(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func stWrite(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// addWorkspace creates a workspace with a manifest, trusts it, and returns its
// root. identities are the CLI names its manifest declares.
func (e *stepEnv) addWorkspace(name, gitName, gitEmail string, identities ...string) string {
	e.t.Helper()
	root := filepath.Join(e.base, name)
	stMkdir(e.t, root)
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
	mp := filepath.Join(root, ".tentaqles.yaml")
	stWrite(e.t, mp, b.String())
	h, err := trust.HashFile(mp)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := trust.Allow(h); err != nil {
		e.t.Fatal(err)
	}
	return root
}

// linkIdentity makes paths.IdentityDir(ws, id) a link to a legacy directory at
// legacy, which it creates with a marker file so a move can be proved.
func (e *stepEnv) linkIdentity(ws, id, legacy string) string {
	e.t.Helper()
	stMkdir(e.t, legacy)
	stWrite(e.t, filepath.Join(legacy, "marker.txt"), ws+"/"+id)
	dir := paths.IdentityDir(ws, id)
	stMkdir(e.t, filepath.Dir(dir))
	if err := MakeLink(dir, legacy); err != nil {
		e.t.Fatalf("MakeLink(%s -> %s): %v", dir, legacy, err)
	}
	return legacy
}

// deps returns Deps wired to the real git binary (pointed at the fixture's
// isolated global config by GIT_CONFIG_GLOBAL) and no running processes.
func (e *stepEnv) deps() Deps {
	return Deps{
		Cfg:       e.cfg,
		Git:       gitcfg.RunGit,
		Env:       os.LookupEnv,
		Processes: func() ([]string, error) { return nil, nil },
	}
}

func (e *stepEnv) journal() *Journal {
	e.t.Helper()
	j, err := Open("20260830T000000Z")
	if err != nil {
		e.t.Fatal(err)
	}
	return j
}

// doctorFindings runs doctor against the fixture and returns the codes it
// reported. doctor is imported by the test, never by the package: migrate must
// not depend on doctor, but proving the migration clears doctor's drift codes
// is exactly what this direction is for.
func (e *stepEnv) doctorFindings(cwd string) map[string]string {
	e.t.Helper()
	fs := doctor.Run(e.cfg, doctor.Deps{
		Env:      os.LookupEnv,
		Cwd:      cwd,
		RunGit:   gitcfg.RunGit,
		RunGitIn: gitcfg.RunGitIn,
		LookPath: exec.LookPath,
	})
	out := map[string]string{}
	for _, f := range fs {
		out[f.Code] = f.Msg
	}
	return out
}

// migrationDriftCodes are the doctor findings `tq migrate --steps identity,git`
// exists to clear.
var migrationDriftCodes = []string{
	"global-email-set",
	"includeif-unmanaged",
	"include-orphan",
	"identity-dir-linked",
	"git-ws-file-tampered",
}

func stKinds(p Plan) []string {
	out := make([]string, 0, len(p.Changes))
	for _, c := range p.Changes {
		out = append(out, c.Kind+" "+c.Path)
	}
	return out
}

func stHasChange(p Plan, kind, pathSubstr string) bool {
	for _, c := range p.Changes {
		if c.Kind == kind && strings.Contains(normKey(c.Path), normKey(pathSubstr)) {
			return true
		}
	}
	return false
}

func stJoin(ss []string) string { return "\n  " + strings.Join(ss, "\n  ") }

func stContains(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- identity

func TestIdentityStep_PlanListsRemoveMoveMakePerLinkedDir(t *testing.T) {
	e := newStepEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude", "gh")
	legacyClaude := e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))
	legacyGH := e.linkIdentity("alpha", "gh", filepath.Join(e.home, ".cli-identities", "alpha"))
	// A directory that is not a link is left alone entirely.
	stMkdir(t, paths.IdentityDir("alpha", "az"))

	p, err := stepRegistry["identity"].Plan(e.deps())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 6 {
		t.Fatalf("want 6 changes (3 per linked dir), got %d:%s", len(p.Changes), stJoin(stKinds(p)))
	}
	for _, c := range []struct{ kind, path string }{
		{"remove-link", paths.IdentityDir("alpha", "claude")},
		{"move-dir", legacyClaude},
		{"make-link", legacyClaude},
		{"remove-link", paths.IdentityDir("alpha", "gh")},
		{"move-dir", legacyGH},
		{"make-link", legacyGH},
	} {
		if !stHasChange(p, c.kind, c.path) {
			t.Errorf("missing change %s %s; got:%s", c.kind, c.path, stJoin(stKinds(p)))
		}
	}
	// move-dir is the one that touches real data.
	for _, c := range p.Changes {
		if c.Kind == "move-dir" && !c.Danger {
			t.Errorf("move-dir %s should be marked Danger", c.Path)
		}
	}
}

func TestIdentityStep_ApplyMovesDataAndLeavesJunctionBehind(t *testing.T) {
	e := newStepEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	legacy := e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))
	dir := paths.IdentityDir("alpha", "claude")

	d := e.deps()
	st := stepRegistry["identity"]
	p, err := st.Plan(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Apply(d, p, e.journal()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// The data is now inside tq, and the old path is a junction to it.
	if linked, _ := IsLink(dir); linked {
		t.Fatalf("%s is still a link after apply", dir)
	}
	if got := stRead(t, filepath.Join(dir, "marker.txt")); got != "alpha/claude" {
		t.Fatalf("marker at new location = %q", got)
	}
	linked, tgt := IsLink(legacy)
	if !linked {
		t.Fatalf("%s should be a junction back to %s", legacy, dir)
	}
	if !strings.EqualFold(filepath.Clean(tgt), filepath.Clean(dir)) {
		t.Fatalf("junction %s points at %q, want %q", legacy, tgt, dir)
	}
	// Reading through the old path still works.
	if got := stRead(t, filepath.Join(legacy, "marker.txt")); got != "alpha/claude" {
		t.Fatalf("marker through old path = %q", got)
	}
}

func TestIdentityStep_RefusesWhileTheCLIIsRunning(t *testing.T) {
	e := newStepEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))

	d := e.deps()
	d.Processes = func() ([]string, error) {
		return []string{"explorer.exe", `C:\Users\x\AppData\Local\claude\claude.exe --resume`}, nil
	}
	st := stepRegistry["identity"]
	p, err := st.Plan(d)
	if err != nil {
		t.Fatal(err)
	}
	if !stContains(p.Warnings, "applying will refuse") {
		t.Errorf("plan should warn that apply will refuse; warnings:%s", stJoin(p.Warnings))
	}
	err = st.Apply(d, p, e.journal())
	if err == nil {
		t.Fatal("Apply must refuse while claude is running")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("refusal should mention --force, got: %v", err)
	}
	// Nothing was touched.
	if linked, _ := IsLink(paths.IdentityDir("alpha", "claude")); !linked {
		t.Fatal("identity dir was modified despite the refusal")
	}

	// --force gets past it.
	d.Force = true
	if err := st.Apply(d, p, e.journal()); err != nil {
		t.Fatalf("Apply with --force: %v", err)
	}
}

func TestIdentityStep_RefusesWhenAProcessReferencesTheTarget(t *testing.T) {
	e := newStepEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "gh")
	legacy := e.linkIdentity("alpha", "gh", filepath.Join(e.home, ".cli-identities", "alpha"))

	d := e.deps()
	// Not named gh; it just has the directory open.
	d.Processes = func() ([]string, error) {
		return []string{"code.exe --folder " + legacy}, nil
	}
	st := stepRegistry["identity"]
	p, _ := st.Plan(d)
	err := st.Apply(d, p, e.journal())
	if err == nil || !strings.Contains(err.Error(), legacy) {
		t.Fatalf("Apply should refuse naming %s, got: %v", legacy, err)
	}
}

func TestIdentityStep_UntrustedWorkspaceAndOrphansAreNotMoved(t *testing.T) {
	e := newStepEnv(t)
	// Trusted workspace with one linked dir.
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))
	// Legacy directories nothing points at: warned about, never moved.
	stMkdir(t, filepath.Join(e.home, ".claude-personal"), filepath.Join(e.home, ".claude-work"))

	// An untrusted workspace: manifest written, trust deliberately not granted.
	untrusted := filepath.Join(e.base, "beta")
	stWrite(t, filepath.Join(untrusted, ".tentaqles.yaml"),
		"schema: tentaqles-client-v2\nclient: beta\ngit:\n  name: B\n  email: b@b.test\nidentities:\n  claude: {}\n")
	e.linkIdentity("beta", "claude", filepath.Join(e.home, ".claude-beta"))

	p, err := stepRegistry["identity"].Plan(e.deps())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 3 {
		t.Fatalf("only the trusted workspace should be planned, got %d changes:%s", len(p.Changes), stJoin(stKinds(p)))
	}
	if !stContains(p.Skipped, "beta") {
		t.Errorf("untrusted workspace should be Skipped with a reason; skipped:%s", stJoin(p.Skipped))
	}
	for _, orphan := range []string{".claude-personal", ".claude-work"} {
		if !stContains(p.Warnings, orphan) {
			t.Errorf("orphan %s should be warned about; warnings:%s", orphan, stJoin(p.Warnings))
		}
	}
	if stContains(p.Warnings, ".claude-beta") {
		t.Errorf("the untrusted workspace's target is not an orphan; warnings:%s", stJoin(p.Warnings))
	}
}

func TestIdentityStep_ApplyRefusesWhenTheMachineMovedSincePlan(t *testing.T) {
	e := newStepEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))

	d := e.deps()
	st := stepRegistry["identity"]
	p, err := st.Plan(d)
	if err != nil {
		t.Fatal(err)
	}
	// Someone removes the link between the dry run and the apply.
	if err := RemoveLink(paths.IdentityDir("alpha", "claude")); err != nil {
		t.Fatal(err)
	}
	if err := st.Apply(d, p, e.journal()); err == nil || !strings.Contains(err.Error(), "changed since the plan") {
		t.Fatalf("Apply should refuse a stale plan, got: %v", err)
	}
}

// ---------------------------------------------------------------- git

const handWrittenGlobal = "[user]\n" +
	"\tname = Renato\n" +
	"\temail = work@corp.test\n" +
	"[includeIf \"gitdir/i:%ALPHA%/\"]\n" +
	"\tpath = %ALPHA%/.gitconfig-tentaqles\n" +
	"[includeIf \"gitdir/i:%BETA%/\"]\n" +
	"\tpath = %BETA%/.gitconfig-tentaqles\n" +
	"[includeIf \"gitdir/i:%ALPHA%/nested/\"]\n" +
	"\tpath = %ALPHA%/nested/.gitconfig-tentaqles\n" +
	"[includeIf \"gitdir/i:%OUTSIDE%/\"]\n" +
	"\tpath = %OUTSIDE%/.gitconfig-tentaqles\n" +
	"[include]\n" +
	"\tpath = %GONE%/orphan.gitconfig\n"

// gitFixture builds the git half of the machine described in the ledger: a
// hand-written global config with per-workspace includeIf blocks, a nested one,
// one for a directory that is not a registered workspace, and a dangling
// include.
func newGitFixture(t *testing.T) *stepEnv {
	t.Helper()
	e := newStepEnv(t)
	alpha := e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	beta := e.addWorkspace("beta", "Beta Dev", "dev@beta.test", "claude")

	// alpha's file is hand-written: not tq-managed, and its identity differs
	// from the manifest's, which the step must preserve and warn about.
	stWrite(t, gitcfg.WorkspaceFile(alpha), "[user]\n\tname = Alpha Legacy\n\temail = legacy@alpha.test\n")
	// beta's file is already exactly what tq writes.
	if err := gitcfg.WriteWorkspace(beta, "Beta Dev", "dev@beta.test"); err != nil {
		t.Fatal(err)
	}
	// A nested repo inside alpha with its own hand-written include target, and
	// a directory outside any base. Both exist, so neither is an orphan.
	nested := filepath.Join(alpha, "nested")
	stWrite(t, gitcfg.WorkspaceFile(nested), "[user]\n\tname = Nested\n\temail = nested@alpha.test\n")
	outside := filepath.Join(e.root, "outside")
	stWrite(t, gitcfg.WorkspaceFile(outside), "[user]\n\tname = Outside\n\temail = out@x.test\n")

	slash := func(p string) string { return filepath.ToSlash(p) }
	body := handWrittenGlobal
	body = strings.ReplaceAll(body, "%ALPHA%", slash(alpha))
	body = strings.ReplaceAll(body, "%BETA%", slash(beta))
	body = strings.ReplaceAll(body, "%OUTSIDE%", slash(outside))
	body = strings.ReplaceAll(body, "%GONE%", slash(filepath.Join(e.root, "deleted-temp")))
	stWrite(t, e.gitcfg, body)
	return e
}

func TestGitStep_PlanClassifiesEveryIncludeIf(t *testing.T) {
	e := newGitFixture(t)
	alpha := filepath.Join(e.base, "alpha")

	p, err := stepRegistry["git"].Plan(e.deps())
	if err != nil {
		t.Fatal(err)
	}
	if !stHasChange(p, "unset-global", "user.email") {
		t.Errorf("global user.email should be unset; changes:%s", stJoin(stKinds(p)))
	}
	if stHasChange(p, "unset-global", "user.name") {
		t.Errorf("global user.name must be kept; changes:%s", stJoin(stKinds(p)))
	}
	if !stContains(p.Skipped, "user.name") {
		t.Errorf("keeping user.name should be recorded in Skipped; skipped:%s", stJoin(p.Skipped))
	}
	// The two registered, trusted workspaces' includeIf blocks go.
	for _, ws := range []string{"alpha", "beta"} {
		if !stHasChange(p, "remove-includeif", filepath.ToSlash(filepath.Join(e.base, ws))) {
			t.Errorf("includeIf for %s should be removed; changes:%s", ws, stJoin(stKinds(p)))
		}
	}
	// The nested one and the one outside any base are skipped, not removed.
	for _, want := range []string{"nested", "outside"} {
		if stHasChange(p, "remove-includeif", want) {
			t.Errorf("includeIf %q must not be removed; changes:%s", want, stJoin(stKinds(p)))
		}
		if !stContains(p.Skipped, want) {
			t.Errorf("includeIf %q should be Skipped with a reason; skipped:%s", want, stJoin(p.Skipped))
		}
	}
	// The dangling include.path goes.
	if !stHasChange(p, "remove-include", "deleted-temp") {
		t.Errorf("orphan include should be removed; changes:%s", stJoin(stKinds(p)))
	}
	// alpha's hand-written file is rewritten; beta's tq-managed one is not.
	if !stHasChange(p, "rewrite-ws-file", gitcfg.WorkspaceFile(alpha)) {
		t.Errorf("alpha's tampered .gitconfig-tentaqles should be rewritten; changes:%s", stJoin(stKinds(p)))
	}
	if stHasChange(p, "rewrite-ws-file", gitcfg.WorkspaceFile(filepath.Join(e.base, "beta"))) {
		t.Errorf("beta's file is already tq-managed and must not be rewritten; changes:%s", stJoin(stKinds(p)))
	}
	// The identity in alpha's file wins over the manifest, loudly.
	if !stContains(p.Warnings, "legacy@alpha.test") {
		t.Errorf("the file's identity should be preserved with a warning; warnings:%s", stJoin(p.Warnings))
	}
	if !stHasChange(p, "sync-include-file", gitcfg.IncludeFile()) {
		t.Errorf("tq's include file should be written; changes:%s", stJoin(stKinds(p)))
	}
}

func TestGitStep_ApplyClearsDoctorDrift(t *testing.T) {
	e := newGitFixture(t)
	alpha := filepath.Join(e.base, "alpha")

	d := e.deps()
	st := stepRegistry["git"]
	p, err := st.Plan(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Apply(d, p, e.journal()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	found := e.doctorFindings(alpha)
	for _, code := range migrationDriftCodes {
		if msg, ok := found[code]; ok {
			t.Errorf("doctor still reports %s after the git step: %s", code, msg)
		}
	}
	// The identity git actually uses inside alpha is the one the file had.
	body := stRead(t, gitcfg.WorkspaceFile(alpha))
	if !strings.Contains(body, "legacy@alpha.test") || !strings.HasPrefix(body, "# managed by tq") {
		t.Fatalf("alpha's file should be tq-managed but keep the old identity:\n%s", body)
	}
	// The skipped includeIf blocks survive.
	global := stRead(t, e.gitcfg)
	for _, want := range []string{"nested", "outside"} {
		if !strings.Contains(filepath.ToSlash(global), want) {
			t.Errorf("includeIf %q was removed; global config now:\n%s", want, global)
		}
	}
	if strings.Contains(global, "work@corp.test") {
		t.Errorf("global user.email survived:\n%s", global)
	}
	if !strings.Contains(global, "Renato") {
		t.Errorf("global user.name should have been kept:\n%s", global)
	}
}

// ---------------------------------------------------------------- end to end

func TestMigrate_IdentityAndGit_ApplyThenRestore(t *testing.T) {
	e := newGitFixture(t)
	alpha := filepath.Join(e.base, "alpha")
	e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))
	e.linkIdentity("beta", "claude", filepath.Join(e.home, ".claude-beta"))
	stMkdir(t, filepath.Join(e.home, ".claude-personal"))

	before := hashTree(t, e.home)
	beforeIdentities := hashTree(t, paths.IdentitiesRoot())

	steps, err := Steps([]string{"git", "identity"})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Name() != "identity" {
		t.Fatalf("Steps must return identity before git, got %v", steps)
	}
	d := e.deps()
	j := e.journal()
	plans, err := Run(d, steps, true, j)
	if err != nil {
		t.Fatalf("Run(apply): %v", err)
	}
	if len(plans["identity"].Changes) != 6 {
		t.Fatalf("identity plan:%s", stJoin(stKinds(plans["identity"])))
	}

	found := e.doctorFindings(alpha)
	for _, code := range migrationDriftCodes {
		if msg, ok := found[code]; ok {
			t.Errorf("doctor still reports %s after migrate: %s", code, msg)
		}
	}

	// Now walk it all backwards.
	lines, err := j.Restore(Runner{Git: gitcfg.RunGit})
	if err != nil {
		t.Fatalf("Restore: %v\n%s", err, stJoin(lines))
	}
	if got := hashTree(t, e.home); got != before {
		t.Errorf("home tree not restored:\n  before %s\n  after  %s\n%s", before, got, stJoin(lines))
	}
	if got := hashTree(t, paths.IdentitiesRoot()); got != beforeIdentities {
		t.Errorf("identities tree not restored:\n  before %s\n  after  %s\n%s", beforeIdentities, got, stJoin(lines))
	}
}

func TestGitStep_ApplyIsIdempotentAgainstAlreadyCleanState(t *testing.T) {
	e := newStepEnv(t)
	ws := e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	if err := gitcfg.WriteWorkspace(ws, "Alpha Dev", "dev@alpha.test"); err != nil {
		t.Fatal(err)
	}
	if err := gitcfg.Sync([]string{ws}); err != nil {
		t.Fatal(err)
	}
	stWrite(t, e.gitcfg, "[user]\n\tname = Renato\n\tuseConfigOnly = true\n[include]\n\tpath = "+
		filepath.ToSlash(gitcfg.IncludeFile())+"\n")

	d := e.deps()
	st := stepRegistry["git"]
	p, err := st.Plan(d)
	if err != nil {
		t.Fatal(err)
	}
	// The only thing left to do is re-sync the (identical) include file.
	for _, c := range p.Changes {
		if c.Kind != "sync-include-file" {
			t.Errorf("nothing should be planned on a clean machine, got %s %s (%s)", c.Kind, c.Path, c.Detail)
		}
	}
	if err := st.Apply(d, p, e.journal()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

func TestSortedChangeKindsAreStable(t *testing.T) {
	// A guard for Task 5: the command layer prints Kind verbatim, so the set of
	// kinds these two steps emit is part of the contract.
	want := []string{
		"add-include", "make-link", "move-dir", "remove-include", "remove-includeif",
		"remove-link", "rewrite-ws-file", "set-global", "sync-include-file", "unset-global",
	}
	got := append([]string(nil), stepChangeKinds...)
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Change.Kind set changed:\n got %v\nwant %v", got, want)
	}
}

func TestProcessIsCLI_MatchesTheExecutableNotASubstring(t *testing.T) {
	for _, tc := range []struct {
		line, id string
		want     bool
	}{
		{"claude.exe", "claude", true},
		{`C:\Users\x\AppData\Local\claude\claude.exe --resume`, "claude", true},
		{`"C:\Program Files\nodejs\node.exe" C:\npm\claude.js`, "claude", true},
		{"gh.exe auth status", "gh", true},
		// A process merely holding a path that contains the word is not the CLI.
		{`code.exe C:\Users\x\.claude-personal`, "claude", false},
		{"claudia.exe", "claude", false},
		{"ghost.exe", "gh", false},
		{"explorer.exe", "claude", false},
		// A bare mention of the word in an argument is not the CLI either.
		// Anything less strict blocks the identity step for a shell that merely
		// echoes "claude", and --force is not an acceptable answer here.
		{`powershell.exe -Command "echo === claude processes ==="`, "claude", false},
		{"bash.exe -lc 'which claude'", "claude", false},
		{"code.exe notes-about-gh.md", "gh", false},
		// ...but a real launcher in a later field still counts.
		{`cmd.exe /c C:\bin\claude.cmd --resume`, "claude", true},
		{"sh -c /usr/local/bin/claude", "claude", true},
	} {
		if got := processIsCLI(tc.line, tc.id); got != tc.want {
			t.Errorf("processIsCLI(%q, %q) = %v, want %v", tc.line, tc.id, got, tc.want)
		}
	}
}

func TestGlobalConfigPath_PrefersTheEnvironmentThenHome(t *testing.T) {
	home := stTmpDir(t)
	d := Deps{Env: func(k string) (string, bool) {
		switch k {
		case "GIT_CONFIG_GLOBAL":
			return filepath.Join(home, "explicit.gitconfig"), true
		}
		return "", false
	}}
	if got := globalConfigPath(d); got != filepath.Join(home, "explicit.gitconfig") {
		t.Fatalf("GIT_CONFIG_GLOBAL should win, got %q", got)
	}
	// Without it, ~/.gitconfig -- whether or not it exists yet.
	d.Env = func(k string) (string, bool) {
		if k == "HOME" || k == "USERPROFILE" {
			return home, true
		}
		return "", false
	}
	if got := globalConfigPath(d); got != filepath.Join(home, ".gitconfig") {
		t.Fatalf("want %s, got %q", filepath.Join(home, ".gitconfig"), got)
	}
}
