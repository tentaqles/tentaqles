package protect

import (
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// The protected path set of PRD v2 §8.2 (ADR-0005): what the hook's
// tq/protected-path rule denies a session's tool calls on. The set is
// computed once per hook run from the facts the caller gathers (Inputs)
// and answers two questions: Normalize turns a path as a tool call or a
// shell word spells it into a canonical absolute path, and Match says
// whether that path is protected and how.

// Kind says what a protected path is and which accesses it denies.
type Kind string

const (
	// KindState is tq's own state: the state folder (config.yaml, trust/,
	// the manifests, the denial and audit logs, anchors, bundles, providers,
	// shims, backups, the integrity record) and, inside a harness config
	// home, the files that register the hook (settings.json,
	// settings.local.json, hooks.json, config.toml). Reads and writes are
	// denied.
	KindState Kind = "state"
	// KindMarker is a workspace root's name-only marker: writes denied,
	// reads allowed (it holds the name and nothing else).
	KindMarker Kind = "marker"
	// KindPlugin is the installed tentaqles plugin (its hooks and scripts)
	// and the harness's plugin registration: writes denied, reads allowed.
	KindPlugin Kind = "plugin"
	// KindBinary is the running tq binary, its directory and any tq-named
	// file beside it: writes denied, reads allowed.
	KindBinary Kind = "binary"
)

// Inputs are the facts the set is computed from; the caller gathers them
// (the hook: paths.Home, the registry, the resolved workspace, the
// environment, os.Executable). Every path may be spelled as the caller
// has it; the set canonicalises.
type Inputs struct {
	Home       string              // tq's state folder, paths.Home()
	UserHome   string              // the user's home, what ~ and $HOME expand to
	Bases      []string            // registered bases: <base>/<ws>/.tentaqles.yaml is a marker
	Roots      []string            // workspace roots the caller knows (the resolved one)
	ConfigDirs []string            // Claude config homes: the identity's, $CLAUDE_CONFIG_DIR, ~/.claude
	CodexDirs  []string            // Codex homes: the identity's, $CODEX_HOME, ~/.codex
	Executable string              // the running tq binary ("" when unknown)
	Env        func(string) string // the environment, for $TQ_HOME, $CLAUDE_CONFIG_DIR, $CODEX_HOME
}

// Hit is a protected path a call touched.
type Hit struct {
	Path string // the path, canonical
	Kind Kind
	What string // what it is, for the deny message
}

// ReadDenied reports whether reading the hit (not only writing it) is
// denied: tq's state is never read into a session; the marker, the plugin
// and the binary may be.
func (h Hit) ReadDenied() bool { return h.Kind == KindState }

type entry struct {
	path string // canonical
	kind Kind
	tree bool // the path and everything under it
	what string
}

// Set is the protected path set of one hook run.
type Set struct {
	home     string // canonical state folder ("" when unknown)
	homeRaw  string // the state folder as given, what $TQ_HOME expands to
	userHome string
	bases    []string // canonical
	entries  []entry
	binDir   string // canonical directory of the binary ("" when unknown)
	env      func(string) string
	ci       bool // case-insensitive paths
}

// New computes the set.
func New(in Inputs) *Set {
	s := &Set{homeRaw: in.Home, userHome: in.UserHome, env: in.Env, ci: caseInsensitivePaths()}
	if s.env == nil {
		s.env = func(string) string { return "" }
	}
	if in.Home != "" {
		s.home = s.canon(in.Home)
	}
	for _, b := range in.Bases {
		if strings.TrimSpace(b) != "" {
			s.bases = append(s.bases, s.canon(b))
		}
	}
	for _, r := range in.Roots {
		if strings.TrimSpace(r) != "" {
			s.add(filepath.Join(r, ".tentaqles.yaml"), KindMarker, false, "the workspace marker")
		}
	}
	for _, d := range in.ConfigDirs {
		if strings.TrimSpace(d) == "" {
			continue
		}
		s.add(filepath.Join(d, "plugins", "cache", "tentaqles"), KindPlugin, true, "the installed tentaqles plugin")
		s.add(filepath.Join(d, "plugins", "installed_plugins.json"), KindPlugin, false, "the plugin registration")
		s.add(filepath.Join(d, "plugins", "known_marketplaces.json"), KindPlugin, false, "the plugin registration")
	}
	for _, d := range in.CodexDirs {
		if strings.TrimSpace(d) == "" {
			continue
		}
		s.add(filepath.Join(d, "hooks.json"), KindState, false, "the Codex hook registration")
		s.add(filepath.Join(d, "config.toml"), KindState, false, "the Codex configuration")
	}
	if strings.TrimSpace(in.Executable) != "" {
		exe := s.canon(in.Executable)
		s.entries = append(s.entries, entry{exe, KindBinary, false, "the tq binary"})
		s.binDir = filepath.Dir(exe)
		// A directory of tentaqles' own (%LOCALAPPDATA%\tentaqles\bin, a
		// plugin's bin/) is protected whole; a shared one (~/.local/bin,
		// /usr/local/bin) as itself and its tq-named files only (match).
		s.entries = append(s.entries, entry{s.binDir, KindBinary, ownedDir(s.binDir), "the tq binary's directory"})
	}
	return s
}

// ownedDir reports whether dir belongs to tentaqles: one of its path
// elements is tentaqles or .tentaqles.
func ownedDir(dir string) bool {
	for _, e := range strings.Split(filepath.ToSlash(strings.ToLower(dir)), "/") {
		if e == "tentaqles" || e == ".tentaqles" {
			return true
		}
	}
	return false
}

func (s *Set) add(p string, k Kind, tree bool, what string) {
	c := s.canon(p)
	for _, e := range s.entries {
		if e.path == c {
			return
		}
	}
	s.entries = append(s.entries, entry{c, k, tree, what})
}

// caseInsensitivePaths reports whether this OS compares paths without
// case (Windows, and macOS by default).
func caseInsensitivePaths() bool { return runtime.GOOS == "windows" || runtime.GOOS == "darwin" }

// canon makes an absolute path canonical: cleaned, the symlinks of its
// existing prefix resolved, and folded to lower case where paths are
// compared without case.
func (s *Set) canon(p string) string {
	r := paths.Resolve(p)
	if s.ci {
		r = strings.ToLower(r)
	}
	return r
}

var (
	// shellVarRe: $NAME and ${NAME} (case as spelled; a shell variable).
	shellVarRe = regexp.MustCompile(`\$\{(HOME|USERPROFILE|TQ_HOME|CLAUDE_CONFIG_DIR|CODEX_HOME)\}|\$(HOME|USERPROFILE|TQ_HOME|CLAUDE_CONFIG_DIR|CODEX_HOME)\b`)
	// winVarRe: $env:NAME and %NAME% (Windows names are case-insensitive).
	winVarRe    = regexp.MustCompile(`(?i)\$env:(HOME|USERPROFILE|TQ_HOME|CLAUDE_CONFIG_DIR|CODEX_HOME)\b|%(HOME|USERPROFILE|TQ_HOME|CLAUDE_CONFIG_DIR|CODEX_HOME)%`)
	tildeRe     = regexp.MustCompile(`(^|[\s"'=(,:;])~(?:/|\\|$)`)
	msysDriveRe = regexp.MustCompile(`^/([A-Za-z])(/|$)`)
	winAbsRe    = regexp.MustCompile(`^[a-zA-Z]:[\\/]`)
)

// value is what a variable expands to; "" leaves the token as written.
func (s *Set) value(name string) string {
	switch strings.ToUpper(name) {
	case "HOME", "USERPROFILE":
		return s.userHome
	case "TQ_HOME":
		if v := s.env("TQ_HOME"); v != "" {
			return v
		}
		return s.homeRaw
	default:
		return s.env(strings.ToUpper(name))
	}
}

// Expand replaces, in text, a leading ~ and the variables a session uses
// to spell tq's paths ($HOME, $USERPROFILE, $TQ_HOME, $CLAUDE_CONFIG_DIR,
// $CODEX_HOME, in the shell, PowerShell and cmd forms) with their values.
// A variable with no value stays as written.
func (s *Set) Expand(text string) string {
	sub := func(m string, names ...string) string {
		for _, n := range names {
			if n == "" {
				continue
			}
			if v := s.value(n); v != "" {
				return v
			}
			return m
		}
		return m
	}
	text = shellVarRe.ReplaceAllStringFunc(text, func(m string) string {
		g := shellVarRe.FindStringSubmatch(m)
		return sub(m, g[1], g[2])
	})
	text = winVarRe.ReplaceAllStringFunc(text, func(m string) string {
		g := winVarRe.FindStringSubmatch(m)
		return sub(m, g[1], g[2])
	})
	if s.userHome != "" {
		text = tildeRe.ReplaceAllStringFunc(text, func(m string) string {
			i := strings.Index(m, "~")
			return m[:i] + s.userHome + m[i+1:]
		})
	}
	return text
}

// Normalize turns p, as a tool call or a shell word spells it, into a
// canonical absolute path: quotes and spaces trimmed, ~ and variables
// expanded, an MSYS drive (/c/...) mapped, a relative path placed under
// base (the directory the call runs in; "" when that is not known, which
// leaves a relative path unplaceable and returns ""), cleaned and
// canonicalised.
func (s *Set) Normalize(p, base string) string {
	p = strings.TrimSpace(strings.Trim(strings.TrimSpace(p), `"'`))
	p = s.Expand(p)
	if p == "" {
		return ""
	}
	if runtime.GOOS == "windows" {
		if m := msysDriveRe.FindStringSubmatch(p); m != nil {
			p = strings.ToUpper(m[1]) + ":/" + strings.TrimPrefix(p[len(m[0]):], "/")
		}
	}
	p = filepath.FromSlash(p)
	if !filepath.IsAbs(p) && !winAbsRe.MatchString(p) {
		if base == "" {
			return ""
		}
		p = filepath.Join(base, p)
	}
	return s.canon(filepath.Clean(p))
}

// Match reports whether the canonical path p is protected. A path with a
// glob (rm -rf ~/.tentaqles/*) matches when a protected path starts with
// its literal prefix.
func (s *Set) Match(p string) (Hit, bool) {
	if p == "" {
		return Hit{}, false
	}
	if h, ok := s.match(p); ok {
		return h, true
	}
	if i := strings.IndexAny(p, "*?["); i >= 0 {
		prefix := p[:i]
		for _, e := range s.entries {
			if strings.HasPrefix(e.path, prefix) {
				return Hit{p, e.kind, e.what}, true
			}
		}
		if s.home != "" && strings.HasPrefix(s.home, prefix) {
			return Hit{p, KindState, "the tq state folder"}, true
		}
	}
	return Hit{}, false
}

func (s *Set) match(p string) (Hit, bool) {
	// The explicit entries come first: a plugin file inside an identity's
	// config home is the plugin (readable), not the state tree.
	for _, e := range s.entries {
		if p == e.path || (e.tree && under(e.path, p)) {
			return Hit{p, e.kind, e.what}, true
		}
	}
	if s.home != "" {
		if rel, ok := relUnder(s.home, p); ok {
			return s.matchState(p, rel)
		}
	}
	name := filepath.Base(p)
	if name == ".tentaqles.yaml" {
		ws := filepath.Dir(p)
		base := filepath.Dir(ws)
		if base != ws {
			for _, b := range s.bases {
				if b == base {
					return Hit{p, KindMarker, "the workspace marker"}, true
				}
			}
		}
	}
	if s.binDir != "" && filepath.Dir(p) == s.binDir && (name == "tq" || strings.HasPrefix(name, "tq.")) {
		return Hit{p, KindBinary, "a tq binary beside the running one"}, true
	}
	return Hit{}, false
}

// harnessHomes are the identity dirs that are a harness's config home,
// where the harness and the developer keep their own working data (memory
// memory, projects, todos, sessions): only the hook's registration files
// and the plugin are protected there.
var harnessHomes = map[string]bool{"claude": true, "codex": true}

// registrationFiles register the hook or set the harness's permissions.
var registrationFiles = map[string]bool{"settings.json": true, "settings.local.json": true, "hooks.json": true, "config.toml": true}

// matchState judges p, at rel inside the state folder.
func (s *Set) matchState(p, rel string) (Hit, bool) {
	if rel == "." {
		return Hit{p, KindState, "the tq state folder"}, true
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) >= 3 && parts[0] == "identities" && harnessHomes[parts[2]] {
		sub := parts[3:]
		switch {
		case len(sub) == 0:
			return Hit{p, KindState, "the " + parts[2] + " config home of " + parts[1]}, true
		case len(sub) == 1 && registrationFiles[sub[0]]:
			return Hit{p, KindState, "the hook registration of " + parts[1] + " (" + sub[0] + ")"}, true
		case sub[0] == "plugins":
			switch {
			case len(sub) == 1, len(sub) == 2 && sub[1] == "cache":
				return Hit{p, KindPlugin, "the plugin cache of " + parts[1]}, true
			case len(sub) == 2 && (sub[1] == "installed_plugins.json" || sub[1] == "known_marketplaces.json"):
				return Hit{p, KindPlugin, "the plugin registration of " + parts[1]}, true
			case len(sub) >= 3 && sub[1] == "cache" && sub[2] == "tentaqles":
				return Hit{p, KindPlugin, "the installed tentaqles plugin"}, true
			}
			return Hit{}, false
		default:
			// The harness's own working data.
			return Hit{}, false
		}
	}
	return Hit{p, KindState, stateWhat(parts)}, true
}

func stateWhat(parts []string) string {
	switch parts[0] {
	case "config.yaml":
		return "tq's registry"
	case "trust":
		return "the trust store"
	case "identities":
		if len(parts) == 3 && parts[2] == "manifest.yaml" {
			return "the manifest of " + parts[1]
		}
		if len(parts) >= 2 {
			return "the identity state of " + parts[1]
		}
		return "the identities"
	case "denials.jsonl", "audit.jsonl":
		return "tq's logs"
	case "anchors.yaml":
		return "the terminal anchors"
	case "bundles":
		return "the bundle catalog"
	case "providers":
		return "the provider catalog"
	case "shims":
		return "the shims"
	case "backups":
		return "the migration journal"
	case "integrity.key":
		return "the integrity key (the keychain's plain-file fallback)"
	}
	if strings.HasPrefix(parts[0], "integrity") {
		return "the integrity record"
	}
	return "the tq state folder"
}

// Mentions scans text (an interpreter's inline code, a .NET call) for a
// protected path spelled in it: each place where a protected root appears
// is read as a path up to the next delimiter and matched.
func (s *Set) Mentions(text string) (Hit, bool) {
	// One spelling for the scan: forward slashes, and lower case where
	// paths are compared without case (what canon did to the roots).
	t := filepath.ToSlash(s.Expand(text))
	if s.ci {
		t = strings.ToLower(t)
	}
	roots := make([]string, 0, len(s.entries)+1)
	if s.home != "" {
		roots = append(roots, filepath.ToSlash(s.home))
	}
	for _, e := range s.entries {
		roots = append(roots, filepath.ToSlash(e.path))
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		for i := strings.Index(t, root); i >= 0; {
			end := i + len(root)
			for end < len(t) && !strings.ContainsRune(" \t\r\n\"'`),;|<>", rune(t[end])) {
				end++
			}
			if h, ok := s.Match(s.canon(filepath.FromSlash(t[i:end]))); ok {
				return h, true
			}
			next := strings.Index(t[end:], root)
			if next < 0 {
				break
			}
			i = end + next
		}
	}
	return Hit{}, false
}

// under reports whether p is strictly inside root (both canonical).
func under(root, p string) bool {
	rel, ok := relUnder(root, p)
	return ok && rel != "."
}

// relUnder is filepath.Rel when p is root or inside it.
func relUnder(root, p string) (string, bool) {
	rel, err := filepath.Rel(root, p)
	if err != nil || paths.IsOutside(rel) {
		return "", false
	}
	return rel, true
}
