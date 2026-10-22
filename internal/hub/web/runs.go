package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func init() {
	extraRoutes = append(extraRoutes, func(s *Server) {
		s.handle("GET /runs", s.runsPage)
		s.handle("GET /runs/{id}", s.runPage)
	})
}

type runRow struct {
	Run   store.Run
	Title string
}

type runsData struct {
	Active, Recent []runRow
	More           int // ended runs not listed
}

const recentRuns = 50

func runTouched(r store.Run) string {
	if r.UpdatedAt != "" {
		return r.UpdatedAt
	}
	return r.QueuedAt
}

func (s *Server) runsPage(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	st := svc.Store()
	runs, err := st.RunsWithStatus(store.Statuses...)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sort.SliceStable(runs, func(i, j int) bool { return runTouched(runs[i]) > runTouched(runs[j]) })
	titles := map[string]string{}
	title := func(cardID string) string {
		if t, ok := titles[cardID]; ok {
			return t
		}
		t := cardID
		if c, err := st.Card(cardID); err == nil {
			t = c.Title
		}
		titles[cardID] = t
		return t
	}
	d := runsData{}
	for _, run := range runs {
		switch {
		case run.Active():
			d.Active = append(d.Active, runRow{Run: run, Title: title(run.CardID)})
		case len(d.Recent) < recentRuns:
			d.Recent = append(d.Recent, runRow{Run: run, Title: title(run.CardID)})
		default:
			d.More++
		}
	}
	p := s.basePage(r, "Runs", ws)
	p.Nav = "runs"
	p.Data = d
	s.render(w, r, "runs", p)
}

type runData struct {
	Run     store.Run
	Card    store.Card
	LogTail string
	LogSize int64
	GateURL string
	// Timeline merges the run's rounds and its decisions, ordered by time.
	Timeline []timelineEntry
}

// timelineEntry is one line of the run page's timeline: a round that started or a decision of this run.
type timelineEntry struct {
	At         string // when it happened (round start, decision asked)
	Class      string // ok | r | w | x: the dot colour
	Text       string
	Detail     string
	Actor      string
	AnsweredAt string
}

func runTimeline(run store.Run, decisions []store.Decision) []timelineEntry {
	var tl []timelineEntry
	for _, rd := range run.RoundLog {
		e := timelineEntry{At: rd.StartedAt, Class: "r", Text: fmt.Sprintf("Round %d started", rd.N)}
		if rd.ExitCode != 0 {
			e.Class = "x"
		}
		e.Detail = fmt.Sprintf("$%.2f", rd.CostUSD)
		if rd.CostUnknown {
			e.Detail += " (cost unknown)"
		}
		if rd.Outcome != "" {
			e.Detail = rd.Outcome + " · " + e.Detail
		}
		tl = append(tl, e)
	}
	for _, d := range decisions {
		if d.RunID != run.ID {
			continue
		}
		what := d.Kind + " decision"
		if d.Checkpoint != "" {
			what += " on checkpoint " + d.Checkpoint
		}
		e := timelineEntry{At: d.AskedAt, Class: "w", Text: what + " asked, waiting on you"}
		if d.Status == store.DecisionAnswered {
			e.Class, e.Text, e.Actor, e.AnsweredAt = "ok", what+" answered "+d.Choice, d.Actor, d.AnsweredAt
		}
		tl = append(tl, e)
	}
	sort.SliceStable(tl, func(i, j int) bool { return tl[i].At < tl[j].At })
	return tl
}

func tailFile(path string, max int64) (string, int64) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", 0
	}
	start := fi.Size() - max
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "", fi.Size()
	}
	b, _ := io.ReadAll(f)
	return string(b), start + int64(len(b))
}

func (s *Server) runPage(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "r")
	if !ok {
		return
	}
	run, err := svc.Store().Run(id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	card, _ := svc.Store().Card(run.CardID)
	d := runData{Run: run, Card: card, GateURL: svc.GateURL()}
	d.LogTail, d.LogSize = tailFile(run.LogPath, 64<<10)
	decisions, _ := svc.Store().DecisionsForCard(run.CardID)
	d.Timeline = runTimeline(run, decisions)
	p := s.basePage(r, "Run "+run.ID, ws)
	p.Nav = "runs"
	p.RunID = run.ID
	p.Data = d
	s.render(w, r, "run", p)
}
