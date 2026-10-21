package insights

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

func TestReportIncludesHubSection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	hub := filepath.Join(home, "hub")
	now := time.Now().UTC()
	if err := activity.Append(filepath.Join(hub, "acme", "activity.jsonl"), activity.Entry{
		TS: now.Format(time.RFC3339), Workspace: "acme", Verb: activity.VerbRunEnd, CardID: "c-a",
		Data: map[string]any{"status": "finished", "cost_usd": 2.25, "kind": "feature"},
	}); err != nil {
		t.Fatal(err)
	}
	rep, err := Run(Options{Since: now.Add(-time.Hour), Until: now.Add(time.Minute), Sources: []Source{}, HubDir: hub,
		DecideDir: filepath.Join(home, "decide"), BuildGateDir: filepath.Join(home, "build-gate")})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Hub.RunsEnded["finished"] != 1 || rep.Hub.CostByKind["feature"] != 2.25 {
		t.Fatalf("hub %+v", rep.Hub)
	}
	var buf bytes.Buffer
	WriteText(&buf, rep)
	out := buf.String()
	if !strings.Contains(out, "== Hub (tq hub)") || !strings.Contains(out, "feature $2.25") {
		t.Fatalf("text:\n%s", out)
	}
	filtered, _ := Run(Options{Since: now.Add(-time.Hour), Until: now, Sources: []Source{}, HubDir: hub, Workspaces: []string{"globex"},
		DecideDir: filepath.Join(home, "decide"), BuildGateDir: filepath.Join(home, "build-gate")})
	if filtered.Hub.RunsEnded["finished"] != 0 {
		t.Fatal("the --ws filter applies to hub activity")
	}
}
