package packs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// Guard bounds (spec C §2.10, §2.14).
const (
	GuardBudget     = 50 * time.Millisecond
	ReadLimit       = 1 << 20
	ReadBudget      = 40 * time.Millisecond
	HashLimit       = 64 << 20
	HashBudget      = 40 * time.Millisecond
	hashChunk       = 1 << 20
	maxGuardRuntime = 2 * time.Second // hard stop even when HashState extends the budget
	// TotalGuardBudget bounds all the pack guards of one hook call together.
	TotalGuardBudget = 250 * time.Millisecond
)

// The budgets in force (tests shorten them).
var (
	guardBudget      = GuardBudget
	readBudget       = ReadBudget
	hashBudget       = HashBudget
	totalGuardBudget = TotalGuardBudget
)

// GenerousBudgetsForTests raises the read, hash, per-guard and total guard
// budgets for a test binary, the git call bound (GitTimeout) and the
// external tools' --version bound (toolrun.GenerousVersionTimeoutForTests):
// a loaded CI runner can take longer than the production bounds to read
// small fixtures, to run git or to start a fresh fake tool, which fails
// closed instead of testing what a guard or a check decides. Production code
// never calls it; the tests that a bound holds set it explicitly
// (TestGuardBudgetMeansDeny, TestReadStateBounds, TestHashStateBounds,
// TestTotalGuardBudget). It returns a function that restores the real
// bounds.
func GenerousBudgetsForTests() (restore func()) {
	g, r, h, t, gt := guardBudget, readBudget, hashBudget, totalGuardBudget, gitTimeout
	guardBudget, readBudget, hashBudget, totalGuardBudget = 10*time.Second, 10*time.Second, 10*time.Second, 30*time.Second
	gitTimeout = 2 * time.Minute
	restoreVersion := toolrun.GenerousVersionTimeoutForTests()
	return func() {
		guardBudget, readBudget, hashBudget, totalGuardBudget, gitTimeout = g, r, h, t, gt
		restoreVersion()
	}
}

// Guard is a decision guard: a pure function over the tool call and the
// loaded config. Check returns the action, the reason and handled: handled
// means the guard owns this call, and every built-in rule match whose id is
// in Supersedes is dropped.
type Guard struct {
	ID   string // tq/<name>
	Pack string
	// Events are tool names (Bash, PowerShell, Edit, Write, Read, ...) or
	// anchored regexes over the tool name (mcp__.*__query). Empty: every tool.
	Events     []string
	Supersedes []string
	Check      func(call policy.ToolCall, g GuardCtx) (policy.Action, string, bool)
	// Category is the guard-mode category (spec E §14): one of the
	// thirteen, equal to the guard id's entry in guardcfg.Categories. The
	// mode maps the guard's verdict by it like a built-in rule's.
	Category guardcfg.Category
	// Default is what tq guard list shows as the guard's action: "ask",
	// "deny", "deny or ask", or "" when a pack policy decides (shown as
	// "policy"). It is for listing only; Check returns the real action.
	Default string

	events []*regexp.Regexp
}

var reMeta = regexp.MustCompile(`[.*+?()\[\]|^$\\]`)

