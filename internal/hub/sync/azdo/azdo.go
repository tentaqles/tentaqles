// Package azdo is the Azure DevOps Boards adapter for hub board sync (spec B §7).
//
// It talks to the Azure DevOps REST API (api-version 7.1): WIQL for what changed, workitemsbatch
// to read items, JSON Patch to change state, title, description and tags, the comments API, and
// System.LinkTypes.Hierarchy-Reverse to make a child. The credential comes from the cred chain;
// its cli source is `az account get-access-token` under the identity's AZURE_CONFIG_DIR (AzToken),
// and a PAT from the keychain or env is sent as Basic.
//
// The manifest scope keys are org (required), project (required), board (optional: the team the
// WIQL runs under) and type (optional: the work item type Create uses when NewItem.Type is empty;
// default Task).
package azdo

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// Provider is the manifest tracker.provider this adapter serves.
const Provider = "azure-devops"

const (
	defaultBase = "https://dev.azure.com"
	defaultType = "Task"
	batchSize   = 200  // workitemsbatch takes at most 200 ids
	wiqlTop     = 1000 // items per flat ListChanged query; the watermark carries the rest to the next tick
	// linkChunk is how many parents one children-by-id link query names. Link queries carry no
	// $top (a capped link reply would drop children silently); ADO's own row limit answers an
	// error instead, and a small chunk keeps every reply far below it.
	linkChunk   = 100
	statesRetry = 5 * time.Minute
	fieldAccept = "Microsoft.VSTS.Common.AcceptanceCriteria"
	relParent   = "System.LinkTypes.Hierarchy-Reverse"
	relChild    = "System.LinkTypes.Hierarchy-Forward"
	relAttached = "AttachedFile"

	// requestTimeout bounds one HTTP call, reply body included, so a tracker that hangs after
	// the handshake cannot hold the sync loop.
	requestTimeout = 60 * time.Second
	// shortWait is the longest throttling delay do() waits out itself; longer ones fail fast
	// with a RateLimitError for the engine to schedule.
	shortWait = 10 * time.Second
	// clockSkew is taken off the local send time when a WIQL reply carries no asOf.
	clockSkew = 5 * time.Second
)

func init() {
	hubsync.Register(Provider, func(cfg hubsync.Config) (hubsync.Adapter, error) { return New(cfg) })
	cred.RegisterCLI(Provider, AzToken)
}

// Adapter is one Azure DevOps project.
type Adapter struct {
	cfg                hubsync.Config
	org, project, team string
	itemType           string
	me                 bool
	base               string // https://dev.azure.com/<org>
	rt                 http.RoundTripper
	now                func() time.Time
	sleep              func(context.Context, time.Duration) error
	timeout            time.Duration

	mu      gosync.Mutex
	client  *http.Client
	src     cred.Source
	blocked time.Time
	account string // unique name from connectionData; empty when it carries none
	display string

	statesMu    gosync.Mutex
	states      map[string]map[string]string // lower(type) -> lower(state) -> category
	byState     map[string]string            // lower(state) -> category, first type wins
	statesTried time.Time
}

var _ hubsync.Adapter = (*Adapter)(nil)

// New builds the adapter. Scope org and project are required. The credential is resolved on the
// first call when cfg.Transport is nil.
func New(cfg hubsync.Config) (*Adapter, error) {
	org, project := strings.TrimSpace(cfg.Scope["org"]), strings.TrimSpace(cfg.Scope["project"])
	if org == "" || project == "" {
		return nil, fmt.Errorf("%s: tracker.scope needs org and project", Provider)
	}
	root := strings.TrimRight(cfg.BaseURL, "/")
	if root == "" {
		root = defaultBase
	}
	typ := strings.TrimSpace(cfg.Scope["type"])
	if typ == "" {
		typ = defaultType
	}
	return &Adapter{cfg: cfg, org: org, project: project, team: strings.TrimSpace(cfg.Scope["board"]),
		itemType: typ, me: cfg.AssignedTo != "anyone", base: root + "/" + url.PathEscape(org), now: time.Now,
		sleep: sleepCtx, timeout: requestTimeout}, nil
}

func (a *Adapter) Name() string { return Provider }

// ---- URLs ----

func (a *Adapter) projURL(path string, q url.Values) string {
	u := a.base + "/" + url.PathEscape(a.project) + "/_apis/" + path
	return withQuery(u, q)
}

