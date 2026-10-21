package runner

import (
	"encoding/json"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// OnRampFor maps a card kind to its on-ramp (spec §8.2). analysis and chore go through feature.
func OnRampFor(kind string) string {
	switch kind {
	case "project":
		return store.OnRampKickoff
	case "fix", "hotfix":
		return store.OnRampFix
	}
	return store.OnRampFeature
}

// TaskText is the card's title and body (front matter removed): the whole task text.
func TaskText(c store.Card) string {
	body := strings.TrimSpace(store.StripFrontMatter(c.Body))
	if body == "" {
		return c.Title
	}
	return c.Title + "\n\n" + body
}

// Prompt is the only text the hub gives the adapter; context comes from the project's files and skills.
func Prompt(r store.Run, c store.Card) string {
	if r.Slug != "" && (r.Phase == store.PhaseResume || r.OnRamp == store.OnRampResume || r.OnRamp == store.OnRampBuild) {
		return "/tentaqles:build resume " + r.Slug + " --headless"
	}
	onRamp := r.OnRamp
	if onRamp == store.OnRampResume {
		onRamp = OnRampFor(c.Kind)
	}
	task := TaskText(c)
	switch onRamp {
	case store.OnRampKickoff:
		return "/tentaqles:kickoff " + task + " --headless"
	case store.OnRampFix:
		if c.Kind == "hotfix" { // only this kind passes --hotfix; the hub never infers it
			return "/tentaqles:fix " + task + " --hotfix --headless"
		}
		return "/tentaqles:fix " + task + " --headless"
	case store.OnRampBuild:
		return "/tentaqles:build " + task + " --headless"
	}
	return "/tentaqles:feature " + task + " --headless"
}

// HubTaskJSON is TQ_HUB_TASK: {"hub","workspace","kind","external"} in that order (spec §13.1).
func HubTaskJSON(workspace string, c store.Card) string {
	b, _ := json.Marshal(struct {
		Hub       string    `json:"hub"`
		Workspace string    `json:"workspace"`
		Kind      string    `json:"kind"`
		External  *struct{} `json:"external"`
	}{c.ID, workspace, c.Kind, nil})
	return string(b)
}
