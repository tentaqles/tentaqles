package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	gosync "sync"
	"testing"
)

// fakeGHDir holds a copy of this test binary named gh(.exe). Run with TQ_FAKE_GH=1 it acts as the
// gh CLI: `gh api ...` is forwarded to the fixture server in FAKE_GH_SERVER and printed the way
// `gh api --include` prints, and `gh auth token` prints FAKE_GH_TOKEN.
var fakeGHDir string

func TestMain(m *testing.M) {
	if os.Getenv("TQ_FAKE_GH") == "1" {
		os.Exit(fakeGH(os.Args[1:]))
	}
	dir, err := os.MkdirTemp("", "tq-fake-gh-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	name := "gh"
	if runtime.GOOS == "windows" {
		name = "gh.exe"
	}
	if err := copyFile(mustExe(), filepath.Join(dir, name)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fakeGHDir = dir
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func mustExe() string {
	p, err := os.Executable()
	if err != nil {
		panic(err)
	}
	return p
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// fakeGH is the gh CLI double.
func fakeGH(args []string) int {
	if log := os.Getenv("FAKE_GH_LOG"); log != "" { // one line per run, so a test sees gh was tried
		if f, err := os.OpenFile(log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			fmt.Fprintln(f, strings.Join(args, " "))
			f.Close()
		}
	}
	switch os.Getenv("FAKE_GH_LOGGEDOUT") {
	case "1": // gh's real behaviour: the hint and exit code 4
		fmt.Fprintln(os.Stderr, "To get started with GitHub CLI, please run:  gh auth login")
		return 4
	case "exit4": // exit code 4 alone
		return 4
	case "hint": // the hint alone, with a generic exit code
		fmt.Fprintln(os.Stderr, "To get started with GitHub CLI, please run:  gh auth login")
		return 1
	}
	if len(args) >= 2 && args[0] == "auth" && args[1] == "token" {
		fmt.Println(os.Getenv("FAKE_GH_TOKEN"))
		return 0
	}
	if len(args) == 0 || args[0] != "api" {
		fmt.Fprintln(os.Stderr, "fake gh: unsupported", args)
		return 2
	}
	method, endpoint, include, input := "GET", "", false, false
	var headers []string
	for i := 1; i < len(args); i++ {
		switch a := args[i]; a {
		case "-X", "--method":
			i++
			method = args[i]
		case "-H", "--header":
			i++
			headers = append(headers, args[i])
		case "-i", "--include":
			include = true
		case "--input":
			i++
			input = args[i] == "-"
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintln(os.Stderr, "fake gh: unknown flag", a)
				return 2
			}
			endpoint = a
		}
	}
	var body io.Reader
	if input {
		b, _ := io.ReadAll(os.Stdin)
		body = bytes.NewReader(b)
	}
	u := strings.TrimRight(os.Getenv("FAKE_GH_SERVER"), "/") + "/" + strings.TrimPrefix(endpoint, "/")
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, h := range headers {
		k, v, _ := strings.Cut(h, ":")
		req.Header.Set(strings.TrimSpace(k), strings.TrimSpace(v))
	}
	req.Header.Set("X-Fake-Gh", "1")
	req.Header.Set("X-Fake-Gh-Config-Dir", os.Getenv("GH_CONFIG_DIR"))
	if os.Getenv("GH_TOKEN") != "" || os.Getenv("GITHUB_TOKEN") != "" {
		req.Header.Set("X-Fake-Gh-Ambient-Token", "1")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if include {
		fmt.Printf("%s %s\r\n", resp.Proto, resp.Status)
		for k, vs := range resp.Header {
			for _, v := range vs {
				fmt.Printf("%s: %s\r\n", k, v)
			}
		}
		fmt.Print("\r\n")
	}
	os.Stdout.Write(rb)
	if resp.StatusCode >= 400 {
		fmt.Fprintf(os.Stderr, "gh: request failed (HTTP %d)\n", resp.StatusCode)
		return 1
	}
	return 0
}

// call is one request the fixture server saw.
type call struct {
	Method, Path, Op string
	URLPath          string // decoded
	Vars             map[string]any
	Body             map[string]any
	Auth             string
	Query            string // the GraphQL document
	ViaGH            bool
	ConfigDir        string
	AmbientToken     bool
}

type reply struct {
	Status int
	Header map[string]string
	Body   string
	Drop   bool // close the connection without an answer
}

// fixture is the recorded GitHub API: GraphQL replies by operation name, REST replies by
// "METHOD path". A missing route is a 500 so a test notices.
type fixture struct {
	t     *testing.T
	srv   *httptest.Server
	mu    gosync.Mutex
	calls []call
	gql   map[string][]reply // op name -> replies (the last one repeats)
	gqlFn map[string]func(vars map[string]any) reply
	rest  map[string][]reply
}

var opRe = regexp.MustCompile(`^\s*(?:query|mutation)\s+(\w+)`)

func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t, gql: map[string][]reply{}, gqlFn: map[string]func(map[string]any) reply{}, rest: map[string][]reply{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fixture) onGQL(op string, rs ...reply) { f.mu.Lock(); f.gql[op] = rs; f.mu.Unlock() }

// onGQLFunc answers an op from its variables; it wins over onGQL replies.
func (f *fixture) onGQLFunc(op string, fn func(vars map[string]any) reply) {
	f.mu.Lock()
	f.gqlFn[op] = fn
	f.mu.Unlock()
}
func (f *fixture) onREST(route string, rs ...reply) {
	f.mu.Lock()
	f.rest[route] = rs
	f.mu.Unlock()
}

func ok(body string) reply { return reply{Status: 200, Body: body} }

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	c := call{Method: r.Method, Path: r.URL.RequestURI(), URLPath: r.URL.Path, Auth: r.Header.Get("Authorization"),
		ViaGH: r.Header.Get("X-Fake-Gh") == "1", ConfigDir: r.Header.Get("X-Fake-Gh-Config-Dir"),
		AmbientToken: r.Header.Get("X-Fake-Gh-Ambient-Token") == "1"}
	var key string
	var table map[string][]reply
	if r.URL.Path == "/graphql" {
		var q struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.Unmarshal(raw, &q); err != nil {
			http.Error(w, "bad graphql body", 400)
			return
		}
		if m := opRe.FindStringSubmatch(q.Query); m != nil {
			c.Op = m[1]
		}
		c.Vars, c.Query = q.Variables, q.Query
		key, table = c.Op, f.gql
	} else {
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &c.Body)
		}
		key, table = r.Method+" "+r.URL.Path, f.rest
	}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	rs := table[key]
	fn := f.gqlFn[c.Op]
	var rep reply
	switch {
	case c.Op != "" && fn != nil:
		f.mu.Unlock()
		rep = fn(c.Vars)
		f.mu.Lock()
	case len(rs) == 0:
		rep = reply{Status: 500, Body: `{"message":"no fixture for ` + key + `"}`}
	case len(rs) == 1:
		rep = rs[0]
	default:
		rep = rs[0]
		table[key] = rs[1:]
	}
	f.mu.Unlock()
	if rep.Drop {
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				conn.Close()
				return
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	for k, v := range rep.Header {
		w.Header().Set(k, v)
	}
	w.WriteHeader(rep.Status)
	io.WriteString(w, rep.Body)
}

func (f *fixture) all() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

// find is the calls for an op name or a "METHOD path" route.
func (f *fixture) find(key string) []call {
	var out []call
	for _, c := range f.all() {
		if c.Op == key || (c.Op == "" && c.Method+" "+c.URLPath == key) {
			out = append(out, c)
		}
	}
	return out
}

// isolate keeps a test off the real state, the real gh and any agent session's markers.
func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	for _, k := range []string{"TQ_HOME", "HOME", "USERPROFILE"} {
		t.Setenv(k, home)
	}
	for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK",
		"GH_TOKEN", "GITHUB_TOKEN", "GH_HOST", "GH_CONFIG_DIR", "FAKE_GH_LOGGEDOUT", "FAKE_GH_LOG"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

// useFakeGH puts the gh double first on PATH (the rest of PATH is kept) and points it at f.
func useFakeGH(t *testing.T, f *fixture) {
	t.Helper()
	t.Setenv("PATH", fakeGHDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TQ_FAKE_GH", "1")
	t.Setenv("FAKE_GH_SERVER", f.srv.URL)
}
