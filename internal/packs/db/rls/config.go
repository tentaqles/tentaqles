package rls

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/db/sqllex"
	"gopkg.in/yaml.v3"
)

// SupabaseConfig is what the RLS gate reads from supabase/config.toml.
type SupabaseConfig struct {
	// Schemas is [api] schemas; HasSchemas reports whether the key is set.
	Schemas    []string
	HasSchemas bool
	// DBPort is [db] port (0 when unset).
	DBPort int
}

// supabaseDefaultSchemas is [api] schemas when the key is absent, and
// supabaseAlways the schemas the Supabase API always exposes
// (supabase.com/docs/guides/local-development/cli/config, api.schemas).
var (
	supabaseDefaultSchemas = []string{"public", "storage", "graphql_public"}
	supabaseAlways         = []string{"public", "storage"}
)

// ParseSupabaseConfig is a tiny line reader for the two keys the gate needs:
// [api] schemas (a string array, possibly over several lines) and [db]
// port. It is not a TOML parser; anything else is ignored.
func ParseSupabaseConfig(src []byte) SupabaseConfig {
	var c SupabaseConfig
	lines := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
	table := ""
	for i := 0; i < len(lines); i++ {
		l := strings.TrimSpace(stripComment(lines[i]))
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "[") {
			table = strings.TrimSpace(strings.Trim(l, "[]"))
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		k = strings.Trim(strings.TrimSpace(k), `"'`)
		v = strings.TrimSpace(v)
		key := k
		if table != "" {
			key = table + "." + k
		}
		switch key {
		case "api.schemas":
			// Gather the array up to its closing bracket.
			for !strings.Contains(v, "]") && i+1 < len(lines) {
				i++
				v += " " + strings.TrimSpace(stripComment(lines[i]))
			}
			c.Schemas, c.HasSchemas = stringArray(v), true
		case "db.port":
			if n, err := strconv.Atoi(v); err == nil {
				c.DBPort = n
			}
		}
	}
	return c
}

// stripComment drops a # comment that is not inside a quoted string.
func stripComment(l string) string {
	var q byte
	for i := 0; i < len(l); i++ {
		switch c := l[i]; {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '#':
			return l[:i]
		}
	}
	return l
}

var quoted = regexp.MustCompile(`"([^"]*)"|'([^']*)'`)

func stringArray(v string) []string {
	out := []string{}
	for _, m := range quoted.FindAllStringSubmatch(v, -1) {
		s := m[1] + m[2]
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ExposedSchemas resolves the exposed schemas: packs.db.rls.exposed_schemas
// when set (on Supabase, plus the schemas the platform always exposes); else,
// on Supabase, config.toml [api] schemas (its default when the key is
// absent) plus the schemas Supabase always exposes; else public.
func ExposedSchemas(cfg []string, toml *SupabaseConfig, supabase bool) []string {
	set := map[string]bool{}
	switch {
	case len(cfg) > 0:
		for _, s := range cfg {
			set[s] = true
		}
		if supabase {
			for _, s := range supabaseAlways {
				set[s] = true
			}
		}
	case supabase:
		list := supabaseDefaultSchemas
		if toml != nil && toml.HasSchemas {
			list = append(append([]string{}, toml.Schemas...), supabaseAlways...)
		}
		for _, s := range list {
			set[s] = true
		}
	default:
		set["public"] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Config is packs.db.rls.
type Config struct {
	ExposedSchemas []string `yaml:"exposed_schemas"`
	Tests          string   `yaml:"tests"`
}

// ValidateRLS validates the packs.db.rls node.
func ValidateRLS(n yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: must be a mapping {exposed_schemas, tests}", n.Line)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		switch k.Value {
		case "exposed_schemas":
			if v.Kind != yaml.SequenceNode {
				return fmt.Errorf("line %d: exposed_schemas must be a list of schema names", v.Line)
			}
			for _, it := range v.Content {
				if it.Kind != yaml.ScalarNode || !schemaName.MatchString(it.Value) {
					return fmt.Errorf("line %d: %q is not a schema name", it.Line, it.Value)
				}
			}
		case "tests":
			if v.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: tests must be a directory path", v.Line)
			}
			if err := RelPath(v.Value); err != nil {
				return fmt.Errorf("line %d: tests: %v", v.Line, err)
			}
		default:
			return fmt.Errorf("line %d: unknown key %q (known: exposed_schemas, tests)", k.Line, k.Value)
		}
	}
	return nil
}

var schemaName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]{0,62}$`)

// RelPath rejects an empty, absolute or ..-escaping path.
func RelPath(p string) error {
	p = strings.TrimSpace(strings.ReplaceAll(p, `\`, "/"))
	switch {
	case p == "":
		return errors.New("path is empty")
	case strings.HasPrefix(p, "/") || (len(p) > 1 && p[1] == ':'):
		return fmt.Errorf("%q must be relative to the repository", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("%q must not contain ..", p)
		}
		// pg_prove gets the directory as an argument: a segment that starts
		// with "-" could read as one of its options.
		if strings.HasPrefix(strings.TrimSpace(seg), "-") {
			return errors.New("the path must not have a segment starting with \"-\"")
		}
	}
	return nil
}

// Untested returns the tables with RLS enabled that no test file names (a
// word match on the table name, schema-qualified or not).
func Untested(tables []*Table, tests map[string][]byte) []*Table {
	var out []*Table
	for _, t := range tables {
		if !t.RLS {
			continue
		}
		re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_$])` + regexp.QuoteMeta(t.Name.Object) + `($|[^A-Za-z0-9_$])`)
		found := false
		for _, b := range tests {
			if re.Match(b) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, t)
		}
	}
	return out
}

// RLST001 is a table with RLS that no pgTAP test names (db/rls-tests).
const RLST001 = "RLST001"

// Coverage reports, at each table's CREATE TABLE (or its ENABLE ROW LEVEL
// SECURITY for a table the migrations never create), the tables with RLS
// that no test file names. A -- tq:allow RLST001: <reason> on that
// statement suppresses it. It also returns the suppressed rules and the
// names of the untested tables it reported.
func Coverage(tables []*Table, tests map[string][]byte, dir, skill string) (out []packs.Finding, supp, untested []string) {
	where := dir
	if where == "" {
		where = "packs.db.rls.tests (not set)"
	}
	for _, t := range Untested(tables, tests) {
		l := t.create
		if !t.Created || l.File == "" {
			l = t.enable
		}
		allowed, reason, line := sqllex.Allows(l.dirs, RLST001)
		if allowed && reason {
			supp = append(supp, RLST001)
			continue
		}
		if allowed {
			out = append(out, packs.Finding{File: l.File, Line: line, Level: packs.LevelError, Rule: SUP001,
				Message: "tq:allow RLST001 needs a reason: -- tq:allow RLST001: <where the policies are tested>"})
		}
		out = append(out, packs.Finding{File: l.File, Line: l.Line, Level: packs.LevelError, Rule: RLST001,
			Message: fmt.Sprintf("table %s has RLS and no pgTAP test under %s that names it; write one with %s %s", t.Name, where, skill, t.Name)})
		untested = append(untested, t.Name.String())
	}
	return out, supp, untested
}
