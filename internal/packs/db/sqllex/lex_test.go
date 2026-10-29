package sqllex

import (
	"reflect"
	"strings"
	"testing"
)

func keywords(f *File) []string {
	var out []string
	for _, s := range f.Statements {
		out = append(out, s.Keyword())
	}
	return out
}

func lines(f *File) []int {
	var out []int
	for _, s := range f.Statements {
		out = append(out, s.Line)
	}
	return out
}

func TestPostgresSplitting(t *testing.T) {
	src := `-- header
SET lock_timeout = '5s';
/* block /* nested ; */ still comment ; */
CREATE TABLE acme.orders (id int, note text DEFAULT 'a;b');
CREATE FUNCTION f() RETURNS int AS $body$ select 1; select 2; $body$ LANGUAGE sql;
DO $$ BEGIN PERFORM 1; END $$;
SELECT E'it\'s; here', "weird;name" FROM t;
CREATE FUNCTION g() RETURNS int LANGUAGE sql
BEGIN ATOMIC
  SELECT 1;
  SELECT 2;
END;
BEGIN;
COMMIT
`
	f := Lex([]byte(src), Postgres)
	want := []string{"SET", "CREATE", "CREATE", "DO", "SELECT", "CREATE", "BEGIN", "COMMIT"}
	if got := keywords(f); !reflect.DeepEqual(got, want) {
		t.Fatalf("keywords = %v, want %v", got, want)
	}
	if got, want := lines(f), []int{2, 4, 5, 6, 7, 8, 13, 14}; !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %v, want %v", got, want)
	}
	if len(f.Statements[0].Leading) != 1 || f.Statements[0].Leading[0].Text != "header" {
		t.Fatalf("leading = %+v", f.Statements[0].Leading)
	}
	if len(f.Statements[1].Leading) != 1 || !strings.Contains(f.Statements[1].Leading[0].Text, "nested") {
		t.Fatalf("nested comment = %+v", f.Statements[1].Leading)
	}
	if f.Statements[5].EndLine != 12 {
		t.Fatalf("atomic end line = %d", f.Statements[5].EndLine)
	}
	sel := f.Statements[4]
	if sel.At(1).Kind != String || sel.At(3).Kind != QuotedIdent || sel.At(3).Ident(Postgres) != "weird;name" {
		t.Fatalf("select tokens = %+v", sel.Tokens)
	}
}

func TestTSQLBatchesAndNoSemicolons(t *testing.T) {
	src := "SET LOCK_TIMEOUT 5000\r\n" +
		"ALTER TABLE [dbo].[Orders]\r\n  ADD [Note] nvarchar(20) NULL\r\n" +
		"UPDATE dbo.Orders\r\nSET Note = N'x;y'\r\nWHERE Id = 1\r\n" +
		"GO\r\n" +
		"CREATE PROCEDURE dbo.p AS\r\nBEGIN\r\n  UPDATE dbo.Orders SET Note = 'z';\r\n  DELETE FROM dbo.Orders;\r\nEND\r\n" +
		"go 2 -- twice\r\n" +
		"ALTER TABLE dbo.Orders\r\nDROP COLUMN Note\r\n" +
		"DROP TABLE dbo.Old\r\n"
	f := Lex([]byte(src), TSQL)
	want := []string{"SET", "ALTER", "UPDATE", "CREATE", "ALTER", "DROP"}
	if got := keywords(f); !reflect.DeepEqual(got, want) {
		t.Fatalf("keywords = %v, want %v", got, want)
	}
	batches := []int{}
	for _, s := range f.Statements {
		batches = append(batches, s.Batch)
	}
	if !reflect.DeepEqual(batches, []int{0, 0, 0, 1, 2, 2}) {
		t.Fatalf("batches = %v", batches)
	}
	if !reflect.DeepEqual(f.GoLines, []int{7, 13}) {
		t.Fatalf("go lines = %v", f.GoLines)
	}
	n, _ := f.Statements[1].Name(2, TSQL)
	if n.String() != "dbo.orders" {
		t.Fatalf("name = %q", n)
	}
}

