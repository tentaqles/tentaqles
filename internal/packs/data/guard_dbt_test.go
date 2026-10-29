package data

import (
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/policy"
)

func guardEnv(t *testing.T, repo, cfg string) packs.GuardEnv {
	t.Helper()
	return packs.GuardEnv{Config: packs.Merge(packsOf(t, cfg), nil), Workspace: "acme", WorkspaceRoot: repo, RepoRoot: repo}
}

func outcome(t *testing.T, env packs.GuardEnv, tool, cmd string, id string) packs.GuardOutcome {
	t.Helper()
	for _, o := range packs.EvaluateGuards(policy.ToolCall{Tool: tool, Command: cmd}, env, nil, nil) {
		if o.ID == id {
			return o
		}
	}
	return packs.GuardOutcome{ID: id, Action: policy.Allow}
}

func TestDBTProdTargetGuard(t *testing.T) {
	isolate(t)
	repo := t.TempDir()
	env := guardEnv(t, repo, "data: {enabled: true}")
	cases := []struct {
		tool, cmd string
		want      policy.Action
	}{
		{"Bash", "dbt run --target prod", policy.Deny},
		{"Bash", "dbt build -t production --select fct_orders", policy.Deny},
		{"Bash", "dbt seed --target=PROD", policy.Deny},
		{"Bash", "dbt --debug snapshot --target prod", policy.Deny},
		{"Bash", "dbt run-operation grant_all --target prod", policy.Deny},
		{"Bash", "dbt test --target prod", policy.Deny},
		{"Bash", "dbt clone --state artifacts --target prod", policy.Deny},
		{"Bash", "cd analytics && uv run dbt run --target prod", policy.Deny},
		{"Bash", "python -m dbt run --target prod", policy.Deny},
		{"Bash", "uv run --with dbt-postgres dbt run -t prod", policy.Deny},
		{"Bash", "dbt run -s fct_orders --target prod --full-refresh", policy.Deny},
		{"Bash", "DBT_TARGET=prod dbt run", policy.Deny},
		{"PowerShell", `$env:DBT_TARGET = "prod"; dbt run --select x`, policy.Deny},
		{"PowerShell", `& "C:\tools\dbt\dbt.exe" build --target prod`, policy.Deny},
		{"PowerShell", `.venv\Scripts\dbt.exe run --target prod`, policy.Deny},
		{"Bash", `bash -c "dbt run --target prod"`, policy.Deny},
		{"PowerShell", `pwsh -Command 'dbt build --target prod'`, policy.Deny},
		{"Bash", "dbt run --target dev", policy.Allow},
		{"Bash", "dbt compile --target prod", policy.Allow},
		{"Bash", "dbt parse --target prod", policy.Allow},
		{"Bash", "dbt docs generate --target prod", policy.Allow},
		{"Bash", "dbt run", policy.Allow},
		{"Bash", `echo "dbt run --target prod is denied"`, policy.Allow},
		{"Bash", `git commit -m "dbt run --target prod"`, policy.Allow},
		{"Bash", "ls models/prod", policy.Allow},
		{"Bash", "dbtx run --target prod", policy.Allow},
	}
	for _, c := range cases {
		o := outcome(t, env, c.tool, c.cmd, guardProdTarget)
		if o.Action != c.want {
			t.Errorf("%s %q: got %q (%s), want %q", c.tool, c.cmd, o.Action, o.Reason, c.want)
		}
		if c.want == policy.Deny && (!o.Handled || !strings.Contains(o.Reason, "--target dev")) {
			t.Errorf("%q: deny is handled and names the dev target: %+v", c.cmd, o)
		}
	}
}

func TestDBTProdTargetConfig(t *testing.T) {
	isolate(t)
	repo := t.TempDir()
	env := guardEnv(t, repo, "data: {dbt: {prod_targets: [live], dev_target: sandbox}}")
	if o := outcome(t, env, "Bash", "dbt run --target live", guardProdTarget); o.Action != policy.Deny || !strings.Contains(o.Reason, "--target sandbox") {
		t.Fatalf("configured prod target: %+v", o)
	}
	if o := outcome(t, env, "Bash", "dbt run --target prod", guardProdTarget); o.Action != policy.Allow {
		t.Fatalf("prod is not a prod target here: %+v", o)
	}
	// disabled pack: no guard
	env = guardEnv(t, repo, "data: {enabled: false}")
	if o := outcome(t, env, "Bash", "dbt run --target prod", guardProdTarget); o.Action != policy.Allow {
		t.Fatalf("disabled pack: %+v", o)
	}
	// guard.disable and guard.actions layers
	env = guardEnv(t, repo, "data: {enabled: true}")
	off := func(id string) bool { return id == guardProdTarget }
	for _, o := range packs.EvaluateGuards(policy.ToolCall{Tool: "Bash", Command: "dbt run --target prod"}, env, off, nil) {
		if o.ID == guardProdTarget {
			t.Fatalf("disabled guard ran: %+v", o)
		}
	}
	ask := func(id string, a policy.Action) policy.Action { return policy.Ask }
	for _, o := range packs.EvaluateGuards(policy.ToolCall{Tool: "Bash", Command: "dbt run --target prod"}, env, nil, ask) {
		if o.ID == guardProdTarget && o.Action != policy.Ask {
			t.Fatalf("guard.actions: %+v", o)
		}
	}
}

