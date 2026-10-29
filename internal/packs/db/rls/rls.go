// Package rls is the db pack's row-level security gate (spec D §11). It
// replays the Postgres migrations of one packs.db.migrations entry in apply
// order, builds the state of the tables, policies, views and functions in
// the exposed schemas, and judges what the changed migrations create or
// touch (every migration with --all) against the RLS001…RLS011 rules.
//
// There is no grammar: the replay matches token patterns per statement over
// the shared lexer (internal/packs/db/sqllex). Dynamic SQL (EXECUTE inside a
// DO block or a routine body) is not replayed; a judged DO block or routine
// body that mentions RLS-relevant DDL is reported instead (RLS011), so it
// never passes silently.
//
// Markers, in the comments before a statement (and, for the deny-all marker,
// a comment on the statement's own last line):
//
//	-- tq:allow RLS007: <reason>          suppresses that rule at that statement
//	-- tq:rls-deny-all <reason>           RLS enabled with no policy on purpose (RLS002)
//	-- tq:rls-anon-exec <reason>          a SECURITY DEFINER function anon may call (RLS008)
//
// A tq:allow without a reason is itself an error (SUP001), and the finding
// it names stays.
package rls

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/db/sqllex"
)

// Rule ids.
const (
	RLS001 = "RLS001"
	RLS002 = "RLS002"
	RLS003 = "RLS003"
	RLS004 = "RLS004"
	RLS005 = "RLS005"
	RLS006 = "RLS006"
	RLS007 = "RLS007"
	RLS008 = "RLS008"
	RLS009 = "RLS009"
	RLS010 = "RLS010"
	RLS011 = "RLS011"
	// SUP001 is a tq:allow with no reason (the same id as the migration lint).
	SUP001 = "SUP001"
)

// Rules lists the rule ids and their one-line descriptions.
func Rules() map[string]string {
	return map[string]string{
		RLS001: "a new table in an exposed schema without ENABLE ROW LEVEL SECURITY in the same migration",
		RLS002: "RLS enabled with no policy (mark service-only tables -- tq:rls-deny-all <reason>)",
		RLS003: "DISABLE / NO FORCE ROW LEVEL SECURITY or DROP POLICY with no replacement in the same migration",
		RLS004: "a policy for anon or authenticated with USING (true) or WITH CHECK (true)",
		RLS005: "a policy that reads user_metadata / raw_user_meta_data, which users can edit",
		RLS006: "GRANT ALL or a write grant to anon, GRANT … TO public in an exposed schema, default privileges to anon, or a role granted to anon or public",
		RLS007: "a view in an exposed schema without security_invoker = true, or a materialized view or foreign table there (all bypass RLS)",
		RLS008: "a SECURITY DEFINER function in an exposed schema without SET search_path, or one anon can execute",
		RLS009: "auth.uid() called per row instead of (select auth.uid())",
		RLS010: "a service_role reference in a policy (service_role bypasses RLS)",
		RLS011: "a DO block or routine body with dynamic RLS-relevant DDL (policies, row level security, grants, security definer, set schema, CREATE TABLE or VIEW, IMPORT FOREIGN SCHEMA) the gate cannot judge",
	}
}

// Unit is one migration file of the replay.
type Unit struct {
	Path string // slash path, repo relative
	Src  []byte
	// Judge is true for the files this run judges (the changed ones, or all
	// with --all); the others are only replayed.
	Judge bool
	// Migrator is the entry's migrator; dbmate files are cut at
	// "-- migrate:down".
	Migrator string
}

// Options configures a replay.
type Options struct {
	// Exposed are the schemas the API exposes.
	Exposed []string
	// Supabase: functions are also executable by anon through Supabase's
	// default privileges, so a SECURITY DEFINER function needs EXECUTE
	// revoked from both public and anon.
	Supabase bool
}

// Result is the outcome of a replay.
type Result struct {
	Findings []packs.Finding
	// Suppressed lists the rule of each finding a tq:allow suppressed.
	Suppressed []string
	// Tables are the tables of the exposed schemas after the replay.
	Tables []*Table
	Files  int
}

// loc is a statement location with its markers.
type loc struct {
	File string
	Line int
	dirs []sqllex.Directive
	// same are the markers on the statement's own last line.
	same []sqllex.Directive
}

// Table is a replayed table.
type Table struct {
	Name sqllex.Name
	// Created is false for a table that the replay only alters (it exists
	// outside the migrations, for example storage.objects).
	Created bool
	create  loc
	// RLS and Forced are the final row-level security switches.
	RLS, Forced bool
	enable      loc
	enabledIn   map[string]bool
	denyAll     *sqllex.Directive
	// Policies are the table's policies by name.
	Policies map[string]*Policy
	touch    map[string]loc // judged file -> first statement on the table
}

// Location is where the table was created ("" for a table never created).
func (t *Table) Location() (string, int) { return t.create.File, t.create.Line }

// Policy is one replayed policy.
type Policy struct {
	Name, Cmd  string
	Roles      []string
	Permissive bool
}

type view struct {
	name    sqllex.Name
	at      loc
	invoker bool
	judged  bool
}

type function struct {
	name                       sqllex.Name
	at                         loc
	definer, searchPath        bool
	revokedPublic, revokedAnon bool
	grantedAnon, grantedPublic bool
	judged                     bool
	reason                     bool
}

type state struct {
	o        Options
	exposed  map[string]bool
	tables   map[string]*Table
	views    map[string]*view
	matviews map[string]bool // every materialized view the replay knows, by name
	foreign  map[string]bool // every foreign table the replay knows, by name
	funcs    map[string]*function
	// pathSet and searchPath are the file's top-level SET search_path:
	// unqualified names resolve against it (public when it is not set).
	pathSet    bool
	searchPath []string
	// localSet and localPath are a SET LOCAL search_path (or set_config(…,
	// true)): they win over the session path until the transaction ends.
	localSet  bool
	localPath []string
	// schemas are the schemas a CREATE SCHEMA in the replay made.
	schemas    map[string]bool
	out        []packs.Finding
	supp       []string
	file       *sqllex.File
	path       string
	judge      bool
	dropped    map[string][]droppedPolicy // per file: dropped policies awaiting a replacement
	disabled   map[string][]pending
	unforced   map[string][]pending
	judgedFile map[string]bool
}

type droppedPolicy struct {
	table, name string
	at          loc
}

type pending struct {
	table string
	at    loc
}

// Run replays the units in order and judges those marked Judge.
func Run(units []Unit, o Options) Result {
	s := &state{o: o, exposed: map[string]bool{}, tables: map[string]*Table{}, views: map[string]*view{}, matviews: map[string]bool{}, foreign: map[string]bool{}, funcs: map[string]*function{}, schemas: map[string]bool{}, judgedFile: map[string]bool{}}
	for _, e := range o.Exposed {
		s.exposed[e] = true
	}
	files := 0
	for _, u := range units {
		f := upSection(sqllex.Lex(u.Src, sqllex.Postgres), u.Migrator)
		s.file, s.path, s.judge = f, u.Path, u.Judge
		s.dropped, s.disabled, s.unforced = map[string][]droppedPolicy{}, map[string][]pending{}, map[string][]pending{}
		s.pathSet, s.searchPath, s.localSet, s.localPath = false, nil, false, nil
		if u.Judge {
			files++
			s.judgedFile[u.Path] = true
		}
		for _, st := range f.Statements {
			s.statement(st)
		}
		s.endFile()
	}
	s.judgeState()
	res := Result{Findings: s.out, Suppressed: s.supp, Files: files}
	keys := make([]string, 0, len(s.tables))
	for k := range s.tables {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if t := s.tables[k]; s.exposed[t.Name.Schema] {
			res.Tables = append(res.Tables, t)
		}
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

// upSection drops a dbmate file's "-- migrate:down" section.
func upSection(f *sqllex.File, migrator string) *sqllex.File {
	if migrator != "dbmate" {
		return f
	}
	down := -1
	for _, c := range f.Comments {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.Text)), "migrate:down") {
			down = c.Pos
			break
		}
	}
	if down < 0 {
		return f
	}
	out := *f
	out.Statements = nil
	for _, st := range f.Statements {
		if len(st.Tokens) > 0 && st.Tokens[0].Pos > down {
			break
		}
		out.Statements = append(out.Statements, st)
	}
	return &out
}

