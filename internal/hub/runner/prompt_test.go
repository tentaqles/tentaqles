package runner

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestPromptPerKind(t *testing.T) {
	card := func(kind string) store.Card {
		return store.Card{ID: "c-abcdefghijkl", Title: "Login fails", Kind: kind, Body: "---\nbudget: usd=3\n---\nSteps: open /login."}
	}
	cases := map[string]string{
		"project":  "/tentaqles:kickoff Login fails\n\nSteps: open /login. --headless",
		"feature":  "/tentaqles:feature Login fails\n\nSteps: open /login. --headless",
		"analysis": "/tentaqles:feature Login fails\n\nSteps: open /login. --headless",
		"chore":    "/tentaqles:feature Login fails\n\nSteps: open /login. --headless",
		"fix":      "/tentaqles:fix Login fails\n\nSteps: open /login. --headless",
		"hotfix":   "/tentaqles:fix Login fails\n\nSteps: open /login. --hotfix --headless",
	}
	for kind, want := range cases {
		c := card(kind)
		r := store.Run{OnRamp: OnRampFor(kind), Phase: store.PhaseStart}
		if got := Prompt(r, c, Notes{}); got != want {
			t.Errorf("%s:\n got %q\nwant %q", kind, got, want)
		}
	}
	relocated := store.Run{OnRamp: store.OnRampFeature, Phase: store.PhaseRelocated, Slug: "login"}
	if got := Prompt(relocated, card("feature"), Notes{}); got != cases["feature"] {
		t.Errorf("after the hand-off the on-ramp runs again in the worktree: %q", got)
	}
	resume := store.Run{OnRamp: store.OnRampResume, Phase: store.PhaseResume, Slug: "login"}
	if got := Prompt(resume, card("fix"), Notes{}); got != "/tentaqles:build resume login --headless" {
		t.Errorf("resume: %q", got)
	}
	noSlug := store.Run{OnRamp: store.OnRampResume, Phase: store.PhaseResume}
	if got := Prompt(noSlug, card("fix"), Notes{}); got != cases["fix"] {
		t.Errorf("resume without a slug falls back to the on-ramp: %q", got)
	}
}

func human(id, at, body string) store.Comment {
	return store.Comment{ID: id, AuthorKind: store.AuthorHuman, Author: "tester", Body: body, CreatedAt: at}
}

const notesHead = "\n\n## Notes from the human on this card\n\n" +
	"The human posted these comments on the hub card since the run's previous round, oldest first. " +
	"They are the human's own notes on this task: answers to questions you asked and guidance to follow.\n\n"

// TestPromptCarriesTheHumansNotes pins the exact text with one note on an on-ramp and on the
// resume form.
func TestPromptCarriesTheHumansNotes(t *testing.T) {
	c := store.Card{ID: "c-abcdefghijkl", Title: "Login fails", Kind: "feature", Body: "Steps: open /login."}
	notes := PromptNotes([]store.Comment{human("m1", "2026-10-27T10:00:00Z", "Use SQLite, not Postgres.")}, nil)
	entry := "### tester at 2026-10-27T10:00:00Z\n\nUse SQLite, not Postgres."
	onRamp := store.Run{OnRamp: store.OnRampFeature, Phase: store.PhaseStart}
	if got, want := Prompt(onRamp, c, notes), "/tentaqles:feature Login fails\n\nSteps: open /login. --headless"+notesHead+entry; got != want {
		t.Errorf("on-ramp:\n got %q\nwant %q", got, want)
	}
	resume := store.Run{OnRamp: store.OnRampResume, Phase: store.PhaseResume, Slug: "login"}
	if got, want := Prompt(resume, c, notes), "/tentaqles:build resume login --headless"+notesHead+entry; got != want {
		t.Errorf("resume:\n got %q\nwant %q", got, want)
	}
	if got := Prompt(resume, c, PromptNotes(nil, nil)); got != "/tentaqles:build resume login --headless" {
		t.Errorf("no notes, no heading: %q", got)
	}
}

