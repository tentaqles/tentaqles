package packs

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
)

var bqUnbounded = regexp.MustCompile(`\bbq\s+query\b`)

func dataEnv(t testing.TB) GuardEnv {
	return envFor(t, "data: { enabled: true }\nml: { enabled: true }\n")
}

// envFor is a hook env with a trusted manifest holding the packs block.
func envFor(t testing.TB, packsBlock string) GuardEnv {
	m := &manifest.Manifest{Packs: packsOf(t, packsBlock)}
	return GuardEnv{Config: Merge(m.Packs, nil), Manifest: m, Workspace: "acme"}
}

func outcome(os []GuardOutcome, id string) *GuardOutcome {
	for i := range os {
		if os[i].ID == id {
			return &os[i]
		}
	}
	return nil
}

func bash(cmd string) policy.ToolCall { return policy.ToolCall{Tool: "Bash", Command: cmd} }

func TestGuardDecisions(t *testing.T) {
	isolate(t)
	env := dataEnv(t)
	o := outcome(EvaluateGuards(bash("bq query 'select 1'"), env, nil, nil), "tq/example-bq")
	if o == nil || o.Action != policy.Deny || !o.Handled || len(o.Supersedes) != 1 {
		t.Fatalf("deny: %+v", o)
	}
	o = outcome(EvaluateGuards(bash("ls"), env, nil, nil), "tq/example-bq")
	if o.Action != policy.Allow || o.Handled || o.Supersedes != nil {
		t.Fatalf("allow, not handled: %+v", o)
	}
	// guard.disable and guard.actions apply to pack guard ids.
	if o := outcome(EvaluateGuards(bash("bq query x"), env, func(id string) bool { return id == "tq/example-bq" }, nil), "tq/example-bq"); o != nil {
		t.Fatal("disabled guard ran")
	}
	o = outcome(EvaluateGuards(bash("bq query x"), env, nil, func(id string, a policy.Action) policy.Action { return policy.Ask }), "tq/example-bq")
	if o.Action != policy.Ask {
		t.Fatalf("actions override: %+v", o)
	}
	// Events: the Bash guard skips Edit; the MCP regex guard matches its tool.
	if o := outcome(EvaluateGuards(policy.ToolCall{Tool: "Edit"}, env, nil, nil), "tq/example-bq"); o != nil {
		t.Fatal("event filter")
	}
	if o := outcome(EvaluateGuards(policy.ToolCall{Tool: "mcp__mlflow__promote"}, env, nil, nil), "tq/example-mcp"); o == nil || o.Action != policy.Deny {
		t.Fatalf("mcp event: %+v", o)
	}
	// A disabled pack's guards never run.
	off := envFor(t, "data: { enabled: false }\n")
	if os := EvaluateGuards(bash("bq query x"), off, nil, nil); len(os) != 0 {
		t.Fatalf("disabled pack: %+v", os)
	}
}

func TestGuardPanicMeansDeny(t *testing.T) {
	isolate(t)
	o := outcome(EvaluateGuards(bash("guard-panic"), dataEnv(t), nil, func(string, policy.Action) policy.Action { return policy.Ask }), "tq/example-bq")
	if o == nil || o.Action != policy.Deny || !o.Failed || !strings.Contains(o.Reason, "guard exploded") {
		t.Fatalf("panic must deny (and guard.actions cannot soften it): %+v", o)
	}
}

func TestGuardBudgetMeansDeny(t *testing.T) {
	isolate(t)
	o := outcome(EvaluateGuards(bash("guard-slow"), dataEnv(t), nil, nil), "tq/example-bq")
	if o == nil || o.Action != policy.Deny || !o.Failed || !strings.Contains(o.Reason, "budget") {
		t.Fatalf("slow guard must deny: %+v", o)
	}
}

func TestGuardConsumeOnlyOnAllow(t *testing.T) {
	isolate(t)
	o := outcome(EvaluateGuards(bash("guard-consume"), dataEnv(t), nil, nil), "tq/example-bq")
	if o.Action != policy.Allow || len(o.Consume) != 1 || o.Consume[0] != "d-example" || len(o.Counters) != 1 {
		t.Fatalf("consume: %+v", o)
	}
	o = outcome(EvaluateGuards(bash("guard-ask-consume"), dataEnv(t), nil, nil), "tq/example-bq")
	if o.Action != policy.Ask || o.Consume != nil {
		t.Fatalf("an ask never consumes: %+v", o)
	}
}

func TestReadStateBounds(t *testing.T) {
	isolate(t)
	g := NewGuardCtx(dataEnv(t), "data")
	dir := StateDir("acme", "data")
	os.WriteFile(filepath.Join(dir, "ledger.jsonl"), []byte("x\n"), 0o600)
	if b, err := g.ReadState("ledger.jsonl"); err != nil || string(b) != "x\n" {
		t.Fatalf("%q %v", b, err)
	}
	big := make([]byte, ReadLimit+1)
	os.WriteFile(filepath.Join(dir, "big"), big, 0o600)
	if _, err := g.ReadState("big"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over 1 MiB: %v", err)
	}
	for _, bad := range []string{"../other/ledger.jsonl", "/etc/passwd", `C:\x`} {
		if _, err := g.ReadState(bad); !errors.Is(err, ErrRefused) {
			t.Fatalf("%s: %v", bad, err)
		}
	}
	if _, err := g.ReadState("missing"); err == nil {
		t.Fatal("missing file must error")
	}
	if NewGuardCtx(GuardEnv{}, "data").StateDir() != "" {
		t.Fatal("no workspace: no state dir")
	}
	prev := readBudget
	readBudget = -1
	defer func() { readBudget = prev }()
	if _, err := g.ReadState("ledger.jsonl"); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("blown read budget: %v", err)
	}
}

