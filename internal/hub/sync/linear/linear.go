// Package linear is the board-sync adapter for Linear (spec B Â§7), over the GraphQL API at
// https://api.linear.app/graphql.
//
// Scope: tracker.scope.team (or org) names the Linear team by key, name or id. ListChanged mirrors
// viewer.assignedIssues plus the children and grandchildren of issues assigned to the viewer (assigned_to: me), or
// every issue of the team (assigned_to: anyone). Workflow state types map to lane kinds: triage
// and backlog -> backlog, unstarted -> todo, started -> active (review when the state's name says
// review), completed and canceled -> done.
//
// Auth: Linear has no CLI, so the credential comes from the keychain or the env var through the
// cred resolver. A personal API key is sent as `Authorization: <key>`, with no scheme.
package linear

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	gosync "sync"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
)

// Name is the provider id.
const Name = "linear"

// DefaultBaseURL is Linear's API origin; the endpoint is <base>/graphql.
const DefaultBaseURL = "https://api.linear.app"

// maxPages caps one ListChanged query's pagination; pageSize is the queries' first: N.
const (
	maxPages = 40
	pageSize = 50
)

func init() { hubsync.Register(Name, New) }

// Adapter is the Linear tracker.
type Adapter struct {
	cfg      hubsync.Config
	endpoint string
	http     *http.Client
	now      func() time.Time

	mu           gosync.Mutex
	limitedUntil time.Time // a spent X-RateLimit budget: no request before this
	teamID       string    // resolved tracker.scope team
	statuses     []hubsync.Status
	warning      string // the last ListChanged's truncation note ("" when it read everything)
}

// LastWarning is the note the last ListChanged left: non-empty when it hit the page cap and
// skipped items (they come back when they change again). The engine can surface it as the
// source's last_error; it is not part of hubsync.Adapter.
func (a *Adapter) LastWarning() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.warning
}

var _ hubsync.Adapter = (*Adapter)(nil)

// New builds the adapter (the registry's Factory).
func New(cfg hubsync.Config) (hubsync.Adapter, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	return &Adapter{
		cfg:      cfg,
		endpoint: base + "/graphql",
		// Redirects are never followed: a GraphQL POST has no reason to move, and the token must
		// not travel to another origin.
		http: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		now:  time.Now,
	}, nil
}

func (a *Adapter) Name() string { return Name }

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func isUUID(s string) bool { return uuidRe.MatchString(s) }

// teamRef is the configured team: scope.team, else scope.org.
func (a *Adapter) teamRef() string {
	if t := strings.TrimSpace(a.cfg.Scope["team"]); t != "" {
		return t
	}
	return strings.TrimSpace(a.cfg.Scope["org"])
}

// team resolves the configured team to its id ("" when none is configured).
func (a *Adapter) team(ctx context.Context) (string, error) {
	ref := a.teamRef()
	if ref == "" {
		return "", nil
	}
	a.mu.Lock()
	id := a.teamID
	a.mu.Unlock()
	if id != "" {
		return id, nil
	}
	if isUUID(ref) {
		id = ref
	} else {
		var out struct {
			Teams struct {
				Nodes []struct{ ID, Key, Name string } `json:"nodes"`
			} `json:"teams"`
		}
		if err := a.do(ctx, "LinearTeam", qTeam, map[string]any{"team": ref}, &out); err != nil {
			return "", err
		}
		n := out.Teams.Nodes
		if len(n) == 0 {
			return "", fmt.Errorf("linear: team %q (tracker.scope.team or org: a team key, name or id): %w", ref, hubsync.ErrNotFound)
		}
		// a key match wins over a name match
		id = n[0].ID
		for _, t := range n {
			if strings.EqualFold(t.Key, ref) {
				id = t.ID
				break
			}
		}
	}
	a.mu.Lock()
	a.teamID = id
	a.mu.Unlock()
	return id, nil
}

func (a *Adapter) needTeam(ctx context.Context) (string, error) {
	id, err := a.team(ctx)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("linear: no team in tracker.scope (set scope.team or scope.org to the team key)")
	}
	return id, nil
}

// Check verifies the credential (and the team, when one is configured) and returns the account.
func (a *Adapter) Check(ctx context.Context) (string, error) {
	var out struct {
		Viewer struct{ ID, Name, DisplayName, Email string } `json:"viewer"`
	}
	if err := a.do(ctx, "LinearViewer", qViewer, nil, &out); err != nil {
		return "", err
	}
	if _, err := a.team(ctx); err != nil {
		return "", err
	}
	v := out.Viewer
	for _, s := range []string{v.Email, v.DisplayName, v.Name, v.ID} {
		if s != "" {
			return s, nil
		}
	}
	return "", errors.New("linear: the API returned no viewer")
}

