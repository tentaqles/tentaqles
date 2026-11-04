package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func collect(fn func(add func(level, code, ws, msg, fix string))) []Finding {
	var out []Finding
	fn(func(level, code, ws, msg, fix string) {
		out = append(out, Finding{Level: level, Code: code, Workspace: ws, Msg: msg, Fix: fix})
	})
	return out
}

func TestManifestDroppedBlock_ListsTheBlocks(t *testing.T) {
	fs := collect(func(add func(string, string, string, string, string)) {
		manifestDroppedBlock("acme", []string{"decision", "data"}, add)
	})
	if len(fs) != 1 || fs[0].Level != "warn" || fs[0].Code != "manifest-dropped-block" || fs[0].Workspace != "acme" {
		t.Fatalf("%+v", fs)
	}
	if !strings.Contains(fs[0].Msg, "decision, data") || !strings.Contains(fs[0].Fix, "tq allow acme") {
		t.Fatalf("%+v", fs[0])
	}
}

func TestManifestDroppedBlock_SilentWhenClean(t *testing.T) {
	fs := collect(func(add func(string, string, string, string, string)) {
		manifestDroppedBlock("acme", nil, add)
	})
	if len(fs) != 0 {
		t.Fatalf("%+v", fs)
	}
}

func TestStateDirUnused_ListsOnlyWhatExists(t *testing.T) {
	home := testutil.TempDir(t)
	if err := os.MkdirAll(filepath.Join(home, "hub", "ws"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "guard.yaml"), []byte("mode: strict\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "trust"), 0o755); err != nil {
		t.Fatal(err)
	}
	fs := collect(func(add func(string, string, string, string, string)) { stateDirUnused(home, add) })
	if len(fs) != 1 || fs[0].Level != "warn" || fs[0].Code != "state-dir-unused" || fs[0].Workspace != "" {
		t.Fatalf("%+v", fs)
	}
	if !strings.Contains(fs[0].Msg, "hub, guard.yaml") || strings.Contains(fs[0].Msg, "packs") || strings.Contains(fs[0].Msg, "trust") {
		t.Fatalf("%+v", fs[0])
	}
	if !strings.Contains(fs[0].Msg, "never deletes") {
		t.Fatalf("the warning must say tq leaves them: %+v", fs[0])
	}
	// Nothing was removed.
	if _, err := os.Stat(filepath.Join(home, "hub", "ws")); err != nil {
		t.Fatal("doctor must not delete state:", err)
	}
}

func TestStateDirUnused_SilentWhenClean(t *testing.T) {
	home := testutil.TempDir(t)
	if err := os.MkdirAll(filepath.Join(home, "trust"), 0o755); err != nil {
		t.Fatal(err)
	}
	fs := collect(func(add func(string, string, string, string, string)) { stateDirUnused(home, add) })
	if len(fs) != 0 {
		t.Fatalf("%+v", fs)
	}
}

// TestRun_OldManifestWarnsAndExitsZero: a trusted manifest that still carries
// decision: and data: loads, doctor warns manifest-dropped-block (and
// state-dir-unused for a leftover decide/ cache) and the exit code stays 0,
// the "old manifest → warns, exit 0" line of the 0.9.0 cut.
func TestRun_OldManifestWarnsAndExitsZero(t *testing.T) {
	h := isolatedHome(t)
	home := filepath.Join(h, ".tentaqles")
	if err := os.MkdirAll(filepath.Join(home, "decide"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	root := filepath.Join(base, "acme")
	os.MkdirAll(root, 0o755)
	mp := filepath.Join(root, manifest.FileName)
	body := "schema: tentaqles-client-v2\nclient: acme\ngit: { email: a@b }\n" +
		"decision:\n  backend: typesafe\n  mode: enforce\n  base_url: http://127.0.0.1:1/v1/x\n" +
		"data:\n  classes: [pii]\n  paths:\n    - { path: \"db/**\", classes: [pii] }\n"
	if err := os.WriteFile(mp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := trust.HashFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.Allow(hash); err != nil {
		t.Fatal(err)
	}
	wsGit := gitcfg.WorkspaceFile(root)
	if err := os.WriteFile(wsGit, []byte("# managed by tq\n[user]\n\temail = a@b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git := map[string]string{
		"include.path":       filepath.ToSlash(gitcfg.IncludeFile()),
		"user.useConfigOnly": "true",
	}
	// cwd outside the workspace: the cwd check has nothing to compare, as in
	// TestRun_GlobalEmailSet.
	fs := Run(cfg, deps(nil, base, git))
	if has(fs, "manifest-invalid") {
		t.Fatalf("an old manifest must still load: %+v", fs)
	}
	dropped := findingsWith(fs, "manifest-dropped-block")
	if len(dropped) != 1 || dropped[0].Level != "warn" || dropped[0].Workspace != "acme" ||
		!strings.Contains(dropped[0].Msg, "decision, data") {
		t.Fatalf("%+v", fs)
	}
	state := findingsWith(fs, "state-dir-unused")
	if len(state) != 1 || state[0].Level != "warn" || !strings.Contains(state[0].Msg, "decide") {
		t.Fatalf("%+v", fs)
	}
	for _, f := range fs {
		if f.Level == "error" {
			t.Fatalf("no error expected from an old manifest, got %+v", f)
		}
	}
	if Exit(fs) != 0 {
		t.Fatalf("warnings must exit 0: %+v", fs)
	}
}
