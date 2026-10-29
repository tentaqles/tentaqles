package sqllex

import (
	"path"
	"sort"
	"strings"
)

// Column is a column the replay has seen.
type Column struct {
	// Type is the normalised type ("varchar(20)", "text", "int").
	Type string
	// NotNull is true when the column was declared or set NOT NULL.
	NotNull bool
}

// CheckConstraint is a CHECK constraint the replay has seen.
type CheckConstraint struct {
	// NotNullOf is the column of a "CHECK (col IS NOT NULL)" constraint.
	NotNullOf string
	// Validated is false for a NOT VALID (Postgres) or WITH NOCHECK (T-SQL)
	// constraint until a VALIDATE CONSTRAINT / CHECK CONSTRAINT.
	Validated bool
}

// Table is a table the replay has seen created.
type Table struct {
	Name Name
	// File is the migration file that created it (slash path).
	File string
	Line int
	// Stub is a table the replay never saw created but saw altered: it is
	// existing (File is "") and only its altered columns and constraints are
	// known.
	Stub bool
	// Partitioned is a Postgres PARTITION BY table.
	Partitioned bool
	Columns     map[string]*Column
	Checks      map[string]*CheckConstraint
}

// Catalog is the table state built by replaying migrations in order. It
// knows only what the migrations say: a table it never saw created is
// "unknown" (Table returns nil) and the lint treats it as existing.
type Catalog struct {
	Dialect Dialect
	tables  map[string]*Table
}

// NewCatalog returns an empty catalog.
func NewCatalog(d Dialect) *Catalog {
	return &Catalog{Dialect: d, tables: map[string]*Table{}}
}

// Table returns the table, or nil when the replay never saw it created or
// altered (a table only altered is a Stub).
func (c *Catalog) Table(n Name) *Table { return c.tables[n.String()] }

// Tables returns every known table, sorted by name.
func (c *Catalog) Tables() []*Table {
	out := make([]*Table, 0, len(c.tables))
	for _, t := range c.tables {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name.String() < out[j].Name.String() })
	return out
}

// ApplyFile replays every statement of f (from file p).
func (c *Catalog) ApplyFile(p string, f *File) {
	for _, st := range f.Statements {
		c.Apply(p, st)
	}
}

// Apply replays one statement of file p: CREATE TABLE, DROP TABLE, ALTER
// TABLE (columns, renames, CHECK constraints, VALIDATE), sp_rename of a
// table. Anything else is ignored.
func (c *Catalog) Apply(p string, st Statement) {
	d := c.Dialect
	if d == TSQL {
		st = st.Unwrap()
	}
	switch {
	case st.Match(0, "CREATE"):
		i := st.Skip(1, []string{"OR", "REPLACE"}, []string{"GLOBAL|LOCAL"}, []string{"TEMP|TEMPORARY|UNLOGGED"})
		if !st.Match(i, "TABLE") {
			return
		}
		i = st.Skip(i+1, []string{"IF", "NOT", "EXISTS"})
		n, j := st.Name(i, d)
		if n.IsZero() || strings.HasPrefix(n.Object, "#") {
			return
		}
		t := &Table{Name: n, File: p, Line: st.Line, Columns: map[string]*Column{}, Checks: map[string]*CheckConstraint{}}
		if st.At(j).IsPunct("(") {
			end := st.Close(j)
			if end < 0 {
				end = len(st.Tokens)
			}
			for _, r := range st.Split(j+1, end) {
				c.tableElement(t, st, r[0], r[1])
			}
			if st.FindTop(end+1, "PARTITION", "BY") >= 0 {
				t.Partitioned = true
			}
		}
		c.tables[n.String()] = t
	case st.Match(0, "DROP", "TABLE"):
		i := st.Skip(2, []string{"IF", "EXISTS"})
		for _, r := range st.Split(i, len(st.Tokens)) {
			n, _ := st.Name(r[0], d)
			delete(c.tables, n.String())
		}
	case st.Match(0, "ALTER", "TABLE"):
		i := st.Skip(2, []string{"IF", "EXISTS"}, []string{"ONLY"})
		n, j := st.Name(i, d)
		if n.IsZero() {
			return
		}
		t := c.tables[n.String()]
		if t == nil {
			t = &Table{Name: n, Stub: true, Columns: map[string]*Column{}, Checks: map[string]*CheckConstraint{}}
			c.tables[n.String()] = t
		}
		if st.At(j).IsPunct("*") {
			j++
		}
		for _, r := range st.Split(j, len(st.Tokens)) {
			c.alterAction(t, st, r[0], r[1])
		}
	case d == TSQL && (st.Match(0, "EXEC|EXECUTE", "sp_rename") || st.Match(0, "sp_rename")):
		// EXEC sp_rename 'dbo.old', 'new' renames a table (no third argument).
		k := st.Find(0, "sp_rename")
		var args []string
		for _, r := range st.Split(k+1, len(st.Tokens)) {
			if st.At(r[0]).Kind == String {
				args = append(args, stringValue(st.At(r[0]).Text))
			} else {
				args = append(args, "")
			}
		}
		if len(args) == 2 {
			old := ParseName(args[0], d)
			if t := c.tables[old.String()]; t != nil {
				delete(c.tables, old.String())
				t.Name = Name{Schema: old.Schema, Object: strings.ToLower(args[1])}
				c.tables[t.Name.String()] = t
			}
		}
	}
}