func (s *state) at(st sqllex.Statement) loc {
	l := loc{File: s.path, Line: st.Line, dirs: st.Directives()}
	if n := len(st.Tokens); n > 0 {
		last := st.Tokens[n-1]
		for _, c := range s.file.Comments {
			if c.Line == st.EndLine && c.Pos > last.Pos {
				cc := c
				if d, ok := sqllex.ParseDirective(cc); ok {
					l.same = append(l.same, d)
				}
			}
		}
	}
	return l
}

// report adds a finding at l unless a tq:allow with a reason suppresses it.
func (s *state) report(l loc, rule, level, msg string) {
	allowed, reason, line := sqllex.Allows(l.dirs, rule)
	if !allowed {
		allowed, reason, line = sqllex.Allows(l.same, rule)
	}
	if allowed && reason {
		s.supp = append(s.supp, rule)
		return
	}
	if allowed {
		s.out = append(s.out, packs.Finding{File: l.File, Line: line, Level: packs.LevelError, Rule: SUP001,
			Message: fmt.Sprintf("tq:allow %s needs a reason: -- tq:allow %s: <why this is safe>", rule, rule)})
	}
	s.out = append(s.out, packs.Finding{File: l.File, Line: l.Line, Level: level, Rule: rule, Message: msg})
}

func (s *state) table(n sqllex.Name) *Table {
	k := n.String()
	t := s.tables[k]
	if t == nil {
		t = &Table{Name: n, Policies: map[string]*Policy{}, enabledIn: map[string]bool{}, touch: map[string]loc{}}
		s.tables[k] = t
	}
	return t
}

func (s *state) touch(t *Table, st sqllex.Statement) {
	if !s.judge {
		return
	}
	if _, ok := t.touch[s.path]; !ok {
		t.touch[s.path] = s.at(st)
	}
}

func (s *state) statement(st sqllex.Statement) {
	if len(st.Tokens) == 0 {
		return
	}
	s.dynamicSQL(st)
	switch st.Keyword() {
	case "CREATE":
		i := st.Skip(1, []string{"OR", "REPLACE"})
		i = st.Skip(i, []string{"GLOBAL|LOCAL"}, []string{"TEMP|TEMPORARY"}, []string{"UNLOGGED"}, []string{"RECURSIVE"})
		temp := i > 1 && st.Find(1, "TEMP|TEMPORARY") >= 0 && st.Find(1, "TEMP|TEMPORARY") < i
		switch {
		case st.Match(i, "TABLE") && !temp:
			s.createTable(st, i+1)
		case st.Match(i, "VIEW") && !temp:
			s.createView(st, i+1)
		case st.Match(i, "MATERIALIZED", "VIEW"):
			s.createMatView(st, i+2)
		case st.Match(i, "FOREIGN", "TABLE"):
			s.createForeign(st, i+2)
		case st.Match(i, "POLICY"):
			s.createPolicy(st, i+1)
		case st.Match(i, "FUNCTION|PROCEDURE"):
			s.createFunction(st, i+1)
		case st.Match(i, "SCHEMA"):
			s.createSchema(st, i+1)
		}
	case "SELECT", "WITH":
		s.selectInto(st)
	case "SET", "RESET":
		s.setSearchPath(st)
	case "COMMIT", "END", "ROLLBACK", "ABORT":
		// The transaction ends, and a SET LOCAL with it. ROLLBACK TO
		// SAVEPOINT and the PREPARED forms leave the current one open.
		if !st.Match(1, "TO|PREPARED") {
			s.localSet, s.localPath = false, nil
		}
	case "IMPORT":
		s.importForeign(st)
	case "ALTER":
		switch {
		case st.Match(1, "TABLE"):
			s.alterTable(st, 2)
		case st.Match(1, "VIEW"):
			s.alterView(st, 2)
		case st.Match(1, "MATERIALIZED", "VIEW"):
			s.alterMatView(st, 3)
		case st.Match(1, "FOREIGN", "TABLE"):
			s.alterForeign(st, 3)
		case st.Match(1, "POLICY"):
			s.alterPolicy(st, 2)
		case st.Match(1, "FUNCTION|PROCEDURE|ROUTINE"):
			s.alterFunction(st, 2)
		case st.Match(1, "DEFAULT", "PRIVILEGES"):
			s.defaultPrivileges(st)
		case st.Match(1, "SCHEMA") && st.Match(3, "RENAME", "TO"):
			if old := st.At(2).Ident(sqllex.Postgres); s.schemas[old] {
				delete(s.schemas, old)
				s.schemas[st.At(5).Ident(sqllex.Postgres)] = true
			}
		}
	case "DROP":
		switch {
		case st.Match(1, "TABLE"):
			for _, n := range s.names(st, st.Skip(2, []string{"IF", "EXISTS"})) {
				delete(s.tables, n.String())
			}
		case st.Match(1, "VIEW"):
			for _, n := range s.names(st, st.Skip(2, []string{"IF", "EXISTS"})) {
				delete(s.views, n.String())
			}
		case st.Match(1, "MATERIALIZED", "VIEW"):
			for _, n := range s.names(st, st.Skip(3, []string{"IF", "EXISTS"})) {
				delete(s.matviews, n.String())
			}
		case st.Match(1, "FOREIGN", "TABLE"):
			for _, n := range s.names(st, st.Skip(3, []string{"IF", "EXISTS"})) {
				delete(s.foreign, n.String())
			}
		case st.Match(1, "FUNCTION|PROCEDURE|ROUTINE"):
			for _, n := range s.names(st, st.Skip(2, []string{"IF", "EXISTS"})) {
				delete(s.funcs, n.String())
			}
		case st.Match(1, "POLICY"):
			s.dropPolicy(st, st.Skip(2, []string{"IF", "EXISTS"}))
		case st.Match(1, "SCHEMA"):
			for k := st.Skip(2, []string{"IF", "EXISTS"}); k < len(st.Tokens); k++ {
				if v := st.At(k).Ident(sqllex.Postgres); v != "" && !st.Match(k, "CASCADE|RESTRICT") {
					delete(s.schemas, v)
				}
			}
		}
	case "GRANT":
		s.grant(st, false)
	case "REVOKE":
		s.revoke(st)
	}
}

// name reads the name of an existing object at i. An unqualified name
// resolves against the file's search_path the way Postgres looks it up: the
// first schema on the path that holds a known object of that name. When none
// does (the object was made outside the replayed migrations: a dashboard
// table, a squashed baseline), the name fails closed to the first exposed
// schema on the path, else to where a CREATE would put it.
func (s *state) name(st sqllex.Statement, i int) (sqllex.Name, int) {
	n, j := st.Name(i, sqllex.Postgres)
	set, path := s.searchPathNow()
	if n.IsZero() || j != i+1 || !set {
		return n, j
	}
	for _, sc := range path {
		k := sc + "." + n.Object
		if s.tables[k] != nil || s.views[k] != nil || s.matviews[k] || s.foreign[k] || s.funcs[k] != nil {
			return sqllex.Name{Schema: sc, Object: n.Object}, j
		}
	}
	for _, sc := range path {
		if s.exposed[sc] {
			return sqllex.Name{Schema: sc, Object: n.Object}, j
		}
	}
	return s.newName(st, i)
}

