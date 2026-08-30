package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func init() { register(identityStep{}) }

// identityStep pulls each workspace identity directory that is really a link
// back inside tq's own identities tree, and leaves a junction behind at the old
// location so aliases, shortcuts and half-remembered paths keep working.
//
// Per directory the order is always remove-link, move-dir, make-link, each
// journaled before it happens, so a crash at any point leaves a state Restore
// can walk backwards.
type identityStep struct{}

func (identityStep) Name() string { return "identity" }

// idAction is one identity directory to reclaim: the link at dir currently
// points at target, and after the step target points back at dir.
type idAction struct {
	ws, id string
	// dir is paths.IdentityDir(ws, id) -- the link today, the real directory
	// afterwards.
	dir string
	// target is where the data lives today, and where the junction is left.
	target string
}

func (identityStep) Plan(d Deps) (Plan, error) {
	p, _, err := planIdentity(d)
	return p, err
}

func (identityStep) Apply(d Deps, p Plan, j *Journal) error {
	// Re-derive rather than trust the plan's strings: Change carries only
	// display text, and re-planning also catches a machine whose state moved
	// between the dry run and the apply.
	fresh, acts, err := planIdentity(d)
	if err != nil {
		return err
	}
	if err := sameChanges("identity", p, fresh); err != nil {
		return err
	}
	if err := identityRefusal(d, acts); err != nil {
		return err
	}
	if j == nil {
		return fmt.Errorf("identity: no journal")
	}
	for _, a := range acts {
		if err := j.RecordRemoveLink("identity", a.dir, a.target); err != nil {
			return err
		}
		if err := RemoveLink(a.dir); err != nil {
			return fmt.Errorf("identity %s/%s: %w", a.ws, a.id, err)
		}
		if err := j.RecordMoveDir("identity", a.target, a.dir); err != nil {
			return err
		}
		if err := MoveDir(a.target, a.dir); err != nil {
			return fmt.Errorf("identity %s/%s: %w", a.ws, a.id, err)
		}
		if err := j.RecordMakeLink("identity", a.target, a.dir); err != nil {
			return err
		}
		if err := MakeLink(a.target, a.dir); err != nil {
			return fmt.Errorf("identity %s/%s: %w", a.ws, a.id, err)
		}
	}
	return nil
}

// planIdentity computes the plan and the typed actions behind it. Plan and
// Apply both call it, so what the user reads is what runs.
func planIdentity(d Deps) (Plan, []idAction, error) {
	var p Plan
	if d.Cfg == nil {
		return p, nil, fmt.Errorf("identity: no registry config")
	}
	all, errs := resolve.ListWorkspaces(d.Cfg)
	for _, e := range errs {
		p.Warnings = append(p.Warnings, "workspace scan: "+e.Error())
	}
	var acts []idAction
	// Every identity-directory link target seen, trusted workspace or not. A
	// directory an untrusted workspace points at is not an orphan -- tq is
	// simply not allowed to move it yet.
	var targets []string
	for _, w := range all {
		trusted := trust.IsTrusted(w.Hash)
		if !trusted {
			p.Skipped = append(p.Skipped, fmt.Sprintf("workspace %s: not trusted, so tq will not touch its identity directories (tq allow %s, then re-run)", w.Name, w.Name))
		}
		for _, id := range w.Manifest.IdentityNames() {
			dir := paths.IdentityDir(w.Name, id)
			linked, target := IsLink(dir)
			if !linked {
				continue
			}
			if target != "" {
				targets = append(targets, target)
			}
			if !trusted {
				continue
			}
			if target == "" {
				p.Warnings = append(p.Warnings, fmt.Sprintf("%s is a link whose target tq could not read; move it by hand and re-run", dir))
				p.Skipped = append(p.Skipped, fmt.Sprintf("%s/%s: link target unreadable", w.Name, id))
				continue
			}
			fi, err := os.Stat(target)
			if err != nil {
				p.Warnings = append(p.Warnings, fmt.Sprintf("%s points at %s, which does not exist (dangling link)", dir, target))
				p.Skipped = append(p.Skipped, fmt.Sprintf("%s/%s: link target %s is missing", w.Name, id, target))
				continue
			}
			if !fi.IsDir() {
				p.Skipped = append(p.Skipped, fmt.Sprintf("%s/%s: link target %s is not a directory", w.Name, id, target))
				continue
			}
			if samePath(target, dir) {
				p.Skipped = append(p.Skipped, fmt.Sprintf("%s/%s: link points at itself", w.Name, id))
				continue
			}
			acts = append(acts, idAction{ws: w.Name, id: id, dir: dir, target: target})
		}
	}
	for _, a := range acts {
		p.Changes = append(p.Changes,
			Change{Step: "identity", Kind: kindRemoveLink, Path: a.dir,
				Detail: "link to " + a.target},
			Change{Step: "identity", Kind: kindMoveDir, Path: a.target,
				Detail: "to " + a.dir + " (" + a.ws + "/" + a.id + ")", Danger: true},
			Change{Step: "identity", Kind: kindMakeLink, Path: a.target,
				Detail: "junction back to " + a.dir + ", so the old path keeps working"},
		)
	}
	// Inner junctions inside a moved Claude directory (plugins, agents, skills
	// pointing at ~/.claude/...) are deliberately left alone: today's sharing
	// is intentional, and copying them would fork the user's plugin set.
	if len(acts) > 0 {
		p.Skipped = append(p.Skipped, "junctions *inside* the moved directories (plugins/agents/skills to ~/.claude/...) are left as they are: the sharing is intentional")
	}

	for _, o := range orphanIdentityDirs(d, targets) {
		p.Warnings = append(p.Warnings, fmt.Sprintf("%s looks like a legacy identity directory but no workspace references it; tq leaves it alone", o))
	}
	if blockers := identityBlockers(d, acts); len(blockers) > 0 {
		p.Warnings = append(p.Warnings, "applying will refuse: "+strings.Join(blockers, "; ")+
			" (close them and re-run, or pass --force)")
	}
	return p, acts, nil
}