// stringValue strips the quotes (and an N/E prefix) of a string token.
func stringValue(s string) string {
	if k := strings.IndexByte(s, '\''); k >= 0 {
		s = s[k:]
	}
	if len(s) >= 2 {
		s = s[1 : len(s)-1]
	}
	return strings.ReplaceAll(s, "''", "'")
}

// StringValue is the content of a String token (quotes and prefix dropped;
// dollar-quoted bodies returned without their tags).
func StringValue(t Token) string {
	if strings.HasPrefix(t.Text, "$") {
		k := strings.IndexByte(t.Text[1:], '$')
		if k >= 0 {
			tag := t.Text[:k+2]
			return strings.TrimSuffix(strings.TrimPrefix(t.Text, tag), tag)
		}
	}
	return stringValue(t.Text)
}

var constraintWords = []string{"CONSTRAINT", "PRIMARY", "UNIQUE", "CHECK", "FOREIGN", "EXCLUDE", "LIKE", "INDEX", "PERIOD"}

func (c *Catalog) tableElement(t *Table, st Statement, from, to int) {
	if from >= to {
		return
	}
	first := st.At(from)
	if first.Is(constraintWords...) {
		name, ci := "", from
		if first.Is("CONSTRAINT") {
			name = st.At(from + 1).Ident(c.Dialect)
			ci = from + 2
		}
		c.checkConstraint(t, st, name, ci, to, true)
		return
	}
	col := first.Ident(c.Dialect)
	if col == "" {
		return
	}
	typ, end := ColumnType(st, from+1, to)
	cc := &Column{Type: typ}
	if k := findIn(st, end, to, "NOT", "NULL"); k >= 0 {
		cc.NotNull = true
	}
	if findIn(st, end, to, "PRIMARY", "KEY") >= 0 {
		cc.NotNull = true
	}
	t.Columns[col] = cc
}

func findIn(st Statement, from, to int, words ...string) int {
	for i := from; i < to; i++ {
		if st.Match(i, words...) {
			return i
		}
	}
	return -1
}

// typeStop ends a column type.
var typeStop = map[string]bool{
	"NOT": true, "NULL": true, "DEFAULT": true, "CONSTRAINT": true, "PRIMARY": true, "UNIQUE": true,
	"CHECK": true, "REFERENCES": true, "COLLATE": true, "GENERATED": true, "IDENTITY": true, "USING": true,
	"WITH": true, "ROWGUIDCOL": true, "SPARSE": true, "FILESTREAM": true, "MASKED": true, "ENCRYPTED": true,
	"AS": true, "ON": true,
}

