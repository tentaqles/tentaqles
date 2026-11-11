package codexconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// goldenLobePostToolUse is Codex 0.154.0's own currentHash for the memory's handler
// "tentaqles lobe-hook codex post-tool-use; exit 0" (timeout 5, alone in PostToolUse, label post_tool_use).
const goldenLobePostToolUse = "sha256:36e35d7278d88e1f884c1b818070a0ced16a988cb18ca3d9d8d29595a6163f47"

// goldenLobeSessionEnd is Codex 0.167.1's own hash (hooks/list, 2026-11-10) for the memory's handler
// "tentaqles lobe-hook codex session-end; exit 0" (timeout 3, alone in SessionEnd, label session_end).
const goldenLobeSessionEnd = "sha256:9069bd4b1850d629b0f36bfcd48ab9fa4c3baf0829e2c759bb60c1fa4fe4a550"

const lobeCommand = "tentaqles lobe-hook codex post-tool-use; exit 0"

// userPostToolUse is a person's PostToolUse group, written before Render.
const userPostToolUse = `[[hooks.PostToolUse]]
matcher = "Bash"

[[hooks.PostToolUse.hooks]]
type = "command"
command = "python3 /home/me/log.py"
`

func TestLobeRenderWritesTheCodexHandler(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, userPostToolUse)
	if _, err := Render(home, Managed{Command: testCommand}); err != nil {
		t.Fatal(err)
	}

	root := readRoot(t, home)
	groups, err := groupsOf(mustHooks(t, root)["PostToolUse"])
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("PostToolUse has %d groups, want 2 (the person's, then ours)", len(groups))
	}
	hs, _ := handlersOf(groups[0])
	if len(hs) != 1 || hs[0]["command"] != "python3 /home/me/log.py" {
		t.Errorf("the person's PostToolUse group moved or changed: %v", groups[0])
	}

	last := groups[1]
	if _, has := last["matcher"]; has {
		t.Errorf("the lobe's group has a matcher: %v", last["matcher"])
	}
	hs, _ = handlersOf(last)
	if len(hs) != 1 {
		t.Fatalf("the lobe's group holds %d handlers, want 1", len(hs))
	}
	if hs[0]["command"] != lobeCommand {
		t.Errorf("command = %v, want %q", hs[0]["command"], lobeCommand)
	}
	if hs[0]["timeout"] != int64(lobeHookTimeoutSec) {
		t.Errorf("timeout = %v, want %d", hs[0]["timeout"], lobeHookTimeoutSec)
	}
	if hs[0]["type"] != "command" {
		t.Errorf("type = %v, want command", hs[0]["type"])
	}

	// Trust: the state entry holds the handler's hash, and the handler is enabled.
	abs, err := configKeyPath(ConfigPath(home))
	if err != nil {
		t.Fatal(err)
	}
	key := hookKey(abs, "post_tool_use", 1, 0)
	entry, _ := hookState(root)[key].(map[string]any)
	if entry == nil {
		t.Fatalf("no [hooks.state] entry for %s", key)
	}
	if want := hookHash("post_tool_use", last, hs[0]); entry["trusted_hash"] != want {
		t.Errorf("trusted_hash = %v, want %s", entry["trusted_hash"], want)
	}
	if on, _ := entry["enabled"].(bool); !on {
		t.Errorf("enabled = %v, want true", entry["enabled"])
	}
	if got, err := LobeHookTrust(home); err != nil || got != Trusted {
		t.Errorf("LobeHookTrust after Render = %v, %v; want trusted", got, err)
	}

	// A second Render with the same command changes nothing.
	before, _ := os.ReadFile(ConfigPath(home))
	changed, err := Render(home, Managed{Command: testCommand})
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("second Render reported changed=true")
	}
	after, _ := os.ReadFile(ConfigPath(home))
	if string(before) != string(after) {
		t.Errorf("second Render changed the bytes:\n%s\n---\n%s", before, after)
	}

	// Another command replaces the handler: still one, and the person's group is still first.
	if _, err := Render(home, Managed{Command: "/opt/tq"}); err != nil {
		t.Fatal(err)
	}
	root = readRoot(t, home)
	if got := ownedCommands(t, root, "PostToolUse"); !reflect.DeepEqual(got, []string{"/opt/tq lobe-hook codex post-tool-use; exit 0"}) {
		t.Errorf("owned PostToolUse commands after the change = %q, want exactly the new one", got)
	}
	groups, _ = groupsOf(mustHooks(t, root)["PostToolUse"])
	if len(groups) != 2 {
		t.Fatalf("PostToolUse has %d groups after the change, want 2", len(groups))
	}
	if hs, _ := handlersOf(groups[0]); hs[0]["command"] != "python3 /home/me/log.py" {
		t.Errorf("the person's group moved after the change: %v", groups[0])
	}
	if got, err := LobeHookTrust(home); err != nil || got != Trusted {
		t.Errorf("LobeHookTrust after the command change = %v, %v; want trusted", got, err)
	}

	// Without the handler the state is Missing. The person's group (it has a matcher) stays; ours
	// (no matcher) goes.
	editConfig(t, home, func(root map[string]any) {
		groups, _ := groupsOf(mustHooks(t, root)["PostToolUse"])
		var kept []map[string]any
		for _, g := range groups {
			if _, has := g["matcher"]; has {
				kept = append(kept, g)
			}
		}
		mustHooks(t, root)["PostToolUse"] = kept
	})
	if got, err := LobeHookTrust(home); err != nil || got != Missing {
		t.Errorf("LobeHookTrust after the handler is deleted = %v, %v; want missing", got, err)
	}
}

