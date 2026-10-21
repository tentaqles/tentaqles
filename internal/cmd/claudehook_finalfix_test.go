package cmd

// Hook-level tests for the final-review fix wave (C1-C4, I1-I8).
//
// Every personal or financial value here is a public test number with a
// valid checksum, never a real person or account:
//   - CPF  529.982.247-25           (widely published CPF test number) tq:allow-pii
//   - CNPJ 11.222.333/0001-81       (widely published CNPJ test number) tq:allow-pii
//   - card 4111 1111 1111 1111      (Visa test card) tq:allow-pii
//   - IBAN GB82 WEST 1234 5698 7654 32 (IBAN registry example) tq:allow-pii
// Values that must fail their checksum say so next to them.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

const cpfRow = "Ana;529.982.247-25;rua x\n" // tq:allow-pii

func hook(t *testing.T, ws, cmd string) (int, string) {
	t.Helper()
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, cmd))
	return code, out + errOut
}

func wantDeny(t *testing.T, ws, cmd, rule string) {
	t.Helper()
	if code, out := hook(t, ws, cmd); code != 2 || !strings.Contains(out, rule) || strings.Contains(out, "529") {
		t.Errorf("%s: want deny %s, got code=%d %q", cmd, rule, code, out)
	}
}

func wantAsk(t *testing.T, ws, cmd, rule, sub string) {
	t.Helper()
	code, out := hook(t, ws, cmd)
	if code == 2 || !strings.Contains(out, `"ask"`) || !strings.Contains(out, rule) || !strings.Contains(out, sub) {
		t.Errorf("%s: want ask %s %q, got code=%d %q", cmd, rule, sub, code, out)
	}
}

