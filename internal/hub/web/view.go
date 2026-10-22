package web

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// now is the page's clock; tests replace it.
var now = time.Now

// pillClasses colours the status vocabulary the way the mockup does; st-<status> stays on every
// pill as a stable hook for tests and scripts.
var pillClasses = map[string]string{
	store.StatusQueued: "p-queue", store.StatusRunning: "p-run", store.StatusWaiting: "p-wait",
	store.StatusFinished: "p-done", store.StatusFailed: "p-fail", store.StatusCancelled: "p-fail",
	store.StatusOverBudget: "p-budget",
}

func pillClass(status string) string {
	c, ok := pillClasses[status]
	if !ok {
		c = "p-queue"
	}
	return c + " st-" + status
}

func statusLabel(status string) string {
	switch status {
	case store.StatusWaiting:
		return "waiting on you"
	case store.StatusOverBudget:
		return "over budget"
	}
	return status
}

// laneTitle capitalises the first letter: "in progress" reads "In progress".
func laneTitle(name string) string {
	r, n := utf8.DecodeRuneInString(name)
	if n == 0 {
		return name
	}
	return string(unicode.ToUpper(r)) + name[n:]
}

func words(s string) string { return strings.ReplaceAll(s, "-", " ") }

// optionLabel is a decision option as a button reads it: raise-and-resume → "Raise and resume".
func optionLabel(o string) string { return laneTitle(words(o)) }

// pct is used as a whole percentage of total, 1–100 once anything is used, 0 without a total.
func pct(used, total float64) int {
	if total <= 0 || used <= 0 {
		return 0
	}
	p := int(used / total * 100)
	if p > 100 {
		p = 100
	}
	if p < 1 {
		p = 1
	}
	return p
}

func parseTS(ts string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, ts)
	return t, err == nil
}

// clock is a stored time as the page shows it: 15:04 today, "Jan 2 15:04" before; unparseable
// text is shown as it is.
func clock(ts string) string {
	t, ok := parseTS(ts)
	if !ok {
		return ts
	}
	t, n := t.Local(), now().Local()
	if t.Year() == n.Year() && t.YearDay() == n.YearDay() {
		return t.Format("15:04")
	}
	return t.Format("Jan 2 15:04")
}

func ago(ts string) string {
	t, ok := parseTS(ts)
	if !ok {
		return ts
	}
	d := now().Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d days ago", int(d.Hours()/24))
}

// prNum is "PR #9" for a pull request URL, "" for anything else.
func prNum(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(p.Path, "/"), "/")
	if len(parts) >= 2 && parts[len(parts)-2] == "pull" && parts[len(parts)-1] != "" {
		return "PR #" + parts[len(parts)-1]
	}
	return ""
}

// clip is the first line of s, cut to max runes with an ellipsis.
func clip(s string, max int) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if r := []rune(l); len(r) > max {
		return string(r[:max]) + "…"
	}
	return l
}

func availableCount(ws []WorkspaceInfo) int {
	n := 0
	for _, w := range ws {
		if w.Available {
			n++
		}
	}
	return n
}

// planningLane: the lane kinds an agent may add to, move between and edit tags in; the board
// offers its + button on these.
func planningLane(kind string) bool {
	return kind == store.LaneBacklog || kind == store.LaneTodo || kind == store.LaneOther
}