// The memory's handler is never a guard handler: the guard's two hashes and its trust do not move
// when the memory's handler is removed, untrusted, or edited.
func TestLobeCodexHooksNeverChangeGuardTrust(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, home string)
	}{
		{"lobe handler removed", func(t *testing.T, home string) {
			editConfig(t, home, func(root map[string]any) { delete(mustHooks(t, root), "PostToolUse") })
		}},
		{"lobe state entry removed", func(t *testing.T, home string) {
			dropState(t, home, ":post_tool_use:")
		}},
		{"lobe command edited", func(t *testing.T, home string) {
			editConfig(t, home, func(root map[string]any) {
				groups, _ := groupsOf(mustHooks(t, root)["PostToolUse"])
				for _, g := range groups {
					hs, _ := handlersOf(g)
					for _, h := range hs {
						if isOwnedHandler(h) {
							h["command"] = "/opt/tq lobe-hook codex post-tool-use; exit 0"
						}
					}
				}
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			mustRender(t, home, testCommand)
			trustAll(t, home)
			tc.setup(t, home)

			if got, err := HookTrust(home); err != nil || got != Trusted {
				t.Errorf("HookTrust = %v, %v; want trusted", got, err)
			}
			if g := CheckGuard(home, "acme"); !g.OK {
				t.Errorf("CheckGuard = %+v; want OK", g)
			}
			assertHashes(t, readRoot(t, home), map[string]string{
				"pre_tool_use:0:0":  goldenPreToolUse,
				"session_start:0:0": goldenSessionStart,
			})
		})
	}
}

// TestLobeHookTrustGoldenHashes checks LobeHookTrust against the hash Codex 0.154.0 reported for
// the memory's handler (see goldenLobePostToolUse), without computing it with this package's hash.
func TestLobeHookTrustGoldenHashes(t *testing.T) {
	home := t.TempDir()
	mustRender(t, home, testCommand)
	abs, err := filepath.Abs(ConfigPath(home))
	if err != nil {
		t.Fatal(err)
	}
	trustKeys(t, home, map[string]string{
		hookKey(abs, "post_tool_use", 0, 0): goldenLobePostToolUse,
		hookKey(abs, "session_end", 0, 0):   goldenLobeSessionEnd,
	})
	if got, err := LobeHookTrust(home); err != nil || got != Trusted {
		t.Fatalf("LobeHookTrust with Codex's own hashes = %v, %v; want Trusted", got, err)
	}
}

// TestLobeCodexSessionEndHandler: after a render the config holds the memory's SessionEnd handler at
// timeout 3, the person's SessionEnd group stays first, and the handler is trusted. The sweep the
// handler runs is the hook step's; this checks the config half only.
func TestLobeCodexSessionEndHandler(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, "[[hooks.SessionEnd]]\n[[hooks.SessionEnd.hooks]]\ntype = \"command\"\ncommand = \"python3 /home/me/bye.py\"\n")
	if _, err := Render(home, Managed{Command: testCommand}); err != nil {
		t.Fatal(err)
	}

	root := readRoot(t, home)
	groups, err := groupsOf(mustHooks(t, root)["SessionEnd"])
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("SessionEnd has %d groups, want 2 (the person's, then ours)", len(groups))
	}
	if hs, _ := handlersOf(groups[0]); len(hs) != 1 || hs[0]["command"] != "python3 /home/me/bye.py" {
		t.Errorf("the person's SessionEnd group moved or changed: %v", groups[0])
	}
	hs, _ := handlersOf(groups[1])
	if len(hs) != 1 || hs[0]["command"] != "tentaqles lobe-hook codex session-end; exit 0" {
		t.Fatalf("the lobe's SessionEnd group = %v, want the session-end handler alone", groups[1])
	}
	if hs[0]["timeout"] != int64(lobeSessionEndSec) {
		t.Errorf("timeout = %v, want %d", hs[0]["timeout"], lobeSessionEndSec)
	}

	abs, err := configKeyPath(ConfigPath(home))
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := hookState(root)[hookKey(abs, "session_end", 1, 0)].(map[string]any)
	if entry == nil {
		t.Fatal("no [hooks.state] entry for the SessionEnd handler")
	}
	if want := hookHash("session_end", groups[1], hs[0]); entry["trusted_hash"] != want {
		t.Errorf("trusted_hash = %v, want %s", entry["trusted_hash"], want)
	}
	if got, err := LobeHookTrust(home); err != nil || got != Trusted {
		t.Errorf("LobeHookTrust after Render = %v, %v; want trusted", got, err)
	}
}

