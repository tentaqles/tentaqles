// Package pgtap runs the db pack's pgTAP RLS tests (spec D §12) against a
// local or ephemeral test database: pg_prove for plain Postgres, or
// `supabase test db --local` for a Supabase project.
//
// The target is packs.db.test_db. Its connection string is resolved from the
// variable named by url_env (the environment first, then the dotenv files,
// as `tq dotenv run` loads them); the value is never printed. Every host of
// the string must be local (localhost, a loopback address, a Unix socket,
// or a docker-compose service listed in test_db.hosts), or the check fails
// before anything connects. If the database does not answer a 2 s TCP
// probe and test_db.start is set, the start command runs first (180 s by
// default). The connection reaches pg_prove as --host/--port/--dbname/
// --username flags plus PG* variables in the child's environment only.
package pgtap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/dotenv"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"github.com/tentaqles/tentaqles/internal/redact"
	"gopkg.in/yaml.v3"
)

// Mode is how the tests run.
type Mode int

const (
	// PgProve runs pg_prove against test_db.
	PgProve Mode = iota
	// Supabase runs `supabase test db --local` against the local stack.
	Supabase
)

// Tool is the tool of a mode.
func (m Mode) Tool() string {
	if m == Supabase {
		return "supabase"
	}
	return "pg_prove"
}

const (
	// ProbeTimeout bounds the TCP probe.
	ProbeTimeout = 2 * time.Second
	// StartTimeout is the default bound of test_db.start (timeouts.<tool>
	// overrides it).
	StartTimeout = 180 * time.Second
	// SupabaseDBPort is the local stack's default database port.
	SupabaseDBPort = 54322
	// RuleNotOK is the rule of a failed pgTAP test.
	RuleNotOK = "pgtap:not-ok"
)

// Dial is the TCP (or Unix socket) probe; a test seam.
var Dial = func(ctx context.Context, network, addr string) error {
	d := net.Dialer{Timeout: ProbeTimeout}
	c, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return err
	}
	return c.Close()
}

// TestDB is packs.db.test_db.
type TestDB struct {
	URLEnv string   `yaml:"url_env"`
	Start  string   `yaml:"start"`
	Hosts  []string `yaml:"hosts"`
	Dotenv []string `yaml:"dotenv"`
}

