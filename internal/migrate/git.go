package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func init() { register(gitStep{}) }

// Change.Kind values the identity and git steps emit. The command layer prints
// them verbatim, so they are part of tq's output contract; stepChangeKinds is
// the complete set, kept next to the constants so a new kind cannot be added
// without noticing that `tq migrate --json` gains a value.
const (
	kindRemoveLink      = "remove-link"
	kindMoveDir         = "move-dir"
	kindMakeLink        = "make-link"
	kindUnsetGlobal     = "unset-global"
	kindSetGlobal       = "set-global"
	kindRemoveIncludeIf = "remove-includeif"
	kindRemoveInclude   = "remove-include"
	kindAddInclude      = "add-include"
	kindRewriteWSFile   = "rewrite-ws-file"
	kindSyncIncludeFile = "sync-include-file"
)

// managedFileName is the per-workspace file tq writes, named here only for
// messages; gitcfg owns the real definition.
var managedFileName = filepath.Base(gitcfg.WorkspaceFile("x"))

var stepChangeKinds = []string{
	kindRemoveLink, kindMoveDir, kindMakeLink,
	kindUnsetGlobal, kindSetGlobal, kindRemoveIncludeIf, kindRemoveInclude,
	kindAddInclude, kindRewriteWSFile, kindSyncIncludeFile,
}

// gitStep takes the hand-rolled git identity setup -- a global user.email, one
// includeIf per workspace written straight into ~/.gitconfig, workspace files
// edited by hand -- and replaces it with the arrangement tq maintains: no
// global email, one managed include file, one tq-written file per workspace.
//
// It is conservative by design. Only a *registered and trusted* workspace's
// includeIf is removed, because only those end up in tq's include file; an
// includeIf for a nested repo, an unregistered directory, or a workspace the
// user has not run `tq allow` on is left exactly where it is, with its reason
// recorded in Skipped.
type gitStep struct{}

func (gitStep) Name() string { return "git" }

// wsRewrite is one workspace .gitconfig-tentaqles to (re)write, with the
// identity it will carry and where that identity came from.
type wsRewrite struct {
	ws, root, file string
	name, email    string
	src            string
}

// gitActions is the typed work behind the git plan.
type gitActions struct {
	// globalPath is the file `git config --global` writes to. It is backed up
	// once, before the first edit, which is the exact inverse of every
	// key-level change below.
	globalPath string
	// email is the global user.email being removed, when there is one.
	email        string
	emailPresent bool
	rmIncludeIf  []gitcfg.IncludeIf
	rmInclude    []string
	rewrites     []wsRewrite
	syncRoots    []string
	syncNames    []string
}

func (gitStep) Plan(d Deps) (Plan, error) {
	p, _, err := planGit(d)
	return p, err
}

func (gitStep) Apply(d Deps, p Plan, j *Journal) error {
	fresh, a, err := planGit(d)
	if err != nil {
		return err
	}
	if err := sameChanges("git", p, fresh); err != nil {
		return err
	}
	if j == nil {
		return fmt.Errorf("git: no journal")
	}

	// Every edit below goes through `git config --global`, which rewrites this
	// one file. Backing it up first -- and recording that backup before the
	// first edit -- gives the whole step a single byte-exact inverse, which the
	// per-key entries could not: git has no "undo one --add of a multi-valued
	// key", and reversing `--add include.path` with `--unset-all` would take
	// the user's own includes with it.
	b, err := j.BackupFile(a.globalPath)
	if err != nil {
		return err
	}
	if err := j.RecordWriteFile("git", a.globalPath, b); err != nil {
		return err
	}

	if a.emailPresent {
		// Recorded as well as covered by the file backup, so the journal names
		// the identity that was removed instead of hiding it inside a blob.
		if err := j.RecordGitGlobalSet("git", "user.email", a.email, "", true); err != nil {
			return err
		}
		if err := gitcfg.UnsetGlobal(d.Git, "user.email"); err != nil {
			return err
		}
	}
	for _, inc := range a.rmIncludeIf {
		if err := gitcfg.RemoveIncludeIf(d.Git, inc); err != nil {
			return err
		}
	}
	for _, path := range a.rmInclude {
		if err := gitcfg.RemoveInclude(d.Git, path); err != nil {
			return err
		}
	}
	for _, r := range a.rewrites {
		rb, err := j.BackupFile(r.file)
		if err != nil {
			return err
		}
		if err := j.RecordWriteFile("git", r.file, rb); err != nil {
			return err
		}
		if err := gitcfg.WriteWorkspace(r.root, r.name, r.email); err != nil {
			return fmt.Errorf("git: %s: %w", r.ws, err)
		}
	}
	inc := gitcfg.IncludeFile()
	ib, err := j.BackupFile(inc)
	if err != nil {
		return err
	}
	if err := j.RecordWriteFile("git", inc, ib); err != nil {
		return err
	}
	if err := gitcfg.Sync(a.syncRoots); err != nil {
		return err
	}
	// EnsureGlobal is idempotent, so it runs whether or not the plan listed
	// add-include/set-global; the file backup already covers both.
	if err := gitcfg.EnsureGlobal(d.Git); err != nil {
		return err
	}
	return nil
}

