package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/lobe/health"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
)

// healthBody is the manifest body of a health test client: no lobe key, so every setting is a default.
func healthBody(name string) string {
	return "git:\n    email: dev@" + name + ".test\nidentities:\n    claude: {}\n"
}

// healthyClient writes client name under the base with its store opened, as a render leaves it.
func healthyClient(t *testing.T, base, name string) {
	t.Helper()
	writeClient(t, base, name, healthBody(name))
	if _, err := store.Open(name, store.ReadWrite); err != nil {
		t.Fatal(err)
	}
}

// makeLink makes link a junction on Windows (cmd /c mklink /J) or a symbolic link elsewhere, to target.
// It skips the test when the link cannot be made.
func makeLink(t *testing.T, link, target string) {
	t.Helper()
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
			t.Skipf("cannot make a junction: %v: %s", err, out)
		}
		t.Cleanup(func() { _ = exec.Command("cmd", "/c", "rmdir", link).Run() })
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot make a symbolic link: %v", err)
	}
}

// lobeDoctorFindings is what tentaqles doctor adds for the lobe, as doctor findings.
func lobeDoctorFindings(t *testing.T) []doctor.Finding {
	t.Helper()
	cfg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	return lobeFindings(cfg, time.Now())
}

// lobeFixture makes one lobe finding for the client name, under the base; the client is named after the
// finding's key so that several fixtures can share one home.
type lobeFixture struct {
	code, level, name string
	setup             func(t *testing.T, base, name string)
}

// pluginWithoutLobe installs a tentaqles plugin for the client's Claude config whose hooks.json holds only
// the guard's entry.
func pluginWithoutLobe(t *testing.T, name string) {
	t.Helper()
	root := filepath.Join(paths.IdentityDir(name, "claude"), "plugins", "cache", "tentaqles-market", "tentaqles", "0.16.0")
	writeTB(t, filepath.Join(root, ".claude-plugin", "plugin.json"), "{}\n")
	writeTB(t, filepath.Join(root, "hooks", "hooks.json"),
		`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"bash x/tq_hook.sh pre-tool-use","timeout":15}]}]}}`+"\n")
}

// lobeFixtures holds one fixture per code this package reports (F-07 step 5, and lobe-hooks-untrusted).
var lobeFixtures = []lobeFixture{
	{health.CodeStoreLinked, "error", "linked", func(t *testing.T, base, name string) {
		healthyClient(t, base, name)
		if err := os.RemoveAll(paths.LobeDir(name)); err != nil {
			t.Fatal(err)
		}
		makeLink(t, paths.LobeDir(name), filepath.Join(base, "elsewhere"))
	}},
	{health.CodeStoreForeign, "error", "foreign", func(t *testing.T, base, name string) {
		healthyClient(t, base, name)
		writeTB(t, filepath.Join(paths.LobeDir(name), "store.json"),
			`{"v":1,"client":"someone-else","created":"2026-01-01T00:00:00Z"}`+"\n")
	}},
	{health.CodeStoreMissing, "warn", "missing", func(t *testing.T, base, name string) {
		healthyClient(t, base, name)
		if err := os.RemoveAll(paths.LobeDir(name)); err != nil {
			t.Fatal(err)
		}
	}},
	{health.CodeStoreUnreadable, "warn", "unreadable", func(t *testing.T, base, name string) {
		healthyClient(t, base, name)
		writeTB(t, filepath.Join(paths.LobeDir(name), "store.json"), "x")
	}},
	{health.CodeStoreVersion, "warn", "version", func(t *testing.T, base, name string) {
		healthyClient(t, base, name)
		writeTB(t, filepath.Join(paths.LobeDir(name), "store.json"),
			`{"v":99,"client":"version","created":"2026-01-01T00:00:00Z"}`+"\n")
	}},
	{health.CodeHooksMissing, "warn", "hooks", func(t *testing.T, base, name string) {
		healthyClient(t, base, name)
		pluginWithoutLobe(t, name)
	}},
	{health.CodeSettingInvalid, "warn", "setting", func(t *testing.T, base, name string) {
		writeClient(t, base, name, healthBody(name)+"lobe:\n  capture: maybe\n  enforce: loud\n")
		if _, err := store.Open(name, store.ReadWrite); err != nil {
			t.Fatal(err)
		}
	}},
	{health.CodeHooksUntrusted, "warn", "codex", func(t *testing.T, base, name string) {
		writeClient(t, base, name, "git:\n    email: dev@"+name+".test\nidentities:\n    codex: {}\n")
		if _, err := store.Open(name, store.ReadWrite); err != nil {
			t.Fatal(err)
		}
		renderCodexFixture(t, name)
		// A changed lobe handler (its timeout is hashed, its command still owned): Codex's trust is Modified.
		editCodex(t, name, func(root map[string]any) {
			hooks := root["hooks"].(map[string]any)
			for _, g := range asList(hooks["PostToolUse"]) {
				for _, h := range asList(g.(map[string]any)["hooks"]) {
					h.(map[string]any)["timeout"] = int64(6)
				}
			}
		})
	}},
	{health.CodeHookFailing, "warn", "failing", func(t *testing.T, base, name string) {
		healthyClient(t, base, name)
		now := time.Now().UTC().Format(time.RFC3339)
		var b strings.Builder
		for i := 0; i < health.HookFailMin; i++ {
			b.WriteString(`{"at":"` + now + `","host":"claude","event":"post-tool-use-failure","step":"S-07","class":"io"}` + "\n")
		}
		writeTB(t, filepath.Join(paths.LobeDir(name), "stats", "hook-errors.jsonl"), b.String())
	}},
	{health.CodeEventsDropped, "warn", "dropped", func(t *testing.T, base, name string) {
		healthyClient(t, base, name)
		now := time.Now().UTC()
		var b strings.Builder
		for i := 0; i < health.EventsDroppedMin; i++ {
			b.WriteString(`{"at":"` + now.Format(time.RFC3339) + `","why":"full"}` + "\n")
		}
		writeTB(t, filepath.Join(paths.LobeDir(name), filepath.FromSlash(eventlog.DropsFileFor(now))), b.String())
	}},
}

