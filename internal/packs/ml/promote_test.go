package ml

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/approval"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"gopkg.in/yaml.v3"
)

const promoteConfig = `
ml:
  enabled: true
  paths: { train: ["src/train/**"] }
  data_manifest: data/snapshots.yaml
  models:
    - { name: churn, card: models/churn/MODEL_CARD.md, metrics: reports/churn/metrics.json }
  promotion:
    protected: [production, champion]
    command: 'FAKE record promoted.txt {model} {version} {target}'
  drift:
    reference: data/profiles/churn-train.json
    current: data/profiles/churn-current.json
    features: { plan: { warn: 0.5, fail: 1 } }
    metrics: { auc: { min: 0.78 } }
`

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=tester@acme.test", "-c", "user.name=tester", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// fakeChecks stands in for the lint workstream's repro checks.
func fakeChecks(t *testing.T, status packs.Status) {
	t.Helper()
	old := lookupCheck
	lookupCheck = func(id string) (packs.Check, bool) {
		return packs.Check{ID: id, Pack: "ml", Kind: packs.Builtin, Run: func(ctx context.Context, in *packs.Input) packs.Result {
			r := packs.Result{ID: id, Status: status, Detail: "fake"}
			if status == packs.Fail {
				r.Findings = []packs.Finding{{File: "src/train/fit.py", Line: 1, Level: packs.LevelError, Rule: "seed", Message: "no seed"}}
			}
			return r
		}}, true
	}
	t.Cleanup(func() { lookupCheck = old })
}

type promoteRepo struct {
	root string
	env  *promoteEnv
	cfg  string
}

func newPromoteRepo(t *testing.T) *promoteRepo {
	t.Helper()
	isolate(t)
	fakeChecks(t, packs.OK)
	root := testutil.TempDir(t)
	cfg := strings.Replace(promoteConfig, "FAKE", `"`+fakeCLI(t)+`"`, 1)
	writeFiles(t, root, map[string]string{
		"models/churn/MODEL_CARD.md":       readFixture(t, "promote-card/good/MODEL_CARD.md"),
		"data/snapshots.yaml":              readFixture(t, "promote-card/snapshots.yaml"),
		"data/profiles/churn-train.json":   readFixture(t, "drift-basic/reference.json"),
		"data/profiles/churn-current.json": readFixture(t, "drift-basic/current.json"),
		"reports/churn/metrics.json":       readFixture(t, "drift-basic/metrics.json"),
		"src/train/fit.py":                 "print('fit')\n",
	})
	git(t, root, "init", "-q")
	git(t, root, "add", ".")
	git(t, root, "commit", "-q", "-m", "init")
	r := &promoteRepo{root: root, cfg: cfg}
	r.env = r.newEnv(t)
	return r
}

