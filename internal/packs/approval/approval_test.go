package approval

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
)

func isolate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"TQ_HUB_TASK", "TENTAQLES_HEADLESS", "CLAUDECODE", "CLAUDE_PLUGIN_ROOT"} {
		t.Setenv(k, "")
	}
}

var ev = map[string]string{"model_card": "sha256:aa", "git_head": "sha256:bb"}

var want = Binding{Pack: "ml", Action: "promote", Subject: "churn@7", Target: "production"}

func ask(t *testing.T) (string, string) {
	t.Helper()
	id, card, err := Ask("acme", Request{Pack: "ml", Action: "promote", Subject: "churn@7", Target: "production", Evidence: ev,
		Summary: "promote churn v7; contact tester@acme.test"})
	if err != nil {
		t.Fatal(err)
	}
	return id, card
}

func answer(t *testing.T, id, choice, actor string) error {
	t.Helper()
	st, err := store.Open("acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_, err = st.AnswerHub(id, choice, "looks fine", actor)
	return err
}

func TestAskAnswerVerifyClaim(t *testing.T) {
	isolate(t)
	id, card := ask(t)
	st, _ := store.Open("acme", nil)
	d, err := st.Decision(id)
	c, _ := st.Card(card)
	st.Close()
	if err != nil || d.Kind != store.KindPermission || d.PackApproval() != "ml" || d.Options[0] != "approve" {
		t.Fatalf("decision: %v %+v", err, d)
	}
	if strings.Contains(d.Text, "tester@acme.test") {
		t.Fatal("summary not redacted")
	}
	if c.Kind != "chore" || !c.HasTag("ml-approval") {
		t.Fatalf("card: %+v", c)
	}
	if _, err := Verify("acme", id, want, ev); !errors.Is(err, ErrPending) {
		t.Fatalf("pending: %v", err)
	}
	if err := answer(t, id, "approve", "hub:tester"); err != nil {
		t.Fatal(err)
	}
	a, err := Verify("acme", id, want, ev)
	if err != nil || a.Choice != "approve" || a.Subject != "churn@7" {
		t.Fatalf("verify: %v %+v", err, a)
	}
	changed := map[string]string{"model_card": "sha256:cc", "git_head": "sha256:bb"}
	if _, err := Verify("acme", id, want, changed); !errors.Is(err, ErrEvidence) {
		t.Fatalf("evidence: %v", err)
	}
	es, _ := ledger.ReadAll("acme")
	if _, err := Find(es, Binding{Pack: "ml", Action: "promote", Target: "production"}, ev, Now(), nil); err != nil {
		t.Fatalf("find without subject: %v", err)
	}
	if _, err := Claim("acme", id, want, ev); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify("acme", id, want, ev); !errors.Is(err, ErrUsed) {
		t.Fatalf("a claim is a use: %v", err)
	}
	if err := MarkUsed("acme", "ml", id); err != nil {
		t.Fatal(err)
	}
	rows, _ := List("acme", "ml")
	if len(rows) != 1 || rows[0].State != "used" || len(rows[0].Evidence) != 2 {
		t.Fatalf("list: %+v", rows)
	}
}

// M5: every field of the binding is compared, and only that pack's ledger
// is read.
func TestVerifyBinding(t *testing.T) {
	isolate(t)
	id, _ := ask(t)
	answer(t, id, "approve", "hub:tester")
	for name, b := range map[string]Binding{
		"action":  {Pack: "ml", Action: "rollback", Subject: "churn@7", Target: "production"},
		"subject": {Pack: "ml", Action: "promote", Subject: "churn@8", Target: "production"},
		"target":  {Pack: "ml", Action: "promote", Subject: "churn@7", Target: "staging"},
	} {
		if _, err := Verify("acme", id, b, ev); !errors.Is(err, ErrBinding) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := Verify("acme", id, Binding{Pack: "cloud", Action: "promote", Subject: "churn@7", Target: "production"}, ev); !errors.Is(err, ErrUnknown) {
		t.Fatalf("another pack's ledger is not read: %v", err)
	}
	if _, err := Verify("acme", id, Binding{Pack: "ml", Action: "promote", Target: "production"}, ev); err == nil {
		t.Fatal("Verify needs the subject")
	}
	es, _ := ledger.Read("acme", "ml")
	if _, err := Find(es, Binding{Pack: "ml", Action: "promote", Subject: "other", Target: "production"}, ev, Now(), nil); !errors.Is(err, ErrUnknown) {
		t.Fatalf("Find compares the subject when given: %v", err)
	}
}

// M3: only human actor kinds grant (an allow-list); anything else is an agent.
func TestRejectedAgentAndExpired(t *testing.T) {
	isolate(t)
	id, _ := ask(t)
	answer(t, id, "reject", "terminal")
	if _, err := Verify("acme", id, want, ev); !errors.Is(err, ErrRejected) {
		t.Fatalf("rejected: %v", err)
	}
	for _, actor := range []string{"agent", "tester", "other", "hub:"} {
		id2, _ := ask(t)
		answer(t, id2, "approve", actor)
		if _, err := Verify("acme", id2, want, ev); !errors.Is(err, ErrAgent) {
			t.Fatalf("actor %q must count as an agent: %v", actor, err)
		}
	}
	id3, _ := ask(t)
	answer(t, id3, "approve", "terminal")
	prev := Now
	Now = func() time.Time { return prev().Add(8 * 24 * time.Hour) }
	defer func() { Now = prev }()
	if _, err := Verify("acme", id3, want, ev); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired: %v", err)
	}
}

// M3: a mirrored gate record never answers a hub-owned pack approval.
func TestMirrorCannotAnswerAPackApproval(t *testing.T) {
	isolate(t)
	id, _ := ask(t)
	st, _ := store.Open("acme", nil)
	_, err := st.UpsertDecision(store.Decision{ID: id, Status: store.DecisionAnswered, Choice: "approve", Actor: "hub:tester"})
	d, _ := st.Decision(id)
	st.Close()
	if err != nil || d.Status != store.DecisionPending {
		t.Fatalf("mirror answered the approval: %v %+v", err, d)
	}
	if _, err := Verify("acme", id, want, ev); !errors.Is(err, ErrPending) {
		t.Fatalf("ledger: %v", err)
	}
}

// M2: exactly one of many concurrent claimers wins.
func TestConcurrentClaims(t *testing.T) {
	isolate(t)
	id, _ := ask(t)
	answer(t, id, "approve", "hub:tester")
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Claim("acme", id, want, ev); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			} else if !errors.Is(err, ErrUsed) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d claimers won", wins)
	}
}

