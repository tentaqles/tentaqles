package db

// Workstream D-DL (spec D §10, §15): the migration SQL lint.
//
// Checks:   db/migration-lint, db/migration-rollback (built-in),
//           db/squawk, db/sqlfluff (external).
// Keys:     migrations, small_tables, edition, sqlfluff_fail.
// Flags:    db_migration (raised), and the reviewer-only mapping of the core
//           Jev flags schema, migration, rls, data_loss and destructive to
//           db-reviewer.

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/db/lint"
	"github.com/tentaqles/tentaqles/internal/packs/db/sqllex"
	"github.com/tentaqles/tentaqles/internal/packs/db/tools"
	"gopkg.in/yaml.v3"
)

// MigrationDirRE mirrors plugin/tentaqles/workflow/gate.py MIGRATION_DIR_RE,
// so db_migration is raised for the same paths as the DoD migration item.
var MigrationDirRE = regexp.MustCompile(`(^|/)(migrations|migrate|alembic/versions|supabase/migrations|prisma/migrations|db/migrate|flyway|sql/migrations)/`)

const (
	reviewer = "db-reviewer"
	// maxMigrationBytes caps one migration file read by the lint.
	maxMigrationBytes = 8 << 20
	// maxSpecBytes caps one changed Markdown file read for "### Rollback".
	maxSpecBytes = 1 << 20
)

func init() {
	packs.RegisterPack(packs.Pack{
		Name: "db",
		ConfigKeys: map[string]func(yaml.Node) error{
			"migrations":    lint.ValidateMigrations,
			"small_tables":  lint.ValidateSmallTables,
			"edition":       lint.ValidateEdition,
			"sqlfluff_fail": lint.ValidateBool,
		},
		Merge: map[string]packs.MergeRule{
			// More migration paths lint more: a repo file may add them.
			"migrations": packs.Union(),
			// Both loosen the lint (lock rules to warn): trusted layer only.
			"small_tables": {},
			"edition":      {},
			// true only tightens.
			"sqlfluff_fail": packs.Or(),
		},
		Flags: []packs.Flag{
			{Name: "db_migration", Reviewers: []string{reviewer}, Raise: raiseMigration},
			{Name: "schema", Reviewers: []string{reviewer}},
			{Name: "migration", Reviewers: []string{reviewer}},
			{Name: "rls", Reviewers: []string{reviewer}},
			{Name: "data_loss", Reviewers: []string{reviewer}},
			{Name: "destructive", Reviewers: []string{reviewer}},
		},
	})
	packs.Register(packs.Check{ID: "db/migration-lint", Pack: "db", Kind: packs.Builtin,
		Applies: func(in *packs.Input) bool { return anyMigrationChanged(in, "") },
		Run:     runMigrationLint})
	packs.Register(packs.Check{ID: "db/migration-rollback", Pack: "db", Kind: packs.Builtin,
		Applies: func(in *packs.Input) bool { return anyMigrationChanged(in, "") },
		Run:     runRollback})
	packs.Register(packs.Check{ID: "db/squawk", Pack: "db", Kind: packs.External, Tool: "squawk", MinVersion: "2.0.0",
		Applies: func(in *packs.Input) bool { return anyMigrationChanged(in, "postgres") },
		Run: func(ctx context.Context, in *packs.Input) packs.Result {
			files := lintableChanged(in, "postgres")
			if len(files) == 0 {
				return packs.Result{Status: packs.Skip, Detail: "no changed Postgres forward migration"}
			}
			return tools.Squawk(ctx, in, files)
		}})
	packs.Register(packs.Check{ID: "db/sqlfluff", Pack: "db", Kind: packs.External, Tool: "sqlfluff",
		Applies: func(in *packs.Input) bool { return anyMigrationChanged(in, "") },
		Run:     runSqlfluff})
}

func raiseMigration(in *packs.Input) bool {
	if anyMigrationChanged(in, "") {
		return true
	}
	for _, p := range in.ChangedPaths() {
		if MigrationDirRE.MatchString(p) {
			return true
		}
	}
	return false
}

// anyMigrationChanged: a changed (not deleted) path under a migrations entry
// of the engine ("" = any engine).
func anyMigrationChanged(in *packs.Input, engine string) bool {
	for _, m := range lint.Migrations(in.Settings()) {
		if engine != "" && m.Engine != engine {
			continue
		}
		if in.AnyChanged(m.Glob()) {
			return true
		}
	}
	return false
}

