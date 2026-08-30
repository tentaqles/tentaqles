package hooks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("testdata %s: %v", name, err)
	}
	return raw
}

// stageProfile copies a testdata file into a temp dir and returns the path
// plus a Profiles map pointing sh at it.
func stageProfile(t *testing.T, name string, sh Shell) (string, Profiles) {
	t.Helper()
	raw := readTestdata(t, name)
	dir := t.TempDir()
	path := filepath.Join(dir, "profile")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path, Profiles{sh: path}
}

func stageBytes(t *testing.T, raw []byte, sh Shell) (string, Profiles) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "profile")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path, Profiles{sh: path}
}

// ---------------------------------------------------------------------------
// FindLegacyBlock
// ---------------------------------------------------------------------------

func TestFindLegacyBlock_RealPowerShellProfile(t *testing.T) {
	raw := readTestdata(t, "real_powershell_profile.ps1")
	content, enc := decodeProfile(raw)
	if enc != encUTF8BOM {
		t.Fatalf("expected the real profile to carry a UTF-8 BOM, got enc=%d", enc)
	}
	if !strings.Contains(content, "\r\n") {
		t.Fatal("expected the real profile to use CRLF")
	}

	start, end, ok := FindLegacyBlock(content, "powershell")
	if !ok {
		t.Fatal("FindLegacyBlock: not found in the real powershell profile")
	}
	if start != 0 {
		t.Errorf("start = %d, want 0 (the block is the whole file)", start)
	}
	if end != len(content) {
		t.Errorf("end = %d, want %d (block runs to EOF)", end, len(content))
	}
	block := content[start:end]
	if !strings.HasPrefix(block, LegacyHeaderPrefix) {
		t.Errorf("block does not start with the tq header: %q", firstLine(block))
	}
	if !strings.HasSuffix(block, "}\r\n") {
		t.Errorf("block does not end with the closing brace line, tail=%q", tailOf(block, 20))
	}
	// The whole if/else must be captured, else branch included.
	if !strings.Contains(block, "tq activate powershell") {
		t.Error("block is missing the else branch (tq activate)")
	}
}

func TestFindLegacyBlock_RealPwshProfile(t *testing.T) {
	raw := readTestdata(t, "real_pwsh_profile.ps1")
	content, _ := decodeProfile(raw)
	if _, _, ok := FindLegacyBlock(content, "pwsh"); !ok {
		t.Fatal("FindLegacyBlock: not found in the real pwsh profile")
	}
}

func TestFindLegacyBlock_RealBashrc(t *testing.T) {
	raw := readTestdata(t, "real_bashrc")
	content, enc := decodeProfile(raw)
	if enc != encUTF8 {
		t.Fatalf("expected the real .bashrc to have no BOM, got enc=%d", enc)
	}

	start, end, ok := FindLegacyBlock(content, "bash")
	if !ok {
		t.Fatal("FindLegacyBlock: not found in the real .bashrc")
	}
	if end != len(content) {
		t.Errorf("end = %d, want %d", end, len(content))
	}
	prefix := content[:start]
	if !strings.HasPrefix(prefix, "alias claude=") {
		t.Errorf("prefix should start with the untouched alias line, got %q", firstLine(prefix))
	}
	if strings.Contains(prefix, "tq") {
		t.Errorf("prefix should not contain any tq lines: %q", prefix)
	}
	block := content[start:end]
	if !strings.HasPrefix(block, LegacyHeaderPrefix) {
		t.Errorf("block does not start with the tq header: %q", firstLine(block))
	}
	if !strings.HasSuffix(block, "fi\r\n") {
		t.Errorf("block should end at the matching fi, tail=%q", tailOf(block, 20))
	}
}

func TestFindLegacyBlock_NotFound(t *testing.T) {
	cases := map[string]string{
		"empty":       "",
		"no header":   "export PATH=/usr/bin\nalias ll='ls -l'\n",
		"header only": "# --- tq (managed by Tentaqles) ---\n",
		"managed":     "# some stuff\n" + Block("bash"),
	}
	for name, content := range cases {
		if _, _, ok := FindLegacyBlock(content, "bash"); ok {
			t.Errorf("%s: expected ok=false", name)
		}
	}
}

func TestFindLegacyBlock_UnknownShellIsNotFound(t *testing.T) {
	raw := readTestdata(t, "real_bashrc")
	content, _ := decodeProfile(raw)
	if _, _, ok := FindLegacyBlock(content, "fish"); ok {
		t.Error("fish has no legacy shape; expected ok=false")
	}
}