func TestReadLocalRefusals(t *testing.T) {
	home := isolate(t)
	ws := filepath.Join(home, "acme")
	writeFile(t, ws, ".terraform/environment", "prod")
	writeFile(t, ws, "supabase/.temp/project-ref", "abc")
	writeFile(t, ws, ".env", "SECRET=1")
	writeFile(t, ws, "app/.env.prod", "SECRET=1")
	writeFile(t, home, ".aws/credentials", "[default]")
	writeFile(t, home, ".azure/azureProfile.json", "{}")
	writeFile(t, home, "outside/file.txt", "no")
	env := dataEnv(t)
	env.WorkspaceRoot, env.RepoRoot = ws, ws
	g := NewGuardCtx(env, "cloud")
	if b, err := g.ReadLocal(".terraform/environment", 0); err != nil || string(b) != "prod" {
		t.Fatalf("%q %v", b, err)
	}
	if _, err := g.ReadLocal("supabase/.temp/project-ref", 64); err != nil {
		t.Fatal(err)
	}
	if _, err := g.ReadLocal(filepath.Join(home, ".azure", "azureProfile.json"), 0); err != nil {
		t.Fatalf("user config dir: %v", err)
	}
	for _, bad := range []string{".env", "app/.env.prod", "../outside/file.txt", filepath.Join(home, ".aws", "credentials"), filepath.Join(home, "outside", "file.txt")} {
		if _, err := g.ReadLocal(bad, 0); !errors.Is(err, ErrRefused) {
			t.Fatalf("%s must be refused, got %v", bad, err)
		}
	}
	if _, err := g.ReadLocal(".terraform/environment", 2); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("max: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(home, "outside", "file.txt"), filepath.Join(ws, "link")); err == nil {
			if _, err := g.ReadLocal("link", 0); !errors.Is(err, ErrRefused) {
				t.Fatalf("symlink out of the roots: %v", err)
			}
		}
	}
}

func TestHashStateBounds(t *testing.T) {
	isolate(t)
	g := NewGuardCtx(dataEnv(t), "data")
	dir := StateDir("acme", "data")
	os.WriteFile(filepath.Join(dir, "plan"), []byte("abc"), 0o600)
	h, err := g.HashState("plan")
	if err != nil || h != HashBytes([]byte("abc")) {
		t.Fatalf("%s %v", h, err)
	}
	f, _ := os.Create(filepath.Join(dir, "huge"))
	f.Truncate(HashLimit + 1)
	f.Close()
	if _, err := g.HashState("huge"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over 64 MiB: %v", err)
	}
	prev := hashBudget
	hashBudget = -1
	defer func() { hashBudget = prev }()
	if _, err := g.HashState("plan"); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("over 40 ms: %v", err)
	}
}

// command64K is a 64 KiB shell command, the bound's worst case.
func command64K() string {
	return "bq query " + strings.Repeat("x", 64<<10-len("bq query "))
}

// TestGuardUnder50ms checks the bound the benchmark measures: the median
// of several runs of every enabled guard over a 64 KiB command.
func TestGuardUnder50ms(t *testing.T) {
	isolate(t)
	env := dataEnv(t)
	call := bash(command64K())
	var ds []time.Duration
	for i := 0; i < 9; i++ {
		start := time.Now()
		EvaluateGuards(call, env, nil, nil)
		ds = append(ds, time.Since(start))
	}
	sortDurations(ds)
	if ds[len(ds)/2] > GuardBudget {
		t.Fatalf("median %v over %v", ds[len(ds)/2], GuardBudget)
	}
}

func sortDurations(ds []time.Duration) {
	for i := range ds {
		for j := i + 1; j < len(ds); j++ {
			if ds[j] < ds[i] {
				ds[i], ds[j] = ds[j], ds[i]
			}
		}
	}
}

func BenchmarkGuards64KiB(b *testing.B) {
	isolate(b)
	env := dataEnv(b)
	call := bash(command64K())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EvaluateGuards(call, env, nil, nil)
	}
}

func BenchmarkHashState1MiB(b *testing.B) {
	isolate(b)
	g := NewGuardCtx(dataEnv(b), "data")
	os.WriteFile(filepath.Join(StateDir("acme", "data"), "plan"), make([]byte, 1<<20), 0o600)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := g.HashState("plan"); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRegisterGuardPanics(t *testing.T) {
	for name, g := range map[string]Guard{
		"bad id":    {ID: "example", Pack: "data", Check: func(policy.ToolCall, GuardCtx) (policy.Action, string, bool) { return "", "", false }},
		"bad pack":  {ID: "tq/x", Pack: "web", Check: func(policy.ToolCall, GuardCtx) (policy.Action, string, bool) { return "", "", false }},
		"nil check": {ID: "tq/x", Pack: "data"},
		"duplicate": {ID: "tq/example-bq", Pack: "data", Check: func(policy.ToolCall, GuardCtx) (policy.Action, string, bool) { return "", "", false }},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("no panic")
				}
			}()
			RegisterGuard(g)
		})
	}
}
