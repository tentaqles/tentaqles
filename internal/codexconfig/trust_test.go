package codexconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// setEnabled sets enabled on the state entry of one of our hooks.
func setEnabled(t *testing.T, home, label string, on bool) {
	t.Helper()
	key := ownedKeyKey(t, home, label)
	editConfig(t, home, func(root map[string]any) {
		hooks, _ := hooksTable(root)
		state, _ := hooks["state"].(map[string]any)
		entry, _ := state[key].(map[string]any)
		if entry == nil {
			t.Fatalf("no state entry for %s", key)
		}
		entry["enabled"] = on
	})
}

func TestRenderTrustsOwnedHooks(t *testing.T) {
	home := t.TempDir()
	mustRender(t, home, testCommand)
	got, err := HookTrust(home)
	if err != nil || got != Trusted {
		t.Fatalf("HookTrust after Render = %v, %v; want trusted", got, err)
	}
	for _, label := range []string{"pre_tool_use", "session_start"} {
		key := ownedKeyKey(t, home, label)
		entry, _ := hookState(readRoot(t, home))[key].(map[string]any)
		if on, _ := entry["enabled"].(bool); !on {
			t.Errorf("%s: enabled = %v, want true", label, entry["enabled"])
		}
	}
}

func TestTrustHooksLeavesOtherState(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, `model = "gpt-5"

[features]
hooks = true
memories = false

[hooks.state."/home/me/other:pre_tool_use:0:0"]
enabled = false
trusted_hash = "sha256:abc"

[[hooks.PreToolUse]]
matcher = "Write"

[[hooks.PreToolUse.hooks]]
type = "command"
command = "python3 /home/me/other.py"
`)
	mustRender(t, home, testCommand)
	// Our trust is taken away, so TrustHooks has something to restore.
	editConfig(t, home, func(root map[string]any) {
		state := hookState(root)
		delete(state, ownedKeyKey(t, home, "pre_tool_use"))
		delete(state, ownedKeyKey(t, home, "session_start"))
	})
	before := readRoot(t, home)
	if got, _ := HookTrust(home); got != Untrusted {
		t.Fatalf("HookTrust before TrustHooks = %v, want untrusted", got)
	}
	if err := TrustHooks(home); err != nil {
		t.Fatal(err)
	}
	after := readRoot(t, home)
	if !reflect.DeepEqual(before["features"], after["features"]) {
		t.Errorf("features changed: %v -> %v", before["features"], after["features"])
	}
	bs := hookState(before)
	as := hookState(after)
	other := "/home/me/other:pre_tool_use:0:0"
	if !reflect.DeepEqual(bs[other], as[other]) {
		t.Errorf("a user state entry changed: %v -> %v", bs[other], as[other])
	}
	if got, _ := HookTrust(home); got != Trusted {
		t.Errorf("HookTrust = %v, want trusted", got)
	}
}

func TestTrustHooksWithoutOwnedHooksWritesNothing(t *testing.T) {
	home := t.TempDir()
	if err := TrustHooks(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, configFile)); !os.IsNotExist(err) {
		t.Fatalf("TrustHooks created a config file with no owned hooks: %v", err)
	}
	body := "model = \"gpt-5\"\n"
	writeConfig(t, home, body)
	if err := TrustHooks(home); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(home, configFile))
	if string(raw) != body {
		t.Errorf("TrustHooks rewrote a config without owned hooks: %q", raw)
	}
}

func TestHookTrustDisabled(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, home string)
		want  Trust
	}{
		{"enabled = false on PreToolUse", func(t *testing.T, home string) {
			mustRender(t, home, testCommand)
			trustAll(t, home)
			setEnabled(t, home, "pre_tool_use", false)
		}, Disabled},
		{"enabled = false on SessionStart", func(t *testing.T, home string) {
			mustRender(t, home, testCommand)
			trustAll(t, home)
			setEnabled(t, home, "session_start", false)
		}, Disabled},
		{"features hooks = false", func(t *testing.T, home string) {
			mustRender(t, home, testCommand)
			trustAll(t, home)
			setFeature(t, home, "hooks", false)
		}, Disabled},
		{"features codex_hooks = false", func(t *testing.T, home string) {
			mustRender(t, home, testCommand)
			trustAll(t, home)
			setFeature(t, home, "codex_hooks", false)
		}, Disabled},
		{"features hooks = true is not off", func(t *testing.T, home string) {
			mustRender(t, home, testCommand)
			trustAll(t, home)
			setFeature(t, home, "hooks", true)
		}, Trusted},
		{"disabled beats untrusted", func(t *testing.T, home string) {
			mustRender(t, home, testCommand)
			trustAll(t, home)
			setEnabled(t, home, "pre_tool_use", false)
			dropState(t, home, ":session_start:")
		}, Disabled},
		{"disabled beats missing only when a hook is there", func(t *testing.T, home string) {
			mustRender(t, home, testCommand)
			editConfig(t, home, func(root map[string]any) { delete(mustHooks(t, root), "SessionStart") })
		}, Missing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			tc.setup(t, home)
			got, err := HookTrust(home)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("HookTrust = %v, want %v", got, tc.want)
			}
		})
	}
}