// planGit computes the plan and the typed actions behind it.
func planGit(d Deps) (Plan, *gitActions, error) {
	var p Plan
	if d.Cfg == nil {
		return p, nil, fmt.Errorf("git: no registry config")
	}
	if d.Git == nil {
		return p, nil, fmt.Errorf("git: no git runner")
	}
	a := &gitActions{globalPath: globalConfigPath(d)}

	all, errs := resolve.ListWorkspaces(d.Cfg)
	for _, e := range errs {
		p.Warnings = append(p.Warnings, "workspace scan: "+e.Error())
	}
	// Every registered workspace's .gitconfig-tentaqles, so a hand-written
	// includeIf can be told apart from one pointing somewhere else entirely.
	type wsRef struct {
		name    string
		root    string
		trusted bool
	}
	byFile := map[string]wsRef{}
	var trusted []resolve.Workspace
	for _, w := range all {
		ok := trust.IsTrusted(w.Hash)
		byFile[normKey(gitcfg.WorkspaceFile(w.Root))] = wsRef{name: w.Name, root: w.Root, trusted: ok}
		if ok {
			trusted = append(trusted, w)
			a.syncRoots = append(a.syncRoots, w.Root)
			a.syncNames = append(a.syncNames, w.Name)
		} else {
			p.Skipped = append(p.Skipped, fmt.Sprintf("workspace %s: not trusted, so tq's include file will not cover it (tq allow %s, then re-run)", w.Name, w.Name))
		}
	}

	// (1) The global email. user.name stays: git needs a name, and
	// useConfigOnly is what stops git guessing an address outside a workspace.
	email, present, err := gitcfg.GetGlobal(d.Git, "user.email")
	if err != nil {
		return p, nil, err
	}
	if present {
		a.email, a.emailPresent = email, true
		p.Changes = append(p.Changes, Change{
			Step: "git", Kind: kindUnsetGlobal, Path: "user.email",
			Detail: fmt.Sprintf("%q -> (unset); with user.useConfigOnly, git refuses to commit outside a workspace instead of guessing", email),
		})
	}
	name, namePresent, err := gitcfg.GetGlobal(d.Git, "user.name")
	if err != nil {
		return p, nil, err
	}
	if namePresent {
		p.Skipped = append(p.Skipped, fmt.Sprintf("global user.name (%q) is kept: only the email picks the wrong identity, and git still wants a name", name))
	} else {
		p.Skipped = append(p.Skipped, "global user.name is not set and tq does not add one; only user.email is removed")
	}

	// (2)(3) includeIf blocks written by hand into ~/.gitconfig.
	includeIfs, err := gitcfg.ListIncludeIf(d.Git)
	if err != nil {
		return p, nil, err
	}
	for _, inc := range includeIfs {
		ep := gitcfg.ExpandPath(inc.Path)
		if ep == "" {
			p.Skipped = append(p.Skipped, fmt.Sprintf("includeIf %q has an empty path; left alone", inc.Cond))
			continue
		}
		ref, known := byFile[normKey(ep)]
		switch {
		case known && ref.trusted:
			a.rmIncludeIf = append(a.rmIncludeIf, inc)
			p.Changes = append(p.Changes, Change{
				Step: "git", Kind: kindRemoveIncludeIf, Path: inc.Path,
				Detail: fmt.Sprintf("includeIf %q for workspace %s; tq's own include file already covers it", inc.Cond, ref.name),
			})
		case known:
			p.Skipped = append(p.Skipped, fmt.Sprintf("includeIf %q -> %s: workspace %s is not trusted, so removing it would leave that repo with no identity at all", inc.Cond, inc.Path, ref.name))
		default:
			if _, err := os.Stat(ep); err != nil {
				a.rmIncludeIf = append(a.rmIncludeIf, inc)
				p.Changes = append(p.Changes, Change{
					Step: "git", Kind: kindRemoveIncludeIf, Path: inc.Path,
					Detail: fmt.Sprintf("includeIf %q points at a file that does not exist", inc.Cond),
				})
				continue
			}
			p.Skipped = append(p.Skipped, fmt.Sprintf("includeIf %q -> %s: not a registered workspace's %s (a nested repo, or a directory tq does not manage), so tq leaves it to you", inc.Cond, inc.Path, managedFileName))
		}
	}

	// (3) plain include.path entries whose target is gone.
	includes, err := gitcfg.ListIncludes(d.Git)
	if err != nil {
		return p, nil, err
	}
	managed := normKey(gitcfg.IncludeFile())
	for _, path := range includes {
		ep := gitcfg.ExpandPath(path)
		if ep == "" || normKey(ep) == managed {
			continue // tq's own file; step (5) writes it.
		}
		if _, err := os.Stat(ep); err != nil {
			a.rmInclude = append(a.rmInclude, path)
			p.Changes = append(p.Changes, Change{
				Step: "git", Kind: kindRemoveInclude, Path: path,
				Detail: "~/.gitconfig includes a file that does not exist",
			})
			continue
		}
		p.Skipped = append(p.Skipped, fmt.Sprintf("include.path %s exists and is not tq's; left alone", path))
	}

	// (4) workspace files that are missing or no longer tq's.
	for _, w := range trusted {
		file := gitcfg.WorkspaceFile(w.Root)
		raw, readErr := os.ReadFile(file)
		if readErr == nil && wsFileTamper(string(raw)) == "" {
			continue
		}
		r := wsRewrite{ws: w.Name, root: w.Root, file: file}
		mName, mEmail := w.Manifest.Git.Name, w.Manifest.Git.Email
		if readErr == nil {
			fName, fEmail, err := gitcfg.ParseUserSection(file)
			if err != nil {
				return p, nil, err
			}
			// The file is what git uses today. Preserve it, and say so when it
			// disagrees with the manifest rather than silently picking one.
			r.name, r.email, r.src = fName, fEmail, "the existing file"
			if r.name == "" || r.email == "" {
				r.name, r.email, r.src = pick(fName, mName), pick(fEmail, mEmail), "the file, filled in from the manifest"
			}
			if (fName != "" && mName != "" && fName != mName) || (fEmail != "" && mEmail != "" && fEmail != mEmail) {
				p.Warnings = append(p.Warnings, fmt.Sprintf(
					"%s: %s is hand-written and uses %s <%s>, while the manifest says %s <%s>; tq keeps what git uses today -- fix one of them so they agree",
					w.Name, file, fName, fEmail, mName, mEmail))
			} else {
				p.Warnings = append(p.Warnings, fmt.Sprintf("%s: %s is not tq-managed (%s); tq rewrites it, keeping %s <%s>",
					w.Name, file, wsFileTamper(string(raw)), r.name, r.email))
			}
		} else {
			r.name, r.email, r.src = mName, mEmail, "the manifest"
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s: %s is missing; tq recreates it from the manifest", w.Name, file))
		}
		if r.name == "" || r.email == "" {
			p.Skipped = append(p.Skipped, fmt.Sprintf("%s: neither %s nor the manifest has a git name and email, so tq cannot write one", w.Name, file))
			continue
		}
		a.rewrites = append(a.rewrites, r)
		p.Changes = append(p.Changes, Change{
			Step: "git", Kind: kindRewriteWSFile, Path: file, Danger: true,
			Detail: fmt.Sprintf("user.name = %q, user.email = %q (from %s)", r.name, r.email, r.src),
		})
	}

	// (5) tq's own include file, and the two global keys that activate it.
	detail := "no trusted workspaces"
	if len(a.syncNames) > 0 {
		detail = fmt.Sprintf("%d workspace(s): %s", len(a.syncNames), strings.Join(a.syncNames, ", "))
	}
	p.Changes = append(p.Changes, Change{
		Step: "git", Kind: kindSyncIncludeFile, Path: gitcfg.IncludeFile(), Detail: detail,
	})
	hasManaged := false
	for _, path := range includes {
		if normKey(gitcfg.ExpandPath(path)) == managed {
			hasManaged = true
			break
		}
	}
	if !hasManaged {
		p.Changes = append(p.Changes, Change{
			Step: "git", Kind: kindAddInclude, Path: gitcfg.IncludeFile(),
			Detail: "added to ~/.gitconfig as include.path",
		})
	}
	uco, ucoPresent, err := gitcfg.GetGlobal(d.Git, "user.useConfigOnly")
	if err != nil {
		return p, nil, err
	}
	if strings.TrimSpace(uco) != "true" {
		was := "(unset)"
		if ucoPresent {
			was = fmt.Sprintf("%q", uco)
		}
		p.Changes = append(p.Changes, Change{
			Step: "git", Kind: kindSetGlobal, Path: "user.useConfigOnly",
			Detail: was + ` -> "true"; git refuses to guess an identity outside a workspace`,
		})
	}
	return p, a, nil
}