// RegisterGuard adds a decision guard. It panics on an id not starting with
// tq/, an unknown pack, a nil Check, a bad event regex or a duplicate
// (id, pack). One id may serve several packs.
func RegisterGuard(g Guard) {
	if !strings.HasPrefix(g.ID, "tq/") || len(g.ID) < 4 {
		panic("packs.RegisterGuard: id must be tq/<name>, got " + g.ID)
	}
	if !knownPack(g.Pack) {
		panic(fmt.Sprintf("packs.RegisterGuard %s: unknown pack %q", g.ID, g.Pack))
	}
	if g.Check == nil {
		panic("packs.RegisterGuard " + g.ID + ": Check is nil")
	}
	if _, ok := guardcfg.ParseCategory(string(g.Category)); !ok {
		panic(fmt.Sprintf("packs.RegisterGuard %s: unknown or empty Category %q", g.ID, g.Category))
	}
	// The table is the one source the mode reads: a shipped guard's Category
	// must match its entry, so a pack can never place itself in a looser
	// category. An id the table does not name (a test guard) may not claim a
	// locked category; cmd's TestPackGuardCategories checks that every
	// shipped guard is in the table.
	want, inTable := guardcfg.Categories[g.ID]
	switch {
	case inTable && want != g.Category:
		panic(fmt.Sprintf("packs.RegisterGuard %s: Category %q, but guardcfg.Categories has %q", g.ID, g.Category, want))
	case !inTable && guardcfg.Locked(g.Category):
		panic(fmt.Sprintf("packs.RegisterGuard %s: the locked category %q is only for ids guardcfg.Categories locks", g.ID, g.Category))
	}
	for _, id := range g.Supersedes {
		if guardcfg.Unsupersedable(id) {
			panic("packs.RegisterGuard " + g.ID + ": " + id + " protects the guard or a secret and cannot be superseded")
		}
	}
	for _, e := range g.Events {
		src := regexp.QuoteMeta(e)
		if reMeta.MatchString(e) {
			src = e
		}
		re, err := regexp.Compile("^(?:" + src + ")$")
		if err != nil {
			panic("packs.RegisterGuard " + g.ID + ": event " + e + ": " + err.Error())
		}
		g.events = append(g.events, re)
	}
	regMu.Lock()
	defer regMu.Unlock()
	for _, have := range guards[g.ID] {
		if have.Pack == g.Pack {
			panic("packs.RegisterGuard: duplicate guard " + g.ID + " for pack " + g.Pack)
		}
	}
	guards[g.ID] = append(guards[g.ID], g)
}

// Guards returns every registered guard, sorted by id then pack.
func Guards() []Guard {
	regMu.RLock()
	defer regMu.RUnlock()
	var out []Guard
	for _, gs := range guards {
		out = append(out, gs...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Pack < out[j].Pack
	})
	return out
}

func (g Guard) matches(tool string) bool {
	if len(g.events) == 0 {
		return true
	}
	for _, re := range g.events {
		if re.MatchString(tool) {
			return true
		}
	}
	return false
}

// GuardEnv is what the hook knows when it runs the guards.
type GuardEnv struct {
	Config        Config
	Manifest      *manifest.Manifest
	Workspace     string
	WorkspaceRoot string
	RepoRoot      string
	// Cwd is the hook payload's cwd (the hook falls back to the process cwd
	// only when the payload has none).
	Cwd string
	// ExtraRoots are more folders ReadLocal may read (tests; nil in the hook).
	ExtraRoots []string
}

// GuardCtx is a guard's view: config, workspace and three bounded reads.
// A guard never writes; Consume and Count are applied by the hook after it
// decides.
type GuardCtx struct{ st *guardState }

type guardState struct {
	env       GuardEnv
	pack      string
	mu        sync.Mutex
	consume   []string
	counters  []Counter
	hashSpent atomic.Int64
	hashed    map[string]string // HashState names verified in this call -> absolute path
	readArgs  []ReadArg
}

// Counter is a pack counter a guard asked for.
type Counter struct {
	Pack, Name string
	N          int
}

// NewGuardCtx builds the context a guard of pack sees (exported for pack
// tests of guard functions).
func NewGuardCtx(env GuardEnv, pack string) GuardCtx {
	return GuardCtx{st: &guardState{env: env, pack: pack}}
}

func (g GuardCtx) Config() Config               { return g.st.env.Config }
func (g GuardCtx) Settings() Settings           { return g.st.env.Config.Pack(g.st.pack) }
func (g GuardCtx) Manifest() *manifest.Manifest { return g.st.env.Manifest }
func (g GuardCtx) Workspace() string            { return g.st.env.Workspace }
func (g GuardCtx) WorkspaceRoot() string        { return g.st.env.WorkspaceRoot }
func (g GuardCtx) RepoRoot() string             { return g.st.env.RepoRoot }
func (g GuardCtx) Pack() string                 { return g.st.pack }