// wiqlURL is the WIQL endpoint; top > 0 caps the reply rows (flat queries only).
func (a *Adapter) wiqlURL(top int) string {
	u := a.base + "/" + url.PathEscape(a.project)
	if a.team != "" {
		u += "/" + url.PathEscape(a.team)
	}
	q := url.Values{"timePrecision": {"true"}}
	if top > 0 {
		q.Set("$top", strconv.Itoa(top))
	}
	return withQuery(u+"/_apis/wit/wiql", q)
}

func withQuery(u string, q url.Values) string {
	if q == nil {
		q = url.Values{}
	}
	if q.Get("api-version") == "" {
		q.Set("api-version", apiVersion)
	}
	return u + "?" + q.Encode()
}

func (a *Adapter) itemURL(id string) string {
	return a.base + "/" + url.PathEscape(a.project) + "/_workitems/edit/" + id
}

var reID = regexp.MustCompile(`^[1-9][0-9]{0,11}$`)

func checkID(id string) error {
	if !reID.MatchString(id) {
		return fmt.Errorf("%s: %q: %w", Provider, id, errBadID)
	}
	return nil
}

// ---- Check / Statuses ----

// Check verifies the credential (connectionData) and the scope's project, and returns the
// signed-in account: its unique name (the value System.AssignedTo takes), or its display name
// when connectionData carries no Account property.
func (a *Adapter) Check(ctx context.Context) (string, error) {
	acct, display, err := a.whoami(ctx)
	if err != nil {
		return "", err
	}
	if acct == "" {
		acct = display
	}
	var p struct {
		ID string `json:"id"`
	}
	if err := a.do(ctx, "GET", withQuery(a.base+"/_apis/projects/"+url.PathEscape(a.project), nil), "", nil, &p); err != nil {
		return "", fmt.Errorf("project %s/%s: %w", a.org, a.project, err)
	}
	return acct, nil
}

// whoami reads connectionData once: the account's unique name (empty when the reply has no
// Account property) and its display name.
func (a *Adapter) whoami(ctx context.Context) (acct, display string, err error) {
	a.mu.Lock()
	acct, display = a.account, a.display
	a.mu.Unlock()
	if acct != "" || display != "" {
		return acct, display, nil
	}
	var cd struct {
		AuthenticatedUser struct {
			ID                  string `json:"id"`
			ProviderDisplayName string `json:"providerDisplayName"`
			Properties          struct {
				Account struct {
					Value string `json:"$value"`
				} `json:"Account"`
			} `json:"properties"`
		} `json:"authenticatedUser"`
	}
	if err := a.do(ctx, "GET", a.base+"/_apis/connectionData", "", nil, &cd); err != nil {
		return "", "", err
	}
	u := cd.AuthenticatedUser
	acct, display = strings.TrimSpace(u.Properties.Account.Value), strings.TrimSpace(u.ProviderDisplayName)
	if acct == "" && display == "" || u.ID == "" {
		a.mu.Lock()
		src := a.src
		a.mu.Unlock()
		return "", "", notAuthorized(src, http.StatusOK)
	}
	a.mu.Lock()
	a.account, a.display = acct, display
	a.mu.Unlock()
	return acct, display, nil
}

// categoryOf maps an Azure DevOps state category to a hub lane kind.
func categoryOf(c string) string {
	switch strings.ToLower(c) {
	case "proposed":
		return hubsync.CategoryTodo
	case "inprogress":
		return hubsync.CategoryActive
	case "resolved":
		return hubsync.CategoryReview
	case "completed", "removed":
		return hubsync.CategoryDone
	}
	return hubsync.CategoryBacklog
}

// guessCategory is the fallback when the process states could not be read.
func guessCategory(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "new", "proposed", "to do", "todo", "approved":
		return hubsync.CategoryTodo
	case "active", "in progress", "committed", "doing", "open":
		return hubsync.CategoryActive
	case "resolved", "in review", "review":
		return hubsync.CategoryReview
	case "closed", "done", "removed", "completed":
		return hubsync.CategoryDone
	}
	return hubsync.CategoryBacklog
}