// identityRefusal turns the blockers into the error Apply stops on. Force
// downgrades it: the user has been told what is running and accepted the risk
// of moving a directory out from under a live process.
func identityRefusal(d Deps, acts []idAction) error {
	if d.Force {
		return nil
	}
	blockers := identityBlockers(d, acts)
	if len(blockers) == 0 {
		return nil
	}
	return fmt.Errorf("refusing to move identity directories while they are in use: %s\nclose them and re-run, or pass --force to move them anyway",
		strings.Join(blockers, "; "))
}

// identityBlockers lists the reasons the identity step would refuse to run.
//
// Two checks, cheapest first. A process whose command line mentions the
// directory being moved is a direct hit. Otherwise a process whose executable
// is the CLI that owns the directory (claude for a claude dir, gh for a gh dir)
// blocks it too: on Windows the command line of another user's process is often
// unreadable, and a running `claude` keeps ~/.claude-<x> open whether or not it
// says so on its command line.
//
// It never returns an error: a machine where the process list cannot be read
// reports one blocker saying so, which --force clears like any other.
func identityBlockers(d Deps, acts []idAction) []string {
	if len(acts) == 0 || d.Processes == nil {
		return nil
	}
	procs, err := d.Processes()
	if err != nil {
		return []string{fmt.Sprintf("tq could not list running processes (%v), so it cannot tell whether these directories are in use", err)}
	}
	var out []string
	seen := map[string]bool{}
	for _, a := range acts {
		for _, line := range procs {
			why := ""
			switch {
			case referencesPath(line, a.target) || referencesPath(line, a.dir):
				why = fmt.Sprintf("a running process references %s: %s", a.target, strings.TrimSpace(line))
			case processIsCLI(line, a.id):
				why = fmt.Sprintf("%s is running, which owns %s: %s", a.id, a.target, strings.TrimSpace(line))
			}
			if why == "" || seen[why] {
				continue
			}
			seen[why] = true
			out = append(out, why)
		}
	}
	sort.Strings(out)
	return out
}

// referencesPath reports whether a process line mentions path. Both sides are
// normalised to forward slashes (a command line may spell a path either way)
// and case-folded on Windows.
func referencesPath(line, path string) bool {
	if path == "" {
		return false
	}
	l := filepath.ToSlash(line)
	p := filepath.ToSlash(filepath.Clean(path))
	if runtime.GOOS == "windows" {
		l, p = strings.ToLower(l), strings.ToLower(p)
	}
	return strings.Contains(l, p)
}

// cliExts are the suffixes a launcher may carry on Windows, plus the .js a
// node-shipped CLI is invoked as.
var cliExts = []string{".exe", ".cmd", ".bat", ".ps1", ".js"}