func TestLedgerFailureFailsTheAnswer(t *testing.T) {
	isolate(t)
	id, _ := ask(t)
	p, _ := ledger.Path("acme", "ml")
	os.Remove(p)
	if err := os.MkdirAll(p, 0o700); err != nil { // a directory where the file must be
		t.Fatal(err)
	}
	if err := answer(t, id, "approve", "hub:tester"); err == nil || !strings.Contains(err.Error(), "ledger") {
		t.Fatalf("answer must fail: %v", err)
	}
	st, _ := store.Open("acme", nil)
	d, _ := st.Decision(id)
	st.Close()
	if d.Status != store.DecisionPending {
		t.Fatalf("the answer was recorded: %+v", d)
	}
}

func TestValidation(t *testing.T) {
	isolate(t)
	for name, r := range map[string]Request{
		"no evidence":  {Pack: "ml", Action: "promote", Subject: "m", Target: "p"},
		"over the cap": {Pack: "cloud", Action: "apply", Subject: "s", Target: "prod", Evidence: ev, ValidFor: 25 * time.Hour},
		"bad subject":  {Pack: "ml", Action: "promote", Subject: "a b", Target: "p", Evidence: ev},
		"one option":   {Pack: "ml", Action: "promote", Subject: "m", Target: "p", Evidence: ev, Options: []string{"approve"}},
		"unknown pack": {Pack: "web", Action: "x", Subject: "m", Target: "p", Evidence: ev},
	} {
		if _, _, err := Ask("acme", r); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func guardCtx(pack string) packs.GuardCtx {
	m := &manifest.Manifest{Packs: map[string]manifest.PackConfig{}}
	return packs.NewGuardCtx(packs.GuardEnv{Workspace: "acme", Manifest: m}, pack)
}

func TestGuardReadsLedgerAndClaims(t *testing.T) {
	isolate(t)
	id, _ := ask(t)
	answer(t, id, "approve", "hub:tester")
	// The hub store is gone: the guard resolves the state from the ledger alone.
	os.RemoveAll(store.Dir("acme"))
	g := guardCtx("ml")
	a, err := FindGuard(g, want, ev)
	if err != nil || a.Decision != id {
		t.Fatalf("%v %+v", err, a)
	}
	if err := ledger.Claim("acme", "ml", id); err != nil {
		t.Fatal(err)
	}
	if _, err := FindGuard(g, want, ev); !errors.Is(err, ErrUsed) {
		t.Fatalf("a claimed approval is used for the guard: %v", err)
	}
	if _, err := FindGuard(guardCtx("cloud"), Binding{Pack: "cloud", Action: "apply", Target: "prod"}, ev); !errors.Is(err, ErrUnknown) {
		t.Fatalf("no ledger: %v", err)
	}
	if _, err := FindGuard(g, Binding{Pack: "cloud", Action: "apply", Target: "prod"}, ev); err == nil {
		t.Fatal("a guard reads only its own pack")
	}
	// A corrupt ledger fails closed.
	p, _ := ledger.Path("acme", "ml")
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("{not json\n")
	f.Close()
	if _, err := FindGuard(g, want, ev); !errors.Is(err, ledger.ErrCorrupt) {
		t.Fatalf("corrupt: %v", err)
	}
}

// m8: Confirm detects the terminal itself and refuses agents.
func TestConfirm(t *testing.T) {
	isolate(t)
	in, err := os.CreateTemp(t.TempDir(), "in")
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	in.WriteString("yes\n")
	in.Seek(0, 0)
	var out bytes.Buffer
	if err := Confirm(in, &out, "python promote.py"); err == nil {
		t.Fatal("a file is not a terminal")
	}
	prev := isTerminal
	isTerminal = func(*os.File) bool { return true }
	defer func() { isTerminal = prev }()
	in.Seek(0, 0)
	if err := Confirm(in, &out, "python promote.py"); err != nil || !strings.Contains(out.String(), "python promote.py") {
		t.Fatal(err)
	}
	t.Setenv("CLAUDECODE", "1")
	in.Seek(0, 0)
	if err := Confirm(in, &out, "x"); !errors.Is(err, packs.ErrHumanOnly) {
		t.Fatalf("agent: %v", err)
	}
}
