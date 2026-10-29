package db

// Workstream D-DR (spec D §11, §12, §15): the RLS gate and the pgTAP tests.
//
// Checks:   db/rls, db/rls-tests (built-in); db/pgtap (external, pg_prove)
//           and db/pgtap-supabase (external, supabase test db).
// Keys:     rls (exposed_schemas, tests), test_db (url_env, start, hosts,
//           dotenv).
// Flags:    rls_change (raised) -> db-reviewer and security-reviewer.

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/db/lint"
	"github.com/tentaqles/tentaqles/internal/packs/db/pgtap"
	"github.com/tentaqles/tentaqles/internal/packs/db/rls"
	"gopkg.in/yaml.v3"
)

const (
	// rlsSkill is the command the coverage findings point to (D-PL's skill).
	rlsSkill = "/tentaqles:rls-tests"
	// supabaseTests is the directory `supabase test db` runs.
	supabaseTests = "supabase/tests"
	// maxTestFiles and maxTestBytes bound the coverage read.
	maxTestFiles = 2000
	maxTestBytes = 1 << 20
)

func init() {
	packs.RegisterPack(packs.Pack{
		Name: "db",
		ConfigKeys: map[string]func(yaml.Node) error{
			"rls":     rls.ValidateRLS,
			"test_db": pgtap.ValidateTestDB,
		},
		Merge: map[string]packs.MergeRule{
			// More exposed schemas judge more: a repo file may add them.
			"rls.exposed_schemas": packs.Union(),
			// The tests directory: the repo may set it when the manifest
			// does not; it never changes the manifest's.
			"rls.tests": packs.TrustedAdd(),
			// test_db stays trusted-layer only (the zero rule): its start is
			// a command tq runs, and its url_env picks what tq connects to.
		},
		Flags: []packs.Flag{
			{Name: "rls_change", Reviewers: []string{reviewer, "security-reviewer"}, Raise: raiseRLSChange},
		},
	})
	packs.Register(packs.Check{ID: "db/rls", Pack: "db", Kind: packs.Builtin,
		Applies: rlsApplies, Run: runRLS})
	packs.Register(packs.Check{ID: "db/rls-tests", Pack: "db", Kind: packs.Builtin,
		Applies: func(in *packs.Input) bool { return rlsApplies(in) || testsChanged(in) },
		Run:     runRLSTests})
	// Env is minimal: the connection comes from test_db only (pgtap.Mode.Env).
	packs.Register(packs.Check{ID: "db/pgtap", Pack: "db", Kind: packs.External, Tool: "pg_prove",
		Env:     pgtap.PgProve.Env(),
		Applies: func(in *packs.Input) bool { return pgtapApplies(in) && !supabaseMode(in) },
		Run:     func(ctx context.Context, in *packs.Input) packs.Result { return runPgtap(ctx, in, pgtap.PgProve) }})
	packs.Register(packs.Check{ID: "db/pgtap-supabase", Pack: "db", Kind: packs.External, Tool: "supabase",
		Env:     pgtap.Supabase.Env(),
		Applies: func(in *packs.Input) bool { return pgtapApplies(in) && supabaseMode(in) },
		Run:     func(ctx context.Context, in *packs.Input) packs.Result { return runPgtap(ctx, in, pgtap.Supabase) }})
}

// postgresEntries are the Postgres packs.db.migrations entries.
func postgresEntries(in *packs.Input) []lint.Migration {
	var out []lint.Migration
	for _, m := range lint.Migrations(in.Settings()) {
		if m.Engine == "postgres" {
			out = append(out, m)
		}
	}
	return out
}

