// Package codexcfg compiles tq's guard mode into Codex's config (spec E §12),
// leaving everything the user wrote as it was:
//
//   - a marked block of top-level keys (approval_policy, sandbox_mode) at the
//     head of config.toml, for the workspace-wide mode;
//   - rules/tq.rules, tq's own exec-policy file (prompt and forbidden prefix
//     rules); the user's other rules/*.rules files are never touched;
//   - tq's entries in hooks.json: PreToolUse runs `tq claude-hook pre-tool-use
//     --agent codex`, SessionStart `tq claude-hook session-start --agent codex`.
//
// Per-project modes are carried by the launch flags (Plan.Flags), which `tq
// run` computes from the cwd. The old projects-only profile file,
// tq-unattended.config.toml, is removed when tq wrote it.
package codexcfg

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	beginMarker = "# >>> managed by tq (guard mode): edit with tq mode, not by hand"
	endMarker   = "# <<< managed by tq (guard mode)"

	// The unattended-era markers and profile file, read for removal only.
	legacyProfile = "tq-unattended"
	legacyBegin   = "# >>> managed by tq (unattended): edit with tq unattended, not by hand"
	legacyEnd     = "# <<< managed by tq (unattended)"
)

var (
	tableRe  = regexp.MustCompile(`^\s*\[`)
	topKeyRe = regexp.MustCompile(`^\s*(approval_policy|sandbox_mode)\s*=`)
)

// Want is what Render brings a CODEX_HOME in line with. A nil Plan removes
// everything tq wrote.
type Want struct {
	Plan *Plan
	// Hook writes tq's hooks.json entries (only with a Plan).
	Hook bool
}

// Result lists the files Render changed (or would change, on a dry run) and
// the parts it left alone because the user owns them.
type Result struct {
	Changed   []string
	Conflicts []string // "<what>: <why>"
}

func keys(approval, sandbox string) string {
	return fmt.Sprintf("approval_policy = %q\nsandbox_mode = %q\n", approval, sandbox)
}

// strip removes every managed block, current or legacy (marker lines
// included). ok is false when a begin marker has no end marker after it:
// nothing is then safe to remove.
func strip(s string) (string, bool) {
	var out []string
	in := false
	for _, l := range strings.SplitAfter(s, "\n") {
		t := strings.TrimRight(l, "\r\n")
		switch {
		case t == beginMarker || t == legacyBegin:
			in = true
		case t == endMarker || t == legacyEnd:
			in = false
		case !in:
			out = append(out, l)
		}
	}
	return strings.Join(out, ""), !in
}

// ownedProfile reports whether s is exactly the block tq's unattended mode
// wrote into the profile file, for any sandbox, with nothing else in it.
func ownedProfile(s string) bool {
	for _, sb := range []string{"read-only", "workspace-write", "danger-full-access"} {
		if s == legacyBegin+"\n"+keys("never", sb)+legacyEnd+"\n" {
			return true
		}
	}
	return false
}

// OwnsProfile reports whether dir holds a tq-unattended profile file that tq
// wrote and nobody edited.
func OwnsProfile(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, legacyProfile+".config.toml"))
	return err == nil && ownedProfile(string(b))
}

func writeAtomic(p, content string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tq.tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// change is one file operation Render plans: content to write, or remove.
type change struct {
	path    string
	content string
	remove  bool
}

func readOpt(p string) (string, bool, error) {
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(b), true, nil
}

// Render brings dir (a CODEX_HOME) in line with w. Each part is independent:
// a user-owned key or file is a conflict for that part only, and the others
// are still written, so a user approval_policy never keeps tq's hook out.
func Render(dir string, w Want, dryRun bool) (Result, error) {
	var res Result
	var plan []change

	// config.toml: the top-level block.
	p := filepath.Join(dir, "config.toml")
	cur, _, err := readOpt(p)
	if err != nil {
		return res, err
	}
	if rest, ok := strip(cur); !ok {
		res.Conflicts = append(res.Conflicts, "config.toml: tq's managed block in "+p+" has no end marker; fix it by hand")
	} else {
		conflict := ""
		if w.Plan != nil {
			for _, l := range strings.Split(rest, "\n") {
				if tableRe.MatchString(l) {
					break
				}
				if topKeyRe.MatchString(l) {
					conflict = "config.toml already sets " + strings.TrimSpace(strings.SplitN(l, "=", 2)[0]) + "; remove it to let tq manage it (" + p + ")"
					break
				}
			}
		}
		if conflict != "" {
			res.Conflicts = append(res.Conflicts, conflict)
		} else {
			next := rest
			if w.Plan != nil {
				next = beginMarker + "\n" + keys(w.Plan.Approval, w.Plan.Sandbox) + endMarker + "\n" + rest
			}
			if next != cur {
				plan = append(plan, change{path: p, content: next, remove: next == ""})
			}
		}
	}

	// The legacy profile file: removed when tq wrote it, else left alone.
	if OwnsProfile(dir) {
		plan = append(plan, change{path: filepath.Join(dir, legacyProfile+".config.toml"), remove: true})
	}

	// rules/tq.rules.
	rp := filepath.Join(dir, filepath.FromSlash(RulesFile))
	rcur, rexists, err := readOpt(rp)
	if err != nil {
		return res, err
	}
	ours := rexists && strings.HasPrefix(strings.ReplaceAll(rcur, "\r\n", "\n"), rulesBegin+"\n")
	switch {
	case rexists && !ours:
		if w.Plan != nil {
			res.Conflicts = append(res.Conflicts, "tq.rules: "+rp+" exists and is not written by tq; rename it to let tq manage it")
		}
	case w.Plan != nil:
		if next := RenderRules(w.Plan.Mode, w.Plan.Rules); next != rcur {
			plan = append(plan, change{path: rp, content: next})
		}
	case ours:
		plan = append(plan, change{path: rp, remove: true})
	}

	// hooks.json.
	hp := filepath.Join(dir, HooksFile)
	hcur, hexists, err := readOpt(hp)
	if err != nil {
		return res, err
	}
	wantHook := w.Plan != nil && w.Hook
	if hexists || wantHook {
		next, remove, conflict := planHooks([]byte(hcur), hexists, wantHook)
		switch {
		case conflict != "":
			res.Conflicts = append(res.Conflicts, conflict+" ("+hp+")")
		case remove:
			if hexists {
				plan = append(plan, change{path: hp, remove: true})
			}
		case !hexists || !sameJSON([]byte(hcur), next):
			plan = append(plan, change{path: hp, content: string(next)})
		}
	}

	for _, c := range plan {
		res.Changed = append(res.Changed, c.path)
	}
	if dryRun {
		return res, nil
	}
	for _, c := range plan {
		if c.remove {
			if err := os.Remove(c.path); err != nil && !os.IsNotExist(err) {
				return res, err
			}
			continue
		}
		if err := writeAtomic(c.path, c.content); err != nil {
			return res, err
		}
	}
	return res, nil
}
