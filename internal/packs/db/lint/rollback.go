package lint

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/db/sqllex"
)

// Rollback rule ids (db/migration-rollback).
const (
	RB001 = "RB001" // the rollback partner is missing
	RB002 = "RB002" // the rollback partner is empty
	RB003 = "RB003" // forward-only migrator and no "### Rollback" in the migration plan
)

// Migrators are the accepted packs.db.migrations[].migrator values.
var Migrators = []string{"supabase", "prisma", "drizzle", "golang-migrate", "dbmate", "flyway", "sqitch", "alembic", "efcore", "dacpac", "raw"}

// ForwardOnly reports a migrator with no down migrations (its rollback is
// the spec's "### Rollback" under "## Migration plan").
func ForwardOnly(migrator string) bool {
	switch migrator {
	case "supabase", "prisma", "drizzle", "dacpac", "raw", "":
		return true
	}
	return false
}

var flywayVersioned = regexp.MustCompile(`^V([0-9][0-9._]*)__.*\.sql$`)

// Lintable reports whether the SQL lint reads path for migrator: .sql
// forward migrations only (no golang-migrate .down.sql, Flyway U undo
// files, sqitch revert/ or verify/ scripts).
func Lintable(migrator, p string) bool {
	if !strings.EqualFold(path.Ext(p), ".sql") {
		return false
	}
	base := path.Base(p)
	switch migrator {
	case "golang-migrate":
		return !strings.HasSuffix(base, ".down.sql")
	case "flyway":
		return !strings.HasPrefix(base, "U")
	case "sqitch":
		return strings.Contains("/"+p, "/deploy/")
	}
	return true
}

// RollbackInput is one packs.db.migrations entry's view of the change.
type RollbackInput struct {
	Migrator string
	// Changed are the changed (not deleted) files under the entry's path.
	Changed []string
	// Files are every known file under the entry's path (tracked plus
	// changed), for partner lookups.
	Files []string
	// Read reads a file's new side.
	Read func(path string) ([]byte, error)
	// Specs are the changed Markdown files (path to content), searched for
	// "## Migration plan" / "### Rollback" when the migrator is forward-only.
	Specs map[string][]byte
}

// Rollback applies the db/migration-rollback rules to one entry.
func Rollback(in RollbackInput) []packs.Finding {
	known := map[string]bool{}
	for _, f := range in.Files {
		known[f] = true
	}
	for _, f := range in.Changed {
		known[f] = true
	}
	changed := append([]string(nil), in.Changed...)
	sort.Strings(changed)
	var out []packs.Finding
	add := func(file, rule, level, msg string) {
		out = append(out, packs.Finding{File: file, Level: level, Rule: rule, Message: msg})
	}
	read := func(p string) string {
		if in.Read == nil {
			return ""
		}
		b, err := in.Read(p)
		if err != nil {
			return ""
		}
		return string(b)
	}
	if ForwardOnly(in.Migrator) {
		var first string
		n := 0
		for _, p := range changed {
			if in.Migrator == "prisma" && path.Base(p) != "migration.sql" {
				continue
			}
			if !strings.EqualFold(path.Ext(p), ".sql") && in.Migrator != "dacpac" {
				continue
			}
			if first == "" {
				first = p
			}
			n++
		}
		if n > 0 && !specHasRollback(in.Specs) {
			add(first, RB003, packs.LevelError, fmt.Sprintf("%d migration file(s) changed and %s migrations have no down step: add a \"### Rollback\" subsection under the spec's \"## Migration plan\"", n, migratorName(in.Migrator)))
		}
		return out
	}
	for _, p := range changed {
		base := path.Base(p)
		dir := path.Dir(p)
		switch in.Migrator {
		case "golang-migrate":
			if !strings.HasSuffix(base, ".up.sql") {
				continue
			}
			down := strings.TrimSuffix(p, ".up.sql") + ".down.sql"
			if !known[down] {
				add(p, RB001, packs.LevelError, "no "+path.Base(down)+" next to this up migration")
			} else if !hasSQL(read(down)) {
				add(down, RB002, packs.LevelError, "the down migration has no statement")
			}
		case "dbmate":
			if !strings.EqualFold(path.Ext(p), ".sql") {
				continue
			}
			src := read(p)
			k := indexDirective(src, "migrate:down")
			if k < 0 {
				add(p, RB001, packs.LevelError, "no -- migrate:down section")
			} else if !hasSQL(src[k:]) {
				add(p, RB002, packs.LevelError, "the -- migrate:down section has no statement")
			}
		case "flyway":
			m := flywayVersioned.FindStringSubmatch(base)
			if m == nil {
				continue
			}
			found := false
			for f := range known {
				if path.Dir(f) == dir && strings.HasPrefix(path.Base(f), "U"+m[1]+"__") {
					found = true
				}
			}
			if !found {
				add(p, RB001, packs.LevelWarn, "no U"+m[1]+"__*.sql undo migration (Flyway undo is a paid feature, so this is a warning)")
			}
		case "sqitch":
			if !strings.Contains("/"+p, "/deploy/") || !strings.EqualFold(path.Ext(p), ".sql") {
				continue
			}
			k := strings.LastIndex("/"+p, "/deploy/")
			rev := p[:k] + "revert/" + p[k+len("deploy/"):]
			if k == 0 {
				rev = "revert/" + strings.TrimPrefix(p, "deploy/")
			}
			if !known[rev] {
				add(p, RB001, packs.LevelError, "no "+rev+" revert script")
			} else if !hasSQL(read(rev)) {
				add(rev, RB002, packs.LevelError, "the revert script has no statement")
			}
		case "alembic":
			if path.Ext(p) != ".py" {
				continue
			}
			switch alembicDowngrade(read(p)) {
			case -1:
				add(p, RB001, packs.LevelError, "no downgrade() function")
			case 0:
				add(p, RB002, packs.LevelError, "downgrade() has no body other than pass")
			}
		case "efcore":
			if path.Ext(p) != ".cs" || strings.HasSuffix(base, ".Designer.cs") || strings.HasSuffix(base, "ModelSnapshot.cs") {
				continue
			}
			switch efDown(read(p)) {
			case -1:
				add(p, RB001, packs.LevelError, "no Down(MigrationBuilder) method")
			case 0:
				add(p, RB002, packs.LevelError, "Down(MigrationBuilder) has no statement")
			}
		}
	}
	return out
}