// newName reads the name of an object being created at i: an unqualified
// one is created in the first schema of the search_path that exists (public
// when no path is set), as in Postgres. A schema the replay never saw
// created, that is not a system schema and holds no known object, is taken
// not to exist, so the CREATE falls through to the next one; when no path
// schema is known the first is used. With an empty path Postgres refuses
// the CREATE; the name then lands in pg_catalog, which is never exposed.
func (s *state) newName(st sqllex.Statement, i int) (sqllex.Name, int) {
	n, j := st.Name(i, sqllex.Postgres)
	set, path := s.searchPathNow()
	if n.IsZero() || j != i+1 || !set {
		return n, j
	}
	if len(path) == 0 {
		return sqllex.Name{Schema: "pg_catalog", Object: n.Object}, j
	}
	for _, sc := range path {
		if s.exposed[sc] || s.schemaKnown(sc) {
			return sqllex.Name{Schema: sc, Object: n.Object}, j
		}
	}
	return sqllex.Name{Schema: path[0], Object: n.Object}, j
}

// searchPathNow is the search_path in force: a transaction-local one, else the
// session one.
func (s *state) searchPathNow() (bool, []string) {
	if s.localSet {
		return true, s.localPath
	}
	return s.pathSet, s.searchPath
}

// systemSchemas exist in a Postgres (or Supabase) database without a CREATE
// SCHEMA in the migrations.
var systemSchemas = map[string]bool{
	"public": true, "pg_catalog": true, "information_schema": true, "pg_toast": true, "pg_temp": true,
	"auth": true, "storage": true, "extensions": true, "graphql": true, "graphql_public": true,
	"realtime": true, "vault": true, "pgsodium": true, "net": true, "supabase_functions": true, "cron": true,
}

// schemaKnown reports whether the replay knows that schema sc exists: a
// CREATE SCHEMA made it, it is a system schema, or it holds a known object.
func (s *state) schemaKnown(sc string) bool {
	if s.schemas[sc] || systemSchemas[sc] {
		return true
	}
	p := sc + "."
	var keys []string
	for k := range s.tables {
		keys = append(keys, k)
	}
	for k := range s.views {
		keys = append(keys, k)
	}
	for k := range s.funcs {
		keys = append(keys, k)
	}
	for k := range s.matviews {
		keys = append(keys, k)
	}
	for k := range s.foreign {
		keys = append(keys, k)
	}
	for _, k := range keys {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

// createSchema records CREATE SCHEMA [IF NOT EXISTS] name, and CREATE
// SCHEMA AUTHORIZATION role (the schema is named after the role).
//
// The schema elements that may follow (CREATE TABLE, VIEW, INDEX,
// SEQUENCE, TRIGGER, GRANT) are replayed as statements of their own, with
// unqualified names created in the new schema, as Postgres does.
func (s *state) createSchema(st sqllex.Statement, i int) {
	i = st.Skip(i, []string{"IF", "NOT", "EXISTS"})
	if st.Match(i, "AUTHORIZATION") {
		i++
	}
	v := st.At(i).Ident(sqllex.Postgres)
	if v == "" {
		return
	}
	s.schemas[v] = true
	i++
	if st.Match(i, "AUTHORIZATION") {
		i += 2
	}
	var starts []int
	for k := i; k < len(st.Tokens); k++ {
		if k = st.FindTop(k, "CREATE|GRANT"); k < 0 {
			break
		}
		starts = append(starts, k)
	}
	if len(starts) == 0 {
		return
	}
	saveSet, savePath := s.localSet, s.localPath
	s.localSet, s.localPath = true, []string{v}
	for n, a := range starts {
		b := len(st.Tokens)
		if n+1 < len(starts) {
			b = starts[n+1]
		}
		sub := st
		sub.Tokens = st.Tokens[a:b]
		sub.Line, sub.EndLine = st.Tokens[a].Line, st.Tokens[b-1].Line
		s.statement(sub)
	}
	s.localSet, s.localPath = saveSet, savePath
}

// setSearchPath follows a top-level SET [SESSION|LOCAL] search_path {TO|=}
// …, SET search_path TO DEFAULT, RESET search_path and RESET ALL.
func (s *state) setSearchPath(st sqllex.Statement) {
	if st.Match(0, "RESET") {
		if st.Match(1, "SEARCH_PATH|ALL") {
			s.pathSet, s.searchPath, s.localSet, s.localPath = false, nil, false, nil
		}
		return
	}
	local := st.Match(1, "LOCAL")
	i := st.Skip(1, []string{"SESSION|LOCAL"})
	if !st.Match(i, "SEARCH_PATH") || !(st.Match(i+1, "TO") || st.Match(i+1, "=")) {
		return
	}
	if st.Match(i+2, "DEFAULT") {
		if local {
			// SET LOCAL … TO DEFAULT: the default path until the
			// transaction ends.
			s.localSet, s.localPath = true, []string{"public"}
			return
		}
		s.pathSet, s.searchPath = false, nil
		return
	}
	var path []string
	for k := i + 2; k < len(st.Tokens); k++ {
		t := st.At(k)
		switch {
		case t.IsPunct(","):
		case t.Kind == sqllex.String:
			path = append(path, pathList(sqllex.StringValue(t))...)
		default:
			if v := t.Ident(sqllex.Postgres); v != "" {
				path = append(path, v)
			}
		}
	}
	s.setPath(path, local)
}

// setPath sets the file's search_path, without "$user" (a schema named after
// the role, which migrations do not create) and empty entries. A local one
// lasts until the transaction ends.
func (s *state) setPath(path []string, local bool) {
	var out []string
	for _, p := range path {
		if p != "" && p != "$user" {
			out = append(out, p)
		}
	}
	if local {
		s.localSet, s.localPath = true, out
		return
	}
	s.pathSet, s.searchPath = true, out
}

// localArg reports whether set_config's is_local argument is true: the
// keyword TRUE or a boolean string such as 't' or 'on'.
func localArg(t sqllex.Token) bool {
	if t.Kind == sqllex.String {
		switch strings.ToLower(strings.TrimSpace(sqllex.StringValue(t))) {
		case "t", "true", "y", "yes", "on", "1":
			return true
		}
		return false
	}
	return strings.EqualFold(t.Ident(sqllex.Postgres), "true")
}

// pathList splits a search_path string value ("api, public", "\"$user\"").
func pathList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if len(p) >= 2 && p[0] == '"' && p[len(p)-1] == '"' {
			p = strings.ReplaceAll(p[1:len(p)-1], `""`, `"`)
		} else {
			p = strings.ToLower(p)
		}
		out = append(out, p)
	}
	return out
}

// selectInto handles a top-level SELECT … INTO [TEMP|TEMPORARY|UNLOGGED]
// [TABLE] name (after an optional CTE list), which creates and fills a
// table the same as CREATE TABLE … AS, and a SELECT set_config('search_path',
// '…', …) (pg_dump writes one at the top of a dump).
func (s *state) selectInto(st sqllex.Statement) {
	k := sqllex.AfterCTE(st)
	if !st.Match(k, "SELECT") {
		return
	}
	for i := k; i+4 < len(st.Tokens); i++ {
		if st.At(i).Ident(sqllex.Postgres) == "set_config" && st.At(i+1).IsPunct("(") && st.At(i+2).Kind == sqllex.String &&
			strings.EqualFold(sqllex.StringValue(st.At(i+2)), "search_path") && st.At(i+3).IsPunct(",") && st.At(i+4).Kind == sqllex.String {
			s.setPath(pathList(sqllex.StringValue(st.At(i+4))), st.At(i+5).IsPunct(",") && localArg(st.At(i+6)))
		}
	}
	in := st.FindTop(k, "INTO")
	if in < 0 {
		return
	}
	i := in + 1
	if st.Match(i, "TEMP|TEMPORARY") || st.Match(i, "LOCAL|GLOBAL", "TEMP|TEMPORARY") {
		return
	}
	s.createTable(st, st.Skip(i, []string{"UNLOGGED"}, []string{"TABLE"}))
}

// names reads a comma-separated list of (possibly argument-carrying) names.
func (s *state) names(st sqllex.Statement, i int) []sqllex.Name {
	var out []sqllex.Name
	for {
		n, j := s.name(st, i)
		if n.IsZero() {
			return out
		}
		out = append(out, n)
		if st.At(j).IsPunct("(") {
			if j = st.Close(j); j < 0 {
				return out
			}
			j++
		}
		if !st.At(j).IsPunct(",") {
			return out
		}
		i = j + 1
	}
}

