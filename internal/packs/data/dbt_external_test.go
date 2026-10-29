package data

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

// TestMain doubles as the fake dbt and sqlfluff: a copy of the test binary
// run with TQ_FAKE_TOOL set records its argv and prints the canned output.
func TestMain(m *testing.M) {
	if mode := os.Getenv("TQ_FAKE_TOOL"); mode != "" {
		fakeTool(mode)
		return
	}
	packs.GenerousBudgetsForTests() // CI load must not fail a guard read closed
	os.Exit(m.Run())
}

func fakeTool(mode string) {
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("Core:\n  - installed: " + os.Getenv("TQ_FAKE_VERSION"))
		os.Exit(0)
	}
	if d := os.Getenv("TQ_FAKE_ARGS"); d != "" {
		f := filepath.Join(d, strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")+".txt")
		wd, _ := os.Getwd()
		env := "DBT_SEND_ANONYMOUS_USAGE_STATS=" + os.Getenv("DBT_SEND_ANONYMOUS_USAGE_STATS")
		_ = os.WriteFile(f, []byte(wd+"\n"+env+"\n"+strings.Join(args, "\n")), 0o600)
	}
	if mode == "sleep" {
		time.Sleep(30 * time.Second)
	}
	if f := os.Getenv("TQ_FAKE_STDOUT"); f != "" {
		b, _ := os.ReadFile(f)
		os.Stdout.Write(b)
	}
	if s := os.Getenv("TQ_FAKE_STDERR"); s != "" {
		fmt.Fprintln(os.Stderr, s)
	}
	code, _ := strconv.Atoi(os.Getenv("TQ_FAKE_EXIT"))
	os.Exit(code)
}

// fakeBin copies the test binary to dir/name(.exe).
func fakeBin(t *testing.T, dir, name string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dst := filepath.Join(dir, name)
	in, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	out.Close()
	return dst
}

func writeFile(t testing.TB, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeTools is a ToolRunner that records the specs it ran.
type fakeTools struct {
	probes map[string]toolrun.Probe
	outs   map[string]toolrun.Output
	errs   map[string]error
	specs  *[]toolrun.Spec
}

func (f fakeTools) Probe(pack, tool string) toolrun.Probe {
	if p, ok := f.probes[tool]; ok {
		return p
	}
	return toolrun.Probe{Tool: tool}
}

func (f fakeTools) Run(ctx context.Context, s toolrun.Spec) (toolrun.Output, error) {
	if f.specs != nil {
		*f.specs = append(*f.specs, s)
	}
	if err := f.errs[s.Tool]; err != nil {
		return toolrun.Output{}, err
	}
	return f.outs[s.Tool], nil
}

func dbtRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "analytics/dbt_project.yml", "name: acme\nprofile: acme\n")
	writeFile(t, root, "analytics/models/marts/fct_orders.sql", "select order_id from {{ ref('stg_orders') }}\n")
	return root
}

func extInput(t *testing.T, root, cfg string, tools packs.ToolRunner, changed ...string) *packs.Input {
	t.Helper()
	var cs []packs.ChangedFile
	for _, c := range changed {
		cs = append(cs, packs.ChangedFile{Path: c, Status: 'M'})
	}
	in := &packs.Input{Root: root, Base: "HEAD", Changed: cs, Config: packs.Merge(packsOf(t, cfg), nil), Tools: tools,
		Budget: time.Now().Add(time.Minute)}
	in.SetWorkspace("acme", root)
	return in.ForPack("data")
}

func TestDBTParseArgsAndScratch(t *testing.T) {
	isolate(t)
	root := dbtRepo(t)
	var specs []toolrun.Spec
	tools := fakeTools{specs: &specs, outs: map[string]toolrun.Output{"dbt": {ExitCode: 0, Version: "1.8.7"}}}
	in := extInput(t, root, "data: {tools: [dbt]}", tools, "analytics/models/marts/fct_orders.sql")
	r := runDBTParse(context.Background(), in)
	if r.Status != packs.OK || len(specs) != 1 {
		t.Fatalf("parse: %+v specs %+v", r, specs)
	}
	s := specs[0]
	if s.Tool != "dbt" || s.Pack != "data" || s.Args[0] != "parse" || s.Timeout != 180*time.Second {
		t.Fatalf("spec %+v", s)
	}
	if s.Dir != filepath.Join(root, "analytics") {
		t.Fatalf("dbt parse must run in the project dir, got %s", s.Dir)
	}
	joined := strings.Join(s.Args, " ")
	scratch := filepath.Join(os.Getenv("TQ_HOME"), "packs", "acme", "scratch")
	if !strings.Contains(joined, "--target-path "+scratch) || !strings.Contains(joined, "--log-path "+scratch) {
		t.Fatalf("artifacts and logs go to the scratch dir: %s", joined)
	}
	if ents, _ := os.ReadDir(scratch); len(ents) != 0 {
		t.Fatalf("the scratch dir is removed after the run: %v", ents)
	}
}