func migratorName(m string) string {
	if m == "" {
		return "raw"
	}
	return m
}

func indexDirective(src, name string) int {
	f := sqllex.Lex([]byte(src), sqllex.Postgres)
	for _, c := range f.Comments {
		if strings.HasPrefix(strings.ToLower(c.Text), name) {
			return c.Pos
		}
	}
	return -1
}

// hasSQL reports any statement outside comments.
func hasSQL(src string) bool {
	return len(sqllex.Lex([]byte(src), sqllex.Postgres).Statements) > 0
}

var (
	planHeading     = regexp.MustCompile(`(?i)^(#{2,})\s+.*migration plan`)
	rollbackHeading = regexp.MustCompile(`(?i)^(#{3,})\s+rollback\b`)
	anyHeading      = regexp.MustCompile(`^(#{1,6})\s`)
)

// specHasRollback: a "### Rollback" heading nested under a "## Migration
// plan" heading (the heading the DoD migration item reads).
func specHasRollback(specs map[string][]byte) bool {
	for _, b := range specs {
		level := 0
		for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
			if m := planHeading.FindStringSubmatch(line); m != nil {
				level = len(m[1])
				continue
			}
			if level == 0 {
				continue
			}
			if m := rollbackHeading.FindStringSubmatch(line); m != nil && len(m[1]) > level {
				return true
			}
			if m := anyHeading.FindStringSubmatch(line); m != nil && len(m[1]) <= level {
				level = 0
			}
		}
	}
	return false
}

// alembicDowngrade: -1 no downgrade(), 0 a body of only pass / docstrings /
// comments, 1 a real body.
func alembicDowngrade(src string) int {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "def downgrade(") {
			continue
		}
		indent := len(l) - len(strings.TrimLeft(l, " \t"))
		inDoc := ""
		for _, b := range lines[i+1:] {
			bt := strings.TrimSpace(b)
			if bt == "" {
				continue
			}
			if len(b)-len(strings.TrimLeft(b, " \t")) <= indent {
				break
			}
			if inDoc != "" {
				if strings.Contains(bt, inDoc) {
					inDoc = ""
				}
				continue
			}
			if strings.HasPrefix(bt, "#") || bt == "pass" || bt == "..." {
				continue
			}
			if q := docQuote(bt); q != "" {
				if strings.Count(bt, q) < 2 {
					inDoc = q
				}
				continue
			}
			return 1
		}
		return 0
	}
	return -1
}

func docQuote(s string) string {
	for _, q := range []string{`"""`, `'''`} {
		if strings.HasPrefix(s, q) {
			return q
		}
	}
	return ""
}

// efDown: -1 no Down(MigrationBuilder …), 0 an empty body, 1 statements.
func efDown(src string) int {
	re := regexp.MustCompile(`\bDown\s*\(\s*MigrationBuilder\b[^)]*\)`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return -1
	}
	rest := src[loc[1]:]
	open := strings.IndexByte(rest, '{')
	if open < 0 {
		return 0
	}
	depth := 0
	var body strings.Builder
	for i := open; i < len(rest); i++ {
		c := rest[i]
		if c == '{' {
			depth++
			if depth == 1 {
				continue
			}
		}
		if c == '}' {
			depth--
			if depth == 0 {
				break
			}
		}
		body.WriteByte(c)
	}
	for _, l := range strings.Split(body.String(), "\n") {
		t := strings.TrimSpace(l)
		if k := strings.Index(t, "//"); k >= 0 {
			t = strings.TrimSpace(t[:k])
		}
		if t != "" {
			return 1
		}
	}
	return 0
}