func (s *state) createTable(st sqllex.Statement, i int) {
	ifNot := st.Match(i, "IF", "NOT", "EXISTS")
	i = st.Skip(i, []string{"IF", "NOT", "EXISTS"})
	n, _ := s.newName(st, i)
	if n.IsZero() {
		return
	}
	if old := s.tables[n.String()]; old != nil && ifNot {
		s.touch(old, st)
		return
	}
	delete(s.tables, n.String())
	t := s.table(n)
	t.Created = true
	t.create = s.at(st)
	s.touch(t, st)
}

func (s *state) alterTable(st sqllex.Statement, i int) {
	i = st.Skip(i, []string{"IF", "EXISTS"}, []string{"ONLY"})
	n, j := s.name(st, i)
	if n.IsZero() {
		return
	}
	// ALTER TABLE also works on views and materialized views; a known one
	// (and no table of that name) is handled as what it is.
	if s.tables[n.String()] == nil {
		switch {
		case s.matviews[n.String()]:
			s.alterMatView(st, i)
			return
		case s.foreign[n.String()]:
			s.alterForeign(st, i)
			return
		case s.views[n.String()] != nil:
			s.alterView(st, i)
			return
		}
	}
	t := s.table(n)
	s.touch(t, st)
	for _, r := range st.Split(j, -1) {
		a := r[0]
		switch {
		case st.Match(a, "ENABLE", "ROW", "LEVEL", "SECURITY"):
			t.RLS = true
			t.enabledIn[s.path] = true
			t.enable = s.at(st)
			t.denyAll = denyAll(t.enable)
			s.resolve(s.disabled, t)
		case st.Match(a, "DISABLE", "ROW", "LEVEL", "SECURITY"):
			t.RLS = false
			if s.judge && s.exposed[t.Name.Schema] {
				s.disabled[s.path] = append(s.disabled[s.path], pending{table: n.String(), at: s.at(st)})
			}
		case st.Match(a, "FORCE", "ROW", "LEVEL", "SECURITY"):
			t.Forced = true
			s.resolve(s.unforced, t)
		case st.Match(a, "NO", "FORCE", "ROW", "LEVEL", "SECURITY"):
			t.Forced = false
			if s.judge && s.exposed[t.Name.Schema] {
				s.unforced[s.path] = append(s.unforced[s.path], pending{table: n.String(), at: s.at(st)})
			}
		case st.Match(a, "RENAME", "TO"):
			to := st.At(a + 2).Ident(sqllex.Postgres)
			if to != "" {
				s.moveTable(st, t, sqllex.Name{Schema: t.Name.Schema, Object: to})
			}
		case st.Match(a, "SET", "SCHEMA"):
			to := st.At(a + 2).Ident(sqllex.Postgres)
			if to != "" {
				s.moveTable(st, t, sqllex.Name{Schema: to, Object: t.Name.Object})
			}
		}
	}
}

// moveTable renames or moves t. A judged migration that moves a table into
// an exposed schema from one that is not exposes it there: that statement
// counts as the table's creation for RLS001 (the table must arrive with RLS
// on, or have it enabled in the same migration).
func (s *state) moveTable(st sqllex.Statement, t *Table, to sqllex.Name) {
	into := !s.exposed[t.Name.Schema] && s.exposed[to.Schema]
	delete(s.tables, t.Name.String())
	t.Name = to
	s.tables[to.String()] = t
	if into && s.judge && t.create.File != s.path {
		t.Created = true
		t.create = s.at(st)
		if t.RLS {
			t.enabledIn[s.path] = true
		}
	}
}

// resolve clears this file's pending DISABLE / NO FORCE on t (a later
// ENABLE / FORCE in the same migration replaces it).
func (s *state) resolve(m map[string][]pending, t *Table) {
	ps := m[s.path]
	out := ps[:0]
	for _, p := range ps {
		if p.table != t.Name.String() {
			out = append(out, p)
		}
	}
	m[s.path] = out
}

func denyAll(l loc) *sqllex.Directive {
	for _, ds := range [][]sqllex.Directive{l.same, l.dirs} {
		for i := range ds {
			if ds[i].Name == "rls-deny-all" {
				return &ds[i]
			}
		}
	}
	return nil
}

// policyClauses parses [AS PERMISSIVE|RESTRICTIVE] [FOR cmd] [TO roles]
// [USING (…)] [WITH CHECK (…)] from i.
type clauses struct {
	permissive      bool
	cmd             string
	roles           []string
	hasRoles        bool
	using, check    [2]int // token ranges inside the parentheses, [0,0] when absent
	hasUsing, hasCk bool
}

func parseClauses(st sqllex.Statement, i int) clauses {
	c := clauses{permissive: true}
	for i < len(st.Tokens) {
		switch {
		case st.Match(i, "AS", "RESTRICTIVE"):
			c.permissive = false
			i += 2
		case st.Match(i, "AS", "PERMISSIVE"):
			i += 2
		case st.Match(i, "FOR"):
			c.cmd = st.At(i + 1).Upper()
			i += 2
		case st.Match(i, "TO"):
			c.hasRoles = true
			i++
			for {
				r := st.At(i).Ident(sqllex.Postgres)
				if st.At(i).Is("GROUP") {
					i++
					continue
				}
				if r == "" {
					break
				}
				c.roles = append(c.roles, r)
				i++
				if !st.At(i).IsPunct(",") {
					break
				}
				i++
			}
		case st.Match(i, "USING", "("):
			e := st.Close(i + 1)
			if e < 0 {
				return c
			}
			c.using, c.hasUsing = [2]int{i + 2, e}, true
			i = e + 1
		case st.Match(i, "WITH", "CHECK", "("):
			e := st.Close(i + 2)
			if e < 0 {
				return c
			}
			c.check, c.hasCk = [2]int{i + 3, e}, true
			i = e + 1
		default:
			i++
		}
	}
	return c
}

func (s *state) createPolicy(st sqllex.Statement, i int) {
	name := st.At(i).Ident(sqllex.Postgres)
	if !st.Match(i+1, "ON") {
		return
	}
	n, j := s.name(st, i+2)
	if name == "" || n.IsZero() {
		return
	}
	c := parseClauses(st, j)
	if c.cmd == "" {
		c.cmd = "ALL"
	}
	if !c.hasRoles {
		c.roles = []string{"public"}
	}
	t := s.table(n)
	s.touch(t, st)
	t.Policies[name] = &Policy{Name: name, Cmd: c.cmd, Roles: c.roles, Permissive: c.permissive}
	// A replacement for a policy dropped earlier in this migration: the same
	// name, or the same command.
	ds := s.dropped[s.path]
	out := ds[:0]
	for _, d := range ds {
		if d.table == n.String() {
			continue
		}
		out = append(out, d)
	}
	s.dropped[s.path] = out
	if s.judge && s.exposed[n.Schema] {
		s.judgePolicy(st, n, t.Policies[name], c)
	}
}

func (s *state) alterPolicy(st sqllex.Statement, i int) {
	name := st.At(i).Ident(sqllex.Postgres)
	if !st.Match(i+1, "ON") {
		return
	}
	n, j := s.name(st, i+2)
	if name == "" || n.IsZero() {
		return
	}
	t := s.table(n)
	s.touch(t, st)
	p := t.Policies[name]
	if p == nil {
		p = &Policy{Name: name, Cmd: "ALL", Roles: []string{"public"}, Permissive: true}
		t.Policies[name] = p
	}
	if st.Match(j, "RENAME", "TO") {
		if to := st.At(j + 2).Ident(sqllex.Postgres); to != "" {
			delete(t.Policies, name)
			p.Name = to
			t.Policies[to] = p
		}
		return
	}
	c := parseClauses(st, j)
	if c.hasRoles {
		p.Roles = c.roles
	}
	c.cmd, c.permissive = p.Cmd, p.Permissive
	if !c.hasRoles {
		c.roles = p.Roles
	}
	if s.judge && s.exposed[n.Schema] {
		s.judgePolicy(st, n, p, c)
	}
}