func TestDBTParseFailureTailIsRedacted(t *testing.T) {
	isolate(t)
	root := dbtRepo(t)
	tools := fakeTools{outs: map[string]toolrun.Output{"dbt": {ExitCode: 2,
		Stderr: []byte("Compilation Error in model fct_orders\n  contact tester@acme.test\n")}}}
	in := extInput(t, root, "data: {tools: [dbt]}", tools, "analytics/models/marts/fct_orders.sql")
	r := runDBTParse(context.Background(), in)
	if r.Status != packs.Fail || !strings.Contains(r.Detail, "Compilation Error") || !strings.Contains(r.Detail, "exit 2") {
		t.Fatalf("parse failure: %+v", r)
	}
	if strings.Contains(r.Detail, "tester@acme.test") {
		t.Fatalf("the tail must be redacted: %s", r.Detail)
	}
	tools = fakeTools{outs: map[string]toolrun.Output{"dbt": {TimedOut: true}}}
	r = runDBTParse(context.Background(), extInput(t, root, "data: {tools: [dbt]}", tools, "analytics/models/marts/fct_orders.sql"))
	if r.Status != packs.Fail || !strings.Contains(r.Detail, "timed out") {
		t.Fatalf("timeout: %+v", r)
	}
}

func TestDBTParseNoProjectSkips(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	writeFile(t, root, "scripts/x.sql", "select 1\n")
	r := runDBTParse(context.Background(), extInput(t, root, "data: {tools: [dbt]}", fakeTools{}, "scripts/x.sql"))
	if r.Status != packs.Skip {
		t.Fatalf("no project: %+v", r)
	}
	// a configured dbt.project that is missing fails
	r = runDBTParse(context.Background(), extInput(t, root, "data: {dbt: {project: nowhere}}", fakeTools{}, "scripts/x.sql"))
	if r.Status != packs.Fail || !strings.Contains(r.Detail, "nowhere") {
		t.Fatalf("missing configured project: %+v", r)
	}
}

func TestDBTBuildModified(t *testing.T) {
	isolate(t)
	root := dbtRepo(t)
	cfg := "data: {dbt: {project: analytics, build: dev, state: artifacts/prod}}"
	var specs []toolrun.Spec
	tools := fakeTools{specs: &specs, outs: map[string]toolrun.Output{"dbt": {}}}
	in := extInput(t, root, cfg, tools, "analytics/models/marts/fct_orders.sql")
	if !buildApplies(in) {
		t.Fatal("build: dev applies")
	}
	if r := runDBTBuildModified(context.Background(), in); r.Status != packs.Fail || !strings.Contains(r.Detail, "export the production manifest to dbt.state") {
		t.Fatalf("missing state: %+v", r)
	}
	writeFile(t, root, "artifacts/prod/manifest.json", "{}")
	r := runDBTBuildModified(context.Background(), in)
	if r.Status != packs.OK || len(specs) != 1 {
		t.Fatalf("build: %+v", r)
	}
	got := strings.Join(specs[0].Args, " ")
	want := "build --select state:modified+ --defer --state " + filepath.Join(root, "artifacts", "prod") + " --target dev --target-path"
	if !strings.HasPrefix(got, want) || specs[0].Timeout != 1200*time.Second {
		t.Fatalf("args %q timeout %v", got, specs[0].Timeout)
	}

	// dev_target in prod_targets: refused, dbt never runs
	specs = nil
	in = extInput(t, root, "data: {dbt: {project: analytics, build: dev, state: artifacts/prod, dev_target: prod}}", tools, "analytics/models/marts/fct_orders.sql")
	if r := runDBTBuildModified(context.Background(), in); r.Status != packs.Fail || !strings.Contains(r.Detail, "refusing") || len(specs) != 0 {
		t.Fatalf("prod dev_target: %+v %v", r, specs)
	}
	// off: not applicable
	in = extInput(t, root, "data: {dbt: {project: analytics}}", tools, "analytics/models/marts/fct_orders.sql")
	if buildApplies(in) {
		t.Fatal("build off must not apply")
	}
}

const sqlfluffJSON = `[{"filepath": "models/marts/fct_orders.sql", "violations": [
 {"start_line_no": 2, "start_line_pos": 1, "code": "LT01", "description": "Expected only single space before 'tester@acme.test'.", "name": "layout.spacing"},
 {"start_line_no": 3, "code": "AM04", "description": "Query produces an unknown number of result columns.", "warning": true}
]}, {"filepath": "models/staging/stg_orders.sql", "violations": []}]`

