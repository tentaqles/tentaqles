package web

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func init() {
	extraRoutes = append(extraRoutes, func(s *Server) { s.handle("GET /cards/{id}/tile", s.cardTile) })
}

type laneView struct {
	Lane  store.Lane
	Cards []boardCard
}

type boardData struct {
	Lanes []laneView
	All   []store.Lane // every lane, in order: each tile's Move menu lists them
}

type tileData struct {
	WS    string
	Card  boardCard
	Lanes []store.Lane
}

// boardCard is everything one board tile shows; the board and the tile route build it the same way.
type boardCard struct {
	runner.CardView
	LaneKind   string
	Run        *store.Run      // the active run
	Last       *store.Run      // the newest run when none is active
	Decision   *store.Decision // the oldest pending decision: the one to answer first
	PR         string          // the PR link a comment carried (review and done lanes)
	BudgetUSD  float64         // the card's own dollar budget, when its front matter sets one
	OverBudget int             // runs of this card that stopped at a budget
}

func newBoardCard(v runner.CardView, laneKind string, runs []store.Run, pending []store.Decision, pr string, def store.Budget) boardCard {
	b := boardCard{CardView: v, LaneKind: laneKind, PR: pr}
	for i := range runs {
		r := runs[i]
		if r.Status == store.StatusOverBudget {
			b.OverBudget++
		}
		if r.Active() && b.Run == nil {
			b.Run = &r
		}
	}
	if b.Run == nil && len(runs) > 0 {
		r := runs[0] // RunsForCard is newest first
		b.Last = &r
	}
	if n := len(pending); n > 0 {
		d := pending[n-1] // pending decisions are newest first
		b.Decision = &d
	}
	if _, ok := store.FrontMatter(v.Body)["budget"]; ok {
		if bud, err := store.CardBudget(v.Card, def); err == nil {
			b.BudgetUSD = bud.USD
		}
	}
	return b
}

// Status is the one status the tile shows: waiting on you, the active run's, the last run's (not
// in done), over-budget for a done card that hit a budget on the way, or "".
func (b boardCard) Status() string {
	switch {
	case b.Decision != nil:
		return store.StatusWaiting
	case b.Run != nil:
		return b.Run.Status
	case b.Last != nil && b.LaneKind != store.LaneDone:
		return b.Last.Status
	case b.LaneKind == store.LaneDone && b.OverBudget > 0:
		return store.StatusOverBudget
	}
	return ""
}

func (b boardCard) doneOverBudget() bool {
	return b.Decision == nil && b.Run == nil && b.LaneKind == store.LaneDone && b.OverBudget > 0
}

func (b boardCard) PillLabel() string {
	if b.doneOverBudget() {
		if b.OverBudget == 1 {
			return "budget raised once"
		}
		return fmt.Sprintf("budget raised %d times", b.OverBudget)
	}
	return statusLabel(b.Status())
}

func (b boardCard) StatusText() string {
	switch {
	case b.Decision != nil:
		if b.Decision.AskedAt == "" {
			return words(b.Decision.Kind)
		}
		return words(b.Decision.Kind) + ", " + ago(b.Decision.AskedAt)
	case b.Run != nil:
		switch b.Run.Status {
		case store.StatusRunning:
			return fmt.Sprintf("round %d of %d", max(b.Run.Rounds, 1), b.Run.Budget.Rounds)
		case store.StatusQueued:
			if b.Run.Rounds == 0 {
				return "starts when a slot is free"
			}
			return "resumes when a slot is free"
		}
		return "waiting on the gate"
	case b.Last != nil && b.LaneKind != store.LaneDone:
		switch b.Last.Status {
		case store.StatusFinished:
			return fmt.Sprintf("%d rounds, $%.2f", b.Last.Rounds, b.Last.CostUSD)
		case store.StatusOverBudget:
			return "stopped at its budget"
		}
		return clip(b.Last.StopReason, 80)
	}
	return ""
}

func (b boardCard) Pct() int {
	if b.Run == nil {
		return 0
	}
	return pct(b.Run.CostUSD, b.Run.Budget.USD)
}

// Live is a run in motion: active and not waiting on the human. Only a live run shows the
// budget bar and the run meta line; a waiting card shows its status and actions only.
func (b boardCard) Live() bool {
	return b.Run != nil && b.Decision == nil && b.Run.Status != store.StatusWaiting
}

// Done is a card in a done-kind lane: its meta line is the PR link and the cost, nothing else.
func (b boardCard) Done() bool { return b.LaneKind == store.LaneDone }

func (b boardCard) CanApprove() bool {
	return b.Decision != nil && b.Decision.Kind == store.KindPlanApproval && slices.Contains(b.Decision.Options, "approve")
}

func (b boardCard) OpenReview() bool {
	return b.Decision == nil && b.Run == nil && b.Last != nil && b.Last.Status == store.StatusFinished && b.LaneKind == store.LaneReview
}

// tile reads what newBoardCard needs beyond the card view: the runs and, for review and done
// lanes, the PR link.
func tile(svc *runner.Service, v runner.CardView, laneKind string, pending []store.Decision) (boardCard, error) {
	runs, err := svc.Store().RunsForCard(v.ID)
	if err != nil {
		return boardCard{}, err
	}
	pr := ""
	if laneKind == store.LaneReview || laneKind == store.LaneDone {
		pr = runner.PRLink(svc.Store(), v.ID)
	}
	return newBoardCard(v, laneKind, runs, pending, pr, svc.Store().DefaultBudget()), nil
}

func (s *Server) board(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	lanes, err := svc.Store().Lanes()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	views, err := svc.List(s.reqCtx(r), store.CardFilter{}, false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	pending, err := svc.Store().PendingDecisions()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	byCard := map[string][]store.Decision{}
	for _, d := range pending {
		byCard[d.CardID] = append(byCard[d.CardID], d)
	}
	data := boardData{All: lanes}
	for _, l := range lanes {
		lv := laneView{Lane: l}
		for _, v := range views {
			if !strings.EqualFold(v.Lane, l.Name) {
				continue
			}
			bc, err := tile(svc, v, l.Kind, byCard[v.ID])
			if err != nil {
				s.fail(w, r, err)
				return
			}
			lv.Cards = append(lv.Cards, bc)
		}
		data.Lanes = append(data.Lanes, lv)
	}
	p := s.basePage(r, "Board", ws)
	p.Nav = "board"
	p.Data = data
	s.render(w, r, "board", p)
}

// cardTile renders one tile: the board's live refresh re-reads a card after an SSE change.
func (s *Server) cardTile(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	d, err := svc.Show(s.reqCtx(r), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	lane, err := svc.Store().Lane(d.Lane)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	lanes, err := svc.Store().Lanes()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var pending []store.Decision
	for _, dec := range d.Decisions {
		if dec.Status == store.DecisionPending {
			pending = append(pending, dec)
		}
	}
	bc, err := tile(svc, d.CardView, lane.Kind, pending)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "board-card", tileData{WS: ws, Card: bc, Lanes: lanes}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
