package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/web"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// hubWS makes a trusted workspace "acme", isolates TQ_HOME, HOME and USERPROFILE, clears the agent
// markers and chdirs into the workspace.
func hubWS(t *testing.T) string {
	t.Helper()
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	home := filepath.Dir(os.Getenv("TQ_HOME"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK", "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN"} {
		unsetenv(t, k)
	}
	t.Setenv("TQ_HUB_GATE_CMD", `["tq-hub-test-no-gate"]`)
	os.MkdirAll(filepath.Join(root, "apps", "billing"), 0o755)
	t.Chdir(root)
	return root
}

func addCard(t *testing.T, args ...string) store.Card {
	t.Helper()
	_, out, errOut, err := runTQ(t, append([]string{"hub", "add"}, append(args, "--json")...)...)
	if err != nil {
		t.Fatalf("%v\n%s", err, errOut)
	}
	var c store.Card
	if err := json.Unmarshal([]byte(out), &c); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	return c
}

func TestHubAddListShow(t *testing.T) {
	hubWS(t)
	c := addCard(t, "Export invoices", "--kind", "feature", "--tag", "agent-ready", "--project", "apps/billing", "--body", "CSV per month.")
	if !strings.HasPrefix(c.ID, "c-") || c.Lane != "backlog" || c.ProjectDir != "apps/billing" {
		t.Fatalf("%+v", c)
	}
	_, out, _, err := runTQ(t, "hub", "list")
	if err != nil || !strings.Contains(out, c.ID) || !strings.Contains(out, "Export invoices") || !strings.Contains(out, "backlog") {
		t.Fatalf("%v\n%s", err, out)
	}
	_, out, _, err = runTQ(t, "hub", "list", "--json", "--kind", "fix")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("filter: %v %q", err, out)
	}
	_, out, _, err = runTQ(t, "hub", "show", c.ID)
	if err != nil || !strings.Contains(out, "CSV per month.") || !strings.Contains(out, "feature") {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(store.DBPath("acme")); err != nil {
		t.Fatal("the store lives under TQ_HOME/hub/<workspace>")
	}
}

func TestHubRefusesUntrustedWorkspaces(t *testing.T) {
	root := mkUntrusted(t, "evil")
	home := filepath.Dir(os.Getenv("TQ_HOME"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK", "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN"} {
		unsetenv(t, k)
	}
	t.Chdir(root)
	_, _, _, err := runTQ(t, "hub", "list")
	if err == nil || !strings.Contains(err.Error(), "untrusted") || !strings.Contains(err.Error(), "tq allow evil") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(store.DBPath("evil")); !os.IsNotExist(err) {
		t.Fatal("no database for an untrusted workspace")
	}
}

func TestHubAgentMayAddAndCommentOnly(t *testing.T) {
	hubWS(t)
	c := addCard(t, "x", "--tag", "agent-ready")
	t.Setenv("TENTAQLES_HEADLESS", "1")
	if _, _, _, err := runTQ(t, "hub", "add", "y", "--budget", "rounds=99"); err == nil || !strings.Contains(err.Error(), "human-only") {
		t.Fatalf("budget: %v", err)
	}
	if _, _, _, err := runTQ(t, "hub", "move", c.ID, "done"); err == nil || !strings.Contains(err.Error(), "human-only") {
		t.Fatalf("move to done: %v", err)
	}
	if _, _, _, err := runTQ(t, "hub", "answer", c.ID, "d1a2b3c4", "--choice", "approve"); err == nil || !strings.Contains(err.Error(), "human-only") {
		t.Fatalf("answer: %v", err)
	}
	if _, _, _, err := runTQ(t, "hub", "comment", c.ID, "progress note"); err != nil {
		t.Fatalf("comment: %v", err)
	}
	if _, _, _, err := runTQ(t, "hub", "add", "agent card"); err != nil {
		t.Fatalf("add: %v", err)
	}
	st, _ := store.OpenExisting("acme", []string{"all"})
	defer st.Close()
	cs, _ := st.Comments(c.ID)
	if len(cs) != 1 || cs[0].AuthorKind != store.AuthorAgent {
		t.Fatalf("%+v", cs)
	}
}

// TestHubTaskMarksAnAgent: a session launched by the hub (TQ_HUB_TASK) is an agent even
// without the other markers, so it cannot run, cancel or answer.
func TestHubTaskMarksAnAgent(t *testing.T) {
	hubWS(t)
	c := addCard(t, "x", "--tag", "agent-ready")
	t.Setenv("TQ_HUB_TASK", `{"hub":"`+c.ID+`","workspace":"acme","kind":"feature","external":null}`)
	if !hubAgent() {
		t.Fatal("TQ_HUB_TASK marks an agent session")
	}
	for _, args := range [][]string{
		{"hub", "run", c.ID},
		{"hub", "cancel", "r-abcdefghijkl"},
		{"hub", "answer", c.ID, "d1a2b3c4", "--choice", "approve"},
		{"hub", "move", c.ID, "in progress"},
	} {
		if _, _, _, err := runTQ(t, args...); err == nil || !strings.Contains(err.Error(), "human-only") {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestHubCommentOnTheTaskCard(t *testing.T) {
	hubWS(t)
	c := addCard(t, "x")
	t.Setenv("TQ_HUB_TASK", `{"hub":"`+c.ID+`","workspace":"acme","kind":"feature","external":null}`)
	t.Setenv("TENTAQLES_HEADLESS", "1")
	if _, _, _, err := runTQ(t, "hub", "comment", "--timeout", "2s", "-", "PR: https://github.com/acme/billing/pull/7"); err != nil {
		t.Fatal(err)
	}
	st, _ := store.OpenExisting("acme", []string{"all"})
	defer st.Close()
	if pr := runner.PRLink(st, c.ID); pr != "https://github.com/acme/billing/pull/7" {
		t.Fatalf("%q", pr)
	}
}

// TestHubCommentBodyFromStdin: with the text omitted (or "-") the body is read from stdin.
func TestHubCommentBodyFromStdin(t *testing.T) {
	hubWS(t)
	c := addCard(t, "x")
	t.Setenv("TQ_HUB_TASK", `{"hub":"`+c.ID+`","workspace":"acme","kind":"feature","external":null}`)
	for _, args := range [][]string{{"hub", "comment", "-"}, {"hub", "comment", c.ID, "-"}} {
		root := NewRoot()
		var out, errOut bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&errOut)
		root.SetIn(strings.NewReader("from stdin\n"))
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v %s", args, err, errOut.String())
		}
	}
	root := NewRoot()
	root.SetIn(strings.NewReader("  \n"))
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"hub", "comment", "-"})
	if err := root.Execute(); err == nil {
		t.Fatal("an empty comment is refused")
	}
	st, _ := store.OpenExisting("acme", []string{"all"})
	defer st.Close()
	cs, _ := st.Comments(c.ID)
	if len(cs) != 2 || cs[0].Body != "from stdin" || cs[1].Body != "from stdin" {
		t.Fatalf("%+v", cs)
	}
}

func TestCLIMoveRunningNeedsCancelRun(t *testing.T) {
	hubWS(t)
	c := addCard(t, "x", "--tag", "agent-ready", "--project", "apps/billing")
	_, out, _, err := runTQ(t, "hub", "move", c.ID, "in progress")
	if err != nil || !strings.Contains(out, "queued run r-") {
		t.Fatalf("%v %s", err, out)
	}
	st, _ := store.OpenExisting("acme", []string{"all"})
	r, _, _ := st.ActiveRun(c.ID)
	r.Status = store.StatusRunning
	st.SaveRun(r)
	st.Close()
	_, _, _, err = runTQ(t, "hub", "move", c.ID, "backlog")
	if err == nil || !strings.Contains(err.Error(), "--cancel-run") {
		t.Fatalf("want a --cancel-run refusal, got %v", err)
	}
	st, _ = store.OpenExisting("acme", []string{"all"})
	card, _ := st.Card(c.ID)
	st.Close()
	if card.Lane != "in progress" {
		t.Fatal("the card must not move")
	}
	_, out, _, err = runTQ(t, "hub", "move", c.ID, "backlog", "--cancel-run")
	if err != nil || !strings.Contains(out, "cancelled run "+r.ID) {
		t.Fatalf("%v %s", err, out)
	}
}

func TestHubTalksToARunningHub(t *testing.T) {
	hubWS(t)
	ws, err := hubWorkspace("")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := newHubService(ws, func(string) {}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Store().Close()
	var srv *web.Server
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		srv.Handler().ServeHTTP(w, r)
	}))
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	// The web guard fails closed without a host allow-list (Task 10).
	srv, err = web.New(web.Options{Backend: web.StaticBackend{Infos: []web.WorkspaceInfo{{Name: "acme", Available: true}},
		Services: map[string]*runner.Service{"acme": svc}}, LocalKey: "lk", AllowedHosts: []string{u.Host}})
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())
	if err := serveinfo.Write(serveinfo.Info{PID: os.Getpid(), Addr: u.Host, Port: port, Mode: "single", Workspaces: []string{"acme"}, LocalKey: "lk"}); err != nil {
		t.Fatal(err)
	}
	defer serveinfo.Remove(port)
	c := addCard(t, "via the API")
	if atomic.LoadInt32(&hits) < 2 { // ping + add
		t.Fatalf("the CLI wrote the store directly (%d API calls)", hits)
	}
	_, out, _, err := runTQ(t, "hub", "review", c.ID)
	if err != nil || !strings.Contains(out, ts.URL+"/cards/"+c.ID+"/review?ws=acme") {
		t.Fatalf("%v %s", err, out)
	}
	// An agent's request carries the agent header, so the hub refuses what the service refuses.
	t.Setenv("TENTAQLES_HEADLESS", "1")
	if _, _, _, err := runTQ(t, "hub", "move", c.ID, "done"); err == nil || !strings.Contains(err.Error(), "human-only") {
		t.Fatalf("agent move over the API: %v", err)
	}
}