func TestDollarQuoteAndPrefixes(t *testing.T) {
	f := Lex([]byte("select $a$ x $b$ y $a$, $1, U&'d\\0061', B'101', 1.5e3;"), Postgres)
	s := f.Statements[0]
	kinds := []Kind{}
	for _, tk := range s.Tokens {
		kinds = append(kinds, tk.Kind)
	}
	want := []Kind{Word, String, Punct, Number, Punct, String, Punct, String, Punct, Number}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds = %v (%+v)", kinds, s.Tokens)
	}
	if StringValue(s.Tokens[1]) != " x $b$ y " {
		t.Fatalf("dollar body = %q", StringValue(s.Tokens[1]))
	}
}

func TestDirectives(t *testing.T) {
	f := Lex([]byte("-- tq:contract expand-0042\n-- tq:allow PG001, PG009: hot path is tiny\nCREATE INDEX i ON t(a);\n-- tq:allow PG008\nDROP TABLE x;"), Postgres)
	ds := f.Statements[0].Directives()
	if len(ds) != 2 || ds[0].Name != "contract" || ds[0].Args[0] != "expand-0042" {
		t.Fatalf("directives = %+v", ds)
	}
	if ok, reason, line := Allows(ds, "pg009"); !ok || !reason || line != 2 {
		t.Fatalf("allow pg009 = %v %v %d", ok, reason, line)
	}
	if ok, reason, _ := Allows(f.Statements[1].Directives(), "PG008"); !ok || reason {
		t.Fatalf("allow without reason = %v %v", ok, reason)
	}
	if len(f.HeaderDirectives()) != 2 {
		t.Fatal("header directives")
	}
}

func TestCatalogReplay(t *testing.T) {
	c := NewCatalog(Postgres)
	c.ApplyFile("m/1.sql", Lex([]byte(`
CREATE TABLE public.orders (id bigint PRIMARY KEY, note character varying(20), total numeric(10,2) NOT NULL);
CREATE TABLE events (id int) PARTITION BY RANGE (id);
ALTER TABLE orders ADD COLUMN status text, ADD CONSTRAINT status_nn CHECK (status IS NOT NULL) NOT VALID;
ALTER TABLE orders RENAME COLUMN note TO memo;
`), Postgres))
	o := c.Table(Name{"public", "orders"})
	if o == nil || o.Columns["memo"] == nil || o.Columns["memo"].Type != "varchar(20)" || !o.Columns["total"].NotNull {
		t.Fatalf("orders = %+v", o)
	}
	if ck := o.Checks["status_nn"]; ck == nil || ck.NotNullOf != "status" || ck.Validated {
		t.Fatalf("check = %+v", ck)
	}
	c.ApplyFile("m/2.sql", Lex([]byte("ALTER TABLE orders VALIDATE CONSTRAINT status_nn; ALTER TABLE orders RENAME TO purchases;"), Postgres))
	if c.Table(Name{"public", "orders"}) != nil || c.Table(Name{"public", "purchases"}).Checks["status_nn"].Validated != true {
		t.Fatal("validate / rename")
	}
	if !c.Table(Name{"public", "events"}).Partitioned {
		t.Fatal("partitioned")
	}
	ts := NewCatalog(TSQL)
	ts.ApplyFile("x.sql", Lex([]byte("CREATE TABLE [dbo].[Customers] ([Id] int NOT NULL, [Name] nvarchar(50) NULL)\nGO\nEXEC sp_rename 'dbo.Customers', 'Clients'\n"), TSQL))
	if cl := ts.Table(Name{"dbo", "clients"}); cl == nil || cl.Columns["name"].Type != "nvarchar(50)" {
		t.Fatalf("tsql catalog = %+v", ts.Tables())
	}
}

func TestSortMigrations(t *testing.T) {
	p := []string{"db/V10__c.sql", "db/V2__b.sql", "db/V1__a.sql", "a/20240102_x.sql", "a/20240101_y.sql"}
	SortMigrations(p)
	want := []string{"a/20240101_y.sql", "a/20240102_x.sql", "db/V1__a.sql", "db/V2__b.sql", "db/V10__c.sql"}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("sorted = %v", p)
	}
}

