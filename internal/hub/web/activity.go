package web

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func init() {
	extraRoutes = append(extraRoutes, func(s *Server) { s.handle("GET /activity", s.activityPage) })
}

// activityKinds are the kind filter's values and the verbs each keeps.
var activityKinds = map[string][]string{
	"runs":      {activity.VerbRunStart, activity.VerbRunWait, activity.VerbRunEnd, activity.VerbBudgetStop},
	"decisions": {activity.VerbDecisionAsk, activity.VerbDecisionAnswer},
	"cards":     {activity.VerbCardCreate, activity.VerbCardMove, activity.VerbCardUpdate, activity.VerbCommentAdd},
	"reviews":   {activity.VerbReviewApprove, activity.VerbReviewFeedback},
}

var (
	kindFilters   = []struct{ Key, Label string }{{"", "All"}, {"runs", "Runs"}, {"decisions", "Decisions"}, {"cards", "Cards"}, {"reviews", "Reviews"}}
	periodFilters = []struct {
		Days  int
		Label string
	}{{1, "Today"}, {7, "7 days"}, {30, "30 days"}}
)

const activityLimit = 2000

type filterLink struct {
	Label, Href string
	On          bool
}

type kindCost struct{ Kind, Cost string }

type statsView struct {
	Spent, Tokens  string
	Ended          int
	EndedDetail    string
	MedianWait     string
	Answered       int
	AnsweredDetail string
	CostByKind     []kindCost
}

// evPart is one piece of an event sentence: plain text, a card link or a machine value.
type evPart struct {
	Text   string
	CardID string // set: Text links to the card
	Code   string // set: shown in monospace
}

type eventView struct {
	Time  string
	Class string // run | wait | done | fail | you | ""
	Verb  string
	Parts []evPart
	By    string
	Num   string
}

type dayGroup struct {
	Label  string
	Events []eventView
}

type activityData struct {
	Stats          statsView
	Kinds, Periods []filterLink
	Days           []dayGroup
	Truncated      bool
}

// windowStart is local midnight days-1 days before at: days=1 is "today".
func windowStart(days int, at time.Time) time.Time {
	l := at.Local()
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, l.Location()).AddDate(0, 0, -(days - 1))
}

func dataStr(v any) string { s, _ := v.(string); return s }

func dataNum(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}