// configPaths are the supabase/config.toml candidates: beside each
// .../supabase/migrations entry, then at the repository root.
func configPaths(in *packs.Input) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range postgresEntries(in) {
		if strings.HasSuffix("/"+m.Path, "/supabase/migrations") {
			p := path.Join(path.Dir(m.Path), "config.toml")
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	if !seen["supabase/config.toml"] {
		out = append(out, "supabase/config.toml")
	}
	return out
}

func configChanged(in *packs.Input) bool {
	for _, p := range in.ChangedPaths() {
		if p == "supabase/config.toml" || strings.HasSuffix(p, "/supabase/config.toml") {
			return true
		}
	}
	return false
}

func rlsApplies(in *packs.Input) bool {
	return anyMigrationChanged(in, "postgres") || (len(postgresEntries(in)) > 0 && configChanged(in))
}

func pgtapApplies(in *packs.Input) bool { return rlsApplies(in) || testsChanged(in) }

func raiseRLSChange(in *packs.Input) bool {
	return anyMigrationChanged(in, "postgres") || testsChanged(in) || configChanged(in)
}

// supabaseMode: the tests run through `supabase test db` when supabase is
// declared, or when a Postgres entry uses the supabase migrator and
// pg_prove is not declared.
func supabaseMode(in *packs.Input) bool {
	s := in.Settings()
	if s.Declared("supabase") {
		return true
	}
	if s.Declared("pg_prove") {
		return false
	}
	for _, m := range postgresEntries(in) {
		if m.Migrator == "supabase" {
			return true
		}
	}
	return false
}

// isSupabase: Supabase semantics for the replay (exposed schemas from
// config.toml, storage.objects, anon's default EXECUTE).
func isSupabase(in *packs.Input, toml *rls.SupabaseConfig) bool {
	if toml != nil || in.Settings().Declared("supabase") {
		return true
	}
	for _, m := range postgresEntries(in) {
		if m.Migrator == "supabase" {
			return true
		}
	}
	return false
}

func readConfig(in *packs.Input) *rls.SupabaseConfig {
	for _, p := range configPaths(in) {
		if b, err := in.ReadNew(p, 1<<20); err == nil {
			c := rls.ParseSupabaseConfig(b)
			return &c
		}
	}
	return nil
}

// testsDir is packs.db.rls.tests, else supabase/tests in Supabase mode.
func testsDir(in *packs.Input) string {
	if d := strings.Trim(strings.ReplaceAll(in.Settings().String("rls.tests"), `\`, "/"), "/"); d != "" && rls.RelPath(d) == nil {
		return strings.TrimPrefix(path.Clean(d), "./")
	}
	if isSupabase(in, nil) {
		return supabaseTests
	}
	return ""
}

func isTestFile(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".sql", ".pg":
		return true
	}
	return false
}

// runTests keeps the test files the runner runs: pg_prove runs with
// --ext .sql (spec D §12), so only a .sql file counts as coverage. A .pg
// file still marks the tests changed, but a table it alone names is
// untested.
func runTests(files []string) []string {
	var out []string
	for _, p := range files {
		if strings.EqualFold(path.Ext(p), ".sql") {
			out = append(out, p)
		}
	}
	return out
}

func testsChanged(in *packs.Input) bool {
	d := testsDir(in)
	if d == "" {
		return false
	}
	for _, p := range in.ChangedPaths() {
		if packs.MatchGlob(d+"/", p) && isTestFile(p) {
			return true
		}
	}
	return false
}

// testFiles lists the test files under the tests directory (tracked plus
// changed, deletions excluded).
func testFiles(in *packs.Input, dir string) []string {
	if dir == "" {
		return nil
	}
	deleted := map[string]bool{}
	for _, c := range in.Changed {
		if c.Status == 'D' {
			deleted[c.Path] = true
		}
	}
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !seen[p] && !deleted[p] && packs.MatchGlob(dir+"/", p) && isTestFile(p) {
			seen[p] = true
			out = append(out, p)
		}
	}
	if in.Git != nil {
		if fs, err := in.Git.ListFiles(); err == nil {
			for _, f := range fs {
				add(f)
			}
		}
	}
	for _, p := range in.ChangedPaths() {
		add(p)
	}
	sort.Strings(out)
	return out
}

type replay struct {
	res      []rls.Result
	exposed  []string
	judged   int
	failRead string
}

// replayAll replays every Postgres entry; judge selects the judged files.
func replayAll(ctx context.Context, in *packs.Input, judge bool) replay {
	s := in.Settings()
	toml := readConfig(in)
	var r replay
	r.exposed = rls.ExposedSchemas(s.Strings("rls.exposed_schemas"), toml, isSupabase(in, toml))
	opt := rls.Options{Exposed: r.exposed, Supabase: isSupabase(in, toml)}
	all := judge && configChanged(in)
	for _, m := range postgresEntries(in) {
		changed, _ := lint.ChangedIn(in, m)
		isChanged := map[string]bool{}
		for _, p := range changed {
			isChanged[p] = true
		}
		var units []rls.Unit
		for _, p := range lint.Files(in, m) {
			if ctx.Err() != nil {
				r.failRead = "timed out"
				return r
			}
			if !lint.Lintable(m.Migrator, p) {
				continue
			}
			j := judge && (all || isChanged[p])
			src, err := in.ReadNew(p, maxMigrationBytes)
			if err != nil {
				if j {
					r.failRead = "cannot read " + p
					return r
				}
				continue
			}
			if j {
				r.judged++
			}
			units = append(units, rls.Unit{Path: p, Src: src, Judge: j, Migrator: m.Migrator})
		}
		r.res = append(r.res, rls.Run(units, opt))
	}
	return r
}

func countSuppressed(rules []string) int {
	byRule := map[string]int{}
	for _, r := range rules {
		byRule[r]++
	}
	keys := make([]string, 0, len(byRule))
	for k := range byRule {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		packs.Count("db", "suppression:"+strings.ToLower(k), byRule[k])
	}
	return len(rules)
}

func runRLS(ctx context.Context, in *packs.Input) packs.Result {
	r := replayAll(ctx, in, true)
	if r.failRead != "" {
		return packs.Result{Status: packs.Fail, Detail: r.failRead}
	}
	var fs []packs.Finding
	var supp []string
	tables := 0
	for _, res := range r.res {
		fs = append(fs, res.Findings...)
		supp = append(supp, res.Suppressed...)
		tables += len(res.Tables)
	}
	n := countSuppressed(supp)
	detail := fmt.Sprintf("%d judged migration file(s), %d table(s) in the exposed schemas (%s)", r.judged, tables, strings.Join(r.exposed, ", "))
	if n > 0 {
		detail += fmt.Sprintf(", %d suppressed", n)
	}
	return packs.Result{Status: packs.OK, Detail: detail, Findings: dedupe(fs)}
}

func runRLSTests(ctx context.Context, in *packs.Input) packs.Result {
	r := replayAll(ctx, in, false)
	if r.failRead != "" {
		return packs.Result{Status: packs.Fail, Detail: r.failRead}
	}
	dir := testsDir(in)
	tests := map[string][]byte{}
	files := runTests(testFiles(in, dir))
	if len(files) > maxTestFiles {
		files = files[:maxTestFiles]
	}
	for _, p := range files {
		if ctx.Err() != nil {
			return packs.Result{Status: packs.Fail, Detail: "timed out"}
		}
		if b, err := in.ReadNew(p, maxTestBytes); err == nil {
			tests[p] = b
		}
	}
	var fs []packs.Finding
	var supp, untested []string
	withRLS := 0
	for _, res := range r.res {
		for _, t := range res.Tables {
			if t.RLS {
				withRLS++
			}
		}
		f, s, u := rls.Coverage(res.Tables, tests, dir, rlsSkill)
		fs = append(fs, f...)
		supp = append(supp, s...)
		untested = append(untested, u...)
	}
	countSuppressed(supp)
	if len(untested) > 0 {
		return packs.Result{Status: packs.Fail, Findings: dedupe(fs),
			Detail: fmt.Sprintf("%d table(s) with RLS have no pgTAP test: %s; write them with %s", len(untested), strings.Join(untested, ", "), rlsSkill)}
	}
	return packs.Result{Status: packs.OK, Findings: dedupe(fs),
		Detail: fmt.Sprintf("%d table(s) with RLS, %d test file(s) under %s", withRLS, len(files), orUnset(dir))}
}

func orUnset(d string) string {
	if d == "" {
		return "packs.db.rls.tests (not set)"
	}
	return d
}

func runPgtap(ctx context.Context, in *packs.Input, mode pgtap.Mode) packs.Result {
	dir := testsDir(in)
	o := pgtap.Options{Mode: mode, TestsDir: dir, TestFiles: testFiles(in, dir)}
	if mode == pgtap.Supabase {
		if c := readConfig(in); c != nil {
			o.SupabasePort = c.DBPort
		}
	}
	return pgtap.Run(ctx, in, o)
}
