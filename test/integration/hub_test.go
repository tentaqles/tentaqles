package integration

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/cmd"
	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func tq(t *testing.T, args ...string) string {
	t.Helper()
	var out, errb bytes.Buffer
	root := cmd.NewRoot()
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("tq %s: %v\n%s%s", strings.Join(args, " "), err, out.String(), errb.String())
	}
	return out.String()
}

func unsetEnv(t *testing.T, k string) {
	t.Helper()
	if v, ok := os.LookupEnv(k); ok {
		os.Unsetenv(k)
		t.Cleanup(func() { os.Setenv(k, v) })
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=T", "-c", "commit.gpgsign=false"}, args...)...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func jsonLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	rest := s[i:]
	if j := strings.Index(rest, end); j >= 0 {
		return rest[:j]
	}
	return rest
}

// TestHubEndToEndWithFakeClaude: tq hub add → move to in progress → the run hands off into its
// worktree → a plan-approval waits → tq hub answer (hub token, checked by the fake gate) → the
// run resumes → a result waits → the review page shows gate evidence only → approve → done.
func TestHubEndToEndWithFakeClaude(t *testing.T) {
	py := testutil.Python(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	wd, _ := os.Getwd()
	repo := filepath.Dir(filepath.Dir(wd)) // test/integration → monorepo root
	fakeClaude := filepath.Join(repo, "internal", "hub", "adapter", "testdata", "fake_claude.py")
	fakeGate := filepath.Join(repo, "internal", "hub", "gate", "testdata", "fake_build_gate.py")

	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK", "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN", "__TQ_STATE", "TQ_WS"} {
		unsetEnv(t, k)
	}
	base := filepath.Join(home, "work")
	ws := filepath.Join(base, "acme")
	project := filepath.Join(ws, "apps", "billing")
	wt := filepath.Join(ws, "apps", "billing-build-demo")
	for _, d := range []string{project, wt} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &registry.Config{}
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	mp := filepath.Join(ws, manifest.FileName)
	os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: acme\nidentities: { gh: {} }\n"), 0o600)
	h, _ := trust.HashFile(mp)
	if err := trust.Allow(h); err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(home, ".tentaqles", "build-gate")
	os.MkdirAll(keyDir, 0o700)
	os.WriteFile(filepath.Join(keyDir, "hmac.key"), bytes.Repeat([]byte{7}, 32), 0o600)

	gitIn(t, wt, "init", "-q")
	os.WriteFile(filepath.Join(wt, "README.md"), []byte("billing\n"), 0o644)
	gitIn(t, wt, "add", "README.md")
	gitIn(t, wt, "commit", "-q", "-m", "chore: base")
	baseHash := gitIn(t, wt, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(wt, "export.py"), []byte("print('csv')\n"), 0o644)
	gitIn(t, wt, "add", "export.py")
	gitIn(t, wt, "commit", "-q", "-m", "feat(demo#1): export invoices as CSV")

	gateDir := t.TempDir()
	t.Setenv("FAKE_GATE_DIR", gateDir)
	argv := func(script string) string {
		b, _ := json.Marshal(append(append([]string(nil), py...), script))
		return string(b)
	}
	t.Setenv("TQ_HUB_GATE_CMD", argv(fakeGate))
	t.Setenv("TQ_HUB_CLAUDE_CMD", argv(fakeClaude))
	t.Chdir(ws)

	var card store.Card
	if err := json.Unmarshal([]byte(tq(t, "hub", "add", "Export invoices", "--kind", "feature", "--tag", "agent-ready",
		"--project", "apps/billing", "--body", "CSV per month.", "--json")), &card); err != nil {
		t.Fatal(err)
	}
	actor := gate.Actor()
	statePath := filepath.Join(gateDir, "demo.json")
	records := map[string]any{
		"plan": map[string]any{"summary": "Export invoices as CSV.", "plain_summary": "", "spec": "docs/specs/demo.md",
			"checkpoints": []any{map[string]any{"id": 1, "title": "Write CSV", "approved": true, "tests": []any{map[string]any{"file": "tests/test_export.py", "layer": "unit"}}}}},
		"evidence": []any{map[string]any{"checkpoint": 1, "ts": "2026-10-21T10:30:00+00:00", "tree": "abcdef0123456789", "ok": true, "current": true,
			"summary": "all verify commands passed", "results": []any{map[string]any{"cmd": "go test ./...", "exit": 0, "seconds": 2.0, "timed_out": false}}}},
		"reviews": []any{map[string]any{"checkpoint": 1, "reviewer": "build-critic", "result": "PASS", "tree": "abcdef0123456789", "ts": "2026-10-21T10:40:00+00:00",
			"human": false, "blockers": 0, "majors": 0, "minors": 0, "unjudged": 0}},
		"dod": map[string]any{"ok": true, "tree": "abcdef0123456789", "ts": "2026-10-21T10:50:00+00:00", "override": nil,
			"checks": []any{map[string]any{"id": "test", "ok": true, "detail": "exit 0: go test ./..."}}},
	}
	state := func(pending, decisions []any) map[string]any {
		return map[string]any{"slug": "demo", "feature": "Export invoices", "workflow": "feature", "approved": true, "rolled_back": false,
			"finished": false, "halted": nil, "task": map[string]any{"hub": card.ID, "workspace": "acme", "kind": "feature", "external": nil},
			"bases": map[string]any{"1": baseHash},
			"checkpoints": []any{map[string]any{"id": 1, "title": "Write CSV", "status": "passed", "gates": map[string]any{"behavior": "passed", "review": "passed"},
				"evidence": "fresh", "reviews": map[string]any{"build-critic": "PASS"}}},
			"dod": map[string]any{"ok": true, "tree": "abcdef0123456789", "override": nil}, "pending_decisions": pending, "decisions": decisions, "records": records}
	}
	planApproval := map[string]any{"id": "d0000001", "kind": "plan-approval", "checkpoint": nil, "reviewer": nil, "text": "1 checkpoint: write CSV. verify: go test ./...",
		"options": []any{"approve", "edit", "stop"}, "asked_at": "2026-10-21T10:00:00+00:00"}
	result := map[string]any{"id": "d0000002", "kind": "result", "checkpoint": nil, "reviewer": nil, "text": "Try: export March.",
		"options": []any{"approve", "request-changes", "stop"}, "asked_at": "2026-10-21T11:00:00+00:00"}
	approvedPlan := map[string]any{"id": "d0000001", "kind": "plan-approval", "status": "answered", "choice": "approve", "actor": actor,
		"options": []any{"approve", "edit", "stop"}, "answered_at": "2026-10-21T10:05:00+00:00"}
	argvLog := filepath.Join(t.TempDir(), "argv.jsonl")
	script, _ := json.Marshal(map[string]any{"counter": filepath.Join(t.TempDir(), "n"), "argv_log": argvLog, "rounds": []any{
		map[string]any{"result": "Worktree and hooks ready.\nTQ_HUB_RELOCATE " + filepath.ToSlash(wt) + " demo", "cost": 0.05, "session_id": "s-0"},
		map[string]any{"result": "Plan written; waiting for approval.", "cost": 0.5, "session_id": "s-1",
			"write_json": map[string]any{statePath: state([]any{planApproval}, []any{map[string]any{"id": "d0000001", "kind": "plan-approval", "status": "pending"}})}},
		map[string]any{"result": "All checkpoints passed. Everything is green, trust me.", "cost": 1.25, "session_id": "s-1",
			"write_json": map[string]any{statePath: state([]any{result}, []any{approvedPlan, map[string]any{"id": "d0000002", "kind": "result", "status": "pending"}})}},
	}})
	scriptPath := filepath.Join(t.TempDir(), "script.json")
	os.WriteFile(scriptPath, script, 0o600)
	t.Setenv("FAKE_CLAUDE_SCRIPT", scriptPath)

	if out := tq(t, "hub", "move", card.ID, "in progress"); !strings.Contains(out, "queued run r-") {
		t.Fatal(out)
	}
	tq(t, "hub", "serve", "--drain", "--no-notify") // round 1 hands off, round 2 waits on the plan

	var waiting []map[string]any
	json.Unmarshal([]byte(tq(t, "hub", "list", "--waiting", "--json")), &waiting)
	if len(waiting) != 1 || waiting[0]["decision"].(map[string]any)["id"] != "d0000001" {
		t.Fatalf("waiting %+v", waiting)
	}
	out := tq(t, "hub", "answer", card.ID, "d0000001", "--choice", "approve", "--note", "ok")
	if !strings.Contains(out, "decision d0000001: approve by "+actor) || !strings.Contains(out, "resumed run") {
		t.Fatal(out)
	}
	var gateState map[string]any
	raw, _ := os.ReadFile(statePath)
	json.Unmarshal(raw, &gateState)
	answered := gateState["decisions"].([]any)
	if last := answered[len(answered)-1].(map[string]any); last["actor"] != actor || last["choice"] != "approve" {
		t.Fatalf("the gate's own record must show the hub actor: %+v", last)
	}
	for _, c := range jsonLines(t, filepath.Join(gateDir, "calls.jsonl")) {
		argv := c["argv"].([]any)
		if argv[0] == "answer" && (c["has_token"] != true || c["actor"] != actor) {
			t.Fatalf("answer without a hub token: %v", c)
		}
		if argv[0] == "state" && c["has_token"] == true {
			t.Fatalf("a state read carried the hub token: %v", c)
		}
	}

	tq(t, "hub", "serve", "--drain", "--no-notify") // round 3 resumes and waits on the result
	st, _ := store.OpenExisting("acme", []string{"all"})
	got, _ := st.Card(card.ID)
	runs, _ := st.RunsForCard(card.ID)
	st.Close()
	if got.Lane != "review" || len(runs) != 1 || runs[0].Status != store.StatusWaiting || runs[0].Rounds != 3 || runs[0].Worktree == "" {
		t.Fatalf("card %+v runs %+v", got, runs)
	}

	html := tq(t, "hub", "review", card.ID, "--html")
	evidence := between(html, `<section id="evidence"`, "</section>")
	if !strings.Contains(evidence, "go test ./...") || !strings.Contains(evidence, "abcdef012345") {
		t.Fatalf("evidence:\n%s", evidence)
	}
	if strings.Contains(html, "trust me") {
		t.Fatal("agent text never reaches the review page; it is only a card comment")
	}
	if !strings.Contains(between(html, `<section id="done"`, "</section>"), "feat(demo#1): export invoices as CSV") {
		t.Fatal("commits since the base")
	}

	tq(t, "hub", "answer", card.ID, "d0000002", "--choice", "approve")
	st, _ = store.OpenExisting("acme", []string{"all"})
	got, _ = st.Card(card.ID)
	run, _ := st.Run(runs[0].ID)
	st.Close()
	if got.Lane != "done" || run.Status != store.StatusFinished || run.CostUSD < 1.79 || run.CostUSD > 1.81 {
		t.Fatalf("card %+v run %+v", got, run)
	}
	if _, err := os.Stat(filepath.Join(store.RunsDir("acme"), run.ID+".review.html")); err != nil {
		t.Fatal("the approved review is exported next to the run log")
	}

	calls := jsonLines(t, argvLog)
	if len(calls) != 3 {
		t.Fatalf("%d rounds", len(calls))
	}
	for i, c := range calls {
		want := wt
		if i == 0 {
			want = project
		}
		if !strings.EqualFold(filepath.Clean(c["cwd"].(string)), want) {
			t.Fatalf("round %d ran in %v, want %s", i, c["cwd"], want)
		}
		var task map[string]any
		json.Unmarshal([]byte(c["hub_task"].(string)), &task)
		if c["headless"] != "1" || c["headless_child"] != false || c["build_token"] != false || c["build_actor"] != false || task["hub"] != card.ID {
			t.Fatalf("round %d environment %v", i, c)
		}
	}
	last := calls[2]["argv"].([]any)
	joined := strings.Join(func() []string {
		var s []string
		for _, a := range last {
			s = append(s, a.(string))
		}
		return s
	}(), " ")
	// The prompt travels on stdin (nothing free-text on argv); the session id is an argv flag.
	prompt, _ := calls[2]["prompt"].(string)
	if !strings.Contains(prompt, "/tentaqles:build resume demo --headless") || !strings.Contains(joined, "--resume=s-1") {
		t.Fatalf("resume round argv %s prompt %q", joined, prompt)
	}
	if strings.Contains(joined, "/tentaqles:") {
		t.Fatalf("the prompt leaked onto argv: %s", joined)
	}
	acts, _ := os.ReadFile(store.ActivityPath("acme"))
	for _, verb := range []string{`"run.start"`, `"decision.ask"`, `"decision.answer"`, `"review.approve"`, `"run.end"`} {
		if !bytes.Contains(acts, []byte(verb)) {
			t.Fatalf("activity.jsonl lacks %s", verb)
		}
	}
}
