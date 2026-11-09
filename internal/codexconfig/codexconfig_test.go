package codexconfig

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

// Ground truth. These hashes are Codex 0.154.0's own currentHash values, read with
// `codex app-server` (JSON-RPC `hooks/list`, initialize then hooks/list) from a scratch
// CODEX_HOME whose config.toml held the hooks below. Codex reported the key as
// "<config.toml path>:<event>:<group>:<handler>", the same form hookKey builds.
//
// The rendered pair is what Render("tentaqles") writes. The probe pair is codexProbeConfig,
// which covers the normalization branches: async, statusMessage, the default timeout,
// timeout 0 (clamped to 1), commandWindows (Windows only), additionalContextLimit set
// and at the 2500 default.
const (
	goldenPreToolUse    = "sha256:e535b008b61cabd99f44b87c3d36cb8d592e3b007368663e82df4fb28d92dd5d"
	goldenSessionStart  = "sha256:3150ac99814931da71b6d7728edcee26d9384d64426218cfe2bad04c909c0898"
	goldenProbePre0     = "sha256:3a869721b588bda3b935a669627acde5dee892eac68a18155c5931e23692f806"
	goldenProbePre1Win  = "sha256:f7c14282627540b6ed56b62ffd881bf8b1ee4124966e261095e4b175c2ef4215"
	goldenProbeSession0 = "sha256:05f29db59d267cb0516cf6cda7f2752877987f510a8b7b7457e5eab353852729"
	goldenProbeSession1 = "sha256:a6907cc080d5e02e71cdaf1f5c38043ee45cab750a2c08fff6c1413f9ea14367"

	// The config.toml path the Codex probe reported (its scratch CODEX_HOME was ...\codex-hash\home).
	codexProbePath = `C:\Users\renato\AppData\Local\Temp\claude\9afa00c4d4304694\f46b45f1-a213-49f8-9684-a79dbe4146e1\scratchpad\codex-hash\home\config.toml`
)

// codexProbeConfig is the config the probe ran against (home2). Group and handler indexes
// in its keys follow this text.
const codexProbeConfig = `[[hooks.PreToolUse]]
matcher = "Bash"

[[hooks.PreToolUse.hooks]]
type = "command"
command = "x --a"
async = true
statusMessage = "hi there"

[[hooks.PreToolUse]]

[[hooks.PreToolUse.hooks]]
type = "command"
command = "a-cmd"
commandWindows = "b-cmd"
timeout = 0

[[hooks.SessionStart]]

[[hooks.SessionStart.hooks]]
type = "command"
command = "y"
timeout = 5
additionalContextLimit = 4096

[[hooks.SessionStart]]
matcher = "startup"

[[hooks.SessionStart.hooks]]
type = "command"
command = "z"
additionalContextLimit = 2500
`

const testCommand = "tentaqles"

func ptr(s string) *string { return &s }

