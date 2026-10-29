package lint

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs/db/sqllex"
)

const pg = sqllex.Postgres

// volatile are the default expressions that force a table rewrite (spec D
// §10 PG003, plus the uuid-ossp generators).
var volatile = map[string]bool{
	"NOW": true, "RANDOM": true, "GEN_RANDOM_UUID": true, "CLOCK_TIMESTAMP": true,
	"UUID_GENERATE_V1": true, "UUID_GENERATE_V4": true, "TIMEOFDAY": true,
}

func (l *linter) pg(i int, st sqllex.Statement) {
	switch {
	case isLockTimeoutSet(st, pg):
		if l.firstLock == nil {
			l.lockTimeout = true
		}
	case st.Match(0, "CREATE") && createIndexAt(st) > 0:
		l.pgCreateIndex(i, st)
	case st.Match(0, "ALTER", "TABLE"):
		l.pgAlterTable(i, st)
	case st.Match(0, "DROP", "TABLE"):
		k := st.Skip(2, []string{"IF", "EXISTS"})
		for _, r := range st.Split(k, len(st.Tokens)) {
			n, _ := st.Name(r[0], pg)
			if n.IsZero() || st.At(r[0]).Is("CASCADE", "RESTRICT") {
				continue
			}
			if l.isNew(n) {
				continue
			}
			l.locking(i, st, n)
			if !l.contracted(st) {
				l.add(st, st.Line, "PG008", n, "DROP TABLE "+n.String())
			}
		}
	case st.Match(0, "DROP", "INDEX"):
		if !st.Match(2, "CONCURRENTLY") {
			l.locking(i, st, sqllex.Name{})
		}
	case st.Match(0, "TRUNCATE"):
		k := st.Skip(1, []string{"TABLE"}, []string{"ONLY"})
		n, _ := st.Name(k, pg)
		l.locking(i, st, n)
	case (st.Match(0, "CREATE", "TRIGGER") || st.Match(0, "CREATE", "CONSTRAINT", "TRIGGER") || st.Match(0, "CREATE", "OR", "REPLACE", "TRIGGER") ||
		st.Match(0, "CREATE", "OR", "REPLACE", "CONSTRAINT", "TRIGGER")) && st.FindTop(0, "ON") > 0:
		on := st.FindTop(0, "ON")
		n, _ := st.Name(on+1, pg)
		l.locking(i, st, n)
	case st.Match(0, "UPDATE"):
		k := st.Skip(1, []string{"ONLY"})
		n, _ := st.Name(k, pg)
		if !l.isNew(n) && st.FindTop(0, "WHERE") < 0 {
			l.add(st, st.Line, "PG011", n, "UPDATE of "+n.String()+" with no WHERE")
		}
	case st.Match(0, "DELETE", "FROM"):
		k := st.Skip(2, []string{"ONLY"})
		n, _ := st.Name(k, pg)
		if !l.isNew(n) && st.FindTop(0, "WHERE") < 0 {
			l.add(st, st.Line, "PG011", n, "DELETE from "+n.String()+" with no WHERE")
		}
	case st.Match(0, "VACUUM"):
		full := false
		k := 1
		if st.Match(1, "FULL") {
			full, k = true, 2
		} else if st.At(1).IsPunct("(") {
			end := st.Close(1)
			for j := 2; j < end; j++ {
				if st.At(j).Is("FULL") && !st.At(j+1).Is("FALSE", "OFF") && !(st.At(j+1).Kind == sqllex.Number && st.At(j+1).Text == "0") {
					full = true
				}
			}
			k = end + 1
		}
		if full {
			n, _ := st.Name(st.Skip(k, []string{"VERBOSE"}, []string{"ANALYZE"}), pg)
			l.locking(i, st, n)
			l.add(st, st.Line, "PG012", n, "VACUUM FULL"+on(n)+" takes an ACCESS EXCLUSIVE lock")
		}
	case st.Match(0, "CLUSTER"):
		n, _ := st.Name(st.Skip(1, []string{"VERBOSE"}), pg)
		l.locking(i, st, n)
		l.add(st, st.Line, "PG012", n, "CLUSTER"+on(n)+" takes an ACCESS EXCLUSIVE lock")
	case st.Match(0, "LOCK"):
		n, _ := st.Name(st.Skip(1, []string{"TABLE"}, []string{"ONLY"}), pg)
		l.locking(i, st, n)
		l.add(st, st.Line, "PG012", n, "LOCK TABLE"+on(n))
	case st.Match(0, "REINDEX"):
		k := 1
		if st.At(1).IsPunct("(") {
			k = st.Close(1) + 1
		}
		if st.At(k).Is("INDEX", "TABLE", "SCHEMA", "DATABASE", "SYSTEM") {
			k++
		}
		if st.Match(k, "CONCURRENTLY") || st.Find(0, "CONCURRENTLY") > 0 {
			return
		}
		var n sqllex.Name
		if st.At(k - 1).Is("TABLE") {
			n, _ = st.Name(k, pg)
		}
		l.locking(i, st, n)
		l.add(st, st.Line, "PG012", n, "REINDEX without CONCURRENTLY"+on(n)+" blocks writes")
	}
}

