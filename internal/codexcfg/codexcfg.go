// Package codexcfg renders tq's Codex hook into a CODEX_HOME, leaving
// everything the user wrote as it was: tq's entries in hooks.json
// (PreToolUse runs `tq claude-hook pre-tool-use --agent codex`, SessionStart
// `tq claude-hook session-start --agent codex`). tq writes nothing into
// config.toml.
//
// What older tq versions wrote is removed when tq owns it: the marked
// config.toml blocks (the current marker and the unattended and guard-mode
// ones; keys outside a marked block are the user's and stay), the guard-mode
// exec-policy file rules/tq.rules and the unattended profile file
// tq-unattended.config.toml.
package codexcfg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	beginMarker = "# >>> managed by tq: edit with tq settings render, not by hand"
	endMarker   = "# <<< managed by tq"

	// The markers and files of older tq versions, read for removal only.
	legacyProfile   = "tq-unattended"
	legacyBegin     = "# >>> managed by tq (unattended): edit with tq unattended, not by hand"
	legacyEnd       = "# <<< managed by tq (unattended)"
	modeBegin       = "# >>> managed by tq (guard mode): edit with tq mode, not by hand"
	modeEnd         = "# <<< managed by tq (guard mode)"
	legacyRulesFile = "rules/tq.rules"
	legacyRulesHead = modeBegin + "\n"
)

// Want is what Render brings a CODEX_HOME in line with. The zero Want
// removes everything tq wrote.
type Want struct {
	// Hook writes tq's hooks.json entries; false removes them.
	Hook bool
}

// Result lists the files Render changed (or would change, on a dry run) and
// the parts it left alone because the user owns them.
type Result struct {
	Changed   []string
	Conflicts []string // "<what>: <why>"
}

// legacyProfileKeys is the content of the unattended profile file an older
// tq wrote, read only to recognise that file.
func legacyProfileKeys(sandbox string) string {
	return fmt.Sprintf("approval_policy = %q\nsandbox_mode = %q\n", "never", sandbox)
}

// strip removes every managed block, current or legacy (marker lines
// included). A marker matches with its surrounding white space ignored (an
// editor's trailing space must not leave tq's approval_policy behind). ok
// is false when a begin marker has no end marker after it, or when a line
// naming tq's block ("managed by tq") is left that no marker form matches:
// nothing is then safe to remove, and the render reports a conflict.
func strip(s string) (string, bool) {
	var out []string
	in := false
	for _, l := range strings.SplitAfter(s, "\n") {
		t := strings.Join(strings.Fields(l), " ")
		switch {
		case t == beginMarker || t == legacyBegin || t == modeBegin:
			in = true
		case t == endMarker || t == legacyEnd || t == modeEnd:
			if !in {
				return s, false // an end marker with no begin: an edited block
			}
			in = false
		case !in:
			if strings.Contains(t, "managed by tq") {
				return s, false
			}
			out = append(out, l)
		}
	}
	return strings.Join(out, ""), !in
}

// ownedProfile reports whether s is exactly the block tq's unattended mode
// wrote into the profile file, for any sandbox, with nothing else in it.
func ownedProfile(s string) bool {
	for _, sb := range []string{"read-only", "workspace-write", "danger-full-access"} {
		if s == legacyBegin+"\n"+legacyProfileKeys(sb)+legacyEnd+"\n" {
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

// stripLegacyRules reports whether s is a rules file tq wrote (it starts with
// tq's marker) and returns what is left once tq's block is removed: "" when
// the block was the whole file, otherwise the user's lines that followed it.
// A file without a closing marker counts as tq's block in full.
func stripLegacyRules(s string) (string, bool) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasPrefix(s, legacyRulesHead) {
		return "", false
	}
	end := strings.Index(s, modeEnd+"\n")
	if end < 0 {
		return "", true
	}
	rest := strings.TrimLeft(s[end+len(modeEnd)+1:], "\n")
	if strings.TrimSpace(rest) == "" {
		return "", true
	}
	return rest, true
}

// ManagedBlock returns the block an older tq wrote in a config.toml's
// content (the current markers, both lines included) and whether there is
// one. The integrity record (internal/trust) covers that block and nothing
// else of the file, which is otherwise the user's; once Render has removed
// it there is none.
func ManagedBlock(content string) (string, bool) {
	var b strings.Builder
	in := false
	for _, l := range strings.SplitAfter(content, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case t == beginMarker:
			in = true
			b.WriteString(l)
		case t == endMarker && in:
			b.WriteString(l)
			return b.String(), true
		case in:
			b.WriteString(l)
		}
	}
	return "", false
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
// a user-owned file is a conflict for that part only, and the others are
// still written.
func Render(dir string, w Want, dryRun bool) (Result, error) {
	var res Result
	var plan []change

	// config.toml: the marked blocks an older tq wrote are removed (their
	// approval_policy and sandbox_mode with them); the rest is the user's.
	p := filepath.Join(dir, "config.toml")
	cur, _, err := readOpt(p)
	if err != nil {
		return res, err
	}
	if rest, ok := strip(cur); !ok {
		res.Conflicts = append(res.Conflicts, "config.toml: tq's managed block in "+p+" has no end marker, or a marker line tq cannot match; fix it by hand")
	} else if rest != cur {
		plan = append(plan, change{path: p, content: rest, remove: rest == ""})
	}

	// The legacy profile file: removed when tq wrote it, else left alone.
	if OwnsProfile(dir) {
		plan = append(plan, change{path: filepath.Join(dir, legacyProfile+".config.toml"), remove: true})
	}

	// The exec-policy file of older tq versions: tq's block (it starts with
	// tq's marker) is removed; lines a user added after the block stay, and a
	// rules file tq did not write is never touched.
	rp := filepath.Join(dir, filepath.FromSlash(legacyRulesFile))
	if rcur, rexists, err := readOpt(rp); err != nil {
		return res, err
	} else if rexists {
		if rest, owned := stripLegacyRules(rcur); owned {
			plan = append(plan, change{path: rp, content: rest, remove: rest == ""})
		}
	}

	// hooks.json.
	hp := filepath.Join(dir, HooksFile)
	hcur, hexists, err := readOpt(hp)
	if err != nil {
		return res, err
	}
	wantHook := w.Hook
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
