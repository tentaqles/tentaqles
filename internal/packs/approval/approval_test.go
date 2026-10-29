package approval

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
)

func isolate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("TQ_HUB_TASK", "")
}

var ev = map[string]string{"model_card": "sha256:aa", "git_head": "sha256:bb"}

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

func TestAskAnswerVerifyUse(t *testing.T) {
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
	if _, err := Verify("acme", id, ev); !errors.Is(err, ErrPending) {
		t.Fatalf("pending: %v", err)
	}
	if err := answer(t, id, "approve", "hub:tester"); err != nil {
		t.Fatal(err)
	}
	a, err := Verify("acme", id, ev)
	if err != nil || a.Choice != "approve" || a.Subject != "churn@7" {
		t.Fatalf("verify: %v %+v", err, a)
	}
	changed := map[string]string{"model_card": "sha256:cc", "git_head": "sha256:bb"}
	if _, err := Verify("acme", id, changed); !errors.Is(err, ErrEvidence) {
		t.Fatalf("evidence: %v", err)
	}
	es, _ := ledger.ReadAll("acme")
	if _, err := Find(es, "promote", "production", ev, Now()); err != nil {
		t.Fatalf("find: %v", err)
	}
	if err := MarkUsed("acme", id); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify("acme", id, ev); !errors.Is(err, ErrUsed) {
		t.Fatalf("single use: %v", err)
	}
	rows, _ := List("acme", "ml")
	if len(rows) != 1 || rows[0].State != "used" || len(rows[0].Evidence) != 2 {
		t.Fatalf("list: %+v", rows)
	}
}

func TestRejectedAgentAndExpired(t *testing.T) {
	isolate(t)
	id, _ := ask(t)
	answer(t, id, "reject", "terminal")
	if _, err := Verify("acme", id, ev); !errors.Is(err, ErrRejected) {
		t.Fatalf("rejected: %v", err)
	}
	id2, _ := ask(t)
	answer(t, id2, "approve", "agent")
	if _, err := Verify("acme", id2, ev); !errors.Is(err, ErrAgent) {
		t.Fatalf("agent: %v", err)
	}
	id3, _ := ask(t)
	answer(t, id3, "approve", "terminal")
	prev := Now
	Now = func() time.Time { return prev().Add(8 * 24 * time.Hour) }
	defer func() { Now = prev }()
	if _, err := Verify("acme", id3, ev); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired: %v", err)
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

func TestGuardReadsLedgerOnly(t *testing.T) {
	isolate(t)
	id, _ := ask(t)
	answer(t, id, "approve", "hub:tester")
	// The hub store is gone: the guard resolves the state from the ledger alone.
	os.RemoveAll(store.Dir("acme"))
	g := packs.NewGuardCtx(packs.GuardEnv{Workspace: "acme"}, "ml")
	a, err := FindGuard(g, "promote", "production", ev)
	if err != nil || a.Decision != id {
		t.Fatalf("%v %+v", err, a)
	}
	if _, err := FindGuard(packs.NewGuardCtx(packs.GuardEnv{Workspace: "acme"}, "cloud"), "apply", "prod", ev); !errors.Is(err, ErrUnknown) {
		t.Fatalf("no ledger: %v", err)
	}
}

func TestConfirm(t *testing.T) {
	var out bytes.Buffer
	if err := Confirm(strings.NewReader("yes\n"), &out, true, "python promote.py"); err != nil || !strings.Contains(out.String(), "python promote.py") {
		t.Fatal(err)
	}
	if err := Confirm(strings.NewReader("y\n"), &out, true, "x"); !errors.Is(err, ErrNotConfirmed) {
		t.Fatal("y is not yes")
	}
	if err := Confirm(strings.NewReader("yes\n"), &out, false, "x"); err == nil {
		t.Fatal("no TTY must refuse")
	}
}