func (r *promoteRepo) newEnv(t *testing.T) *promoteEnv {
	t.Helper()
	var packsBlock map[string]manifest.PackConfig
	if err := yaml.Unmarshal([]byte(r.cfg), &packsBlock); err != nil {
		t.Fatal(err)
	}
	m := &manifest.Manifest{Packs: packsBlock}
	env, err := newPromoteEnv(r.root, &resolve.Workspace{Name: "acme", Root: r.root, Manifest: m})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func answer(t *testing.T, id, choice, actor string) {
	t.Helper()
	st, err := store.Open("acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := answerErr(st, id, choice, actor); err != nil {
		t.Fatal(err)
	}
}

// answerErr answers a pack approval the only way one is answered: the
// terminal path's signed store.AnswerPackApproval.
func answerErr(st *store.Store, id, choice, actor string) error {
	key, err := ledger.SigningKey("acme")
	if err != nil {
		return err
	}
	_, err = st.AnswerPackApproval(id, choice, "ok", actor, store.PackSigner(ledger.AnswerSigner("acme", key)))
	return err
}

// typed is a confirm that reads what the human typed (approval.Confirm on
// a terminal, without the terminal).
func typed(r io.Reader, out io.Writer) func(string) error {
	return func(cmd string) error {
		if err := packs.HumanOnly(); err != nil {
			return err
		}
		fmt.Fprintf(out, "tq will run:\n\n  %s\n\nType yes to run it: ", cmd)
		b := make([]byte, 64)
		n, _ := r.Read(b)
		if strings.TrimSpace(string(b[:n])) != "yes" {
			return approval.ErrNotConfirmed
		}
		return nil
	}
}

var promoteWant = func(res RequestResult) approval.Binding {
	return approval.Binding{Pack: "ml", Action: "promote", Subject: res.Model + "@" + res.Version, Target: res.Target}
}

func (r *promoteRepo) request(t *testing.T, target string) RequestResult {
	t.Helper()
	res, err := promoteRequest(context.Background(), r.env, "churn", "7", target)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (r *promoteRepo) apply(id, text string) (string, error) {
	var out bytes.Buffer
	err := promoteApply(context.Background(), newPromoteApplyCmd(), r.env, id, typed(strings.NewReader(text), &out), &out)
	return out.String(), err
}

func TestRequestNotProtected(t *testing.T) {
	r := newPromoteRepo(t)
	res := r.request(t, "staging")
	if res.Protected || res.Decision != "" {
		t.Fatalf("%+v", res)
	}
}

func TestRequestValidatesNames(t *testing.T) {
	r := newPromoteRepo(t)
	for _, c := range [][3]string{{"nope", "7", "production"}, {"churn", "7;rm", "production"}, {"churn", "-x", "production"}, {"churn", "7", "prod uction"}} {
		if _, err := promoteRequest(context.Background(), r.env, c[0], c[1], c[2]); err == nil {
			t.Fatalf("accepted %v", c)
		}
	}
}

func TestRequestRefusesDirtyTree(t *testing.T) {
	r := newPromoteRepo(t)
	writeFiles(t, r.root, map[string]string{"src/train/fit.py": "print('changed')\n"})
	_, err := promoteRequest(context.Background(), r.env, "churn", "7", "production")
	if err == nil || !strings.Contains(err.Error(), "clean") {
		t.Fatalf("dirty tree: %v", err)
	}
}

func TestRequestRefusesFailingRequirements(t *testing.T) {
	r := newPromoteRepo(t)
	fakeChecks(t, packs.Fail)
	writeFiles(t, r.root, map[string]string{"models/churn/MODEL_CARD.md": readFixture(t, "promote-card/empty-metrics/MODEL_CARD.md")})
	git(t, r.root, "commit", "-q", "-am", "bad card")
	_, err := promoteRequest(context.Background(), r.env, "churn", "7", "production")
	if err == nil {
		t.Fatal("refused requirements must fail the request")
	}
	for _, want := range []string{"model_card", "card-metrics-empty", "repro", "ml/seeds"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("missing %q in %v", want, err)
		}
	}
	if es, _ := ledger.Read("acme", "ml"); len(es) != 0 {
		t.Fatal("nothing is asked when a requirement fails")
	}
}

func TestRequestRefusesMissingReproCheck(t *testing.T) {
	r := newPromoteRepo(t)
	lookupCheck = func(string) (packs.Check, bool) { return packs.Check{}, false }
	_, err := promoteRequest(context.Background(), r.env, "churn", "7", "production")
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("missing check must fail closed: %v", err)
	}
}

// A tq-packs.yaml that drops drift and repro from require, while the
// manifest leaves require out: every item still runs, the evidence still
// binds drift, and the approver sees which items ran and what was clamped.
func TestRequestRepoLayerCannotDropRequirements(t *testing.T) {
	r := newPromoteRepo(t)
	writeFiles(t, r.root, map[string]string{"tq-packs.yaml": "ml:\n  promotion: { require: [model_card], valid_for: 30d }\n"})
	git(t, r.root, "add", "tq-packs.yaml")
	git(t, r.root, "commit", "-q", "-m", "loosen")
	r.env = r.newEnv(t)
	// valid_for 30d is refused by the merge (over the 7d Default); require is clamped by load.
	if strings.Join(r.env.St.Promotion.Require, ",") != "model_card,drift,repro" || len(r.env.St.Clamped) != 1 || r.env.St.ValidFor() > 7*24*time.Hour {
		t.Fatalf("%v %q", r.env.St.Promotion.Require, r.env.St.Clamped)
	}
	res := r.request(t, "production")
	if res.Evidence["drift"] == "" {
		t.Fatalf("drift is still bound: %v", res.Evidence)
	}
	if strings.Join(res.Require, ",") != "model_card,drift,repro" || len(res.Clamped) != 1 {
		t.Fatalf("%+v", res)
	}
	m, _ := r.env.St.Model("churn")
	sum := requestSummary(r.env, m, "7", "production", res.Drift)
	for _, want := range []string{"Checks run: model_card, drift, repro", "Clamped from tq-packs.yaml", "promotion.require"} {
		if !strings.Contains(sum, want) {
			t.Fatalf("summary misses %q:\n%s", want, sum)
		}
	}
	es, err := ledger.Read("acme", "ml")
	if err != nil || len(es) == 0 {
		t.Fatalf("asked: %v", err)
	}
}

