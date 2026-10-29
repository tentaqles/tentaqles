package cmd

// Round-2 hook tests (final-rereview N1-N3 and the stall-margin minors).
// Test values: CPF 529.982.247-25 is the widely published CPF test number
// (valid check digits, no real person). tq:allow-pii

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/tentaqles/tentaqles/internal/policy"
)

func hookIn(t *testing.T, cwd, payload string) (int, string) {
	t.Helper()
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, payload)
	return code, out + errOut
}

func notAllowed(code int, out, rule string) bool {
	return strings.Contains(out, rule) && (code == 2 || strings.Contains(out, `"ask"`))
}

func utf16LE(s string) []byte {
	b := []byte{0xFF, 0xFE}
	for _, r := range utf16.Encode([]rune(s)) {
		b = append(b, byte(r), byte(r>>8))
	}
	return b
}

// N1: untracked files are listed repo-wide, and plans are keyed by repository.
func TestRound2_N1_UntrackedRepoWide(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	stage(t, ws, run, "docs/readme.md", "docs\n")
	run("-c", noHooks(t), "commit", "-q", "-m", "docs: readme")
	if err := os.WriteFile(filepath.Join(ws, "export.csv"), []byte(cpfRow), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{
		`git add -A && git commit -m "feat: x"`,
		`cd docs && git add -A && git commit -m "feat: x"`,
		`git -C docs add -A && git -C docs commit -m "feat: x"`,
		`git add -A && git -C docs commit -m "feat: x"`,
		`cd docs && git add .. && git commit -m "feat: x"`,
		`cd docs && git add :/ && git commit -m "feat: x"`,
		`cd docs && git add ../export.csv && git commit -m "feat: x"`,
	} {
		if code, out := hookIn(t, ws, hookPayloadFor(t, ws, c)); !notAllowed(code, out, "tq/commit-pii") || strings.Contains(out, "529") {
			t.Errorf("%s: code=%d %q", c, code, out)
		}
	}
	docs := filepath.Join(ws, "docs")
	if code, out := hookIn(t, docs, hookPayloadFor(t, docs, `git add -A && git commit -m "feat: x"`)); !notAllowed(code, out, "tq/commit-pii") {
		t.Errorf("hook cwd docs/: code=%d %q", code, out)
	}
	// `git add .` in a subdirectory stays scoped to it
	if code, out := hookIn(t, ws, hookPayloadFor(t, ws, `cd docs && git add . && git commit -m "feat: x"`)); strings.Contains(out, "tq/commit-pii") {
		t.Errorf("add . in docs/ must not see the root file: code=%d %q", code, out)
	}
}

// N2: wrappers and nested shells no longer hide git.
func TestRound2_N2_WrappersAndNestedShells(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	stage(t, ws, run, "seed.sql", cpfRow)
	bash := []string{
		`time -p git commit -m "feat: seed"`,
		`command -p git commit -m "feat: seed"`,
		`exec -a x git commit -m "feat: seed"`,
		`timeout 30 git commit -m "feat: seed"`,
		`timeout -k 5 30s git commit -m "feat: seed"`,
		`xargs git commit -m seed < /dev/null`,
		`xargs -0 -n 1 git commit -m seed < /dev/null`,
		`bash -c "git commit -m seed"`,
		`sh -c 'git commit -m "feat: seed"'`,
		`bash -c "git -C wt commit -m seed"`,
		`bash -lc "git commit -m seed"`,
		`echo x | git commit -F -`,
	}
	for _, c := range bash {
		if code, out := hookIn(t, ws, hookPayloadFor(t, ws, c)); !notAllowed(code, out, "tq/commit-pii") {
			t.Errorf("%s: code=%d %q", c, code, out)
		}
	}
	for _, c := range []string{
		`iex "git commit -m seed"`,
		`Invoke-Expression 'git commit -m seed'`,
		`Start-Process git -ArgumentList 'commit','-m','seed' -Wait`,
		`pwsh -Command "git commit -m seed"`,
		`powershell -c "git commit -m seed"`,
	} {
		if code, out := hookIn(t, ws, psPayload(t, ws, c)); !notAllowed(code, out, "tq/commit-pii") {
			t.Errorf("PS %s: code=%d %q", c, code, out)
		}
	}
}

// N3: push decodes UTF-16 blobs that log -p reports as binary.
func TestRound2_N3_PushDecodesUTF16(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	bareRemote(t, run)
	if err := os.WriteFile(filepath.Join(ws, "export.csv"), utf16LE("nome;cpf\r\n"+cpfRow), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "export.csv")
	run("-c", noHooks(t), "commit", "-q", "-m", "feat: export") // outside the hook
	wantDeny(t, ws, `git push -u origin main`, "tq/push-pii")
}

func TestRound2_N3_PushLargeBinaryPasses(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	bareRemote(t, run)
	big := append([]byte{0x89, 'P', 'N', 'G', 0, 0}, []byte(strings.Repeat("x", 2<<20))...)
	if err := os.WriteFile(filepath.Join(ws, "img.png"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "img.png")
	run("-c", noHooks(t), "commit", "-q", "-m", "feat: img")
	if code, out := hook(t, ws, `git push -u origin main`); strings.Contains(out, "tq/push-pii") {
		t.Fatalf("a large real binary must pass: code=%d %q", code, out)
	}
}

// Minor (a): the budget runs from hook entry and leaves a margin.
func TestRound2_BudgetFromEntryAndClamp(t *testing.T) {
	t.Setenv("TQ_HOOK_SCAN_BUDGET", "60s")
	if hookScanBudget() > 10*time.Second {
		t.Fatalf("clamp must leave >= 5 s under the 15 s hook timeout: %v", hookScanBudget())
	}
	t.Setenv("TQ_HOOK_SCAN_BUDGET", "")
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	gitRepo(t, ws)
	call := policy.ToolCall{Tool: "Bash", Command: `git commit -m "feat: x"`}
	ws2, cfg := resolveTrusted(ws)
	d := policyDecision(ws, ws2, cfg, call, time.Now().Add(-time.Second), nil)
	if d.Action != policy.Ask || !strings.Contains(d.Reason(), "scan did not finish in time") {
		t.Fatalf("an exhausted entry budget must ask: %+v %q", d.Action, d.Reason())
	}
}
