package asana

import (
	"strings"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
)

// Asana has no workflow states: a project's sections are its statuses. categoryOf guesses the
// status category from the section name, unless the manifest's tracker.lanes names a hub lane
// for it; then the lane decides (a lane kind such as "review", the default lane "in progress", or
// a lane name the same heuristics read).

// phrases are checked in this order, so "Ready for review" is review, not todo, and "Not started"
// is todo, not active. A phrase matches whole words of the normalised name.
var phrases = []struct {
	category string
	words    []string
}{
	{hubsync.CategoryDone, []string{"done", "complete", "completed", "closed", "shipped", "released", "finished", "resolved", "archived", "deployed"}},
	{hubsync.CategoryReview, []string{"review", "reviewing", "in review", "code review", "qa", "testing", "test", "verify", "verification", "approval", "validation", "uat", "staging"}},
	{hubsync.CategoryTodo, []string{"not started"}},
	{hubsync.CategoryActive, []string{"in progress", "progress", "doing", "wip", "active", "working", "started", "development", "in development", "dev", "implementation", "ongoing"}},
	{hubsync.CategoryTodo, []string{"to do", "todo", "ready", "next", "up next", "selected", "planned", "sprint", "this week", "committed"}},
	{hubsync.CategoryBacklog, []string{"backlog", "icebox", "ideas", "inbox", "untitled section", "new", "triage", "later", "someday"}},
}

func normalise(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("-", " ", "_", " ", "/", " ", ":", " ", ".", " ", "(", " ", ")", " ").Replace(s)
	return " " + strings.Join(strings.Fields(s), " ") + " "
}

// heuristic is the category a name reads as; backlog when nothing matches.
func heuristic(name string) string {
	n := normalise(name)
	if n == "  " {
		return hubsync.CategoryBacklog
	}
	for _, p := range phrases {
		for _, w := range p.words {
			if strings.Contains(n, " "+w+" ") {
				return p.category
			}
		}
	}
	return hubsync.CategoryBacklog
}

// laneFor is the manifest lane override for a section name (case-insensitive), if any.
func laneFor(name string, lanes map[string]string) (string, bool) {
	if v, ok := lanes[name]; ok {
		return v, true
	}
	key := normalise(name)
	for k, v := range lanes {
		if normalise(k) == key {
			return v, true
		}
	}
	return "", false
}

// categoryOf is the status category of a section, with the manifest lanes override.
func categoryOf(name string, lanes map[string]string) string {
	if lane, ok := laneFor(name, lanes); ok {
		l := strings.ToLower(strings.TrimSpace(lane))
		if hubsync.ValidCategory(l) {
			return l
		}
		return heuristic(l)
	}
	return heuristic(name)
}
