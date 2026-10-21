package cmd

// Round-3 hook tests (final-rereview-2 R1-R3).
// Test values: CPF 529.982.247-25 is the widely published CPF test number
// (valid check digits, no real person). tq:allow-pii

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R1: real binaries are sniffed, not read whole, so asset pushes pass.
func TestRound3_R1_PushOfManyBinariesPasses(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	bareRemote(t, run)
	for i := 0; i < 6; i++ {
		b := make([]byte, 1<<20)
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
		b[0], b[1], b[2], b[3], b[4] = 0x89, 'P', 'N', 'G', 0 // PNG-like, a NUL early, no BOM
		if err := os.WriteFile(filepath.Join(ws, fmt.Sprintf("img%d.png", i)), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run("add", ".")
	run("-c", noHooks(t), "commit", "-q", "-m", "feat: assets")
	if code, out := hook(t, ws, `git push -u origin main`); strings.Contains(out, "tq/push-pii") {
		t.Fatalf("6 x 1 MiB binaries must pass: code=%d %q", code, out)
	}
	// a UTF-16 blob with the CPF in the same push is still found
	if err := os.WriteFile(filepath.Join(ws, "export.csv"), utf16LE("nome;cpf\r\n"+cpfRow), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "export.csv")
	run("-c", noHooks(t), "commit", "-q", "-m", "feat: export")
	wantDeny(t, ws, `git push -u origin main`, "tq/push-pii")
}

// R2: the untracked scan covers only what the add calls stage.
func TestRound3_R2_AddScopeNoFalseDeny(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	stage(t, ws, run, "docs/readme.md", "docs\n")
	run("-c", noHooks(t), "commit", "-q", "-m", "docs: readme")
	if err := os.WriteFile(filepath.Join(ws, "export.csv"), []byte(cpfRow), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "docs", "readme.md"), []byte("docs\nmore\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "docs", "new.md"), []byte("clean\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{
		`git add -u && git commit -m "feat: x"`,
		`git add --update && git commit -m "feat: x"`,
		`git add -A docs && git commit -m "feat: x"`,
		`git add --all docs/ && git commit -m "feat: x"`,
		`cd docs && git add -A . && git commit -m "feat: x"`,
		`cd docs && git add *.md && git commit -m "feat: x"`,
		`git add docs/*.md && git commit -m "feat: x"`,
		`git add 'docs/*.md' && git commit -m "feat: x"`,
	} {
		if code, out := hook(t, ws, c); strings.Contains(out, "tq/commit-pii") {
			t.Errorf("%s: false positive (code=%d) %q", c, code, out)
		}
	}
	// the N1 commands still deny
	for _, c := range []string{
		`git add -A && git commit -m "feat: x"`,
		`cd docs && git add -A && git commit -m "feat: x"`,
		`cd docs && git add .. && git commit -m "feat: x"`,
		`cd docs && git add :/ && git commit -m "feat: x"`,
		`git add *.csv && git commit -m "feat: x"`,
		`git add -A docs export.csv && git commit -m "feat: x"`,
	} {
		wantDeny(t, ws, c, "tq/commit-pii")
	}
}

// R3: Start-Process with a single-string or array argument list.
func TestRound3_R3_StartProcessArgumentList(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	stage(t, ws, run, "seed.sql", cpfRow)
	for _, c := range []string{
		`Start-Process -FilePath git -ArgumentList "commit -m x" -Wait`,
		`Start-Process git -ArgumentList @("commit","-m","x") -Wait`,
		`Start-Process git "commit -m x"`,
	} {
		if code, out := hookIn(t, ws, psPayload(t, ws, c)); !notAllowed(code, out, "tq/commit-pii") {
			t.Errorf("%s: code=%d %q", c, code, out)
		}
	}
}