// The promotion command gets the workspace's identity env: another
// client's provider variable left in the parent env is blanked, and the
// workspace's own vars win (review r0 finding 5).
func TestIdentityEnvIsolates(t *testing.T) {
	isolate(t)
	vars := envplan.AllProviderVars()
	if len(vars) == 0 {
		t.Skip("no provider vars in the catalog")
	}
	other := vars[0]
	t.Setenv(other, "other-client-config")
	ws := &resolve.Workspace{Name: "acme", Root: t.TempDir(), Manifest: &manifest.Manifest{}}
	env := append(os.Environ(), identityEnv(ws)...) // toolrun's order: parent, then Spec.Env
	last := map[string]string{}
	for _, kv := range env {
		if i := strings.Index(kv, "="); i > 0 {
			last[strings.ToUpper(kv[:i])] = kv[i+1:]
		}
	}
	if v := last[strings.ToUpper(other)]; v != "" {
		t.Fatalf("%s keeps another client's value %q", other, v)
	}
	if last["TQ_WS"] != "acme" {
		t.Fatalf("the workspace's vars apply: %q", last["TQ_WS"])
	}
}

// cancelReader cancels the request context when the human answers, as a
// long wait at the yes prompt does with the request-wide timeout.
type cancelReader struct {
	r      io.Reader
	cancel context.CancelFunc
}

func (c cancelReader) Read(p []byte) (int, error) {
	c.cancel()
	return c.r.Read(p)
}

