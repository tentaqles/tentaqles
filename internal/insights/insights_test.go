package insights

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

var (
	since = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	until = time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
)

func near(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestLookupPrice(t *testing.T) {
	cases := map[string]string{
		"claude-opus-5-5":           "opus-5-5",
		"claude-opus-5[1m]":         "opus",
		"claude-opus-4-7":           "opus",
		"claude-sonnet-5-5":         "sonnet-5",
		"claude-sonnet-4-6":         "sonnet",
		"claude-haiku-5-5":          "haiku-5",
		"claude-haiku-4-5-20251001": "haiku",
		"claude-fable-5-1":          "fable-5-1",
		"claude-fable-5":            "fable",
	}
	for model, match := range cases {
		r, ok := LookupPrice(model)
		if !ok || r.Match != match {
			t.Errorf("%s matched %q (ok=%v), want %q", model, r.Match, ok, match)
		}
	}
	if _, ok := LookupPrice("<synthetic>"); ok {
		t.Error("<synthetic> must not be priced")
	}
	if TierOf("claude-opus-5-5") != "opus" || TierOf("gpt-9") != "" {
		t.Error("TierOf wrong")
	}
}

func TestCostUSD(t *testing.T) {
	u := Usage{Input: 100, Output: 50, CacheRead: 1000, CacheWrite: 400}
	near(t, "opus-5-5", CostUSD("claude-opus-5-5", u), (100*4+50*20+1000*0.2+400*5)/1e6)
	// 1-hour cache writes cost 2x input instead of 1.25x.
	u.CacheDetail = &struct {
		Write1h int64 `json:"ephemeral_1h_input_tokens"`
	}{Write1h: 400}
	near(t, "opus-5-5 1h", CostUSD("claude-opus-5-5", u), (100*4+50*20+1000*0.2+400*8)/1e6)
	near(t, "unknown", CostUSD("mystery", u), 0)
	near(t, "ratio", TierRatio("haiku", "opus"), 0.025)
	near(t, "ratio unknown", TierRatio("x", "opus"), 1)
}

func TestGuardAndAskRules(t *testing.T) {
	got := GuardRules("hook error: BLOCKED [tq/env-file-shell]: x\nBLOCKED [jev/destructive-sql]: y\nBLOCKED [tq/env-file-shell]: again")
	if want := []string{"jev/destructive-sql", "tq/env-file-shell"}; !reflect.DeepEqual(got, want) {
		t.Errorf("GuardRules = %v, want %v", got, want)
	}
	if got := GuardRules("BLOCKED: cwd is not inside a trusted tq workspace (outside any base)"); !reflect.DeepEqual(got, []string{"identity/neutral-remote"}) {
		t.Errorf("identity rule = %v", got)
	}
	if got := GuardRules("BLOCKED: something new"); !reflect.DeepEqual(got, []string{"identity/other"}) {
		t.Errorf("unknown identity rule = %v", got)
	}
	// Brackets that are not rule ids are never captured.
	if got := GuardRules("BLOCKED [some free text here]"); len(got) != 0 {
		t.Errorf("captured non-id: %v", got)
	}
	if got := AskRules(`{"permissionDecisionReason":"CONFIRM [tq/cloud-delete]: a\nCONFIRM [tq/force-push]: b"}`); !reflect.DeepEqual(got, []string{"tq/cloud-delete", "tq/force-push"}) {
		t.Errorf("AskRules = %v", got)
	}
}

func TestClassifyPrompt(t *testing.T) {
	cases := []struct{ prompt, ep, want string }{
		{"Review this change for security vulnerabilities in x", "sdk-py", ClassSecurityReview},
		{"You previously flagged these candidate vulnerabilities", "sdk-py", ClassSecurityReview},
		{"Reply with exactly: ok", "sdk-py", ClassProbe},
		{"do a thing", "sdk-ts", ClassSDKOther},
		{"fix the build", "cli", ClassInteractive},
	}
	for _, c := range cases {
		if got := ClassifyPrompt(c.prompt, c.ep); got != c.want {
			t.Errorf("ClassifyPrompt(%q, %q) = %s, want %s", c.prompt, c.ep, got, c.want)
		}
	}
}

func scanFixture(t *testing.T, rel string) *FileStats {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "projects", rel))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fs, err := ScanTranscript(f, since)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestScanTranscript(t *testing.T) {
	fs := scanFixture(t, filepath.Join("C--proj-a", "sess1.jsonl"))
	if fs.Class != ClassInteractive {
		t.Errorf("class = %s", fs.Class)
	}
	if fs.Model != "claude-opus-5-5" {
		t.Errorf("model = %s", fs.Model)
	}
	// msg_a1 appears twice (one line per content block) and counts once;
	// msg_old is before the window.
	if fs.Messages != 2 {
		t.Errorf("messages = %d, want 2", fs.Messages)
	}
	if want := (Tokens{Input: 110, Output: 70, CacheRead: 451000, CacheWrite: 400}); fs.Tokens != want {
		t.Errorf("tokens = %+v, want %+v", fs.Tokens, want)
	}
	near(t, "cost", fs.CostUSD, 0.0036+0.09044)
	near(t, "recorded", fs.Recorded, 0.09)
	if fs.PeakContext != 450010 {
		t.Errorf("peak = %d", fs.PeakContext)
	}
	if !fs.Start.Equal(time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)) || !fs.End.Equal(time.Date(2026, 10, 1, 11, 0, 4, 0, time.UTC)) {
		t.Errorf("start/end = %v %v", fs.Start, fs.End)
	}
	want := ShellStats{Bash: ToolStats{Calls: 3, Errors: 3}, PowerShell: ToolStats{Calls: 1, Errors: 1}, HeredocEOF: 1, Exit127: 1}
	if fs.Shell != want {
		t.Errorf("shell = %+v, want %+v", fs.Shell, want)
	}
	if !reflect.DeepEqual(fs.Guard.Deny, map[string]int{"tq/env-file-shell": 1, "identity/git-email-drift": 1}) {
		t.Errorf("deny = %v", fs.Guard.Deny)
	}
	if !reflect.DeepEqual(fs.Guard.Ask, map[string]int{"tq/cloud-delete": 1}) {
		t.Errorf("ask = %v", fs.Guard.Ask)
	}
	if fs.Guard.StopBlocks != 1 || fs.Guard.HookErrors["PostToolUse"] != 1 {
		t.Errorf("stop/hook errors = %d %v", fs.Guard.StopBlocks, fs.Guard.HookErrors)
	}
	if !reflect.DeepEqual(fs.Hooks, map[string][]float64{"PreToolUse": {120}, "PostToolUse": {30}}) {
		t.Errorf("hooks = %v", fs.Hooks)
	}

	sub := scanFixture(t, filepath.Join("C--proj-a", "sess1", "subagents", "agent-a1b2.jsonl"))
	if sub.Shell.WorktreeRefusals != 1 || sub.Shell.Bash.Errors != 1 || sub.PeakContext != 0 {
		t.Errorf("subagent = %+v peak %d", sub.Shell, sub.PeakContext)
	}
	rev := scanFixture(t, filepath.Join("C--proj-b", "sess2.jsonl"))
	if rev.Class != ClassSecurityReview {
		t.Errorf("review class = %s", rev.Class)
	}
	old := scanFixture(t, filepath.Join("C--proj-b", "old.jsonl"))
	if !old.Start.IsZero() || old.Messages != 0 {
		t.Errorf("old session counted: %+v", old)
	}
}

