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

	"github.com/tentaqles/tentaqles/internal/paths"
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
			if cerr := c.checkEnvCollisions(); cerr != nil {
				return nil, cerr
			}
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
		c.override(p)
	}
	if err := c.checkEnvCollisions(); err != nil {
		return nil, err
	}
	return c, nil
}

// LoadAll is Load but never stops at the first bad user file: it loads every readable,
// valid user file over the embedded catalog and returns one error per file it skipped, so
// `tq providers list` and `tq doctor` can report them all and the hook can fail closed
// naming them (ADR-0007 round 2). The embedded catalog alone still errors.
func LoadAll() (*Catalog, []error) {
	c, err := loadEmbedded()
	if err != nil {
		return nil, []error{err}
	}
	var errs []error
	dir := UserDir()
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		if !os.IsNotExist(rerr) {
			errs = append(errs, rerr)
		}
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		p, err := loadYAMLProvider(e.Name(), data, path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		c.override(p)
	}
	if cerr := c.checkEnvCollisions(); cerr != nil {
		errs = append(errs, cerr)
	}
	return c, errs
}

// Overlaps reports every pair of providers that claim the same executable (same first
// subcommand or both wholesale), credential path tree, API host or MCP token, so nothing is
// silent (ADR-0007 round 2). Each string is one human-readable overlap.
func (c *Catalog) Overlaps() []string {
	var out []string
	ps := c.All()
	seen := map[string]bool{}
	add := func(kind, a, b, what string) {
		if a > b {
			a, b = b, a
		}
		key := kind + "|" + a + "|" + b + "|" + what
		if a == b || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, fmt.Sprintf("%s and %s both claim %s %q", a, b, kind, what))
	}
	for i := range ps {
		for j := i + 1; j < len(ps); j++ {
			a, b := ps[i], ps[j]
			for _, e := range a.Match.Executables {
				if contains(b.Match.Executables, e) {
					add("executable", a.ID, b.ID, e)
				}
			}
			for exe, subs := range a.Match.Subcommands {
				for _, sub := range subs {
					for _, bs := range b.Match.Subcommands[exe] {
						if sub == bs {
							add("subcommand", a.ID, b.ID, exe+" "+sub)
						}
					}
					if contains(b.Match.Executables, exe) {
						add("subcommand", a.ID, b.ID, exe+" "+sub)
					}
				}
			}
			for _, h := range a.Match.Hosts {
				if contains(b.Match.Hosts, h) {
					add("host", a.ID, b.ID, h)
				}
			}
			for _, t := range a.Match.MCP {
				if contains(b.Match.MCP, t) {
					add("MCP token", a.ID, b.ID, t)
				}
			}
			for _, p := range a.Match.Paths {
				if contains(b.Match.Paths, p) {
					add("path", a.ID, b.ID, p)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
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
			c.override(p)
		}
	}
	return c, nil
}

// override puts a user provider file into the catalog: a new id is appended, a known id is
// replaced, except its match lists, which are the union of both (Match.Union): a user file
// extends what the guard recognises and never shortens it (ADR-0007).
func (c *Catalog) override(p Provider) {
	prev, exists := c.byID[p.ID]
	if !exists {
		c.order = append(c.order, p.ID)
	} else {
		p.Match = prev.Match.Union(p.Match)
	}
	c.byID[p.ID] = p
}

// Embedded returns the embedded catalog alone, without the user files: what tests and a
// caller that must not read the tq home use.
func Embedded() (*Catalog, error) { return loadEmbedded() }

// Get returns the provider with the given id.
func (c *Catalog) Get(id string) (Provider, bool) {
	p, ok := c.byID[id]
	return p, ok
}

// All returns every provider sorted by category then id.
// All returns every provider, sorted by category then id. The returned
// Providers share pointer fields (CLI, Login, Verify) and maps (Identity.Env)
// with the cached catalog; callers must treat them as read-only and must not
// mutate them.
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

// checkEnvCollisions refuses a catalog where two providers claim the same
// identity variable.
//
// envplan.Desired flattens every enabled provider's variables into one map, so
// a shared key means the alphabetically-last provider silently wins and the
// other reads its directory. Nothing errors, nothing warns, and the losing
// provider looks isolated while sharing an identity -- the exact failure this
// project exists to prevent.
//
// It is a real risk rather than a theoretical one: several CLIs are
// configurable only through generic variables like XDG_CONFIG_HOME, and
// cataloguing two of those would collide on the first workspace that enabled
// both.
func (c *Catalog) checkEnvCollisions() error {
	owner := map[string]string{}
	ids := make([]string, 0, len(c.byID))
	for id := range c.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for k, v := range c.byID[id].Identity.Env {
			if prev, taken := owner[k]; taken {
				if sameCLIVariable(c.byID[prev], c.byID[id], k, v) {
					continue
				}
				return fmt.Errorf("providers %q and %q both set identity.env[%s]: "+
					"one would silently overwrite the other for any workspace enabling both", prev, id, k)
			}
			owner[k] = id
		}
	}
	return nil
}

// sameCLIVariable reports whether two catalog entries declare one variable because they are the
// same CLI (az and azure-devops are both `az`): the variable is the CLI's own config home, the
// templates match, and envplan points it at a single directory when a workspace enables both.
func sameCLIVariable(a, b Provider, key, value string) bool {
	if a.CLI == nil || b.CLI == nil || a.CLI.Command == "" || a.CLI.Command != b.CLI.Command {
		return false
	}
	return a.Identity.Env[key] == value
}