// TestLobeHookTrustMissingWhenEitherLobeHandlerIsGone: LobeHookTrust is Missing when the PostToolUse
// or the SessionEnd handler is absent, and the guard's trust is unchanged.
func TestLobeHookTrustMissingWhenEitherLobeHandlerIsGone(t *testing.T) {
	for _, table := range []string{"PostToolUse", "SessionEnd"} {
		t.Run(table, func(t *testing.T) {
			home := t.TempDir()
			mustRender(t, home, testCommand)
			trustAll(t, home)
			editConfig(t, home, func(root map[string]any) { delete(mustHooks(t, root), table) })
			if got, err := LobeHookTrust(home); err != nil || got != Missing {
				t.Errorf("LobeHookTrust without %s = %v, %v; want missing", table, got, err)
			}
			if got, err := HookTrust(home); err != nil || got != Trusted {
				t.Errorf("HookTrust without %s = %v, %v; want trusted", table, got, err)
			}
		})
	}
}

// TestLobeMalformedPostToolUseKeepsTheGuard: a person's PostToolUse or SessionEnd that is not a list
// of groups leaves Render writing and trusting the guard's two handlers; the memory's handler is not
// written there and its table stays as the person wrote it.
func TestLobeMalformedPostToolUseKeepsTheGuard(t *testing.T) {
	for _, table := range []string{"PostToolUse", "SessionEnd"} {
		t.Run(table, func(t *testing.T) {
			home := t.TempDir()
			writeConfig(t, home, "[hooks]\n"+table+" = \"not a list of groups\"\n")
			if _, err := Render(home, Managed{Command: testCommand}); err != nil {
				t.Fatalf("Render with a malformed %s: %v", table, err)
			}
			if tr, err := HookTrust(home); err != nil || tr != Trusted {
				t.Errorf("HookTrust = %v, %v; want trusted", tr, err)
			}
			if g := CheckGuard(home, "acme"); !g.OK {
				t.Errorf("CheckGuard not OK: %s", g.Why)
			}
			if tr, err := LobeHookTrust(home); err != nil || tr != Missing {
				t.Errorf("LobeHookTrust = %v, %v; want missing", tr, err)
			}
			if v := mustHooks(t, readRoot(t, home))[table]; v != "not a list of groups" {
				t.Errorf("%s changed to %v", table, v)
			}
		})
	}
}
