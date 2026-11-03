// Package registry stores which base folders tq manages (~/.tentaqles/config.yaml).
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/paths"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Bases []string `yaml:"bases"`
}

func Load() (*Config, error) {
	var c Config
	raw, err := os.ReadFile(paths.Config())
	if os.IsNotExist(err) {
		return &c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", paths.Config(), err)
	}
	return &c, nil
}

func (c *Config) Save() error {
	if err := os.MkdirAll(paths.Home(), 0o700); err != nil {
		return err
	}
	raw, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(paths.Config(), raw, 0o600)
}

// Normalize returns an absolute, cleaned, symlink-resolved path.
//
// If the path itself doesn't exist (e.g. a not-yet-created child of a
// workspace base), EvalSymlinks on the full path fails and would silently
// leave it un-resolved. That's a problem when a sibling path DOES get fully
// resolved (e.g. a stored base) and the two are later compared for equality
// or prefix-containment: on macOS, a temp dir under /var/folders/... resolves
// to /private/var/folders/..., so an existing base normalizes to the
// /private/... form while a nonexistent child of it would not. To keep both
// sides consistent, resolve the longest existing prefix of the path via
// EvalSymlinks and re-append whatever tail doesn't exist yet.
func Normalize(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	abs = resolveExistingPrefix(abs)
	return filepath.Clean(abs), nil
}

// resolveExistingPrefix walks up p from the full path towards its root,
// resolving the first (longest) prefix for which EvalSymlinks succeeds, and
// re-joins the remaining (possibly nonexistent) suffix onto the result. If no
// prefix can be resolved (e.g. EvalSymlinks fails even at the root, which
// should not normally happen), p is returned unchanged.
func resolveExistingPrefix(p string) string {
	cur := filepath.Clean(p)
	var suffix []string
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			resolved := r
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Reached the root without resolving anything; give up.
			return p
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
}

// SamePath compares paths case-insensitively on Windows.
func SamePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func (c *Config) AddBase(dir string) (bool, error) {
	n, err := Normalize(dir)
	if err != nil {
		return false, err
	}
	st, err := os.Stat(n)
	if err != nil || !st.IsDir() {
		return false, fmt.Errorf("base %q is not a directory", dir)
	}
	for _, b := range c.Bases {
		if SamePath(b, n) {
			return false, nil
		}
	}
	c.Bases = append(c.Bases, n)
	return true, nil
}
