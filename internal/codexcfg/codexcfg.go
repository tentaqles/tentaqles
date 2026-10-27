// Package codexcfg renders tq's unattended settings into Codex's config,
// leaving everything the user wrote as it was.
//
// Workspace-wide setups put a marked block of top-level keys at the head of
// config.toml. Projects-only setups write a Codex profile file,
// $CODEX_HOME/tq-unattended.config.toml, which `codex --profile tq-unattended`
// layers on top of config.toml (Codex 0.154 and the current docs use profile
// files rather than [profiles.<name>] tables).
package codexcfg

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

const (
	Profile     = "tq-unattended"
	beginMarker = "# >>> managed by tq (unattended): edit with tq unattended, not by hand"
	endMarker   = "# <<< managed by tq (unattended)"
)

var (
	tableRe  = regexp.MustCompile(`^\s*\[`)
	topKeyRe = regexp.MustCompile(`^\s*(approval_policy|sandbox_mode)\s*=`)
)

type Want struct {
	Top, Profile bool
	Sandbox      string
}

type Result struct {
	Path     string
	Changed  bool
	Conflict string
}

// Desired maps the manifest block to what the Codex config should carry.
func Desired(m *manifest.Manifest, bypassTrusted bool) Want {
	if m == nil || !bypassTrusted || !m.Guard.Unattended.Configured() {
		return Want{}
	}
	u := m.Guard.Unattended
	return Want{Top: u.Workspace, Profile: !u.Workspace, Sandbox: u.Sandbox()}
}

func keys(sandbox string) string {
	return fmt.Sprintf("approval_policy = \"never\"\nsandbox_mode = %q\n", sandbox)
}

// strip removes every managed block (marker lines included).
func strip(s string) string {
	var out []string
	in := false
	for _, l := range strings.SplitAfter(s, "\n") {
		t := strings.TrimRight(l, "\r\n")
		switch {
		case t == beginMarker:
			in = true
		case t == endMarker:
			in = false
		case !in:
			out = append(out, l)
		}
	}
	return strings.Join(out, "")
}

func writeAtomic(dir, p, content string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
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

// Render brings dir/config.toml and dir/tq-unattended.config.toml in line with
// w. A user's own top-level approval_policy/sandbox_mode, or their own
// tq-unattended profile file, is a conflict: nothing is written and Conflict
// says why.
func Render(dir string, w Want, dryRun bool) (Result, error) {
	p := filepath.Join(dir, "config.toml")
	pp := filepath.Join(dir, Profile+".config.toml")
	res := Result{Path: p}

	raw, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return res, err
	}
	cur := string(raw)
	rest := strip(cur)
	if w.Top {
		for _, l := range strings.Split(rest, "\n") {
			if tableRe.MatchString(l) {
				break
			}
			if topKeyRe.MatchString(l) {
				res.Conflict = "config.toml already sets " + strings.TrimSpace(strings.SplitN(l, "=", 2)[0]) + "; remove it to let tq manage it"
				return res, nil
			}
		}
	}
	next := rest
	if w.Top {
		next = beginMarker + "\n" + keys(w.Sandbox) + endMarker + "\n" + next
	}

	praw, perr := os.ReadFile(pp)
	if perr != nil && !os.IsNotExist(perr) {
		return res, perr
	}
	pexists := perr == nil
	ours := pexists && strings.HasPrefix(string(praw), beginMarker+"\n")
	if pexists && !ours && (w.Profile) {
		res.Conflict = Profile + ".config.toml already exists and is not managed by tq"
		res.Path = pp
		return res, nil
	}
	pnext := ""
	if w.Profile {
		pnext = beginMarker + "\n" + keys(w.Sandbox) + endMarker + "\n"
	}

	cfgChanged := next != cur
	profChanged := (w.Profile && (!pexists || string(praw) != pnext)) || (!w.Profile && ours)
	if !cfgChanged && !profChanged {
		return res, nil
	}
	res.Changed = true
	if !cfgChanged && profChanged {
		res.Path = pp
	}
	if dryRun {
		return res, nil
	}
	if cfgChanged {
		if next == "" {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return res, err
			}
		} else if err := writeAtomic(dir, p, next); err != nil {
			return res, err
		}
	}
	if profChanged {
		if w.Profile {
			if err := writeAtomic(dir, pp, pnext); err != nil {
				return res, err
			}
		} else if err := os.Remove(pp); err != nil && !os.IsNotExist(err) {
			return res, err
		}
	}
	return res, nil
}