func pick(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// wsFileTamper returns a non-empty explanation when a workspace's
// .gitconfig-tentaqles no longer looks like the file tq writes.
//
// It is deliberately a copy of doctor's identical check rather than a call into
// it: internal/doctor must stay importable from migrate's *tests* (which assert
// that a migration clears doctor's findings), and importing it here would make
// that a cycle. The two must agree, so if one changes the other has to.
func wsFileTamper(body string) string {
	if !strings.HasPrefix(body, "# managed by tq") {
		return "does not start with the tq header, so it was hand-edited or replaced"
	}
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "[") {
			continue
		}
		if t != "[user]" {
			return "contains the git config section " + t + ", which tq never writes"
		}
	}
	return ""
}

// globalConfigPath returns the file `git config --global` reads and writes,
// following git's own precedence: $GIT_CONFIG_GLOBAL wins outright, then
// ~/.gitconfig if it exists, then $XDG_CONFIG_HOME/git/config. When none
// exists, ~/.gitconfig is where git would create one.
//
// It is resolved from the environment rather than by asking git, so the answer
// is the same for a real runner and a fake one -- and the file that gets backed
// up before the step edits anything is never in doubt.
func globalConfigPath(d Deps) string {
	get := d.Env
	if get == nil {
		get = os.LookupEnv
	}
	if v, ok := get("GIT_CONFIG_GLOBAL"); ok && strings.TrimSpace(v) != "" {
		return v
	}
	home := stepHome(d)
	dot := filepath.Join(home, ".gitconfig")
	if _, err := os.Stat(dot); err == nil {
		return dot
	}
	xdg := ""
	if v, ok := get("XDG_CONFIG_HOME"); ok && strings.TrimSpace(v) != "" {
		xdg = v
	} else if home != "" {
		xdg = filepath.Join(home, ".config")
	}
	if xdg != "" {
		q := filepath.Join(xdg, "git", "config")
		if _, err := os.Stat(q); err == nil {
			return q
		}
	}
	return dot
}