// lintableChanged: the changed forward migration files of the engine.
func lintableChanged(in *packs.Input, engine string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range lint.Migrations(in.Settings()) {
		if engine != "" && m.Engine != engine {
			continue
		}
		changed, _ := lint.ChangedIn(in, m)
		for _, p := range changed {
			if lint.Lintable(m.Migrator, p) && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sqllex.SortMigrations(out)
	return out
}

func runMigrationLint(ctx context.Context, in *packs.Input) packs.Result {
	s := in.Settings()
	var all []packs.Finding
	files, stmts, paths := 0, 0, 0
	suppressed := map[string]int{}
	for _, m := range lint.Migrations(s) {
		changed, added := lint.ChangedIn(in, m)
		if len(changed) == 0 {
			continue
		}
		paths++
		isChanged := map[string]bool{}
		for _, p := range changed {
			isChanged[p] = true
		}
		var units []lint.Unit
		for _, p := range lint.Files(in, m) {
			if ctx.Err() != nil {
				return packs.Result{Status: packs.Fail, Detail: "timed out"}
			}
			if !lint.Lintable(m.Migrator, p) {
				continue
			}
			src, err := in.ReadNew(p, maxMigrationBytes)
			if err != nil {
				if isChanged[p] {
					return packs.Result{Status: packs.Fail, Detail: "cannot read " + p}
				}
				continue
			}
			u := lint.Unit{Path: p, Src: src, New: added[p], Lint: isChanged[p]}
			if m.Migrator == "flyway" && isChanged[p] {
				u.NoTransaction = flywayNoTransaction(in, p)
			}
			units = append(units, u)
		}
		res := lint.Run(units, lint.Options{Dialect: m.Dialect(), Migrator: m.Migrator, SmallTables: lint.SmallTables(s), Edition: lint.Edition(s)})
		all = append(all, res.Findings...)
		files += res.Files
		stmts += res.Statements
		for _, r := range res.Suppressed {
			suppressed[r]++
		}
	}
	keys := make([]string, 0, len(suppressed))
	for r := range suppressed {
		keys = append(keys, r)
	}
	sort.Strings(keys)
	for _, r := range keys {
		packs.Count("db", "suppression:"+strings.ToLower(r), suppressed[r])
	}
	detail := fmt.Sprintf("%d migration file(s) in %d path(s), %d statement(s)", files, paths, stmts)
	if n := len(keys); n > 0 {
		total := 0
		for _, c := range suppressed {
			total += c
		}
		detail += fmt.Sprintf(", %d suppressed", total)
	}
	return packs.Result{Status: packs.OK, Detail: detail, Findings: dedupe(all)}
}

// dedupe drops repeated findings (same file, line and rule), which a path
// listed twice in packs.db.migrations (for example by a repo tq-packs.yaml
// entry with another migrator) would otherwise report once per entry.
func dedupe(fs []packs.Finding) []packs.Finding {
	type key struct {
		file string
		line int
		rule string
	}
	seen := map[key]bool{}
	out := make([]packs.Finding, 0, len(fs))
	for _, f := range fs {
		k := key{f.File, f.Line, f.Rule}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, f)
	}
	return out
}

// flywayNoTransaction reads Flyway's per-script config <file>.conf for
// executeInTransaction=false.
func flywayNoTransaction(in *packs.Input, p string) bool {
	b, err := in.ReadNew(p+".conf", 64<<10)
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), "=")
		if ok && strings.EqualFold(strings.TrimSpace(k), "executeInTransaction") && strings.EqualFold(strings.TrimSpace(v), "false") {
			return true
		}
	}
	return false
}

func runRollback(ctx context.Context, in *packs.Input) packs.Result {
	specs := map[string][]byte{}
	for _, p := range in.ChangedPaths() {
		if strings.EqualFold(path.Ext(p), ".md") {
			if b, err := in.ReadNew(p, maxSpecBytes); err == nil {
				specs[p] = b
			}
		}
	}
	var all []packs.Finding
	n := 0
	for _, m := range lint.Migrations(in.Settings()) {
		changed, _ := lint.ChangedIn(in, m)
		if len(changed) == 0 {
			continue
		}
		n += len(changed)
		all = append(all, lint.Rollback(lint.RollbackInput{
			Migrator: m.Migrator,
			Changed:  changed,
			Files:    lint.Files(in, m),
			Read:     func(p string) ([]byte, error) { return in.ReadNew(p, maxMigrationBytes) },
			Specs:    specs,
		})...)
	}
	return packs.Result{Status: packs.OK, Detail: fmt.Sprintf("%d changed migration file(s) checked for a rollback", n), Findings: dedupe(all)}
}

func runSqlfluff(ctx context.Context, in *packs.Input) packs.Result {
	fail := lint.SqlfluffFail(in.Settings())
	var merged *packs.Result
	for _, eng := range []struct{ engine, dialect string }{{"postgres", "postgres"}, {"sqlserver", "tsql"}} {
		files := lintableChanged(in, eng.engine)
		if len(files) == 0 {
			continue
		}
		r := tools.Sqlfluff(ctx, in, eng.dialect, files, fail)
		if merged == nil {
			merged = &r
			continue
		}
		m := tools.Merge(*merged, r)
		merged = &m
	}
	if merged == nil {
		return packs.Result{Status: packs.Skip, Detail: "no changed forward migration"}
	}
	return *merged
}
