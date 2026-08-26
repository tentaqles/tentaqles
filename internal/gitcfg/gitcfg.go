// Package gitcfg manages git identity via includeIf, never touching credentials.
package gitcfg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const managedName = ".gitconfig-tentaqles"

// ValidateValue rejects control characters (including newlines) that could be
// used to inject extra keys/sections into a git config file.
func ValidateValue(v string) error {
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("git config value %q contains control characters", v)
		}
	}
	return nil
}

func IncludeFile() string {
	h, err := os.UserHomeDir()
	if err != nil {
		h = "."
	}
	return filepath.Join(h, managedName)
}

func WorkspaceFile(root string) string { return filepath.Join(root, managedName) }

func WriteWorkspace(root, name, email string) error {
	if err := ValidateValue(name); err != nil {
		return err
	}
	if err := ValidateValue(email); err != nil {
		return err
	}
	body := fmt.Sprintf("# managed by tq\n[user]\n\tname = %s\n\temail = %s\n\tuseConfigOnly = true\n", name, email)
	return os.WriteFile(WorkspaceFile(root), []byte(body), 0o644)
}

// Sync rewrites the managed include file with one includeIf block per workspace root.
func Sync(roots []string) error {
	rs := append([]string(nil), roots...)
	for i := range rs {
		if err := ValidateValue(rs[i]); err != nil {
			return err
		}
		rs[i] = filepath.ToSlash(filepath.Clean(rs[i]))
	}
	sort.Strings(rs)
	var b strings.Builder
	b.WriteString("# managed by tq — do not edit; run `tq doctor` / `tq add`\n")
	for _, r := range rs {
		fmt.Fprintf(&b, "[includeIf \"gitdir:%s/\"]\n\tpath = %s/%s\n", r, r, managedName)
	}
	return os.WriteFile(IncludeFile(), []byte(b.String()), 0o644)
}

// RunGit executes git and returns trimmed stdout.
func RunGit(args ...string) (string, error) {
	out, err := exec.Command("git", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// EnsureGlobal makes ~/.gitconfig include our file and refuse guessed identities.
func EnsureGlobal(run func(args ...string) (string, error)) error {
	inc := filepath.ToSlash(IncludeFile())
	existing, _ := run("config", "--global", "--get-all", "include.path")
	found := false
	for _, line := range strings.Split(existing, "\n") {
		if filepath.ToSlash(strings.TrimSpace(line)) == inc {
			found = true
		}
	}
	if !found {
		if _, err := run("config", "--global", "--add", "include.path", inc); err != nil {
			return fmt.Errorf("git config include.path: %w", err)
		}
	}
	if _, err := run("config", "--global", "user.useConfigOnly", "true"); err != nil {
		return fmt.Errorf("git config user.useConfigOnly: %w", err)
	}
	return nil
}