// Settings decodes packs.db.test_db (nil when unset or invalid).
func Settings(s packs.Settings) *TestDB {
	n := s.Node("test_db")
	if n == nil || ValidateTestDB(*n) != nil {
		return nil
	}
	var t TestDB
	if n.Decode(&t) != nil {
		return nil
	}
	return &t
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,253}$`)

// ValidateTestDB validates the packs.db.test_db node.
func ValidateTestDB(n yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: must be a mapping {url_env, start, hosts, dotenv}", n.Line)
	}
	has := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		has[k.Value] = true
		switch k.Value {
		case "url_env":
			if v.Kind != yaml.ScalarNode || !dotenv.ValidKey(v.Value) {
				return fmt.Errorf("line %d: url_env must be a variable name", v.Line)
			}
		case "start":
			if v.Kind != yaml.ScalarNode || len(strings.Fields(v.Value)) == 0 {
				return fmt.Errorf("line %d: start must be a command", v.Line)
			}
		case "hosts":
			if v.Kind != yaml.SequenceNode {
				return fmt.Errorf("line %d: hosts must be a list of docker-compose service names", v.Line)
			}
			for _, it := range v.Content {
				if it.Kind != yaml.ScalarNode || !hostRe.MatchString(it.Value) {
					return fmt.Errorf("line %d: %q is not a host name", it.Line, it.Value)
				}
			}
		case "dotenv":
			if v.Kind != yaml.SequenceNode {
				return fmt.Errorf("line %d: dotenv must be a list of .env files", v.Line)
			}
			for _, it := range v.Content {
				if it.Kind != yaml.ScalarNode || !dotenv.IsEnvFile(it.Value) || relErr(it.Value) != nil {
					return fmt.Errorf("line %d: %q is not a .env file inside the repository", it.Line, it.Value)
				}
			}
		default:
			return fmt.Errorf("line %d: unknown key %q (known: url_env, start, hosts, dotenv)", k.Line, k.Value)
		}
	}
	if !has["url_env"] && !has["start"] {
		return fmt.Errorf("line %d: test_db needs url_env or start", n.Line)
	}
	return nil
}

func relErr(p string) error {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	if p == "" || strings.HasPrefix(p, "/") || (len(p) > 1 && p[1] == ':') {
		return errors.New("not relative")
	}
	for _, s := range strings.Split(p, "/") {
		if s == ".." {
			return errors.New("escapes")
		}
	}
	return nil
}

// ResolveURL resolves the url_env variable the way `tq dotenv run` loads
// it: the environment wins, then the dotenv files (default .env) under
// root, the last file that sets it winning.
func ResolveURL(root string, t TestDB) string {
	if v, ok := os.LookupEnv(t.URLEnv); ok && v != "" {
		return v
	}
	files := t.Dotenv
	if len(files) == 0 {
		files = []string{".env"}
	}
	v := ""
	for _, f := range files {
		if relErr(f) != nil || !dotenv.IsEnvFile(f) {
			continue
		}
		if x := dotenv.Lookup(filepath.Join(root, filepath.FromSlash(f)), t.URLEnv); x != "" {
			v = x
		}
	}
	return v
}

// Options configures a run.
type Options struct {
	Mode Mode
	// TestsDir is the pgTAP directory (repo relative), and TestFiles the
	// test files under it, for mapping the harness's paths.
	TestsDir  string
	TestFiles []string
	// SupabasePort is config.toml [db] port (0: the default 54322).
	SupabasePort int
}

// redirectVars point libpq somewhere else than the host tq checked.
var redirectVars = []string{"PGHOSTADDR", "PGSERVICE"}

// Run runs the tests. in must be bound to the db pack.
func Run(ctx context.Context, in *packs.Input, o Options) packs.Result {
	s := in.Settings()
	tool := o.Mode.Tool()
	cfg := Settings(s)
	fail := func(detail string) packs.Result { return packs.Result{Status: packs.Fail, Detail: detail} }

	var target *Target
	if cfg != nil && cfg.URLEnv != "" {
		v := ResolveURL(in.Root, *cfg)
		if v == "" {
			return fail(cfg.URLEnv + " is not set (environment or the test_db dotenv files); point it at a local test database")
		}
		t, err := ParseTarget(v)
		if err != nil {
			return fail(err.Error())
		}
		if err := t.Local(cfg.Hosts); err != nil {
			return fail("test_db is not local: " + cfg.URLEnv + " must point at localhost, a loopback address, a Unix socket or a service listed in test_db.hosts; tq never runs the tests against a shared database")
		}
		target = &t
	}
	switch {
	case target == nil && o.Mode == PgProve && s.Declared(tool):
		return fail("pg_prove is declared but packs.db.test_db.url_env is not set: pg_prove needs a local test database")
	case target == nil && o.Mode == PgProve:
		return packs.Result{Status: packs.Skip, Detail: "packs.db.test_db is not set"}
	}
	for _, v := range redirectVars {
		if os.Getenv(v) != "" {
			return fail(v + " is set in the environment: it can send libpq elsewhere than the checked host; unset it for the tests")
		}
	}

	// The probe address.
	network, addr := "tcp", ""
	if target != nil {
		h := target.Hosts[0]
		if h.Socket() {
			network, addr = "unix", filepath.ToSlash(filepath.Join(h.Host, ".s.PGSQL."+strconv.Itoa(h.Port)))
		} else {
			addr = net.JoinHostPort(h.Host, strconv.Itoa(h.Port))
		}
	} else {
		port := o.SupabasePort
		if port == 0 {
			port = SupabaseDBPort
		}
		addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	}
	if !answers(ctx, network, addr) {
		start := ""
		if cfg != nil {
			start = cfg.Start
		}
		if start == "" {
			if o.Mode == Supabase {
				return fail("the local Supabase database does not answer: run supabase start (or set packs.db.test_db.start)")
			}
			return fail("test_db does not answer a 2 s probe and packs.db.test_db.start is not set")
		}
		if r, ok := runStart(ctx, in, start); !ok {
			return r
		}
		if !answers(ctx, network, addr) {
			return fail("test_db does not answer after test_db.start")
		}
	}

	var spec toolrun.Spec
	if o.Mode == Supabase {
		spec = toolrun.Spec{Pack: "db", Tool: tool, Args: []string{"test", "db", "--local"}, Dir: in.Root}
	} else {
		dir := o.TestsDir
		if dir == "" {
			return fail("packs.db.rls.tests is not set: pg_prove needs the pgTAP directory")
		}
		args := append([]string{"--ext", ".sql", "-r", "--failures"}, target.Args()...)
		spec = toolrun.Spec{Pack: "db", Tool: tool, Args: append(args, dir), Dir: in.Root, Env: target.Env()}
	}
	out, err := in.Tools.Run(ctx, spec)
	run := &packs.ToolRun{Name: tool, Version: out.Version, Path: out.Path, ExitCode: out.ExitCode, DurationMS: out.Duration.Milliseconds(), TimedOut: out.TimedOut}
	var secrets []string
	if target != nil {
		secrets = target.Secrets()
	}
	mask(&out, secrets)
	if err != nil {
		return packs.Result{Status: packs.Fail, Detail: tool + " did not run: " + redact.Redact(err.Error(), nil), Tool: run, Findings: []packs.Finding{runFailed(tool, "did not run")}}
	}
	if out.TimedOut {
		return packs.Result{Status: packs.Fail, Detail: tool + " timed out", Tool: run, Findings: []packs.Finding{runFailed(tool, "timed out")}}
	}
	all := append(append([]byte{}, out.Stdout...), '\n')
	all = append(all, out.Stderr...)
	tap := ParseTAP(all, MapPaths(o.TestFiles, o.TestsDir))
	var fs []packs.Finding
	for _, f := range tap.Failures {
		msg := fmt.Sprintf("pgTAP test %d failed", f.Test)
		if f.Test == 0 {
			msg = "the test file failed"
		}
		if f.Desc != "" {
			msg += ": " + capLen(redact.Redact(f.Desc, in.Classes()), 300)
		}
		fs = append(fs, packs.Finding{File: f.File, Line: f.Test, Level: packs.LevelError, Rule: RuleNotOK, Message: msg})
	}
	switch {
	case len(fs) > 0:
		return packs.Result{Status: packs.Fail, Detail: fmt.Sprintf("%d failed pgTAP test(s); fix the policy, not the test", len(fs)), Findings: fs, Tool: run}
	case out.ExitCode != 0:
		tail := out.Tail(20, in.Classes())
		return packs.Result{Status: packs.Fail, Detail: fmt.Sprintf("%s exited %d without a TAP failure:\n%s", tool, out.ExitCode, tail), Tool: run,
			Findings: []packs.Finding{runFailed(tool, fmt.Sprintf("exited %d", out.ExitCode))}}
	}
	detail := "pgTAP tests passed"
	if tap.Files > 0 {
		detail = fmt.Sprintf("%d test(s) in %d file(s) passed", tap.Tests, tap.Files)
	}
	return packs.Result{Status: packs.OK, Detail: detail, Tool: run}
}

func runFailed(tool, why string) packs.Finding {
	return packs.Finding{Level: packs.LevelError, Rule: tool + ":run-failed", Message: tool + " " + why}
}

func answers(ctx context.Context, network, addr string) bool {
	c, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	return Dial(c, network, addr) == nil
}

// runStart runs test_db.start (argv split on spaces, no shell).
func runStart(ctx context.Context, in *packs.Input, start string) (packs.Result, bool) {
	argv := strings.Fields(start)
	out, err := in.Tools.Run(ctx, toolrun.Spec{Pack: "db", Tool: argv[0], Args: argv[1:], Dir: in.Root, Timeout: StartTimeout})
	switch {
	case errors.Is(err, toolrun.ErrNotFound):
		return packs.Result{Status: packs.Fail, Detail: "test_db.start: " + argv[0] + " is not installed"}, false
	case err != nil:
		return packs.Result{Status: packs.Fail, Detail: "test_db.start did not run: " + redact.Redact(err.Error(), nil)}, false
	case out.TimedOut:
		return packs.Result{Status: packs.Fail, Detail: "test_db.start timed out"}, false
	case out.ExitCode != 0:
		return packs.Result{Status: packs.Fail, Detail: fmt.Sprintf("test_db.start exited %d:\n%s", out.ExitCode, out.Tail(20, in.Classes()))}, false
	}
	return packs.Result{}, true
}

// mask replaces the connection secrets in the tool's output.
func mask(o *toolrun.Output, secrets []string) {
	for _, s := range secrets {
		if len(s) < 3 {
			continue
		}
		o.Stdout = []byte(strings.ReplaceAll(string(o.Stdout), s, "[REDACTED:test_db]"))
		o.Stderr = []byte(strings.ReplaceAll(string(o.Stderr), s, "[REDACTED:test_db]"))
	}
}

func capLen(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