// TestHubWarnsWhenTheGateIsUnavailable: a gate that cannot be built is reported on stderr at once,
// not first discovered when a run needs it.
func TestHubWarnsWhenTheGateIsUnavailable(t *testing.T) {
	hubWS(t)
	t.Setenv("TQ_HUB_GATE_CMD", "not json")
	_, _, errOut, err := runTQ(t, "hub", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "warning: gate unavailable: TQ_HUB_GATE_CMD must be a JSON array of strings") {
		t.Fatalf("stderr: %q", errOut)
	}
}

// countingReader records whether a command read its stdin.
type countingReader struct{ reads int }

func (r *countingReader) Read(p []byte) (int, error) { r.reads++; return 0, io.EOF }

func runTQIn(t *testing.T, in io.Reader, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRoot()
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetIn(in)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errOut.String(), err
}

// TestHubCommentWithoutTextOnATerminal: no text and an interactive stdin is an error, not a hang.
func TestHubCommentWithoutTextOnATerminal(t *testing.T) {
	hubWS(t)
	c := addCard(t, "x")
	prev := stdinIsTerminal
	stdinIsTerminal = func(io.Reader) bool { return true }
	t.Cleanup(func() { stdinIsTerminal = prev })
	in := &countingReader{}
	_, _, err := runTQIn(t, in, "hub", "comment", c.ID)
	if err == nil || !strings.Contains(err.Error(), "no comment text: pass it as an argument or pipe it on stdin") {
		t.Fatalf("%v", err)
	}
	if in.reads != 0 {
		t.Fatal("a terminal stdin is never read")
	}
}

