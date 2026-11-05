package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Nothing tq writes into a profile names a permission mode (PRD v2 §13):
// not the managed block of any shell, not what Adopt writes for the real
// profiles in testdata. The carry comment an older tq wrote stays only as
// text to recognise and remove (oldCarryComment).
func TestNothingWrittenNamesAPermissionMode(t *testing.T) {
	for _, sh := range []Shell{"bash", "zsh", "fish", "powershell", "pwsh", "cmd"} {
		if b := Block(sh); strings.Contains(b, "permission_mode") || strings.Contains(b, "permission-mode") {
			t.Errorf("%s block names a permission mode", sh)
		}
	}
	ents, _ := filepath.Glob(filepath.Join("testdata", "real_*"))
	for _, p := range ents {
		if strings.Contains(p, ".carried.") || strings.Contains(p, ".want") {
			continue
		}
		sh := Shell("bash")
		if strings.HasSuffix(p, ".ps1") {
			sh = "pwsh"
		}
		dir := t.TempDir()
		cp := filepath.Join(dir, filepath.Base(p))
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cp, b, 0o600); err != nil {
			t.Fatal(err)
		}
		cs, err := AdoptFile(cp, sh)
		if err != nil {
			continue
		}
		added := strings.Replace(cs.New, cs.Old, "", 1)
		if cs.Changed && (strings.Contains(cs.New, oldCarryComment) || (strings.Contains(added, "permission_mode") && !strings.Contains(cs.Old, "permission_mode"))) {
			t.Errorf("%s: Adopt writes a permission mode", p)
		}
	}
}
