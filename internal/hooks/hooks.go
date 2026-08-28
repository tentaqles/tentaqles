// Package hooks manages tq's marker-delimited shell activation block in
// user shell profiles (bash, zsh, fish, pwsh, powershell).
package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

// Shell identifies a supported shell.
type Shell string

// Shells lists all shells tq knows how to hook, in a stable order.
var Shells = []Shell{"bash", "zsh", "fish", "pwsh", "powershell"}

// Profiles maps a shell to its profile file path. Production code uses
// DefaultProfiles; tests inject their own temp paths.
type Profiles map[Shell]string

const (
	startMarker = "# >>> tq >>>"
	endMarker   = "# <<< tq <<<"

	tmpSuffix    = ".tq-tmp"
	backupSuffix = ".tq-backup"
)

// DefaultProfiles returns the real, per-OS shell profile paths.
// powershell (Windows PowerShell 5.1) is only offered on Windows.
func DefaultProfiles() Profiles {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	p := Profiles{
		"bash": filepath.Join(home, ".bashrc"),
		"zsh":  filepath.Join(home, ".zshrc"),
		"fish": filepath.Join(home, ".config", "fish", "config.fish"),
	}
	if runtime.GOOS == "windows" {
		p["pwsh"] = filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
		p["powershell"] = filepath.Join(home, "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1")
	} else {
		p["pwsh"] = filepath.Join(home, ".config", "powershell", "Microsoft.PowerShell_profile.ps1")
	}
	return p
}

// ProfilesFn is the seam commands use to look up profile paths; tests
// override it to point at temp files.
var ProfilesFn = DefaultProfiles

// activateLine returns the "tq activate <shell> ..." invocation line
// (without leading indentation) used both inside our block and to detect
// hand-installed hooks.
func activateLine(sh Shell) string {
	switch sh {
	case "bash", "zsh":
		return `eval "$(tq activate ` + string(sh) + `)"`
	case "fish":
		return `tq activate fish | source`
	case "pwsh", "powershell":
		return `tq activate ` + string(sh) + ` | Out-String | Invoke-Expression`
	default:
		return `tq activate ` + string(sh)
	}
}

// Block returns the full marker-delimited block text for sh, ending with a
// trailing newline.
func Block(sh Shell) string {
	var body string
	switch sh {
	case "bash", "zsh":
		body = `if [ "${TQ_ENABLED:-1}" != "0" ] && command -v tq >/dev/null 2>&1; then eval "$(tq activate ` + string(sh) + `)"; fi`
	case "fish":
		body = `if test "$TQ_ENABLED" != "0"; and command -q tq; tq activate fish | source; end`
	case "pwsh", "powershell":
		body = `if ($env:TQ_ENABLED -ne '0' -and (Get-Command tq -ErrorAction SilentlyContinue)) { tq activate ` + string(sh) + ` | Out-String | Invoke-Expression }`
	default:
		body = activateLine(sh)
	}
	return startMarker + "\n" + body + "\n" + endMarker + "\n"
}

// encoding records how a profile file was stored on disk so a rewrite can
// reproduce it byte-for-byte (BOM and endianness included).
type encoding int

const (
	encUTF8 encoding = iota
	encUTF8BOM
	encUTF16LE
	encUTF16BE
)

var (
	bomUTF8    = []byte{0xEF, 0xBB, 0xBF}
	bomUTF16LE = []byte{0xFF, 0xFE}
	bomUTF16BE = []byte{0xFE, 0xFF}
)

// decodeProfile turns raw profile bytes into UTF-8 text plus the encoding
// needed to write it back out unchanged.
func decodeProfile(raw []byte) (string, encoding) {
	switch {
	case len(raw) >= 3 && string(raw[:3]) == string(bomUTF8):
		return string(raw[3:]), encUTF8BOM
	case len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE:
		return decodeUTF16(raw[2:], true), encUTF16LE
	case len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF:
		return decodeUTF16(raw[2:], false), encUTF16BE
	default:
		return string(raw), encUTF8
	}
}

func decodeUTF16(b []byte, little bool) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if little {
			u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
		} else {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		}
	}
	return string(utf16.Decode(u))
}