// TestHubCommentChecksTrustBeforeStdin: an untrusted workspace fails before stdin is read.
func TestHubCommentChecksTrustBeforeStdin(t *testing.T) {
	root := mkUntrusted(t, "evil")
	home := filepath.Dir(os.Getenv("TQ_HOME"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK", "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN"} {
		unsetenv(t, k)
	}
	t.Chdir(root)
	in := &countingReader{}
	_, _, err := runTQIn(t, in, "hub", "comment", "c-abcdefghijkl")
	if err == nil || !strings.Contains(err.Error(), "untrusted") {
		t.Fatalf("%v", err)
	}
	if in.reads != 0 {
		t.Fatal("stdin was read before the workspace was trusted")
	}
}

func TestOpenBrowserOnlyLoopbackHTTP(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1:7470/?ws=acme", "http://localhost:8080/cards/c-x", "http://[::1]:7470/", "http://127.0.0.1/"} {
		if err := checkBrowserURL(u); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	for _, u := range []string{"https://127.0.0.1:7470/", "http://example.com/", "file:///C:/Windows/notepad.exe", "http://127.0.0.1.evil.com/",
		"http://user@127.0.0.1/", "javascript:alert(1)", "ms-settings:", "http://10.0.0.5:7470/", "", "127.0.0.1:7470"} {
		if err := checkBrowserURL(u); err == nil {
			t.Errorf("%q accepted", u)
		}
		if err := openBrowser(u); err == nil || !strings.Contains(err.Error(), "refusing to open") {
			t.Errorf("openBrowser(%q): %v", u, err)
		}
	}
}

// TestHubRunsGetOnlyTheirWorkspaceEnvironment: a hub started from workspace acme's shell (which
// set GH_CONFIG_DIR) launches globex's runs without acme's GH_CONFIG_DIR (spec §1, §5).
func TestHubRunsGetOnlyTheirWorkspaceEnvironment(t *testing.T) {
	py := testutil.Python(t)
	fake, _ := filepath.Abs(filepath.Join("..", "hub", "adapter", "testdata", "fake_claude.py")) // before hubWS changes the cwd
	// acme: identities { gh }
	root := hubWS(t)
	globex := filepath.Join(filepath.Dir(root), "globex")
	os.MkdirAll(filepath.Join(globex, "apps", "web"), 0o755)
	mp := filepath.Join(globex, manifest.FileName)
	os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: globex\nidentities: { claude: {} }\n"), 0o600)
	h, err := trust.HashFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.Allow(h); err != nil {
		t.Fatal(err)
	}
	// acme's shell hook state: GH_CONFIG_DIR was unset before acme set it.
	acmeGH := filepath.Join(os.Getenv("TQ_HOME"), "identities", "acme", "gh")
	t.Setenv("GH_CONFIG_DIR", acmeGH)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(os.Getenv("TQ_HOME"), "identities", "acme", "claude"))
	t.Setenv(envplan.StateVar, envplan.State{WS: "acme", Prev: map[string]*string{"GH_CONFIG_DIR": nil, "CLAUDE_CONFIG_DIR": nil}}.Encode())
	t.Setenv("TQ_WS", "acme")
	dir := t.TempDir()
	script, _ := json.Marshal(map[string]any{"counter": filepath.Join(dir, "n"), "argv_log": filepath.Join(dir, "argv.jsonl"),
		"rounds": []map[string]any{{"result": "I looked around.", "cost": 0.1}}})
	os.WriteFile(filepath.Join(dir, "script.json"), script, 0o600)
	t.Setenv("FAKE_CLAUDE_SCRIPT", filepath.Join(dir, "script.json"))
	argv, _ := json.Marshal(append(py, fake))
	t.Setenv("TQ_HUB_CLAUDE_CMD", string(argv))
	t.Chdir(globex)
	c := addCard(t, "x", "--tag", "agent-ready", "--project", "apps/web")
	if _, _, _, err := runTQ(t, "hub", "move", c.ID, "in progress"); err != nil {
		t.Fatal(err)
	}
	if _, out, errOut, err := runTQ(t, "hub", "serve", "--drain", "--no-notify"); err != nil {
		t.Fatalf("%v\n%s\n%s", err, out, errOut)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "argv.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var call map[string]any
	if err := json.Unmarshal(bytes.SplitN(raw, []byte("\n"), 2)[0], &call); err != nil {
		t.Fatalf("argv log %q: %v", raw, err)
	}
	if call["gh_config_dir"] != nil {
		t.Fatalf("globex's run got acme's GH_CONFIG_DIR: %v", call["gh_config_dir"])
	}
	if call["ws"] != "globex" {
		t.Fatalf("TQ_WS %v", call["ws"])
	}
	if cd, _ := call["claude_config_dir"].(string); !strings.Contains(cd, filepath.Join("identities", "globex", "claude")) {
		t.Fatalf("CLAUDE_CONFIG_DIR %q is not globex's", cd)
	}
}

// TestHubAgentAddRules: an agent adds only into planning lanes and never carries a budget in the
// card body's front matter.
func TestHubAgentAddRules(t *testing.T) {
	hubWS(t)
	t.Setenv("TENTAQLES_HEADLESS", "1")
	for _, args := range [][]string{
		{"hub", "add", "x", "--lane", "done"},
		{"hub", "add", "x", "--lane", "in progress", "--tag", "agent-ready"},
		{"hub", "add", "x", "--body", "---\nbudget: rounds=999\n---\nDo it."},
	} {
		if _, _, _, err := runTQ(t, args...); err == nil || !strings.Contains(err.Error(), "human-only") {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if c := addCard(t, "planned", "--lane", "todo"); c.Lane != "todo" {
		t.Fatalf("%+v", c)
	}
	st, _ := store.OpenExisting("acme", []string{"all"})
	defer st.Close()
	if cs, _ := st.ListCards(store.CardFilter{}); len(cs) != 1 {
		t.Fatalf("only the todo card is stored: %+v", cs)
	}
}

// TestHubAgentSignals: each agent marker alone makes the CLI an agent; none of them, a human.
func TestHubAgentSignals(t *testing.T) {
	markers := []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK"}
	for _, tc := range []struct {
		name, key, val string
		want           bool
	}{
		{"none", "", "", false},
		{"headless", "TENTAQLES_HEADLESS", "1", true},
		{"headless other value", "TENTAQLES_HEADLESS", "0", false},
		{"plugin root", "CLAUDE_PLUGIN_ROOT", filepath.Join("x", "plugin"), true},
		{"claudecode", "CLAUDECODE", "1", true},
		{"hub task", "TQ_HUB_TASK", `{"hub":"c-abcdefghijkl","workspace":"acme","kind":"feature","external":null}`, true},
		{"blank hub task", "TQ_HUB_TASK", "  ", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range markers {
				unsetenv(t, k)
			}
			if tc.key != "" {
				t.Setenv(tc.key, tc.val)
			}
			if got := hubAgent(); got != tc.want {
				t.Fatalf("hubAgent() = %v, want %v", got, tc.want)
			}
		})
	}
}