// StateDir is the pack's state directory path (not created: guards never write).
func (g GuardCtx) StateDir() string { return stateDirPath(g.st.env.Workspace, g.st.pack) }

// Cwd is the hook payload's cwd, made absolute and resolved to its final
// path when it exists; the process cwd only when the payload has none.
func (g GuardCtx) Cwd() string {
	c := g.st.env.Cwd
	if c == "" {
		c, _ = os.Getwd()
	}
	if c == "" {
		return ""
	}
	if abs, err := filepath.Abs(c); err == nil {
		c = abs
	}
	if fp, err := FinalPath(c); err == nil {
		return fp
	}
	return filepath.Clean(c)
}

// ReadArg is one stored file an allowed command reads: the program that
// reads it (its first word, such as tofu, terraform or pulumi) and the
// file's absolute path under the pack's plans/ directory.
type ReadArg struct {
	Program string
	// Flag is the one flag whose value the file may be (--plan); empty: a
	// positional argument only. A positional argument is always accepted.
	Flag string
	// BoolFlags are the program's flags that take no value (-auto-approve,
	// --yes): a plan right after one is still positional. Any other flag
	// before the plan is read as taking it as its value (fail closed).
	BoolFlags []string
	Path      string
}

// ReadSpec is how a program reads its stored plans: its name (the
// segment's first word), the flag whose value the plan may be ("" for
// positional only) and its boolean flags.
type ReadSpec struct {
	Program   string
	Flag      string
	BoolFlags []string
}

var flagNameRe = regexp.MustCompile(`^-{1,2}[A-Za-z0-9][A-Za-z0-9-]*$`)

// ReadArgs names stored plans this guard verified with HashState in this
// call and that spec.Program reads as a positional argument (terraform
// apply <plan>, also after a declared boolean flag: -auto-approve <plan>) or
// as the value of spec.Flag (pulumi up --plan <plan>, --plan=<plan>; Flag ""
// allows the positional form only). For this call only, the core state-dir rule
// (tq/hub-store-write) does not count that exact path, provided:
//   - the command writes nothing (no redirect, no write verb, shell,
//     interpreter, downloader or wrapper, no substitution);
//   - the path occurs exactly once, in a segment whose first word is
//     program;
//
// every other path is checked as usual. It takes effect only when the guard
// handles the call and allows it. Only files under the pack's plans/
// directory can be named; a name not hashed in this call, outside plans/,
// or an empty program is an error (the guard should deny).
func (g GuardCtx) ReadArgs(spec ReadSpec, names ...string) error {
	program, flag := spec.Program, spec.Flag
	var bools []string
	for _, b := range spec.BoolFlags {
		if !flagNameRe.MatchString(b) {
			return fmt.Errorf("%w: %q is not a flag name", ErrRefused, b)
		}
		bools = append(bools, strings.ToLower(b))
	}
	program = strings.ToLower(strings.TrimSpace(program))
	if program == "" || strings.ContainsAny(program, `/\ `) || len(names) == 0 {
		return fmt.Errorf("%w: ReadArgs needs the program name that reads the file and at least one file", ErrRefused)
	}
	if flag != "" && !flagNameRe.MatchString(flag) {
		return fmt.Errorf("%w: %q is not a flag name", ErrRefused, flag)
	}
	g.st.mu.Lock()
	defer g.st.mu.Unlock()
	for _, n := range names {
		rel, err := cleanRel(n)
		if err != nil {
			return err
		}
		if s := filepath.ToSlash(rel); !strings.HasPrefix(s, "plans/") || len(s) == len("plans/") {
			return fmt.Errorf("%w: %q is not a stored plan (only files under plans/ can be read args)", ErrRefused, n)
		}
		abs, ok := g.st.hashed[rel]
		if !ok {
			return fmt.Errorf("%w: %q was not verified with HashState in this call", ErrRefused, n)
		}
		g.st.readArgs = append(g.st.readArgs, ReadArg{Program: program, Flag: strings.ToLower(flag), BoolFlags: bools, Path: abs})
	}
	return nil
}

