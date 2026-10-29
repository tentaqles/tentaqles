package lint

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/db/sqllex"
	"gopkg.in/yaml.v3"
)

// Migration is one packs.db.migrations entry.
type Migration struct {
	Path     string `yaml:"path"`
	Engine   string `yaml:"engine"`
	Migrator string `yaml:"migrator"`
}

// Dialect is the entry's SQL dialect.
func (m Migration) Dialect() sqllex.Dialect {
	d, _ := sqllex.ParseDialect(m.Engine)
	return d
}

// Glob is the entry's path as a "under this directory" glob.
func (m Migration) Glob() string { return strings.TrimSuffix(m.Path, "/") + "/" }

// Contains reports whether the slash path p is under the entry's path.
func (m Migration) Contains(p string) bool { return packs.MatchGlob(m.Glob(), p) }

// Migrations decodes packs.db.migrations from the merged settings; entries
// that fail validation are dropped (the manifest load already rejected
// them). A missing migrator is "raw".
func Migrations(s packs.Settings) []Migration {
	n := s.Node("migrations")
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	var out []Migration
	for _, it := range n.Content {
		var m Migration
		if it.Decode(&m) != nil || validateMigration(m) != nil {
			continue
		}
		m.Path = cleanPath(m.Path)
		// Canonical engine name (postgres or sqlserver): every alias the
		// validator accepts (supabase, postgresql, mssql, azure-sql, tsql …)
		// compares equal downstream.
		m.Engine = m.Dialect().String()
		if m.Migrator == "" {
			m.Migrator = "raw"
		}
		out = append(out, m)
	}
	return out
}

// SmallTables is packs.db.small_tables.
func SmallTables(s packs.Settings) []string { return s.Strings("small_tables") }

// Edition is packs.db.edition (lower-case, "" when unset).
func Edition(s packs.Settings) string { return strings.ToLower(s.String("edition")) }

// SqlfluffFail is packs.db.sqlfluff_fail.
func SqlfluffFail(s packs.Settings) bool { return strings.EqualFold(s.String("sqlfluff_fail"), "true") }

func cleanPath(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, `\`, "/"))
	p = path.Clean(p)
	return strings.TrimSuffix(strings.TrimPrefix(p, "./"), "/")
}

func validateMigration(m Migration) error {
	p := strings.TrimSpace(strings.ReplaceAll(m.Path, `\`, "/"))
	switch {
	case p == "":
		return errors.New("path is required")
	case strings.HasPrefix(p, "/") || (len(p) > 1 && p[1] == ':'):
		return fmt.Errorf("path %q must be relative to the repository", m.Path)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("path %q must not contain ..", m.Path)
		}
	}
	if _, ok := sqllex.ParseDialect(m.Engine); !ok || m.Engine == "" {
		return fmt.Errorf("engine %q is unknown (known: postgres, sqlserver)", m.Engine)
	}
	if m.Migrator != "" && !contains(Migrators, m.Migrator) {
		return fmt.Errorf("migrator %q is unknown (known: %s)", m.Migrator, strings.Join(Migrators, ", "))
	}
	return nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// ValidateMigrations validates the packs.db.migrations node.
func ValidateMigrations(n yaml.Node) error {
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("line %d: must be a list of {path, engine, migrator}", n.Line)
	}
	for _, it := range n.Content {
		if it.Kind != yaml.MappingNode {
			return fmt.Errorf("line %d: each entry is {path, engine, migrator}", it.Line)
		}
		for i := 0; i+1 < len(it.Content); i += 2 {
			if k := it.Content[i].Value; k != "path" && k != "engine" && k != "migrator" {
				return fmt.Errorf("line %d: unknown key %q (known: path, engine, migrator)", it.Content[i].Line, k)
			}
		}
		var m Migration
		if err := it.Decode(&m); err != nil {
			return fmt.Errorf("line %d: %v", it.Line, err)
		}
		if err := validateMigration(m); err != nil {
			return fmt.Errorf("line %d: %v", it.Line, err)
		}
	}
	return nil
}

// ValidateSmallTables validates packs.db.small_tables (a list of table names).
func ValidateSmallTables(n yaml.Node) error {
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("line %d: must be a list of table names", n.Line)
	}
	for _, it := range n.Content {
		if it.Kind != yaml.ScalarNode || strings.TrimSpace(it.Value) == "" || sqllex.ParseName(it.Value, sqllex.Postgres).IsZero() {
			return fmt.Errorf("line %d: %q is not a table name", it.Line, it.Value)
		}
	}
	return nil
}

// ValidateEdition validates packs.db.edition.
func ValidateEdition(n yaml.Node) error {
	if n.Kind != yaml.ScalarNode || !contains(Editions, strings.ToLower(n.Value)) {
		return fmt.Errorf("line %d: edition %q is unknown (known: %s)", n.Line, n.Value, strings.Join(Editions, ", "))
	}
	return nil
}

// ValidateBool validates a boolean key.
func ValidateBool(n yaml.Node) error {
	var b bool
	if n.Kind != yaml.ScalarNode || n.Decode(&b) != nil {
		return fmt.Errorf("line %d: must be true or false", n.Line)
	}
	return nil
}

// Files lists the entry's migration files: the tracked files under its
// path plus the changed (not deleted) ones, de-duplicated, in apply order.
func Files(in *packs.Input, m Migration) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !seen[p] && m.Contains(p) {
			seen[p] = true
			out = append(out, p)
		}
	}
	deleted := map[string]bool{}
	for _, c := range in.Changed {
		if c.Status == 'D' {
			deleted[c.Path] = true
		}
	}
	if in.Git != nil {
		if fs, err := in.Git.ListFiles(); err == nil {
			for _, f := range fs {
				if !deleted[f] {
					add(f)
				}
			}
		}
	}
	for _, p := range in.ChangedPaths() {
		add(p)
	}
	sqllex.SortMigrations(out)
	return out
}

// ChangedIn returns the changed (not deleted) paths under the entry, and
// which of them the diff adds (status A, untracked included).
func ChangedIn(in *packs.Input, m Migration) (changed []string, added map[string]bool) {
	added = map[string]bool{}
	for _, c := range in.Changed {
		if c.Status == 'D' || !m.Contains(c.Path) {
			continue
		}
		changed = append(changed, c.Path)
		if c.Status == 'A' && !in.All {
			added[c.Path] = true
		}
	}
	return changed, added
}
