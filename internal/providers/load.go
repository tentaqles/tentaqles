package providers

import (
	"embed"
	"errors"
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
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	p, err := parseProvider(data, base, source)
	if err != nil {
		return Provider{}, fmt.Errorf("%s: %w", name, err)
	}
	return p, nil
}

// parseProvider decodes and validates one provider file named for id; its errors do not
// repeat the file name (the caller adds it).
func parseProvider(data []byte, id, source string) (Provider, error) {
	if err := checkKnownKeys(data); err != nil {
		return Provider{}, err
	}
	var p Provider
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Provider{}, err
	}
	if p.ID == "" {
		p.ID = id
	}
	if p.ID != id {
		return Provider{}, fmt.Errorf("filename must equal id (got id %q)", p.ID)
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

// FileError is a user provider file (~/.tentaqles/providers/<id>.yaml) that tq did not load:
// it cannot be read, does not parse or validate, collides with another provider's identity
// variable, or (a new id) claims an entry a built-in provider lists. The file contributes
// nothing; for a built-in id the built-in entry stays in force.
type FileError struct {
	Path    string // the full path of the file
	ID      string // the id the file is named for (its basename without .yaml)
	Builtin bool   // the id is a built-in provider's: its built-in lists stay in force
	Err     error  // what is wrong
}

// First is the first line of the first error, without the file name the parser put in front.
func (e *FileError) First() string {
	msg := e.Err.Error()
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return strings.TrimSpace(msg)
}

// Fix is what the developer does about it; the message has already named the file.
func (e *FileError) Fix() string {
	return "fix or delete it, then run tentaqles providers list"
}

// Detail is the file (with the line and column of a schema error: path:5:3) and the first
// error, without what tq does about it or the fix.
func (e *FileError) Detail() string {
	sep := ": "
	var ke *keyError
	if errors.As(e.Err, &ke) {
		sep = ":" // the error starts with line:col
	}
	return e.Path + sep + e.First()
}

// Error names the file once (with the line and column of a schema error: path:5:3), the
// first error, what tq does instead and the fix.
func (e *FileError) Error() string {
	var what string
	if e.Builtin {
		what = "tq uses the built-in " + e.ID + " lists instead"
	} else {
		what = "a workspace whose scope names " + e.ID + " is refused every call"
	}
	return e.Detail() + " (" + what + "); " + e.Fix()
}

func (e *FileError) Unwrap() error { return e.Err }

// loadUserFiles reads every user provider file into c (embedded already loaded) and returns
// one FileError per file it did not load. A ReadDir failure other than "no folder" is
// returned as dirErr: tq cannot tell which files there are.
func loadUserFiles(c *Catalog) (bad []*FileError, dirErr error) {
	builtin := make(map[string]Provider, len(c.byID))
	for id, p := range c.byID {
		builtin[id] = p
	}
	dir := UserDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		p, fe := loadUserFile(filepath.Join(dir, e.Name()), builtin, c)
		if fe != nil {
			bad = append(bad, fe)
			continue
		}
		c.override(p)
	}
	return bad, nil
}

// loadUserFile loads one user file against the built-in entries and the catalog so far.
func loadUserFile(path string, builtin map[string]Provider, c *Catalog) (Provider, *FileError) {
	id := strings.TrimSuffix(filepath.Base(path), ".yaml")
	_, isBuiltin := builtin[id]
	fail := func(err error) (Provider, *FileError) {
		return Provider{}, &FileError{Path: path, ID: id, Builtin: isBuiltin, Err: err}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail(err)
	}
	p, err := parseProvider(data, id, path)
	if err != nil {
		return fail(err)
	}
	if !isBuiltin {
		if err := claimConflict(p, builtin); err != nil {
			return fail(err)
		}
	}
	if err := c.envCollision(p); err != nil {
		return fail(err)
	}
	return p, nil
}

// UserFileError is the FileError of the user file named for id, or nil when there is no such
// file or it loads. It reads the file and the embedded catalog afresh: callers use it on an
// error path (an unknown provider id in a manifest), to say which file to fix.
func UserFileError(id string) *FileError {
	if !idRe.MatchString(id) {
		return nil
	}
	path := filepath.Join(UserDir(), id+".yaml")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	c, err := loadEmbedded()
	if err != nil {
		return nil
	}
	builtin := make(map[string]Provider, len(c.byID))
	for k, p := range c.byID {
		builtin[k] = p
	}
	_, fe := loadUserFile(path, builtin, c)
	return fe
}

// Load builds the provider catalog from the embedded catalog, then merges in user overrides
// from UserDir() (same id replaces the embedded entry, its match lists merged; new ids are
// appended). It returns the first user file error, for callers that must not run on a partial
// catalog; LoadAll reports them all.
func Load() (*Catalog, error) {
	c, errs := LoadAll()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return c, nil
}