func TestFindLegacyBlock_SurroundedByOtherContent(t *testing.T) {
	raw := readTestdata(t, "real_bashrc")
	content, _ := decodeProfile(raw)
	wrapped := content + "\r\n# after the block\r\nexport AFTER=1\r\n"

	start, end, ok := FindLegacyBlock(wrapped, "bash")
	if !ok {
		t.Fatal("not found")
	}
	if got, want := wrapped[end:], "\r\n# after the block\r\nexport AFTER=1\r\n"; got != want {
		t.Errorf("suffix after block = %q, want %q", got, want)
	}
	if wrapped[:start] != content[:strings.Index(content, LegacyHeaderPrefix)] {
		t.Error("prefix changed")
	}
}

// ---------------------------------------------------------------------------
// SplitLegacy
// ---------------------------------------------------------------------------

func TestSplitLegacy_PowerShell(t *testing.T) {
	raw := readTestdata(t, "real_powershell_profile.ps1")
	content, _ := decodeProfile(raw)
	start, end, ok := FindLegacyBlock(content, "powershell")
	if !ok {
		t.Fatal("FindLegacyBlock failed")
	}
	body, ok := SplitLegacy(content[start:end], "powershell")
	if !ok {
		t.Fatal("SplitLegacy: expected a TQ_ENABLED=0 branch")
	}
	for _, want := range []string{
		"function Get-ClientContext",
		"function claude-dbi",
		"function claude-uplabs",
		"function claude-personal",
		"function prompt",
		"$ClaudeExe = ",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("legacy body missing %q", want)
		}
	}
	if strings.Contains(body, "tq activate") {
		t.Error("legacy body leaked the else branch")
	}
	if strings.Contains(body, LegacyHeaderPrefix) {
		t.Error("legacy body leaked the header line")
	}
	if !strings.HasSuffix(body, "\r\n") {
		t.Errorf("legacy body should keep its trailing CRLF, tail=%q", tailOf(body, 20))
	}
	// Verbatim: the body must appear as-is inside the original file.
	if !strings.Contains(content, body) {
		t.Error("legacy body is not a verbatim substring of the original profile")
	}
	// It must be the *whole* branch: first and last statements intact.
	if !strings.HasPrefix(strings.TrimLeft(body, " \t"), "# --- Tentaqles multi-identity launcher") {
		t.Errorf("legacy body does not start at the first legacy line: %q", firstLine(body))
	}
	if !strings.Contains(lastNonEmptyLine(body), "Set-IdentityEnv") {
		t.Errorf("legacy body does not end at Set-IdentityEnv: %q", lastNonEmptyLine(body))
	}
}

func TestSplitLegacy_BashHasNone(t *testing.T) {
	raw := readTestdata(t, "real_bashrc")
	content, _ := decodeProfile(raw)
	start, end, ok := FindLegacyBlock(content, "bash")
	if !ok {
		t.Fatal("FindLegacyBlock failed")
	}
	if body, ok := SplitLegacy(content[start:end], "bash"); ok {
		t.Errorf("bash has no legacy branch, got ok=true body=%q", body)
	}
}

// ---------------------------------------------------------------------------
// Adopt — PowerShell
// ---------------------------------------------------------------------------