func readRoot(t *testing.T, home string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, configFile))
	if err != nil {
		t.Fatal(err)
	}
	root, err := parseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func writeConfig(t *testing.T, home, body string) {
	t.Helper()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, configFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// editConfig decodes config.toml, applies edit, and writes it back.
func editConfig(t *testing.T, home string, edit func(root map[string]any)) {
	t.Helper()
	root := map[string]any{}
	if _, err := os.Stat(filepath.Join(home, configFile)); err == nil {
		root = readRoot(t, home)
	}
	edit(root)
	out, err := toml.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, configFile), out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// ownedCommands lists the tentaqles handler commands under hooks.<table>, in file order.
func ownedCommands(t *testing.T, root map[string]any, table string) []string {
	t.Helper()
	hooks, err := hooksTable(root)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := groupsOf(hooks[table])
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, g := range groups {
		hs, err := handlersOf(g)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hs {
			if isOwnedHandler(h) {
				out = append(out, h["command"].(string))
			}
		}
	}
	return out
}

// checkOwned asserts that each event carries exactly one tentaqles hook for command, and
// that the hook is the last group with the expected matcher and timeout.
func checkOwned(t *testing.T, root map[string]any, command string) {
	t.Helper()
	hooks, _ := hooksTable(root)
	for _, ev := range hookEvents {
		want := []string{command + " codex-hook " + ev.sub}
		if got := ownedCommands(t, root, ev.table); !reflect.DeepEqual(got, want) {
			t.Errorf("hooks.%s owned commands = %q, want %q", ev.table, got, want)
		}
		groups, _ := groupsOf(hooks[ev.table])
		last := groups[len(groups)-1]
		hs, _ := handlersOf(last)
		if len(hs) != 1 || !isOwnedHandler(hs[0]) {
			t.Errorf("hooks.%s: last group is not the tentaqles group: %v", ev.table, last)
			continue
		}
		if got := hs[0]["timeout"]; got != int64(hookTimeoutSec) {
			t.Errorf("hooks.%s timeout = %v, want %d", ev.table, got, hookTimeoutSec)
		}
		m, has := last["matcher"]
		if ev.matcher == "" && has {
			t.Errorf("hooks.%s has matcher %v, want none", ev.table, m)
		}
		if ev.matcher != "" && m != ev.matcher {
			t.Errorf("hooks.%s matcher = %v, want %q", ev.table, m, ev.matcher)
		}
	}
}

// ownedKeyHashes returns Codex's key -> trust hash for each tentaqles handler in home.
func ownedKeyHashes(t *testing.T, home string) map[string]string {
	t.Helper()
	abs, err := configKeyPath(filepath.Join(home, configFile))
	if err != nil {
		t.Fatal(err)
	}
	root := readRoot(t, home)
	hooks, _ := hooksTable(root)
	out := map[string]string{}
	for _, ev := range hookEvents {
		groups, _ := groupsOf(hooks[ev.table])
		for gi, g := range groups {
			hs, _ := handlersOf(g)
			for hi, h := range hs {
				if isOwnedHandler(h) {
					out[hookKey(abs, ev.label, gi, hi)] = hookHash(ev.label, g, h)
				}
			}
		}
	}
	return out
}

// trustAll simulates the user accepting the tentaqles hooks on Codex's /hooks screen.
func trustAll(t *testing.T, home string) {
	t.Helper()
	trustKeys(t, home, ownedKeyHashes(t, home))
}

// trustKeys writes [hooks.state] trusted_hash entries for the given key -> hash map.
func trustKeys(t *testing.T, home string, keys map[string]string) {
	t.Helper()
	editConfig(t, home, func(root map[string]any) {
		hooks, _ := hooksTable(root)
		state, _ := hooks["state"].(map[string]any)
		if state == nil {
			state = map[string]any{}
			hooks["state"] = state
		}
		for key, hash := range keys {
			state[key] = map[string]any{"trusted_hash": hash}
		}
	})
}

// dropState removes the [hooks.state] entries whose key contains substr.
func dropState(t *testing.T, home, substr string) {
	t.Helper()
	editConfig(t, home, func(root map[string]any) {
		hooks, _ := hooksTable(root)
		state, _ := hooks["state"].(map[string]any)
		for key := range state {
			if strings.Contains(key, substr) {
				delete(state, key)
			}
		}
	})
}

func TestRenderWritesOwnedHooks(t *testing.T) {
	cases := []struct {
		name string
		m    Managed
		di   string // developer_instructions after Render; "" means absent
	}{
		{"hooks only", Managed{Command: testCommand}, ""},
		{"with language line", Managed{Command: testCommand, DeveloperInstructions: "Reply in English."}, "Reply in English."},
		{"command with spaces", Managed{Command: `C:\Program Files\tentaqles\tq.exe`}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "codex") // the folder does not exist yet
			changed, err := Render(home, tc.m)
			if err != nil {
				t.Fatal(err)
			}
			if !changed {
				t.Fatal("changed = false for a new config file")
			}
			root := readRoot(t, home)
			checkOwned(t, root, tc.m.Command)
			got, has := root[devInstrKey]
			if tc.di == "" && has {
				t.Errorf("developer_instructions = %v, want absent", got)
			}
			if tc.di != "" && got != tc.di {
				t.Errorf("developer_instructions = %v, want %q", got, tc.di)
			}
		})
	}
}

