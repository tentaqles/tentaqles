package doctor

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

func codexDoctorWS(t *testing.T, codex bool) (*resolve.Workspace, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ids := map[string]manifest.Identity{"gh": {}}
	if codex {
		ids["codex"] = manifest.Identity{}
	}
	w := &resolve.Workspace{Name: "acme", Hash: "h", Root: t.TempDir(), Manifest: &manifest.Manifest{Client: "acme", Identities: ids}}
	dir := paths.IdentityDir("acme", "codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return w, dir
}

func runCodexCheck(w *resolve.Workspace) map[string]bool {
	got := map[string]bool{}
	codexWorkspaceCheck(w, Deps{}, func(level, code, ws, msg, fix string) { got[code] = true })
	return got
}

func renderHook(t *testing.T, dir string) {
	t.Helper()
	p := codexcfg.Plan{Mode: "default", Approval: "on-request", Sandbox: "workspace-write"}
	if _, err := codexcfg.Render(dir, codexcfg.Want{Plan: &p, Hook: true}, false); err != nil {
		t.Fatal(err)
	}
}

func appendConfig(t *testing.T, dir, s string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, "config.toml"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func TestCodexCheckMissingHook(t *testing.T) {
	w, _ := codexDoctorWS(t, true)
	if got := runCodexCheck(w); !got["codex-hooks-missing"] || got["codex-hooks-untrusted"] {
		t.Fatalf("got %v", got)
	}
}

func TestCodexCheckUntrustedThenTrusted(t *testing.T) {
	w, dir := codexDoctorWS(t, true)
	renderHook(t, dir)
	if got := runCodexCheck(w); !got["codex-hooks-untrusted"] || got["codex-hooks-missing"] {
		t.Fatalf("no trust record: %v", got)
	}
	g, h, ok := codexcfg.FindHook(dir, "PreToolUse")
	if !ok {
		t.Fatal("hook not found")
	}
	// A record for another handler does not count.
	appendConfig(t, dir, "\n[hooks.state.\"/x/.codex/hooks.json:pre_tool_use:9:9\"]\ntrusted_hash = \"sha256:00\"\n")
	if got := runCodexCheck(w); !got["codex-hooks-untrusted"] {
		t.Fatalf("other record: %v", got)
	}
	appendConfig(t, dir, "\n[hooks.state.\"/x/.codex/hooks.json:pre_tool_use:"+strconv.Itoa(g)+":"+strconv.Itoa(h)+"\"]\ntrusted_hash = \"sha256:ab\"\n")
	if got := runCodexCheck(w); len(got) != 0 {
		t.Fatalf("trusted: %v", got)
	}
}

func TestCodexCheckHooksDisabled(t *testing.T) {
	w, dir := codexDoctorWS(t, true)
	renderHook(t, dir)
	appendConfig(t, dir, "\n[features]\nhooks = false\n")
	if got := runCodexCheck(w); !got["codex-hooks-disabled"] {
		t.Fatalf("got %v", got)
	}
}

func TestCodexCheckSkipsWithoutCodexIdentity(t *testing.T) {
	w, _ := codexDoctorWS(t, false)
	if got := runCodexCheck(w); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestCodexCheckRegistered(t *testing.T) {
	for _, c := range Registered() {
		if c.Name == "codex" && c.Workspace != nil {
			return
		}
	}
	t.Fatal("the codex check must be registered")
}
