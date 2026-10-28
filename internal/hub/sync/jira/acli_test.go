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

// The fake acli is this test binary, copied onto PATH as acli(.exe): with fakeOutVar set, TestMain
// prints that file and exits with fakeCodeVar instead of running the tests. A real executable,
// not a .bat shim, because runACLI refuses batch files on Windows.
const (
	fakeOutVar  = "TQ_JIRA_FAKE_ACLI_OUT"
	fakeCodeVar = "TQ_JIRA_FAKE_ACLI_CODE"
)

func TestMain(m *testing.M) {
	if path := os.Getenv(fakeOutVar); path != "" {
		out, _ := os.ReadFile(path)
		os.Stdout.Write(out)
		code, _ := strconv.Atoi(os.Getenv(fakeCodeVar))
		os.Exit(code)
	}
	os.Exit(m.Run())
}

// fakeACLIOnPath puts an acli on PATH (first, keeping the rest) that prints out and exits with code.
func fakeACLIOnPath(t *testing.T, out string, code int) {
	t.Helper()
	bin := testutil.TempDir(t)
	msg := filepath.Join(bin, "out.txt")
	if err := os.WriteFile(msg, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	name := "acli"
	if runtime.GOOS == "windows" {
		name = "acli.exe"
	}
	if err := os.WriteFile(filepath.Join(bin, name), raw, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeOutVar, msg)
	t.Setenv(fakeCodeVar, strconv.Itoa(code))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestRunACLIRefusesBatchShims(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("batch shims are a Windows concern")
	}
	for _, name := range []string{`C:\tools\acli.bat`, `C:\tools\ACLI.CMD`} {
		old := lookPath
		lookPath = func(string) (string, error) { return name, nil }
		_, err := runACLI(context.Background(), nil, "jira", "auth", "status")
		_, herr := cliHook(context.Background(), nil)
		lookPath = old
		if err == nil || !strings.Contains(err.Error(), "batch") {
			t.Fatalf("%s: %v", name, err)
		}
		if !errors.Is(herr, cred.ErrNoCredential) || !strings.Contains(herr.Error(), "batch") {
			t.Fatalf("hook %s: %v", name, herr)
		}
	}
}

func TestRunACLICapsTheCommandLine(t *testing.T) {
	ran := false
	old := lookPath
	lookPath = func(string) (string, error) { ran = true; return "acli", nil }
	defer func() { lookPath = old }()
	_, err := runACLI(context.Background(), nil, "jira", "workitem", "comment", "create", "--body", strings.Repeat("x", 40000))
	if err == nil || !strings.Contains(err.Error(), "too long") || ran {
		t.Fatalf("ran=%v %v", ran, err)
	}
}

func TestACLISiteMustMatchTheManifest(t *testing.T) {
	for _, status := range []string{
		"✓ Authenticated\n  Site: other.atlassian.net\n  Email: tester@example.com\n",
		"✓ Authenticated\n  Email: tester@example.com\n", // no site: cannot confirm
	} {
		s := &scripted{answers: map[string]string{
			"jira auth status":     status,
			"jira workitem search": `[]`,
			"jira workitem":        "ok",
		}}
		a := newACLIAdapter(t, s)
		_, _, err := a.ListChanged(ctx, time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC))
		var se *cred.SourceError
		if !errors.As(err, &se) || se.Source != cred.SourceCLI || !strings.Contains(err.Error(), "acme.atlassian.net") {
			t.Fatalf("%q: %v", status, err)
		}
		if strings.Contains(status, "other") && !strings.Contains(err.Error(), "other.atlassian.net") {
			t.Fatalf("the error names only one site: %v", err)
		}
		if _, err := a.Check(ctx); err == nil {
			t.Fatal("Check passed with the wrong site")
		}
		if err := a.Comment(ctx, "ACME-1", "x"); err == nil {
			t.Fatal("Comment went to the wrong site")
		}
		for _, argv := range s.argv {
			if argv[1] == "workitem" {
				t.Fatalf("acli ran %q against the wrong site", argv)
			}
		}
	}
	// matching is case-insensitive and tolerates a scheme
	s := &scripted{answers: map[string]string{"jira auth status": "Site: https://ACME.atlassian.net/\nEmail: tester@example.com\n", "jira workitem search": `[]`}}
	if _, _, err := newACLIAdapter(t, s).ListChanged(ctx, time.Time{}); err != nil {
		t.Fatal(err)
	}
}

func TestACLIRefusesFlagLikeIDs(t *testing.T) {
	s := &scripted{answers: map[string]string{"jira auth status": "Site: acme.atlassian.net\n", "jira workitem": "{}"}}
	a := newACLIAdapter(t, s)
	title := "t"
	for name, err := range map[string]error{
		"get":     func() error { _, err := a.Get(ctx, "--web"); return err }(),
		"move":    a.Move(ctx, "-x", "Done"),
		"comment": a.Comment(ctx, "--help", "x"),
		"update":  a.Update(ctx, "ACME-1 --x", &title, nil),
		"labels":  a.SetLabels(ctx, "-1", []string{"a"}, nil),
		"create":  func() error { _, err := a.Create(ctx, hubsync.NewItem{Title: "t", ParentID: "--p"}); return err }(),
	} {
		if err == nil || !strings.Contains(err.Error(), "not a Jira issue key or id") {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, argv := range s.argv {
		if argv[1] == "workitem" {
			t.Fatalf("acli ran %q", argv)
		}
	}
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
