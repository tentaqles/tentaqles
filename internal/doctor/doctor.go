// Package doctor verifies expected vs actual identity state. It never mutates.
package doctor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/detect"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
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
	// ReadFile reads a file the checks need to inspect (a PATH shim, say).
	// Optional; nil falls back to os.ReadFile.
	ReadFile func(string) ([]byte, error)
	// RunCLI runs a provider's verify command with the workspace's identity
	// in env. Optional; nil disables account verification entirely.
	//
	// This shells out, so it MUST stay off tq's hot paths: RunForCwd backs
	// the pre-tool-use hook that fires on every Bash command an agent runs,
	// and a subprocess there would be felt on every keystroke of work.
	RunCLI func(ctx context.Context, env []string, name string, args []string) (string, error)
	// Environ builds the child environment carrying a workspace's identity.
	// Optional; nil falls back to the real process environment plus the
	// workspace's variables.
	Environ func(*resolve.Workspace) []string
	// VerifyMode is VerifyAuto (default), VerifyAll or VerifyOff.
	VerifyMode string
}

func Run(cfg *registry.Config, d Deps) []Finding {
	var fs []Finding
	add := func(level, code, ws, msg, fix string) { fs = append(fs, Finding{level, code, ws, msg, fix}) }

	if len(cfg.Bases) == 0 {
		providerFileFindings(add, nil)
		add("error", "no-bases", "", "no clients yet", "tentaqles reach <client folder>")
		return fs
	}
	all, errs := resolve.ListWorkspaces(cfg)
	providerFileFindings(add, all)
	for _, e := range errs {
		add("error", "manifest-invalid", "", e.Error(), "fix the client's manifest under ~/.tentaqles/identities")
	}
	provs := envplan.Providers()
	provCat := providers.MustLoad()
	seenCLI := map[string]bool{}
	detectDeps := detect.Deps{
		LookPath: d.LookPath,
		Run:      func(context.Context, string, ...string) (string, error) { return "", nil },
		GOOS:     runtime.GOOS,
	}
	// Registered workspaces' .gitconfig-tentaqles paths, so the global-config
	// section below can tell a hand-written includeIf that duplicates tq's
	// managed include file from an unrelated one.
	wsGitFile := map[string]string{}
	pythonStoreAlias(d.LookPath, runtime.GOOS, add)
	for _, w := range all {
		browserChecks(&w, add)
		claudeChecks(&w, add)
		guardHookCheck(&w, add)
		runWorkspaceChecks(&w, d, add)
		wf := gitcfg.WorkspaceFile(w.Root)
		wsGitFile[normPath(wf)] = w.Name
		if raw, err := os.ReadFile(wf); err != nil {
			add("error", "git-ws-file-missing", w.Name, "missing "+wf, "tentaqles reach "+w.Root+" rewrites it")
		} else if bad := tamperReason(string(raw)); bad != "" {
			add("error", "git-ws-file-tampered", w.Name, wf+": "+bad, "delete it, then tentaqles reach "+w.Root+" rewrites it")
		}
		for _, id := range w.Manifest.IdentityNames() {
			dir := paths.IdentityDir(w.Name, id)
			if _, err := os.Stat(dir); err != nil {
				add("warn", "identity-dir-missing", w.Name, "missing "+dir, "mkdir it")
			} else if id == "claude" && runtime.GOOS != "darwin" {
				if _, err := os.Stat(filepath.Join(dir, ".credentials.json")); err != nil {
					add("warn", "claude-not-logged-in", w.Name, "no Claude credentials in "+dir, "tq login "+w.Name+" claude")
				}
			}
			if p, ok := provs[id]; ok && p.LoginCmd != "" && !seenCLI[p.LoginCmd] {
				seenCLI[p.LoginCmd] = true
				fix := "install it or drop the provider from the client"
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
				// A script earlier on PATH than the real CLI can overwrite the
				// identity tq just exported, which makes `tq run` and
				// `tq login` act on the wrong workspace without saying so.
				if resolved, err := d.LookPath(p.LoginCmd); err == nil {
					if why, shadowed := shimShadow(resolved, identityVars(p), d.ReadFile); shadowed {
						add("warn", "cli-shadowed", "",
							p.LoginCmd+" on PATH resolves to "+resolved+", and "+why+
								": that overrides the identity tq exports, so tq run and tq login can act on the wrong workspace",
							"put the real "+p.LoginCmd+" ahead of "+filepath.Dir(resolved)+" on PATH, or remove that directory")
					}
				}
			}
		}
	} // git global — only meaningful if git is actually installed.
	if _, err := d.LookPath("git"); err != nil {
		add("error", "git-missing", "", "git not found on PATH: tq cannot enforce per-workspace git identity", "install git, then re-run tq doctor")
	} else {
		inc, _ := d.RunGit("config", "--global", "--get-all", "include.path")
		if !strings.Contains(filepath.ToSlash(inc), filepath.ToSlash(gitcfg.IncludeFile())) {
			add("error", "git-include-missing", "", "~/.gitconfig does not include "+gitcfg.IncludeFile(), "git config --global --add include.path "+gitcfg.IncludeFile())
		}
		if v, _ := d.RunGit("config", "--global", "user.useConfigOnly"); strings.TrimSpace(v) != "true" {
			add("error", "git-useconfigonly", "", "global user.useConfigOnly is not true (commits outside client folders will use a guessed identity)", "git config --global user.useConfigOnly true")
		}
		if email, present, _ := gitcfg.GetGlobal(d.RunGit, "user.email"); present {
			add("error", "global-email-set", "", "global user.email is set: commits outside workspaces will silently use "+email, "remove it from ~/.gitconfig")
		}
		includeIfs, _ := gitcfg.ListIncludeIf(d.RunGit)
		for _, inc := range includeIfs {
			if ws, ok := wsGitFile[normPath(gitcfg.ExpandPath(inc.Path))]; ok {
				add("warn", "includeif-unmanaged", ws, "~/.gitconfig has a hand-written includeIf \""+inc.Cond+"\" → "+inc.Path+", which tq's include file already covers", "remove it from ~/.gitconfig")
			}
		}
		includes, _ := gitcfg.ListIncludes(d.RunGit)
		for _, inc := range includeIfs {
			includes = append(includes, inc.Path)
		}
		seenInc := map[string]bool{}
		for _, p := range includes {
			ep := gitcfg.ExpandPath(p)
			if ep == "" || seenInc[normPath(ep)] {
				continue
			}
			seenInc[normPath(ep)] = true
			if _, err := os.Stat(ep); err != nil {
				add("warn", "include-orphan", "", "global git config includes "+p+", which does not exist", "remove it from ~/.gitconfig")
			}
		}
	}
	runGlobalChecks(d, add)
	// env vs cwd — shared with tq claude-hook so both agree. RunForCwd is the
	// hook path; it deliberately does not verify.
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
	var names []string
	for _, w := range all {
		names = append(names, w.Name)
	}
	pluginSkew(names, add)
	pluginForeignPath(names, add)
	return finish(fs)
}

func Exit(fs []Finding) int {
	for _, f := range fs {
		if f.Level == "error" {
			return 1
		}
	}
	return 0
}

// normPath makes two spellings of the same path comparable as map keys:
// cleaned, forward slashes, case-folded on Windows.
func normPath(p string) string {
	p = filepath.ToSlash(filepath.Clean(strings.TrimSpace(p)))
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
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

// finish adds "all checks passed" when nothing but ok findings were reported.
func finish(fs []Finding) []Finding {
	for _, f := range fs {
		if f.Level != "ok" {
			return fs
		}
	}
	return append(fs, Finding{"ok", "ok", "", "all checks passed", ""})
}
