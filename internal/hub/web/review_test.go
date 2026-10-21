package web

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=T", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func section(body, id string) string {
	i := strings.Index(body, `<section id="`+id+`"`)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	j := strings.Index(rest, "</section>")
	if j < 0 {
		return rest
	}
	return rest[:j]
}

func ok(b bool) *bool { return &b }

// reviewEnv: a card waiting on a result, a git repo with one commit after the base, gate records.
func reviewEnv(t *testing.T) (*env, store.Card, store.Run) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	repo := filepath.Join(e.root, "apps", "billing")
	os.MkdirAll(repo, 0o755)
	git(t, repo, "init", "-q")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a"), 0o644)
	git(t, repo, "add", "a.txt")
	git(t, repo, "commit", "-q", "-m", "chore: base")
	base := git(t, repo, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(repo, "export.py"), []byte("print(1)\n"), 0o644)
	git(t, repo, "add", "export.py")
	git(t, repo, "commit", "-q", "-m", "feat(demo#1): export csv")
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "Export invoices", Body: "CSV per month.", ProjectDir: "apps/billing", Tags: []string{store.TagAgentReady}}, nil)
	r, _ := e.pendingDecision("acme", c, store.KindResult)
	e.gate.mu.Lock()
	st := e.gate.states["demo"]
	st.Bases = map[string]string{"1": base}
	st.Checkpoints = []gate.Checkpoint{{ID: "1", Title: "Parse rows", Status: "passed", Gates: map[string]string{"behavior": "passed", "review": "passed"}, Evidence: "fresh", Reviews: map[string]string{"build-critic": "PASS"}}}
	st.Records = &gate.Records{
		Plan:     &gate.PlanRecord{Summary: "Export invoices as CSV.", Spec: "docs/specs/demo.md", Checkpoints: []gate.PlanCheckpoint{{ID: "1", Title: "Parse rows", Approved: true, Tests: []gate.PlanTest{{File: "tests/test_rows.py", Layer: "unit"}}}}},
		Evidence: []gate.Evidence{{Checkpoint: "1", TS: "2026-10-21T09:30:00+00:00", Tree: "abcdef0123456789", OK: true, Current: true, Summary: "all verify commands passed", Results: []gate.VerifyResult{{Cmd: "python -m pytest tests/test_rows.py -q", Exit: 0, Seconds: 1.2}}}},
		Reviews:  []gate.Review{{Checkpoint: "1", Reviewer: "build-critic", Result: "PASS", Tree: "abcdef0123456789", TS: "2026-10-21T09:40:00+00:00", Minors: 1}},
		DoD:      &gate.DoDRecord{OK: true, Tree: "abcdef0123456789", Checks: []gate.DoDCheck{{ID: "test", OK: ok(true), Detail: "exit 0: python -m pytest -q"}}},
	}
	e.gate.mu.Unlock()
	svc.Store().AddComment(c.ID, store.AuthorAgent, "agent", "ALL GREEN, trust me. PR: https://github.com/acme/billing/pull/9", r.ID)
	svc.OnApprove = func(card store.Card, run store.Run) {
		if _, err := ExportReview(context.Background(), svc, card, run); err != nil {
			t.Errorf("export: %v", err)
		}
	}
	return e, c, r
}

