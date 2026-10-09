package ignore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"
)

// Every file here is read as text; nothing is executed, and git is never
// run. Where git would pick one source over another (GIT_CONFIG_GLOBAL
// replacing ~/.gitconfig, core.excludesFile replacing the XDG default),
// the union is used instead: more exclusion, never less.
//
// Fail closed: a source that does not exist is fine, but one that exists
// and cannot be loaded (unreadable, a directory, a dangling or disallowed
// link, oversized, not UTF-8, unparsable, an unresolvable path, an include
// chain too deep) is an error, and callers skip whatever it governs.

// maxSourceBytes caps any config or ignore file read.
const maxSourceBytes = 1 << 20

// maxIncludeDepth matches git's own limit on nested config includes.
const maxIncludeDepth = 10

// ErrUnusable marks an ignore or config source that exists but cannot be
// applied.
var ErrUnusable = errors.New("ignore source exists but cannot be used")

func unusable(path, why string) error {
	return fmt.Errorf("%w: %s: %s", ErrUnusable, path, why)
}

// absent reports whether err means "nothing there": the file, or a parent
// of it that would have to be a directory, does not exist.
func absent(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// Load reads one source. present is false (with no error) only when the
// file does not exist. With noFollow, a symlink is an error rather than
// followed (in-tree ignore files); otherwise links are followed, and a
// dangling one is an error.
func Load(path string, noFollow bool) (text string, present bool, err error) {
	fi, err := os.Lstat(path)
	if err != nil {
		if absent(err) {
			return "", false, nil
		}
		return "", true, unusable(path, err.Error())
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		if noFollow {
			return "", true, unusable(path, "is a symlink")
		}
		if fi, err = os.Stat(path); err != nil {
			return "", true, unusable(path, "dangling or unreadable link: "+err.Error())
		}
	}
	if !fi.Mode().IsRegular() {
		return "", true, unusable(path, "not a regular file")
	}
	if fi.Size() > maxSourceBytes {
		return "", true, unusable(path, "too large")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", true, unusable(path, err.Error())
	}
	if !utf8.Valid(raw) {
		return "", true, unusable(path, "not valid UTF-8")
	}
	return string(raw), true, nil
}

// LoadRules loads and parses one ignore file. A missing file yields empty
// rules; anything else that goes wrong is an error.
func LoadRules(path string, noFollow bool) (Rules, error) {
	text, present, err := Load(path, noFollow)
	if err != nil || !present {
		return Rules{}, err
	}
	r, err := Parse(text)
	if err != nil {
		return Rules{}, unusable(path, err.Error())
	}
	return r, nil
}

// Repo is the repository a directory belongs to.
type Repo struct {
	Top       string // work tree top
	GitDir    string // the .git directory (per worktree)
	CommonDir string // shared git dir (differs for linked worktrees)
}

// FindRepo walks up from dir to the directory holding .git. found is false
// outside any repository. A .git that exists but cannot be resolved (an
// unreadable entry, a gitdir: pointer to nowhere, a broken commondir) is
// an error: its info/exclude and config would otherwise be silently lost.
func FindRepo(dir string) (repo Repo, found bool, err error) {
	for d := dir; ; {
		dotgit := filepath.Join(d, ".git")
		fi, lerr := os.Lstat(dotgit)
		switch {
		case lerr == nil:
			r, err := resolveDotGit(d, dotgit, fi)
			return r, true, err
		case !absent(lerr):
			return Repo{Top: d}, true, unusable(dotgit, lerr.Error())
		}
		parent := filepath.Dir(d)
		if parent == d {
			return Repo{}, false, nil
		}
		d = parent
	}
}

func resolveDotGit(top, dotgit string, fi fs.FileInfo) (Repo, error) {
	r := Repo{Top: top}
	switch {
	case fi.IsDir():
		r.GitDir, r.CommonDir = dotgit, dotgit
		return r, nil
	case fi.Mode().IsRegular():
	default:
		return r, unusable(dotgit, "neither a directory nor a gitdir file")
	}
	text, _, err := Load(dotgit, true)
	if err != nil {
		return r, err
	}
	line := strings.TrimSpace(text)
	if !strings.HasPrefix(line, "gitdir:") {
		return r, unusable(dotgit, "no gitdir: line")
	}
	g := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if g == "" || strings.ContainsAny(g, "\n\r") {
		return r, unusable(dotgit, "malformed gitdir: line")
	}
	if !filepath.IsAbs(g) {
		g = filepath.Join(top, g)
	}
	if err := isReadableDir(g); err != nil {
		return r, unusable(dotgit, "gitdir: "+err.Error())
	}
	r.GitDir, r.CommonDir = g, g
	ctext, present, err := Load(filepath.Join(g, "commondir"), false)
	if err != nil {
		return r, err
	}
	if present {
		c := strings.TrimSpace(ctext)
		if c == "" {
			return r, unusable(filepath.Join(g, "commondir"), "empty")
		}
		if !filepath.IsAbs(c) {
			c = filepath.Join(g, c)
		}
		if err := isReadableDir(c); err != nil {
			return r, unusable(filepath.Join(g, "commondir"), err.Error())
		}
		r.CommonDir = c
	}
	return r, nil
}

func isReadableDir(p string) error {
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return errors.New("not a directory")
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	return f.Close()
}

func homeDirs() []string {
	var out []string
	seen := map[string]bool{}
	add := func(h string) {
		if h != "" && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	add(os.Getenv("HOME"))
	if h, err := os.UserHomeDir(); err == nil {
		add(h)
	}
	return out
}

func xdgConfigDirs() []string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return []string{x}
	}
	var out []string
	for _, h := range homeDirs() {
		out = append(out, filepath.Join(h, ".config"))
	}
	return out
}

