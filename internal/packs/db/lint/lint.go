// Package lint is the db pack's migration SQL lint (spec D §10): the
// Postgres rules PG001…PG012 and the SQL Server / Azure SQL rules
// MS001…MS008, over statements from internal/packs/db/sqllex, plus the
// rollback rules of db/migration-rollback (rollback.go).
//
// The lint replays the migrations in order: a table created in the same
// file, or by an earlier migration of this diff that has not been applied
// yet, is new, and the lock rules skip it. Partitioned tables and
// packs.db.small_tables downgrade the lock rules to warn.
//
// Findings name tables, columns and rule ids only; they never quote a
// literal from the file. A finding is suppressed in the source with
// `-- tq:allow <rule>: <reason>` on the line before the statement; a
// suppression without a reason is itself an error (SUP001). In the file
// header (before the first statement) a tq:allow covers the file rules
// only: PG009, PG010, MS006 and MS008.
package lint

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/db/sqllex"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
)

// Rule describes one lint rule.
type Rule struct {
	ID, Title, Fix string
	// Lock rules downgrade to warn for small and partitioned tables.
	Lock bool
}

var rules = []Rule{
	{"PG001", "CREATE INDEX without CONCURRENTLY on an existing table", "use CREATE INDEX CONCURRENTLY, outside a transaction", true},
	{"PG002", "ADD COLUMN … NOT NULL without DEFAULT on an existing table", "add it nullable, backfill, add CHECK (col IS NOT NULL) NOT VALID, VALIDATE it, then SET NOT NULL", true},
	{"PG003", "ADD COLUMN with a volatile default on an existing table", "add the column without the default, then SET DEFAULT for new rows only and backfill in batches", true},
	{"PG004", "ALTER COLUMN … TYPE rewrites an existing table", "add a new column and expand and contract (only varchar widening to a larger size or to text is safe)", true},
	{"PG005", "SET NOT NULL with no validated CHECK (col IS NOT NULL) earlier", "add CHECK (col IS NOT NULL) NOT VALID, VALIDATE CONSTRAINT, then SET NOT NULL", true},
	{"PG006", "FOREIGN KEY or CHECK constraint without NOT VALID on an existing table", "add it NOT VALID, then VALIDATE CONSTRAINT in a later statement", true},
	{"PG007", "UNIQUE or PRIMARY KEY constraint not USING INDEX", "build a unique index CONCURRENTLY, then ADD CONSTRAINT … USING INDEX", true},
	{"PG008", "DROP COLUMN, DROP TABLE or RENAME with no tq:contract marker", "expand and contract across releases (db-migration §3), then mark the contract step with -- tq:contract <expand ref>", false},
	{"PG009", "locking statement in a file with no SET lock_timeout", "put SET lock_timeout = '5s'; at the top of the file", false},
	{"PG010", "CONCURRENTLY inside a transaction", "move it to its own migration that the migrator runs outside a transaction (its no-transaction marker)", false},
	{"PG011", "UPDATE or DELETE with no WHERE in a schema migration", "move the backfill to a batched job, or add the WHERE that bounds it", false},
	{"PG012", "VACUUM FULL, CLUSTER, LOCK TABLE or REINDEX without CONCURRENTLY", "use an online alternative (REINDEX … CONCURRENTLY, pg_repack, a plain VACUUM)", true},
	{"MS001", "CREATE INDEX or ALTER INDEX … REBUILD without ONLINE = ON on an existing table", "add WITH (ONLINE = ON)", true},
	{"MS002", "ADD <col> … NOT NULL without DEFAULT on an existing table", "add it NULL, backfill in batches, then ALTER COLUMN … NOT NULL, or add a DEFAULT constraint", true},
	{"MS003", "ALTER COLUMN that changes the type or shrinks the size (a size-of-data operation)", "add a new column and expand and contract", true},
	{"MS004", "FOREIGN KEY or CHECK constraint without WITH NOCHECK on an existing table", "ALTER TABLE … WITH NOCHECK ADD CONSTRAINT, then ALTER TABLE … WITH CHECK CHECK CONSTRAINT in a later step", true},
	{"MS005", "DROP COLUMN, DROP TABLE or sp_rename with no tq:contract marker", "expand and contract across releases (db-migration §3), then mark the contract step with -- tq:contract <expand ref>", false},
	{"MS006", "locking statement in a file with no SET LOCK_TIMEOUT", "put SET LOCK_TIMEOUT 5000; at the top of the file", false},
	{"MS007", "UPDATE or DELETE without WHERE", "move the backfill to a batched job, or add the WHERE that bounds it", false},
	{"MS008", "DDL inside BEGIN TRAN that spans a GO batch boundary", "commit the transaction before GO, or drop the GO", false},
}