func TestSqlfluffParsesAndRedacts(t *testing.T) {
	fs, err := parseSqlfluff([]byte(sqlfluffJSON), "/repo")
	if err != nil || len(fs) != 2 {
		t.Fatalf("%v %+v", err, fs)
	}
	if fs[0].File != "models/marts/fct_orders.sql" || fs[0].Line != 2 || fs[0].Rule != "LT01" || fs[0].Level != packs.LevelError {
		t.Fatalf("first finding %+v", fs[0])
	}
	if strings.Contains(fs[0].Message, "tester") {
		t.Fatalf("quoted text must be blanked: %q", fs[0].Message)
	}
	if fs[1].Level != packs.LevelWarn {
		t.Fatalf("warning: true is a warn: %+v", fs[1])
	}
	// sqlfluff 1.x used line_no
	fs, _ = parseSqlfluff([]byte(`[{"filepath":"a.sql","violations":[{"line_no":7,"code":"L010","description":"x"}]}]`), "/repo")
	if len(fs) != 1 || fs[0].Line != 7 {
		t.Fatalf("line_no: %+v", fs)
	}
	if _, err := parseSqlfluff([]byte("Traceback (most recent call last)"), "/repo"); err == nil {
		t.Fatal("non-JSON must be an error")
	}
}

func TestSqlfluffDialectAndArgs(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	writeFile(t, root, "dbt_project.yml", "name: acme\nprofile: acme\n")
	writeFile(t, root, "models/marts/fct_orders.sql", "select 1\n")
	writeFile(t, root, "target/compiled/x.sql", "select 1\n")
	var specs []toolrun.Spec
	tools := fakeTools{specs: &specs, outs: map[string]toolrun.Output{"sqlfluff": {ExitCode: 1, Stdout: []byte(sqlfluffJSON)}}}

	// no .sqlfluff, no profile, no engines: fail with the fix
	in := extInput(t, root, "data: {tools: [sqlfluff]}", tools, "models/marts/fct_orders.sql")
	if r := runSqlfluff(context.Background(), in); r.Status != packs.Fail || !strings.Contains(r.Detail, ".sqlfluff") {
		t.Fatalf("no dialect: %+v", r)
	}
	// the dbt adapter of an in-repo profiles.yml names it
	writeFile(t, root, "profiles.yml", "acme:\n  target: dev\n  outputs:\n    dev:\n      type: snowflake\n      account: \"{{ env_var('SNOWFLAKE_ACCOUNT') }}\"\n")
	r := runSqlfluff(context.Background(), extInput(t, root, "data: {tools: [sqlfluff]}", tools, "models/marts/fct_orders.sql", "target/compiled/x.sql"))
	if r.Status != packs.OK || len(r.Findings) != 2 {
		t.Fatalf("lint: %+v", r)
	}
	got := strings.Join(specs[len(specs)-1].Args, " ")
	if got != "lint --format json --templater jinja --dialect snowflake "+filepath.FromSlash("models/marts/fct_orders.sql") {
		t.Fatalf("args %q (target/ is never linted)", got)
	}
	// a .sqlfluff with a dialect wins: no --dialect flag
	writeFile(t, root, ".sqlfluff", "[sqlfluff]\ndialect = postgres\ntemplater = dbt\n")
	runSqlfluff(context.Background(), extInput(t, root, "data: {tools: [sqlfluff]}", tools, "models/marts/fct_orders.sql"))
	if got := strings.Join(specs[len(specs)-1].Args, " "); strings.Contains(got, "--dialect") || !strings.Contains(got, "--templater jinja") {
		t.Fatalf(".sqlfluff dialect: %q", got)
	}
	// a crash with no JSON fails with the tail
	tools.outs["sqlfluff"] = toolrun.Output{ExitCode: 2, Stderr: []byte("User Error: bad config")}
	if r := runSqlfluff(context.Background(), extInput(t, root, "data: {tools: [sqlfluff]}", tools, "models/marts/fct_orders.sql")); r.Status != packs.Fail || !strings.Contains(r.Detail, "User Error") {
		t.Fatalf("crash: %+v", r)
	}
}

