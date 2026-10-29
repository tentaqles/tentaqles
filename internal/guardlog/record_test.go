package guardlog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordModeFieldsAndLegacyMode(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	// A record written before spec E: "mode" held Claude's permission mode.
	legacy := `{"ts":"` + time.Now().UTC().Format(time.RFC3339) + `","ws":"acme","scope":"workspace","mode":"auto","action":"ask","rule_ids":["tq/force-push"],"tool":"Bash","command":"old"}` + "\n"
	if err := os.WriteFile(Path(), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	Append(Record{WS: "acme", Scope: "workspace", Mode: "relaxed", ClaudeMode: "bypassPermissions", Action: "deny", To: "ask", Rules: []string{"tq/force-push-main"}, Tool: "Bash", Command: "new"}, nil)
	recs := Read("acme", time.Now().Add(-time.Hour), 0)
	if len(recs) != 2 {
		t.Fatalf("want 2 records, got %d", len(recs))
	}
	if r := recs[0]; r.Mode != "relaxed" || r.ClaudeMode != "bypassPermissions" || r.To != "ask" {
		t.Fatalf("new record: %+v", r)
	}
	if r := recs[1]; r.Mode != "" || r.ClaudeMode != "auto" {
		t.Fatalf("legacy record: %+v", r)
	}
}