func (s *state) dropPolicy(st sqllex.Statement, i int) {
	name := st.At(i).Ident(sqllex.Postgres)
	if !st.Match(i+1, "ON") {
		return
	}
	n, _ := s.name(st, i+2)
	if name == "" || n.IsZero() {
		return
	}
	t := s.table(n)
	s.touch(t, st)
	delete(t.Policies, name)
	if s.judge && s.exposed[n.Schema] {
		s.dropped[s.path] = append(s.dropped[s.path], droppedPolicy{table: n.String(), name: name, at: s.at(st)})
	}
}

// endFile reports this migration's unreplaced DISABLE / NO FORCE / DROP POLICY.
func (s *state) endFile() {
	for _, p := range s.disabled[s.path] {
		s.report(p.at, RLS003, packs.LevelError, fmt.Sprintf("DISABLE ROW LEVEL SECURITY on %s: every row becomes visible to any role with a grant; keep RLS on and change the policies instead", p.table))
	}
	for _, p := range s.unforced[s.path] {
		s.report(p.at, RLS003, packs.LevelError, fmt.Sprintf("NO FORCE ROW LEVEL SECURITY on %s: the table owner bypasses the policies again", p.table))
	}
	for _, d := range s.dropped[s.path] {
		s.report(d.at, RLS003, packs.LevelError, fmt.Sprintf("DROP POLICY %s on %s with no replacement policy in the same migration", d.name, d.table))
	}
}

func hasRole(roles []string, want ...string) bool {
	for _, r := range roles {
		for _, w := range want {
			if strings.EqualFold(r, w) {
				return true
			}
		}
	}
	return false
}

// isTrue reports an expression that is the constant true: TRUE (or a true
// boolean literal such as 't'), a ::bool cast of one, NOT of a false one, a
// literal compared to itself (1 = 1, 'a' = 'a'), an OR with a true operand
// and an AND of true operands. It is a fold of constants, not a prover.
func isTrue(st sqllex.Statement, r [2]int) bool { return constBool(st, r[0], r[1], true) }

// constBool reports whether tokens [a, b) are the boolean constant want.
func constBool(st sqllex.Statement, a, b int, want bool) bool {
	for a < b && st.At(a).IsPunct("(") && st.Close(a) == b-1 {
		a, b = a+1, b-1
	}
	if a >= b {
		return false
	}
	// OR binds loosest, then AND, then NOT; :: binds tightest.
	for _, op := range []string{"OR", "AND"} {
		parts := splitWord(st, a, b, op)
		if len(parts) < 2 {
			continue
		}
		// a OR b is true when one operand is, false when both are; AND the
		// other way round.
		one := (op == "OR") == want
		for _, p := range parts {
			v := constBool(st, p[0], p[1], want)
			if one && v {
				return true
			}
			if !one && !v {
				return false
			}
		}
		return !one
	}
	if st.At(a).Is("NOT") {
		return constBool(st, a+1, b, !want)
	}
	if b-a >= 3 && st.At(b-2).IsPunct("::") && st.At(b-1).Is("BOOL", "BOOLEAN") {
		return constBool(st, a, b-2, want)
	}
	if b-a == 1 {
		t := st.At(a)
		v := strings.ToLower(t.Text)
		if t.Kind == sqllex.String {
			v = strings.ToLower(sqllex.StringValue(t))
		} else if t.Kind != sqllex.Word {
			return false
		}
		if want {
			return v == "true" || (t.Kind == sqllex.String && (v == "t" || v == "on" || v == "yes" || v == "y" || v == "1"))
		}
		return v == "false" || (t.Kind == sqllex.String && (v == "f" || v == "off" || v == "no" || v == "n" || v == "0"))
	}
	if b-a == 3 && want && st.At(a+1).IsPunct("=") {
		l, r := st.At(a), st.At(a+2)
		literal := func(t sqllex.Token) bool {
			return t.Kind == sqllex.Number || t.Kind == sqllex.String || t.Is("TRUE", "FALSE")
		}
		return literal(l) && literal(r) && l.Kind == r.Kind && strings.EqualFold(l.Text, r.Text)
	}
	return false
}

// splitWord splits tokens [a, b) at the top-level word op. The AND of an
// x BETWEEN y AND z is not a conjunction and is skipped.
func splitWord(st sqllex.Statement, a, b int, op string) [][2]int {
	var out [][2]int
	depth, start, between := 0, a, false
	for i := a; i < b; i++ {
		t := st.At(i)
		switch {
		case t.IsPunct("("):
			depth++
		case t.IsPunct(")"):
			depth--
		case depth == 0 && t.Is("BETWEEN"):
			between = true
		case depth == 0 && t.Is("AND") && between:
			between = false
		case depth == 0 && t.Is(op):
			out = append(out, [2]int{start, i})
			start = i + 1
		}
	}
	return append(out, [2]int{start, b})
}

func (s *state) judgePolicy(st sqllex.Statement, n sqllex.Name, p *Policy, c clauses) {
	l := s.at(st)
	if c.permissive && hasRole(c.roles, "anon", "authenticated", "public") {
		trueUsing := c.hasUsing && isTrue(st, c.using)
		trueCheck := c.hasCk && isTrue(st, c.check)
		switch {
		case p.Cmd == "SELECT" && trueUsing:
			s.report(l, RLS004, packs.LevelWarn, fmt.Sprintf("policy %s on %s lets %s read every row (USING (true)); fine for public data, otherwise compare to (select auth.uid())", p.Name, n, strings.Join(c.roles, ", ")))
		case p.Cmd != "SELECT" && (trueUsing || trueCheck):
			s.report(l, RLS004, packs.LevelError, fmt.Sprintf("policy %s on %s lets %s %s any row (%s); restrict it to the row owner", p.Name, n, strings.Join(c.roles, ", "), strings.ToLower(p.Cmd), trueWhich(trueUsing, trueCheck)))
		}
	}
	var exprs [][2]int
	if c.hasUsing {
		exprs = append(exprs, c.using)
	}
	if c.hasCk {
		exprs = append(exprs, c.check)
	}
	meta, perRow, service := false, false, hasRole(c.roles, "service_role")
	for _, r := range exprs {
		for k := r[0]; k < r[1]; k++ {
			t := st.At(k)
			if (t.Kind == sqllex.String && strings.EqualFold(sqllex.StringValue(t), "user_metadata")) || t.Ident(sqllex.Postgres) == "raw_user_meta_data" {
				meta = true
			}
			if t.Kind == sqllex.String && strings.EqualFold(sqllex.StringValue(t), "service_role") || t.Ident(sqllex.Postgres) == "service_role" {
				service = true
			}
			if t.Ident(sqllex.Postgres) == "auth" && st.At(k+1).IsPunct(".") && st.At(k+2).Ident(sqllex.Postgres) == "uid" && st.At(k+3).IsPunct("(") {
				if !(st.At(k-1).Is("SELECT") && st.At(k-2).IsPunct("(")) {
					perRow = true
				}
			}
		}
	}
	if meta {
		s.report(l, RLS005, packs.LevelError, fmt.Sprintf("policy %s on %s reads user_metadata / raw_user_meta_data, which every user can edit; use app_metadata or a table the user cannot write", p.Name, n))
	}
	if perRow {
		s.report(l, RLS009, packs.LevelWarn, fmt.Sprintf("policy %s on %s calls auth.uid() per row; write (select auth.uid()) so it runs once per statement", p.Name, n))
	}
	if service {
		s.report(l, RLS010, packs.LevelWarn, fmt.Sprintf("policy %s on %s names service_role, which bypasses RLS anyway; the policy is misleading", p.Name, n))
	}
}

func trueWhich(u, c bool) string {
	switch {
	case u && c:
		return "USING (true) and WITH CHECK (true)"
	case u:
		return "USING (true)"
	}
	return "WITH CHECK (true)"
}