// TestLobeDoctorCodes: each of the eight codes is produced by its own fixture and by no other. A healthy
// client gives none. Each finding names its client and has the level of F-07 step 5.
func TestLobeDoctorCodes(t *testing.T) {
	t.Run("a healthy client has no lobe finding", func(t *testing.T) {
		base := newBase(t)
		healthyClient(t, base, "plain")
		if fs := lobeDoctorFindings(t); len(fs) != 0 {
			t.Fatalf("healthy client: %+v", fs)
		}
	})
	for _, fx := range lobeFixtures {
		t.Run(fx.code, func(t *testing.T) {
			base := newBase(t)
			fx.setup(t, base, fx.name)
			fs := lobeDoctorFindings(t)
			if len(fs) != 1 {
				t.Fatalf("%s: %d findings, want exactly 1: %+v", fx.code, len(fs), fs)
			}
			f := fs[0]
			if f.Code != fx.code {
				t.Fatalf("code = %s, want %s (%s)", f.Code, fx.code, f.Msg)
			}
			if f.Level != fx.level {
				t.Errorf("%s level = %s, want %s", fx.code, f.Level, fx.level)
			}
			if f.Workspace != fx.name {
				t.Errorf("%s names client %q, want %q", fx.code, f.Workspace, fx.name)
			}
			if f.Msg == "" || f.Fix == "" {
				t.Errorf("%s has an empty message or fix: %+v", fx.code, f)
			}
		})
	}
}

// TestLobeDoctorCodes_OneFindingPerClient: two wrong keys of one client give one lobe-setting-invalid
// finding that names both keys.
func TestLobeDoctorCodes_OneFindingPerClient(t *testing.T) {
	base := newBase(t)
	for _, fx := range lobeFixtures {
		if fx.code == health.CodeSettingInvalid {
			fx.setup(t, base, fx.name)
		}
	}
	fs := lobeDoctorFindings(t)
	if len(fs) != 1 || !strings.Contains(fs[0].Msg, "capture, enforce") {
		t.Fatalf("findings = %+v, want one lobe-setting-invalid naming capture and enforce", fs)
	}
}

// TestLobeHealthNeverReachesTheGuard: with every lobe finding present, the guard's pre-tool-use still
// allows `echo hi` in each client's folder.
func TestLobeHealthNeverReachesTheGuard(t *testing.T) {
	base := newBase(t)
	seen := map[string]bool{}
	for _, fx := range lobeFixtures {
		fx.setup(t, base, fx.name)
		seen[fx.code] = false
	}
	for _, f := range lobeDoctorFindings(t) {
		seen[f.Code] = true
	}
	for code, ok := range seen {
		if !ok {
			t.Fatalf("finding %s is not present, so the test would prove nothing", code)
		}
	}
	for _, fx := range lobeFixtures {
		ws := filepath.Join(base, fx.name)
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, "echo hi"))
		if code != 0 {
			t.Fatalf("%s client with the lobe findings: echo hi code=%d stderr=%q, want 0", fx.name, code, errOut)
		}
	}
}

// TestSettingsRender_OpensTheLobeStore: after a render the store exists and names its client.
func TestSettingsRender_OpensTheLobeStore(t *testing.T) {
	base := newBase(t)
	writeClient(t, base, "acme", healthBody("acme"))
	if out, err := runLobe(t, "", "settings", "render", "acme"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(paths.LobeDir("acme"), "store.json"))
	if err != nil {
		t.Fatalf("no store after the render: %v", err)
	}
	if !strings.Contains(string(raw), `"client":"acme"`) {
		t.Fatalf("store.json = %s, want the client acme", raw)
	}
}

// TestSettingsRender_LinkedLobeWarnsAndFinishes: a lobe folder that is a link gives a warning line, and the
// render still writes the client's Claude settings and finishes without an error.
func TestSettingsRender_LinkedLobeWarnsAndFinishes(t *testing.T) {
	base := newBase(t)
	writeClient(t, base, "acme", healthBody("acme"))
	makeLink(t, paths.LobeDir("acme"), filepath.Join(base, "elsewhere"))
	out, err := runLobe(t, "", "settings", "render", "acme")
	if err != nil {
		t.Fatalf("a linked lobe folder failed the render: %v\n%s", err, out)
	}
	if !strings.Contains(out, "warning: acme: lobe store not opened") {
		t.Fatalf("no warning for the linked lobe folder:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(paths.IdentityDir("acme", "claude"), "settings.json")); err != nil {
		t.Fatalf("the render did not finish, no Claude settings: %v", err)
	}
}