// Statuses lists every state of the project's enabled work item types, once per name (the first
// type that has it decides its category).
func (a *Adapter) Statuses(ctx context.Context) ([]hubsync.Status, error) {
	var res struct {
		Value []struct {
			Name       string `json:"name"`
			IsDisabled bool   `json:"isDisabled"`
			States     []struct {
				Name     string `json:"name"`
				Category string `json:"category"`
			} `json:"states"`
		} `json:"value"`
	}
	if err := a.do(ctx, "GET", a.projURL("wit/workitemtypes", nil), "", nil, &res); err != nil {
		return nil, err
	}
	states := map[string]map[string]string{}
	byState := map[string]string{}
	var out []hubsync.Status
	for _, t := range res.Value {
		if t.IsDisabled {
			continue
		}
		m := map[string]string{}
		for _, s := range t.States {
			cat := categoryOf(s.Category)
			key := strings.ToLower(s.Name)
			m[key] = cat
			if _, seen := byState[key]; !seen {
				byState[key] = cat
				out = append(out, hubsync.Status{Name: s.Name, Category: cat})
			}
		}
		states[strings.ToLower(t.Name)] = m
	}
	a.statesMu.Lock()
	a.states, a.byState, a.statesTried = states, byState, a.now()
	a.statesMu.Unlock()
	return out, nil
}

// category is the lane kind of a state of a work item type, reading the process states once.
func (a *Adapter) category(ctx context.Context, typ, state string) string {
	a.statesMu.Lock()
	need := a.states == nil && (a.statesTried.IsZero() || a.now().Sub(a.statesTried) > statesRetry)
	if need {
		a.statesTried = a.now()
	}
	a.statesMu.Unlock()
	if need {
		_, _ = a.Statuses(ctx) // on error the name decides below
	}
	a.statesMu.Lock()
	defer a.statesMu.Unlock()
	key := strings.ToLower(state)
	if c, ok := a.states[strings.ToLower(typ)][key]; ok {
		return c
	}
	if c, ok := a.byState[key]; ok {
		return c
	}
	return guessCategory(state)
}

// ---- reading ----

type workItem struct {
	ID        int            `json:"id"`
	Rev       int            `json:"rev"`
	Fields    map[string]any `json:"fields"`
	Relations []struct {
		Rel        string         `json:"rel"`
		URL        string         `json:"url"`
		Attributes map[string]any `json:"attributes"`
	} `json:"relations"`
}

