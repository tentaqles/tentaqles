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

	"github.com/tentaqles/tentaqles/internal/manifest"
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
)

// The budgets in force (tests shorten them).
var (
	guardBudget = GuardBudget
	readBudget  = ReadBudget
	hashBudget  = HashBudget
)

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
	p := filepath.Join(dir, rel)
	fi, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrRefused, name)
	}
	return boundedRead(p, ReadLimit)
}

func boundedRead(p string, max int64) ([]byte, error) {
	start := time.Now()
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
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

// refusedLocal are paths ReadLocal never reads: the secret stores of
// tq/secret-store-read plus cloud token caches.
var refusedLocal = regexp.MustCompile(`(?i)(^|/)\.ssh/|(^|/)\.aws/(credentials|sso/cache/|cli/cache/)|(^|/)\.git-credentials$|(^|/)\.netrc$|(^|/)\.docker/config\.json$|(^|/)\.azure/(accesstokens\.json|msal_token_cache|service_principal_entries)|(^|/)\.tentaqles/(bundles/catalog\.yaml|secrets?/)|(^|/)\.config/gh/hosts\.yml$|(^|/)gh/hosts\.yml$|(^|/)\.pulumi/credentials\.json$|(^|/)\.kube/config$|(^|/)application_default_credentials\.json$|(^|/)credentials\.db$|(^|/)access_tokens\.db$|(^|/)\.?credentials(\.json)?$`)

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
	if d, err := os.UserConfigDir(); err == nil {
		add(d)
	}
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
// root (the workspace root when there is no repo). It refuses .env* (any
// path segment), the secret stores, "..", and a symlink that resolves
// outside the roots.
func (g GuardCtx) ReadLocal(path string, max int64) ([]byte, error) {
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
	if !within(p, roots) {
		return nil, fmt.Errorf("%w: %q is outside the workspace and config dirs", ErrRefused, path)
	}
	if secretStorePath(p) {
		return nil, fmt.Errorf("%w: %q is a credential store", ErrRefused, path)
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			return nil, err
		}
		if !within(real, roots) || envSegment(real) || secretStorePath(real) {
			return nil, fmt.Errorf("%w: %q links outside the allowed roots", ErrRefused, path)
		}
		p = real
	} else if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %q is not a regular file", ErrRefused, path)
	}
	return boundedRead(p, max)
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
	p := filepath.Join(dir, rel)
	fi, err := os.Lstat(p)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s is not a regular file", ErrRefused, name)
	}
	if fi.Size() > HashLimit {
		return "", fmt.Errorf("%w (%d > %d bytes)", ErrTooLarge, fi.Size(), HashLimit)
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
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
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
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
	Counters   []Counter
	Failed     bool // a panic, a timeout: the outcome is a deny
}

// EvaluateGuards runs the guards of the enabled packs that match the call's
// tool. off reports a guard id switched off (guard.disable); actionFor
// applies guard.actions to an ask or deny. A panic or a blown budget is a
// deny (fail closed).
func EvaluateGuards(call policy.ToolCall, env GuardEnv, off func(string) bool, actionFor func(string, policy.Action) policy.Action) []GuardOutcome {
	var out []GuardOutcome
	for _, g := range Guards() {
		if !env.Config.IsEnabled(g.Pack) || !g.matches(call.Tool) {
			continue
		}
		if off != nil && off(g.ID) {
			continue
		}
		o := runGuard(g, call, env)
		if !o.Failed && actionFor != nil && (o.Action == policy.Ask || o.Action == policy.Deny) {
			o.Action = actionFor(g.ID, o.Action)
		}
		out = append(out, o)
	}
	return out
}

func runGuard(g Guard, call policy.ToolCall, env GuardEnv) GuardOutcome {
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
			if o.Action != policy.Allow {
				o.Consume = nil // an approval is only used by an allow
			}
			return o
		case <-timer.C:
			// HashState has its own 40 ms budget on top of the guard's.
			allowed := guardBudget + time.Duration(ctx.st.hashSpent.Load())
			el := time.Since(start)
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
