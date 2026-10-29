// Package drill is the db pack's backup and restore drill (spec D §13): the
// packs.db.drill config, the drill run (`tq pack db drill <name>`), the
// evidence line in docs/ops/restore-drills.jsonl and the db/restore-drill
// check.
//
// The drill reads a backup and writes only to a scratch database on the
// local test_db server. Only names and numbers are recorded, never row
// content, a host or a connection string.
package drill

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

// Engines.
const (
	Postgres  = "postgres"
	SQLServer = "sqlserver"
)

// Source kinds (§13.1).
const (
	KindFile     = "file"
	KindSupabase = "supabase-dump"
	KindBacpac   = "azure-sql-bacpac"
	KindCommand  = "command"
)

// Defaults and bounds.
const (
	DefaultFreshness = 30 * 24 * time.Hour
	MaxFreshness     = 365 * 24 * time.Hour
	DefaultTimeout   = 30 * time.Minute
	// MaxTimeout is the framework's per-tool cap (toolrun.MaxTimeout).
	MaxTimeout = 30 * time.Minute
	// MaxVerify bounds the verify tables of one drill.
	MaxVerify = 64
)

// Source is where the backup comes from. It is only ever read.
type Source struct {
	Kind string
	// PathEnv names the variable holding the dump path (file,
	// azure-sql-bacpac). A name, never a value.
	PathEnv string
	// Command is the fetch command's argv (command): it prints the dump path
	// on its last stdout line, or writes the dump to $TQ_DRILL_OUT.
	Command []string
}

// Drill is one packs.db.drill entry.
type Drill struct {
	Name      string
	Engine    string
	Source    Source
	Verify    []string
	Freshness time.Duration
	Timeout   time.Duration
	Line      int
}

var (
	nameRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
	varRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	identRe = `[A-Za-z_][A-Za-z0-9_$]*`
	tableRe = regexp.MustCompile(`^` + identRe + `(\.` + identRe + `)?$`)
)

// ValidTable reports a verify entry tq can quote safely: table or
// schema.table, plain identifiers only.
func ValidTable(s string) bool { return tableRe.MatchString(s) && len(s) <= 128 }

// Validate is the ConfigKeys validator of packs.db.drill.
func Validate(n yaml.Node) error {
	_, err := parse(&n)
	return err
}

// Parse decodes packs.db.drill from the pack's settings (nil when unset).
// An invalid block returns an error; callers fail closed.
func Parse(s packs.Settings) ([]Drill, error) { return parse(s.Node("drill")) }

func deref(n *yaml.Node) *yaml.Node {
	if n != nil && n.Kind == yaml.AliasNode && n.Alias != nil {
		return n.Alias
	}
	return n
}

func parse(n *yaml.Node) ([]Drill, error) {
	n = deref(n)
	if n == nil || n.Kind == 0 || (n.Kind == yaml.ScalarNode && n.Tag == "!!null") {
		return nil, nil
	}
	if n.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("line %d: drill must be a list of {name, engine, source, verify, freshness}", n.Line)
	}
	var out []Drill
	seen := map[string]bool{}
	for _, it := range n.Content {
		it = deref(it)
		if it.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("line %d: a drill must be a mapping", it.Line)
		}
		d := Drill{Freshness: DefaultFreshness, Timeout: DefaultTimeout, Line: it.Line}
		has := map[string]bool{}
		for i := 0; i+1 < len(it.Content); i += 2 {
			k, v := it.Content[i].Value, deref(it.Content[i+1])
			has[k] = true
			switch k {
			case "name":
				if v.Kind != yaml.ScalarNode || !nameRe.MatchString(v.Value) {
					return nil, fmt.Errorf("line %d: drill name %q must match [a-z0-9][a-z0-9_-]* (at most 40 characters)", v.Line, v.Value)
				}
				d.Name = v.Value
			case "engine":
				if v.Kind != yaml.ScalarNode || (v.Value != Postgres && v.Value != SQLServer) {
					return nil, fmt.Errorf("line %d: drill engine %q is unknown (postgres or sqlserver)", v.Line, v.Value)
				}
				d.Engine = v.Value
			case "source":
				src, err := parseSource(v)
				if err != nil {
					return nil, err
				}
				d.Source = src
			case "verify":
				if v.Kind != yaml.SequenceNode {
					return nil, fmt.Errorf("line %d: drill verify must be a list of tables", v.Line)
				}
				if len(v.Content) > MaxVerify {
					return nil, fmt.Errorf("line %d: drill verify lists more than %d tables", v.Line, MaxVerify)
				}
				for _, t := range v.Content {
					if t.Kind != yaml.ScalarNode || !ValidTable(t.Value) {
						return nil, fmt.Errorf("line %d: verify entry %q must be table or schema.table (plain identifiers)", t.Line, t.Value)
					}
					d.Verify = append(d.Verify, t.Value)
				}
			case "freshness":
				dur, err := manifest.ParseDuration(v.Value)
				if err != nil || dur <= 0 || dur > MaxFreshness {
					return nil, fmt.Errorf("line %d: drill freshness %q must be a duration up to 365d (for example 30d)", v.Line, v.Value)
				}
				d.Freshness = dur
			case "timeout":
				dur, err := manifest.ParseDuration(v.Value)
				if err != nil || dur <= 0 || dur > MaxTimeout {
					return nil, fmt.Errorf("line %d: drill timeout %q must be a duration up to 30m", v.Line, v.Value)
				}
				d.Timeout = dur
			default:
				return nil, fmt.Errorf("line %d: drill: unknown key %q (known: name, engine, source, verify, freshness, timeout)", it.Content[i].Line, k)
			}
		}
		for _, req := range []string{"name", "engine", "source"} {
			if !has[req] {
				return nil, fmt.Errorf("line %d: a drill needs %s", it.Line, req)
			}
		}
		if seen[d.Name] {
			return nil, fmt.Errorf("line %d: drill %s is declared twice", it.Line, d.Name)
		}
		seen[d.Name] = true
		switch {
		case d.Source.Kind == KindSupabase && d.Engine != Postgres:
			return nil, fmt.Errorf("line %d: drill %s: supabase-dump needs engine postgres", it.Line, d.Name)
		case d.Source.Kind == KindBacpac && d.Engine != SQLServer:
			return nil, fmt.Errorf("line %d: drill %s: azure-sql-bacpac needs engine sqlserver", it.Line, d.Name)
		}
		out = append(out, d)
	}
	return out, nil
}