// encodeProfile is the inverse of decodeProfile.
func encodeProfile(content string, e encoding) []byte {
	switch e {
	case encUTF8BOM:
		return append(append([]byte(nil), bomUTF8...), content...)
	case encUTF16LE:
		return append(append([]byte(nil), bomUTF16LE...), encodeUTF16(content, true)...)
	case encUTF16BE:
		return append(append([]byte(nil), bomUTF16BE...), encodeUTF16(content, false)...)
	default:
		return []byte(content)
	}
}

func encodeUTF16(s string, little bool) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, 0, len(u)*2)
	for _, c := range u {
		if little {
			out = append(out, byte(c), byte(c>>8))
		} else {
			out = append(out, byte(c>>8), byte(c))
		}
	}
	return out
}

// readProfile reads path and returns its decoded text, its encoding and its
// file mode. A missing file yields empty text, encUTF8 and ok == false.
func readProfile(path string) (content string, e encoding, mode os.FileMode, ok bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", encUTF8, 0o644, false, nil
		}
		return "", encUTF8, 0o644, false, err
	}
	mode = os.FileMode(0o644)
	if fi, serr := os.Stat(path); serr == nil {
		mode = fi.Mode().Perm()
	}
	content, e = decodeProfile(raw)
	return content, e, mode, true, nil
}

// backupProfile copies path to "<path>.tq-backup" the first time tq is
// about to modify it. A backup that already exists is left alone, so the
// backup always reflects the file as it was before tq ever touched it.
func backupProfile(path string) error {
	bak := path + backupSuffix
	if _, err := os.Stat(bak); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	return os.WriteFile(bak, raw, mode)
}

// writeProfile writes data to path atomically: it writes a sibling
// "<path>.tq-tmp" first and renames it over the destination, so a crash
// mid-write can never leave a truncated shell profile behind.
func writeProfile(path string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	tmp := path + tmpSuffix
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		// Windows refuses to rename over an existing file; drop the
		// destination and retry once.
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			_ = os.Remove(tmp)
			return err
		}
		if err2 := os.Rename(tmp, path); err2 != nil {
			_ = os.Remove(tmp)
			return err2
		}
	}
	return os.Chmod(path, mode)
}

// Status describes the hook state for a shell.
type Status struct {
	Shell   Shell
	Profile string
	// State is one of: "installed", "present (unmanaged)", "missing",
	// "no profile", "corrupt" (exactly one marker present), "unreadable",
	// or "repaired" (returned by Remove after fixing a corrupt block).
	State string
}

// Detect returns the shells to offer: those with a profile mapping whose
// file exists, or whose executable is found via lookPath. Shells with no
// profile path mapping are skipped entirely — tq has nowhere to install a
// hook for them.
func Detect(p Profiles, lookPath func(string) (string, error)) []Shell {
	var out []Shell
	for _, sh := range Shells {
		profile, ok := p[sh]
		if !ok || profile == "" {
			continue
		}
		if _, err := os.Stat(profile); err == nil {
			out = append(out, sh)
			continue
		}
		if lookPath != nil {
			if _, err := lookPath(string(sh)); err == nil {
				out = append(out, sh)
				continue
			}
		}
	}
	return out
}

// LookPath is the default executable lookup, exposed so callers don't need
// to import os/exec themselves.
func LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

// StatusOf reports the current install state for sh.
func StatusOf(sh Shell, p Profiles) Status {
	profile, ok := p[sh]
	if !ok || profile == "" {
		return Status{Shell: sh, State: "no profile"}
	}
	st := Status{Shell: sh, Profile: profile}
	content, _, _, exists, err := readProfile(profile)
	if err != nil {
		st.State = "unreadable"
		return st
	}
	if !exists {
		st.State = "no profile"
		return st
	}
	hasStart := strings.Contains(content, startMarker)
	hasEnd := strings.Contains(content, endMarker)
	switch {
	case hasStart && hasEnd:
		st.State = "installed"
	case hasStart != hasEnd:
		st.State = "corrupt"
	case strings.Contains(content, "tq activate "+string(sh)):
		st.State = "present (unmanaged)"
	default:
		st.State = "missing"
	}
	return st
}

