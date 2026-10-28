package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Board sync on the page (spec B §10): the Connections page, Sync now, the write-policy toggles,
// Push to company board, and the external link on cards. The engine itself runs beside the runner
// (MultiBackend sibling); the pages only read its state, start a pass, or queue a push.

// SyncEngine is what the pages need from a workspace's board sync engine (*hubsync.Engine).
type SyncEngine interface {
	Overview() (hubsync.Overview, error)
	Connection() (hubsync.Connection, bool)
	Check(ctx context.Context) (hubsync.Connection, error)
	Tick(ctx context.Context) (hubsync.Report, error)
	Push(ctx context.Context, cardID string) (store.OutboxRow, error)
}

// SyncBackend is a Backend that knows each workspace's sync engine. A workspace without a tracker
// block answers hubsync.ErrNoTracker.
type SyncBackend interface {
	SyncEngine(ws string) (SyncEngine, error)
}

// SyncRunner is an engine with its poll loop.
type SyncRunner interface {
	SyncEngine
	Run(ctx context.Context)
}

// SyncSibling builds a workspace's sync engine when its runner opens; nil = no tracker. It runs
// beside svc.Start like any Sibling and is what SyncEngine returns for the workspace.
type SyncSibling func(ws *resolve.Workspace, svc *runner.Service) SyncRunner

type syncEngines struct {
	mu gosync.Mutex
	m  map[string]SyncEngine
}

// SetSyncSibling installs the sync engine builder (it replaces SetSibling); call it before the
// first Service.
func (b *MultiBackend) SetSyncSibling(f SyncSibling) {
	b.SetSibling(func(ws *resolve.Workspace, svc *runner.Service) func(context.Context) {
		r := f(ws, svc)
		if r == nil {
			return nil
		}
		b.engines.mu.Lock()
		if b.engines.m == nil {
			b.engines.m = map[string]SyncEngine{}
		}
		b.engines.m[ws.Name] = r
		b.engines.mu.Unlock()
		return r.Run
	})
}

// SyncEngine opens the workspace (never an unavailable one) and returns its engine.
func (b *MultiBackend) SyncEngine(ws string) (SyncEngine, error) {
	if _, err := b.Service(ws); err != nil {
		return nil, err
	}
	b.engines.mu.Lock()
	defer b.engines.mu.Unlock()
	if e := b.engines.m[ws]; e != nil {
		return e, nil
	}
	return nil, hubsync.ErrNoTracker
}

func (s *Server) syncEngine(ws string) (SyncEngine, error) {
	sb, ok := s.opts.Backend.(SyncBackend)
	if !ok {
		return nil, hubsync.ErrNoTracker
	}
	return sb.SyncEngine(ws)
}

// syncErrStatus maps the sync errors for errStatus.
func syncErrStatus(err error) (int, string, bool) {
	switch {
	case errors.Is(err, hubsync.ErrNoTracker):
		return http.StatusNotFound, "no_tracker", true
	case errors.Is(err, hubsync.ErrDisabled):
		return http.StatusBadRequest, "sync_off", true
	case errors.Is(err, hubsync.ErrBusy):
		return http.StatusConflict, "busy", true
	}
	return 0, "", false
}

// syncFlights are the Sync now passes running in this process, by workspace: a second press while
// one runs starts nothing.
type syncFlights struct {
	mu gosync.Mutex
	on map[string]bool
}

func (f *syncFlights) start(ws string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.on[ws] {
		return false
	}
	if f.on == nil {
		f.on = map[string]bool{}
	}
	f.on[ws] = true
	return true
}

func (f *syncFlights) end(ws string) {
	f.mu.Lock()
	delete(f.on, ws)
	f.mu.Unlock()
}

func (f *syncFlights) running(ws string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.on[ws]
}

const syncNowTimeout = 4 * time.Minute

