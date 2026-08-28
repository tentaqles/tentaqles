// Package doctor verifies expected vs actual identity state. It never mutates.
package doctor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/cli/internal/bundle"
	"github.com/tentaqles/tentaqles/cli/internal/detect"
	"github.com/tentaqles/tentaqles/cli/internal/envplan"
	"github.com/tentaqles/tentaqles/cli/internal/gitcfg"
	"github.com/tentaqles/tentaqles/cli/internal/paths"
	"github.com/tentaqles/tentaqles/cli/internal/providers"
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
	Env    func(string) (string, bool)
	Cwd    string
	RunGit func(args ...string) (string, error)
	// RunGitIn runs git inside dir. Optional; nil falls back to RunGit, which
	// runs in the process cwd.
	RunGitIn func(dir string, args ...string) (string, error)
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
	provCat := providers.MustLoad()
	seenCLI := map[string]bool{}
	detectDeps := detect.Deps{
		LookPath: d.LookPath,
		Run:      func(context.Context, string, ...string) (string, error) { return "", nil },
		GOOS:     runtime.GOOS,
	}
	var cat *bundle.Catalog
	catalogWarned := false
	for _, w := range all {
		trusted := trust.IsTrusted(w.Hash)
		if !trusted {
			add("warn", "untrusted", w.Name, "manifest not trusted", "tq allow "+w.Name)
		}
		if trusted && w.Manifest.Claude.Bundle != nil {
			if cat == nil {
				c, err := bundle.LoadCatalog()
				if err != nil {
					add("error", "bundle-catalog-error", w.Name, err.Error(), "")
				} else {
					cat = c
				}
			}
			if cat != nil {
				ww := w
				if drifts, err := bundle.Diff(&ww, cat); err != nil {
					add("error", "bundle-diff-error", w.Name, err.Error(), "")
				} else if len(drifts) > 0 {
					parts := make([]string, len(drifts))
					for i, dr := range drifts {
						parts[i] = dr.Kind + ":" + dr.Name
					}
					add("warn", "bundle-drift", w.Name, "bundle drift: "+strings.Join(parts, ", "), "tq bundle sync "+w.Name)
				}
				if !catalogWarned {
					catalogWarned = true
					for _, cw := range cat.Validate() {
						add("warn", "bundle-catalog-warning", "", cw, "")
					}
				}
			}
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
				fix := "install it or drop the identity from the manifest"
				if cp, ok := provCat.Get(id); ok && cp.CLI != nil {
					if r := detect.Check(cp, detectDeps); !r.Installed {
						if hints := detect.Hints(cp, runtime.GOOS); len(hints) > 0 {
							fix = hints[0]
						}
						add("warn", "cli-missing", "", p.LoginCmd+" not found on PATH", fix)
					}
				} else if _, err := d.LookPath(p.LoginCmd); err != nil {
					add("warn", "cli-missing", "", p.LoginCmd+" not found on PATH", fix)
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
	// env vs cwd — shared with tq claude-hook so both agree.
	cwd := RunForCwd(cfg, d)
	// Run already walks every workspace, so a cwd finding may repeat one it
	// emitted (untrusted, notably). Dedupe on code+workspace.
	seen := make(map[[2]string]bool, len(fs))
	for _, f := range fs {
		seen[[2]string{f.Code, f.Workspace}] = true
	}
	for _, f := range cwd.Findings {
		k := [2]string{f.Code, f.Workspace}
		if seen[k] {
			continue
		}
		seen[k] = true
		fs = append(fs, f)
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