// Category maps a Linear WorkflowState type (and name) to a lane kind.
func Category(stateType, name string) string {
	switch stateType {
	case "triage", "backlog":
		return hubsync.CategoryBacklog
	case "unstarted":
		return hubsync.CategoryTodo
	case "started":
		if strings.Contains(strings.ToLower(name), "review") {
			return hubsync.CategoryReview
		}
		return hubsync.CategoryActive
	case "completed", "canceled", "duplicate":
		return hubsync.CategoryDone
	}
	return hubsync.CategoryBacklog
}

// Statuses lists the team's workflow states in board order.
func (a *Adapter) Statuses(ctx context.Context) ([]hubsync.Status, error) {
	a.mu.Lock()
	cached := a.statuses
	a.mu.Unlock()
	if cached != nil {
		return slices.Clone(cached), nil
	}
	id, err := a.needTeam(ctx)
	if err != nil {
		return nil, err
	}
	var out struct {
		Team *struct {
			States struct {
				Nodes []struct {
					ID, Name, Type string
					Position       float64
				} `json:"nodes"`
			} `json:"states"`
		} `json:"team"`
	}
	if err := a.do(ctx, "LinearStates", qStates, map[string]any{"id": id}, &out); err != nil {
		return nil, err
	}
	if out.Team == nil {
		return nil, fmt.Errorf("linear: team %q: %w", a.teamRef(), hubsync.ErrNotFound)
	}
	nodes := out.Team.States.Nodes
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Position < nodes[j].Position })
	st := make([]hubsync.Status, 0, len(nodes))
	for _, n := range nodes {
		st = append(st, hubsync.Status{Name: n.Name, Category: Category(n.Type, n.Name)})
	}
	a.mu.Lock()
	a.statuses = st
	a.mu.Unlock()
	return slices.Clone(st), nil
}

// closedTypes are the WorkflowState types a first sync skips: years of finished history are not
// worth mirroring, and paging them would spend the hourly complexity budget.
var closedTypes = []string{"completed", "canceled", "duplicate"}

