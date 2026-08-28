package doctor

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/cli/internal/envplan"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
	"github.com/tentaqles/tentaqles/cli/internal/resolve"
)

// CwdReport is the doctor view scoped to a single directory: what tq resolves
// there, what is wrong with the current shell/git state for it, and the git
// email actually in effect. It is the input the claude-hook guard consumes.
type CwdReport struct {
	Result      resolve.Result
	Findings    []Finding
	ActualEmail string
}

// Codes returns the finding codes, in order.
func (r CwdReport) Codes() []string {
	out := make([]string, 0, len(r.Findings))
	for _, f := range r.Findings {
		out = append(out, f.Code)
	}
	return out
}

// RunForCwd computes the cwd-scoped findings: untrusted, env-drift,
// hook-missing, git-email-drift and claude-config-drift. It performs no I/O
// beyond what Deps provides plus the registry/manifest reads resolve does.
func RunForCwd(cfg *registry.Config, d Deps) CwdReport {
	rep := CwdReport{Result: resolve.Resolve(d.Cwd, cfg)}
	add := func(level, code, ws, msg, fix string) {
		rep.Findings = append(rep.Findings, Finding{level, code, ws, msg, fix})
	}
	res := rep.Result
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
	if res.Workspace == nil {
		if name, ok := untrustedWorkspace(res.Reason); ok {
			add("warn", "untrusted", name, "manifest not trusted", "tq allow "+name)
		}
		return rep
	}

	ws := res.Workspace
	// git email drift — only meaningful when the manifest pins one and git exists.
	if want := strings.TrimSpace(ws.Manifest.Git.Email); want != "" {
		if _, err := d.LookPath("git"); err == nil {
			rep.ActualEmail = strings.TrimSpace(runGitIn(d, ws.Root, "config", "user.email"))
			if rep.ActualEmail != "" && !strings.EqualFold(rep.ActualEmail, want) {
				add("error", "git-email-drift", ws.Name, fmt.Sprintf("git user.email in %s is %q but the manifest pins %q", ws.Root, rep.ActualEmail, want), "tq doctor / re-run tq add to restore .gitconfig-tentaqles")
			}
		}
	}
	// claude config dir drift — only when the workspace has a claude identity.
	if want := envplan.Desired(ws)["CLAUDE_CONFIG_DIR"]; want != "" {
		got, ok := d.Env("CLAUDE_CONFIG_DIR")
		switch {
		case ok && !samePath(got, want):
			add("error", "claude-config-drift", ws.Name, fmt.Sprintf("CLAUDE_CONFIG_DIR=%q but %s expects %q", got, ws.Name, want), "eval \"$(tq env --shell <shell>)\"")
		case !ok && envWS == ws.Name:
			add("error", "claude-config-drift", ws.Name, fmt.Sprintf("TQ_WS=%s but CLAUDE_CONFIG_DIR is unset (expected %q)", ws.Name, want), "eval \"$(tq env --shell <shell>)\"")
		}
	}
	return rep
}

// runGitIn prefers RunGitIn (git run inside dir); when it is nil it falls back
// to RunGit, which runs in the process cwd. Errors collapse to "".
func runGitIn(d Deps, dir string, args ...string) string {
	var (
		out string
		err error
	)
	switch {
	case d.RunGitIn != nil:
		out, err = d.RunGitIn(dir, args...)
	case d.RunGit != nil:
		out, err = d.RunGit(args...)
	default:
		return ""
	}
	if err != nil {
		return ""
	}
	return out
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// untrustedWorkspace extracts the name out of resolve's untrusted reason,
// which reads: untrusted (run: tq allow <name>).
func untrustedWorkspace(reason string) (string, bool) {
	const pfx = "untrusted (run: tq allow "
	if !strings.HasPrefix(reason, pfx) {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(reason, pfx), ")"), true
}
