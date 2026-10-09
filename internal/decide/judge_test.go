package decide

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJudgePrefilter(t *testing.T) {
	set := BuildJudgeSet(nil, nil)
	if len(set.Errors) > 0 {
		t.Fatal(set.Errors)
	}
	ids := func(s Subject) string {
		var out []string
		for _, r := range set.Candidates(s) {
			out = append(out, r.ID)
		}
		return strings.Join(out, ",")
	}
	cases := []struct {
		sub  Subject
		want string
	}{
		{Subject{Tool: "Bash", Command: "git stash drop"}, ""},
		{Subject{Tool: "Bash", Command: `psql -c "DROP TABLE users"`}, "jev/destructive-sql"},
		{Subject{Tool: "Edit", Path: "db/m/1.sql", Content: "DELETE FROM invoices;"}, "jev/destructive-sql"},
		{Subject{Tool: "Read", Path: "db/m/1.sql"}, ""},
		{Subject{Tool: "Write", Path: "src/App.tsx", Content: "createClient(url, SERVICE_ROLE_KEY)"}, "jev/service-role-client"},
		{Subject{Tool: "Write", Path: "server/app.py", Content: "SERVICE_ROLE_KEY = os.environ[...]"}, ""},
		{Subject{Tool: "mcp__n8n__get_workflow_details", Content: `{"token":"x"}`}, ""},
		{Subject{Tool: "mcp__n8n__update_workflow", Content: `{"headers":{"Authorization":"Bearer x"}}`}, "jev/n8n-inline-credentials"},
		{Subject{Tool: "Edit", Path: "supabase/m.sql", Content: "CREATE POLICY p ON t USING (true)"}, "jev/rls-weakened"},
	}
	for _, c := range cases {
		if got := ids(c.sub); got != c.want {
			t.Errorf("%+v: got %q want %q", c.sub, got, c.want)
		}
	}
	if n := len(BuildJudgeSet(nil, []string{"jev/destructive-sql"}).Candidates(cases[1].sub)); n != 0 {
		t.Error("disabled rule still matched")
	}
	if bad := BuildJudgeSet([]JudgeRule{{ID: "x", Tool: "(", Question: "?"}}, nil); len(bad.Errors) != 1 {
		t.Error("broken rule not reported")
	}
}

func TestJudgeModesAndCap(t *testing.T) {
	f := newFake(t, answerAll(0.95))
	set := BuildJudgeSet([]JudgeRule{{ID: "x/deny-ok", Tool: "Edit", Question: "?", Max: Deny}}, nil)
	sub := Subject{Tool: "Edit", Path: "db/1.sql", Content: "DROP TABLE t;"}
	rules := set.Candidates(sub)
	if len(rules) != 2 {
		t.Fatalf("candidates = %d", len(rules))
	}
	apply := func(p Policy) map[string]Action {
		out := map[string]Action{}
		for _, v := range Judge(context.Background(), client(f), p, sub, rules) {
			out[v.Rule.ID] = v.Escalation.Apply
		}
		return out
	}
	if got := apply(Policy{Backend: "typesafe"}); got["jev/destructive-sql"] != None || got["x/deny-ok"] != None {
		t.Fatalf("shadow applied: %v", got)
	}
	got := apply(Policy{Backend: "typesafe", Mode: "enforce"})
	if got["jev/destructive-sql"] != Ask || got["x/deny-ok"] != Deny {
		t.Fatalf("enforce: %v (built-ins are capped at ask)", got)
	}
	down := newFake(t, func(w http.ResponseWriter, _ map[string]any) { w.WriteHeader(503) })
	for _, v := range Judge(context.Background(), client(down), Policy{Backend: "typesafe", Mode: "enforce"}, sub, rules) {
		if v.Escalation.Apply != None || v.Escalation.Err == nil {
			t.Fatalf("outage must apply nothing: %+v", v.Escalation)
		}
	}
	j := VerdictsJudgment("acme", "enforce", sub, Judge(context.Background(), client(f), Policy{Backend: "typesafe", Mode: "enforce"}, sub, rules))
	raw, _ := json.Marshal(j)
	if strings.Contains(string(raw), "DROP TABLE") {
		t.Fatal("judgment log holds content")
	}
}

