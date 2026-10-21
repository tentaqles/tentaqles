package runner

import (
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
		if got := Prompt(r, c); got != want {
			t.Errorf("%s:\n got %q\nwant %q", kind, got, want)
		}
	}
	relocated := store.Run{OnRamp: store.OnRampFeature, Phase: store.PhaseRelocated, Slug: "login"}
	if got := Prompt(relocated, card("feature")); got != cases["feature"] {
		t.Errorf("after the hand-off the on-ramp runs again in the worktree: %q", got)
	}
	resume := store.Run{OnRamp: store.OnRampResume, Phase: store.PhaseResume, Slug: "login"}
	if got := Prompt(resume, card("fix")); got != "/tentaqles:build resume login --headless" {
		t.Errorf("resume: %q", got)
	}
	noSlug := store.Run{OnRamp: store.OnRampResume, Phase: store.PhaseResume}
	if got := Prompt(noSlug, card("fix")); got != cases["fix"] {
		t.Errorf("resume without a slug falls back to the on-ramp: %q", got)
	}
}

func TestHubTaskJSONShape(t *testing.T) {
	got := HubTaskJSON("acme", store.Card{ID: "c-abcdefghijkl", Kind: "hotfix"})
	if got != `{"hub":"c-abcdefghijkl","workspace":"acme","kind":"hotfix","external":null}` {
		t.Fatal(got)
	}
}