// ColumnType reads a column type starting at token i (up to to) and returns
// it normalised ("varchar(20)", "text", "int", "numeric(10,2)",
// "timestamptz") with the index after it.
func ColumnType(st Statement, i, to int) (string, int) {
	if to > len(st.Tokens) || to < 0 {
		to = len(st.Tokens)
	}
	var b strings.Builder
	for i < to {
		t := st.At(i)
		if t.Kind == Word && typeStop[t.Upper()] {
			break
		}
		if t.IsPunct(",") {
			break
		}
		if t.IsPunct("(") {
			end := st.Close(i)
			if end < 0 || end >= to {
				end = to - 1
			}
			b.WriteString("(")
			for k := i + 1; k < end; k++ {
				b.WriteString(strings.ToLower(st.At(k).Text))
			}
			b.WriteString(")")
			i = end + 1
			continue
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), ")") && t.Kind == Word {
			b.WriteString(" ")
		}
		if t.Kind == QuotedIdent {
			b.WriteString(strings.ToLower(unquote(t.Text)))
		} else {
			b.WriteString(strings.ToLower(t.Text))
		}
		i++
	}
	return NormalizeType(b.String()), i
}

var typeAliases = map[string]string{
	"character varying": "varchar", "char varying": "varchar", "character": "char",
	"integer": "int", "int4": "int", "int8": "bigint", "int2": "smallint",
	"float8": "double precision", "float4": "real", "bool": "boolean",
	"timestamp with time zone": "timestamptz", "timestamp without time zone": "timestamp",
	"decimal": "numeric", "national character varying": "nvarchar", "pg_catalog.text": "text",
}

// NormalizeType folds type aliases ("character varying(20)" → "varchar(20)").
func NormalizeType(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	base, args := s, ""
	if k := strings.IndexByte(s, '('); k >= 0 {
		base, args = strings.TrimSpace(s[:k]), s[k:]
	}
	if a, ok := typeAliases[base]; ok {
		base = a
	}
	return base + args
}

// SplitType splits a normalised type into its base and its argument list
// ("varchar(20)" → "varchar", ["20"]).
func SplitType(t string) (string, []string) {
	k := strings.IndexByte(t, '(')
	if k < 0 {
		return t, nil
	}
	inner := strings.TrimSuffix(t[k+1:], ")")
	rest := ""
	if e := strings.IndexByte(inner, ')'); e >= 0 {
		inner, rest = inner[:e], inner[e+1:]
	}
	return t[:k] + rest, strings.Split(inner, ",")
}