// viewOptions reads WITH (security_invoker [= v], …) at i; the bool reports
// whether the option list mentions security_invoker at all.
func viewOption(st sqllex.Statement, open int) (invoker, found bool) {
	e := st.Close(open)
	if e < 0 {
		return false, false
	}
	for _, r := range st.Split(open+1, e) {
		if st.At(r[0]).Ident(sqllex.Postgres) != "security_invoker" {
			continue
		}
		found = true
		if r[1]-r[0] == 1 {
			return true, true
		}
		if st.At(r[0] + 1).IsPunct("=") {
			v := strings.ToLower(st.At(r[0] + 2).Text)
			if st.At(r[0]+2).Kind == sqllex.String {
				v = strings.ToLower(sqllex.StringValue(st.At(r[0] + 2)))
			}
			switch v {
			case "true", "on", "1", "yes", "t", "y":
				return true, true
			}
			return false, true
		}
	}
	return false, found
}

func (s *state) createView(st sqllex.Statement, i int) {
	n, j := s.newName(st, i)
	if n.IsZero() {
		return
	}
	if st.At(j).IsPunct("(") {
		if j = st.Close(j); j < 0 {
			return
		}
		j++
	}
	v := &view{name: n, at: s.at(st), judged: s.judge}
	if st.Match(j, "WITH", "(") {
		v.invoker, _ = viewOption(st, j+1)
	}
	if old := s.views[n.String()]; old != nil && old.judged {
		v.judged = true
	}
	s.views[n.String()] = v
}

func (s *state) alterView(st sqllex.Statement, i int) {
	i = st.Skip(i, []string{"IF", "EXISTS"})
	n, j := s.name(st, i)
	if n.IsZero() {
		return
	}
	v := s.views[n.String()]
	if v == nil {
		// A view the replay never saw created (it exists outside the
		// migrations): its options are unknown, so a judged move into an
		// exposed schema or a change of security_invoker is judged as if it
		// ran with its owner's rights, unless the statement sets it on.
		if !s.judge || !(st.Match(j, "SET", "SCHEMA") || st.Match(j, "SET", "(") || st.Match(j, "RESET", "(")) {
			return
		}
		v = &view{name: n, at: s.at(st)}
		s.views[n.String()] = v
	}
	switch {
	case st.Match(j, "SET", "("):
		if inv, ok := viewOption(st, j+1); ok {
			v.invoker = inv
			v.judged = v.judged || s.judge
			if s.judge {
				v.at = s.at(st)
			}
		}
	case st.Match(j, "RESET", "("):
		if _, ok := viewOption(st, j+1); ok {
			v.invoker = false
			v.judged = v.judged || s.judge
			if s.judge {
				v.at = s.at(st)
			}
		}
	case st.Match(j, "RENAME", "TO"):
		if to := st.At(j + 2).Ident(sqllex.Postgres); to != "" {
			delete(s.views, n.String())
			v.name.Object = to
			s.views[v.name.String()] = v
		}
	case st.Match(j, "SET", "SCHEMA"):
		if to := st.At(j + 2).Ident(sqllex.Postgres); to != "" {
			delete(s.views, n.String())
			v.name.Schema = to
			s.views[v.name.String()] = v
			// Moving a view into an exposed schema exposes it: the move is
			// judged where it happens.
			if s.judge {
				v.judged = true
				v.at = s.at(st)
			}
		}
	}
}

// createMatView reports a materialized view in an exposed schema: it has no
// row-level security at all (and cannot be made security_invoker), so the
// API serves every row it holds.
func (s *state) createMatView(st sqllex.Statement, i int) {
	i = st.Skip(i, []string{"IF", "NOT", "EXISTS"})
	n, _ := s.newName(st, i)
	if n.IsZero() {
		return
	}
	s.matviews[n.String()] = true
	s.exposedMatView(st, n)
}

// exposedMatView reports materialized view n, now in an exposed schema, in a
// judged migration.
func (s *state) exposedMatView(st sqllex.Statement, n sqllex.Name) {
	if !s.judge || !s.exposed[n.Schema] {
		return
	}
	s.report(s.at(st), RLS007, packs.LevelError, fmt.Sprintf("materialized view %s is in an exposed schema and has no row-level security: the API serves every row; move it to a private schema and expose it through a security_invoker view or a function", n))
}

// createForeign follows CREATE FOREIGN TABLE [IF NOT EXISTS] name. A foreign
// table cannot have row-level security, so one in an exposed schema is
// reported like a materialized view (RLS007).
func (s *state) createForeign(st sqllex.Statement, i int) {
	i = st.Skip(i, []string{"IF", "NOT", "EXISTS"})
	n, _ := s.newName(st, i)
	if n.IsZero() {
		return
	}
	s.foreign[n.String()] = true
	if s.judge && s.exposed[n.Schema] {
		s.exposedForeign(st, n.String())
	}
}

// exposedForeign reports foreign table what, now in an exposed schema.
func (s *state) exposedForeign(st sqllex.Statement, what string) {
	s.report(s.at(st), RLS007, packs.LevelError, fmt.Sprintf("foreign table %s is in an exposed schema and cannot have row-level security: the API serves every row the remote server returns; keep it in a private schema and expose only what a caller may see through a function", what))
}

// alterForeign follows ALTER FOREIGN TABLE [IF EXISTS] [ONLY] renames and
// moves; a move into an exposed schema is judged even when the replay never
// saw the table created.
func (s *state) alterForeign(st sqllex.Statement, i int) {
	i = st.Skip(i, []string{"IF", "EXISTS"}, []string{"ONLY"})
	n, j := s.name(st, i)
	if n.IsZero() {
		return
	}
	switch {
	case st.Match(j, "RENAME", "TO"):
		if to := st.At(j + 2).Ident(sqllex.Postgres); to != "" {
			delete(s.foreign, n.String())
			s.foreign[sqllex.Name{Schema: n.Schema, Object: to}.String()] = true
		}
	case st.Match(j, "SET", "SCHEMA"):
		if to := st.At(j + 2).Ident(sqllex.Postgres); to != "" {
			delete(s.foreign, n.String())
			m := sqllex.Name{Schema: to, Object: n.Object}
			s.foreign[m.String()] = true
			if s.judge && s.exposed[to] {
				s.exposedForeign(st, m.String())
			}
		}
	}
}

// importForeign judges IMPORT FOREIGN SCHEMA remote … INTO local: it creates
// foreign tables in local, which the replay cannot list.
func (s *state) importForeign(st sqllex.Statement) {
	if !st.Match(1, "FOREIGN", "SCHEMA") {
		return
	}
	in := st.FindTop(3, "INTO")
	if in < 0 {
		return
	}
	local := st.At(in + 1).Ident(sqllex.Postgres)
	if local == "" || !s.judge || !s.exposed[local] {
		return
	}
	s.report(s.at(st), RLS007, packs.LevelError, fmt.Sprintf("IMPORT FOREIGN SCHEMA creates foreign tables in exposed schema %s, and a foreign table cannot have row-level security: the API serves every row the remote server returns; import into a private schema and expose only what a caller may see through a function", local))
}

// alterMatView follows a materialized view's renames and moves; moving one
// into an exposed schema exposes it, so the move is reported like a CREATE.
// The move is judged even when the replay never saw the view created.
func (s *state) alterMatView(st sqllex.Statement, i int) {
	i = st.Skip(i, []string{"IF", "EXISTS"})
	n, j := s.name(st, i)
	if n.IsZero() {
		return
	}
	switch {
	case st.Match(j, "RENAME", "TO"):
		if to := st.At(j + 2).Ident(sqllex.Postgres); to != "" {
			delete(s.matviews, n.String())
			s.matviews[sqllex.Name{Schema: n.Schema, Object: to}.String()] = true
		}
	case st.Match(j, "SET", "SCHEMA"):
		if to := st.At(j + 2).Ident(sqllex.Postgres); to != "" {
			delete(s.matviews, n.String())
			m := sqllex.Name{Schema: to, Object: n.Object}
			s.matviews[m.String()] = true
			s.exposedMatView(st, m)
		}
	}
}

