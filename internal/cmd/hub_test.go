package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/web"
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
