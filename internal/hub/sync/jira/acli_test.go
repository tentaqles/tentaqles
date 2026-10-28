package jira

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// fakeACLIOnPath puts an acli on PATH (first, keeping the rest) that prints out and exits with code.
func fakeACLIOnPath(t *testing.T, out string, code int) {
	t.Helper()
	bin := testutil.TempDir(t)
	msg := filepath.Join(bin, "out.txt")
	if err := os.WriteFile(msg, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		body := "@echo off\r\ntype \"" + msg + "\"\r\nexit /b " + strconv.Itoa(code) + "\r\n"
		if err := os.WriteFile(filepath.Join(bin, "acli.bat"), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	} else {
		body := "#!/bin/sh\ncat '" + msg + "'\nexit " + strconv.Itoa(code) + "\n"
		if err := os.WriteFile(filepath.Join(bin, "acli"), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCLIHookDetectsACLI(t *testing.T) {
	hook, ok := cred.CLIHookFor("jira")
	if !ok {
		t.Fatal("no hook")
	}
	// not installed: an empty PATH dir first is not enough, so point lookPath at nothing
	old := lookPath
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	_, err := hook(context.Background(), nil)
	lookPath = old
	if !errors.Is(err, cred.ErrNoCredential) || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("missing acli: %v", err)
	}

	fakeACLIOnPath(t, "You are not logged in.\n", 1)
	_, err = hook(context.Background(), nil)
	if !errors.Is(err, cred.ErrNoCredential) || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("logged out: %v", err)
	}

	fakeACLIOnPath(t, "✓ Authenticated\n  Site: acme.atlassian.net\n  Email: tester@example.com\n", 0)
	tok, err := hook(context.Background(), []string{"ACME_X=1"})
	if err != nil || tok.Scheme != ACLIScheme || tok.Value != "" {
		t.Fatalf("logged in: %v %v", tok.Scheme, err)
	}
}

func TestRunACLIRealProcess(t *testing.T) {
	fakeACLIOnPath(t, `[{"key":"ACME-1"}]`, 0)
	out, err := runACLI(context.Background(), nil, "jira", "workitem", "search")
	if err != nil || !strings.Contains(string(out), "ACME-1") {
		t.Fatalf("%s %v", out, err)
	}
	fakeACLIOnPath(t, "boom", 2)
	if _, err := runACLI(context.Background(), nil, "jira"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("%v", err)
	}
}

// scripted is a fake acli runner: it records argv and answers by the first matching prefix.
type scripted struct {
	argv    [][]string
	answers map[string]string
}

func (s *scripted) run(_ context.Context, _ []string, args ...string) ([]byte, error) {
	s.argv = append(s.argv, args)
	j := strings.Join(args, " ")
	best := ""
	for prefix := range s.answers {
		if strings.HasPrefix(j, prefix) && len(prefix) > len(best) {
			best = prefix
		}
	}
	if best == "" {
		return nil, errors.New("acli: unexpected " + j)
	}
	return []byte(s.answers[best]), nil
}

func (s *scripted) find(prefix string) []string {
	for i := len(s.argv) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.Join(s.argv[i], " "), prefix) {
			return s.argv[i]
		}
	}
	return nil
}

func newACLIAdapter(t *testing.T, s *scripted) *Adapter {
	t.Helper()
	a, err := newAdapter(hubsync.Config{Workspace: "acme", Provider: "jira", AssignedTo: "me",
		Scope:     map[string]string{"org": "acme", "project": "ACME"},
		Transport: cred.Static(cred.SourceCLI, cred.Token{Scheme: ACLIScheme})})
	if err != nil {
		t.Fatal(err)
	}
	a.run = s.run
	a.now = func() time.Time { return time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC) }
	return a
}

func TestACLIBackend(t *testing.T) {
	s := &scripted{answers: map[string]string{
		"jira auth status":         "✓ Authenticated\n  Site: acme.atlassian.net\n  Email: tester@example.com\n",
		"jira workitem search":     `[` + issueJSON("10001", "ACME-1", "First", "In Progress", "indeterminate", "2026-10-27T10:00:00.000+0000", "") + `]`,
		"jira workitem view":       issueJSON("10001", "ACME-1", "First", "In Progress", "indeterminate", "2026-10-27T10:00:00.000+0000", ""),
		"jira workitem transition": "ok",
		"jira workitem comment":    "ok",
		"jira workitem edit":       "ok",
		"jira workitem create":     `{"id":"10011","key":"ACME-11"}`,
	}}
	a := newACLIAdapter(t, s)
	if acct, err := a.Check(ctx); err != nil || acct != "tester@example.com" {
		t.Fatal(acct, err)
	}
	items, mark, err := a.ListChanged(ctx, time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC))
	if err != nil || len(items) != 1 || items[0].Key != "ACME-1" || items[0].StatusCategory != "active" || items[0].Body != "Body of ACME-1" {
		t.Fatalf("%+v %v", items, err)
	}
	if !mark.Equal(time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)) {
		t.Fatal(mark)
	}
	argv := s.find("jira workitem search")
	if i := slices.Index(argv, "--jql"); i < 0 || !strings.Contains(argv[i+1], "assignee = currentUser()") || !slices.Contains(argv, "--json") {
		t.Fatalf("%q", argv)
	}
	if it, err := a.Get(ctx, "ACME-1"); err != nil || it.Title != "First" {
		t.Fatal(it, err)
	}
	if err := a.Move(ctx, "ACME-1", "Done"); err != nil {
		t.Fatal(err)
	}
	if argv := s.find("jira workitem transition"); !slices.Equal(argv, []string{"jira", "workitem", "transition", "--key", "ACME-1", "--status", "Done", "--yes"}) {
		t.Fatalf("%q", argv)
	}
	if err := a.Comment(ctx, "ACME-1", "done"); err != nil {
		t.Fatal(err)
	}
	if argv := s.find("jira workitem comment"); !slices.Equal(argv, []string{"jira", "workitem", "comment", "create", "--key", "ACME-1", "--body", "done"}) {
		t.Fatalf("%q", argv)
	}
	title := "T"
	if err := a.Update(ctx, "ACME-1", &title, nil); err != nil {
		t.Fatal(err)
	}
	if argv := s.find("jira workitem edit"); !slices.Equal(argv, []string{"jira", "workitem", "edit", "--key", "ACME-1", "--summary", "T", "--yes"}) {
		t.Fatalf("%q", argv)
	}
	if err := a.SetLabels(ctx, "ACME-1", []string{"a", "b"}, []string{"c"}); err != nil {
		t.Fatal(err)
	}
	if argv := s.find("jira workitem edit"); !slices.Equal(argv, []string{"jira", "workitem", "edit", "--key", "ACME-1", "--labels", "a,b", "--remove-labels", "c", "--yes"}) {
		t.Fatalf("%q", argv)
	}
	it, err := a.Create(ctx, hubsync.NewItem{Title: "Child", Body: "b", ParentID: "ACME-1"})
	if err != nil || it.Key != "ACME-1" { // the fake view answers ACME-1 for every key
		t.Fatal(it, err)
	}
	argv = s.find("jira workitem create")
	for _, want := range [][2]string{{"--project", "ACME"}, {"--type", "Subtask"}, {"--summary", "Child"}, {"--parent", "ACME-1"}, {"--description", "b"}} {
		if i := slices.Index(argv, want[0]); i < 0 || argv[i+1] != want[1] {
			t.Fatalf("%q misses %v", argv, want)
		}
	}
	if _, err := a.Statuses(ctx); !errors.Is(err, hubsync.ErrUnsupported) {
		t.Fatalf("%v", err)
	}
}
