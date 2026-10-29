package lint

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/db/sqllex"
)

const ms = sqllex.TSQL

// noOnlineEditions have no online index operations.
var noOnlineEditions = map[string]bool{"standard": true, "web": true, "express": true}

// Editions are the accepted packs.db.edition values.
var Editions = []string{"enterprise", "developer", "standard", "web", "express", "azure-sql", "managed-instance"}

func (l *linter) tsql(i int, st sqllex.Statement) {
	switch {
	case isLockTimeoutSet(st, ms):
		if l.firstLock == nil {
			l.lockTimeout = true
		}
	case st.Match(0, "CREATE") && createIndexAt(st) > 0:
		k := createIndexAt(st)
		onAt := st.FindTop(k, "ON")
		if onAt < 0 {
			return
		}
		n, _ := st.Name(onAt+1, ms)
		if l.isNew(n) {
			return
		}
		l.locking(i, st, n)
		if !onlineOn(st) {
			l.msOnline(st, n, "CREATE INDEX on "+n.String()+" without ONLINE = ON blocks writes")
		}
	case st.Match(0, "ALTER", "INDEX"):
		onAt := st.FindTop(2, "ON")
		if onAt < 0 || st.FindTop(onAt, "REBUILD") < 0 {
			return
		}
		n, _ := st.Name(onAt+1, ms)
		if l.isNew(n) {
			return
		}
		l.locking(i, st, n)
		if !onlineOn(st) {
			l.msOnline(st, n, "ALTER INDEX … REBUILD on "+n.String()+" without ONLINE = ON blocks the table")
		}
	case st.Match(0, "ALTER", "TABLE"):
		l.msAlterTable(i, st)
	case st.Match(0, "DROP", "TABLE"):
		k := st.Skip(2, []string{"IF", "EXISTS"})
		for _, r := range st.Split(k, len(st.Tokens)) {
			n, _ := st.Name(r[0], ms)
			if n.IsZero() || l.isNew(n) {
				continue
			}
			l.locking(i, st, n)
			if !l.contracted(st) {
				l.add(st, st.Line, "MS005", n, "DROP TABLE "+n.String())
			}
		}
	case st.Match(0, "TRUNCATE", "TABLE"):
		n, _ := st.Name(2, ms)
		l.locking(i, st, n)
	case st.Match(0, "EXEC|EXECUTE", "sp_rename") || st.Match(0, "sp_rename"):
		k := st.Find(0, "sp_rename")
		obj := ""
		if st.At(k+1).Kind == sqllex.String {
			obj = sqllex.StringValue(st.At(k + 1))
		}
		l.locking(i, st, sqllex.Name{})
		if !l.contracted(st) {
			what := "sp_rename"
			if obj != "" {
				what += " of " + strings.ToLower(obj)
			}
			l.addLevel(st, st.Line, "MS005", packs.LevelError, what+" breaks the running code")
		}
	case st.Match(0, "UPDATE"):
		n, _ := st.Name(st.Skip(1, []string{"TOP"}), ms)
		if st.At(1).Is("TOP") {
			n = sqllex.Name{}
		}
		if (n.IsZero() || !l.isNew(n)) && st.FindTop(0, "WHERE") < 0 {
			l.add(st, st.Line, "MS007", n, "UPDATE"+on(n)+" with no WHERE")
		}
	case st.Match(0, "DELETE"):
		k := st.Skip(1, []string{"FROM"})
		n, _ := st.Name(k, ms)
		if !l.isNew(n) && st.FindTop(0, "WHERE") < 0 {
			l.add(st, st.Line, "MS007", n, "DELETE"+on(n)+" with no WHERE")
		}
	}
}

func (l *linter) msOnline(st sqllex.Statement, n sqllex.Name, msg string) {
	if noOnlineEditions[strings.ToLower(l.o.Edition)] {
		l.addLevel(st, st.Line, "MS001", packs.LevelWarn, msg+" (the "+strings.ToLower(l.o.Edition)+" edition has no online index operations)")
		return
	}
	l.add(st, st.Line, "MS001", n, msg)
}

// onlineOn finds ONLINE = ON in a WITH (…) option list.
func onlineOn(st sqllex.Statement) bool {
	for k := 0; k+2 < len(st.Tokens); k++ {
		if st.At(k).Is("ONLINE") && st.At(k+1).IsPunct("=") && st.At(k+2).Is("ON") {
			return true
		}
	}
	return false
}

