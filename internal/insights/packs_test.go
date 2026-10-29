package insights

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadPacks(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	ts := now.Add(-time.Hour).Format(time.RFC3339)
	ev := strings.Join([]string{
		`{"ts":"` + ts + `","workspace":"acme","check":"data/contract-breaking","pack":"data","status":"ok","required":true,"duration_ms":40}`,
		`{"ts":"` + ts + `","workspace":"acme","check":"data/contract-breaking","pack":"data","status":"fail","required":true,"duration_ms":90}`,
		`{"ts":"` + ts + `","workspace":"acme","check":"data/sqlfluff","pack":"data","status":"fail","required":true,"declared_missing":true}`,
		`{"ts":"` + ts + `","workspace":"acme","check":"ml/ruff","pack":"ml","status":"fail","required":false}`,
		`{"ts":"` + ts + `","workspace":"acme","pack":"cloud","counter":"apply_denied:expired","n":2}`,
		`{"ts":"` + ts + `","workspace":"other","check":"ml/ruff","pack":"ml","status":"ok"}`,
	}, "\n") + "\n"
	os.MkdirAll(filepath.Join(dir, "acme", "ml"), 0o700)
	os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(ev), 0o600)
	asked := now.Add(-3 * time.Hour).Format(time.RFC3339)
	answered := now.Add(-2 * time.Hour).Format(time.RFC3339)
	exp := now.Add(-time.Minute).Format(time.RFC3339)
	led := strings.Join([]string{
		`{"event":"asked","decision":"d1","pack":"ml","action":"promote","grant":"approve","expires_at":"2026-11-30T00:00:00Z","ts":"` + asked + `"}`,
		`{"event":"answered","decision":"d1","pack":"ml","choice":"approve","actor_kind":"hub","ts":"` + answered + `"}`,
		`{"event":"consumed","decision":"d1","pack":"ml","ts":"` + answered + `"}`,
		`{"event":"asked","decision":"d2","pack":"ml","action":"promote","grant":"approve","expires_at":"` + exp + `","ts":"` + asked + `"}`,
		`{"event":"asked","decision":"d3","pack":"ml","action":"promote","grant":"approve","expires_at":"2026-11-30T00:00:00Z","ts":"` + asked + `"}`,
		`{"event":"answered","decision":"d3","pack":"ml","choice":"reject","actor_kind":"terminal","ts":"` + answered + `"}`,
	}, "\n") + "\n"
	os.WriteFile(filepath.Join(dir, "acme", "ml", "ledger.jsonl"), []byte(led), 0o600)
	rep, err := ReadPacks(dir, now.Add(-24*time.Hour), now, func(ws string) bool { return ws == "acme" })
	if err != nil {
		t.Fatal(err)
	}
	cb := rep.Checks["data/contract-breaking"]
	if cb == nil || cb.Runs != 2 || cb.OK != 1 || cb.Fail != 1 || cb.P95MS != 90 {
		t.Fatalf("contract-breaking: %+v", cb)
	}
	if rep.DeclaredMissing != 1 || rep.Checks["ml/ruff"].AdvisoryFail != 1 || rep.Checks["ml/ruff"].Runs != 1 {
		t.Fatalf("missing/advisory: %+v %+v", rep.DeclaredMissing, rep.Checks["ml/ruff"])
	}
	if rep.Counters["cloud"]["apply_denied:expired"] != 2 {
		t.Fatalf("counters: %v", rep.Counters)
	}
	a := rep.Approvals
	if a.Asked != 3 || a.Approved != 1 || a.Rejected != 1 || a.Expired != 1 || a.Used != 1 || a.PromotionsApplied != 1 || a.MedianAnswerMin != 60 {
		t.Fatalf("approvals: %+v", a)
	}
	var buf bytes.Buffer
	writePacks(&buf, rep)
	for _, want := range []string{"== Packs", "data/contract-breaking", "cloud counters: apply_denied:expired 2", "approvals asked 3, approved 1"} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("missing %q:\n%s", want, buf.String())
		}
	}
}
