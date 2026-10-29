package cmd

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/guardlog"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

const lockedTail = " · locked: identity, secrets, self-protection, privacy"

// modeLineWS makes a trusted workspace acme and returns it resolved.
func modeLineWS(t *testing.T, body string) *resolve.Workspace {
	t.Helper()
	root := setupTrustedWorkspaceWithManifest(t, "acme", body)
	mp := filepath.Join(root, manifest.FileName)
	m, err := manifest.Load(mp)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := trust.HashFile(mp)
	return &resolve.Workspace{Name: "acme", Root: root, ManifestPath: mp, Hash: h, Manifest: m}
}

func preset(t *testing.T, name string) guardcfg.Table {
	t.Helper()
	tb, ok := guardcfg.Preset(name)
	if !ok {
		t.Fatalf("no preset %s", name)
	}
	return tb
}

func TestModeLineVariants(t *testing.T) {
	ws := modeLineWS(t, "")
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		guardlog.Append(guardlog.Record{Time: now, WS: "acme", Action: "ask", Rules: []string{"tq/cloud-delete"}}, nil)
	}
	guardlog.Append(guardlog.Record{Time: now, WS: "acme", Action: "deny", Rules: []string{"tq/gha-branch-ref"}}, nil)

	custom := preset(t, "relaxed")
	custom[guardcfg.CatDBDestructive] = guardcfg.ValueAsk
	cases := []struct {
		name string
		in   modeLineInput
		want string
	}{
		{"relaxed granted", modeLineInput{WS: ws, Resolved: guardcfg.Resolved{
			Mode: "relaxed", Requested: "relaxed", ModeSource: "acme", Table: preset(t, "relaxed"), Ceiling: &guardcfg.Grant{Mode: "relaxed"},
		}, ClaudeMode: "bypassPermissions"},
			"Guard: relaxed (workspace, granted) · 3 confirms and 1 block skipped in 24h" + lockedTail},
		{"not granted", modeLineInput{WS: ws, Resolved: guardcfg.Resolved{
			Mode: "default", Requested: "relaxed", ModeSource: "acme", Table: preset(t, "default"),
		}, ClaudeMode: "bypassPermissions"},
			"Guard: default (relaxed requested, not granted: tq allow --mode acme relaxed)" + lockedTail},
		{"launch", modeLineInput{WS: ws, Resolved: guardcfg.Resolved{
			Mode: "strict", Requested: "strict", ModeSource: "launch", Table: preset(t, "strict"),
		}},
			"Guard: strict (launch override)" + lockedTail},
		{"project", modeLineInput{WS: ws, Resolved: guardcfg.Resolved{
			Mode: "strict", Requested: "strict", ModeSource: "project:web", Table: preset(t, "strict"),
		}},
			"Guard: strict (project web)" + lockedTail},
		{"custom with repo loosening", modeLineInput{WS: ws, Resolved: guardcfg.Resolved{
			Mode: "custom", Base: "relaxed", Requested: "custom", ModeSource: "acme", Table: custom, Ceiling: &guardcfg.Grant{Mode: "custom"}, Ignored: []string{"system"},
		}},
			"Guard: custom (relaxed + db-destructive ask; workspace, granted) · 3 confirms and 1 block skipped in 24h · repo file tried to loosen: system" + lockedTail},
		{"clamped categories", modeLineInput{WS: ws, Resolved: guardcfg.Resolved{
			Mode: "relaxed", Requested: "relaxed", ModeSource: "global", Table: preset(t, "relaxed"), Ceiling: &guardcfg.Grant{Mode: "relaxed"}, Clamped: []string{"tq/iac-apply"},
		}},
			"Guard: relaxed (global, granted) · 3 confirms and 1 block skipped in 24h · clamped to the grant: tq/iac-apply" + lockedTail},
		{"auto", modeLineInput{WS: ws, Resolved: guardcfg.DefaultResolved(), ClaudeMode: "auto"},
			"Guard: default" + lockedTail + " · Claude mode: auto (adds classifier prompts tq cannot configure)"},
		{"acceptEdits", modeLineInput{WS: ws, Resolved: guardcfg.DefaultResolved(), ClaudeMode: "acceptEdits"},
			"Guard: default" + lockedTail + " · Claude mode: acceptEdits (Claude also prompts for calls tq does not cover)"},
		{"workspace default", modeLineInput{WS: ws, Resolved: guardcfg.DefaultResolved(), ClaudeMode: "bypassPermissions"},
			"Guard: default" + lockedTail},
		{"neutral default", modeLineInput{Resolved: guardcfg.DefaultResolved(), ClaudeMode: "default"}, ""},
		{"neutral strict", modeLineInput{Resolved: guardcfg.Resolved{Mode: "strict", Requested: "strict", ModeSource: "global", Table: preset(t, "strict")}},
			"Guard: strict (global)" + lockedTail},
	}
	for _, c := range cases {
		c.in.Now = now
		if got := modeLine(c.in); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestModeLineNamesPrivacyLocked(t *testing.T) {
	got := modeLine(modeLineInput{WS: modeLineWS(t, ""), Resolved: guardcfg.DefaultResolved(), Now: time.Now()})
	if !strings.Contains(got, "privacy") {
		t.Fatalf("privacy is locked (spec E §19 Q1): %q", got)
	}
}

func TestModeLineCodexCountsAsks(t *testing.T) {
	ws := modeLineWS(t, "")
	got := modeLine(modeLineInput{WS: ws, Resolved: guardcfg.DefaultResolved(), Agent: hookAgentCodex, Now: time.Now()})
	n := len(codexAskIDs(guardcfg.DefaultResolved(), ws.Manifest))
	if n == 0 {
		t.Fatal("default mode has ask rules")
	}
	if !strings.HasSuffix(got, " · asks become blocks in Codex: "+strconv.Itoa(n)+" rules") {
		t.Fatalf("got %q", got)
	}
}

func TestSessionStartPrintsModeLine(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	code, out, _ := runHook(t, []string{"claude-hook", "session-start"}, hookPayloadFor(t, root, ""))
	if code != 0 || !strings.Contains(out, "Guard: default"+lockedTail) {
		t.Fatalf("code %d out %q", code, out)
	}
}