// processIsCLI reports whether a process line looks like the CLI named id.
//
// A line is "<name> <command line>", so the first field is the executable name
// and is matched directly. Every later field only counts when it actually looks
// like an executable -- it carries a path separator or a launcher suffix -- so
// `C:\...\node.exe C:\...\claude.js` is recognised while a bare mention of the
// word in an argument is not.
//
// That distinction is load-bearing: a plain token match blocks the identity
// step for any process whose command line merely says "claude" (a shell echoing
// the word is enough), and because moving identity directories under a live CLI
// is exactly what this check exists to prevent, the documented answer to a
// false positive is never --force. An unclearable refusal would leave the step
// permanently unusable.
func processIsCLI(line, id string) bool {
	if id == "" {
		return false
	}
	id = strings.ToLower(id)
	for i, f := range strings.Fields(line) {
		f = strings.Trim(f, "\"'")
		slashed := filepath.ToSlash(f)
		base := strings.ToLower(filepath.Base(slashed))
		hadExt := false
		for _, ext := range cliExts {
			if strings.HasSuffix(base, ext) {
				hadExt = true
			}
			base = strings.TrimSuffix(base, ext)
		}
		if base != id {
			continue
		}
		// The process name, or something that spells itself out as a program.
		if i == 0 || hadExt || strings.Contains(slashed, "/") {
			return true
		}
	}
	return false
}

// orphanIdentityDirs finds directories that sit beside the ones workspaces
// point at and look like identity directories nothing references any more --
// ~/.claude-work next to ~/.claude-dbi, or a stale name under
// ~/.cli-identities. targets is every identity-directory link target found,
// including those of untrusted workspaces, so a directory tq is merely not
// allowed to move yet is never reported as abandoned.
//
// The family is derived from the targets themselves rather than hardcoded: for
// a target like ~/.claude-dbi the siblings must share the ".claude-" prefix; for
// one inside a dedicated directory (~/.cli-identities/tentaqles) every sibling
// counts. A prefix-less target sitting directly in the home directory is
// ignored, since "every dotfile in ~" is not a useful warning.
func orphanIdentityDirs(d Deps, targets []string) []string {
	home := stepHome(d)
	known := map[string]bool{}
	for _, t := range targets {
		known[normKey(t)] = true
	}
	type family struct{ dir, prefix string }
	var fams []family
	seenFam := map[string]bool{}
	for _, t := range targets {
		par := filepath.Dir(t)
		base := filepath.Base(t)
		prefix := ""
		if i := strings.LastIndex(base, "-"); i > 0 {
			prefix = base[:i+1]
		}
		if prefix == "" && home != "" && samePath(par, home) {
			continue
		}
		k := normKey(par) + "|" + strings.ToLower(prefix)
		if seenFam[k] {
			continue
		}
		seenFam[k] = true
		fams = append(fams, family{dir: par, prefix: prefix})
	}
	var out []string
	seen := map[string]bool{}
	for _, f := range fams {
		ents, err := os.ReadDir(f.dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if f.prefix != "" && !strings.HasPrefix(strings.ToLower(e.Name()), strings.ToLower(f.prefix)) {
				continue
			}
			p := filepath.Join(f.dir, e.Name())
			if known[normKey(p)] || seen[normKey(p)] {
				continue
			}
			if linked, _ := IsLink(p); linked {
				continue
			}
			if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
				continue
			}
			seen[normKey(p)] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// stepHome resolves the user's home directory, preferring the injected Env so a
// test (and a caller with a synthetic environment) cannot reach the real one.
func stepHome(d Deps) string {
	get := d.Env
	if get == nil {
		get = os.LookupEnv
	}
	if runtime.GOOS == "windows" {
		if v, ok := get("USERPROFILE"); ok && v != "" {
			return v
		}
	}
	if v, ok := get("HOME"); ok && v != "" {
		return v
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// normKey makes two spellings of a path comparable as a map key.
func normKey(p string) string {
	p = filepath.ToSlash(filepath.Clean(strings.TrimSpace(p)))
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// sameChanges refuses to apply a plan that no longer matches the machine.
//
// Apply re-derives its work from Deps rather than parsing the display strings
// in Change, so this is the guard that the two agree: if anything moved between
// the dry run the user read and the apply they authorised, the step stops
// before touching anything instead of doing something they did not see.
func sameChanges(step string, shown, fresh Plan) error {
	sig := func(p Plan) []string {
		out := make([]string, 0, len(p.Changes))
		for _, c := range p.Changes {
			out = append(out, c.Kind+"\x00"+c.Path+"\x00"+c.Detail)
		}
		return out
	}
	a, b := sig(shown), sig(fresh)
	if len(a) != len(b) {
		return fmt.Errorf("%s: the machine changed since the plan was computed (%d planned changes, %d now); re-run tq migrate", step, len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			return fmt.Errorf("%s: the machine changed since the plan was computed (%q became %q); re-run tq migrate",
				step, strings.ReplaceAll(a[i], "\x00", " "), strings.ReplaceAll(b[i], "\x00", " "))
		}
	}
	return nil
}