// Consume asks the hook to append a `consumed` ledger line for decisionID
// when this guard's allow stands. The append happens before the hook returns
// the allow; if it fails, the call is denied.
func (g GuardCtx) Consume(decisionID string) {
	g.st.mu.Lock()
	g.st.consume = append(g.st.consume, decisionID)
	g.st.mu.Unlock()
}

// Count asks the hook to record a pack counter after it decides.
func (g GuardCtx) Count(counter string, n int) {
	g.st.mu.Lock()
	g.st.counters = append(g.st.counters, Counter{Pack: g.st.pack, Name: counter, N: n})
	g.st.mu.Unlock()
}

// Read errors. Any of them must make the guard deny.
var (
	ErrRefused    = errors.New("read refused")
	ErrTooLarge   = errors.New("file over the read cap")
	ErrOverBudget = errors.New("read over its time budget")
)

func cleanRel(name string) (string, error) {
	n := strings.ReplaceAll(name, `\`, "/")
	if n == "" || filepath.IsAbs(name) || strings.HasPrefix(n, "/") || (len(n) > 1 && n[1] == ':') {
		return "", fmt.Errorf("%w: %q is not a relative name", ErrRefused, name)
	}
	for _, seg := range strings.Split(n, "/") {
		if seg == ".." {
			return "", fmt.Errorf("%w: %q leaves the directory", ErrRefused, name)
		}
	}
	return filepath.FromSlash(n), nil
}

// openState opens a state file: a regular file with one link whose final
// path (from the handle) stays inside the state directory.
func openState(dir, rel, name string) (*os.File, os.FileInfo, error) {
	p := filepath.Join(dir, rel)
	li, err := os.Lstat(p)
	if err != nil {
		return nil, nil, err
	}
	if !li.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%w: %s is not a regular file", ErrRefused, name)
	}
	f, err := openNoBlock(p)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err == nil && !fi.Mode().IsRegular() {
		err = fmt.Errorf("%w: %s is not a regular file", ErrRefused, name)
	}
	if err == nil {
		var final string
		var links int
		final, links, err = finalPath(f, p)
		switch {
		case err != nil:
			err = fmt.Errorf("%w: cannot resolve %s (%v)", ErrRefused, name, err)
		case links != 1:
			err = fmt.Errorf("%w: %s has %d hard links", ErrRefused, name, links)
		case !within(final, resolvedRoots([]string{dir})):
			err = fmt.Errorf("%w: %s resolves outside the state directory", ErrRefused, name)
		}
	}
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, fi, nil
}

// ReadState reads a file under StateDir(), at most 1 MiB, within 40 ms.
func (g GuardCtx) ReadState(name string) ([]byte, error) {
	dir := g.StateDir()
	if dir == "" {
		return nil, fmt.Errorf("%w: no workspace state directory", ErrRefused)
	}
	rel, err := cleanRel(name)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	f, fi, err := openState(dir, rel, name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	max := int64(ReadLimit)
	if fi.Size() > max {
		return nil, fmt.Errorf("%w (%d > %d bytes)", ErrTooLarge, fi.Size(), max)
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, ErrTooLarge
	}
	if time.Since(start) > readBudget {
		return nil, ErrOverBudget
	}
	return b, nil
}

// refusedLocal are paths ReadLocal never reads: the secret stores of
// tq/secret-store-read plus cloud token caches.
var refusedLocal = regexp.MustCompile(`(?i)(^|/)\.ssh/|(^|/)\.aws/(credentials|sso/cache/|cli/cache/)|(^|/)\.git-credentials$|(^|/)\.netrc$|(^|/)\.docker/config\.json$|(^|/)\.azure/(accesstokens\.json|msal_token_cache|service_principal_entries)|(^|/)\.tentaqles/(bundles/catalog\.yaml|secrets?/)|(^|/)\.config/gh/hosts\.yml$|(^|/)gh/hosts\.yml$|(^|/)\.pulumi/credentials\.json$|(^|/)\.kube/config$|(^|/)application_default_credentials\.json$|(^|/)credentials\.db$|(^|/)access_tokens\.db$|(^|/)\.?credentials(\.json)?$|(^|/)github cli/hosts\.yml$|(^|/)microsoft/(usersecrets|credentials|protect|crypto)/|(^|/)gcloud/(credentials\.db|access_tokens\.db|legacy_credentials/|application_default_credentials\.json)|(^|/)\.azure/(msal_http_cache|tokencache)|(^|/)\.config/(gh|gcloud|hub)/|(^|/)\.docker/|(^|/)\.npmrc$|(^|/)\.pypirc$|(^|/)\.terraform\.d/credentials\.tfrc\.json$|(^|/)\.supabase/access-token$`)

func secretStorePath(p string) bool {
	s := filepath.ToSlash(p)
	if refusedLocal.MatchString(s) {
		return true
	}
	for _, r := range policy.Builtin() {
		if r.ID == "tq/secret-store-read" && r.Path != "" {
			if re, err := regexp.Compile("(?i)" + r.Path); err == nil && re.MatchString(s) {
				return true
			}
		}
	}
	return false
}

func envSegment(p string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if strings.HasPrefix(strings.ToLower(seg), ".env") {
			return true
		}
	}
	return false
}

// localRoots are the folders ReadLocal may read inside.
func (g GuardCtx) localRoots() []string {
	e := g.st.env
	var roots []string
	add := func(p string) {
		if p != "" {
			if abs, err := filepath.Abs(p); err == nil {
				roots = append(roots, abs)
			}
		}
	}
	add(e.WorkspaceRoot)
	add(e.RepoRoot)
	if plainName(e.Workspace) {
		add(filepath.Join(paths.IdentitiesRoot(), e.Workspace))
	}
	// %APPDATA% / ~/.config as a whole is not a root: it holds token stores
	// (GitHub CLI hosts.yml, dotnet user-secrets, Windows credentials). Only
	// the named config folders below are.
	if h, err := os.UserHomeDir(); err == nil {
		for _, d := range []string{".azure", ".aws", ".pulumi", ".config", ".supabase", ".terraform.d"} {
			add(filepath.Join(h, d))
		}
	}
	for _, r := range e.ExtraRoots {
		add(r)
	}
	return roots
}

func within(p string, roots []string) bool {
	norm := func(s string) string {
		s = filepath.Clean(s)
		if runtime.GOOS == "windows" {
			s = strings.ToLower(s)
		}
		return s
	}
	np := norm(p)
	for _, r := range roots {
		nr := norm(r)
		if np == nr || strings.HasPrefix(np, strings.TrimRight(nr, `\/`)+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// ReadLocal reads one named local file inside the workspace root, the repo,
// the identity's config dirs or the user's config dirs, at most max bytes
// (capped at 1 MiB), within 40 ms. A relative path is relative to the repo
// root (the workspace root when there is no repo).
//
// It refuses .env* (any path segment), the secret stores and token caches,
// "..", anything that is not a regular file, and a file with more than one
// hard link. The checks run twice: on the path as written, and on the final
// path resolved from the open handle (GetFinalPathNameByHandle on Windows,
// EvalSymlinks elsewhere), which defeats 8.3 short names, junctions and
// symlinks anywhere in the path. The bytes come from that same handle.
func (g GuardCtx) ReadLocal(path string, max int64) ([]byte, error) {
	start := time.Now()
	if max <= 0 || max > ReadLimit {
		max = ReadLimit
	}
	raw := strings.ReplaceAll(path, `\`, "/")
	for _, seg := range strings.Split(raw, "/") {
		if seg == ".." {
			return nil, fmt.Errorf("%w: %q contains ..", ErrRefused, path)
		}
	}
	if envSegment(raw) {
		return nil, fmt.Errorf("%w: .env files are never read", ErrRefused)
	}
	p := path
	if !filepath.IsAbs(p) {
		base := g.st.env.RepoRoot
		if base == "" {
			base = g.st.env.WorkspaceRoot
		}
		if base == "" {
			return nil, fmt.Errorf("%w: no repository or workspace root for %q", ErrRefused, path)
		}
		p = filepath.Join(base, filepath.FromSlash(raw))
	}
	p = filepath.Clean(p)
	roots := g.localRoots()
	// A path named from Cwd() is a final path while a root may be spelled
	// through a symlink (macOS /var -> /private/var): compare the resolved
	// forms too. The final-path check below still runs on the opened file.
	if !within(p, roots) && !within(paths.Resolve(p), resolvedRoots(roots)) {
		return nil, fmt.Errorf("%w: %q is outside the workspace and config dirs", ErrRefused, path)
	}
	if secretStorePath(p) {
		return nil, fmt.Errorf("%w: %q is a credential store", ErrRefused, path)
	}
	if fi, err := os.Lstat(p); err != nil {
		return nil, err
	} else if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %q is not a regular file", ErrRefused, path)
	}
	f, err := openNoBlock(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %q is not a regular file", ErrRefused, path)
	}
	final, links, err := finalPath(f, p)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot resolve %q (%v)", ErrRefused, path, err)
	}
	if links != 1 {
		return nil, fmt.Errorf("%w: %q has %d hard links", ErrRefused, path, links)
	}
	if !within(final, resolvedRoots(roots)) || envSegment(final) || secretStorePath(final) {
		return nil, fmt.Errorf("%w: %q resolves outside the allowed roots or to a protected file", ErrRefused, path)
	}
	if fi.Size() > max {
		return nil, fmt.Errorf("%w (%d > %d bytes)", ErrTooLarge, fi.Size(), max)
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, ErrTooLarge
	}
	if time.Since(start) > readBudget {
		return nil, ErrOverBudget
	}
	return b, nil
}

// resolvedRoots resolves each root to its final long path (roots that do
// not exist are dropped: nothing can be read inside them).
func resolvedRoots(roots []string) []string {
	var out []string
	for _, r := range roots {
		f, err := os.Open(r)
		if err != nil {
			continue
		}
		if fp, _, err := finalPath(f, r); err == nil {
			out = append(out, fp)
		}
		f.Close()
	}
	return out
}

// HashState returns "sha256:<hex>" of a file under StateDir(), up to 64 MiB,
// within 40 ms. Over the cap or the budget is an error: the guard denies.
func (g GuardCtx) HashState(name string) (string, error) {
	start := time.Now()
	defer func() { g.st.hashSpent.Add(int64(time.Since(start))) }()
	dir := g.StateDir()
	if dir == "" {
		return "", fmt.Errorf("%w: no workspace state directory", ErrRefused)
	}
	rel, err := cleanRel(name)
	if err != nil {
		return "", err
	}
	f, fi, err := openState(dir, rel, name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if fi.Size() > HashLimit {
		return "", fmt.Errorf("%w (%d > %d bytes)", ErrTooLarge, fi.Size(), HashLimit)
	}
	h := sha256.New()
	buf := make([]byte, hashChunk)
	var total int64
	for {
		if time.Since(start) > hashBudget {
			return "", ErrOverBudget
		}
		n, err := f.Read(buf)
		h.Write(buf[:n])
		total += int64(n)
		if total > HashLimit {
			return "", ErrTooLarge
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	if time.Since(start) > hashBudget {
		return "", ErrOverBudget
	}
	g.st.mu.Lock()
	if g.st.hashed == nil {
		g.st.hashed = map[string]string{}
	}
	g.st.hashed[rel] = filepath.Join(dir, rel)
	g.st.mu.Unlock()
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// MaxListEntries caps ListLocal.
const MaxListEntries = 256

// LocalEntry is one ListLocal row: a name and a size, never content.
type LocalEntry struct {
	Name string
	Size int64
	Dir  bool
}

// ListLocal lists one local directory under the same roots and refusals as
// ReadLocal (no .., no .env*, no secret store, no link, the final path
// checked again), at most max entries (capped at MaxListEntries), within
// the read budget. Names starting with .env and credential stores are left out. More entries
// than max is ErrTooLarge. Entries are sorted by name.
func (g GuardCtx) ListLocal(dir string, max int) ([]LocalEntry, error) {
	start := time.Now()
	if max <= 0 || max > MaxListEntries {
		max = MaxListEntries
	}
	raw := strings.ReplaceAll(dir, `\`, "/")
	for _, seg := range strings.Split(raw, "/") {
		if seg == ".." {
			return nil, fmt.Errorf("%w: %q contains ..", ErrRefused, dir)
		}
	}
	if envSegment(raw) {
		return nil, fmt.Errorf("%w: .env paths are never listed", ErrRefused)
	}
	p := dir
	if !filepath.IsAbs(p) {
		base := g.st.env.RepoRoot
		if base == "" {
			base = g.st.env.WorkspaceRoot
		}
		if base == "" {
			return nil, fmt.Errorf("%w: no repository or workspace root for %q", ErrRefused, dir)
		}
		p = filepath.Join(base, filepath.FromSlash(raw))
	}
	p = filepath.Clean(p)
	roots := g.localRoots()
	if !within(p, roots) && !within(paths.Resolve(p), resolvedRoots(roots)) { // a symlinked root, as in ReadLocal
		return nil, fmt.Errorf("%w: %q is outside the workspace and config dirs", ErrRefused, dir)
	}
	if secretStorePath(p) || secretStorePath(p+"/") {
		return nil, fmt.Errorf("%w: %q is a credential store", ErrRefused, dir)
	}
	if li, err := os.Lstat(p); err != nil {
		return nil, err
	} else if !li.IsDir() {
		return nil, fmt.Errorf("%w: %q is not a directory", ErrRefused, dir)
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	final, _, err := finalPath(f, p)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot resolve %q (%v)", ErrRefused, dir, err)
	}
	if !within(final, resolvedRoots(roots)) || envSegment(final) || secretStorePath(final) || secretStorePath(final+"/") {
		return nil, fmt.Errorf("%w: %q resolves outside the allowed roots or to a protected folder", ErrRefused, dir)
	}
	des, err := f.ReadDir(max + 1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(des) > max {
		return nil, fmt.Errorf("%w (more than %d entries)", ErrTooLarge, max)
	}
	out := make([]LocalEntry, 0, len(des))
	for _, de := range des {
		if strings.HasPrefix(strings.ToLower(de.Name()), ".env") {
			continue
		}
		if full := filepath.Join(final, de.Name()); secretStorePath(full) || secretStorePath(full+"/") ||
			secretStorePath(filepath.Join(p, de.Name())) || secretStorePath(filepath.Join(p, de.Name())+"/") {
			continue // a credential store is never named
		}
		e := LocalEntry{Name: de.Name(), Dir: de.IsDir()}
		if fi, err := de.Info(); err == nil && !de.IsDir() {
			e.Size = fi.Size()
		}
		out = append(out, e)
		if time.Since(start) > readBudget {
			return nil, ErrOverBudget
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if time.Since(start) > readBudget {
		return nil, ErrOverBudget
	}
	return out, nil
}

// HashBytes is the sha256 form HashState returns, for evidence computed at
// request time.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// GuardOutcome is one guard's verdict on a call.
type GuardOutcome struct {
	ID, Pack   string
	Action     policy.Action
	Reason     string
	Handled    bool
	Supersedes []string
	Consume    []string // decision ids to mark consumed when the allow stands
	// ReadArgs are StateDir files (absolute paths) the guard verified and the
	// allowed command reads as arguments (GuardCtx.ReadArgs); set only on a
	// handled allow.
	ReadArgs []ReadArg
	Counters []Counter
	Failed   bool // a panic, a timeout: the outcome is a deny
}

// EvaluateGuards runs the guards of the enabled packs that match the call's
// tool. off reports a guard id switched off (guard.disable); actionFor
// applies guard.actions to an ask or deny. A panic or a blown budget is a
// deny (fail closed).
func EvaluateGuards(call policy.ToolCall, env GuardEnv, off func(string) bool, actionFor func(string, policy.Action) policy.Action) []GuardOutcome {
	var out []GuardOutcome
	deadline := time.Now().Add(totalGuardBudget)
	for _, g := range Guards() {
		// Only the trusted manifest enables a pack, so only its packs' guards
		// run (and may supersede). No trusted manifest: no pack guard.
		if env.Manifest == nil || !env.Manifest.Packs[g.Pack].IsEnabled() || !env.Config.IsEnabled(g.Pack) || !g.matches(call.Tool) {
			continue
		}
		if off != nil && off(g.ID) {
			continue
		}
		var o GuardOutcome
		if time.Now().After(deadline) {
			o = GuardOutcome{ID: g.ID, Pack: g.Pack, Action: policy.Deny, Failed: true,
				Reason: fmt.Sprintf("the pack guards ran past their %s total budget; %s denied to stay safe", totalGuardBudget, g.ID)}
		} else {
			o = runGuard(g, call, env, deadline)
		}
		if !o.Failed && actionFor != nil && (o.Action == policy.Ask || o.Action == policy.Deny) {
			o.Action = actionFor(g.ID, o.Action)
		}
		var keep []string
		for _, id := range o.Supersedes {
			if !guardcfg.Unsupersedable(id) {
				keep = append(keep, id)
			}
		}
		o.Supersedes = keep
		out = append(out, o)
	}
	return out
}

func runGuard(g Guard, call policy.ToolCall, env GuardEnv, deadline time.Time) GuardOutcome {
	ctx := NewGuardCtx(env, g.Pack)
	type res struct {
		a       policy.Action
		reason  string
		handled bool
		panic   string
	}
	done := make(chan res, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				msg := fmt.Sprint(p)
				if len(msg) > 200 {
					msg = msg[:200]
				}
				done <- res{panic: msg}
			}
		}()
		a, reason, handled := g.Check(call, ctx)
		done <- res{a: a, reason: reason, handled: handled}
	}()
	o := GuardOutcome{ID: g.ID, Pack: g.Pack}
	start := time.Now()
	timer := time.NewTimer(guardBudget)
	defer timer.Stop()
	for {
		select {
		case r := <-done:
			if r.panic != "" {
				o.Action, o.Failed = policy.Deny, true
				o.Reason = "the " + g.ID + " guard failed (" + r.panic + "); denied to stay safe"
				return o
			}
			switch r.a {
			case policy.Allow, policy.Ask, policy.Deny:
			default:
				o.Action, o.Failed = policy.Deny, true
				o.Reason = "the " + g.ID + " guard returned an unknown action; denied to stay safe"
				return o
			}
			o.Action, o.Reason, o.Handled = r.a, r.reason, r.handled
			if o.Handled {
				o.Supersedes = append([]string(nil), g.Supersedes...)
			}
			ctx.st.mu.Lock()
			o.Consume = append([]string(nil), ctx.st.consume...)
			o.Counters = append([]Counter(nil), ctx.st.counters...)
			ctx.st.mu.Unlock()
			if o.Handled && o.Action == policy.Allow {
				o.ReadArgs = append([]ReadArg(nil), ctx.st.readArgs...)
			}
			if o.Action != policy.Allow {
				o.Consume = nil // an approval is only used by an allow
			}
			return o
		case <-timer.C:
			// HashState has its own 40 ms budget on top of the guard's.
			allowed := guardBudget + time.Duration(ctx.st.hashSpent.Load())
			el := time.Since(start)
			if rem := time.Until(deadline); rem < allowed-el {
				allowed = el + rem
			}
			if el < allowed && el < maxGuardRuntime {
				timer.Reset(allowed - el)
				continue
			}
			o.Action, o.Failed = policy.Deny, true
			o.Reason = fmt.Sprintf("the %s guard ran past its %s budget; denied to stay safe", g.ID, guardBudget)
			return o
		}
	}
}

// FinalPath resolves an existing file or directory to its final long path
// through an open handle (GetFinalPathNameByHandle on Windows, so junctions,
// symlinks and 8.3 short names are all resolved; EvalSymlinks elsewhere).
func FinalPath(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fp, _, err := finalPath(f, p)
	return fp, err
}