// systemConfigs are the usual system-level git config locations.
func systemConfigs() []string {
	out := []string{"/etc/gitconfig", "/usr/local/etc/gitconfig", "/opt/homebrew/etc/gitconfig"}
	if s := os.Getenv("GIT_CONFIG_SYSTEM"); s != "" {
		out = append(out, s)
	}
	for _, env := range []string{"ProgramFiles", "ProgramW6432"} {
		if d := os.Getenv(env); d != "" {
			out = append(out, filepath.Join(d, "Git", "etc", "gitconfig"))
		}
	}
	if d := os.Getenv("ProgramData"); d != "" {
		out = append(out, filepath.Join(d, "Git", "config"))
	}
	return out
}

// GlobalExcludeFiles lists every global excludes file git could use:
// core.excludesFile from the system config, GIT_CONFIG_GLOBAL,
// ~/.gitconfig, the XDG config and the repo configs (following [include]
// and every [includeIf]), plus the XDG default $XDG_CONFIG_HOME/git/ignore.
// An error means some config that exists could not be fully read: its
// excludes may be missing, so the caller must fail closed.
func GlobalExcludeFiles(repoConfigs ...string) ([]string, error) {
	var configs []string
	if os.Getenv("GIT_CONFIG_NOSYSTEM") == "" {
		configs = append(configs, systemConfigs()...)
	}
	if g := os.Getenv("GIT_CONFIG_GLOBAL"); g != "" {
		configs = append(configs, g)
	}
	for _, h := range homeDirs() {
		configs = append(configs, filepath.Join(h, ".gitconfig"))
	}
	var out []string
	for _, x := range xdgConfigDirs() {
		configs = append(configs, filepath.Join(x, "git", "config"))
		out = append(out, filepath.Join(x, "git", "ignore"))
	}
	for _, c := range repoConfigs {
		if c != "" {
			configs = append(configs, c)
		}
	}
	seen := map[string]bool{}
	for _, c := range configs {
		files, err := excludesFromConfig(c, 0, seen)
		if err != nil {
			return nil, err
		}
		out = append(out, files...)
	}
	return dedupe(out), nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		k := strings.ToLower(filepath.Clean(s))
		if !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
}