func (c *Catalog) checkConstraint(t *Table, st Statement, name string, i, to int, validated bool) {
	if !st.Match(i, "CHECK") {
		return
	}
	if findIn(st, i, to, "NOT", "VALID") >= 0 {
		validated = false
	}
	cc := &CheckConstraint{Validated: validated}
	// CHECK ( col IS NOT NULL )
	if st.At(i+1).IsPunct("(") && st.Close(i+1) == i+6 && st.Match(i+3, "IS", "NOT", "NULL") {
		cc.NotNullOf = st.At(i + 2).Ident(c.Dialect)
	}
	if name == "" {
		name = "#" + itoa(len(t.Checks))
	}
	t.Checks[name] = cc
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func (c *Catalog) alterAction(t *Table, st Statement, from, to int) {
	d := c.Dialect
	i := from
	nocheck := false
	if d == TSQL && st.Match(i, "WITH", "NOCHECK") {
		nocheck = true
		i += 2
	} else if d == TSQL && st.Match(i, "WITH", "CHECK") {
		i += 2
	}
	switch {
	case st.Match(i, "ADD"):
		i++
		if st.Match(i, "CONSTRAINT") {
			c.checkConstraint(t, st, st.At(i+1).Ident(d), i+2, to, !nocheck)
			return
		}
		if st.At(i).Is(constraintWords...) {
			c.checkConstraint(t, st, "", i, to, !nocheck)
			return
		}
		i = st.Skip(i, []string{"COLUMN"}, []string{"IF", "NOT", "EXISTS"})
		col := st.At(i).Ident(d)
		if col == "" {
			return
		}
		typ, end := ColumnType(st, i+1, to)
		cc := &Column{Type: typ}
		if findIn(st, end, to, "NOT", "NULL") >= 0 {
			cc.NotNull = true
		}
		t.Columns[col] = cc
	case st.Match(i, "DROP"):
		i++
		if st.Match(i, "CONSTRAINT") {
			k := st.Skip(i+1, []string{"IF", "EXISTS"})
			delete(t.Checks, st.At(k).Ident(d))
			return
		}
		i = st.Skip(i, []string{"COLUMN"}, []string{"IF", "EXISTS"})
		delete(t.Columns, st.At(i).Ident(d))
	case st.Match(i, "ALTER"):
		i = st.Skip(i+1, []string{"COLUMN"})
		col := st.At(i).Ident(d)
		cc := t.Columns[col]
		if cc == nil {
			cc = &Column{}
			t.Columns[col] = cc
		}
		k := i + 1
		switch {
		case st.Match(k, "SET", "NOT", "NULL"):
			cc.NotNull = true
		case st.Match(k, "DROP", "NOT", "NULL"):
			cc.NotNull = false
		case st.Match(k, "TYPE") || st.Match(k, "SET", "DATA", "TYPE"):
			if st.Match(k, "SET") {
				k += 2
			}
			cc.Type, _ = ColumnType(st, k+1, to)
		case d == TSQL:
			cc.Type, _ = ColumnType(st, k, to)
			if findIn(st, k, to, "NOT", "NULL") >= 0 {
				cc.NotNull = true
			} else if findIn(st, k, to, "NULL") >= 0 {
				cc.NotNull = false
			}
		}
	case st.Match(i, "VALIDATE", "CONSTRAINT"):
		if cc := t.Checks[st.At(i+2).Ident(d)]; cc != nil {
			cc.Validated = true
		}
	case d == TSQL && st.Match(i, "CHECK", "CONSTRAINT"):
		if cc := t.Checks[st.At(i+2).Ident(d)]; cc != nil {
			cc.Validated = true
		}
	case st.Match(i, "RENAME", "TO"):
		delete(c.tables, t.Name.String())
		t.Name = Name{Schema: t.Name.Schema, Object: st.At(i + 2).Ident(d)}
		c.tables[t.Name.String()] = t
	case st.Match(i, "RENAME"):
		k := st.Skip(i+1, []string{"COLUMN"})
		if st.Match(k+1, "TO") {
			old, nw := st.At(k).Ident(d), st.At(k+2).Ident(d)
			if cc := t.Columns[old]; cc != nil {
				delete(t.Columns, old)
				t.Columns[nw] = cc
			}
		}
	case st.Match(i, "SET", "SCHEMA"):
		delete(c.tables, t.Name.String())
		t.Name = Name{Schema: st.At(i + 2).Ident(d), Object: t.Name.Object}
		c.tables[t.Name.String()] = t
	}
}

// SortMigrations orders migration paths the way migrators apply them: by
// directory, then by base name with numbers compared numerically (so V2
// comes before V10 and 20240101_a before 20240102_a).
func SortMigrations(paths []string) {
	sort.SliceStable(paths, func(i, j int) bool {
		di, dj := path.Dir(paths[i]), path.Dir(paths[j])
		if di != dj {
			return di < dj
		}
		return NaturalLess(path.Base(paths[i]), path.Base(paths[j]))
	})
}

// NaturalLess compares two strings with digit runs compared as numbers.
func NaturalLess(a, b string) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ca, cb := a[i], b[j]
		if isDigit(ca) && isDigit(cb) {
			si := i
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			sj := j
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na := strings.TrimLeft(a[si:i], "0")
			nb := strings.TrimLeft(b[sj:j], "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		la, lb := lower(ca), lower(cb)
		if la != lb {
			return la < lb
		}
		i++
		j++
	}
	return len(a)-i < len(b)-j
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
}