func dataList(v any) string {
	var out []string
	if xs, ok := v.([]any); ok {
		for _, x := range xs {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return strings.Join(out, ", ")
}

func actorLabel(a string) string {
	switch a {
	case "agent":
		return "An agent session"
	case "hub":
		return "The hub"
	case "terminal":
		return "The terminal"
	case "":
		return "Someone"
	}
	return a
}

func summarize(es []activity.Entry) statsView {
	rep := activity.Summarize(es)
	var spent, tokens float64
	for _, c := range rep.CostByCard {
		spent += c
	}
	answeredBy := map[string]int{}
	for _, e := range es {
		switch e.Verb {
		case activity.VerbRunEnd:
			tokens += dataNum(e.Data["tokens"])
		case activity.VerbDecisionAnswer:
			answeredBy[dataStr(e.Data["actor_kind"])]++
		}
	}
	v := statsView{Spent: fmt.Sprintf("$%.2f", spent), Tokens: kTok(int64(tokens)) + " tok", Answered: rep.DecisionsAnswered}
	var ended []string
	for _, st := range store.Statuses {
		if n := rep.RunsEnded[st]; n > 0 {
			v.Ended += n
			ended = append(ended, fmt.Sprintf("%d %s", n, statusLabel(st)))
		}
	}
	v.EndedDetail = strings.Join(ended, ", ")
	switch {
	case rep.DecisionsAnswered == 0:
		v.MedianWait = "none"
	case rep.MedianWaitMin < 1:
		v.MedianWait = "under 1 min"
	default:
		v.MedianWait = fmt.Sprintf("%.0f min", rep.MedianWaitMin)
	}
	var by []string
	for _, k := range []struct{ key, label string }{{"hub", "page"}, {"terminal", "terminal"}, {"other", "other"}} {
		if n := answeredBy[k.key]; n > 0 {
			by = append(by, fmt.Sprintf("%d %s", n, k.label))
		}
	}
	v.AnsweredDetail = strings.Join(by, ", ")
	for k, c := range rep.CostByKind {
		v.CostByKind = append(v.CostByKind, kindCost{Kind: k, Cost: fmt.Sprintf("$%.2f", c)})
	}
	sort.Slice(v.CostByKind, func(i, j int) bool {
		a, b := rep.CostByKind[v.CostByKind[i].Kind], rep.CostByKind[v.CostByKind[j].Kind]
		if a != b {
			return a > b
		}
		return v.CostByKind[i].Kind < v.CostByKind[j].Kind
	})
	return v
}

func eventFor(e activity.Entry, title func(string) string) eventView {
	ev := eventView{Verb: e.Verb}
	if t, ok := parseTS(e.TS); ok {
		ev.Time = t.Local().Format("15:04")
	}
	txt := func(s string) evPart { return evPart{Text: s} }
	card := evPart{CardID: e.CardID, Text: title(e.CardID)}
	run := evPart{Code: e.RunID}
	who := actorLabel(e.Actor)
	switch e.Verb {
	case activity.VerbCardCreate:
		ev.Parts = []evPart{txt(who + " added "), card, txt(" to " + laneTitle(dataStr(e.Data["lane"])))}
		ev.By = dataStr(e.Data["kind"])
	case activity.VerbCardMove:
		ev.Parts = []evPart{txt(who + " moved "), card, txt(" from " + laneTitle(dataStr(e.Data["from"])) + " to " + laneTitle(dataStr(e.Data["to"])))}
	case activity.VerbCardUpdate:
		ev.Parts = []evPart{txt(who + " edited "), card}
		ev.By = dataList(e.Data["fields"])
	case activity.VerbCommentAdd:
		ev.Parts = []evPart{txt(who + " commented on "), card}
		ev.By = dataStr(e.Data["author_kind"])
	case activity.VerbRunStart:
		ev.Class = "run"
		ev.Parts = []evPart{txt("Run "), run, txt(" started on "), card}
		if r := dataStr(e.Data["on_ramp"]); r != "" {
			ev.By = r + " on-ramp"
		}
	case activity.VerbRunWait:
		ev.Class = "wait"
		ev.Parts = []evPart{txt("Run "), run, txt(" waits on you: "), card}
	case activity.VerbRunEnd:
		st := dataStr(e.Data["status"])
		ev.Class = "fail"
		if st == store.StatusFinished {
			ev.Class = "done"
		}
		ev.Parts = []evPart{txt("Run "), run, txt(" " + statusLabel(st) + ": "), card}
		ev.Num = fmt.Sprintf("$%.2f", dataNum(e.Data["cost_usd"]))
	case activity.VerbBudgetStop:
		ev.Class = "fail"
		ev.Parts = []evPart{txt("Run "), run, txt(" stopped at its " + dataStr(e.Data["limit"]) + " budget on "), card}
	case activity.VerbDecisionAsk:
		ev.Class = "wait"
		ev.Parts = []evPart{card, txt(" asks for a " + words(dataStr(e.Data["kind"])))}
	case activity.VerbDecisionAnswer:
		ev.Class = "you"
		ev.Parts = []evPart{txt(who + " answered " + words(dataStr(e.Data["kind"])) + " on "), card, txt(": " + dataStr(e.Data["choice"]))}
		if dataStr(e.Data["actor_kind"]) == "hub" {
			ev.By = "from the hub"
		} else {
			ev.By = "from the terminal"
		}
	case activity.VerbReviewApprove:
		ev.Class = "done"
		ev.Parts = []evPart{txt(who + " approved "), card}
	case activity.VerbReviewFeedback:
		ev.Class = "you"
		ev.Parts = []evPart{txt(who + " requested changes on "), card}
	case activity.VerbSettingsUpdate:
		ev.Parts = []evPart{txt(who + " changed the settings")}
		ev.By = dataList(e.Data["fields"])
	default:
		ev.Parts = []evPart{txt(e.Verb + " "), card}
	}
	return ev
}

func sameDay(a, b time.Time) bool { return a.Year() == b.Year() && a.YearDay() == b.YearDay() }

// groupByDay sorts newest first by time (rowid order can differ from it) and groups by local day.
func groupByDay(es []activity.Entry, title func(string) string, at time.Time) []dayGroup {
	sort.SliceStable(es, func(i, j int) bool {
		a, oka := parseTS(es[i].TS)
		b, okb := parseTS(es[j].TS)
		return oka && (!okb || a.After(b))
	})
	today := at.Local()
	var out []dayGroup
	for _, e := range es {
		label := "Undated"
		if t, ok := parseTS(e.TS); ok {
			l := t.Local()
			switch {
			case sameDay(l, today):
				label = "Today"
			case sameDay(l, today.AddDate(0, 0, -1)):
				label = "Yesterday"
			default:
				label = l.Format("Mon Jan 2")
			}
		}
		if len(out) == 0 || out[len(out)-1].Label != label {
			out = append(out, dayGroup{Label: label})
		}
		out[len(out)-1].Events = append(out[len(out)-1].Events, eventFor(e, title))
	}
	return out
}

func activityHref(ws, kind string, days int) string {
	v := url.Values{"ws": {ws}}
	if kind != "" {
		v.Set("kind", kind)
	}
	if days != 1 {
		v.Set("days", strconv.Itoa(days))
	}
	return "/activity?" + v.Encode()
}

func (s *Server) activityPage(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	kind := q.Get("kind")
	if _, known := activityKinds[kind]; !known {
		kind = ""
	}
	days, _ := strconv.Atoi(q.Get("days"))
	if days != 7 && days != 30 {
		days = 1
	}
	at := now()
	es, err := svc.Store().ActivitySince(windowStart(days, at), activityLimit)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d := activityData{Stats: summarize(es), Truncated: len(es) == activityLimit}
	for _, k := range kindFilters {
		d.Kinds = append(d.Kinds, filterLink{Label: k.Label, Href: activityHref(ws, k.Key, days), On: k.Key == kind})
	}
	for _, p := range periodFilters {
		d.Periods = append(d.Periods, filterLink{Label: p.Label, Href: activityHref(ws, kind, p.Days), On: p.Days == days})
	}
	shown := es
	if kind != "" {
		shown = nil
		for _, e := range es {
			if slices.Contains(activityKinds[kind], e.Verb) {
				shown = append(shown, e)
			}
		}
	}
	titles := map[string]string{}
	title := func(id string) string {
		if id == "" {
			return ""
		}
		if t, ok := titles[id]; ok {
			return t
		}
		t := id
		if c, err := svc.Store().Card(id); err == nil {
			t = c.Title
		}
		titles[id] = t
		return t
	}
	d.Days = groupByDay(shown, title, at)
	p := s.basePage(r, "Activity", ws)
	p.Nav = "activity"
	p.Data = d
	s.render(w, r, "activity", p)
}