// C1: commit and push are scanned in the repository the command targets.
func TestFinal_C1_ScansTargetRepository(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	gitRepo(t, ws) // the hook cwd: a clean repository
	wt := filepath.Join(ws, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	runWt := gitRepo(t, wt)
	stage(t, wt, runWt, "seed.sql", cpfRow)
	for _, c := range []string{
		`git -C wt commit -m "feat: seed"`,
		`cd wt && git commit -m "feat: seed"`,
		`cd "` + wt + `" && git commit -m "feat: seed"`,
		`cd ` + filepath.ToSlash(wt) + `; git commit -m "feat: seed"`,
	} {
		wantDeny(t, ws, c, "tq/commit-pii")
	}
	wantAsk(t, ws, `git -C missing commit -m "feat: seed"`, "tq/commit-pii", "does not exist")
	wantAsk(t, ws, `cd $WT && git commit -m "feat: seed"`, "tq/commit-pii", "cannot evaluate")

	// commit-secret follows the same directory
	runWt("reset", "-q")
	stage(t, wt, runWt, "cfg.py", "API"+"_KEY="+strings.Repeat("q7", 10)+"\n")
	wantDeny(t, ws, `cd wt && git commit -m "feat: cfg"`, "tq/commit-secret")
}

// C2: `git commit <pathspec>` scans the named paths' working tree.
func TestFinal_C2_CommitPathspec(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	stage(t, ws, run, "export.csv", "nome;cpf\n")
	run("-c", noHooks(t), "commit", "-q", "-m", "chore: track")
	if err := os.WriteFile(filepath.Join(ws, "export.csv"), []byte("nome;cpf\n"+cpfRow), 0o600); err != nil {
		t.Fatal(err)
	}
	wantDeny(t, ws, `git commit -m "feat: x" export.csv`, "tq/commit-pii")
	wantDeny(t, ws, `git commit -m "feat: x" -- export.csv`, "tq/commit-pii")
	wantDeny(t, ws, `git commit -m "feat(api): x" --include export.csv`, "tq/commit-pii")
}

// C3: command-word forms reach every commit rule.
func TestFinal_C3_CommandWordForms(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	stage(t, ws, run, "seed.sql", cpfRow)
	for _, c := range []string{
		`GIT_AUTHOR_DATE=now git commit -m "feat: seed"`,
		`"git" commit -m "feat: seed"`,
		`git.exe commit -m "feat: seed"`,
		`/usr/bin/git commit -m "feat: seed"`,
		`command git commit -m "feat: seed"`,
		`env A=1 git commit -m "feat: seed"`,
	} {
		wantDeny(t, ws, c, "tq/commit-pii")
	}
	wantAsk(t, ws, `git --git-dir .git commit -m "feat: seed"`, "tq/commit-pii", "--git-dir")
}

// C4: large and UTF-16 content is never skipped silently.
func TestFinal_C4_LargeAndUTF16(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	big := strings.Repeat(cpfRow, (1<<20)/len(cpfRow)+100)
	if err := os.WriteFile(filepath.Join(ws, "export.csv"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	wantAsk(t, ws, `git add -A && git commit -m "feat: export"`, "tq/commit-pii", "export.csv is larger than the 1 MiB scan limit")
	_ = os.Remove(filepath.Join(ws, "export.csv"))

	u := utf16.Encode([]rune("nome;cpf\r\n" + cpfRow))
	b := []byte{0xFF, 0xFE}
	for _, r := range u {
		b = append(b, byte(r), byte(r>>8))
	}
	if err := os.WriteFile(filepath.Join(ws, "le.csv"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "le.csv")
	wantDeny(t, ws, `git commit -m "feat: export"`, "tq/commit-pii")
}

// I1: one budget for the whole decision, under the plugin's 15 s timeout.
func TestFinal_I1_SharedBudget(t *testing.T) {
	if hookScanBudget() > 10*time.Second || hookScanBudget() <= 0 {
		t.Fatalf("default budget %v must stay well under the 15 s hook timeout", hookScanBudget())
	}
	t.Setenv("TQ_HOOK_SCAN_BUDGET", "3s")
	if hookScanBudget() != 3*time.Second {
		t.Fatalf("env override: %v", hookScanBudget())
	}
	t.Setenv("TQ_HOOK_SCAN_BUDGET", "60s")
	if hookScanBudget() > 12*time.Second {
		t.Fatalf("an override must be clamped under the hook timeout: %v", hookScanBudget())
	}
	t.Setenv("TQ_HOOK_SCAN_BUDGET", "")

	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	withRemote(t, run)
	origGit, origBudget := pushGit, scanBudget
	defer func() { pushGit, scanBudget = origGit, origBudget }()
	scanBudget = 300 * time.Millisecond
	calls := 0
	pushGit = func(ctx context.Context, cwd string, limit int, args ...string) (string, bool, error) {
		calls++
		<-ctx.Done()
		return "", false, ctx.Err()
	}
	start := time.Now()
	code, out := hook(t, ws, `git commit -m "feat: x" && git push`)
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("commit+push took %v with a 300ms budget: the budget is not shared", d)
	}
	if code == 2 || !strings.Contains(out, "scan did not finish in time") {
		t.Fatalf("code=%d %q", code, out)
	}
	if !strings.Contains(out, "tq/commit-secret") && !strings.Contains(out, "tq/commit-pii") {
		t.Fatalf("the commit rules must ask on the deadline: %q", out)
	}
}

func TestFinal_I1_OversizedDiffAsks(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	line := strings.Repeat("x", 99) + "\n"
	stage(t, ws, run, "big.txt", strings.Repeat(line, (9<<20)/len(line)))
	wantAsk(t, ws, `git commit -m "feat: big"`, "tq/commit-pii", "scan limit")
}

func bareRemote(t *testing.T, run func(args ...string) string) {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "main", remote).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	run("remote", "add", "origin", remote)
}

// C1: push is scanned in the repository it targets.
func TestFinal_C1_PushTargetRepository(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	withRemote(t, gitRepo(t, ws)) // the hook cwd: nothing unpushed
	wt := filepath.Join(ws, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	runWt := gitRepo(t, wt)
	withRemote(t, runWt)
	stage(t, wt, runWt, "export.csv", cpfRow)
	runWt("-c", noHooks(t), "commit", "-q", "-m", "feat: export")
	wantDeny(t, ws, `git -C wt push`, "tq/push-pii")
	wantDeny(t, ws, `cd wt && git push origin main`, "tq/push-pii")
	wantAsk(t, ws, `git -C missing push`, "tq/push-pii", "does not exist")
}

// I2: a first push (no upstream, no origin/HEAD) scans every commit that is
// on no remote.
func TestFinal_I2_FirstPushScansUnpushedHistory(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	bareRemote(t, run)
	stage(t, ws, run, "export.csv", cpfRow)
	run("-c", noHooks(t), "commit", "-q", "-m", "feat: export")
	for _, c := range []string{`git push -u origin main`, `git push origin HEAD`, `cd . && git push`, `git push`} {
		wantDeny(t, ws, c, "tq/push-pii")
	}
	wantAsk(t, ws, `git push origin nosuchbranch`, "tq/push-pii", "could not scan")
}

func psPayload(t *testing.T, cwd, command string) string {
	t.Helper()
	return `{"tool_name":"PowerShell","cwd":` + jsonPath(t, cwd) + `,"tool_input":{"command":` + jsonPath(t, command) + `}}`
}

// I6: absolute, ./-relative and case-variant source paths, and PowerShell writers.
func TestFinal_I6_FixtureRulePathsAndPowerShell(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	if err := os.MkdirAll(filepath.Join(ws, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(ws, "data", "exports", "q3.csv")
	deny := []string{
		`cp ` + filepath.ToSlash(abs) + ` tests/fixtures/q3.csv`,
		`cp "` + abs + `" tests/fixtures/q3.csv`,
		`cp ./data/exports/q3.csv tests/fixtures/q3.csv`,
		`cp Data/Exports/q3.csv tests/fixtures/`,
		`cd sub && cp ../data/exports/q3.csv ../tests/fixtures/`,
		`Get-Content data/exports/q3.csv | Set-Content tests/fixtures/q3.csv`,
		`Get-Content data/exports/q3.csv | Set-Content -Path tests/fixtures/q3.csv`,
		`Get-Content ./data/exports/q3.csv | Add-Content -Path tests/q3.csv`,
		`Get-Content data\exports\q3.csv | Out-File -FilePath tests\fixtures\q3.csv`,
		`Get-Content data/exports/q3.csv | Out-File tests/fixtures/q3.csv`,
		`Import-Csv data/exports/q3.csv | Export-Csv tests/fixtures/q3.csv`,
		`Import-Csv data/exports/q3.csv | Export-Csv -Path tests/fixtures/q3.csv -NoTypeInformation`,
		`Copy-Item data/exports/q3.csv tests/fixtures/q3.csv`,
		`Copy-Item -Path data/exports/q3.csv -Destination tests/fixtures/`,
		`Copy-Item -Destination tests/fixtures/ -Path data/exports/q3.csv`,
		`copy-item data/exports/q3.csv -dest seeds/`,
		`Move-Item data/exports/q3.csv tests/fixtures/q3.csv`,
		`Get-Content data/exports/q3.csv | Tee-Object -FilePath tests/fixtures/q3.csv`,
	}
	if runtime.GOOS == "windows" {
		vol := strings.ToLower(filepath.VolumeName(abs)[:1])
		deny = append(deny, `cp /`+vol+filepath.ToSlash(abs[len(filepath.VolumeName(abs)):])+` tests/fixtures/q3.csv`)
	}
	for _, c := range deny {
		code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, psPayload(t, ws, c))
		if code != 2 || !strings.Contains(out+errOut, "tq/pii-fixture-dump") {
			t.Errorf("%s: code=%d %q", c, code, out+errOut)
		}
	}
	for _, c := range []string{
		`Copy-Item data/exports/q3.csv backups/q3.csv`,
		`Set-Content tests/fixtures/x.csv -Value "synthetic"`,
		`Get-Content data/exports/q3.csv`,
		`Copy-Item tests/fixtures/a.csv data/exports/`,
	} {
		code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, psPayload(t, ws, c))
		if code != 0 || strings.Contains(out+errOut, "pii") {
			t.Errorf("%s: wrongly flagged (code=%d): %q", c, code, out+errOut)
		}
	}
}

// I7: gh api text, @file fields, --input, and substituted bodies.
func TestFinal_I7_TextPIIGhAPIAndSubstitution(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	gitRepo(t, ws)
	for name, body := range map[string]string{"body.md": "cliente 529.982.247-25\n", "body.json": `{"body":"cpf 529.982.247-25"}`} { // tq:allow-pii
		if err := os.WriteFile(filepath.Join(ws, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []string{
		`gh api repos/o/r/pulls/1/comments/2/replies -f body="cliente 529.982.247-25"`, // tq:allow-pii
		`gh api repos/o/r/issues/1/comments --raw-field body="cliente 529.982.247-25"`, // tq:allow-pii
		`gh api repos/o/r/pulls/1/comments/2/replies -F body=@body.md`,
		`gh api repos/o/r/pulls/1/comments/2/replies --field=body=@body.md`,
		`gh api repos/o/r/issues/1/comments --input body.json`,
		`gh api -X POST repos/o/r/issues/1/comments --input=body.json`,
	} {
		wantDeny(t, ws, c, "tq/text-pii")
	}
	for c, want := range map[string]string{
		`gh pr create --title x --body "$(cat body.md)"`:      "command substitution",
		"gh pr create --title x --body \"`cat body.md`\"":     "command substitution",
		`gh issue comment 1 --body "$(cat body.md)"`:          "command substitution",
		`git commit -m "$(cat msg.txt)"`:                      "command substitution",
		`gh api repos/o/r/issues/1/comments -F body=@-`:       "stdin",
		`gh api repos/o/r/issues/1/comments --input -`:        "stdin",
		`gh api repos/o/r/issues/1/comments -F body=@nope.md`: "could not read",
	} {
		wantAsk(t, ws, c, "tq/text-pii", want)
	}
	for _, c := range []string{
		`gh api repos/o/r`,
		`gh api repos/o/r/issues -f title="login fails"`,
		"git commit -m \"$(cat <<'EOF'\nfix: handle `nil` input\n\nCo-Authored-By: X <x@y.z>\nEOF\n)\"",
		`gh pr create --title x --body 'literal $(not run) and ` + "`" + `code` + "`" + `'`,
	} {
		if code, out := hook(t, ws, c); strings.Contains(out, "tq/text-pii") {
			t.Errorf("%s: wrongly flagged (code=%d): %q", c, code, out)
		}
	}
	// PowerShell: the backtick is an escape, not a substitution
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, psPayload(t, ws, "gh pr create --title x --body \"line`nnext\""))
	if strings.Contains(out+errOut, "tq/text-pii") {
		t.Errorf("PowerShell backtick flagged (code=%d): %q", code, out+errOut)
	}
}

// I8: the marker also counts in test files, never in docs or migrations.
func TestFinal_I8_MarkerScope(t *testing.T) {
	for f, want := range map[string]bool{
		"internal/redact/redact_test.go":      true,
		"internal/cmd/claudehook_pii_test.go": true,
		"pkg/test_export.py":                  true,
		"pkg/export_test.py":                  true,
		"src/app.test.ts":                     true,
		"src/app.spec.js":                     true,
		"conftest.py":                         true,
		"tests/fixtures/people.sql":           true,
		"seeds/a.sql":                         true,
		"src/seed.sql":                        false,
		"src/export.go":                       false,
		"docs/example_test.go":                false,
		"docs/tests/people.sql":               false,
		"db/migrations/0001_test.py":          false,
		"migrations/conftest.py":              false,
		"notes/a.test.md":                     false,
		"README.md":                           false,
		"latest.py":                           false,
		"contest.py":                          false,
	} {
		if got := markerAllowed(f); got != want {
			t.Errorf("markerAllowed(%q) = %v, want %v", f, got, want)
		}
	}
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	stage(t, ws, run, "internal/redact/x_test.go", "package redact\n\nconst cpf = \"529.982.247-25\" // tq:allow-pii\n") // tq:allow-pii
	if code, out := hook(t, ws, `git commit -m "test: fixture"`); code != 0 || strings.Contains(out, "tq/commit-pii") {
		t.Fatalf("marked line in a Go test file: code=%d %q", code, out)
	}
	run("reset", "-q")
	stage(t, ws, run, "docs/x_test.go", "const cpf = \"529.982.247-25\" // tq:allow-pii\n") // tq:allow-pii
	if code, out := hook(t, ws, `git commit -m "docs: x"`); code != 2 || !strings.Contains(out, "test files") {
		t.Fatalf("marker in docs must not count: code=%d %q", code, out)
	}
}