// LoadAll is Load but never stops at the first bad user file: it loads every readable, valid
// user file over the embedded catalog and returns one error per file it did not load (a
// *FileError) plus any error reading the folder, so `tentaqles providers list` and `tentaqles doctor` can
// report them all and the hook can decide per workspace. The embedded
// catalog alone still errors with a nil catalog.
func LoadAll() (*Catalog, []error) {
	c, err := loadEmbedded()
	if err != nil {
		return nil, []error{err}
	}
	var errs []error
	bad, dirErr := loadUserFiles(c)
	if dirErr != nil {
		errs = append(errs, dirErr)
	}
	for _, fe := range bad {
		errs = append(errs, fe)
	}
	if cerr := c.checkEnvCollisions(); cerr != nil {
		errs = append(errs, cerr)
	}
	return c, errs
}

// claimConflict refuses a user provider with a new id that claims anything a built-in
// provider lists: an in-scope provider's claims come from the embedded catalog, so a new id
// in scope would otherwise unmask a built-in's CLI or variable.
// To extend a built-in, a file named for its id is used instead.
func claimConflict(p Provider, builtin map[string]Provider) error {
	ids := make([]string, 0, len(builtin))
	for id := range builtin {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	m := p.Match
	refuse := func(list, entry, owner string) error {
		return fmt.Errorf("match.%s: %q is listed by the built-in provider %q; a provider of your own may not claim a built-in's entries (to extend %s, name the file %s.yaml)",
			list, entry, owner, owner, owner)
	}
	for _, id := range ids {
		b := builtin[id].Match
		for _, e := range m.Executables {
			_, sub := b.Subcommands[e]
			if containsFold(b.Executables, e) || sub {
				return refuse("executables", e, id)
			}
		}
		for exe, subs := range m.Subcommands {
			for _, s := range subs {
				if containsFold(b.Executables, exe) || containsFold(b.Subcommands[exe], s) {
					return refuse("subcommands", exe+" "+s, id)
				}
			}
		}
		for _, e := range m.Env {
			for _, be := range b.Env {
				if envOverlap(e, be) {
					return refuse("env", e, id)
				}
			}
		}
	}
	return nil
}

func containsFold(xs []string, x string) bool {
	for _, v := range xs {
		if strings.EqualFold(v, x) {
			return true
		}
	}
	return false
}

// envOverlap reports whether two env entries (a trailing * is a prefix) can name one variable.
func envOverlap(a, b string) bool {
	a, b = strings.ToUpper(a), strings.ToUpper(b)
	ap, aw := strings.CutSuffix(a, "*")
	bp, bw := strings.CutSuffix(b, "*")
	switch {
	case aw && bw:
		return strings.HasPrefix(ap, bp) || strings.HasPrefix(bp, ap)
	case aw:
		return strings.HasPrefix(b, ap)
	case bw:
		return strings.HasPrefix(a, bp)
	}
	return a == b
}

// Overlaps reports every pair of providers that claim the same executable (same first
// subcommand or both wholesale), so nothing is silent.
// Each string is one human-readable overlap.
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
			for exe, subs := range b.Match.Subcommands {
				if contains(a.Match.Executables, exe) {
					for _, sub := range subs {
						add("subcommand", a.ID, b.ID, exe+" "+sub)
					}
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
	// Best-effort merge of user overrides: a bad file is left out here, since MustLoad must
	// not panic on user data. Callers wanting the errors use LoadAll().
	_, _ = loadUserFiles(c)
	return c, nil
}

// override puts a user provider file into the catalog: a new id is appended, a known id is
// replaced, except its match lists, which are the union of both (Match.Union): a user file
// extends what the guard recognises and never shortens it.
func (c *Catalog) override(p Provider) {
	prev, exists := c.byID[p.ID]
	if !exists {
		c.order = append(c.order, p.ID)
	} else {
		p.Match = prev.Match.Union(p.Match)
	}
	c.byID[p.ID] = p
}

// envCollision is checkEnvCollisions for one incoming user provider against the catalog so
// far (its own id excluded: an override replaces it).
func (c *Catalog) envCollision(p Provider) error {
	ids := make([]string, 0, len(c.byID))
	for id := range c.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for k, v := range p.Identity.Env {
		for _, id := range ids {
			q := c.byID[id]
			if id == p.ID {
				continue
			}
			if _, taken := q.Identity.Env[k]; taken && !sameCLIVariable(q, p, k, v) {
				return fmt.Errorf("identity.env[%s] is also set by provider %q: one would silently overwrite the other for any workspace enabling both", k, id)
			}
		}
	}
	return nil
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