// dynamicRe matches the DDL the gate would judge if it were not inside a
// string: policies, row level security, grants, security definer, set
// schema, security_invoker, a non-temporary CREATE [FOREIGN] TABLE or CREATE
// [MATERIALIZED|RECURSIVE] VIEW, and IMPORT FOREIGN SCHEMA (RLS001 /
// RLS007). A temporary table or view is never exposed and stays unflagged.
var dynamicRe = regexp.MustCompile(`(?is)\brow\s+level\s+security\b|\b(?:create|alter|drop)\s+policy\b|\bgrant\b|\bsecurity\s+definer\b|\bset\s+schema\b|\bsecurity_invoker\b` +
	`|\bcreate\s+(?:(?:unlogged|global|local|foreign)\s+)?table\b|\bimport\s+foreign\s+schema\b|\bcreate\s+(?:or\s+replace\s+)?(?:materialized\s+|recursive\s+)?view\b`)

// dynamicSQL reports a judged DO block or routine whose body mentions DDL
// the replay cannot see (RLS011). It fails closed: the reviewer judges the
// body and suppresses the finding with -- tq:allow RLS011: <reason>.
func (s *state) dynamicSQL(st sqllex.Statement) {
	if !s.judge {
		return
	}
	switch st.Keyword() {
	case "DO":
	case "CREATE":
		i := st.Skip(1, []string{"OR", "REPLACE"})
		if !st.Match(i, "FUNCTION|PROCEDURE") {
			return
		}
	default:
		return
	}
	for _, t := range st.Tokens {
		if t.Kind == sqllex.String && dynamicRe.MatchString(t.Text) {
			s.report(s.at(st), RLS011, packs.LevelError, "this DO block or routine body runs DDL the RLS gate cannot judge (policies, row level security, grants, security definer, set schema, or a CREATE TABLE or VIEW inside a string); review it and mark it -- tq:allow RLS011: <what it does>, or write the DDL as plain statements")
			return
		}
	}
}

// functionOptions applies SECURITY DEFINER|INVOKER and SET/RESET search_path
// found at the top level from i.
func functionOptions(st sqllex.Statement, i int, f *function) {
	for k := i; k < len(st.Tokens); k++ {
		switch {
		case st.Match(k, "SECURITY", "DEFINER"):
			f.definer = true
		case st.Match(k, "SECURITY", "INVOKER"):
			f.definer = false
		case st.Match(k, "SET", "SEARCH_PATH"):
			f.searchPath = true
		case st.Match(k, "RESET", "SEARCH_PATH"), st.Match(k, "RESET", "ALL"):
			f.searchPath = false
		case st.Match(k, "BEGIN", "ATOMIC"):
			return
		}
	}
}

func (s *state) createFunction(st sqllex.Statement, i int) {
	n, j := s.newName(st, i)
	if n.IsZero() || !st.At(j).IsPunct("(") {
		return
	}
	e := st.Close(j)
	if e < 0 {
		return
	}
	f := &function{name: n, at: s.at(st), judged: s.judge}
	if old := s.funcs[n.String()]; old != nil {
		// CREATE OR REPLACE keeps the privileges.
		f.revokedPublic, f.revokedAnon, f.grantedAnon, f.grantedPublic = old.revokedPublic, old.revokedAnon, old.grantedAnon, old.grantedPublic
		f.judged = f.judged || old.judged
		f.reason = old.reason
	}
	functionOptions(st, e+1, f)
	f.reason = f.reason || hasMarker(f.at, "rls-anon-exec")
	s.funcs[n.String()] = f
}

func hasMarker(l loc, name string) bool {
	for _, ds := range [][]sqllex.Directive{l.dirs, l.same} {
		for _, d := range ds {
			if d.Name == name && strings.TrimSpace(d.Raw) != "" {
				return true
			}
		}
	}
	return false
}

func (s *state) alterFunction(st sqllex.Statement, i int) {
	i = st.Skip(i, []string{"IF", "EXISTS"})
	n, j := s.name(st, i)
	f := s.funcs[n.String()]
	if n.IsZero() || f == nil {
		return
	}
	if st.At(j).IsPunct("(") {
		if j = st.Close(j); j < 0 {
			return
		}
		j++
	}
	switch {
	case st.Match(j, "RENAME", "TO"):
		if to := st.At(j + 2).Ident(sqllex.Postgres); to != "" {
			delete(s.funcs, n.String())
			f.name.Object = to
			s.funcs[f.name.String()] = f
		}
		return
	case st.Match(j, "SET", "SCHEMA"):
		if to := st.At(j + 2).Ident(sqllex.Postgres); to != "" {
			delete(s.funcs, n.String())
			f.name.Schema = to
			s.funcs[f.name.String()] = f
		}
	}
	functionOptions(st, j, f)
	if s.judge {
		f.judged = true
		f.at = s.at(st)
		f.reason = f.reason || hasMarker(f.at, "rls-anon-exec")
	}
}

// grantParts splits GRANT/REVOKE into privileges, the ON part and the roles.
type grantParts struct {
	privs   []string
	objKind string // TABLE, FUNCTION, SCHEMA, ALL TABLES, … ("" = a table name list)
	objects []sqllex.Name
	schemas []string
	roles   []string
}

func (s *state) parseGrant(st sqllex.Statement, start int, toWord string) (grantParts, bool) {
	var g grantParts
	on := st.FindTop(start, "ON")
	to := -1
	if on >= 0 {
		to = st.FindTop(on, toWord)
	}
	if on < 0 || to < 0 {
		return g, false
	}
	g.privs = st.Words(start, on)
	k := on + 1
	switch {
	case st.Match(k, "ALL", "TABLES|SEQUENCES|FUNCTIONS|PROCEDURES|ROUTINES", "IN", "SCHEMA"):
		g.objKind = "ALL " + st.At(k+1).Upper()
		for _, r := range st.Split(k+4, to) {
			if v := st.At(r[0]).Ident(sqllex.Postgres); v != "" {
				g.schemas = append(g.schemas, v)
			}
		}
	case st.Match(k, "SCHEMA"):
		g.objKind = "SCHEMA"
		for _, r := range st.Split(k+1, to) {
			if v := st.At(r[0]).Ident(sqllex.Postgres); v != "" {
				g.schemas = append(g.schemas, v)
			}
		}
	case st.Match(k, "TABLE|SEQUENCE|FUNCTION|PROCEDURE|ROUTINE|VIEW"), st.Match(k, "FOREIGN", "TABLE"):
		g.objKind = st.At(k).Upper()
		if g.objKind == "FOREIGN" {
			k++
		}
		fallthrough
	default:
		if st.Match(k, "DATABASE|TYPE|DOMAIN|LANGUAGE|TABLESPACE|SERVER|LARGE") || (st.Match(k, "FOREIGN", "DATA")) {
			g.objKind = "OTHER"
			break
		}
		if g.objKind != "" {
			k++
		}
		for _, r := range st.Split(k, to) {
			if n, _ := s.name(st, r[0]); !n.IsZero() {
				g.objects = append(g.objects, n)
				g.schemas = append(g.schemas, n.Schema)
			}
		}
	}
	for i := to + 1; i < len(st.Tokens); i++ {
		t := st.At(i)
		if t.Is("WITH", "GRANTED", "CASCADE", "RESTRICT") {
			break
		}
		if t.Is("GROUP") || t.IsPunct(",") {
			continue
		}
		if v := t.Ident(sqllex.Postgres); v != "" {
			g.roles = append(g.roles, v)
		}
	}
	return g, true
}

var writePrivs = []string{"ALL", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "CREATE"}

func hasWord(ws []string, want ...string) bool {
	for _, w := range ws {
		for _, x := range want {
			if w == x {
				return true
			}
		}
	}
	return false
}