func parseSource(v *yaml.Node) (Source, error) {
	if v.Kind != yaml.MappingNode {
		return Source{}, fmt.Errorf("line %d: drill source must be a mapping {kind, path_env | command}", v.Line)
	}
	var s Source
	for i := 0; i+1 < len(v.Content); i += 2 {
		k, x := v.Content[i].Value, deref(v.Content[i+1])
		switch k {
		case "kind":
			switch x.Value {
			case KindFile, KindSupabase, KindBacpac, KindCommand:
				s.Kind = x.Value
			default:
				return Source{}, fmt.Errorf("line %d: drill source kind %q is unknown (file, supabase-dump, azure-sql-bacpac or command)", x.Line, x.Value)
			}
		case "path_env":
			if x.Kind != yaml.ScalarNode || !varRe.MatchString(x.Value) {
				return Source{}, fmt.Errorf("line %d: path_env must be a variable name", x.Line)
			}
			s.PathEnv = x.Value
		case "command":
			if x.Kind != yaml.SequenceNode || len(x.Content) == 0 {
				return Source{}, fmt.Errorf("line %d: source command must be a non-empty argv list (no shell)", x.Line)
			}
			for _, a := range x.Content {
				if a.Kind != yaml.ScalarNode || a.Value == "" {
					return Source{}, fmt.Errorf("line %d: source command holds an empty or non-string word", a.Line)
				}
				s.Command = append(s.Command, a.Value)
			}
		default:
			return Source{}, fmt.Errorf("line %d: drill source: unknown key %q (known: kind, path_env, command)", v.Content[i].Line, k)
		}
	}
	switch s.Kind {
	case "":
		return Source{}, fmt.Errorf("line %d: drill source needs a kind", v.Line)
	case KindFile, KindBacpac:
		if s.PathEnv == "" || len(s.Command) > 0 {
			return Source{}, fmt.Errorf("line %d: a %s source needs path_env (and no command)", v.Line, s.Kind)
		}
	case KindCommand:
		if len(s.Command) == 0 || s.PathEnv != "" {
			return Source{}, fmt.Errorf("line %d: a command source needs command (and no path_env)", v.Line)
		}
	case KindSupabase:
		if len(s.Command) > 0 || s.PathEnv != "" {
			return Source{}, fmt.Errorf("line %d: a supabase-dump source takes no path_env or command (it dumps the linked project)", v.Line)
		}
	}
	return s, nil
}

// Find returns the drill named name.
func Find(ds []Drill, name string) (Drill, bool) {
	for _, d := range ds {
		if d.Name == name {
			return d, true
		}
	}
	return Drill{}, false
}

// Names lists the drill names.
func Names(ds []Drill) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Name)
	}
	return out
}

// ScratchName is the scratch database of a drill run: tq_drill_<name>_<ts>,
// with - turned into _ (a plain identifier that needs no quoting).
func ScratchName(name string, at time.Time) string {
	return "tq_drill_" + strings.ReplaceAll(name, "-", "_") + "_" + at.UTC().Format("20060102150405")
}