// The 300 s run starts after the typed yes, not inside the request-wide
// context (review r0 finding 7).
func TestApplyRunsAfterSlowConfirm(t *testing.T) {
	r := newPromoteRepo(t)
	res := r.request(t, "production")
	answer(t, res.Decision, "approve", "hub:tester")
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	err := promoteApply(ctx, newPromoteApplyCmd(), r.env, res.Decision, typed(cancelReader{strings.NewReader("yes\n"), cancel}, &out), &out)
	if err != nil {
		t.Fatalf("a slow yes must not fail the run: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "promoted churn@7 to production") {
		t.Fatal(out.String())
	}
}

func TestPromoteEndToEnd(t *testing.T) {
	r := newPromoteRepo(t)
	res := r.request(t, "production")
	if !res.Protected || res.Decision == "" || res.Card == "" {
		t.Fatalf("%+v", res)
	}
	for _, k := range []string{"model_card", "metrics", "drift", "checks", "git_head", "clean_tree", "command"} {
		if res.Evidence[k] == "" {
			t.Fatalf("evidence %s missing: %v", k, res.Evidence)
		}
	}
	if _, err := os.Stat(filepath.Join(r.root, ".tq", "drift", "churn-result.json")); err != nil {
		t.Fatal("the drift result is written")
	}
	// Pending: apply refuses.
	if _, err := r.apply(res.Decision, "yes\n"); !errors.Is(err, approval.ErrPending) {
		t.Fatalf("pending: %v", err)
	}
	answer(t, res.Decision, "approve", "hub:tester")
	// Not confirmed: nothing runs.
	if _, err := r.apply(res.Decision, "no\n"); !errors.Is(err, approval.ErrNotConfirmed) {
		t.Fatalf("not confirmed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.root, "promoted.txt")); err == nil {
		t.Fatal("the command ran without a yes")
	}
	out, err := r.apply(res.Decision, "yes\n")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "record promoted.txt churn 7 production") {
		t.Fatalf("the exact command is shown: %s", out)
	}
	argv, _ := os.ReadFile(filepath.Join(r.root, "promoted.txt"))
	if string(argv) != "churn\n7\nproduction" {
		t.Fatalf("argv %q", argv)
	}
	card, _ := os.ReadFile(filepath.Join(r.root, "models", "churn", "MODEL_CARD.md"))
	if !strings.Contains(string(card), "| 7 | production | "+res.Decision+" | human |") {
		t.Fatalf("history row missing:\n%s", card)
	}
	// Single use.
	if _, err := r.apply(res.Decision, "yes\n"); !errors.Is(err, approval.ErrUsed) {
		t.Fatalf("reuse: %v", err)
	}
	rows, err := promoteStatus(r.env, "churn")
	if err != nil || len(rows) != 1 || rows[0].State != "used" {
		t.Fatalf("%v %+v", err, rows)
	}
	if rows, _ := promoteStatus(r.env, "other"); len(rows) != 0 {
		t.Fatalf("filtered by model: %+v", rows)
	}
}

func TestApplyRefusesAgentSession(t *testing.T) {
	r := newPromoteRepo(t)
	res := r.request(t, "production")
	answer(t, res.Decision, "approve", "hub:tester")
	for _, k := range []string{"CLAUDECODE", "TENTAQLES_HEADLESS"} {
		t.Setenv(k, "1")
		if _, err := r.apply(res.Decision, "yes\n"); !errors.Is(err, packs.ErrHumanOnly) {
			t.Fatalf("%s: %v", k, err)
		}
		t.Setenv(k, "")
	}
}

func TestApplyRefusesAgentAnswer(t *testing.T) {
	r := newPromoteRepo(t)
	res := r.request(t, "production")
	st, err := store.Open("acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	aerr := answerErr(st, res.Decision, "approve", "agent:builder")
	st.Close()
	if _, err := r.apply(res.Decision, "yes\n"); !errors.Is(err, approval.ErrAgent) && !(aerr != nil && errors.Is(err, approval.ErrPending)) {
		t.Fatalf("agent answer (%v): %v", aerr, err)
	}
}

func TestApplyRefusesRejected(t *testing.T) {
	r := newPromoteRepo(t)
	res := r.request(t, "production")
	answer(t, res.Decision, "reject", "hub:tester")
	if _, err := r.apply(res.Decision, "yes\n"); !errors.Is(err, approval.ErrRejected) {
		t.Fatalf("rejected: %v", err)
	}
}

func TestApplyRefusesStaleEvidence(t *testing.T) {
	r := newPromoteRepo(t)
	res := r.request(t, "production")
	answer(t, res.Decision, "approve", "hub:tester")
	// A new commit changes HEAD (and the metrics file).
	writeFiles(t, r.root, map[string]string{"reports/churn/metrics.json": `{"auc": 0.95}`})
	git(t, r.root, "commit", "-q", "-am", "better numbers")
	_, err := r.apply(res.Decision, "yes\n")
	if !errors.Is(err, approval.ErrEvidence) || !strings.Contains(err.Error(), "metrics") || !strings.Contains(err.Error(), "git_head") {
		t.Fatalf("stale: %v", err)
	}
	// A dirty tree refuses too.
	git(t, r.root, "reset", "-q", "--hard", "HEAD~1")
	writeFiles(t, r.root, map[string]string{"src/train/fit.py": "print('dirty')\n"})
	if _, err := r.apply(res.Decision, "yes\n"); !errors.Is(err, approval.ErrEvidence) || !strings.Contains(err.Error(), "clean_tree") {
		t.Fatalf("dirty: %v", err)
	}
}

func TestApplyRefusesChangedCommand(t *testing.T) {
	r := newPromoteRepo(t)
	res := r.request(t, "production")
	answer(t, res.Decision, "approve", "hub:tester")
	r.cfg = strings.Replace(r.cfg, "{target}", "{target} --force", 1)
	r.env = r.newEnv(t)
	if _, err := r.apply(res.Decision, "yes\n"); !errors.Is(err, approval.ErrEvidence) || !strings.Contains(err.Error(), "command") {
		t.Fatalf("command changed: %v", err)
	}
}

func TestApplyCommandFailureKeepsApproval(t *testing.T) {
	r := newPromoteRepo(t)
	res := r.request(t, "production")
	answer(t, res.Decision, "approve", "hub:tester")
	t.Setenv("TQ_ML_FAKE_EXIT", "1")
	if _, err := r.apply(res.Decision, "yes\n"); err == nil || !strings.Contains(err.Error(), "exited 1") || !strings.Contains(err.Error(), "used") {
		t.Fatalf("failure: %v", err)
	}
	if _, err := approval.Verify("acme", res.Decision, promoteWant(res), res.Evidence); !errors.Is(err, approval.ErrUsed) {
		t.Fatalf("a claimed approval is used even when the run fails: %v", err)
	}
}

// The typed yes comes before the claim: a "no" leaves the approval usable.
func TestApplyDeclineKeepsApproval(t *testing.T) {
	r := newPromoteRepo(t)
	res := r.request(t, "production")
	answer(t, res.Decision, "approve", "hub:tester")
	if _, err := r.apply(res.Decision, "no\n"); !errors.Is(err, approval.ErrNotConfirmed) {
		t.Fatalf("declined: %v", err)
	}
	if _, err := approval.Verify("acme", res.Decision, promoteWant(res), res.Evidence); err != nil {
		t.Fatalf("a declined apply does not claim: %v", err)
	}
}

func TestApplyNeedsTTY(t *testing.T) {
	r := newPromoteRepo(t)
	res := r.request(t, "production")
	answer(t, res.Decision, "approve", "hub:tester")
	var out bytes.Buffer
	c := newPromoteApplyCmd()
	confirm := func(cmd string) error { return approval.Confirm(c, nil, &out, cmd) } // not a terminal
	if err := promoteApply(context.Background(), c, r.env, res.Decision, confirm, &out); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("no tty: %v", err)
	}
}

func TestFillCommand(t *testing.T) {
	argv, err := FillCommand(`uv run python "scripts/promote.py" {model} v{version} {target}`, map[string]string{"model": "churn", "version": "7", "target": "production"})
	if err != nil || strings.Join(argv, "|") != "uv|run|python|scripts/promote.py|churn|v7|production" {
		t.Fatalf("%v %q", err, argv)
	}
	for _, bad := range []map[string]string{{"model": "a b"}, {"model": "$(x)"}, {"model": "-rf"}, {"model": "a'b"}} {
		if _, err := FillCommand("x {model}", bad); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	for _, bad := range []string{"a | b", "a && b", "a; b", "echo $HOME", "a `b`", "'open", "a > f"} {
		if _, err := SplitCommand(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if got := QuoteArgv([]string{"uv", "a b", "it's"}); got != `uv 'a b' 'it'\''s'` {
		t.Fatal(got)
	}
}

// Claim refuses a command that does not declare itself an executor.
func TestApplyNeedsTheExecutorAnnotation(t *testing.T) {
	r := newPromoteRepo(t)
	res := r.request(t, "production")
	answer(t, res.Decision, "approve", "hub:tester")
	var out bytes.Buffer
	plain := newPromoteStatusCmd() // no tq.human-only: execute
	if err := promoteApply(context.Background(), plain, r.env, res.Decision, typed(strings.NewReader("yes\n"), &out), &out); !errors.Is(err, approval.ErrNotExecutor) {
		t.Fatalf("a non-executor claimed: %v", err)
	}
	if _, err := approval.Verify("acme", res.Decision, promoteWant(res), res.Evidence); err != nil {
		t.Fatalf("the refused claim leaves the approval usable: %v", err)
	}
	if a := newPromoteApplyCmd().Annotations[packs.AnnotationHumanOnly]; a != packs.HumanOnlyExecute {
		t.Fatalf("apply annotation %q", a)
	}
	if a := newPromoteRequestCmd().Annotations[packs.AnnotationHumanOnly]; a != packs.ApprovalAsk {
		t.Fatalf("request annotation %q", a)
	}
}