func (l *linter) msAlterTable(i int, st sqllex.Statement) {
	n, j := st.Name(2, ms)
	if n.IsZero() || l.isNew(n) {
		return
	}
	l.locking(i, st, n)
	t := l.cat.Table(n)
	nocheck := false
	if st.Match(j, "WITH", "NOCHECK") {
		nocheck, j = true, j+2
	} else if st.Match(j, "WITH", "CHECK") {
		j += 2
	}
	action := ""
	for _, r := range st.Split(j, len(st.Tokens)) {
		a, b := r[0], r[1]
		line := st.At(a).Line
		switch {
		case st.Match(a, "ADD"):
			action = "ADD"
			a++
		case st.Match(a, "DROP"):
			action = "DROP"
			a++
		case st.Match(a, "ALTER", "COLUMN"):
			action = "ALTER"
		case st.Match(a, "CHECK|NOCHECK", "CONSTRAINT"):
			action = "CHECK"
		}
		switch action {
		case "ADD":
			c := a
			if st.Match(c, "CONSTRAINT") {
				cname := st.At(c + 1).Ident(ms)
				c += 2
				l.msConstraint(st, line, n, c, nocheck, "constraint "+cname)
				continue
			}
			if st.At(c).Is("PRIMARY", "UNIQUE", "CHECK", "FOREIGN", "INDEX") {
				l.msConstraint(st, line, n, c, nocheck, "constraint")
				continue
			}
			col := st.At(c).Ident(ms)
			_, end := sqllex.ColumnType(st, c+1, b)
			if findIn(st, end, b, "NOT", "NULL") >= 0 && findIn(st, end, b, "DEFAULT") < 0 && findIn(st, end, b, "IDENTITY") < 0 && findIn(st, c+1, b, "AS") < 0 {
				l.add(st, line, "MS002", n, fmt.Sprintf("column %s.%s is NOT NULL with no DEFAULT", n, col))
			}
		case "DROP":
			if st.Match(a, "CONSTRAINT") || st.Match(a, "PERIOD") {
				continue
			}
			if l.contracted(st) {
				continue
			}
			c := st.Skip(a, []string{"COLUMN"}, []string{"IF", "EXISTS"})
			l.add(st, line, "MS005", n, fmt.Sprintf("DROP COLUMN %s.%s", n, st.At(c).Ident(ms)))
		case "ALTER":
			c := a + 2
			col := st.At(c).Ident(ms)
			nt, _ := sqllex.ColumnType(st, c+1, b)
			old := ""
			if t != nil && t.Columns[col] != nil {
				old = t.Columns[col].Type
			}
			switch sizeOfData(old, nt) {
			case 1:
				l.add(st, line, "MS003", n, fmt.Sprintf("changing %s.%s from %s to %s is a size-of-data operation", n, col, old, nt))
			case 0:
				l.addLevel(st, line, "MS003", packs.LevelWarn, fmt.Sprintf("ALTER COLUMN %s.%s to %s: the old type is not in the replayed migrations, so a type change or shrink cannot be ruled out", n, col, nt))
			}
		}
	}
}

func (l *linter) msConstraint(st sqllex.Statement, line int, n sqllex.Name, c int, nocheck bool, label string) {
	if st.At(c).Is("FOREIGN", "CHECK") && !nocheck {
		l.add(st, line, "MS004", n, fmt.Sprintf("%s %s on %s is checked against every row under a lock", strings.ToUpper(st.At(c).Text), label, n))
	}
}

// sizeOfData: 1 when old → nw changes the base type or shrinks the size, -1
// when it is safe (same type, same or larger size), 0 when the old type is
// unknown.
func sizeOfData(old, nw string) int {
	if old == "" {
		return 0
	}
	if old == nw {
		return -1
	}
	ob, oa := sqllex.SplitType(old)
	nb, na := sqllex.SplitType(nw)
	if ob != nb {
		return 1
	}
	if len(oa) != len(na) {
		return 1
	}
	for k := range oa {
		o, nv := strings.TrimSpace(oa[k]), strings.TrimSpace(na[k])
		if nv == "max" {
			continue
		}
		if o == "max" {
			return 1
		}
		x, e1 := strconv.Atoi(o)
		y, e2 := strconv.Atoi(nv)
		if e1 != nil || e2 != nil || y < x {
			return 1
		}
	}
	return -1
}

// msTransactions: MS008, DDL inside BEGIN TRAN that spans a GO.
func (l *linter) msTransactions() {
	sts := l.file.Statements
	for i := 0; i < len(sts); i++ {
		st := sts[i]
		if !(st.Match(0, "BEGIN", "TRAN|TRANSACTION") || st.Match(0, "BEGIN", "DISTRIBUTED")) {
			continue
		}
		ddl, spans := false, false
		j := i + 1
		for ; j < len(sts); j++ {
			s := sts[j]
			if s.Match(0, "COMMIT") || s.Match(0, "ROLLBACK") {
				break
			}
			if s.Batch != st.Batch {
				spans = true
			}
			if s.Match(0, "CREATE") || s.Match(0, "ALTER") || s.Match(0, "DROP") || s.Match(0, "EXEC|EXECUTE", "sp_rename") {
				ddl = true
			}
		}
		if j < len(sts) && sts[j].Batch != st.Batch {
			spans = true
		}
		if ddl && spans {
			l.addLevel(st, st.Line, "MS008", packs.LevelError, "the transaction opened here contains DDL and spans a GO batch boundary")
		}
		i = j
	}
}