func str(f map[string]any, k string) string {
	switch v := f[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// identity reads an identity field: an IdentityRef object (uniqueName, else displayName) or the
// older "Name <account>" string.
func identity(v any, preferDisplay bool) string {
	switch x := v.(type) {
	case map[string]any:
		u, _ := x["uniqueName"].(string)
		d, _ := x["displayName"].(string)
		if preferDisplay && d != "" || u == "" {
			return d
		}
		return u
	case string:
		if i, j := strings.LastIndex(x, "<"), strings.LastIndex(x, ">"); i >= 0 && j > i && !preferDisplay {
			return x[i+1 : j]
		}
		if i := strings.LastIndex(x, "<"); i > 0 && preferDisplay {
			return strings.TrimSpace(x[:i])
		}
		return x
	}
	return ""
}

func splitTags(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ";") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func lastID(u string) string {
	u = strings.TrimRight(u, "/")
	if i := strings.LastIndex(u, "/"); i >= 0 && reID.MatchString(u[i+1:]) {
		return u[i+1:]
	}
	return ""
}

func (a *Adapter) toItem(ctx context.Context, w workItem) hubsync.Item {
	f := w.Fields
	if f == nil {
		f = map[string]any{}
	}
	id := strconv.Itoa(w.ID)
	typ, state := str(f, "System.WorkItemType"), str(f, "System.State")
	it := hubsync.Item{
		ID: id, Key: "#" + id, URL: a.itemURL(id),
		Title: str(f, "System.Title"), Body: HTMLToText(str(f, "System.Description")),
		Status: state, StatusCategory: a.category(ctx, typ, state),
		Assignee: identity(f["System.AssignedTo"], false), Rev: strconv.Itoa(w.Rev),
		Labels: splitTags(str(f, "System.Tags")), Acceptance: HTMLToText(str(f, fieldAccept)),
	}
	if p := str(f, "System.Parent"); reID.MatchString(p) {
		it.ParentID = p
	}
	for _, r := range w.Relations {
		switch r.Rel {
		case relParent:
			if it.ParentID == "" {
				it.ParentID = lastID(r.URL)
			}
		case relAttached:
			name, _ := r.Attributes["name"].(string)
			it.Attachments = append(it.Attachments, hubsync.Attachment{Name: name, URL: r.URL})
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, str(f, "System.ChangedDate")); err == nil {
		it.UpdatedAt = t.UTC()
	}
	return it
}

func commentCount(w workItem) int {
	n, _ := w.Fields["System.CommentCount"].(float64)
	return int(n)
}

// comments reads the newest comments of an item, newest first, capped at MaxComments.
func (a *Adapter) comments(ctx context.Context, id string) ([]hubsync.Comment, error) {
	var res struct {
		Comments []struct {
			ID          int            `json:"id"`
			Text        string         `json:"text"`
			IsDeleted   bool           `json:"isDeleted"`
			CreatedBy   map[string]any `json:"createdBy"`
			CreatedDate string         `json:"createdDate"`
		} `json:"comments"`
	}
	q := url.Values{"$top": {strconv.Itoa(hubsync.MaxComments)}, "order": {"desc"}, "api-version": {commentsAPIVersion}}
	if err := a.do(ctx, "GET", a.projURL("wit/workitems/"+id+"/comments", q), "", nil, &res); err != nil {
		return nil, err
	}
	var out []hubsync.Comment
	for _, c := range res.Comments {
		if c.IsDeleted {
			continue
		}
		cm := hubsync.Comment{ID: strconv.Itoa(c.ID), Author: identity(c.CreatedBy, true), Body: HTMLToText(c.Text)}
		if t, err := time.Parse(time.RFC3339Nano, c.CreatedDate); err == nil {
			cm.CreatedAt = t.UTC()
		}
		out = append(out, cm)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > hubsync.MaxComments {
		out = out[:hubsync.MaxComments]
	}
	return out, nil
}

// full is toItem plus the comments (only fetched when the item has any).
func (a *Adapter) full(ctx context.Context, w workItem) (hubsync.Item, error) {
	it := a.toItem(ctx, w)
	if commentCount(w) > 0 {
		cs, err := a.comments(ctx, it.ID)
		if err != nil {
			return hubsync.Item{}, err
		}
		it.Comments = cs
	}
	return it, nil
}

// Get reads one work item with its relations and newest comments.
func (a *Adapter) Get(ctx context.Context, id string) (hubsync.Item, error) {
	if err := checkID(id); err != nil {
		return hubsync.Item{}, err
	}
	var w workItem
	if err := a.do(ctx, "GET", a.projURL("wit/workitems/"+id, url.Values{"$expand": {"Relations"}}), "", nil, &w); err != nil {
		return hubsync.Item{}, err
	}
	return a.full(ctx, w)
}

// wiqlTime formats a WIQL date with milliseconds (timePrecision is on), truncated, so a >=
// query re-reads an edge item rather than skipping it.
func wiqlTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.999Z") }

func (a *Adapter) wiql(ctx context.Context, query string, top int, out any) error {
	return a.do(ctx, "POST", a.wiqlURL(top), "application/json", map[string]string{"query": query}, out)
}

// ListChanged runs WIQL on System.ChangedDate: the items assigned to @Me (all items in scope for
// assigned_to anyone) plus, for me, the children of those items: the ones that changed since,
// and every child of an item that came back (a parent just assigned brings its old tasks). It
// returns them oldest change first, and the newest ChangedDate seen as the watermark, capped at
// the time the flat query ran (its asOf) so a change landing between the query and the batch
// read is not skipped; since when nothing changed.
func (a *Adapter) ListChanged(ctx context.Context, since time.Time) ([]hubsync.Item, time.Time, error) {
	changed, target := "", ""
	if !since.IsZero() {
		changed = " AND [System.ChangedDate] >= '" + wiqlTime(since) + "'"
		target = " AND ([Target].[System.ChangedDate] >= '" + wiqlTime(since) + "')"
	}
	me := ""
	if a.me {
		me = " AND [System.AssignedTo] = @Me"
	}
	var flat struct {
		AsOf      string `json:"asOf"`
		WorkItems []struct {
			ID int `json:"id"`
		} `json:"workItems"`
	}
	sent := a.now()
	q := "SELECT [System.Id] FROM WorkItems WHERE [System.TeamProject] = @project" + me + changed +
		" ORDER BY [System.ChangedDate] ASC"
	if err := a.wiql(ctx, q, wiqlTop, &flat); err != nil {
		return nil, since, err
	}
	var ids []int
	seen := map[int]bool{}
	for _, w := range flat.WorkItems {
		if !seen[w.ID] {
			seen[w.ID] = true
			ids = append(ids, w.ID)
		}
	}
	flatIDs := map[int]bool{}
	for _, id := range ids {
		flatIDs[id] = true
	}
	capped := len(flat.WorkItems) >= wiqlTop
	if a.me {
		parents := slices.Clone(ids)
		addChildren := func(lq string) error {
			var links struct {
				WorkItemRelations []struct {
					Rel    *string           `json:"rel"`
					Target *struct{ ID int } `json:"target"`
				} `json:"workItemRelations"`
			}
			if err := a.wiql(ctx, lq, 0, &links); err != nil {
				return err
			}
			for _, r := range links.WorkItemRelations {
				if r.Rel == nil || r.Target == nil || seen[r.Target.ID] {
					continue
				}
				seen[r.Target.ID] = true
				ids = append(ids, r.Target.ID)
			}
			return nil
		}
		// Children that changed since, under any of my items (changed or not). On the first
		// sync there is no date to bound it, so it is skipped: the per-parent query below reads
		// the children of every item the flat query returned, and later ticks reach the rest.
		if !since.IsZero() {
			lq := "SELECT [System.Id] FROM WorkItemLinks WHERE ([Source].[System.TeamProject] = @project AND " +
				"[Source].[System.AssignedTo] = @Me) AND ([System.Links.LinkType] = '" + relChild + "')" + target +
				" MODE (MustContain)"
			if err := addChildren(lq); err != nil {
				return nil, since, err
			}
		}
		// Every child of the items that came back, changed or not (a parent just assigned
		// brings its old tasks).
		for chunk := range slices.Chunk(parents, linkChunk) {
			in := make([]string, len(chunk))
			for i, id := range chunk {
				in[i] = strconv.Itoa(id)
			}
			cq := "SELECT [System.Id] FROM WorkItemLinks WHERE ([Source].[System.Id] IN (" + strings.Join(in, ",") +
				")) AND ([System.Links.LinkType] = '" + relChild + "') MODE (MustContain)"
			if err := addChildren(cq); err != nil {
				return nil, since, err
			}
		}
	}
	ws, err := a.batch(ctx, ids)
	if err != nil {
		return nil, since, err
	}
	items := make([]hubsync.Item, 0, len(ws))
	mark := since
	for _, w := range ws {
		it, err := a.full(ctx, w)
		if err != nil {
			return nil, since, err
		}
		items = append(items, it)
		// A capped flat query leaves items for the next tick: only its own items move the mark.
		if it.UpdatedAt.After(mark) && (!capped || flatIDs[w.ID]) {
			mark = it.UpdatedAt
		}
	}
	// Nothing after the flat query ran is known to be complete: cap the mark there. Re-reading
	// an item next tick is harmless (the query uses >=).
	ceiling := sent.Add(-clockSkew)
	if t, err := time.Parse(time.RFC3339Nano, flat.AsOf); err == nil {
		ceiling = t.UTC()
	}
	if mark.After(ceiling) {
		mark = ceiling
	}
	if mark.Before(since) {
		mark = since
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].UpdatedAt.Before(items[j].UpdatedAt) })
	return items, mark, nil
}

// batch reads work items by id through workitemsbatch, 200 at a time. Deleted ids are skipped.
func (a *Adapter) batch(ctx context.Context, ids []int) ([]workItem, error) {
	var out []workItem
	for chunk := range slices.Chunk(ids, batchSize) {
		var res struct {
			Value []*workItem `json:"value"`
		}
		in := map[string]any{"ids": chunk, "$expand": "Relations", "errorPolicy": "Omit"}
		if err := a.do(ctx, "POST", a.projURL("wit/workitemsbatch", nil), "application/json", in, &res); err != nil {
			return nil, err
		}
		for _, w := range res.Value {
			if w != nil {
				out = append(out, *w)
			}
		}
	}
	return out, nil
}

// ---- writing ----

type patchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

func set(path string, v any) patchOp { return patchOp{Op: "add", Path: path, Value: v} }

func (a *Adapter) patch(ctx context.Context, id string, ops []patchOp, out any) error {
	if err := checkID(id); err != nil {
		return err
	}
	return a.do(ctx, "PATCH", a.projURL("wit/workitems/"+id, nil), jsonPatch, ops, out)
}

// Move sets System.State to the tracker state name.
func (a *Adapter) Move(ctx context.Context, id, status string) error {
	if strings.TrimSpace(status) == "" {
		return fmt.Errorf("%s: move %s: empty state", Provider, id)
	}
	return a.patch(ctx, id, []patchOp{set("/fields/System.State", status)}, nil)
}

// Comment posts markdown as an HTML comment.
func (a *Adapter) Comment(ctx context.Context, id, markdown string) error {
	if err := checkID(id); err != nil {
		return err
	}
	q := url.Values{"api-version": {commentsAPIVersion}}
	return a.do(ctx, "POST", a.projURL("wit/workitems/"+id+"/comments", q), "application/json",
		map[string]string{"text": MarkdownToHTML(markdown)}, nil)
}

// Create makes a work item of in.Type (else scope type, else Task). in.ParentID makes it a child
// (System.LinkTypes.Hierarchy-Reverse to the parent). With assigned_to me it is assigned to the
// signed-in account's unique name, so it mirrors back; when connectionData gave only a display
// name (ambiguous, and Azure DevOps may reject it) AssignedTo is left unset.
func (a *Adapter) Create(ctx context.Context, in hubsync.NewItem) (hubsync.Item, error) {
	if strings.TrimSpace(in.Title) == "" {
		return hubsync.Item{}, fmt.Errorf("%s: create: a title is required", Provider)
	}
	typ := strings.TrimSpace(in.Type)
	if typ == "" {
		typ = a.itemType
	}
	ops := []patchOp{set("/fields/System.Title", in.Title)}
	if in.Body != "" {
		ops = append(ops, set("/fields/System.Description", MarkdownToHTML(in.Body)))
	}
	if in.Acceptance != "" {
		ops = append(ops, set("/fields/"+fieldAccept, MarkdownToHTML(in.Acceptance)))
	}
	if len(in.Labels) > 0 {
		ops = append(ops, set("/fields/System.Tags", strings.Join(in.Labels, "; ")))
	}
	if in.Status != "" {
		ops = append(ops, set("/fields/System.State", in.Status))
	}
	if a.me {
		acct, _, err := a.whoami(ctx)
		if err != nil {
			return hubsync.Item{}, err
		}
		if acct != "" {
			ops = append(ops, set("/fields/System.AssignedTo", acct))
		}
	}
	if in.ParentID != "" {
		if err := checkID(in.ParentID); err != nil {
			return hubsync.Item{}, err
		}
		ops = append(ops, set("/relations/-", map[string]string{"rel": relParent,
			"url": a.base + "/_apis/wit/workItems/" + in.ParentID}))
	}
	var w workItem
	if err := a.do(ctx, "POST", a.projURL("wit/workitems/$"+url.PathEscape(typ), nil), jsonPatch, ops, &w); err != nil {
		return hubsync.Item{}, err
	}
	return a.toItem(ctx, w), nil
}

// Update replaces the title and/or the description (markdown, sent as HTML). Both nil is a no-op;
// a blank title is refused before any call (Azure DevOps would answer 400).
func (a *Adapter) Update(ctx context.Context, id string, title, body *string) error {
	var ops []patchOp
	if title != nil {
		if strings.TrimSpace(*title) == "" {
			return fmt.Errorf("%s: update %s: a title cannot be blank", Provider, id)
		}
		ops = append(ops, set("/fields/System.Title", *title))
	}
	if body != nil {
		ops = append(ops, set("/fields/System.Description", MarkdownToHTML(*body)))
	}
	if len(ops) == 0 {
		return checkID(id)
	}
	return a.patch(ctx, id, ops, nil)
}

// SetLabels adds and removes tags (case-insensitive). It reads the current tags and patches with
// a rev test, so a concurrent edit fails instead of being overwritten. No change, no patch.
func (a *Adapter) SetLabels(ctx context.Context, id string, add, remove []string) error {
	if err := checkID(id); err != nil {
		return err
	}
	var w workItem
	if err := a.do(ctx, "GET", a.projURL("wit/workitems/"+id, url.Values{"fields": {"System.Tags"}}), "", nil, &w); err != nil {
		return err
	}
	cur := splitTags(str(w.Fields, "System.Tags"))
	has := func(tags []string, t string) bool {
		return slices.ContainsFunc(tags, func(x string) bool { return strings.EqualFold(x, t) })
	}
	var next []string
	for _, t := range cur {
		if !has(remove, t) {
			next = append(next, t)
		}
	}
	for _, t := range add {
		if t = strings.TrimSpace(t); t != "" && !has(next, t) && !has(remove, t) {
			next = append(next, t)
		}
	}
	if slices.Equal(cur, next) {
		return nil
	}
	return a.patch(ctx, id, []patchOp{{Op: "test", Path: "/rev", Value: w.Rev},
		set("/fields/System.Tags", strings.Join(next, "; "))}, nil)
}
