package ignore

import (
	"os"
	"path/filepath"
	"strings"
)

// Every file here is read as text; nothing is executed, and git is never
// run. Where git would pick one source over another (GIT_CONFIG_GLOBAL
// replacing ~/.gitconfig, core.excludesFile replacing the XDG default),
// the union is used instead: more exclusion, never less.

// maxSourceBytes caps any config or ignore file read.
const maxSourceBytes = 1 << 20

// ReadText reads a regular file up to the cap. Links are followed: these
// are the user's own config files, read only for ignore patterns.
func ReadText(path string) (string, bool) {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxSourceBytes {
		return "", false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// FindRepo walks up from dir to the directory holding .git. It returns the
// work tree top and the git dir and common dir (they differ for linked
// worktrees). ok is false outside a repository.
func FindRepo(dir string) (top, gitDir, commonDir string, ok bool) {
	for d := dir; ; {
		dotgit := filepath.Join(d, ".git")
		if fi, err := os.Lstat(dotgit); err == nil {
			switch {
			case fi.IsDir():
				return d, dotgit, dotgit, true
			case fi.Mode().IsRegular():
				text, _ := ReadText(dotgit)
				g := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "gitdir:"))
				if g == "" {
					return d, "", "", true
				}
				if !filepath.IsAbs(g) {
					g = filepath.Join(d, g)
				}
				common := g
				if c, ok := ReadText(filepath.Join(g, "commondir")); ok && strings.TrimSpace(c) != "" {
					c = strings.TrimSpace(c)
					if !filepath.IsAbs(c) {
						c = filepath.Join(g, c)
					}
					common = c
				}
				return d, g, common, true
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", "", "", false
		}
		d = parent
	}
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

// GlobalExcludeFiles lists every global excludes file git could use:
// core.excludesFile from GIT_CONFIG_GLOBAL, ~/.gitconfig, the XDG config
// and the repo config (following [include] and every [includeIf]), plus
// the XDG default $XDG_CONFIG_HOME/git/ignore.
func GlobalExcludeFiles(repoConfig string) []string {
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
	if repoConfig != "" {
		configs = append(configs, repoConfig)
	}
	seen := map[string]bool{}
	for _, c := range configs {
		out = append(out, excludesFromConfig(c, 0, seen)...)
	}
	return dedupe(out)
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
// file and, recursively, from the files it includes.
func excludesFromConfig(path string, depth int, seen map[string]bool) []string {
	key := strings.ToLower(filepath.Clean(path))
	if depth > 8 || seen[key] {
		return nil
	}
	seen[key] = true
	text, ok := ReadText(path)
	if !ok {
		return nil
	}
	base := filepath.Dir(path)
	var out []string
	section := ""
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			name := strings.TrimPrefix(line, "[")
			if i := strings.IndexAny(name, " \t\"]"); i >= 0 {
				name = name[:i]
			}
			section = strings.ToLower(name)
			continue
		}
		k, v, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = configValue(v)
		switch {
		case section == "core" && k == "excludesfile" && v != "":
			out = append(out, expandPath(v, base))
		case (section == "include" || section == "includeif") && k == "path" && v != "":
			out = append(out, excludesFromConfig(expandPath(v, base), depth+1, seen)...)
		}
	}
	return out
}

// configValue trims a value, drops an unquoted trailing comment and
// surrounding quotes.
func configValue(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, `"`) {
		if end := strings.Index(v[1:], `"`); end >= 0 {
			return v[1 : end+1]
		}
		return strings.Trim(v, `"`)
	}
	if i := strings.IndexAny(v, "#;"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}

func expandPath(p, base string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if hs := homeDirs(); len(hs) > 0 {
			p = filepath.Join(hs[0], strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	p = filepath.FromSlash(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return p
}