func on(n sqllex.Name) string {
	if n.IsZero() {
		return ""
	}
	return " on " + n.String()
}

// createIndexAt returns the index of INDEX in CREATE [UNIQUE] INDEX, or -1.
func createIndexAt(st sqllex.Statement) int {
	k := 1
	for st.At(k).Is("UNIQUE", "CLUSTERED", "NONCLUSTERED", "COLUMNSTORE", "XML", "SPATIAL", "FULLTEXT") {
		k++
	}
	if st.At(k).Is("INDEX") {
		return k
	}
	return -1
}

func isLockTimeoutSet(st sqllex.Statement, d sqllex.Dialect) bool {
	if d == sqllex.TSQL {
		return st.Match(0, "SET", "LOCK_TIMEOUT")
	}
	if st.Match(0, "SET") {
		k := st.Skip(1, []string{"LOCAL|SESSION"})
		return st.At(k).Is("lock_timeout")
	}
	if st.Match(0, "SELECT") {
		k := st.Find(0, "set_config")
		return k > 0 && st.At(k+1).IsPunct("(") && st.At(k+2).Kind == sqllex.String && strings.EqualFold(sqllex.StringValue(st.At(k+2)), "lock_timeout")
	}
	return false
}

func (l *linter) pgCreateIndex(i int, st sqllex.Statement) {
	k := createIndexAt(st) + 1
	conc := st.Match(k, "CONCURRENTLY")
	onAt := st.FindTop(k, "ON")
	if onAt < 0 {
		return
	}
	n, _ := st.Name(st.Skip(onAt+1, []string{"ONLY"}), pg)
	if conc || l.isNew(n) {
		return
	}
	l.locking(i, st, n)
	l.add(st, st.Line, "PG001", n, "CREATE INDEX on "+n.String()+" without CONCURRENTLY blocks writes")
}