func (s *state) grant(st sqllex.Statement, defaults bool) {
	start := 1
	if defaults {
		start = st.FindTop(0, "GRANT")
		if start < 0 {
			return
		}
		start++
	}
	if !defaults && st.FindTop(start, "ON") < 0 {
		s.roleGrant(st, start)
		return
	}
	g, ok := s.parseGrant(st, start, "TO")
	if !ok {
		return
	}
	if !defaults && isFuncKind(g.objKind) && hasWord(g.privs, "EXECUTE", "ALL") {
		toAnon := hasRole(g.roles, "anon", "public")
		for _, f := range s.funcsOf(g) {
			if hasRole(g.roles, "anon") {
				f.grantedAnon = true
			}
			if hasRole(g.roles, "public") {
				f.grantedPublic = true
			}
			// A judged grant that lets anon call the function judges the
			// function there (RLS008), wherever it was created.
			if toAnon && s.judge {
				l := s.at(st)
				if !f.judged || f.at.File != s.path {
					f.at = l
				}
				f.judged = true
				f.reason = f.reason || hasMarker(l, "rls-anon-exec")
			}
		}
	}
	if !s.judge {
		return
	}
	l := s.at(st)
	exposed := false
	for _, sc := range g.schemas {
		if s.exposed[sc] {
			exposed = true
		}
	}
	switch {
	case defaults && hasRole(g.roles, "anon"):
		s.report(l, RLS006, packs.LevelError, "ALTER DEFAULT PRIVILEGES grants to anon: every future object gets the grant without a review; grant per object instead")
	case !defaults && hasRole(g.roles, "anon") && hasWord(g.privs, writePrivs...):
		s.report(l, RLS006, packs.LevelError, fmt.Sprintf("GRANT %s to anon: the anonymous key can write; grant only SELECT (and let RLS decide) or grant to authenticated", strings.Join(g.privs, " ")))
	case hasRole(g.roles, "public") && (exposed || (defaults && len(g.schemas) == 0)):
		s.report(l, RLS006, packs.LevelError, "GRANT … TO public in an exposed schema: public includes anon; grant to the roles that need it")
	}
}

// roleGrant judges GRANT role [, …] TO role [, …] (a membership grant, no
// ON): granted to anon or public, it gives anon every privilege of those
// roles and lets it pass the policies written TO them (RLS006).
func (s *state) roleGrant(st sqllex.Statement, start int) {
	to := st.FindTop(start, "TO")
	if to < 0 || !s.judge {
		return
	}
	var granted, grantees []string
	for i := start; i < to; i++ {
		if v := st.At(i).Ident(sqllex.Postgres); v != "" {
			granted = append(granted, v)
		}
	}
	for i := to + 1; i < len(st.Tokens); i++ {
		t := st.At(i)
		if t.Is("WITH", "GRANTED") {
			break
		}
		if t.Is("GROUP") || t.IsPunct(",") {
			continue
		}
		if v := t.Ident(sqllex.Postgres); v != "" {
			grantees = append(grantees, v)
		}
	}
	if len(granted) == 0 || !hasRole(grantees, "anon", "public") {
		return
	}
	s.report(s.at(st), RLS006, packs.LevelError, fmt.Sprintf("GRANT %s to anon or public: the anonymous key inherits every privilege of %s and passes the policies written for it; grant per object instead", strings.Join(granted, ", "), strings.Join(granted, ", ")))
}

func (s *state) defaultPrivileges(st sqllex.Statement) {
	if st.FindTop(0, "GRANT") >= 0 {
		s.grant(st, true)
	}
}

func (s *state) funcsOf(g grantParts) []*function {
	var out []*function
	if strings.HasPrefix(g.objKind, "ALL ") {
		for _, f := range s.funcs {
			for _, sc := range g.schemas {
				if f.name.Schema == sc {
					out = append(out, f)
				}
			}
		}
		return out
	}
	for _, n := range g.objects {
		if f := s.funcs[n.String()]; f != nil {
			out = append(out, f)
		}
	}
	return out
}

func (s *state) revoke(st sqllex.Statement) {
	start := st.Skip(1, []string{"GRANT", "OPTION", "FOR"})
	g, ok := s.parseGrant(st, start, "FROM")
	if !ok || !hasWord(g.privs, "EXECUTE", "ALL") {
		return
	}
	if !isFuncKind(g.objKind) {
		return
	}
	for _, f := range s.funcsOf(g) {
		if hasRole(g.roles, "public") {
			f.revokedPublic, f.grantedPublic = true, false
		}
		if hasRole(g.roles, "anon") {
			f.revokedAnon, f.grantedAnon = true, false
		}
	}
}

func isFuncKind(k string) bool {
	switch k {
	case "FUNCTION", "PROCEDURE", "ROUTINE", "ALL FUNCTIONS", "ALL PROCEDURES", "ALL ROUTINES":
		return true
	}
	return false
}

// anonExec reports whether anon can execute f.
func (s *state) anonExec(f *function) bool {
	if f.grantedAnon || f.grantedPublic {
		return true
	}
	if !f.revokedPublic {
		return true // EXECUTE is granted to PUBLIC by default
	}
	return s.o.Supabase && !f.revokedAnon // Supabase's default privileges grant anon too
}

func (s *state) judgeState() {
	keys := make([]string, 0, len(s.tables))
	for k := range s.tables {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t := s.tables[k]
		if !s.exposed[t.Name.Schema] {
			continue
		}
		if t.Created && t.create.File != "" && s.judged(t.create.File) && !t.enabledIn[t.create.File] {
			s.report(t.create, RLS001, packs.LevelError, fmt.Sprintf("table %s is in an exposed schema and this migration does not ENABLE ROW LEVEL SECURITY on it: the API serves every row", t.Name))
		}
		if t.RLS && len(t.Policies) == 0 && len(t.touch) > 0 {
			at := t.enable
			if !s.judged(at.File) {
				at = firstTouch(t)
			}
			switch {
			case t.denyAll != nil && strings.TrimSpace(t.denyAll.Raw) != "":
			case t.denyAll != nil:
				s.report(at, RLS002, packs.LevelError, fmt.Sprintf("tq:rls-deny-all on %s needs a reason: -- tq:rls-deny-all <why only the service role uses it>", t.Name))
			default:
				s.report(at, RLS002, packs.LevelError, fmt.Sprintf("table %s has RLS enabled and no policy: nobody but the service role can use it; add a policy, or mark it -- tq:rls-deny-all <reason>", t.Name))
			}
		}
	}
	vkeys := make([]string, 0, len(s.views))
	for k := range s.views {
		vkeys = append(vkeys, k)
	}
	sort.Strings(vkeys)
	for _, k := range vkeys {
		v := s.views[k]
		if v.judged && s.exposed[v.name.Schema] && !v.invoker && s.judged(v.at.File) {
			s.report(v.at, RLS007, packs.LevelError, fmt.Sprintf("view %s runs with its owner's rights and bypasses RLS on the tables it reads; create it WITH (security_invoker = true)", v.name))
		}
	}
	fkeys := make([]string, 0, len(s.funcs))
	for k := range s.funcs {
		fkeys = append(fkeys, k)
	}
	sort.Strings(fkeys)
	for _, k := range fkeys {
		f := s.funcs[k]
		if !f.judged || !f.definer || !s.exposed[f.name.Schema] || !s.judged(f.at.File) {
			continue
		}
		if !f.searchPath {
			s.report(f.at, RLS008, packs.LevelError, fmt.Sprintf("SECURITY DEFINER function %s has no SET search_path: a caller's objects can shadow the ones it uses; add SET search_path = ''", f.name))
		}
		if s.anonExec(f) && !f.reason {
			s.report(f.at, RLS008, packs.LevelError, fmt.Sprintf("SECURITY DEFINER function %s is callable by anon through the API; REVOKE EXECUTE … FROM public, anon, or mark it -- tq:rls-anon-exec <reason>", f.name))
		}
	}
}

// judged reports whether file is judged; a location in a replay-only file
// is never reported.
func (s *state) judged(file string) bool { return file != "" && s.judgedFile[file] }

func firstTouch(t *Table) loc {
	var best loc
	for _, l := range t.touch {
		if best.File == "" || l.File < best.File || (l.File == best.File && l.Line < best.Line) {
			best = l
		}
	}
	return best
}
