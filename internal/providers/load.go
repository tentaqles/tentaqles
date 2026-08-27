package providers

import (
	"embed"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/tentaqles/tentaqles/cli/internal/paths"
	"gopkg.in/yaml.v3"
)

//go:embed catalog/*.yaml
var embeddedFS embed.FS

// expandVars replaces "{dir}" in each template value with dir, then
// normalizes the result with filepath.FromSlash.
func expandVars(env map[string]string, dir string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		expanded := strings.ReplaceAll(v, "{dir}", dir)
		out[k] = filepath.FromSlash(expanded)
	}
	return out
}

// UserDir is the directory holding user provider override YAML files.
func UserDir() string {
	return filepath.Join(paths.Home(), "providers")
}

// Catalog is the merged set of embedded and user-defined providers.
type Catalog struct {
	byID  map[string]Provider
	order []string
}

func loadYAMLProvider(name string, data []byte, source string) (Provider, error) {
	var p Provider
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Provider{}, fmt.Errorf("%s: %w", name, err)
	}
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	if p.ID == "" {
		p.ID = base
	}
	if p.ID != base {
		return Provider{}, fmt.Errorf("%s: filename must equal id (got id %q)", name, p.ID)
	}
	p.Source = source
	if err := p.Validate(); err != nil {
		return Provider{}, err
	}
	return p, nil
}

func loadEmbedded() (*Catalog, error) {
	entries, err := embeddedFS.ReadDir("catalog")
	if err != nil {
		return nil, err
	}
	c := &Catalog{byID: make(map[string]Provider)}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := embeddedFS.ReadFile(path.Join("catalog", e.Name()))
		if err != nil {
			return nil, err
		}
		p, err := loadYAMLProvider(e.Name(), data, "embedded")
		if err != nil {
			return nil, err
		}
		if _, exists := c.byID[p.ID]; exists {
			return nil, fmt.Errorf("duplicate provider id %q in embedded catalog", p.ID)
		}
		c.byID[p.ID] = p
		c.order = append(c.order, p.ID)
	}
	return c, nil
}

// Load builds the provider catalog from the embedded catalog, then merges in
// user overrides from UserDir() (same id replaces the embedded entry; new
// ids are appended). User file errors are returned, not panicked.
func Load() (*Catalog, error) {
	c, err := loadEmbedded()
	if err != nil {
		return nil, err
	}

	dir := UserDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		p, err := loadYAMLProvider(e.Name(), data, path)
		if err != nil {
			return nil, err
		}
		if _, exists := c.byID[p.ID]; !exists {
			c.order = append(c.order, p.ID)
		}
		c.byID[p.ID] = p
	}
	return c, nil
}

var (
	mustLoadOnce sync.Once
	mustLoadC    *Catalog
	mustLoadErr  error
)

// MustLoad loads the catalog and panics only on embedded-catalog errors,
// which indicate a build defect. The result is memoized: the first call
// parses the catalog, later calls return the same cached *Catalog. Callers
// that must see fresh user files (e.g. after WriteUser) should use Load().
//
// If the embedded catalog fails to load, the failure is cached and every
// subsequent call panics again (sync.Once alone would swallow the panic on
// calls after the first, since Do treats a panicking f as having "returned").
func MustLoad() *Catalog {
	mustLoadOnce.Do(func() {
		mustLoadC, mustLoadErr = mustLoadUncached()
	})
	if mustLoadErr != nil {
		panic(fmt.Sprintf("providers: embedded catalog error: %v", mustLoadErr))
	}
	return mustLoadC
}

func mustLoadUncached() (*Catalog, error) {
	c, err := loadEmbedded()
	if err != nil {
		return nil, err
	}
	// Best-effort merge of user overrides; ignore errors here since MustLoad
	// must not panic on user data. Callers wanting user-error visibility
	// should use Load().
	dir := UserDir()
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			p, err := loadYAMLProvider(e.Name(), data, path)
			if err != nil {
				continue
			}
			if _, exists := c.byID[p.ID]; !exists {
				c.order = append(c.order, p.ID)
			}
			c.byID[p.ID] = p
		}
	}
	return c, nil
}

// Get returns the provider with the given id.
func (c *Catalog) Get(id string) (Provider, bool) {
	p, ok := c.byID[id]
	return p, ok
}

// All returns every provider sorted by category then id.
func (c *Catalog) All() []Provider {
	out := make([]Provider, 0, len(c.byID))
	for _, id := range c.order {
		out = append(out, c.byID[id])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// IDs returns every provider id, sorted the same way as All().
func (c *Catalog) IDs() []string {
	all := c.All()
	ids := make([]string, len(all))
	for i, p := range all {
		ids[i] = p.ID
	}
	return ids
}

// ByCategory returns providers in the given category, sorted by id.
func (c *Catalog) ByCategory(cat string) []Provider {
	var out []Provider
	for _, p := range c.All() {
		if p.Category == cat {
			out = append(out, p)
		}
	}
	return out
}

// WriteUser validates p and writes it as YAML to UserDir()/<id>.yaml (0600).
func WriteUser(p Provider) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	dir := UserDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, p.ID+".yaml")
	data, err := yaml.Marshal(p)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
