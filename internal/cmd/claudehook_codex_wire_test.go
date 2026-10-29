package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// The hook passes the payload's permission mode and workspace to
// codexAnswer: a covered ask is left to Codex's own prompt rule only in a
// prompting permission mode, and bypass still denies.
func TestPreToolUseAgentCodexPassesPermissionMode(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	mp := filepath.Join(root, manifest.FileName)
	if err := os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: acme\nidentities: { gh: {}, codex: {} }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := trust.Allow(manifestHash(t, root)); err != nil {
		t.Fatal(err)
	}
	t.Setenv(guardcfg.EnvMode, "")
	t.Setenv("TENTAQLES_HEADLESS", "")
	w := wsFor(t, root)
	home := paths.IdentityDir(w.Name, "codex")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	renderOut(t, w, false)
	t.Setenv("CODEX_HOME", home)
	prev := codexPromptOS
	codexPromptOS = "linux"
	t.Cleanup(func() { codexPromptOS = prev })
	args := []string{"claude-hook", "pre-tool-use", "--agent", "codex"}

	code, out, _ := runHook(t, args, modePayload(t, w.Root, "terraform apply", "default"))
	if dec, _ := codexAnswerJSON(t, out); code != 0 || dec != "" {
		t.Fatalf("covered ask in default mode: code %d out %q", code, out)
	}
	code, out, _ = runHook(t, args, modePayload(t, w.Root, "terraform apply", "bypassPermissions"))
	if dec, _ := codexAnswerJSON(t, out); code != 0 || dec != "deny" {
		t.Fatalf("bypass session: code %d out %q", code, out)
	}
}

// Under --agent codex a guard panic is a Codex deny: Codex lets an ask run.
func TestPreToolUseAgentCodexPanicDenies(t *testing.T) {
	isolateHome(t)
	prev := hookPolicyDecision
	hookPolicyDecision = func(string, *resolve.Workspace, *registry.Config, policy.ToolCall, time.Time, *modeGate) policy.Decision {
		panic("boom")
	}
	t.Cleanup(func() { hookPolicyDecision = prev })
	dir := testutil.TempDir(t)
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, hookPayloadFor(t, dir, "ls"))
	dec, reason := codexAnswerJSON(t, out)
	if code != 0 || dec != "deny" || !strings.Contains(reason, "tq/guard-error") {
		t.Fatalf("code %d out %q", code, out)
	}
}

// Under --agent codex the identity guard's block also writes a Codex deny.
func TestPreToolUseAgentCodexIdentityGuardDenies(t *testing.T) {
	_, proj := modeWS(t, "blocked_commands: [\"npm publish\"]\n", "")
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, modePayload(t, proj, "npm publish", "default"))
	dec, reason := codexAnswerJSON(t, out)
	if code != 2 || dec != "deny" || !strings.Contains(reason, "blocked by client manifest") || !strings.Contains(errOut, "blocked by client manifest") {
		t.Fatalf("code %d out %q stderr %q", code, out, errOut)
	}
}

func applyPatchPayload(t *testing.T, cwd, patch string) string {
	t.Helper()
	return `{"tool_name":"apply_patch","cwd":` + jsonPath(t, cwd) + `,"tool_input":{"command":` + jsonPath(t, patch) + `}}`
}

// Codex sends apply_patch with the patch in tool_input.command: the hook
// runs the file and path rules on every file the patch names, and denies a
// patch it cannot read.
func TestPreToolUseApplyPatchRunsFileRules(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	deny := map[string]string{
		"settings widen (update)":        "*** Begin Patch\n*** Update File: .claude/settings.json\n@@\n-  \"x\": 1\n+  \"defaultMode\": \"bypassPermissions\"\n*** End Patch",
		"trust store (add, second file)": "*** Begin Patch\n*** Add File: notes.md\n+hello\n*** Add File: ~/.tentaqles/trust/abc.bypass\n+x\n*** End Patch",
		"registry (delete)":              "*** Begin Patch\n*** Delete File: ~/.tentaqles/config.yaml\n*** End Patch",
		"move into the trust store":      "*** Begin Patch\n*** Update File: notes.md\n*** Move to: ~/.tentaqles/trust/abc\n@@\n-a\n+b\n*** End Patch",
		"no file header":                 "*** Begin Patch\n*** End Patch",
		"not a patch":                    "rm -rf ~/.tentaqles/trust",
		"unknown header":                 "*** Begin Patch\n*** Frobnicate File: x\n*** End Patch",
		"empty path":                     "*** Begin Patch\n*** Add File: \n+x\n*** End Patch",
	}
	for name, patch := range deny {
		t.Run(name, func(t *testing.T) {
			code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, applyPatchPayload(t, root, patch))
			if code != 2 {
				t.Fatalf("claude: want a block, code %d stderr %q", code, errOut)
			}
			code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, applyPatchPayload(t, root, patch))
			if dec, _ := codexAnswerJSON(t, out); dec != "deny" {
				t.Fatalf("codex: want deny, code %d out %q", code, out)
			}
		})
	}
	ok := "*** Begin Patch\n*** Add File: notes.md\n+hello\n*** Update File: src/main.go\n@@\n-a\n+b\n*** End Patch\n"
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, applyPatchPayload(t, root, ok))
	if dec, _ := codexAnswerJSON(t, out); code != 0 || dec != "" {
		t.Fatalf("a plain patch is allowed: code %d out %q stderr %q", code, out, errOut)
	}
}