func TestReadLineSkipsHugeLines(t *testing.T) {
	old := maxLine
	maxLine = 150
	defer func() { maxLine = old }()
	big := `{"type":"assistant","message":{"id":"x","model":"claude-opus-5-5","usage":{"output_tokens":1},"content":"` + strings.Repeat("a", 200) + `"}}`
	small := `{"type":"assistant","timestamp":"2026-10-01T00:00:00Z","message":{"id":"y","model":"claude-haiku-5-5"}}`
	fs, err := ScanTranscript(strings.NewReader(big+"\n"+small), since)
	if err != nil {
		t.Fatal(err)
	}
	if fs.Messages != 0 || fs.Model != "claude-haiku-5-5" {
		t.Errorf("huge line not skipped: messages=%d model=%s", fs.Messages, fs.Model)
	}
}

// isolateHome points TQ_HOME and the OS home at a temp dir so no test can
// read the developer's real transcripts or decide logs.
func isolateHome(t *testing.T) {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestDiscoverSources(t *testing.T) {
	isolateHome(t)
	home, _ := os.UserHomeDir()
	for _, d := range []string{
		filepath.Join(home, ".tentaqles", "identities", "acme", "claude", "projects"),
		filepath.Join(home, ".tentaqles", "identities", "nogit", "gh"),
		filepath.Join(home, ".claude", "projects"),
	} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var names []string
	for _, s := range DiscoverSources() {
		names = append(names, s.Name)
	}
	if !reflect.DeepEqual(names, []string{"acme", DefaultIdentity}) {
		t.Errorf("sources = %v", names)
	}
}

func fixtureOptions(ws ...string) Options {
	return Options{Since: since, Until: until, Top: 5, Workspaces: ws,
		Sources:   []Source{{Name: "acme", Dir: filepath.Join("testdata", "projects")}},
		DecideDir: filepath.Join("testdata", "decide")}
}

func TestRun(t *testing.T) {
	isolateHome(t)
	rep, err := Run(fixtureOptions("acme"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Identities) != 1 || rep.Identities[0].Name != "acme" {
		t.Fatalf("identities = %+v", rep.Identities)
	}
	tot := rep.Total
	if tot.Sessions != 2 || tot.SessionsByClass[ClassInteractive] != 1 || tot.SessionsByClass[ClassSecurityReview] != 1 || tot.SubagentTranscripts != 1 {
		t.Errorf("sessions = %d %v subagents %d", tot.Sessions, tot.SessionsByClass, tot.SubagentTranscripts)
	}
	near(t, "total cost", tot.CostUSD, 0.09404+0.012+0.0125)
	near(t, "opus cost", tot.CostByTier["opus"], 0.09404+0.0125)
	if tot.SessionsOver400k != 1 || tot.PeakContextMax != 450010 {
		t.Errorf("over400k=%d peak=%d", tot.SessionsOver400k, tot.PeakContextMax)
	}
	if tot.Shell.Bash != (ToolStats{4, 4}) || tot.Shell.WorktreeRefusals != 1 {
		t.Errorf("shell = %+v", tot.Shell)
	}
	near(t, "failure rate", tot.Shell.FailureRate, 1)
	if tot.Reviews != (ReviewReport{Sessions: 1, Opus: 1, CostUSD: 0.0125}) {
		t.Errorf("reviews = %+v", tot.Reviews)
	}
	if h := tot.HookLatency["PreToolUse"]; h.N != 1 || h.P95MS != 120 {
		t.Errorf("hook latency = %+v", h)
	}
	if len(rep.TopByCost) != 2 || rep.TopByCost[0].SessionID != "sess1" || rep.TopByCost[0].Subagents != 1 || rep.TopByCost[0].Project != "C--proj-a" {
		t.Fatalf("top by cost = %+v", rep.TopByCost)
	}
	near(t, "sess1 cost incl subagent", rep.TopByCost[0].CostUSD, 0.09404+0.012)
	if rep.TopByContext[0].PeakContext != 450010 {
		t.Errorf("top by context = %+v", rep.TopByContext[0])
	}

	j := rep.Jev
	if j.Log.Calls != 4 || j.Log.Cached != 1 || j.Log.Errors != 1 || j.Log.P50MS != 100 || j.Log.P95MS != 300 {
		t.Errorf("jev log = %+v", j.Log)
	}
	near(t, "hit rate", j.Log.CacheHitRate, 0.25)
	near(t, "jev cost", j.Log.CostUSD, 0.00003)
	if g := j.Kinds["guard"]; g.Lines != 3 || g.Errors != 1 || g.Modes["enforce"] != 1 {
		t.Errorf("guard kind = %+v", g)
	}
	wantRules := []RuleJudgments{
		{Kind: "guard", Rule: "jev/destructive-sql", Evaluated: 2, WouldAsk: 1, WouldDeny: 1, AppliedAsk: 1, Enforced: true},
		{Kind: "guard", Rule: "jev/rls-weakened", Evaluated: 1},
		{Kind: "stop", Rule: "jev/stop-evidence", Evaluated: 1, WouldAsk: 1},
	}
	if !reflect.DeepEqual(j.Rules, wantRules) {
		t.Errorf("rules = %+v", j.Rules)
	}
	r := j.Route
	if r.Decisions != 3 || r.Cheaper != 2 || r.Applied != 1 || r.Picks["opus->haiku"] != 1 || r.Picks["sonnet->opus"] != 1 {
		t.Errorf("route = %+v", r)
	}
	near(t, "avg subagent", r.AvgSubagentCostUSD, 0.012)
	near(t, "savings", r.EstSavingsUSD, 0.012*0.5+0.012*0.975)
	if j.Triage.Low != 1 || j.Triage.High != 1 || j.Triage.Flags["auth"] != 1 {
		t.Errorf("triage = %+v", j.Triage)
	}

	st := map[string]string{}
	for _, tg := range rep.Targets {
		st[tg.ID] = tg.Status
	}
	want := map[string]string{"sessions_over_400k": "not met", "stop_hook_blocks_per_30d": "not met", "exit_127_per_30d": "not met",
		"shell_failure_rate": "not met", "review_sessions_per_30d": "met", "jev_enforce_precision": "check"}
	if !reflect.DeepEqual(st, want) {
		t.Errorf("targets = %v", st)
	}
}

func TestRunFilterExcludesOtherIdentities(t *testing.T) {
	isolateHome(t)
	rep, err := Run(fixtureOptions("nobody"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total.Sessions != 0 || rep.Jev.Log.Calls != 0 || len(rep.Identities) != 0 {
		t.Errorf("filter leaked: sessions=%d jev=%d", rep.Total.Sessions, rep.Jev.Log.Calls)
	}
	// No filter: the other workspace's Jev lines count too.
	rep, err = Run(fixtureOptions())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Jev.Log.Calls != 5 {
		t.Errorf("unfiltered jev calls = %d", rep.Jev.Log.Calls)
	}
}

// TestJSONSchemaStable pins the top-level and identity keys of --json.
func TestJSONSchemaStable(t *testing.T) {
	isolateHome(t)
	rep, err := Run(fixtureOptions("acme"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, rep); err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	keys := func(m map[string]json.RawMessage) []string {
		var ks []string
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return ks
	}
	if got, want := keys(m), []string{"generated_at", "identities", "jev", "schema", "since", "targets", "top_by_context", "top_by_cost", "total", "until", "window_days", "worktrees"}; !reflect.DeepEqual(got, want) {
		t.Errorf("top-level keys = %v", got)
	}
	var tot map[string]json.RawMessage
	_ = json.Unmarshal(m["total"], &tot)
	if got, want := keys(tot), []string{"cost_by_tier", "cost_usd", "guard", "hook_latency", "name", "peak_context_max", "recorded_cost_usd", "reviews", "sessions", "sessions_by_class", "sessions_over_400k", "shell", "subagent_transcripts", "tokens"}; !reflect.DeepEqual(got, want) {
		t.Errorf("identity keys = %v", got)
	}
	if !strings.Contains(string(m["schema"]), SchemaVersion) {
		t.Errorf("schema = %s", m["schema"])
	}
}

func TestWriteTextHasNoContent(t *testing.T) {
	isolateHome(t)
	rep, err := Run(fixtureOptions("acme"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	WriteText(&buf, rep)
	out := buf.String()
	for _, s := range []string{"30-day targets", "tq/env-file-shell 1", "sess1", "jev/destructive-sql"} {
		if !strings.Contains(out, s) {
			t.Errorf("text output lacks %q", s)
		}
	}
	// Transcript content (prompts, tool output) must never be printed.
	for _, s := range []string{"Please fix the build", "frobnicate", "Review this change", "reading dotenv files"} {
		if strings.Contains(out, s) {
			t.Errorf("text output leaks transcript content %q", s)
		}
	}
}

func TestTargetsNormalisePer30Days(t *testing.T) {
	r := &Report{WindowDays: 60}
	r.Total.Guard.StopBlocks = 6 // 3 per 30d: exactly "near zero"
	r.Total.Reviews.Sessions = 60
	got := map[string]Target{}
	for _, tg := range Targets(r) {
		got[tg.ID] = tg
	}
	if g := got["stop_hook_blocks_per_30d"]; g.Current != 3 || g.Status != "met" {
		t.Errorf("stop target = %+v", g)
	}
	if g := got["review_sessions_per_30d"]; g.Current != 30 || g.Status != "not met" {
		t.Errorf("review target = %+v", g)
	}
	if g := got["jev_enforce_precision"]; g.Status != "met" {
		t.Errorf("jev target = %+v", g)
	}
}

func TestPercentile(t *testing.T) {
	if Percentile(nil, 95) != 0 {
		t.Error("empty")
	}
	xs := []float64{5, 1, 4, 2, 3}
	if Percentile(xs, 50) != 3 || Percentile(xs, 95) != 5 || Percentile(xs, 0) != 1 {
		t.Errorf("percentiles wrong")
	}
}