// setFeature sets a key of the top-level [features] table.
func setFeature(t *testing.T, home, key string, on bool) {
	t.Helper()
	editConfig(t, home, func(root map[string]any) {
		f, _ := root["features"].(map[string]any)
		if f == nil {
			f = map[string]any{}
			root["features"] = f
		}
		f[key] = on
	})
}

// Render turns a disabled tentaqles entry back on, and leaves the user's [features] alone.
func TestRenderReEnablesAndKeepsFeatures(t *testing.T) {
	home := t.TempDir()
	mustRender(t, home, testCommand)
	trustAll(t, home)
	setEnabled(t, home, "pre_tool_use", false)
	setFeature(t, home, "hooks", false)
	before := readRoot(t, home)["features"]
	if got, _ := HookTrust(home); got != Disabled {
		t.Fatalf("HookTrust = %v, want disabled", got)
	}
	if _, err := Render(home, Managed{Command: testCommand}); err != nil {
		t.Fatal(err)
	}
	if got := readRoot(t, home)["features"]; !reflect.DeepEqual(got, before) {
		t.Errorf("Render changed [features]: %v -> %v", before, got)
	}
	// The feature switch is still off, so the hooks are Disabled until the user turns it on.
	if got, _ := HookTrust(home); got != Disabled {
		t.Errorf("HookTrust with features off = %v, want disabled", got)
	}
	setFeature(t, home, "hooks", true)
	if got, _ := HookTrust(home); got != Trusted {
		t.Errorf("HookTrust after features on = %v, want trusted", got)
	}
}

func TestTrustOrder(t *testing.T) {
	order := []Trust{Missing, Disabled, Untrusted, Modified, Trusted}
	for i := 1; i < len(order); i++ {
		if order[i-1] >= order[i] {
			t.Errorf("%v must rank worse than %v", order[i-1], order[i])
		}
	}
	if Disabled.String() != "disabled" {
		t.Errorf("Disabled.String() = %q", Disabled.String())
	}
}

// The quoted command form (a program path with spaces, as Render writes on Windows) is ours,
// and so is any path; another "codex-hook" subcommand is not.
func TestOwnedCommandQuotedPath(t *testing.T) {
	quoted := `"C:\Program Files\tentaqles\tentaqles.exe" codex-hook pre-tool-use`
	if !ownedCommand(quoted) {
		t.Errorf("quoted path not owned: %s", quoted)
	}
	if !ownedCommand(`"/opt/my tools/tq" codex-hook session-start`) {
		t.Errorf("quoted unix path not owned")
	}
	for _, cmd := range []string{`"" codex-hook pre-tool-use`, `"x" codex-hook other`, `codex-hook pre-tool-use`} {
		if ownedCommand(cmd) {
			t.Errorf("%q must not be owned", cmd)
		}
	}
	home := t.TempDir()
	mustRender(t, home, `"C:\Program Files\tentaqles\tentaqles.exe"`)
	if got, _ := HookTrust(home); got != Trusted {
		t.Errorf("HookTrust with a quoted command = %v, want trusted", got)
	}
}

func TestCheckGuard(t *testing.T) {
	t.Run("no codex home", func(t *testing.T) {
		g := CheckGuard("", "acme")
		if g.OK || !strings.Contains(g.Why, "no codex in its scope") || g.Fix[0] != "add codex to the identities in acme's manifest, then: tentaqles settings render acme" {
			t.Errorf("CheckGuard(\"\") = %+v", g)
		}
	})
	t.Run("missing", func(t *testing.T) {
		home := t.TempDir()
		g := CheckGuard(home, "acme")
		if g.OK || g.Trust != Missing || !strings.Contains(g.Why, "not in") || len(g.Fix) != 1 || g.Fix[0] != "tentaqles settings render acme" {
			t.Errorf("CheckGuard = %+v", g)
		}
	})
	t.Run("trusted", func(t *testing.T) {
		home := t.TempDir()
		mustRender(t, home, testCommand)
		if g := CheckGuard(home, "acme"); !g.OK || g.Err != nil || g.Fix != nil {
			t.Errorf("CheckGuard = %+v", g)
		}
	})
	t.Run("disabled names features", func(t *testing.T) {
		home := t.TempDir()
		mustRender(t, home, testCommand)
		trustAll(t, home)
		setFeature(t, home, "codex_hooks", false)
		g := CheckGuard(home, "acme")
		if g.OK || g.Trust != Disabled || !strings.Contains(g.Fix[0], "[features]") || g.Fix[len(g.Fix)-1] != "tentaqles settings render acme" {
			t.Errorf("CheckGuard = %+v", g)
		}
	})
	t.Run("unreadable config", func(t *testing.T) {
		home := t.TempDir()
		writeConfig(t, home, "hooks = [\n")
		g := CheckGuard(home, "acme")
		if g.OK || g.Err == nil || !strings.Contains(g.Fix[0], "repair") {
			t.Errorf("CheckGuard = %+v", g)
		}
	})
}
