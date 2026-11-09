package reach

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

// The agent category: the catalog's agent providers except claude, as add and remove take them.
func TestAgentOptions_CatalogMinusClaude(t *testing.T) {
	var ids []string
	for _, o := range AgentOptions() {
		ids = append(ids, o.Value)
	}
	for _, want := range []string{"codex", "gemini", "cursor", "opencode"} {
		if !slices.Contains(ids, want) {
			t.Errorf("%s missing from %v", want, ids)
		}
	}
	if slices.Contains(ids, "claude") {
		t.Errorf("claude is offered: %v", ids)
	}
	if !slices.Equal(Values(CatAgent), sortedCopy(ids)) {
		t.Errorf("Values(agent) = %v", Values(CatAgent))
	}
}

func sortedCopy(xs []string) []string {
	out := slices.Clone(xs)
	slices.Sort(out)
	return out
}

func TestLocate_AgentByValueAlone(t *testing.T) {
	if c, v, ok := Locate("codex"); !ok || c != CatAgent || v != "codex" {
		t.Fatalf("codex: %q %q %v", c, v, ok)
	}
	if _, _, ok := Locate("claude"); ok {
		t.Fatal("claude is located as an agent")
	}
}

func TestChange_AgentAddAndRemove(t *testing.T) {
	cur := current()
	// The category, and the value alone, both add codex; the value is case-folded.
	for _, call := range []struct {
		cat    string
		values []string
	}{{CatAgent, []string{"codex"}}, {CatAgent, []string{"Codex"}}, {"codex", nil}} {
		n, drop, err := Change(cur, true, call.cat, call.values)
		if err != nil || drop != nil || !slices.Equal(n.Agent, []string{"codex"}) {
			t.Fatalf("add %s %v: %+v %v %v", call.cat, call.values, n, drop, err)
		}
	}
	if len(cur.Agent) != 0 {
		t.Fatal("Change changed its input")
	}
	n, _, err := Change(cur, true, CatAgent, []string{"gemini", "cursor"})
	if err != nil || !slices.Equal(n.Agent, []string{"gemini", "cursor"}) {
		t.Fatalf("add two: %v %v", n.Agent, err)
	}
	n, _, err = Change(n, false, CatAgent, []string{"gemini"})
	if err != nil || !slices.Equal(n.Agent, []string{"cursor"}) {
		t.Fatalf("remove gemini: %v %v", n.Agent, err)
	}
	if n, _, err := Change(n, false, "cursor", nil); err != nil || len(n.Agent) != 0 {
		t.Fatalf("remove cursor by value alone: %v %v", n.Agent, err)
	}
	if _, _, err := Change(cur, false, CatAgent, []string{"codex"}); err == nil {
		t.Fatal("removing an agent the client lacks was accepted")
	}
	if _, _, err := Change(cur, true, CatAgent, []string{"claude"}); err == nil {
		t.Fatal("claude was accepted as an agent")
	}
	if _, _, err := Change(cur, true, CatAgent, nil); err == nil {
		t.Fatal("add agent with no value was accepted")
	}
}

// codex is set up with its own folder: the identity the manifest gets, and the scope it puts it in.
func TestAddOptions_AgentInScope(t *testing.T) {
	a := &Answers{Email: "me@acme.test", Agent: []string{"codex"}}
	o, err := a.AddOptions("/base", "acme", "Me")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(o.Identities, "codex") || !slices.Contains(o.ExtraScope, "codex") {
		t.Fatalf("identities %v scope %v", o.Identities, o.ExtraScope)
	}
	if slices.Contains(o.Identities, "gemini") {
		t.Fatal("an agent not chosen is set up")
	}
}

// codex added and removed by Edit: the identity and the scope: entry follow; every other key and
// comment stays, and removing it again gives back the scope the client had.
func TestEdit_AgentAddAndRemove(t *testing.T) {
	before := AnswersFrom(parse(t, []byte(handEdited)))
	if len(before.Agent) != 0 {
		t.Fatalf("agents read from a manifest with none: %v", before.Agent)
	}
	after, _, err := Change(before, true, CatAgent, []string{"codex"})
	if err != nil {
		t.Fatal(err)
	}
	out, changes, added, removed, err := Edit([]byte(handEdited), before, after)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(added, []string{"codex"}) || len(removed) != 0 {
		t.Fatalf("added %v removed %v", added, removed)
	}
	m := parse(t, out)
	if _, ok := m.Identities["codex"]; !ok || !slices.Contains(m.Scope(), "codex") {
		t.Fatalf("identities %v scope %v", m.Identities, m.Scope())
	}
	if len(changes) == 0 || !strings.Contains(strings.Join(changes, "\n"), "team, data, agent and task providers") {
		t.Errorf("changes: %v", changes)
	}
	s := string(out)
	for _, kept := range []string{"# tentaqles workspace manifest", "# the stack this client runs", "# kept by hand", "postgres", "In-house", "Profile 3", "vercel"} {
		if !strings.Contains(s, kept) {
			t.Errorf("%q was lost:\n%s", kept, s)
		}
	}
	// The form's answers read back the way they were written.
	if got := AnswersFrom(parse(t, out)).Agent; !slices.Equal(got, []string{"codex"}) {
		t.Fatalf("read back %v", got)
	}

	// Removed again: the identity and the scope: entry go, and the rest of the manifest is what it was.
	back, _, err := Change(AnswersFrom(parse(t, out)), false, CatAgent, []string{"codex"})
	if err != nil {
		t.Fatal(err)
	}
	out2, _, _, removed2, err := Edit(out, AnswersFrom(parse(t, out)), back)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(removed2, []string{"codex"}) {
		t.Fatalf("removed %v", removed2)
	}
	m2 := parse(t, out2)
	if _, ok := m2.Identities["codex"]; ok || slices.Contains(m2.Scope(), "codex") {
		t.Fatalf("codex still there: %v %v", m2.Identities, m2.Scope())
	}
	if !slices.Equal(m2.Scope(), parse(t, []byte(handEdited)).Scope()) {
		t.Fatalf("scope %v, want %v", m2.Scope(), parse(t, []byte(handEdited)).Scope())
	}
}

// A declined change is not an Edit at all: the answers stay as they were and no bytes change.
func TestEdit_AgentNoChangeIsTheSameBytes(t *testing.T) {
	before := AnswersFrom(parse(t, []byte(handEdited)))
	out, changes, _, _, err := Edit([]byte(handEdited), before, AnswersFrom(parse(t, []byte(handEdited))))
	if err != nil || len(changes) != 0 || !bytes.Equal(out, []byte(handEdited)) {
		t.Fatalf("err=%v changes=%v", err, changes)
	}
}
