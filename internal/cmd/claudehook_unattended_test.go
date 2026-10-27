package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardlog"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

const unattendedProject = "guard:\n  unattended:\n    projects: [demo-todo]\n"

func unattendedWS(t *testing.T, body string, bypass bool) (root, proj string) {
	t.Helper()
	root = setupTrustedWorkspaceWithManifest(t, "acme", body)
	proj = filepath.Join(root, "demo-todo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if bypass {
		h, err := trust.HashFile(filepath.Join(root, manifest.FileName))
		if err != nil {
			t.Fatal(err)
		}
		if err := trust.AllowBypass(h); err != nil {
			t.Fatal(err)
		}
	}
	return root, proj
}

func modePayload(t *testing.T, cwd, command, mode string) string {
	t.Helper()
	p := hookPayloadFor(t, cwd, command)
	if mode == "" {
		return p
	}
	return strings.TrimSuffix(p, "}") + `,"permission_mode":` + jsonPath(t, mode) + `}`
}

func TestUnattendedSkipsConfirmInBypassAndAuto(t *testing.T) {
	_, proj := unattendedWS(t, unattendedProject, true)
	for _, mode := range []string{"bypassPermissions", "auto"} {
		code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, `psql -c "drop table t"`, mode))
		if code != 0 || strings.TrimSpace(out) != "" {
			t.Fatalf("%s: want silent allow, got code %d out %q", mode, code, out)
		}
	}
	recs := guardlog.Read("acme", time.Now().Add(-time.Minute), 0)
	if len(recs) != 2 || recs[0].Scope != "demo-todo" || recs[0].Rules[0] != "tq/sql-destructive-shell" {
		t.Fatalf("audit records = %+v", recs)
	}
}

func TestUnattendedAsksInOtherModes(t *testing.T) {
	_, proj := unattendedWS(t, unattendedProject, true)
	for _, mode := range []string{"", "default", "acceptEdits", "plan"} {
		_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, `psql -c "drop table t"`, mode))
		if !strings.Contains(askReason(t, out), "tq/sql-destructive-shell") {
			t.Fatalf("mode %q: want ask, got %q", mode, out)
		}
	}
}

func TestUnattendedNeedsBypassTrustAndCoverage(t *testing.T) {
	_, proj := unattendedWS(t, unattendedProject, false)
	_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, `psql -c "drop table t"`, "bypassPermissions"))
	askReason(t, out) // fails the test if it is not an ask
}

func TestUnattendedOutsideProjectAsks(t *testing.T) {
	root, _ := unattendedWS(t, unattendedProject, true)
	_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, root, `psql -c "drop table t"`, "bypassPermissions"))
	askReason(t, out)
}

func TestUnattendedKeepAskingAndGuardChangeStillAsk(t *testing.T) {
	_, proj := unattendedWS(t, "guard:\n  unattended:\n    workspace: true\n", true)
	for _, cmd := range []string{"terraform apply", "tq guard off tq/force-push"} {
		_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, cmd, "bypassPermissions"))
		askReason(t, out)
	}
}

func TestUnattendedGuardChangeStillAsks(t *testing.T) {
	empty := "guard:\n  unattended:\n    workspace: true\n    keep_asking: []\n"
	_, proj := unattendedWS(t, empty, true)
	_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "tq guard off tq/force-push", "bypassPermissions"))
	if !strings.Contains(askReason(t, out), "tq/guard-change") {
		t.Fatalf("want tq/guard-change ask, got %q", out)
	}
}

func TestUnattendedDenyStillBlocks(t *testing.T) {
	_, proj := unattendedWS(t, "guard:\n  unattended:\n    workspace: true\n", true)
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "cat .env", "bypassPermissions"))
	if code != 2 || !strings.Contains(errOut, "BLOCKED") {
		t.Fatalf("deny must still block: code %d stderr %q", code, errOut)
	}
}

func TestUnattendedJSONReportsSkip(t *testing.T) {
	_, proj := unattendedWS(t, unattendedProject, true)
	_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, modePayload(t, proj, `psql -c "drop table t"`, "auto"))
	var v struct {
		Block  bool   `json:"block"`
		Rule   string `json:"rule"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.Block || v.Rule != "tq/sql-destructive-shell" || !strings.HasPrefix(v.Reason, "unattended: ") {
		t.Fatalf("json = %q (%v)", out, err)
	}
}

func wsFor(t *testing.T, root string) *resolve.Workspace {
	t.Helper()
	mp := filepath.Join(root, manifest.FileName)
	m, err := manifest.Load(mp)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := trust.HashFile(mp)
	return &resolve.Workspace{Name: "acme", Root: root, ManifestPath: mp, Hash: h, Manifest: m}
}

func TestUnattendedLine(t *testing.T) {
	root, proj := unattendedWS(t, unattendedProject, true)
	ws := wsFor(t, root)
	now := time.Now()
	for i := 0; i < 2; i++ {
		guardlog.Append(guardlog.Record{WS: "acme", Scope: "demo-todo", Mode: "auto", Rules: []string{"x"}, Tool: "Bash", Command: "c"}, nil)
	}
	if got, want := unattendedLine(ws, proj, now), "Unattended: on (demo-todo) · 2 confirms skipped in 24h · applies in bypass and auto sessions"; got != want {
		t.Fatalf("got %q", got)
	}
	if got, want := unattendedLine(ws, root, now), "Unattended: on for demo-todo (not this folder)"; got != want {
		t.Fatalf("got %q", got)
	}
}

func TestUnattendedLineNotEffectiveAndUnset(t *testing.T) {
	root, proj := unattendedWS(t, unattendedProject, false)
	if got, want := unattendedLine(wsFor(t, root), proj, time.Now()), "Unattended: configured, not effective (needs tq allow --bypass acme)"; got != want {
		t.Fatalf("got %q", got)
	}
	root2, _ := unattendedWS(t, "", false)
	if got := unattendedLine(wsFor(t, root2), root2, time.Now()); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestSessionStartShowsUnattended(t *testing.T) {
	_, proj := unattendedWS(t, unattendedProject, true)
	code, out, _ := runHook(t, []string{"claude-hook", "session-start"}, hookPayloadFor(t, proj, ""))
	if code != 0 || !strings.Contains(out, "Unattended: on") {
		t.Fatalf("code %d out %q", code, out)
	}
}
