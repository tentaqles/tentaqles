package web

import (
	"net/http"
	"slices"
	"sort"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// decisionItem is one decision as the inbox and the card page show it.
type decisionItem struct {
	Workspace  string
	Card       store.Card
	Decision   store.Decision
	Run        *store.Run // the decision's run, when the row still exists
	OverBudget bool       // a hub budget stop: the page shows the run's numbers
	Sync       bool       // a send/skip question about a write to the company board (outbox)
	Pack       *packApproval
}

// packApproval is what the page shows for a pack approval (spec C §2.11):
// the gated action and the evidence names with their hashes.
type packApproval struct {
	Pack, Action, Subject, Target, ExpiresAt string
	Evidence                                 []evidenceRow
}

type evidenceRow struct{ Name, Hash string }

func newPackApproval(d store.Decision) *packApproval {
	p := d.PackApproval()
	if p == "" {
		return nil
	}
	str := func(k string) string { s, _ := d.Data[k].(string); return s }
	pa := &packApproval{Pack: p, Action: str("action"), Subject: str("subject"), Target: str("target"), ExpiresAt: str("expires_at")}
	if ev, ok := d.Data["evidence"].(map[string]any); ok {
		for k, v := range ev {
			h, _ := v.(string)
			pa.Evidence = append(pa.Evidence, evidenceRow{k, h})
		}
	}
	sort.Slice(pa.Evidence, func(i, j int) bool { return pa.Evidence[i].Name < pa.Evidence[j].Name })
	return pa
}

func newDecisionItem(svc *runner.Service, ws string, card store.Card, d store.Decision) decisionItem {
	it := decisionItem{Workspace: ws, Card: card, Decision: d,
		OverBudget: d.Kind == store.KindPermission && slices.Contains(d.Options, "raise-and-resume"), Sync: isSyncDecision(d),
		Pack: newPackApproval(d)}
	if d.RunID != "" {
		if run, err := svc.Store().Run(d.RunID); err == nil {
			it.Run = &run
		}
	}
	return it
}

// waitingItems gathers pending decisions across the available workspaces; an unavailable one is
// never opened, so it is skipped.
func (s *Server) waitingItems(r *http.Request, only string) []runner.WaitingItem {
	items := []runner.WaitingItem{}
	for _, info := range s.opts.Backend.Workspaces() {
		if !info.Available || (only != "" && info.Name != only) {
			continue
		}
		svc, err := s.opts.Backend.Service(info.Name)
		if err != nil {
			continue
		}
		got, err := svc.Waiting(s.reqCtx(r))
		if err != nil {
			continue
		}
		items = append(items, got...)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Decision.AskedAt > items[j].Decision.AskedAt })
	return items
}

func (s *Server) waitingPage(w http.ResponseWriter, r *http.Request) {
	ws := r.URL.Query().Get("ws")
	if ws != "" {
		if _, _, ok := s.service(w, r); !ok {
			return
		}
	}
	p := s.basePage(r, "Decisions", ws)
	p.Nav = "decisions"
	items := s.waitingItems(r, "")
	views := make([]decisionItem, 0, len(items))
	for _, it := range items {
		svc, err := s.opts.Backend.Service(it.Workspace)
		if err != nil {
			continue
		}
		views = append(views, newDecisionItem(svc, it.Workspace, it.Card, it.Decision))
	}
	p.Data = views
	if ws == "" {
		p.WaitingCount = len(items)
	}
	s.render(w, r, "waiting", p)
}
