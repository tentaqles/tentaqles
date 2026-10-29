// Package toolrun runs the external tools behind pack checks (spec C §2.6):
// a fixed probe order, a bounded --version capture, argv-only execution with
// a timeout that kills the whole process tree (a Job Object on Windows, a
// process group elsewhere), capped output and no shell.
package toolrun

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/redact"
)

const (
	// DefaultTimeout applies when neither the manifest nor the check sets one.
	DefaultTimeout = 120 * time.Second
	// MaxTimeout caps every tool timeout.
	MaxTimeout = 1800 * time.Second
	// VersionTimeout bounds `<tool> --version`.
	VersionTimeout = 5 * time.Second
	// WaitDelay bounds the wait for pipes after the process exits or is killed.
	WaitDelay = 5 * time.Second
	// MaxOutput caps stdout and stderr, each.
	MaxOutput = 1 << 20
)

// Telemetry holds the known telemetry switches added to a tool's
// environment, besides DO_NOT_TRACK=1 for every tool. dbt Core reads
// DBT_SEND_ANONYMOUS_USAGE_STATS; dbt v2 / the Fusion engine reads
// DBT_ENGINE_SEND_ANONYMOUS_USAGE_STATS (docs.getdbt.com usage-stats).
var Telemetry = map[string][]string{
	"dbt": {"DBT_SEND_ANONYMOUS_USAGE_STATS=false", "DBT_ENGINE_SEND_ANONYMOUS_USAGE_STATS=false"},
}

// Probe is where a tool was found and its version.
type Probe struct {
	Tool    string `json:"tool"`
	Found   bool   `json:"found"`
	Path    string `json:"path,omitempty"`
	Source  string `json:"source,omitempty"` // tool_paths | venv | path
	Version string `json:"version,omitempty"`
}

// Spec is one tool invocation. Args never go through a shell.
type Spec struct {
	Pack, Tool string
	Args       []string
	Dir        string        // working directory ("" = Runner.Root)
	Timeout    time.Duration // the check's default; the manifest's timeouts.<tool> wins
	Env        []string      // extra KEY=VALUE pairs (never read from .env files)
	// PassEnv are more parent variable names the tool may see, besides the
	// base allow-list (BaseEnv): a check's declared Check.Env. A declared
	// name passes even when it looks like a secret.
	PassEnv []string
	Stdin   []byte
}

// Output is what the tool printed, capped at MaxOutput per stream.
type Output struct {
	Stdout, Stderr                   []byte
	StdoutTruncated, StderrTruncated bool
	ExitCode                         int
	Duration                         time.Duration
	TimedOut                         bool
	Path, Version                    string
	// Warnings are framework notes for the check, such as a declared
	// secret not passed to a repo-local binary (names only, never values).
	Warnings []string
}

// Tail returns the last n lines of stderr (stdout when stderr is empty),
// passed through redact with classes (nil: every detector).
func (o Output) Tail(n int, classes []string) string {
	src := o.Stderr
	if len(bytes.TrimSpace(src)) == 0 {
		src = o.Stdout
	}
	lines := strings.Split(strings.TrimRight(string(src), "\r\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if len(classes) == 0 {
		classes = []string{"all"}
	}
	return redact.Redact(strings.Join(lines, "\n"), classes)
}

// Runner probes and runs tools for one repository.
type Runner struct {
	Root string
	// ToolPaths and Timeouts come from the merged pack config, keyed by pack
	// then tool. ToolPaths is trusted-layer only.
	ToolPaths map[string]map[string]string
	Timeouts  map[string]map[string]time.Duration
	// LookPath defaults to exec.LookPath.
	LookPath func(string) (string, error)
	// NoExecRepo: never execute a repo-local binary (the .venv source) to
	// read its version; tq doctor and tq pack doctor set it.
	NoExecRepo bool

	mu     sync.Mutex
	probes map[string]Probe
}

// Probe finds tool: tool_paths.<tool>, then the project's .venv, then PATH.
// A found tool's version is captured once (5 s bound).
func (r *Runner) Probe(pack, tool string) Probe {
	key := pack + "\x00" + tool
	r.mu.Lock()
	if p, ok := r.probes[key]; ok {
		r.mu.Unlock()
		return p
	}
	r.mu.Unlock()
	p := Probe{Tool: tool}
	if tp := r.ToolPaths[pack][tool]; tp != "" {
		if fi, err := os.Stat(tp); err == nil && !fi.IsDir() {
			p.Found, p.Path, p.Source = true, tp, "tool_paths"
		}
	}
	if !p.Found && r.Root != "" {
		cand := filepath.Join(r.Root, ".venv", "bin", tool)
		if runtime.GOOS == "windows" {
			cand = filepath.Join(r.Root, ".venv", "Scripts", tool+".exe")
		}
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			p.Found, p.Path, p.Source = true, cand, "venv"
		}
	}
	if !p.Found {
		look := r.LookPath
		if look == nil {
			look = exec.LookPath
		}
		if lp, err := look(tool); err == nil {
			p.Found, p.Path, p.Source = true, lp, "path"
		}
	}
	if p.Found && !(r.NoExecRepo && p.Source == "venv") {
		ctx, cancel := context.WithTimeout(context.Background(), VersionTimeout)
		p.Version = Version(ctx, p.Path)
		cancel()
	}
	r.mu.Lock()
	if r.probes == nil {
		r.probes = map[string]Probe{}
	}
	r.probes[key] = p
	r.mu.Unlock()
	return p
}