func TestAdopt_RealPowerShellProfile(t *testing.T) {
	path, profiles := stageProfile(t, "real_powershell_profile.ps1", "powershell")
	orig := readTestdata(t, "real_powershell_profile.ps1")

	cs, err := Adopt("powershell", profiles)
	if err != nil {
		t.Fatal(err)
	}
	if !cs.Changed {
		t.Fatalf("expected a change, got Changed=false reason=%q", cs.Reason)
	}
	if cs.Legacy == "" {
		t.Fatal("expected a legacy branch")
	}
	if cs.Wrapper == "" {
		t.Fatal("expected the claude wrapper to be carried over")
	}
	if !strings.Contains(cs.Wrapper, "--dangerously-skip-permissions") {
		t.Errorf("carried wrapper = %q", cs.Wrapper)
	}

	if err := cs.Apply(); err != nil {
		t.Fatal(err)
	}

	if st := StatusOf("powershell", profiles); st.State != "installed" {
		t.Fatalf("StatusOf = %q, want installed", st.State)
	}

	newRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(newRaw, bomUTF8) {
		t.Error("BOM lost")
	}
	newContent, _ := decodeProfile(newRaw)
	assertCRLFOnly(t, newContent)

	// legacy body preserved verbatim, inside the tq-legacy wrapper
	li := strings.Index(newContent, LegacyStartMarker)
	le := strings.Index(newContent, LegacyEndMarker)
	if li < 0 || le < 0 || le < li {
		t.Fatalf("tq-legacy markers missing or out of order (%d,%d)", li, le)
	}
	if !strings.Contains(newContent[li:le], cs.Legacy) {
		t.Error("legacy body not preserved verbatim inside the tq-legacy wrapper")
	}
	if !strings.Contains(newContent[li:le], "$env:TQ_ENABLED -eq '0'") {
		t.Error("tq-legacy wrapper is missing its TQ_ENABLED=0 guard")
	}

	// the managed block, after the legacy wrapper
	mi := strings.Index(newContent, startMarker)
	if mi < le {
		t.Error("managed block should come after the tq-legacy wrapper")
	}
	if !strings.Contains(newContent, "tq activate powershell | Out-String | Invoke-Expression") {
		t.Error("managed activate line missing")
	}

	// carried-over wrapper, after the managed block, under its comment
	ci := strings.Index(newContent, CarryComment)
	if ci < strings.Index(newContent, endMarker) {
		t.Error("carried wrapper should come after the managed block")
	}
	if !strings.Contains(newContent[ci:], cs.Wrapper) {
		t.Error("carried wrapper not emitted under its comment")
	}
	// The wrapper lived in the old `else` branch: it must stay guarded so
	// TQ_ENABLED=0 leaves the legacy launcher's own `claude` router in place.
	if !strings.Contains(newContent[ci:], carriedGuard) {
		t.Error("carried wrapper is not guarded by the inverse TQ_ENABLED check")
	}
	if strings.Index(newContent[ci:], carriedGuard) > strings.Index(newContent[ci:], cs.Wrapper) {
		t.Error("guard must precede the carried wrapper")
	}

	// The old else-branch scaffolding must be gone.
	if strings.Contains(newContent, "Write-Warning 'tq not found") {
		t.Error("old else-branch scaffolding survived")
	}
	// Nothing outside the block existed in this file, so length must still grow
	// only by our additions; but the original prefix/suffix (empty) hold.
	if bytes.Equal(orig, newRaw) {
		t.Error("file unchanged")
	}

	// backup holds the original bytes verbatim
	bak, err := os.ReadFile(path + backupSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bak, orig) {
		t.Error("backup is not byte-identical to the original profile")
	}
}

func TestAdopt_PowerShellPreservesBytesOutsideBlock(t *testing.T) {
	orig := readTestdata(t, "real_powershell_profile.ps1")
	prefix := []byte("\xEF\xBB\xBF# my own header\r\n$foo = 1\r\n\r\n")
	suffix := []byte("\r\n# my own footer\r\nSet-Alias ll Get-ChildItem\r\n")
	// splice: BOM stays at the front, drop the original BOM.
	raw := append(append(append([]byte{}, prefix...), orig[3:]...), suffix...)

	path, profiles := stageBytes(t, raw, "powershell")
	cs, err := Adopt("powershell", profiles)
	if err != nil {
		t.Fatal(err)
	}
	if !cs.Changed {
		t.Fatalf("Changed=false reason=%q", cs.Reason)
	}
	if err := cs.Apply(); err != nil {
		t.Fatal(err)
	}
	newRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(newRaw, prefix) {
		t.Errorf("bytes before the block changed:\n got %q", firstBytes(newRaw, len(prefix)+10))
	}
	if !bytes.HasSuffix(newRaw, suffix) {
		t.Errorf("bytes after the block changed:\n got %q", lastBytes(newRaw, len(suffix)+10))
	}
	if st := StatusOf("powershell", profiles); st.State != "installed" {
		t.Fatalf("StatusOf = %q, want installed", st.State)
	}
}

// ---------------------------------------------------------------------------
// Adopt — bash
// ---------------------------------------------------------------------------