// excludesFromConfig reads core.excludesFile values from one git config
// file and, recursively, from the files it includes. A missing file is
// fine; a file revisited through an include cycle was already read.
func excludesFromConfig(path string, depth int, seen map[string]bool) ([]string, error) {
	if depth > maxIncludeDepth {
		return nil, unusable(path, "include chain deeper than git allows")
	}
	key := strings.ToLower(filepath.Clean(path))
	if seen[key] {
		return nil, nil
	}
	seen[key] = true
	text, present, err := Load(path, false)
	if err != nil || !present {
		return nil, err
	}
	base := filepath.Dir(path)
	var out []string
	section := ""
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			end := strings.LastIndex(line, "]")
			if end < 0 {
				return nil, unusable(path, fmt.Sprintf("line %d: malformed section header", n+1))
			}
			name := line[1:end]
			if i := strings.IndexAny(name, " \t\""); i >= 0 {
				name = name[:i]
			}
			name = strings.ToLower(name)
			// "[core] excludesFile = x" on one line is legal git syntax.
			rest := strings.TrimSpace(line[end+1:])
			section = name
			if rest == "" || rest[0] == '#' || rest[0] == ';' {
				continue
			}
			line = rest
		}
		k, v, found := strings.Cut(line, "=")
		k = strings.ToLower(strings.TrimSpace(k))
		if !found {
			continue // a boolean key: no value, nothing to resolve
		}
		val, err := configValue(v)
		if err != nil {
			return nil, unusable(path, fmt.Sprintf("line %d: %v", n+1, err))
		}
		switch {
		case section == "core" && k == "excludesfile":
			if val == "" {
				continue
			}
			p, err := expandPath(val, base)
			if err != nil {
				return nil, unusable(path, fmt.Sprintf("line %d: core.excludesFile: %v", n+1, err))
			}
			out = append(out, p)
		case (section == "include" || section == "includeif") && k == "path":
			if val == "" {
				continue
			}
			p, err := expandPath(val, base)
			if err != nil {
				return nil, unusable(path, fmt.Sprintf("line %d: include.path: %v", n+1, err))
			}
			more, err := excludesFromConfig(p, depth+1, seen)
			if err != nil {
				return nil, err
			}
			out = append(out, more...)
		}
	}
	return out, nil
}

// configValue decodes a git config value: double-quoted segments, the
// escapes git defines, and an unquoted trailing comment. A trailing
// backslash (line continuation) or an unknown escape is an error rather
// than a guess.
func configValue(v string) (string, error) {
	v = strings.TrimSpace(v)
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == '\\':
			if i+1 >= len(v) {
				return "", errors.New("line continuation is not supported")
			}
			i++
			switch v[i] {
			case '\\', '"':
				b.WriteByte(v[i])
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'b':
				return "", errors.New("backspace escape is not supported")
			default:
				return "", fmt.Errorf("unknown escape \\%c", v[i])
			}
		case c == '"':
			inQuote = !inQuote
		case !inQuote && (c == '#' || c == ';'):
			return strings.TrimSpace(b.String()), nil
		default:
			b.WriteByte(c)
		}
	}
	if inQuote {
		return "", errors.New("unterminated quote")
	}
	return strings.TrimSpace(b.String()), nil
}

// expandPath resolves a config path the way git does for the forms it
// documents; anything it cannot resolve with certainty is an error.
func expandPath(p, base string) (string, error) {
	switch {
	case strings.HasPrefix(p, "%(prefix)/"):
		return "", errors.New("%(prefix) paths are not supported")
	case p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`):
		hs := homeDirs()
		if len(hs) == 0 {
			return "", errors.New("~ used but no home directory is known")
		}
		p = filepath.Join(hs[0], p[1:])
	case strings.HasPrefix(p, "~"):
		return "", errors.New("~user paths are not supported")
	}
	p = filepath.FromSlash(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return p, nil
}
