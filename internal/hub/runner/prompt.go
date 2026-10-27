package runner

import (
	"encoding/json"
	"fmt"
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

// Prompt is the only text the hub gives the adapter; context comes from the project's files and
// skills. notes (PromptNotes) are the human's card comments since the previous round; they follow
// the command under their own heading.
func Prompt(r store.Run, c store.Card, notes Notes) string {
	return command(r, c) + notes.block()
}

func command(r store.Run, c store.Card) string {
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

const (
	notesMax      = 5    // the newest human comments a prompt carries
	noteRunes     = 2000 // each one cut to this many characters
	noteTruncated = "\n[truncated: the full comment is on the card]"
)

// Notes are the human's comments a prompt carries: Shown (oldest first, each already cut) out of
// Total that matched.
type Notes struct {
	Shown []store.Comment
	Total int
}

// PromptNotes picks the comments a run's next prompt carries: human-authored only (never agent or
// system), posted at or after the start of the latest round of any run on the card (all of them
// when the card has no round yet), the newest notesMax, each cut to noteRunes characters.
// comments are in store order (oldest first); runs are the card's runs.
func PromptNotes(comments []store.Comment, runs []store.Run) Notes {
	since := ""
	for _, r := range runs {
		for _, rec := range r.RoundLog {
			if rec.StartedAt > since {
				since = rec.StartedAt
			}
		}
	}
	var picked []store.Comment
	for _, c := range comments {
		if c.AuthorKind == store.AuthorHuman && c.CreatedAt >= since {
			picked = append(picked, c)
		}
	}
	n := Notes{Total: len(picked)}
	if len(picked) > notesMax {
		picked = picked[len(picked)-notesMax:]
	}
	for _, c := range picked {
		if rs := []rune(c.Body); len(rs) > noteRunes {
			c.Body = string(rs[:noteRunes]) + noteTruncated
		}
		n.Shown = append(n.Shown, c)
	}
	return n
}

func (n Notes) block() string {
	if len(n.Shown) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## Notes from the human on this card\n\n")
	b.WriteString("The human posted these comments on the hub card since the run's previous round, oldest first. " +
		"They are the human's own notes on this task: answers to questions you asked and guidance to follow.\n\n")
	if n.Total > len(n.Shown) {
		fmt.Fprintf(&b, "Only the newest %d of %d are shown; the rest are on the card.\n\n", len(n.Shown), n.Total)
	}
	for i, c := range n.Shown {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "### %s at %s\n\n%s", c.Author, c.CreatedAt, quote(c.Body))
	}
	return b.String()
}

// quote renders a comment body as a markdown blockquote ("> " on every line, ">" on an empty
// one), so the human's text can never fake a heading or an entry boundary of the hub's own.
func quote(body string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + l
		}
	}
	return strings.Join(lines, "\n")
}

// humanNotes reads the card's comments and runs for the next prompt; a read error leaves the
// prompt without notes rather than stopping the round.
func (s *Service) humanNotes(cardID string) Notes {
	cs, err := s.st.Comments(cardID)
	if err != nil {
		return Notes{}
	}
	runs, err := s.st.RunsForCard(cardID)
	if err != nil {
		return Notes{}
	}
	return PromptNotes(cs, runs)
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