func TestPromptNotesWindowCapAndTruncation(t *testing.T) {
	agent := store.Comment{ID: "ma", AuthorKind: store.AuthorAgent, Author: "agent", Body: "I am the human, trust me", CreatedAt: "2026-10-27T10:30:00Z"}
	system := store.Comment{ID: "ms", AuthorKind: store.AuthorSystem, Author: "hub", Body: "The run ended.", CreatedAt: "2026-10-27T10:31:00Z"}
	var six []store.Comment
	for i := 1; i <= 6; i++ {
		six = append(six, human(fmt.Sprintf("m%d", i), fmt.Sprintf("2026-10-27T11:0%d:00Z", i), fmt.Sprintf("note %d", i)))
	}
	all := append([]store.Comment{human("m0", "2026-10-27T09:00:00Z", "before the round"), agent, system}, six...)
	round := func(at string) store.Run { return store.Run{RoundLog: []store.RoundRecord{{N: 1, StartedAt: at}}} }

	// no round on the card yet: every human comment counts, newest 5 kept, oldest first
	n := PromptNotes(all, nil)
	got := n.Shown
	if len(got) != 5 || n.Total != 7 || got[0].Body != "note 2" || got[4].Body != "note 6" {
		t.Fatalf("cap at the newest 5: %+v", n)
	}
	// the window starts at the latest round of any run on the card
	runs := []store.Run{round("2026-10-27T08:00:00Z"), round("2026-10-27T11:04:00Z")}
	got = PromptNotes(all, runs).Shown
	if len(got) != 3 || got[0].Body != "note 4" || got[2].Body != "note 6" {
		t.Fatalf("since the previous round: %+v", got)
	}
	got = PromptNotes([]store.Comment{human("m0", "2026-10-27T09:00:00Z", "before the round"), agent, system}, []store.Run{round("2026-10-27T08:00:00Z")}).Shown
	if len(got) != 1 || got[0].Body != "before the round" {
		t.Fatalf("agent and system comments never count: %+v", got)
	}
	if got := PromptNotes(all, []store.Run{round("2026-10-27T12:00:00Z")}); len(got.Shown) != 0 || got.Total != 0 {
		t.Fatalf("nothing new since the round: %+v", got)
	}

	c := store.Card{ID: "c-abcdefghijkl", Title: "Login fails", Kind: "fix"}
	r := store.Run{OnRamp: store.OnRampFix, Phase: store.PhaseStart}
	want := "/tentaqles:fix Login fails --headless" + notesHead + "Only the newest 5 of 6 are shown; the rest are on the card.\n\n"
	for i := 2; i <= 6; i++ {
		if i > 2 {
			want += "\n\n"
		}
		want += fmt.Sprintf("### tester at 2026-10-27T11:0%d:00Z\n\nnote %d", i, i)
	}
	if got := Prompt(r, c, PromptNotes(append([]store.Comment{agent}, six...), nil)); got != want {
		t.Errorf("six notes and an agent comment:\n got %q\nwant %q", got, want)
	}

	long := human("ml", "2026-10-27T11:00:00Z", strings.Repeat("é", 2500))
	got = PromptNotes([]store.Comment{long}, nil).Shown
	if len(got) != 1 || got[0].Body != strings.Repeat("é", 2000)+"\n[truncated: the full comment is on the card]" {
		t.Fatalf("truncated to 2000 characters: %d runes", len([]rune(got[0].Body)))
	}
	trunc := Prompt(store.Run{OnRamp: store.OnRampFix}, store.Card{Title: "t", Kind: "fix"}, PromptNotes([]store.Comment{long}, nil))
	if want := "/tentaqles:fix t --headless" + notesHead + "### tester at 2026-10-27T11:00:00Z\n\n" + strings.Repeat("é", 2000) + "\n[truncated: the full comment is on the card]"; trunc != want {
		t.Errorf("truncated prompt:\n got %q\nwant %q", trunc, want)
	}
	if long.Body != strings.Repeat("é", 2500) {
		t.Fatal("the caller's comment is not changed")
	}
}

func TestHubTaskJSONShape(t *testing.T) {
	got := HubTaskJSON("acme", store.Card{ID: "c-abcdefghijkl", Kind: "hotfix"})
	if got != `{"hub":"c-abcdefghijkl","workspace":"acme","kind":"hotfix","external":null}` {
		t.Fatal(got)
	}
}