// Version runs `<path> --version` (bounded by ctx and VersionTimeout) and
// returns the parsed version, or "" when it cannot be read.
func Version(ctx context.Context, path string) string {
	ctx, cancel := context.WithTimeout(ctx, VersionTimeout)
	defer cancel()
	out, _ := run(ctx, path, []string{"--version"}, "", toolEnv("", nil, nil), nil)
	if v := ParseVersion(string(out.Stdout)); v != "" {
		return v
	}
	return ParseVersion(string(out.Stderr))
}

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)(?:\.(\d+))?`)

// ParseVersion pulls the first x.y[.z] out of s as "x.y.z", or "".
func ParseVersion(s string) string {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	if m[3] == "" {
		m[3] = "0"
	}
	return m[1] + "." + m[2] + "." + m[3]
}

// CompareVersions compares two x.y.z versions: -1, 0 or 1. An unreadable
// side compares as lower.
func CompareVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < 3; i++ {
		switch {
		case pa[i] < pb[i]:
			return -1
		case pa[i] > pb[i]:
			return 1
		}
	}
	return 0
}

func versionParts(v string) [3]int {
	var out [3]int
	m := versionRe.FindStringSubmatch(v)
	if m == nil {
		return [3]int{-1, -1, -1}
	}
	for i := 0; i < 3; i++ {
		out[i], _ = strconv.Atoi(m[i+1])
	}
	return out
}

// ErrNotFound is returned by Run when the tool cannot be found.
var ErrNotFound = errors.New("tool not found")

// Timeout resolves a spec's timeout: timeouts.<tool> from the manifest, else
// the check's default, else DefaultTimeout; capped at MaxTimeout.
func (r *Runner) Timeout(s Spec) time.Duration {
	t := s.Timeout
	if v := r.Timeouts[s.Pack][s.Tool]; v > 0 {
		t = v
	}
	if t <= 0 {
		t = DefaultTimeout
	}
	if t > MaxTimeout {
		t = MaxTimeout
	}
	return t
}

// Run runs the tool with argv only. A timeout kills the whole process tree
// and returns TimedOut; a non-zero exit is not an error (the check reads
// ExitCode). The tool gets a filtered environment (toolEnv): the base
// allow-list, the names in PassEnv, DO_NOT_TRACK=1, the tool's telemetry
// switch and Env; never the rest of the parent environment.
func (r *Runner) Run(ctx context.Context, s Spec) (Output, error) {
	p := r.Probe(s.Pack, s.Tool)
	if !p.Found {
		return Output{}, fmt.Errorf("%s: %w", s.Tool, ErrNotFound)
	}
	if err := batchSafe(p.Path, s.Args); err != nil {
		return Output{Path: p.Path, Version: p.Version}, err
	}
	dir := s.Dir
	if dir == "" {
		dir = r.Root
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout(s))
	defer cancel()
	pass, dropped := s.PassEnv, []string(nil)
	if r.RepoLocal(p) {
		pass, dropped = withoutSecretNames(s.PassEnv)
	}
	out, err := run(ctx, p.Path, s.Args, dir, toolEnv(s.Tool, pass, s.Env), s.Stdin)
	out.Path, out.Version = p.Path, p.Version
	if len(dropped) > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("%s resolves inside the repository (%s); the declared secret variable(s) %s were not passed to it. Install the tool outside the repo or set packs.%s.tool_paths.%s in the manifest", s.Tool, p.Source, strings.Join(dropped, ", "), s.Pack, s.Tool))
	}
	return out, err
}

// FinalPath resolves a path to its final long, link-free form. The packs
// package sets it to packs.FinalPath (handle based: junctions, symlinks and
// 8.3 names); the default is EvalSymlinks.
var FinalPath = func(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// RepoLocal reports whether the probed binary lives inside the repository
// (the project .venv, node_modules/.bin, any path under Root, by its final
// path too): code the repository controls. A path that cannot be resolved
// counts as repo-local (fail closed: a declared secret is dropped).
func (r *Runner) RepoLocal(p Probe) bool {
	if p.Source == "venv" {
		return true
	}
	if r.Root == "" || p.Path == "" || p.Source == "tool_paths" {
		return false
	}
	in := func(path, root string) bool {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return false
		}
		return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
	}
	norm := func(s string) string {
		s = filepath.Clean(s)
		if runtime.GOOS == "windows" {
			s = strings.ToLower(s)
		}
		return s
	}
	abs, _ := filepath.Abs(p.Path)
	root, _ := filepath.Abs(r.Root)
	if in(norm(abs), norm(root)) {
		return true
	}
	fp, err1 := FinalPath(abs)
	fr, err2 := FinalPath(root)
	if err1 != nil || err2 != nil {
		return true
	}
	return in(norm(fp), norm(fr))
}

// credValue is a value that carries credentials in a URL or URI
// (scheme://user:pass@host).
var credValue = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@]*:[^/\s@]*@`)

