// Package doctor verifies expected vs actual identity state. It never mutates.
package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/cli/internal/envplan"
	"github.com/tentaqles/tentaqles/cli/internal/gitcfg"
	"github.com/tentaqles/tentaqles/cli/internal/paths"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
	"github.com/tentaqles/tentaqles/cli/internal/resolve"
	"github.com/tentaqles/tentaqles/cli/internal/trust"
)

type Finding struct {
	Level     string `json:"level"`
	Code      string `json:"code"`
	Workspace string `json:"workspace,omitempty"`
	Msg       string `json:"msg"`
	Fix       string `json:"fix,omitempty"`
}

type Deps struct {
	Env      func(string) (string, bool)
	Cwd      string
	RunGit   func(args ...string) (string, error)
	LookPath func(string) (string, error)
}

func Run(cfg *registry.Config, d Deps) []Finding {
	var fs []Finding
	add := func(level, code, ws, msg, fix string) { fs = append(fs, Finding{level, code, ws, msg, fix}) }

	if len(cfg.Bases) == 0 {
		add("error", "no-bases", "", "no base folders registered", "tq init <folder>")
		return fs
	}
	all, errs := resolve.ListWorkspaces(cfg)
	for _, e := range errs {
		add("error", "manifest-invalid", "", e.Error(), "fix the manifest, then tq allow <name>")
	}
	provs := envplan.Providers()
	seenCLI := map[string]bool{}
	for _, w := range all {
		if !trust.IsTrusted(w.Hash) {
			add("warn", "untrusted", w.Name, "manifest not trusted", "tq allow "+w.Name)
		}
		if w.Manifest.Claude.PermissionMode == "bypass" && w.Manifest.HasCloudIdentity() {
			add("warn", "bypass-cloud", w.Name, "permission_mode bypass with a cloud identity: Claude may run cloud CLIs unattended", "")
		}
		wf := gitcfg.WorkspaceFile(w.Root)
		if raw, err := os.ReadFile(wf); err != nil {
			add("error", "git-ws-file-missing", w.Name, "missing "+wf, "tq add would have created it; re-run tq allow after restoring")
		} else if bad := tamperReason(string(raw)); bad != "" {
			add("error", "git-ws-file-tampered", w.Name, wf+": "+bad, "delete it and re-run tq add, or restore the tq-managed contents")
		}
		for _, id := range w.Manifest.IdentityNames() {
			dir := paths.IdentityDir(w.Name, id)
			if _, err := os.Stat(dir); err != nil {
				add("warn", "identity-dir-missing", w.Name, "missing "+dir, "mkdir it or re-run tq add")
			} else if id == "claude" && runtime.GOOS != "darwin" {
				if _, err := os.Stat(filepath.Join(dir, ".credentials.json")); err != nil {
					add("warn", "claude-not-logged-in", w.Name, "no Claude credentials in "+dir, "tq login "+w.Name+" claude")
				}
			}
			if p, ok := provs[id]; ok && p.LoginCmd != "" && !seenCLI[p.LoginCmd] {
				seenCLI[p.LoginCmd] = true
				if _, err := d.LookPath(p.LoginCmd); err != nil {
					add("warn", "cli-missing", "", p.LoginCmd+" not found on PATH", "install it or drop the identity from the manifest")
				}
			}
		}
	}
	// git global — only meaningful if git is actually installed.
	if _, err := d.LookPath("git"); err != nil {
		add("error", "git-missing", "", "git not found on PATH: tq cannot enforce per-workspace git identity", "install git, then re-run tq doctor")
	} else {
		inc, _ := d.RunGit("config", "--global", "--get-all", "include.path")
		if !strings.Contains(filepath.ToSlash(inc), filepath.ToSlash(gitcfg.IncludeFile())) {
			add("error", "git-include-missing", "", "~/.gitconfig does not include "+gitcfg.IncludeFile(), "tq init <base>")
		}
		if v, _ := d.RunGit("config", "--global", "user.useConfigOnly"); strings.TrimSpace(v) != "true" {
			add("error", "git-useconfigonly", "", "global user.useConfigOnly is not true (commits outside workspaces will use a guessed identity)", "tq init <base>")
		}
	}
	// env vs cwd
	res := resolve.Resolve(d.Cwd, cfg)
	envWS, _ := d.Env("TQ_WS")
	_, hasState := d.Env(envplan.StateVar)
	switch {
	case res.Workspace != nil && envWS != res.Workspace.Name:
		add("error", "env-drift", res.Workspace.Name, fmt.Sprintf("cwd resolves to %s but TQ_WS=%q", res.Workspace.Name, envWS), "open a new shell or run: eval \"$(tq env --shell <shell>)\"")
	case res.Workspace == nil && envWS != "":
		add("error", "env-drift", "", fmt.Sprintf("cwd is neutral (%s) but TQ_WS=%q is still set", res.Reason, envWS), "eval \"$(tq env --shell <shell>)\"")
	}
	if !hasState && envWS == "" && res.Reason != "outside any base" {
		add("warn", "hook-missing", "", "inside a base but no tq state in this shell: is the hook installed?", "tq init prints the profile line")
	}
	if len(fs) == 0 {
		add("ok", "ok", "", "all checks passed", "")
	}
	return fs
}

func Exit(fs []Finding) int {
	for _, f := range fs {
		if f.Level == "error" {
			return 1
		}
	}
	return 0
}

// tamperReason returns a non-empty explanation when a workspace's
// .gitconfig-tentaqles no longer looks like the file tq writes. tq only ever
// writes a "# managed by tq" header and a [user] section; anything else (e.g.
// [core] sshCommand, [alias], [include]) would be executed-as-config by git for
// every repo under that root.
func tamperReason(body string) string {
	if !strings.HasPrefix(body, "# managed by tq") {
		return "does not start with the tq header (hand-edited or replaced)"
	}
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "[") {
			continue
		}
		if t != "[user]" {
			return "contains unexpected git config section " + t
		}
	}
	return ""
}