func TestDBTProdTargetDefaultFromProfiles(t *testing.T) {
	isolate(t)
	repo := t.TempDir()
	writeFile(t, repo, "analytics/dbt_project.yml", "name: acme\nprofile: acme\n")
	writeFile(t, repo, "analytics/profiles.yml", "acme:\n  target: prod\n  outputs:\n    prod: {type: postgres}\n    dev: {type: postgres}\n")
	env := guardEnv(t, repo, "data: {dbt: {project: analytics}}")
	if o := outcome(t, env, "Bash", "dbt run", guardProdTarget); o.Action != policy.Deny || !strings.Contains(o.Reason, `"prod"`) {
		t.Fatalf("default target prod: %+v", o)
	}
	if o := outcome(t, env, "Bash", "dbt run --target dev", guardProdTarget); o.Action != policy.Allow {
		t.Fatalf("explicit dev: %+v", o)
	}
	writeFile(t, repo, "analytics/profiles.yml", "acme:\n  target: dev\n  outputs:\n    dev: {type: postgres}\n")
	if o := outcome(t, env, "Bash", "dbt run", guardProdTarget); o.Action != policy.Allow {
		t.Fatalf("default dev: %+v", o)
	}
	// --profiles-dir outside the readable roots: refused read, deny
	outside := t.TempDir()
	writeFile(t, outside, "profiles.yml", "acme:\n  target: dev\n")
	if o := outcome(t, env, "Bash", "dbt run --profiles-dir "+outside, guardProdTarget); o.Action != policy.Deny || !strings.Contains(o.Reason, "cannot read") {
		t.Fatalf("refused profiles read: %+v", o)
	}
	// .env-style names are never read
	if o := outcome(t, env, "Bash", "dbt run --profiles-dir .env.d", guardProdTarget); o.Action != policy.Deny {
		t.Fatalf(".env read: %+v", o)
	}
}

func TestDBTFullRefreshGuard(t *testing.T) {
	isolate(t)
	env := guardEnv(t, t.TempDir(), "data: {enabled: true}")
	for _, c := range []string{"dbt run --full-refresh --target dev", "dbt build -f -s fct_orders", "DBT_FULL_REFRESH=true dbt run", "dbt seed --full-refresh"} {
		o := outcome(t, env, "Bash", c, guardFullRefresh)
		if o.Action != policy.Ask || !o.Handled || !strings.Contains(o.Reason, "drops and rebuilds incremental tables") {
			t.Errorf("%q: %+v", c, o)
		}
		if len(o.Counters) != 1 || o.Counters[0].Name != "dbt_full_refresh:asked" {
			t.Errorf("%q counters: %+v", c, o.Counters)
		}
	}
	for _, c := range []string{"dbt run --target dev", "dbt ls -s x", "grep -f patterns.txt"} {
		if o := outcome(t, env, "Bash", c, guardFullRefresh); o.Action != policy.Allow {
			t.Errorf("%q: %+v", c, o)
		}
	}
	// prod + full refresh: both guards speak; the deny is the strongest
	if o := outcome(t, env, "Bash", "dbt run --full-refresh --target prod", guardProdTarget); o.Action != policy.Deny {
		t.Errorf("prod full refresh: %+v", o)
	}
}

func TestDBTGuardsUnder50ms(t *testing.T) {
	isolate(t)
	repo := t.TempDir()
	writeFile(t, repo, "profiles.yml", "acme:\n  target: dev\n")
	env := guardEnv(t, repo, "data: {enabled: true}")
	cmd := strings.Repeat("echo dbt run --target dev 'quoted text' ; ", 64<<10/40)[:64<<10] + " dbt run"
	for _, id := range []string{guardProdTarget, guardFullRefresh} {
		var durs []time.Duration
		for i := 0; i < 9; i++ {
			start := time.Now()
			o := outcome(t, env, "Bash", cmd, id)
			durs = append(durs, time.Since(start))
			if o.Failed {
				t.Fatalf("%s failed: %+v", id, o)
			}
		}
		med := durs[len(durs)/2]
		if med > 50*time.Millisecond {
			t.Fatalf("%s median %v on a 64 KiB command", id, med)
		}
	}
}

func BenchmarkDBTGuards64KiB(b *testing.B) {
	isolate(b)
	cmd := strings.Repeat("dbt run --target dev -s x; ", 64<<10/27)
	g := packs.NewGuardCtx(packs.GuardEnv{Config: packs.Merge(packsOf(b, "data: {enabled: true}"), nil), RepoRoot: b.TempDir()}, "data")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checkProdTarget(policy.ToolCall{Tool: "Bash", Command: cmd}, g)
		checkFullRefresh(policy.ToolCall{Tool: "Bash", Command: cmd}, g)
	}
}

func TestShellSegments(t *testing.T) {
	segs := shellSegments(`A=1 dbt run --vars '{"k": 1}' && echo "x;y" | tee (out)`)
	var got []string
	for _, s := range segs {
		var ws []string
		for _, w := range s {
			ws = append(ws, w.text)
		}
		got = append(got, strings.Join(ws, ","))
	}
	want := []string{`A=1,dbt,run,--vars,{"k": 1}`, "echo,x;y", "tee", "out"}
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Fatalf("got %q", got)
	}
}