// Install appends the tq block to sh's profile, creating parent dirs and
// the file if needed. It is idempotent: if already installed or present
// unmanaged, it leaves the file untouched and returns the current status.
// A profile with only one of the two markers is refused rather than
// silently appended to.
func Install(sh Shell, p Profiles) (Status, error) {
	profile, ok := p[sh]
	if !ok || profile == "" {
		return Status{Shell: sh, State: "no profile"}, nil
	}

	st := StatusOf(sh, p)
	if st.State == "installed" || st.State == "present (unmanaged)" {
		return st, nil
	}
	if st.State == "corrupt" {
		return st, fmt.Errorf("%s: %s contains a partial tq block; run: tq hooks remove %s", sh, profile, sh)
	}
	if st.State == "unreadable" {
		return st, fmt.Errorf("%s: cannot read %s", sh, profile)
	}

	if err := os.MkdirAll(filepath.Dir(profile), 0755); err != nil {
		return st, err
	}

	existing, enc, mode, exists, err := readProfile(profile)
	if err != nil {
		return st, err
	}

	// Match the existing file's line ending: if it already uses CRLF, write
	// our block with CRLF too so we don't introduce a mixed-ending file.
	block := Block(sh)
	if strings.Contains(existing, "\r\n") {
		block = strings.ReplaceAll(block, "\r\n", "\n")
		block = strings.ReplaceAll(block, "\n", "\r\n")
	}

	var newContent string
	switch {
	case existing == "":
		newContent = block
	case strings.HasSuffix(existing, "\n"):
		sep := "\n"
		if strings.Contains(existing, "\r\n") {
			sep = "\r\n"
		}
		newContent = existing + sep + block
	default:
		sep := "\n\n"
		if strings.Contains(existing, "\r\n") {
			sep = "\r\n\r\n"
		}
		newContent = existing + sep + block
	}

	if exists {
		if err := backupProfile(profile); err != nil {
			return st, err
		}
	}
	if err := writeProfile(profile, encodeProfile(newContent, enc), mode); err != nil {
		return st, err
	}
	return StatusOf(sh, p), nil
}

// Remove deletes tq's marker-delimited block (and one preceding blank line,
// if present) from sh's profile. Removing when absent is a no-op. A
// profile that kept only one of the two markers is repaired: everything
// from that marker to the next marker (or EOF) is dropped, and the returned
// Status reports State "repaired".
func Remove(sh Shell, p Profiles) (Status, error) {
	st := StatusOf(sh, p)
	if st.State != "installed" && st.State != "corrupt" {
		return st, nil
	}
	repairing := st.State == "corrupt"

	profile := p[sh]
	content, enc, mode, _, err := readProfile(profile)
	if err != nil {
		return st, err
	}
	lines := strings.Split(content, "\n")

	isMarker := func(l string) bool {
		t := strings.TrimSpace(strings.TrimSuffix(l, "\r"))
		return t == startMarker || t == endMarker
	}
	trimmed := func(l string) string { return strings.TrimSpace(strings.TrimSuffix(l, "\r")) }

	startIdx, endIdx := -1, -1
	for i, l := range lines {
		if trimmed(l) == startMarker && startIdx == -1 {
			startIdx = i
			continue
		}
		if trimmed(l) == endMarker && endIdx == -1 {
			endIdx = i
			if startIdx != -1 {
				break
			}
		}
	}

	switch {
	case startIdx != -1 && endIdx != -1 && endIdx > startIdx:
		// well-formed block
	case startIdx != -1:
		// Only a start marker: drop from it to the next marker, or to EOF.
		endIdx = len(lines) - 1
		for i := startIdx + 1; i < len(lines); i++ {
			if isMarker(lines[i]) {
				endIdx = i
				break
			}
		}
	case endIdx != -1:
		// Only an end marker: drop just that line.
		startIdx = endIdx
	default:
		return st, nil
	}

	removeFrom := startIdx
	if removeFrom > 0 && trimmed(lines[removeFrom-1]) == "" {
		removeFrom--
	}

	newLines := append(append([]string{}, lines[:removeFrom]...), lines[endIdx+1:]...)
	newContent := strings.Join(newLines, "\n")

	if err := backupProfile(profile); err != nil {
		return st, err
	}
	if err := writeProfile(profile, encodeProfile(newContent, enc), mode); err != nil {
		return st, err
	}
	if repairing {
		return Status{Shell: sh, Profile: profile, State: "repaired"}, nil
	}
	return StatusOf(sh, p), nil
}
