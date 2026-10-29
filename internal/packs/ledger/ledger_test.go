package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func isolate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestAppendResolve(t *testing.T) {
	isolate(t)
	exp := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(Append("acme", Entry{Event: Asked, Decision: "d1", Pack: "ml", Action: "promote", Subject: "churn@7", Target: "production",
		Evidence: map[string]string{"card": "sha256:a"}, ExpiresAt: exp, Grant: "approve"}))
	must(RecordAnswer("acme", "d1", map[string]any{"pack": "ml", "action": "promote", "evidence": map[string]any{"card": "masked"}}, "approve", "hub", time.Now()))
	must(RecordAnswer("acme", "d2", map[string]any{}, "approve", "hub", time.Now())) // not a pack decision: ignored
	es, err := Read("acme", "ml")
	must(err)
	if len(es) != 2 {
		t.Fatalf("%d lines", len(es))
	}
	s := Resolve(es)["d1"]
	if s.Status != Answered || !s.Granted() || s.Evidence["card"] != "sha256:a" || s.Used() || s.Expired(time.Now()) {
		t.Fatalf("an answered line never changes the binding: %+v", s)
	}
	must(Append("acme", Entry{Event: Consumed, Decision: "d1", Pack: "ml"}))
	must(Append("acme", Entry{Event: Answered, Decision: "d1", Pack: "ml", Choice: "approve", ActorKind: "hub"}))
	s = Resolve(mustRead(t))["d1"]
	if !s.Used() || s.Status != Consumed {
		t.Fatalf("consumed stays consumed: %+v", s)
	}
	p, _ := Path("acme", "ml")
	fi, _ := os.Stat(p)
	if fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("mode %v", fi.Mode())
	}
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), "note") {
		t.Fatal("no note text in the ledger")
	}
	if _, err := Path("../x", "ml"); err == nil {
		t.Fatal("bad workspace accepted")
	}
}

func mustRead(t *testing.T) []Entry {
	es, err := ReadAll("acme")
	if err != nil {
		t.Fatal(err)
	}
	return es
}

func TestAgentAnswerIsNotGranted(t *testing.T) {
	s := Resolve([]Entry{{Event: Asked, Decision: "d", Pack: "ml", Grant: "approve", TS: time.Now().UTC().Format(time.RFC3339)},
		{Event: Answered, Decision: "d", Pack: "ml", Choice: "approve", ActorKind: "agent"}})["d"]
	if s.Granted() {
		t.Fatal("agent answer granted")
	}
}