// TestFakeCLIsOnPath runs the dbt and sqlfluff checks through the framework
// (RunChecks, the real toolrun.Runner) with fake binaries prepended to PATH.
func TestFakeCLIsOnPath(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	writeFile(t, root, "dbt_project.yml", "name: acme\nprofile: acme\n")
	writeFile(t, root, ".sqlfluff", "[sqlfluff]\ndialect = postgres\n")
	writeFile(t, root, "models/staging/stg_orders.sql", "select id as order_id from {{ ref('raw_orders') }}\n")
	writeFile(t, root, "models/staging/_staging.yml", "version: 2\nmodels:\n  - name: stg_orders\n    description: Orders.\n    columns:\n      - name: order_id\n        data_tests: [unique, not_null]\n")
	bin := t.TempDir()
	fakeBin(t, bin, "dbt")
	fakeBin(t, bin, "sqlfluff")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TQ_FAKE_TOOL", "record")
	argsDir := t.TempDir()
	t.Setenv("TQ_FAKE_ARGS", argsDir)
	out := filepath.Join(t.TempDir(), "out.json")
	os.WriteFile(out, []byte(`[{"filepath":"models/staging/stg_orders.sql","violations":[]}]`), 0o600)
	t.Setenv("TQ_FAKE_STDOUT", out)

	run := func(version string) map[string]packs.Result {
		t.Setenv("TQ_FAKE_VERSION", version)
		cfg := packs.Merge(packsOf(t, "data: {tools: [dbt, sqlfluff]}"), nil)
		in := &packs.Input{Root: root, Base: "HEAD", Config: cfg, Tools: packs.NewToolRunner(root, cfg), Budget: time.Now().Add(time.Minute),
			Changed: []packs.ChangedFile{{Path: "models/staging/stg_orders.sql", Status: 'A'}, {Path: "models/staging/_staging.yml", Status: 'A'}}}
		in.SetWorkspace("acme", root)
		rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"data"}})
		got := map[string]packs.Result{}
		for _, r := range rep.Results {
			got[r.ID] = r
		}
		return got
	}
	got := run("1.8.7")
	for _, id := range []string{idDBTParse, idSQLFluff, idNewModelTests, idSelectStar, idHardcodedRel} {
		if r := got[id]; r.Status != packs.OK || !r.Required {
			t.Errorf("%s: %+v", id, r)
		}
	}
	if r := got[idDBTParse]; r.Tool == nil || r.Tool.Version != "1.8.7" {
		t.Errorf("dbt version recorded: %+v", r.Tool)
	}
	if _, ok := got[idDBTBuildModified]; ok {
		t.Error("dbt-build-modified is opt-in")
	}
	raw, _ := os.ReadFile(filepath.Join(argsDir, "dbt.txt"))
	if !strings.Contains(string(raw), "DBT_SEND_ANONYMOUS_USAGE_STATS=false") || !strings.Contains(string(raw), "\nparse\n") {
		t.Errorf("dbt telemetry is switched off: %s", raw)
	}
	// below the 1.8 floor: the declared dbt check fails
	got = run("1.7.4")
	if r := got[idDBTParse]; r.Status != packs.Fail || !strings.Contains(r.Detail, "below the minimum 1.8.0") {
		t.Errorf("dbt 1.7: %+v", r)
	}
}

// TestFakeCLIsTimeout runs a hung dbt and sqlfluff through the real runner
// (review r0 Minor 7): the per-tool timeout kills them and both checks fail
// as timed out, well before the fake's 30 s sleep ends.
func TestFakeCLIsTimeout(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	writeFile(t, root, "dbt_project.yml", "name: acme\nprofile: acme\n")
	writeFile(t, root, ".sqlfluff", "[sqlfluff]\ndialect = postgres\n")
	writeFile(t, root, "models/staging/stg_orders.sql", "select id as order_id from {{ ref('raw_orders') }}\n")
	bin := t.TempDir()
	fakeBin(t, bin, "dbt")
	fakeBin(t, bin, "sqlfluff")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TQ_FAKE_TOOL", "sleep")
	t.Setenv("TQ_FAKE_VERSION", "1.8.7")
	cfg := packs.Merge(packsOf(t, "data: {tools: [dbt, sqlfluff], timeouts: {dbt: 1s, sqlfluff: 1s}}"), nil)
	in := &packs.Input{Root: root, Base: "HEAD", Config: cfg, Tools: packs.NewToolRunner(root, cfg), Budget: time.Now().Add(time.Minute),
		Changed: []packs.ChangedFile{{Path: "models/staging/stg_orders.sql", Status: 'A'}}}
	in.SetWorkspace("acme", root)
	start := time.Now()
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"data"}})
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("the hung tools were not killed: %v", d)
	}
	got := map[string]packs.Result{}
	for _, r := range rep.Results {
		got[r.ID] = r
	}
	for _, id := range []string{idDBTParse, idSQLFluff} {
		r := got[id]
		if r.Status != packs.Fail || !strings.Contains(r.Detail, "timed out") || r.Tool == nil || !r.Tool.TimedOut {
			t.Errorf("%s: %+v (tool %+v)", id, r, r.Tool)
		}
	}
}