func (l *linter) pgAlterTable(i int, st sqllex.Statement) {
	k := st.Skip(2, []string{"IF", "EXISTS"}, []string{"ONLY"})
	n, j := st.Name(k, pg)
	if n.IsZero() || l.isNew(n) {
		return
	}
	if st.At(j).IsPunct("*") {
		j++
	}
	l.locking(i, st, n)
	t := l.cat.Table(n)
	// Validated NOT NULL checks known before this statement, plus those this
	// statement validates in an earlier action.
	for _, r := range st.Split(j, len(st.Tokens)) {
		a, b := r[0], r[1]
		line := st.At(a).Line
		switch {
		case st.Match(a, "ADD") && !st.Match(a+1, "CONSTRAINT") && !st.At(a+1).Is("PRIMARY", "UNIQUE", "CHECK", "FOREIGN", "EXCLUDE"):
			c := st.Skip(a+1, []string{"COLUMN"}, []string{"IF", "NOT", "EXISTS"})
			col := st.At(c).Ident(pg)
			_, typEnd := sqllex.ColumnType(st, c+1, b)
			notNull := findIn(st, typEnd, b, "NOT", "NULL") >= 0
			def := findIn(st, typEnd, b, "DEFAULT")
			if notNull && def < 0 && findIn(st, typEnd, b, "GENERATED") < 0 {
				l.add(st, line, "PG002", n, fmt.Sprintf("column %s.%s is NOT NULL with no DEFAULT", n, col))
			}
			if def >= 0 {
				for x := def + 1; x < b; x++ {
					tk := st.At(x)
					if tk.Kind == sqllex.Word && volatile[tk.Upper()] && st.At(x+1).IsPunct("(") {
						l.add(st, line, "PG003", n, fmt.Sprintf("column %s.%s defaults to %s(), which rewrites the table", n, col, strings.ToLower(tk.Text)))
						break
					}
				}
			}
		case st.Match(a, "ADD"):
			c := a + 1
			cname := ""
			if st.Match(c, "CONSTRAINT") {
				cname = st.At(c + 1).Ident(pg)
				c += 2
			}
			label := "constraint"
			if cname != "" {
				label = "constraint " + cname
			}
			switch {
			case st.At(c).Is("FOREIGN", "CHECK"):
				if findIn(st, c, b, "NOT", "VALID") < 0 {
					l.add(st, line, "PG006", n, fmt.Sprintf("%s %s on %s is validated under a lock", strings.ToUpper(st.At(c).Text), label, n))
				}
			case st.At(c).Is("UNIQUE", "PRIMARY"):
				if findIn(st, c, b, "USING", "INDEX") < 0 {
					l.add(st, line, "PG007", n, fmt.Sprintf("%s %s on %s builds its index under a lock", strings.ToUpper(st.At(c).Text), label, n))
				}
			}
		case st.Match(a, "ALTER"):
			c := st.Skip(a+1, []string{"COLUMN"})
			col := st.At(c).Ident(pg)
			x := c + 1
			switch {
			case st.Match(x, "TYPE") || st.Match(x, "SET", "DATA", "TYPE"):
				if st.Match(x, "SET") {
					x += 2
				}
				nt, end := sqllex.ColumnType(st, x+1, b)
				old := ""
				if t != nil && t.Columns[col] != nil {
					old = t.Columns[col].Type
				}
				if findIn(st, end, b, "USING") >= 0 || !safeWidening(old, nt) {
					from := old
					if from == "" {
						from = "its current type"
					}
					l.add(st, line, "PG004", n, fmt.Sprintf("changing %s.%s from %s to %s rewrites the table", n, col, from, nt))
				}
			case st.Match(x, "SET", "NOT", "NULL"):
				if !validatedNotNull(l.cat.Table(n), col) && !validatedInStatement(st, j, a, col) {
					l.add(st, line, "PG005", n, fmt.Sprintf("SET NOT NULL on %s.%s scans the table under an ACCESS EXCLUSIVE lock", n, col))
				}
			}
		case st.Match(a, "DROP") && !st.At(a+1).Is("CONSTRAINT", "DEFAULT", "EXPRESSION", "IDENTITY"):
			if l.contracted(st) {
				continue
			}
			c := st.Skip(a+1, []string{"COLUMN"}, []string{"IF", "EXISTS"})
			l.add(st, line, "PG008", n, fmt.Sprintf("DROP COLUMN %s.%s", n, st.At(c).Ident(pg)))
		case st.Match(a, "RENAME", "CONSTRAINT"):
		case st.Match(a, "RENAME", "TO"):
			if !l.contracted(st) {
				l.add(st, line, "PG008", n, fmt.Sprintf("RENAME TO of %s breaks the running code", n))
			}
		case st.Match(a, "RENAME"):
			if !l.contracted(st) {
				c := st.Skip(a+1, []string{"COLUMN"})
				l.add(st, line, "PG008", n, fmt.Sprintf("RENAME COLUMN %s.%s breaks the running code", n, st.At(c).Ident(pg)))
			}
		}
	}
}

func findIn(st sqllex.Statement, from, to int, words ...string) int {
	for i := from; i < to; i++ {
		if st.Match(i, words...) {
			return i
		}
	}
	return -1
}