func TestBreaker(t *testing.T) {
	b := Breaker{Path: filepath.Join(t.TempDir(), "b"), CoolDown: time.Minute}
	if b.Open() {
		t.Fatal("fresh breaker open")
	}
	b.Trip()
	if !b.Open() {
		t.Fatal("tripped breaker closed")
	}
	b.Reset()
	if b.Open() {
		t.Fatal("reset breaker open")
	}
}

func choiceFake(t *testing.T, pick string, conf float64) *fake {
	return newFake(t, func(w http.ResponseWriter, _ map[string]any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"tier": map[string]any{"choice": pick, "confidence": conf}}})
	})
}

func TestRouteGuardrails(t *testing.T) {
	enforce := Policy{Backend: "typesafe", Mode: "enforce"}
	in := func(kv ...string) map[string]any {
		m := map[string]any{"prompt": "find every API route", "description": "find routes"}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	cases := []struct {
		name   string
		input  map[string]any
		parent string
		pick   string
		conf   float64
		pol    Policy
		apply  bool
	}{
		{"cheaper and confident", in(), "opus", "haiku", 0.9, enforce, true},
		{"shadow never applies", in(), "opus", "haiku", 0.9, Policy{Backend: "typesafe"}, false},
		{"not cheaper", in(), "sonnet", "opus", 0.95, enforce, false},
		{"same tier", in(), "sonnet", "sonnet", 0.95, enforce, false},
		{"low confidence", in(), "opus", "sonnet", 0.6, enforce, false},
		{"explicit model", in("model", "opus"), "opus", "haiku", 0.95, enforce, false},
		{"typed agent", in("subagent_type", "Explore"), "opus", "haiku", 0.95, enforce, false},
		{"unknown parent", in(), "", "haiku", 0.95, enforce, false},
	}
	for _, c := range cases {
		f := choiceFake(t, c.pick, c.conf)
		plan := Route(context.Background(), client(f), c.pol, c.input, c.parent)
		if plan.Apply != c.apply {
			t.Errorf("%s: apply=%v want %v (%+v)", c.name, plan.Apply, c.apply, plan)
		}
		if !plan.Eligible && f.calls.Load() != 0 {
			t.Errorf("%s: ineligible launch still called Jev", c.name)
		}
	}
	bad := choiceFake(t, "gpt", 0.99)
	if p := Route(context.Background(), client(bad), enforce, in(), "opus"); p.Apply || p.Err == nil {
		t.Errorf("unknown tier must not apply: %+v", p)
	}
}

func TestParentTier(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(p, []byte(strings.Join([]string{
		`{"type":"assistant","message":{"model":"claude-sonnet-5-5"}}`,
		`{"type":"user","message":{"content":"model opus please"}}`,
		`{"type":"assistant","message":{"model":"claude-opus-5-5"}}`,
	}, "\n")), 0o600)
	if got := ParentTier(p); got != "opus" {
		t.Fatalf("got %q", got)
	}
	if ParentTier("") != "" || ParentTier(filepath.Join(t.TempDir(), "none")) != "" {
		t.Fatal("missing transcript should be unknown")
	}
}

func TestTriage(t *testing.T) {
	pol := Policy{Backend: "typesafe"}
	low := newFake(t, answerAll(0.1))
	if r := Triage(context.Background(), client(low), pol, "+fix typo"); r.Risk != "low" || len(r.Flags) != 0 {
		t.Fatalf("low: %+v", r)
	}
	if r := Triage(context.Background(), client(low), pol, strings.Repeat("+x\n", 40_000)); r.Risk != "high" || !r.Truncated {
		t.Fatalf("truncated diff must be high: %+v", r)
	}
	flagged := newFake(t, func(w http.ResponseWriter, body map[string]any) {
		ans := map[string]any{}
		for id := range body["questions"].(map[string]any) {
			p := 0.1
			if id == "migration" {
				p = 0.8
			}
			ans[id] = map[string]any{"noul": p}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": ans})
	})
	if r := Triage(context.Background(), client(flagged), pol, "+ALTER TABLE"); r.Risk != "high" || strings.Join(r.Flags, ",") != "migration" {
		t.Fatalf("flagged: %+v", r)
	}
	down := newFake(t, func(w http.ResponseWriter, _ map[string]any) { w.WriteHeader(500) })
	if r := Triage(context.Background(), client(down), pol, "+x"); r.Risk != "high" || r.Error == "" {
		t.Fatalf("outage must be high: %+v", r)
	}
}