func TestTypes(t *testing.T) {
	cases := map[string]string{"character varying(20)": "varchar(20)", "INTEGER": "int", "timestamp with time zone": "timestamptz"}
	for in, want := range cases {
		if got := NormalizeType(in); got != want {
			t.Errorf("NormalizeType(%q) = %q, want %q", in, got, want)
		}
	}
	if b, a := SplitType("numeric(10,2)"); b != "numeric" || len(a) != 2 {
		t.Fatalf("split = %q %v", b, a)
	}
}

func TestUnwrapIf(t *testing.T) {
	f := Lex([]byte("IF OBJECT_ID(N'dbo.T') IS NULL CREATE TABLE dbo.T (Id int)"), TSQL)
	u := f.Statements[0].Unwrap()
	if u.Keyword() != "CREATE" || !u.Match(1, "TABLE") {
		t.Fatalf("unwrap = %v", u.Words(0, 3))
	}
	plain := Lex([]byte("ALTER TABLE x ADD y int"), TSQL).Statements[0]
	if plain.Unwrap().Keyword() != "ALTER" {
		t.Fatal("non-IF statements are unchanged")
	}
	c := NewCatalog(TSQL)
	c.ApplyFile("a.sql", f)
	if c.Table(Name{"dbo", "t"}) == nil {
		t.Fatal("the catalog replays guarded DDL")
	}
}

func TestTSQLInlineBlocks(t *testing.T) {
	src := `IF COL_LENGTH('dbo.T', 'A') IS NULL BEGIN ALTER TABLE dbo.T ADD A int NOT NULL END
UPDATE dbo.T SET A = CASE WHEN B = 1 THEN 2 ELSE 3 END WHERE Id = 1
BEGIN TRAN ALTER TABLE dbo.T ADD C int NULL
COMMIT
BEGIN TRY DELETE FROM dbo.T WHERE Id = 2 END TRY BEGIN CATCH THROW; END CATCH
`
	f := Lex([]byte(src), TSQL)
	want := []string{"IF", "ALTER", "END", "UPDATE", "BEGIN", "COMMIT", "BEGIN", "DELETE", "END", "THROW", "END"}
	if got := keywords(f); !reflect.DeepEqual(got, want) {
		t.Fatalf("keywords = %v, want %v", got, want)
	}
	if got := f.Statements[1].Tokens[len(f.Statements[1].Tokens)-1].Text; got != "NULL" {
		t.Fatalf("the guarded ALTER ends at %q, want NULL (END split off)", got)
	}
	if !f.Statements[3].Has("WHERE") {
		t.Fatalf("CASE … END must not split the UPDATE: %+v", f.Statements[3].Tokens)
	}
}

func TestTSQLCTEStatements(t *testing.T) {
	src := `SET LOCK_TIMEOUT 5000
WITH c AS (SELECT * FROM dbo.T)
UPDATE c
SET A = 0
CREATE INDEX ix ON dbo.T (A)
WITH (ONLINE = ON)
`
	f := Lex([]byte(src), TSQL)
	if got, want := keywords(f), []string{"SET", "WITH", "CREATE"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("keywords = %v, want %v", got, want)
	}
}

func TestAfterCTE(t *testing.T) {
	cases := map[string]string{
		"WITH x AS (SELECT 1) UPDATE t SET a = 1":                                            "UPDATE",
		"WITH RECURSIVE x(id) AS (SELECT 1), y AS NOT MATERIALIZED (SELECT 2) DELETE FROM t": "DELETE",
		`WITH "q" AS MATERIALIZED (SELECT 1) INSERT INTO t SELECT * FROM "q"`:                "INSERT",
		"SELECT 1":                    "",
		"WITH x AS (SELECT 1":         "",
		"WITH x AS (SELECT 1)":        "",
		"WITH (ONLINE = ON) UPDATE t": "",
	}
	for src, want := range cases {
		st := Lex([]byte(src), Postgres).Statements[0]
		k := AfterCTE(st)
		got := ""
		if k > 0 {
			got = st.From(k).Keyword()
		}
		if got != want {
			t.Errorf("AfterCTE(%q) -> %q, want %q", src, got, want)
		}
	}
}