func init() {
	extraRoutes = append(extraRoutes, func(s *Server) {
		s.handle("POST /connections/sync", s.formSyncNow)
		s.handle("POST /connections/settings", s.formSyncSettings)
		s.handle("GET /api/sync", s.apiSync)
		s.handle("POST /api/sync/now", s.apiSyncNow)
		s.handle("POST /cards/{id}/push", s.formPush)
		s.handle("POST /api/cards/{id}/push", s.apiPush)
	})
	flashes["sync-started"] = "Sync started. This page updates when it ends."
	flashes["sync-running"] = "A sync is already running."
	flashes["pushed"] = "Queued for the company board."
	funcs["extLabel"] = extLabel
	funcs["extURL"] = extURL
	funcs["providerName"] = providerName
	funcs["policyValues"] = func() []string { return store.PolicyValues }
}

// startSync checks auth and runs one full pass in the background: the tracker may be slow, and
// the page learns the result from the store (sync_state, the connection) on its live refresh.
// false: one is already running. A workspace with sync off is refused.
func (s *Server) startSync(ws string, eng SyncEngine) (bool, error) {
	ov, err := eng.Overview()
	if err != nil {
		return false, err
	}
	if !ov.Enabled {
		return false, hubsync.ErrDisabled
	}
	if !s.syncing.start(ws) {
		return false, nil
	}
	go func() {
		defer s.syncing.end(ws)
		ctx, cancel := context.WithTimeout(context.Background(), syncNowTimeout)
		defer cancel()
		_, _ = eng.Check(ctx) // records the account and the source, or the error
		_, _ = eng.Tick(ctx)  // records last_ok_at or last_error in sync_state
	}()
	return true, nil
}

func (s *Server) formSyncNow(w http.ResponseWriter, r *http.Request) {
	_, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	eng, err := s.syncEngine(ws)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	started, err := s.startSync(ws, eng)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	flash := "sync-started"
	if !started {
		flash = "sync-running"
	}
	s.redirect(w, r, "/connections?ws="+url.QueryEscape(ws)+"&flash="+flash)
}

func (s *Server) apiSyncNow(w http.ResponseWriter, r *http.Request) {
	_, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	eng, err := s.syncEngine(ws)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	started, err := s.startSync(ws, eng)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"started": started, "running": true})
}

func (s *Server) apiSync(w http.ResponseWriter, r *http.Request) {
	_, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	eng, err := s.syncEngine(ws)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ov, err := eng.Overview()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	conn, checked := eng.Connection()
	writeJSON(w, 200, map[string]any{"overview": ov, "connection": conn, "checked": checked, "running": s.syncing.running(ws)})
}

// syncUpdateFromForm reads the Connections page's settings form: policy.<action> = off|ask|auto,
// enabled = on|off, interval = seconds. Blank fields stay as they are; the runner validates.
func syncUpdateFromForm(r *http.Request) (runner.SettingsUpdate, error) {
	if err := r.ParseForm(); err != nil {
		return runner.SettingsUpdate{}, err
	}
	su := &runner.SyncUpdate{Policy: map[string]string{}}
	for k, vs := range r.PostForm {
		a, isPolicy := strings.CutPrefix(k, "policy.")
		if !isPolicy || len(vs) == 0 {
			continue
		}
		if v := strings.TrimSpace(vs[0]); v != "" {
			su.Policy[a] = v // an unknown action or value is refused by the runner
		}
	}
	switch strings.TrimSpace(r.PostFormValue("enabled")) {
	case "on":
		on := true
		su.Enabled = &on
	case "off":
		off := false
		su.Enabled = &off
	case "":
	default:
		return runner.SettingsUpdate{}, errors.New("board sync: choose on or off")
	}
	if v := cleanNumber(r.PostFormValue("interval")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return runner.SettingsUpdate{}, errors.New("poll interval: use a whole number of seconds, 60 or more")
		}
		su.Interval = &n
	}
	return runner.SettingsUpdate{Sync: su}, nil
}

