//go:build windows

package setupapi

import (
	"fmt"
	"os/exec"
	"strings"
)

// setUserPathOS adds dir to the current user's PATH via the registry
// (HKCU\Environment), using PowerShell's [Environment]::SetEnvironmentVariable
// so the change is picked up by new shells without a reboot. It is a no-op
// if dir is already present.
func setUserPathOS(dir string) error {
	getCmd := exec.Command("powershell", "-NoProfile", "-Command",
		"[Environment]::GetEnvironmentVariable('Path','User')")
	out, err := getCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("read user PATH: %w: %s", err, string(out))
	}
	current := strings.TrimSpace(string(out))

	for _, part := range strings.Split(current, ";") {
		if strings.EqualFold(strings.TrimSuffix(part, `\`), strings.TrimSuffix(dir, `\`)) {
			return nil
		}
	}

	newPath := dir
	if current != "" {
		newPath = current + ";" + dir
	}

	setCmd := exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf("[Environment]::SetEnvironmentVariable('Path', %s, 'User')", psQuote(newPath)))
	if out, err := setCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("set user PATH: %w: %s", err, string(out))
	}
	return nil
}

// psQuote wraps s in a single-quoted PowerShell string literal, doubling any
// embedded single quotes.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