func TestAdopt_RealBashrc(t *testing.T) {
	path, profiles := stageProfile(t, "real_bashrc", "bash")
	orig := readTestdata(t, "real_bashrc")
	origContent, _ := decodeProfile(orig)
	prefix := origContent[:strings.Index(origContent, LegacyHeaderPrefix)]

	cs, err := Adopt("bash", profiles)
	if err != nil {
		t.Fatal(err)
	}
	if !cs.Changed {
		t.Fatalf("Changed=false reason=%q", cs.Reason)
	}
	if cs.Legacy != "" {
		t.Errorf("bash has no legacy branch, got %q", cs.Legacy)
	}
	if cs.Wrapper != "" {
		t.Errorf("bash carries no wrapper, got %q", cs.Wrapper)
	}
	if err := cs.Apply(); err != nil {
		t.Fatal(err)
	}

	if st := StatusOf("bash", profiles); st.State != "installed" {
		t.Fatalf("StatusOf = %q, want installed", st.State)
	}
	newRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(newRaw, bomUTF8) {
		t.Error("a BOM was introduced into .bashrc")
	}
	newContent, _ := decodeProfile(newRaw)
	if !strings.HasPrefix(newContent, prefix) {
		t.Errorf("bytes before the block changed\n got %q\nwant %q", firstLine(newContent), firstLine(prefix))
	}
	if !strings.Contains(newContent, "alias claude='claude --dangerously-skip-permissions'") {
		t.Error("the untouched alias line was lost")
	}
	if strings.Contains(newContent, LegacyStartMarker) {
		t.Error("bash should not get a tq-legacy wrapper")
	}
	if strings.Contains(newContent, CarryComment) {
		t.Error("bash should not get a carried wrapper")
	}
	assertCRLFOnly(t, newContent)
}

func TestAdopt_ReportsDroppedPathLines(t *testing.T) {
	_, profiles := stageProfile(t, "real_bashrc", "bash")
	cs, err := Adopt("bash", profiles)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cs.Dropped, "\n")
	if !strings.Contains(joined, "LOCALAPPDATA/tentaqles/bin") {
		t.Errorf("expected the PATH line to be reported as dropped, got %q", joined)
	}
}

// ---------------------------------------------------------------------------
// Adopt — refusals
// ---------------------------------------------------------------------------

func TestAdopt_NoRecognisableBlockIsSkipped(t *testing.T) {
	raw := []byte("alias ll='ls -l'\nexport EDITOR=vim\n")
	path, profiles := stageBytes(t, raw, "bash")
	cs, err := Adopt("bash", profiles)
	if err != nil {
		t.Fatal(err)
	}
	if cs.Changed {
		t.Fatal("expected Changed=false")
	}
	if cs.Reason == "" {
		t.Error("expected a Reason explaining the skip")
	}
	if err := cs.Apply(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Error("Apply on a no-op ChangeSet modified the file")
	}
	if _, err := os.Stat(path + backupSuffix); !os.IsNotExist(err) {
		t.Error("Apply on a no-op ChangeSet created a backup")
	}
}

func TestAdopt_AlreadyManagedIsSkipped(t *testing.T) {
	raw := []byte("alias ll='ls -l'\n\n" + Block("bash"))
	_, profiles := stageBytes(t, raw, "bash")
	cs, err := Adopt("bash", profiles)
	if err != nil {
		t.Fatal(err)
	}
	if cs.Changed {
		t.Fatal("expected Changed=false for an already-managed profile")
	}
}

func TestAdopt_MissingProfileIsSkipped(t *testing.T) {
	dir := t.TempDir()
	profiles := Profiles{"bash": filepath.Join(dir, "nope")}
	cs, err := Adopt("bash", profiles)
	if err != nil {
		t.Fatal(err)
	}
	if cs.Changed {
		t.Fatal("expected Changed=false")
	}
}

func TestAdopt_NoProfileMapping(t *testing.T) {
	cs, err := Adopt("bash", Profiles{})
	if err != nil {
		t.Fatal(err)
	}
	if cs.Changed {
		t.Fatal("expected Changed=false")
	}
}

// ---------------------------------------------------------------------------
// Round trip: adopt then restore from the backup
// ---------------------------------------------------------------------------