func TestReviewPageIsBuiltFromGateRecords(t *testing.T) {
	e, c, _ := reviewEnv(t)
	ts := e.server(Options{})
	code, body, _ := get(t, ts, "/cards/"+c.ID+"/review?ws=acme", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	for _, id := range []string{"goal", "approach", "done", "stands", "evidence", "cost", "feedback", "approve"} {
		if section(body, id) == "" {
			t.Fatalf("section %s missing", id)
		}
	}
	ev := section(body, "evidence")
	if !strings.Contains(ev, "python -m pytest tests/test_rows.py -q") || !strings.Contains(ev, "abcdef012345") {
		t.Fatalf("evidence:\n%s", ev)
	}
	if strings.Contains(ev, "trust me") || strings.Contains(section(body, "stands"), "trust me") {
		t.Fatal("agent text must never appear as evidence or status")
	}
	if !strings.Contains(section(body, "done"), "feat(demo#1): export csv") || !strings.Contains(section(body, "done"), "pull/9") {
		t.Fatalf("done:\n%s", section(body, "done"))
	}
	if !strings.Contains(section(body, "approach"), "tests/test_rows.py") || !strings.Contains(section(body, "goal"), "docs/specs/demo.md") {
		t.Fatal("approach and goal come from the signed plan record")
	}
	if !strings.Contains(section(body, "approve"), `action="/cards/`+c.ID+`/review/approve?ws=acme"`) || strings.Contains(body, "/review/done") {
		t.Fatal("a pending result offers Approve, not Mark done")
	}
}

func TestApproveFromTheReviewPageExportsIt(t *testing.T) {
	e, c, r := reviewEnv(t)
	ts := e.server(Options{})
	code, _, hdr := postForm(t, ts, "/cards/"+c.ID+"/review/approve?ws=acme", url.Values{}, origin(ts))
	if code != http.StatusSeeOther || hdr.Get("Location") != "/cards/"+c.ID+"?ws=acme&flash=approved" {
		t.Fatalf("%d %s", code, hdr.Get("Location"))
	}
	got, _ := e.svcs["acme"].Store().Card(c.ID)
	if got.Lane != "done" || e.gate.answers[0] != "demo d1a2b3c4 approve hub:tester" {
		t.Fatalf("lane %s answers %v", got.Lane, e.gate.answers)
	}
	out := filepath.Join(store.RunsDir("acme"), r.ID+".review.html")
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	if strings.Contains(html, "<script") || strings.Contains(html, "<form") || !strings.Contains(html, "python -m pytest tests/test_rows.py -q") || !strings.Contains(html, "<style>") {
		t.Fatalf("export:\n%s", html)
	}
}

func TestRequestChangesFromTheReviewPage(t *testing.T) {
	e, c, _ := reviewEnv(t)
	ts := e.server(Options{})
	code, _, _ := postForm(t, ts, "/cards/"+c.ID+"/review/request-changes?ws=acme", url.Values{"note": {"Name the file by month."}}, origin(ts))
	if code != http.StatusSeeOther || e.gate.answers[0] != "demo d1a2b3c4 request-changes hub:tester" {
		t.Fatalf("%d %v", code, e.gate.answers)
	}
	cs, _ := e.svcs["acme"].Store().Comments(c.ID)
	if cs[len(cs)-1].Body != "Name the file by month." || cs[len(cs)-1].AuthorKind != store.AuthorHuman {
		t.Fatalf("%+v", cs[len(cs)-1])
	}
}

func TestReviewWithoutAResultOffersMarkDone(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x"}, nil)
	ts := e.server(Options{})
	if code, body, _ := get(t, ts, "/cards/"+c.ID+"/review?ws=acme", nil); code != http.StatusNotFound || !strings.Contains(body, "No finished run") {
		t.Fatalf("%d %s", code, body)
	}
	r, _, _ := svc.Enqueue(context.Background(), c.ID, "test")
	r.Status = store.StatusFinished
	svc.Store().SaveRun(r)
	svc.Store().Release(c.ID, r.ID)
	_, body, _ := get(t, ts, "/cards/"+c.ID+"/review?ws=acme", nil)
	if !strings.Contains(body, "/review/done?ws=acme") || strings.Contains(body, "/review/approve") || !strings.Contains(body, "no build slug") {
		t.Fatalf("%s", body)
	}
	code, _, _ := postForm(t, ts, "/cards/"+c.ID+"/review/done?ws=acme", url.Values{}, origin(ts))
	if got, _ := svc.Store().Card(c.ID); code != http.StatusSeeOther || got.Lane != "done" {
		t.Fatalf("%d %s", code, got.Lane)
	}
}

func TestReviewMasksRepositoryAndPlanText(t *testing.T) {
	e, c, _ := reviewEnv(t)
	repo := filepath.Join(e.root, "apps", "billing")
	os.WriteFile(filepath.Join(repo, "b.txt"), []byte("b"), 0o644)
	git(t, repo, "add", "b.txt")
	git(t, repo, "commit", "-q", "-m", "fix: notify ops@example.com")
	e.gate.mu.Lock()
	rec := e.gate.states["demo"].Records
	rec.Plan.Summary = "Mail the export to finance@example.com."
	rec.Evidence[0].Results[0].Cmd = "send --to finance@example.com"
	e.gate.mu.Unlock()
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/cards/"+c.ID+"/review?ws=acme", nil)
	if strings.Contains(body, "@example.com") || !strings.Contains(body, "[REDACTED:email]") {
		t.Fatalf("unmasked review:\n%s", body)
	}
	if strings.Contains(body, "<script>") || strings.Contains(body, " onclick=") || strings.Contains(body, "<style") {
		t.Fatal("the served review page carries no inline script, handler or style")
	}
}

func TestReviewActionsRefuseAgents(t *testing.T) {
	e, c, _ := reviewEnv(t)
	ts := e.server(Options{})
	hdr := origin(ts)
	hdr[HeaderAgent] = "1"
	for _, path := range []string{"approve", "request-changes", "done"} {
		code, _, _ := postForm(t, ts, "/cards/"+c.ID+"/review/"+path+"?ws=acme", url.Values{"note": {"x"}}, hdr)
		if code != http.StatusForbidden {
			t.Fatalf("%s: %d", path, code)
		}
	}
	if len(e.gate.answers) != 0 {
		t.Fatalf("an agent answered: %v", e.gate.answers)
	}
	if _, pending := e.svcs["acme"].PendingResult(c.ID); !pending {
		t.Fatal("the result decision must stay pending after an agent's approve")
	}
	if code, _, _ := postForm(t, ts, "/cards/not-an-id/review/approve?ws=acme", url.Values{}, origin(ts)); code != http.StatusNotFound {
		t.Fatalf("bad id: %d", code)
	}
}

func TestExportReviewIsPrivateAndStandalone(t *testing.T) {
	e, c, r := reviewEnv(t)
	svc := e.svcs["acme"]
	path, err := ExportReview(context.Background(), svc, c, r)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(store.RunsDir("acme"), r.ID+".review.html") {
		t.Fatalf("path %s", path)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	raw, _ := os.ReadFile(path)
	html := string(raw)
	for _, bad := range []string{"<script", "<form", "/static/", "<link"} {
		if strings.Contains(html, bad) {
			t.Fatalf("export carries %q", bad)
		}
	}
	if !strings.HasPrefix(html, "<!doctype html>") || !strings.Contains(html, "<style>") || !strings.Contains(html, "run "+r.ID) {
		t.Fatalf("export:\n%s", html)
	}
}
