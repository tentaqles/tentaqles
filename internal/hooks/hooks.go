// Package hooks manages tq's marker-delimited shell activation block in
// user shell profiles (bash, zsh, fish, pwsh, powershell).
package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// Status describes the hook state for a shell.
type Status struct {
	Shell   Shell
	Profile string
	State   string // "installed" | "present (unmanaged)" | "missing" | "no profile"
}

// Detect returns the shells to offer: those whose profile file exists, or
// whose executable is found via lookPath.
func Detect(p Profiles, lookPath func(string) (string, error)) []Shell {
	var out []Shell
	for _, sh := range Shells {
		profile, ok := p[sh]
		if ok {
			if _, err := os.Stat(profile); err == nil {
				out = append(out, sh)
				continue
			}
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
	data, err := os.ReadFile(profile)
	if err != nil {
		if os.IsNotExist(err) {
			st.State = "no profile"
		} else {
			st.State = "missing"
		}
		return st
	}
	content := string(data)
	if strings.Contains(content, startMarker) && strings.Contains(content, endMarker) {
		st.State = "installed"
		return st
	}
	if strings.Contains(content, "tq activate "+string(sh)) {
		st.State = "present (unmanaged)"
		return st
	}
	st.State = "missing"
	return st
}

// Install appends the tq block to sh's profile, creating parent dirs and
// the file if needed. It is idempotent: if already installed or present
// unmanaged, it leaves the file untouched and returns the current status.
func Install(sh Shell, p Profiles) (Status, error) {
	profile, ok := p[sh]
	if !ok || profile == "" {
		return Status{Shell: sh, State: "no profile"}, nil
	}

	st := StatusOf(sh, p)
	if st.State == "installed" || st.State == "present (unmanaged)" {
		return st, nil
	}

	if err := os.MkdirAll(filepath.Dir(profile), 0755); err != nil {
		return st, err
	}

	var existing []byte
	if data, err := os.ReadFile(profile); err == nil {
		existing = data
	}

	// Match the existing file's line ending: if it already uses CRLF, write
	// our block with CRLF too so we don't introduce a mixed-ending file.
	block := Block(sh)
	if strings.Contains(string(existing), "\r\n") {
		block = strings.ReplaceAll(block, "\r\n", "\n")
		block = strings.ReplaceAll(block, "\n", "\r\n")
	}

	var newContent string
	if len(existing) == 0 {
		newContent = block
	} else if strings.HasSuffix(string(existing), "\n") {
		sep := "\n"
		if strings.Contains(string(existing), "\r\n") {
			sep = "\r\n"
		}
		newContent = string(existing) + sep + block
	} else {
		sep := "\n\n"
		if strings.Contains(string(existing), "\r\n") {
			sep = "\r\n\r\n"
		}
		newContent = string(existing) + sep + block
	}

	if err := os.WriteFile(profile, []byte(newContent), 0644); err != nil {
		return st, err
	}
	return StatusOf(sh, p), nil
}

// Remove deletes tq's marker-delimited block (and one preceding blank line,
// if present) from sh's profile. Removing when absent is a no-op.
func Remove(sh Shell, p Profiles) (Status, error) {
	st := StatusOf(sh, p)
	if st.State != "installed" {
		return st, nil
	}

	profile := p[sh]
	data, err := os.ReadFile(profile)
	if err != nil {
		return st, err
	}
	lines := strings.Split(string(data), "\n")

	startIdx, endIdx := -1, -1
	for i, l := range lines {
		if strings.TrimSpace(l) == startMarker {
			startIdx = i
		}
		if strings.TrimSpace(l) == endMarker && startIdx != -1 && endIdx == -1 {
			endIdx = i
			break
		}
	}
	if startIdx == -1 || endIdx == -1 {
		return st, nil
	}

	removeFrom := startIdx
	if removeFrom > 0 && strings.TrimSpace(lines[removeFrom-1]) == "" {
		removeFrom--
	}

	newLines := append(append([]string{}, lines[:removeFrom]...), lines[endIdx+1:]...)
	newContent := strings.Join(newLines, "\n")

	if err := os.WriteFile(profile, []byte(newContent), 0644); err != nil {
		return st, err
	}
	return StatusOf(sh, p), nil
}
