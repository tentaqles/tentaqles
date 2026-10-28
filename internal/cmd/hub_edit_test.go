package cmd

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/web"
)

func storedCard(t *testing.T, id string) store.Card {
	t.Helper()
	st, err := store.OpenExisting("acme", []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	c, err := st.Card(id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestHubEditByTheHuman: the human edits title, body (from a file) and tags in one call; tags are
// added and removed against the stored list.
func TestHubEditByTheHuman(t *testing.T) {
	root := hubWS(t)
	c := addCard(t, "FR-1 Add a todo", "--tag", "prd", "--tag", "stale")
	body := filepath.Join(root, "body.md")
	if err := os.WriteFile(body, []byte("Refined.\n\n**Acceptance checks**\n- it works\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, out, errOut, err := runTQ(t, "hub", "edit", c.ID, "--title", "FR-1 Add a todo (refined)", "--body-file", body,
		"--add-tag", "agent-ready", "--remove-tag", "stale")
	if err != nil {
		t.Fatalf("%v\n%s", err, errOut)
	}
	if !strings.Contains(out, "edited "+c.ID) {
		t.Fatalf("%q", out)
	}
	got := storedCard(t, c.ID)
	if got.Title != "FR-1 Add a todo (refined)" || !strings.Contains(got.Body, "- it works") {
		t.Fatalf("%+v", got)
	}
	if !slices.Equal(got.Tags, []string{"prd", "agent-ready"}) {
		t.Fatalf("tags %v", got.Tags)
	}
	// Adding a tag the card already has, or removing one it lacks, changes nothing else.
	if _, _, _, err := runTQ(t, "hub", "edit", c.ID, "--add-tag", "prd", "--remove-tag", "nope"); err != nil {
		t.Fatal(err)
	}
	if got := storedCard(t, c.ID); !slices.Equal(got.Tags, []string{"prd", "agent-ready"}) {
		t.Fatalf("tags %v", got.Tags)
	}
}

func TestHubEditNeedsSomethingToChange(t *testing.T) {
	hubWS(t)
	c := addCard(t, "x")
	if _, _, _, err := runTQ(t, "hub", "edit", c.ID); err == nil || !strings.Contains(err.Error(), "nothing to edit") {
		t.Fatalf("%v", err)
	}
	if _, _, _, err := runTQ(t, "hub", "edit", c.ID, "--body", "a", "--body-file", "b"); err == nil || !strings.Contains(err.Error(), "--body or --body-file") {
		t.Fatalf("%v", err)
	}
}

// TestHubEditByAnAgent: an agent session may change tags on a card in a planning lane, and nothing
// else: a body or title edit and a tag edit outside a planning lane are human-only.
func TestHubEditByAnAgent(t *testing.T) {
	hubWS(t)
	c := addCard(t, "FR-2 List todos")
	moved := addCard(t, "running work")
	if _, _, _, err := runTQ(t, "hub", "move", moved.ID, "in progress"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDECODE", "1")
	if _, _, errOut, err := runTQ(t, "hub", "edit", c.ID, "--add-tag", "agent-ready", "--json"); err != nil {
		t.Fatalf("agent tag edit on a planning lane: %v %s", err, errOut)
	}
	if got := storedCard(t, c.ID); !slices.Equal(got.Tags, []string{"agent-ready"}) {
		t.Fatalf("tags %v", got.Tags)
	}
	for _, args := range [][]string{
		{"hub", "edit", c.ID, "--body", "agent rewrite"},
		{"hub", "edit", c.ID, "--title", "agent title"},
		{"hub", "edit", moved.ID, "--add-tag", "agent-ready"},
	} {
		if _, _, _, err := runTQ(t, args...); err == nil || !strings.Contains(err.Error(), "human-only") {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if got := storedCard(t, c.ID); got.Title != "FR-2 List todos" || got.Body != "" {
		t.Fatalf("%+v", got)
	}
}

// TestHubEditTalksToARunningHub: with a hub serving the workspace the edit goes over the API, and
// the agent header makes the hub apply the same rules.
func TestHubEditTalksToARunningHub(t *testing.T) {
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
	var edits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/cards/c-") && strings.Count(r.URL.Path, "/") == 3 {
			atomic.AddInt32(&edits, 1)
		}
		srv.Handler().ServeHTTP(w, r)
	}))
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
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
	c := addCard(t, "via the API", "--tag", "prd")
	t.Setenv("TENTAQLES_HEADLESS", "1")
	if _, _, errOut, err := runTQ(t, "hub", "edit", c.ID, "--add-tag", "agent-ready"); err != nil {
		t.Fatalf("%v %s", err, errOut)
	}
	if atomic.LoadInt32(&edits) != 1 {
		t.Fatalf("the edit did not go over the API (%d)", edits)
	}
	got, err := svc.Store().Card(c.ID)
	if err != nil || !slices.Equal(got.Tags, []string{"prd", "agent-ready"}) {
		t.Fatalf("%v %v", err, got.Tags)
	}
	if _, _, _, err := runTQ(t, "hub", "edit", c.ID, "--body", "x"); err == nil || !strings.Contains(err.Error(), "human-only") {
		t.Fatalf("agent body edit over the API: %v", err)
	}
}