// secretVar reports a variable that looks like a secret by its name or its
// value.
func secretVar(name, value string) bool {
	return secretName.MatchString(name) || credValue.MatchString(value)
}

// withoutSecretNames splits declared names into those that may pass to a
// repo-local binary and the secret-looking ones (by name, or by a
// credential-bearing URL value) that may not.
func withoutSecretNames(names []string) (keep, dropped []string) {
	for _, n := range names {
		if secretVar(n, os.Getenv(n)) {
			dropped = append(dropped, n)
		} else {
			keep = append(keep, n)
		}
	}
	return keep, dropped
}

// BaseEnv are the parent variables every tool sees (spec C Â§2.6, spec D
// Â§20): the search path, the home and temp folders, the Windows system
// variables a process needs, and the locale (LANG, LC_*).
var BaseEnv = []string{"PATH", "HOME", "USERPROFILE", "TEMP", "TMP", "TMPDIR", "SystemRoot", "ComSpec", "PATHEXT", "LANG"}

// secretName is a variable name that looks like a credential. It never
// passes unless a check declares it by name.
var secretName = regexp.MustCompile(`(?i)(TOKEN|SECRET|PASSW(OR)?D|PASS$|PWD$|CREDENTIAL|AUTH|API_?KEY|ACCESS_?KEY|PRIVATE|SESSION|COOKIE|_KEY$|^KEY$|DSN|CONN(ECTION)?_?STR|_PAT$|_URL$|_URI$|^DATABASE_URL$|^(DB|DATABASE|REDIS|MONGO\w*|AMQP|RABBITMQ|KAFKA|ELASTIC\w*)_UR[LI])`)

// localeValue is what a LANG or LC_* value may be (en_US.UTF-8, C, POSIX,
// de_DE@euro); anything else is not a locale and is dropped (R5-5).
var localeValue = regexp.MustCompile(`^[A-Za-z0-9_.@-]{0,64}$`)

// testPass are names the package tests pass to the fake tool.
var testPass []string

// PassForTests adds names every tool run passes, for the fake tools of
// other packages' tests (TQ_FAKE_*). Only _test.go files call it; it
// returns the restore function.
func PassForTests(names ...string) (restore func()) {
	prev := testPass
	testPass = append(append([]string(nil), prev...), names...)
	return func() { testPass = prev }
}

