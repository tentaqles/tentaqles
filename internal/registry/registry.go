// Package registry stores which base folders tq manages (~/.tentaqles/config.yaml).
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/cli/internal/paths"
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
func Normalize(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		abs = r
	}
	return filepath.Clean(abs), nil
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