func TestRenderKeepsUserConfig(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"user keys and a user hook in the same event", `model = "gpt-5"
approval_policy = "on-request"

[mcp_servers.docs]
command = "docs-mcp"

[[hooks.PreToolUse]]
matcher = "Write"

[[hooks.PreToolUse.hooks]]
type = "command"
command = "python3 /home/me/guard.py"
timeout = 5

[hooks.state."/home/me/guard:pre_tool_use:0:0"]
enabled = false
`},
		{"user hook only in SessionStart", `[[hooks.SessionStart]]

[[hooks.SessionStart.hooks]]
type = "command"
command = "echo hello"
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			writeConfig(t, home, tc.body)
			before := readRoot(t, home)
			if _, err := Render(home, Managed{Command: testCommand, DeveloperInstructions: "Be brief."}); err != nil {
				t.Fatal(err)
			}
			after := readRoot(t, home)
			checkOwned(t, after, testCommand)

			// Every top-level key except hooks is unchanged.
			for k, v := range before {
				if k == "hooks" {
					continue
				}
				if !reflect.DeepEqual(after[k], v) {
					t.Errorf("%s changed: %v -> %v", k, v, after[k])
				}
			}
			// Each user group is still at its original index, ahead of ours.
			bh, _ := hooksTable(before)
			ah, _ := hooksTable(after)
			for _, ev := range hookEvents {
				bg, _ := groupsOf(bh[ev.table])
				ag, _ := groupsOf(ah[ev.table])
				if len(ag) != len(bg)+1 {
					t.Fatalf("hooks.%s: %d groups, want %d", ev.table, len(ag), len(bg)+1)
				}
				for i := range bg {
					if !reflect.DeepEqual(ag[i], bg[i]) {
						t.Errorf("hooks.%s[%d] changed: %v -> %v", ev.table, i, bg[i], ag[i])
					}
				}
			}
			if !reflect.DeepEqual(ah["state"], bh["state"]) {
				t.Errorf("hooks.state changed: %v -> %v", bh["state"], ah["state"])
			}
		})
	}
}

func TestRenderIdempotent(t *testing.T) {
	cases := []struct {
		name string
		body string // config written before the first render; "" means none
		m    Managed
	}{
		{"fresh home", "", Managed{Command: testCommand}},
		{"fresh home with language line", "", Managed{Command: testCommand, DeveloperInstructions: "Reply in English."}},
		{"user config", "model = \"gpt-5\"\n\n[[hooks.PreToolUse]]\nmatcher = \"Write\"\n\n[[hooks.PreToolUse.hooks]]\ntype = \"command\"\ncommand = \"guard\"\n", Managed{Command: testCommand}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.body != "" {
				writeConfig(t, home, tc.body)
			}
			if changed, err := Render(home, tc.m); err != nil || !changed {
				t.Fatalf("first Render: changed=%v err=%v, want changed=true", changed, err)
			}
			first, err := os.ReadFile(filepath.Join(home, configFile))
			if err != nil {
				t.Fatal(err)
			}
			changed, err := Render(home, tc.m)
			if err != nil {
				t.Fatal(err)
			}
			if changed {
				t.Error("second Render reported changed=true")
			}
			second, _ := os.ReadFile(filepath.Join(home, configFile))
			if !bytes.Equal(first, second) {
				t.Errorf("second Render changed the bytes:\n%s\n---\n%s", first, second)
			}
		})
	}
}

func TestRenderCommandChangeReplacesOwnedHooks(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, `[[hooks.PreToolUse]]
matcher = "Write"

[[hooks.PreToolUse.hooks]]
type = "command"
command = "python3 /home/me/guard.py"
`)
	steps := []Managed{
		{Command: "tentaqles", DeveloperInstructions: "Reply in English."},
		{Command: "/opt/tq", DeveloperInstructions: "Reply in German."},
		{Command: "tentaqles"},
	}
	for _, m := range steps {
		changed, err := Render(home, m)
		if err != nil {
			t.Fatal(err)
		}
		if !changed {
			t.Errorf("Render(%q) changed=false, want true", m.Command)
		}
		root := readRoot(t, home)
		checkOwned(t, root, m.Command)
		userHooks := ownedCommands(t, root, "PreToolUse")
		if len(userHooks) != 1 {
			t.Errorf("after Render(%q): owned PreToolUse hooks = %q, want exactly one", m.Command, userHooks)
		}
		groups, _ := groupsOf(mustHooks(t, root)["PreToolUse"])
		if len(groups) != 2 {
			t.Fatalf("after Render(%q): %d PreToolUse groups, want 2 (user + ours)", m.Command, len(groups))
		}
		hs, _ := handlersOf(groups[0])
		if hs[0]["command"] != "python3 /home/me/guard.py" {
			t.Errorf("user hook moved or changed: %v", groups[0])
		}
	}
}

func mustHooks(t *testing.T, root map[string]any) map[string]any {
	t.Helper()
	h, err := hooksTable(root)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestRenderDeveloperInstructionsOwnership(t *testing.T) {
	cases := []struct {
		name    string
		renders []string // developer_instructions values rendered before the user's edit
		user    *string  // a value the user writes over ours before the final render
		final   string   // developer_instructions for the final render
		want    string   // developer_instructions after the final render; "" means absent
		owned   bool     // whether tentaqles should own the value afterwards
	}{
		{"absent, set", nil, nil, "A", "A", true},
		{"absent, empty stays absent", nil, nil, "", "", false},
		{"owned, changed", []string{"A"}, nil, "B", "B", true},
		{"owned, removed", []string{"A"}, nil, "", "", false},
		{"owned, same value", []string{"A"}, nil, "A", "A", true},
		{"user value, set over", nil, ptr("U"), "A", "U", false},
		{"user value, removal does not delete it", nil, ptr("U"), "", "U", false},
		{"user edited ours, set over", []string{"A"}, ptr("U"), "B", "U", false},
		{"user edited ours, removal keeps it", []string{"A"}, ptr("U"), "", "U", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			for _, di := range tc.renders {
				if _, err := Render(home, Managed{Command: testCommand, DeveloperInstructions: di}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.user != nil {
				editConfig(t, home, func(root map[string]any) { root[devInstrKey] = *tc.user })
			}
			if _, err := Render(home, Managed{Command: testCommand, DeveloperInstructions: tc.final}); err != nil {
				t.Fatal(err)
			}
			root := readRoot(t, home)
			got, _ := root[devInstrKey].(string)
			if got != tc.want {
				t.Errorf("developer_instructions = %q, want %q", got, tc.want)
			}
			if _, has := root[devInstrKey]; has != (tc.want != "") {
				t.Errorf("developer_instructions present = %v, want %v", has, tc.want != "")
			}
			_, err := os.Stat(filepath.Join(home, ownedFile))
			if tc.owned != (err == nil) {
				t.Errorf("%s present = %v, want %v", ownedFile, err == nil, tc.owned)
			}
		})
	}
}

func TestRenderRefusals(t *testing.T) {
	cases := []struct {
		name string
		m    Managed
		body *string // config written first; nil means none
		want string  // substring of the error
	}{
		{"empty command", Managed{Command: "  "}, nil, "Command is empty"},
		{"malformed config is not overwritten", Managed{Command: testCommand}, ptr("model = \n"), "config.toml"},
		{"hooks is not a table", Managed{Command: testCommand}, ptr("hooks = \"nope\"\n"), "hooks"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.body != nil {
				writeConfig(t, home, *tc.body)
			}
			_, err := Render(home, tc.m)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Render error = %v, want it to mention %q", err, tc.want)
			}
			if tc.body != nil {
				data, _ := os.ReadFile(filepath.Join(home, configFile))
				if string(data) != *tc.body {
					t.Errorf("config.toml was changed on refusal: %q", data)
				}
			}
		})
	}
}

func TestHookTrust(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, home string)
		want  Trust
	}{
		{"no config file", func(t *testing.T, home string) {}, Missing},
		{"config without tentaqles hooks", func(t *testing.T, home string) {
			writeConfig(t, home, "model = \"gpt-5\"\n")
		}, Missing},
		{"only PreToolUse is rendered", func(t *testing.T, home string) {
			mustRender(t, home, testCommand)
			editConfig(t, home, func(root map[string]any) { delete(mustHooks(t, root), "SessionStart") })
		}, Missing},
		{"rendered and never trusted", func(t *testing.T, home string) {
			mustRender(t, home, testCommand)
		}, Untrusted},
		{"only PreToolUse trusted", func(t *testing.T, home string) {
			mustRender(t, home, testCommand)
			trustAll(t, home)
			dropState(t, home, ":session_start:")
		}, Untrusted},
		{"both trusted", func(t *testing.T, home string) {
			mustRender(t, home, testCommand)
			trustAll(t, home)
		}, Trusted},
		{"trusted hash is not the current one", func(t *testing.T, home string) {
			mustRender(t, home, testCommand)
			trustAll(t, home)
			trustKeys(t, home, map[string]string{
				ownedKeyKey(t, home, "session_start"): "sha256:" + strings.Repeat("0", 64),
			})
		}, Modified},
		{"command changed after trust", func(t *testing.T, home string) {
			mustRender(t, home, testCommand)
			trustAll(t, home)
			mustRender(t, home, "/opt/tq")
		}, Modified},
		{"developer_instructions changed after trust", func(t *testing.T, home string) {
			mustRenderDI(t, home, "A")
			trustAll(t, home)
			mustRenderDI(t, home, "B")
		}, Trusted},
		{"user group appended after ours", func(t *testing.T, home string) {
			mustRender(t, home, testCommand)
			trustAll(t, home)
			editConfig(t, home, func(root map[string]any) {
				hooks := mustHooks(t, root)
				groups, _ := groupsOf(hooks["PreToolUse"])
				hooks["PreToolUse"] = append(groups, userGroup())
			})
		}, Trusted},
		{"user group inserted before ours shifts the index", func(t *testing.T, home string) {
			mustRender(t, home, testCommand)
			trustAll(t, home)
			editConfig(t, home, func(root map[string]any) {
				hooks := mustHooks(t, root)
				groups, _ := groupsOf(hooks["PreToolUse"])
				hooks["PreToolUse"] = append([]map[string]any{userGroup()}, groups...)
			})
		}, Untrusted},
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

// TestHookTrustGoldenHashes checks HookTrust against the hashes Codex 0.154.0 reported
// (see the constants above), without computing them with this package's hash.
func TestHookTrustGoldenHashes(t *testing.T) {
	home := t.TempDir()
	mustRender(t, home, testCommand)
	abs, err := filepath.Abs(filepath.Join(home, configFile))
	if err != nil {
		t.Fatal(err)
	}
	trustKeys(t, home, map[string]string{
		hookKey(abs, "pre_tool_use", 0, 0):  goldenPreToolUse,
		hookKey(abs, "session_start", 0, 0): goldenSessionStart,
	})
	if got, err := HookTrust(home); err != nil || got != Trusted {
		t.Fatalf("HookTrust with Codex's own hashes = %v, %v; want Trusted", got, err)
	}
}

func TestHookHashMatchesCodex(t *testing.T) {
	// The rendered hooks.
	home := t.TempDir()
	mustRender(t, home, testCommand)
	assertHashes(t, readRoot(t, home), map[string]string{
		"pre_tool_use:0:0":  goldenPreToolUse,
		"session_start:0:0": goldenSessionStart,
	})

	// The normalization probe. The commandWindows entry is hashed over the Windows
	// command only on Windows, so it is checked there alone.
	want := map[string]string{
		"pre_tool_use:0:0":  goldenProbePre0,
		"session_start:0:0": goldenProbeSession0,
		"session_start:1:0": goldenProbeSession1,
	}
	if runtime.GOOS == "windows" {
		want["pre_tool_use:1:0"] = goldenProbePre1Win
	}
	var root map[string]any
	if err := toml.Unmarshal([]byte(codexProbeConfig), &root); err != nil {
		t.Fatal(err)
	}
	assertHashes(t, root, want)
}

// assertHashes computes the hash of each handler named in want (label:group:handler keys)
// and compares it with the expected value.
func assertHashes(t *testing.T, root map[string]any, want map[string]string) {
	t.Helper()
	hooks, _ := hooksTable(root)
	got := map[string]string{}
	for _, ev := range hookEvents {
		groups, _ := groupsOf(hooks[ev.table])
		for gi, g := range groups {
			hs, _ := handlersOf(g)
			for hi, h := range hs {
				key := ev.label + ":" + strconv.Itoa(gi) + ":" + strconv.Itoa(hi)
				got[key] = hookHash(ev.label, g, h)
			}
		}
	}
	for key, w := range want {
		if got[key] != w {
			t.Errorf("hash of %s = %s, want %s", key, got[key], w)
		}
	}
}

func TestHookKeyFormat(t *testing.T) {
	got := hookKey(codexProbePath, "pre_tool_use", 0, 0)
	want := codexProbePath + ":pre_tool_use:0:0"
	if got != want {
		t.Errorf("hookKey = %q, want %q", got, want)
	}
	if got := hookKey(codexProbePath, "session_start", 1, 0); got != codexProbePath+":session_start:1:0" {
		t.Errorf("hookKey session_start = %q", got)
	}
}

func TestTrustString(t *testing.T) {
	cases := []struct {
		t    Trust
		want string
	}{
		{Missing, "missing"},
		{Untrusted, "untrusted"},
		{Modified, "modified"},
		{Trusted, "trusted"},
		{Trust(9), "Trust(9)"},
	}
	for _, tc := range cases {
		if got := tc.t.String(); got != tc.want {
			t.Errorf("Trust(%d).String() = %q, want %q", int(tc.t), got, tc.want)
		}
	}
}

func mustRender(t *testing.T, home, command string) {
	t.Helper()
	if _, err := Render(home, Managed{Command: command}); err != nil {
		t.Fatal(err)
	}
}

func mustRenderDI(t *testing.T, home, di string) {
	t.Helper()
	if _, err := Render(home, Managed{Command: testCommand, DeveloperInstructions: di}); err != nil {
		t.Fatal(err)
	}
}

// ownedKeyKey returns the key of the tentaqles handler for event label in home.
func ownedKeyKey(t *testing.T, home, label string) string {
	t.Helper()
	for key := range ownedKeyHashes(t, home) {
		if strings.Contains(key, ":"+label+":") {
			return key
		}
	}
	t.Fatalf("no tentaqles %s hook in %s", label, home)
	return ""
}

// TestHookTrustThroughLink: Codex keys trust on the resolved CODEX_HOME, so a link to the
// home sees the same trust as the home itself.
func TestHookTrustThroughLink(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	link := filepath.Join(base, "link")
	mustRender(t, real, testCommand)
	if err := makeLink(real, link); err != nil {
		t.Skipf("cannot make a link here: %v", err)
	}
	trustAll(t, real)
	if got, err := HookTrust(link); err != nil || got != Trusted {
		t.Errorf("HookTrust through the link = %v, %v; want Trusted", got, err)
	}
	if got, err := HookTrust(real); err != nil || got != Trusted {
		t.Errorf("HookTrust of the real home = %v, %v; want Trusted", got, err)
	}
}

// makeLink links link to the folder target: a junction on Windows (no privilege needed),
// a symlink elsewhere.
func makeLink(target, link string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "mklink", "/J", link, target).Run()
	}
	return os.Symlink(target, link)
}

func userGroup() map[string]any {
	return map[string]any{
		"matcher": "Write",
		"hooks":   []map[string]any{{"type": "command", "command": "python3 /home/me/guard.py"}},
	}
}