// Rules returns the PG and MS rules.
func Rules() []Rule { return append([]Rule(nil), rules...) }

// RuleByID returns a rule.
func RuleByID(id string) (Rule, bool) {
	for _, r := range rules {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}

// SUP001 is the finding for a tq:allow without a reason.
const SUP001 = "SUP001"

// SQL001 is the finding for a statement the framework scanner marks
// ambiguous (scan.Statement.Ambiguous): a nested block comment that MySQL
// or SQLite would end early, or a MySQL executable comment /*! ... */. Code
// may hide there, so the statement is unknown and dangerous: an error that
// no tq:allow suppresses (the comment itself decides where a statement and
// its directives start).
const SQL001 = "SQL001"

// ambiguous lists the SQL001 findings of one file.
func ambiguous(path string, src []byte) []packs.Finding {
	var out []packs.Finding
	for _, st := range scan.SQLStatements(src) {
		if st.Ambiguous() {
			out = append(out, packs.Finding{File: path, Line: st.Line, Level: packs.LevelError, Rule: SQL001,
				Message: "a nested /* */ or a /*! */ comment that another engine reads as code: the statement is treated as unknown and dangerous; remove the nested or executable comment"})
		}
	}
	return out
}

// Options configure one engine group (one packs.db.migrations entry).
type Options struct {
	Dialect sqllex.Dialect
	// Migrator is packs.db.migrations[].migrator (raw when "").
	Migrator string
	// SmallTables downgrade the lock rules to warn ("public.feature_flags").
	SmallTables []string
	// Edition is packs.db.edition (SQL Server): standard, web and express
	// have no online index operations, so MS001 is a warn there.
	Edition string
}

// Unit is one migration file in apply order.
type Unit struct {
	Path string // slash path, repo relative
	Src  []byte
	// New is true when the file is added by this diff (not applied yet).
	New bool
	// Lint is true when the file is linted (a changed file); other units
	// are only replayed.
	Lint bool
	// NoTransaction is true when the migrator is told to run this file
	// outside a transaction by a side file (Flyway's <file>.conf with
	// executeInTransaction=false).
	NoTransaction bool
}

// Result is the lint outcome.
type Result struct {
	Findings []packs.Finding
	// Suppressed lists the rule of each finding a tq:allow suppressed.
	Suppressed []string
	Files      int
	Statements int
}

// Run replays the units in the given order and lints those marked Lint.
func Run(units []Unit, o Options) Result {
	cat := sqllex.NewCatalog(o.Dialect)
	newFiles := map[string]bool{}
	small := map[string]bool{}
	for _, s := range o.SmallTables {
		if n := sqllex.ParseName(s, o.Dialect); !n.IsZero() {
			small[n.String()] = true
		}
	}
	var res Result
	for _, u := range units {
		if u.New {
			newFiles[u.Path] = true
		}
		f := upSection(sqllex.Lex(u.Src, o.Dialect), o.Migrator)
		if !u.Lint {
			cat.ApplyFile(u.Path, f)
			continue
		}
		l := &linter{o: o, cat: cat, path: u.Path, file: f, newFiles: newFiles, small: small, unit: u}
		l.run()
		res.Files++
		res.Statements += len(f.Statements)
		res.Findings = append(res.Findings, l.out...)
		res.Findings = append(res.Findings, ambiguous(u.Path, u.Src)...)
		res.Suppressed = append(res.Suppressed, l.suppressed...)
	}
	sort.SliceStable(res.Findings, func(i, j int) bool {
		a, b := res.Findings[i], res.Findings[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Rule < b.Rule
	})
	return res
}

type linter struct {
	o          Options
	cat        *sqllex.Catalog
	path       string
	file       *sqllex.File
	unit       Unit
	newFiles   map[string]bool
	small      map[string]bool
	out        []packs.Finding
	suppressed []string

	header []sqllex.Directive
	// lock timeout state
	lockTimeout bool
	firstLock   *lockRef
}

type lockRef struct {
	st    int
	line  int
	soft  bool // every locking target so far is small or partitioned
	count int
}

func (l *linter) run() {
	l.header = l.file.HeaderDirectives()
	for _, d := range allDirectives(l.file) {
		if d.Name == "allow" && !d.HasReason {
			l.out = append(l.out, packs.Finding{File: l.path, Line: d.Line, Level: packs.LevelError, Rule: SUP001,
				Message: "tq:allow needs a reason: -- tq:allow <rule>: <reason>"})
		}
	}
	for i, st := range l.file.Statements {
		if l.o.Dialect == sqllex.TSQL {
			st = st.Unwrap()
			l.file.Statements[i] = st
			l.tsql(i, st)
		} else {
			l.pg(i, st)
		}
		l.cat.Apply(l.path, st)
	}
	l.fileRules()
}

func allDirectives(f *sqllex.File) []sqllex.Directive {
	var ds []sqllex.Directive
	for _, st := range f.Statements {
		ds = append(ds, st.Directives()...)
	}
	return ds
}

// isNew: created in this file, or by an earlier unapplied migration.
func (l *linter) isNew(n sqllex.Name) bool {
	if strings.HasPrefix(n.Object, "#") {
		return true // T-SQL temp table
	}
	t := l.cat.Table(n)
	return t != nil && (t.File == l.path || l.newFiles[t.File])
}

func (l *linter) soft(n sqllex.Name) bool {
	if l.small[n.String()] {
		return true
	}
	t := l.cat.Table(n)
	return t != nil && t.Partitioned
}

// add records a finding for statement st, honouring tq:allow.
func (l *linter) add(st sqllex.Statement, line int, rule string, target sqllex.Name, msg string) {
	r, _ := RuleByID(rule)
	level := packs.LevelError
	if r.Lock && !target.IsZero() && l.soft(target) {
		level = packs.LevelWarn
	}
	l.addLevel(st, line, rule, level, msg)
}

// fileRules are the rules about the file as a whole (lock timeout and
// transaction shape). Only they honour a tq:allow in the file header; any
// other rule is allowed on the line before the statement (spec D §5), so one
// header line cannot silence a rule for every statement of the file.
var fileRules = map[string]bool{"PG009": true, "PG010": true, "MS006": true, "MS008": true}

func (l *linter) addLevel(st sqllex.Statement, line int, rule, level, msg string) {
	if ok, reason, _ := sqllex.Allows(st.Directives(), rule); ok && reason {
		l.suppressed = append(l.suppressed, rule)
		return
	}
	if ok, reason, _ := sqllex.Allows(l.header, rule); ok && reason && fileRules[rule] {
		l.suppressed = append(l.suppressed, rule)
		return
	}
	r, _ := RuleByID(rule)
	if r.Fix != "" {
		msg += "; " + r.Fix
	}
	if line == 0 {
		line = st.Line
	}
	l.out = append(l.out, packs.Finding{File: l.path, Line: line, Level: level, Rule: rule, Message: msg})
}

// locking records a statement that takes a strong lock on an existing table
// (PG009 / MS006).
func (l *linter) locking(i int, st sqllex.Statement, target sqllex.Name) {
	if !target.IsZero() && l.isNew(target) {
		return
	}
	soft := !target.IsZero() && l.soft(target)
	if l.firstLock == nil {
		l.firstLock = &lockRef{st: i, line: st.Line, soft: soft}
	} else if !soft {
		l.firstLock.soft = false
	}
	l.firstLock.count++
}

// contracted reports a tq:contract marker on the statement or the file header.
func (l *linter) contracted(st sqllex.Statement) bool {
	for _, ds := range [][]sqllex.Directive{st.Directives(), l.header} {
		for _, d := range ds {
			if d.Name == "contract" && len(d.Args) > 0 {
				return true
			}
		}
	}
	return false
}

func (l *linter) fileRules() {
	if l.firstLock != nil && !l.lockTimeout {
		st := l.file.Statements[l.firstLock.st]
		rule, what := "PG009", "SET lock_timeout"
		if l.o.Dialect == sqllex.TSQL {
			rule, what = "MS006", "SET LOCK_TIMEOUT"
		}
		level := packs.LevelError
		if l.firstLock.soft {
			level = packs.LevelWarn
		}
		l.addLevel(st, l.firstLock.line, rule, level, fmt.Sprintf("%d locking statement(s) and no %s before the first", l.firstLock.count, what))
	}
	if l.o.Dialect == sqllex.Postgres {
		l.pgTransactions()
	} else {
		l.msTransactions()
	}
}

// upSection drops a dbmate file's "-- migrate:down" section: dbmate keeps
// the rollback in the same file, and only the up section is applied.
func upSection(f *sqllex.File, migrator string) *sqllex.File {
	if migrator != "dbmate" {
		return f
	}
	downPos := -1
	for _, c := range f.Comments {
		if strings.HasPrefix(strings.ToLower(c.Text), "migrate:down") {
			downPos = c.Pos
			break
		}
	}
	if downPos < 0 {
		return f
	}
	var up []sqllex.Statement
	for _, st := range f.Statements {
		if st.Tokens[0].Pos < downPos {
			up = append(up, st)
		}
	}
	f.Statements = up
	return f
}