// formSyncSettings saves the policy toggles through the settings API path (svc.UpdateSettings):
// human-only, validated, audited as settings.update.
func (s *Server) formSyncSettings(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	u, err := syncUpdateFromForm(r)
	if err == nil {
		_, err = svc.UpdateSettings(s.reqCtx(r), u)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, "/connections?ws="+url.QueryEscape(ws)+"&flash=settings-saved")
}

// push queues a local card for the company board (tq hub push). Human-only: an agent session is
// refused before anything is queued. The engine applies the create_card policy on its next
// outbox pass (Push wakes it).
func (s *Server) push(r *http.Request, ws, id string) (store.OutboxRow, error) {
	if runner.IsAgent(s.reqCtx(r)) {
		return store.OutboxRow{}, runner.ErrHumanOnly
	}
	eng, err := s.syncEngine(ws)
	if err != nil {
		return store.OutboxRow{}, err
	}
	return eng.Push(r.Context(), id)
}

func (s *Server) formPush(w http.ResponseWriter, r *http.Request) {
	_, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	if _, err := s.push(r, ws, id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, flashURL(ws, id, "pushed"))
}

func (s *Server) apiPush(w http.ResponseWriter, r *http.Request) {
	_, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	row, err := s.push(r, ws, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, row)
}

// ---- views ----

var providerNames = map[string]string{
	"github": "GitHub", "azure-devops": "Azure DevOps", "jira": "Jira", "linear": "Linear", "asana": "Asana",
}

// providerShort is the badge prefix: "ADO #123".
var providerShort = map[string]string{"azure-devops": "ADO"}

func providerName(p string) string {
	if n, ok := providerNames[p]; ok {
		return n
	}
	return p
}

// extLabel is a linked card's badge: "ADO #123", "GitHub #45", "Jira ACME-7". A numeric id gets
// a #; a key keeps its own spelling.
func extLabel(provider, id string) string {
	name := providerShort[provider]
	if name == "" {
		name = providerName(provider)
	}
	if id == "" {
		return name
	}
	if _, err := strconv.ParseUint(id, 10, 64); err == nil {
		return name + " #" + id
	}
	return name + " " + id
}

// extURL is the tracker link when it is an absolute http(s) URL, else "" (the badge is text).
func extURL(u string) string {
	p, err := url.Parse(strings.TrimSpace(u))
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return ""
	}
	return p.String()
}

// policyRow is one write-policy toggle on the Connections page.
type policyRow struct {
	Action, Label, Note, Value string
}

var policyLabels = map[string][2]string{
	store.OutboxMoveOnStart:    {"Lane move when work starts", "a card you move into In progress moves its item to the active status"},
	store.OutboxMoveOnReview:   {"Lane move to review on approve", "approving a result moves the item to its review status"},
	store.OutboxCommentResults: {"Result comments", "the run summary and the PR link, posted on the item"},
	store.OutboxCreateCard:     {"Create a company card", "Push to company board on a local card"},
	store.OutboxEditText:       {"Edit title or description", "your edits to a linked card's title or body"},
	store.OutboxCloseItem:      {"Close an item", "a linked card that reaches Done"},
}

func policyRows(p store.SyncPolicy) []policyRow {
	out := make([]policyRow, 0, len(store.OutboxActions))
	for _, a := range store.OutboxActions {
		l := policyLabels[a]
		out = append(out, policyRow{Action: a, Label: l[0], Note: l[1], Value: p.Get(a)})
	}
	return out
}

// trackerChoice is one supported tracker on the Connections page.
type trackerChoice struct{ ID, Name, Logo, Class, What string }

var trackerChoices = []trackerChoice{
	{"github", "GitHub", "Gh", "lg-github", "Issues and Projects v2; auth through gh auth login."},
	{"azure-devops", "Azure DevOps", "Az", "lg-azure", "Boards work items; auth through az login."},
	{"jira", "Jira", "Ji", "lg-jira", "Jira Cloud issues; auth through acli, the keychain or an env token."},
	{"linear", "Linear", "Li", "lg-linear", "A team's issues; a token in the keychain or env."},
	{"asana", "Asana", "As", "lg-asana", "A project's tasks; a token in the keychain or env."},
}

