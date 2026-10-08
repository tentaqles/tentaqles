package secrets

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// AuditHit is one location holding a secret-shaped value. It never carries
// the value.
type AuditHit struct {
	Path    string
	Line    int    // 0 for whole-file findings
	Pattern string // detector name, or "env_not_gitignored"
}

// AuditOptions scopes an audit.
type AuditOptions struct {
	Roots    []string // directories walked (workspace bases, identity dirs)
	Files    []string // individual files always scanned (catalog, ~/.claude.json)
	MaxDepth int      // directory depth below each root (default 6)
	// IsIgnored reports whether git ignores path; nil disables the .env check.
	IsIgnored func(path string) (ignored, inRepo bool)
}

// skipDirs are never descended into: dependency trees, VCS data, caches and
// Claude transcripts (huge, and scanned by a separate tool).
var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, ".venv": true, "venv": true, "__pycache__": true,
	"dist": true, "build": true, ".next": true, "target": true, "projects": true,
	"file-history": true, "shell-snapshots": true, "paste-cache": true, "cache": true,
	".cache": true, "telemetry": true, "backups": true, "session-env": true,
}

// configNames are the agent-config files whose content is scanned.
func isConfigFile(name string) bool {
	switch strings.ToLower(name) {
	case ".mcp.json", "settings.json", "settings.local.json", ".claude.json", "catalog.yaml", "claude_desktop_config.json":
		return true
	}
	return false
}

func isEnvFile(name string) bool {
	n := strings.ToLower(name)
	if n != ".env" && !strings.HasPrefix(n, ".env.") {
		return false
	}
	for _, t := range []string{".example", ".sample", ".template", ".dist", ".defaults", ".schema"} {
		if strings.HasSuffix(n, t) {
			return false
		}
	}
	return true
}

// Audit walks the configured roots and reports where secrets sit:
// secret-shaped values inside agent config files, and .env files that a git
// repository would pick up because they are not ignored. It only reads.
func Audit(o AuditOptions) []AuditHit {
	if o.MaxDepth == 0 {
		o.MaxDepth = 6
	}
	var hits []AuditHit
	seen := map[string]bool{}
	scan := func(p string) {
		key := strings.ToLower(filepath.Clean(p))
		if seen[key] {
			return
		}
		seen[key] = true
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() || fi.Size() > 8<<20 {
			return
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return
		}
		for _, f := range Scan(string(raw)) {
			hits = append(hits, AuditHit{Path: p, Line: f.Line, Pattern: f.Pattern})
		}
	}
	for _, f := range o.Files {
		scan(f)
	}
	for _, root := range o.Roots {
		root = filepath.Clean(root)
		base := strings.Count(root, string(os.PathSeparator))
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != root && (skipDirs[strings.ToLower(d.Name())] || strings.Count(p, string(os.PathSeparator))-base > o.MaxDepth) {
					return filepath.SkipDir
				}
				return nil
			}
			name := d.Name()
			switch {
			case isConfigFile(name):
				scan(p)
			case isEnvFile(name) && o.IsIgnored != nil:
				if ignored, inRepo := o.IsIgnored(p); inRepo && !ignored {
					hits = append(hits, AuditHit{Path: p, Pattern: "env_not_gitignored"})
				}
			}
			return nil
		})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Path != hits[j].Path {
			return hits[i].Path < hits[j].Path
		}
		return hits[i].Line < hits[j].Line
	})
	return hits
}

// GitIgnored asks git whether path is ignored. inRepo is false when path is
// not inside a git work tree.
func GitIgnored(path string) (ignored, inRepo bool) {
	cmd := exec.Command("git", "check-ignore", "-q", filepath.Base(path))
	cmd.Dir = filepath.Dir(path)
	err := cmd.Run()
	if err == nil {
		return true, true
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
		return false, true
	}
	return false, false
}