func validatedNotNull(t *sqllex.Table, col string) bool {
	if t == nil {
		return false
	}
	for _, c := range t.Checks {
		if c.NotNullOf == col && c.Validated {
			return true
		}
	}
	return false
}

// validatedInStatement: an earlier action of the same ALTER TABLE adds a
// validated CHECK (col IS NOT NULL) (no NOT VALID).
func validatedInStatement(st sqllex.Statement, from, upto int, col string) bool {
	for _, r := range st.Split(from, upto) {
		a, b := r[0], r[1]
		if !st.Match(a, "ADD") {
			continue
		}
		c := findIn(st, a, b, "CHECK")
		if c < 0 || findIn(st, c, b, "NOT", "VALID") >= 0 {
			continue
		}
		if st.At(c+2).Ident(pg) == col && st.Match(c+3, "IS", "NOT", "NULL") {
			return true
		}
	}
	return false
}

// safeWidening: varchar(n) to varchar(m >= n), to unbounded varchar, or to
// text (spec D §10 PG004).
func safeWidening(old, nw string) bool {
	if old == "" {
		return false
	}
	if old == nw {
		return true
	}
	ob, oa := sqllex.SplitType(old)
	nb, na := sqllex.SplitType(nw)
	if ob != "varchar" {
		return ob == "text" && nb == "text"
	}
	switch nb {
	case "text":
		return true
	case "varchar":
		if len(na) == 0 {
			return true
		}
		if len(oa) == 0 {
			return false
		}
		o, err1 := strconv.Atoi(strings.TrimSpace(oa[0]))
		m, err2 := strconv.Atoi(strings.TrimSpace(na[0]))
		return err1 == nil && err2 == nil && m >= o
	}
	return false
}

// migratorWraps: whether the migrator runs a Postgres file inside a
// transaction (spec D §10 PG010). golang-migrate sends a file as one
// multi-statement query, which Postgres runs as one implicit transaction.
func (l *linter) migratorWraps() (bool, string) {
	if l.unit.NoTransaction {
		return false, ""
	}
	switch l.o.Migrator {
	case "dbmate":
		for _, c := range l.file.Comments {
			t := strings.ToLower(c.Text)
			if strings.HasPrefix(t, "migrate:up") && strings.Contains(t, "transaction:false") {
				return false, ""
			}
		}
		return true, "dbmate runs each migration in a transaction unless -- migrate:up transaction:false"
	case "flyway":
		return true, "Flyway runs each migration in a transaction unless its <file>.conf sets executeInTransaction=false"
	case "supabase":
		return true, "the Supabase CLI applies each migration file in a transaction"
	case "drizzle":
		return true, "drizzle-kit migrate applies pending migrations in one transaction"
	case "golang-migrate":
		if len(l.file.Statements) > 1 {
			return true, "golang-migrate sends the file as one multi-statement query, which Postgres runs as one transaction; keep the CONCURRENTLY statement alone in its file"
		}
	}
	return false, ""
}

func (l *linter) pgTransactions() {
	var conc []int
	begin := -1
	for i, st := range l.file.Statements {
		if begin < 0 && (st.Match(0, "BEGIN") && !st.Match(1, "ATOMIC") || st.Match(0, "START", "TRANSACTION")) {
			begin = i
		}
		if (st.Match(0, "CREATE") || st.Match(0, "DROP", "INDEX") || st.Match(0, "REINDEX") || st.Match(0, "ALTER", "TABLE") || st.Match(0, "VACUUM")) && st.Find(0, "CONCURRENTLY") > 0 {
			conc = append(conc, i)
		}
	}
	if len(conc) == 0 {
		return
	}
	why := ""
	if begin >= 0 {
		why = fmt.Sprintf("the file opens a transaction at line %d", l.file.Statements[begin].Line)
	} else if w, reason := l.migratorWraps(); w {
		why = reason
	}
	if why == "" {
		return
	}
	for _, i := range conc {
		st := l.file.Statements[i]
		l.addLevel(st, st.Line, "PG010", "error", "CONCURRENTLY cannot run inside a transaction: "+why)
	}
}