func TestAdopt_RestoreRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		file string
		sh   Shell
	}{
		{"real_powershell_profile.ps1", "powershell"},
		{"real_pwsh_profile.ps1", "pwsh"},
		{"real_bashrc", "bash"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			path, profiles := stageProfile(t, tc.file, tc.sh)
			orig := readTestdata(t, tc.file)

			cs, err := Adopt(tc.sh, profiles)
			if err != nil {
				t.Fatal(err)
			}
			if !cs.Changed {
				t.Fatalf("Changed=false reason=%q", cs.Reason)
			}
			if err := cs.Apply(); err != nil {
				t.Fatal(err)
			}
			if st := StatusOf(tc.sh, profiles); st.State != "installed" {
				t.Fatalf("StatusOf = %q, want installed", st.State)
			}

			bak, err := os.ReadFile(path + backupSuffix)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(bak, orig) {
				t.Fatal("backup is not byte-identical to the original")
			}
			if err := os.WriteFile(path, bak, 0o644); err != nil {
				t.Fatal(err)
			}
			restored, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(restored, orig) {
				t.Fatal("restore round trip is not byte-identical")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Golden files
// ---------------------------------------------------------------------------

func TestAdopt_Golden(t *testing.T) {
	for _, tc := range []struct {
		file, golden string
		sh           Shell
	}{
		{"real_powershell_profile.ps1", "real_powershell_profile.adopted.ps1", "powershell"},
		{"real_pwsh_profile.ps1", "real_pwsh_profile.adopted.ps1", "pwsh"},
		{"real_bashrc", "real_bashrc.adopted", "bash"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			path, profiles := stageProfile(t, tc.file, tc.sh)
			cs, err := Adopt(tc.sh, profiles)
			if err != nil {
				t.Fatal(err)
			}
			if err := cs.Apply(); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			gp := filepath.Join("testdata", tc.golden)
			if os.Getenv("TQ_UPDATE_GOLDEN") != "" {
				if err := os.WriteFile(gp, got, 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("updated golden %s", gp)
				return
			}
			want, err := os.ReadFile(gp)
			if err != nil {
				t.Fatalf("%v (re-run with TQ_UPDATE_GOLDEN=1 to create)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", tc.golden, got, want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// small assertions
// ---------------------------------------------------------------------------

func assertCRLFOnly(t *testing.T, s string) {
	t.Helper()
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' && (i == 0 || s[i-1] != '\r') {
			t.Errorf("bare LF introduced at offset %d: %q", i, s[max(0, i-40):min(len(s), i+10)])
			return
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}

func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func firstBytes(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[:n]
}

func lastBytes(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[len(b)-n:]
}

// ---------------------------------------------------------------------------
// idempotence, interaction with Remove, and non-UTF-8 encodings
// ---------------------------------------------------------------------------

func TestAdopt_IsIdempotent(t *testing.T) {
	_, profiles := stageProfile(t, "real_powershell_profile.adopted.ps1", "powershell")
	cs, err := Adopt("powershell", profiles)
	if err != nil {
		t.Fatal(err)
	}
	if cs.Changed {
		t.Fatal("adopting an already-adopted profile should be a no-op")
	}
	if cs.Reason != ReasonManaged {
		t.Errorf("Reason = %q, want %q", cs.Reason, ReasonManaged)
	}
}

func TestRemove_LeavesAdoptedLegacyBlockAlone(t *testing.T) {
	path, profiles := stageProfile(t, "real_powershell_profile.adopted.ps1", "powershell")
	if _, err := Remove("powershell", profiles); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := decodeProfile(raw)
	if strings.Contains(content, startMarker) {
		t.Error("managed block should be gone")
	}
	if !strings.Contains(content, LegacyStartMarker) || !strings.Contains(content, LegacyEndMarker) {
		t.Error("Remove must not touch the preserved tq-legacy wrapper")
	}
	if !strings.Contains(content, "function claude-dbi") {
		t.Error("Remove must not touch the legacy launcher body")
	}
}

func TestAdopt_PreservesUTF16LE(t *testing.T) {
	raw := readTestdata(t, "real_powershell_profile.ps1")
	content, _ := decodeProfile(raw)
	u16 := encodeProfile(content, encUTF16LE)

	path, profiles := stageBytes(t, u16, "powershell")
	cs, err := Adopt("powershell", profiles)
	if err != nil {
		t.Fatal(err)
	}
	if !cs.Changed {
		t.Fatalf("Changed=false reason=%q", cs.Reason)
	}
	if err := cs.Apply(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(got, bomUTF16LE) {
		t.Fatalf("UTF-16LE BOM lost, got %x", firstBytes(got, 8))
	}
	back, enc := decodeProfile(got)
	if enc != encUTF16LE {
		t.Fatalf("encoding changed to %d", enc)
	}
	if !strings.Contains(back, startMarker) {
		t.Error("managed block missing after a UTF-16 round trip")
	}
	if st := StatusOf("powershell", profiles); st.State != "installed" {
		t.Fatalf("StatusOf = %q, want installed", st.State)
	}
}
