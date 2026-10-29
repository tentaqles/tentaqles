package ledger

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	must(Append("acme", Entry{Event: Answered, Decision: "d1", Pack: "ml", Action: "promote", Choice: "approve", ActorKind: "hub"}))
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

// m1: a malformed or oversized line is ErrCorrupt, never silently skipped.
func TestParseFailsClosed(t *testing.T) {
	good := `{"event":"asked","decision":"d1","pack":"ml","ts":"2026-10-28T00:00:00Z"}`
	if es, err := Parse([]byte(good + "\n\n" + good + "\n")); err != nil || len(es) != 2 {
		t.Fatalf("%v %d", err, len(es))
	}
	for _, bad := range []string{good + "\n{torn", good + "\n{\"event\":\"asked\"}\n", good + "\n" + `{"decision":"` + strings.Repeat("x", 2<<20) + `"}`} {
		if _, err := Parse([]byte(bad)); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("not corrupt: %v", err)
		}
	}
}

// m2: an asked line without a readable expiry is expired.
func TestMissingExpiryIsExpired(t *testing.T) {
	s := Resolve([]Entry{{Event: Asked, Decision: "d", Pack: "ml", Grant: "approve", TS: time.Now().UTC().Format(time.RFC3339)}})["d"]
	if !s.Expired(time.Now()) {
		t.Fatal("no expiry must mean expired")
	}
}

// M2: exactly one claimer wins, across goroutines (O_EXCL is per process
// and across processes alike).
func TestClaimOnce(t *testing.T) {
	isolate(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Claim("acme", "cloud", "d-1"); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			} else if !errors.Is(err, ErrClaimed) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 || !Claimed("acme", "cloud", "d-1") || Claimed("acme", "cloud", "d-2") {
		t.Fatalf("wins %d", wins)
	}
	if err := Claim("acme", "cloud", "../x"); err == nil {
		t.Fatal("bad id accepted")
	}
}

// m3: compaction drops finished decisions and keeps live ones.
func TestCompact(t *testing.T) {
	isolate(t)
	now := time.Now().UTC()
	future := now.Add(time.Hour).Format(time.RFC3339)
	past := now.Add(-time.Hour).Format(time.RFC3339)
	for _, e := range []Entry{
		{Event: Asked, Decision: "live", Pack: "ml", Grant: "approve", ExpiresAt: future},
		{Event: Asked, Decision: "used", Pack: "ml", Grant: "approve", ExpiresAt: future},
		{Event: Consumed, Decision: "used", Pack: "ml"},
		{Event: Asked, Decision: "old", Pack: "ml", Grant: "approve", ExpiresAt: past},
	} {
		if err := Append("acme", e); err != nil {
			t.Fatal(err)
		}
	}
	if err := Compact("acme", "ml", now); err != nil {
		t.Fatal(err)
	}
	es, err := Read("acme", "ml")
	if err != nil || len(es) != 1 || es[0].Decision != "live" {
		t.Fatalf("%v %+v", err, es)
	}
}