func TestParseApplyPatch(t *testing.T) {
	files, err := parseApplyPatch("*** Begin Patch\n*** Add File: a.txt\n+one\n+two\n*** Update File: b.go\n*** Move to: c.go\n@@ func x\n-old\n+new\n context\n*** End of File\n*** Delete File: d.txt\n*** End Patch\n")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, f := range files {
		got = append(got, f.Tool+":"+f.Path+"="+f.Content)
	}
	want := []string{"Write:a.txt=one\ntwo", "Edit:b.go=new", "Write:c.go=new", "Write:d.txt="}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
	for _, bad := range []string{"", "x", "*** Begin Patch\n*** End Patch", "*** Begin Patch\n*** Add File: a\n+x", "*** Begin Patch\n*** Move to: a\n*** End Patch"} {
		if _, err := parseApplyPatch(bad); err == nil {
			t.Errorf("%q must not parse", bad)
		}
	}
}

// Codex sends apply_patch paths relative to the session cwd. They are
// resolved against it and cleaned before the rules run, so a ../ escape
// into a system folder, or a relative write from inside an identity dir,
// is judged at its real place (final security review M4).
func TestPreToolUseApplyPatchResolvesRelativePaths(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	up := strings.Repeat("../", 30)
	sys := up + "etc/hosts"
	if runtime.GOOS == "windows" {
		sys = up + "Windows/System32/drivers/etc/hosts"
	}
	codexDir := filepath.Join(t.TempDir(), ".tentaqles", "identities", "acme", "codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, cwd, patch string }{
		{"system folder via ../", root, "*** Begin Patch\n*** Add File: " + sys + "\n+127.0.0.1 x\n*** End Patch"},
		{"rules from the identity dir", codexDir, "*** Begin Patch\n*** Update File: rules/tq.rules\n@@\n-a\n+b\n*** End Patch"},
		{"config.toml from the identity dir", codexDir, "*** Begin Patch\n*** Update File: ./config.toml\n@@\n-a\n+b\n*** End Patch"},
		{"trust store via a messy path", root, "*** Begin Patch\n*** Add File: x/../" + up + "home/u/.tentaqles/./trust/abc\n+x\n*** End Patch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, applyPatchPayload(t, tc.cwd, tc.patch))
			if dec, _ := codexAnswerJSON(t, out); dec != "deny" {
				t.Fatalf("want deny: code %d out %q stderr %q", code, out, errOut)
			}
		})
	}
}

// A patch with more files than tq judges in one call is denied, fast: the
// hook never runs past its budget, which would fail open (M5).
func TestPreToolUseApplyPatchCapsTheFileCount(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	var b strings.Builder
	b.WriteString("*** Begin Patch\n")
	for i := 0; i < 5000; i++ {
		b.WriteString("*** Add File: f" + strconv.Itoa(i) + ".txt\n+x\n")
	}
	b.WriteString("*** End Patch")
	start := time.Now()
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, applyPatchPayload(t, root, b.String()))
	dec, reason := codexAnswerJSON(t, out)
	if dec != "deny" || !strings.Contains(reason, "tq/guard-error") || !strings.Contains(reason, "split the patch") {
		t.Fatalf("code %d out %q", code, out)
	}
	if el := time.Since(start); el > 10*time.Second {
		t.Fatalf("took %s", el)
	}
	// 256 files are still judged, within the budget.
	b.Reset()
	b.WriteString("*** Begin Patch\n")
	for i := 0; i < maxPatchFiles; i++ {
		b.WriteString("*** Add File: f" + strconv.Itoa(i) + ".txt\n+x\n")
	}
	b.WriteString("*** End Patch")
	start = time.Now()
	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, applyPatchPayload(t, root, b.String()))
	if dec, _ := codexAnswerJSON(t, out); code != 0 || dec != "" {
		t.Fatalf("%d files: code %d out %q", maxPatchFiles, code, out)
	}
	if el := time.Since(start); el > hookScanBudget() {
		t.Fatalf("%d files took %s", maxPatchFiles, el)
	}
}

// Codex payload shapes tq cannot judge fail closed under --agent codex: no
// tool name, and an edit tool with neither a path nor a patch (m6).
func TestPreToolUseAgentCodexUnknownShapesDeny(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	for name, payload := range map[string]string{
		"no tool name":         `{"tool_name":"","cwd":` + jsonPath(t, root) + `,"tool_input":{"command":"ls"}}`,
		"edit with no path":    `{"tool_name":"Edit","cwd":` + jsonPath(t, root) + `,"tool_input":{"patch":"*** Begin Patch"}}`,
		"write with no path":   `{"tool_name":"Write","cwd":` + jsonPath(t, root) + `,"tool_input":{}}`,
		"apply_patch no input": `{"tool_name":"apply_patch","cwd":` + jsonPath(t, root) + `,"tool_input":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, payload)
			if dec, _ := codexAnswerJSON(t, out); dec != "deny" {
				t.Fatalf("code %d out %q", code, out)
			}
		})
	}
}