// toolEnv is the environment of a tool run: the parent's BaseEnv and LC_*
// variables, the names in pass, DO_NOT_TRACK=1, the tool's telemetry
// switches, then extra (explicit KEY=VALUE pairs). A base or LC_* name that
// looks like a secret is dropped; a name in pass is the check's explicit
// declaration and passes.
func toolEnv(tool string, pass, extra []string) []string {
	allowed := map[string]bool{}
	for _, n := range BaseEnv {
		allowed[strings.ToUpper(n)] = true
	}
	declared := map[string]bool{}
	for _, n := range append(append([]string(nil), pass...), testPass...) {
		declared[strings.ToUpper(n)] = true
	}
	var env []string
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			continue
		}
		u := strings.ToUpper(k)
		switch {
		case declared[u]:
		case allowed[u] && !secretVar(k, v) && (u != "LANG" || localeValue.MatchString(v)):
		case strings.HasPrefix(u, "LC_") && !secretVar(k, v) && localeValue.MatchString(v):
		default:
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "DO_NOT_TRACK=1")
	env = append(env, Telemetry[tool]...)
	return append(env, extra...)
}

// run executes path with args, killing the tree when ctx ends.
func run(ctx context.Context, path string, args []string, dir string, env []string, stdin []byte) (Output, error) {
	var out Output
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	cmd.Env = env
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	so, se := &capped{max: MaxOutput}, &capped{max: MaxOutput}
	cmd.Stdout, cmd.Stderr = so, se
	cmd.WaitDelay = WaitDelay
	prepareTree(cmd)
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return out, err
	}
	tree, err := attachTree(cmd)
	if err != nil {
		_ = cmd.Wait()
		return out, fmt.Errorf("could not contain the tool's process tree: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var werr error
	select {
	case werr = <-done:
	case <-ctx.Done():
		out.TimedOut = true
		tree.kill()
		werr = <-done
	}
	tree.close()
	out.Duration = time.Since(start)
	out.Stdout, out.StdoutTruncated = so.buf.Bytes(), so.truncated
	out.Stderr, out.StderrTruncated = se.buf.Bytes(), se.truncated
	if cmd.ProcessState != nil {
		out.ExitCode = cmd.ProcessState.ExitCode()
	}
	var ee *exec.ExitError
	if errors.As(werr, &ee) || out.TimedOut || errors.Is(werr, exec.ErrWaitDelay) {
		werr = nil
	}
	return out, werr
}

// capped keeps the first max bytes and drops the rest, so a chatty tool
// cannot exhaust memory.
type capped struct {
	max       int
	buf       bytes.Buffer
	truncated bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
			c.truncated = true
		} else {
			c.buf.Write(p)
		}
	} else if len(p) > 0 {
		c.truncated = true
	}
	return len(p), nil
}

// Scratch creates $TQ_HOME/packs/<ws>/scratch/<run-id>/ (a temp dir outside a
// workspace) for tools that write artifacts, and a cleanup that removes it.
func Scratch(ws string) (string, func(), error) {
	var b [8]byte
	_, _ = rand.Read(b[:])
	id := hex.EncodeToString(b[:])
	var dir string
	if ws != "" && !strings.ContainsAny(ws, `/\:`) && ws != "." && ws != ".." {
		dir = filepath.Join(paths.Home(), "packs", ws, "scratch", id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", func() {}, err
		}
	} else {
		d, err := os.MkdirTemp("", "tq-pack-"+id)
		if err != nil {
			return "", func() {}, err
		}
		dir = d
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

// ErrUnsafeBatchArgs refuses a .cmd/.bat tool whose arguments hold cmd.exe
// metacharacters: Windows runs batch files through cmd.exe, which would
// read them as commands ("argv only" would no longer hold).
var ErrUnsafeBatchArgs = errors.New("a .cmd/.bat tool cannot take arguments with & | ^ % < > ! or quotes")

func batchSafe(path string, args []string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".cmd" && ext != ".bat" {
		return nil
	}
	for _, a := range args {
		if strings.ContainsAny(a, "&|^%<>!\"\r\n") {
			return ErrUnsafeBatchArgs
		}
	}
	return nil
}