type connectionsData struct {
	Tracker  bool   // the workspace has a tracker block and this hub runs its engine
	Error    string // the overview could not be read
	Overview hubsync.Overview
	Conn     hubsync.Connection
	Checked  bool
	State    store.SyncState
	Synced   bool // a poll ran (ok or not)
	Running  bool // a Sync now is in flight
	Policies []policyRow
	Choices  []trackerChoice
	Order    string
}

func (s *Server) connectionsData(ws string) connectionsData {
	d := connectionsData{Choices: trackerChoices, Order: "cli → keychain → env → mcp"}
	eng, err := s.syncEngine(ws)
	if err != nil {
		if !errors.Is(err, hubsync.ErrNoTracker) {
			d.Error = err.Error()
		}
		return d
	}
	d.Tracker = true
	d.Overview, err = eng.Overview()
	if err != nil {
		d.Error = err.Error()
	}
	d.Conn, d.Checked = eng.Connection()
	if len(d.Overview.States) > 0 {
		d.State = d.Overview.States[0]
		d.Synced = d.State.LastOKAt != "" || d.State.LastErrorAt != ""
	}
	d.Running = s.syncing.running(ws)
	d.Policies = policyRows(d.Overview.Settings.Policy)
	return d
}

// cardSync is the card page's company-board panel.
type cardSync struct {
	Tracker  string           // the workspace's tracker provider; "" = no sync in this hub
	CanPush  bool             // a local card with no create_card row pending
	Queued   *store.OutboxRow // the pending create_card row
	Rows     []store.OutboxRow
	Provider string
}

func (s *Server) cardSync(svc *runner.Service, ws string, c store.Card) cardSync {
	var out cardSync
	rows, err := svc.Store().ListOutbox(store.OutboxFilter{CardID: c.ID})
	if err == nil {
		out.Rows = rows
	}
	eng, err := s.syncEngine(ws)
	if err != nil {
		return out
	}
	if ov, err := eng.Overview(); err == nil {
		out.Tracker = ov.Provider
	}
	if c.ExternalProvider != "" && c.ExternalID != "" {
		return out
	}
	for i := range out.Rows {
		if o := out.Rows[i]; o.Action == store.OutboxCreateCard && o.Status == store.OutboxPending {
			out.Queued = &o
		}
	}
	out.CanPush = out.Tracker != "" && out.Queued == nil
	return out
}

// withOutbox adds the card's outbox rows to its timeline: each row at the time it was queued, with
// its status now (sent, pending, waiting on you, skipped, failed).
func withOutbox(tl []tlItem, rows []store.OutboxRow) []tlItem {
	if len(rows) == 0 {
		return tl
	}
	out := append([]tlItem(nil), tl...)
	for _, o := range rows {
		it := tlItem{When: o.CreatedAt, Code: o.ID}
		state := o.Status
		switch o.Status {
		case store.OutboxSent:
			it.Class = "ok"
		case store.OutboxFailed:
			it.Class = "x"
			it.Detail = clip(o.LastError, 120)
		case store.OutboxSkipped:
			it.Detail = clip(o.LastError, 120)
		case store.OutboxPending:
			it.Class = "r"
			if o.DecisionID != "" {
				it.Class, state = "w", "waiting on you"
			} else if o.LastError != "" {
				it.Detail = clip(o.LastError, 120)
			}
		}
		it.Text = "Company board: " + strings.ReplaceAll(o.Action, "_", " ") + " " + state
		out = append(out, it)
	}
	sortTimeline(out)
	if len(out) > timelineMax {
		out = out[len(out)-timelineMax:]
	}
	return out
}

func sortTimeline(tl []tlItem) {
	sort.SliceStable(tl, func(i, j int) bool { return tsBefore(tl[i].When, tl[j].When) })
}

// isSyncDecision: a send/skip question the sync engine asked about an outbox row.
func isSyncDecision(d store.Decision) bool {
	_, ok := d.Data["outbox"]
	return ok && d.Kind == store.KindPermission
}