// ListChanged returns the mirrored issues updated after since, and the new watermark: the latest
// updatedAt seen, or since when nothing changed.
//
// The first sync (since zero) reads open issues only. With assigned_to me it reads the viewer's
// issues plus their children and grandchildren; a child comes in when it, its parent or its
// grandparent changed after since, so the existing children of a newly assigned parent arrive on
// the tick the parent does. Deeper descendants are not mirrored.
//
// One query reads at most maxPages pages. When it hits that cap, the watermark still moves to the
// newest updatedAt seen (so the next tick does not reread the same pages), the unread items are
// skipped until they change again, and LastWarning says so.
func (a *Adapter) ListChanged(ctx context.Context, since time.Time) ([]hubsync.Item, time.Time, error) {
	team, err := a.team(ctx)
	if err != nil {
		return nil, since, err
	}
	base := map[string]any{}
	var sinceF map[string]any
	if since.IsZero() {
		base["state"] = map[string]any{"type": map[string]any{"nin": closedTypes}}
	} else {
		sinceF = map[string]any{"gt": since.UTC().Format(time.RFC3339Nano)}
		base["updatedAt"] = sinceF
	}
	if team != "" {
		base["team"] = map[string]any{"id": map[string]any{"eq": team}}
	}
	type query struct {
		op, doc string
		filter  map[string]any
		viewer  bool
	}
	var qs []query
	if a.cfg.AssignedTo == "anyone" {
		if team == "" {
			return nil, since, errors.New("linear: assigned_to anyone needs a team in tracker.scope")
		}
		qs = []query{{"LinearIssues", qIssues, base, false}}
	} else {
		children := cloneMap(base)
		delete(children, "updatedAt")
		isMe := map[string]any{"assignee": map[string]any{"isMe": map[string]any{"eq": true}}}
		and := []any{map[string]any{"or": []any{
			map[string]any{"parent": isMe},
			map[string]any{"parent": map[string]any{"parent": isMe}},
		}}}
		if sinceF != nil {
			upd := map[string]any{"updatedAt": sinceF}
			and = append(and, map[string]any{"or": []any{
				upd,
				map[string]any{"parent": upd},
				map[string]any{"parent": map[string]any{"parent": upd}},
			}})
		}
		children["and"] = and
		qs = []query{{"LinearAssigned", qAssigned, base, true}, {"LinearChildren", qChildren, children, false}}
	}

	var items []hubsync.Item
	seen := map[string]bool{}
	wm := since
	var truncated []string
	for _, q := range qs {
		after := ""
		for page := 0; ; page++ {
			if page == maxPages {
				truncated = append(truncated, q.op)
				break
			}
			vars := map[string]any{"filter": q.filter}
			if after != "" {
				vars["after"] = after
			}
			var conn issueConn
			if q.viewer {
				var out struct {
					Viewer struct {
						AssignedIssues issueConn `json:"assignedIssues"`
					} `json:"viewer"`
				}
				if err := a.do(ctx, q.op, q.doc, vars, &out); err != nil {
					return nil, since, err
				}
				conn = out.Viewer.AssignedIssues
			} else {
				var out struct {
					Issues issueConn `json:"issues"`
				}
				if err := a.do(ctx, q.op, q.doc, vars, &out); err != nil {
					return nil, since, err
				}
				conn = out.Issues
			}
			for _, n := range conn.Nodes {
				if seen[n.ID] {
					continue
				}
				seen[n.ID] = true
				it := n.item()
				if it.UpdatedAt.After(wm) {
					wm = it.UpdatedAt
				}
				items = append(items, it)
			}
			if !conn.PageInfo.HasNextPage || conn.PageInfo.EndCursor == "" {
				break
			}
			after = conn.PageInfo.EndCursor
		}
	}
	warn := ""
	if len(truncated) > 0 {
		warn = fmt.Sprintf("linear: %s truncated at %d pages of %d issues; issues past the cap are skipped until they change again",
			strings.Join(truncated, ", "), maxPages, pageSize)
	}
	a.mu.Lock()
	a.warning = warn
	a.mu.Unlock()
	return items, wm, nil
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Get reads one issue by id or identifier (ACME-123).
func (a *Adapter) Get(ctx context.Context, id string) (hubsync.Item, error) {
	var out struct {
		Issue *issueNode `json:"issue"`
	}
	if err := a.read(ctx, "LinearIssue", qIssue, map[string]any{"id": id}, &out); err != nil {
		return hubsync.Item{}, err
	}
	if out.Issue == nil {
		return hubsync.Item{}, fmt.Errorf("linear issue %s: %w", id, hubsync.ErrNotFound)
	}
	return out.Issue.item(), nil
}

// issueID resolves an identifier to the issue's UUID (a UUID passes through).
func (a *Adapter) issueID(ctx context.Context, id string) (string, error) {
	if isUUID(id) {
		return id, nil
	}
	var out struct {
		Issue *struct{ ID string } `json:"issue"`
	}
	if err := a.read(ctx, "LinearIssueID", qIssueID, map[string]any{"id": id}, &out); err != nil {
		return "", err
	}
	if out.Issue == nil || out.Issue.ID == "" {
		return "", fmt.Errorf("linear issue %s: %w", id, hubsync.ErrNotFound)
	}
	return out.Issue.ID, nil
}

type named struct{ ID, Name, Type string }

type namedConn struct {
	Nodes []named `json:"nodes"`
}

type teamMeta struct {
	ID     string    `json:"id"`
	States namedConn `json:"states"`
	Labels namedConn `json:"labels"`
}

// issueTeam is an issue with its labels, its team's states and labels, and the workspace labels.
type issueTeam struct {
	ID     string
	Labels []named
	Team   teamMeta
	WSLbls []named
}

func (a *Adapter) issueTeam(ctx context.Context, id string) (issueTeam, error) {
	var out struct {
		Issue *struct {
			ID     string    `json:"id"`
			Labels namedConn `json:"labels"`
			Team   teamMeta  `json:"team"`
		} `json:"issue"`
		IssueLabels namedConn `json:"issueLabels"`
	}
	if err := a.read(ctx, "LinearIssueTeam", qIssueTeam, map[string]any{"id": id}, &out); err != nil {
		return issueTeam{}, err
	}
	if out.Issue == nil {
		return issueTeam{}, fmt.Errorf("linear issue %s: %w", id, hubsync.ErrNotFound)
	}
	return issueTeam{ID: out.Issue.ID, Labels: out.Issue.Labels.Nodes, Team: out.Issue.Team, WSLbls: out.IssueLabels.Nodes}, nil
}

func findName(list []named, name string) (named, bool) {
	name = strings.TrimSpace(name)
	for _, n := range list {
		if strings.EqualFold(n.Name, name) {
			return n, true
		}
	}
	return named{}, false
}

func names(list []named) string {
	out := make([]string, len(list))
	for i, n := range list {
		out[i] = n.Name
	}
	return strings.Join(out, ", ")
}

func stateID(states []named, status string) (string, error) {
	s, ok := findName(states, status)
	if !ok {
		return "", fmt.Errorf("linear: no state %q in the team (have: %s)", status, names(states))
	}
	return s.ID, nil
}

func labelID(team, workspace []named, label string) (string, error) {
	if l, ok := findName(team, label); ok {
		return l.ID, nil
	}
	if l, ok := findName(workspace, label); ok {
		return l.ID, nil
	}
	return "", fmt.Errorf("linear: no label %q in the team or the workspace", label)
}

func (a *Adapter) mutate(ctx context.Context, op, doc, field string, vars map[string]any) error {
	var out map[string]struct {
		Success bool `json:"success"`
	}
	if err := a.do(ctx, op, doc, vars, &out); err != nil {
		return err
	}
	if !out[field].Success {
		return fmt.Errorf("linear %s: the API reported no success", op)
	}
	return nil
}

// Move sets the issue's workflow state, resolved by name (case-insensitive) within its team.
func (a *Adapter) Move(ctx context.Context, id, status string) error {
	it, err := a.issueTeam(ctx, id)
	if err != nil {
		return err
	}
	sid, err := stateID(it.Team.States.Nodes, status)
	if err != nil {
		return err
	}
	return a.mutate(ctx, "LinearIssueUpdate", mIssueUpdate, "issueUpdate",
		map[string]any{"id": it.ID, "input": map[string]any{"stateId": sid}})
}

// Comment adds a comment; Linear takes markdown as is.
func (a *Adapter) Comment(ctx context.Context, id, markdown string) error {
	uid, err := a.issueID(ctx, id)
	if err != nil {
		return err
	}
	return a.mutate(ctx, "LinearCommentCreate", mCommentCreate, "commentCreate",
		map[string]any{"input": map[string]any{"issueId": uid, "body": markdown}})
}

// description is the body plus, when set, an acceptance criteria section (Linear has no field).
func description(body, acceptance string) string {
	body = strings.TrimRight(body, "\n")
	acceptance = strings.TrimSpace(acceptance)
	if acceptance == "" {
		return body
	}
	sec := "## Acceptance criteria\n\n" + acceptance
	if body == "" {
		return sec
	}
	return body + "\n\n" + sec
}

// Create makes an issue in the configured team; in.ParentID makes it a sub-issue. in.Type is
// ignored (Linear has one issue type).
func (a *Adapter) Create(ctx context.Context, in hubsync.NewItem) (hubsync.Item, error) {
	team, err := a.needTeam(ctx)
	if err != nil {
		return hubsync.Item{}, err
	}
	input := map[string]any{"teamId": team, "title": in.Title}
	if d := description(in.Body, in.Acceptance); d != "" {
		input["description"] = d
	}
	if in.ParentID != "" {
		pid, err := a.issueID(ctx, in.ParentID)
		if err != nil {
			return hubsync.Item{}, fmt.Errorf("linear: parent: %w", err)
		}
		input["parentId"] = pid
	}
	if in.Status != "" || len(in.Labels) > 0 {
		var out struct {
			Team        *teamMeta `json:"team"`
			IssueLabels namedConn `json:"issueLabels"`
		}
		if err := a.do(ctx, "LinearTeamMeta", qTeamMeta, map[string]any{"id": team}, &out); err != nil {
			return hubsync.Item{}, err
		}
		if out.Team == nil {
			return hubsync.Item{}, fmt.Errorf("linear: team %q: %w", a.teamRef(), hubsync.ErrNotFound)
		}
		if in.Status != "" {
			sid, err := stateID(out.Team.States.Nodes, in.Status)
			if err != nil {
				return hubsync.Item{}, err
			}
			input["stateId"] = sid
		}
		if len(in.Labels) > 0 {
			ids := make([]string, 0, len(in.Labels))
			for _, l := range in.Labels {
				lid, err := labelID(out.Team.Labels.Nodes, out.IssueLabels.Nodes, l)
				if err != nil {
					return hubsync.Item{}, err
				}
				if !slices.Contains(ids, lid) {
					ids = append(ids, lid)
				}
			}
			input["labelIds"] = ids
		}
	}
	var out struct {
		IssueCreate struct {
			Success bool       `json:"success"`
			Issue   *issueNode `json:"issue"`
		} `json:"issueCreate"`
	}
	if err := a.do(ctx, "LinearIssueCreate", mIssueCreate, map[string]any{"input": input}, &out); err != nil {
		return hubsync.Item{}, err
	}
	if !out.IssueCreate.Success || out.IssueCreate.Issue == nil {
		return hubsync.Item{}, errors.New("linear LinearIssueCreate: the API reported no success")
	}
	return out.IssueCreate.Issue.item(), nil
}

// Update sets the title and/or the description; nil leaves a field alone.
func (a *Adapter) Update(ctx context.Context, id string, title, body *string) error {
	input := map[string]any{}
	if title != nil {
		input["title"] = *title
	}
	if body != nil {
		input["description"] = *body
	}
	if len(input) == 0 {
		return nil
	}
	return a.mutate(ctx, "LinearIssueUpdate", mIssueUpdate, "issueUpdate", map[string]any{"id": id, "input": input})
}

// SetLabels adds and removes labels by name. Labels resolve within the issue's team, then the
// workspace; every name is checked before anything is written. Adding a label the issue has, or
// removing one it lacks, is a no-op.
func (a *Adapter) SetLabels(ctx context.Context, id string, add, remove []string) error {
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	it, err := a.issueTeam(ctx, id)
	if err != nil {
		return err
	}
	var addIDs, removeIDs []string
	for _, l := range add {
		if _, has := findName(it.Labels, l); has {
			continue
		}
		lid, err := labelID(it.Team.Labels.Nodes, it.WSLbls, l)
		if err != nil {
			return err
		}
		if !slices.Contains(addIDs, lid) {
			addIDs = append(addIDs, lid)
		}
	}
	for _, l := range remove {
		if cur, has := findName(it.Labels, l); has && !slices.Contains(removeIDs, cur.ID) {
			removeIDs = append(removeIDs, cur.ID)
		}
	}
	for _, lid := range addIDs {
		if err := a.mutate(ctx, "LinearIssueAddLabel", mAddLabel, "issueAddLabel", map[string]any{"id": it.ID, "labelId": lid}); err != nil {
			return err
		}
	}
	for _, lid := range removeIDs {
		if err := a.mutate(ctx, "LinearIssueRemoveLabel", mRemoveLabel, "issueRemoveLabel", map[string]any{"id": it.ID, "labelId": lid}); err != nil {
			return err
		}
	}
	return nil
}

// issueNode is the LinearIssueFields fragment.
type issueNode struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	UpdatedAt   string `json:"updatedAt"`
	State       *struct {
		Name, Type string
	} `json:"state"`
	Assignee *struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Email       string `json:"email"`
	} `json:"assignee"`
	Parent *struct {
		ID string `json:"id"`
	} `json:"parent"`
	Labels   namedConn `json:"labels"`
	Comments struct {
		Nodes []struct {
			ID        string `json:"id"`
			Body      string `json:"body"`
			CreatedAt string `json:"createdAt"`
			User      *struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
			} `json:"user"`
		} `json:"nodes"`
	} `json:"comments"`
	Attachments struct {
		Nodes []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"nodes"`
	} `json:"attachments"`
}

type issueConn struct {
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
	Nodes []issueNode `json:"nodes"`
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func (n issueNode) item() hubsync.Item {
	it := hubsync.Item{
		ID: n.ID, Key: n.Identifier, URL: n.URL, Title: n.Title, Body: n.Description,
		Rev: n.UpdatedAt, UpdatedAt: parseTime(n.UpdatedAt),
	}
	if n.State != nil {
		it.Status = n.State.Name
		it.StatusCategory = Category(n.State.Type, n.State.Name)
	}
	if n.Assignee != nil {
		it.Assignee = firstNonEmpty(n.Assignee.Email, n.Assignee.DisplayName, n.Assignee.Name)
	}
	if n.Parent != nil {
		it.ParentID = n.Parent.ID
	}
	for _, l := range n.Labels.Nodes {
		it.Labels = append(it.Labels, l.Name)
	}
	for _, c := range n.Comments.Nodes {
		cm := hubsync.Comment{ID: c.ID, Body: c.Body, CreatedAt: parseTime(c.CreatedAt)}
		if c.User != nil {
			cm.Author = firstNonEmpty(c.User.DisplayName, c.User.Name)
		}
		it.Comments = append(it.Comments, cm)
	}
	sort.SliceStable(it.Comments, func(i, j int) bool { return it.Comments[i].CreatedAt.After(it.Comments[j].CreatedAt) })
	if len(it.Comments) > hubsync.MaxComments {
		it.Comments = it.Comments[:hubsync.MaxComments]
	}
	for _, at := range n.Attachments.Nodes {
		it.Attachments = append(it.Attachments, hubsync.Attachment{Name: at.Title, URL: at.URL})
	}
	return it
}
