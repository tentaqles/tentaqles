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
	// defaultCommentBudget is how many flat items with comments one ListChanged reads the
	// comments of (one request each: ADO has no cross-item comment read); the page stops before
	// the next one and the watermark carries the rest to the next tick.
	defaultCommentBudget = 200
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
	commentBudget      int // comment reads one ListChanged may make for its flat items

	mu      gosync.Mutex
	client  *http.Client
	src     cred.Source
	blocked time.Time
	account string // unique name from connectionData; empty when it carries none
	display string
	// tie is where the last truncated flat page ended: its ChangedDate (the mark it returned)
	// and the id of its last item. A ListChanged from exactly that mark pages on by id, so a page
	// of items sharing one ChangedDate cannot pin the mark.
	tie struct {
		at time.Time
		id int
	}

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
		sleep: sleepCtx, timeout: requestTimeout, commentBudget: defaultCommentBudget}, nil
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

// tagSemi stands in for a ';' inside a hub label: System.Tags is ';'-separated, so a raw ';'
// would split one label into two tags and SetLabels would never converge. It is the fullwidth
// semicolon, turned back into ';' on the way in.
const tagSemi = "；"

// splitTags reads System.Tags as hub labels.
func splitTags(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ";") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, strings.ReplaceAll(t, tagSemi, ";"))
		}
	}
	return out
}

// joinTags writes hub labels as System.Tags.
func joinTags(labels []string) string {
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = strings.ReplaceAll(l, ";", tagSemi)
	}
	return strings.Join(out, "; ")
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
// returns them oldest change first, with a watermark; since when nothing changed.
//
// Every item is read as of the flat query's snapshot (its asOf), so the watermark always comes
// from the ChangedDates the query ordered by, never from an edit that landed after it. A flat
// page is truncated when the query hits $top or the comment budget runs out: then only its own
// items, up to the last one kept, move the mark, and a later tick from that mark pages on past
// it by id (ORDER BY ChangedDate, Id), so items sharing one ChangedDate never pin the mark.
// Otherwise the newest ChangedDate seen is the mark, capped at the query's asOf.
func (a *Adapter) ListChanged(ctx context.Context, since time.Time) ([]hubsync.Item, time.Time, error) {
	changed, target := "", ""
	if !since.IsZero() {
		ts := wiqlTime(since)
		a.mu.Lock()
		tie := a.tie
		a.mu.Unlock()
		if tie.id > 0 && tie.at.Equal(since) {
			changed = " AND ([System.ChangedDate] > '" + ts + "' OR ([System.ChangedDate] = '" + ts +
				"' AND [System.Id] > " + strconv.Itoa(tie.id) + "))"
		} else {
			changed = " AND [System.ChangedDate] >= '" + ts + "'"
		}
		target = " AND ([Target].[System.ChangedDate] >= '" + ts + "')"
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
		" ORDER BY [System.ChangedDate] ASC, [System.Id] ASC"
	if err := a.wiql(ctx, q, wiqlTop, &flat); err != nil {
		return nil, since, err
	}
	var flatIDs, ids []int // flatIDs in the query's order
	seen := map[int]bool{}
	for _, w := range flat.WorkItems {
		if !seen[w.ID] {
			seen[w.ID] = true
			flatIDs = append(flatIDs, w.ID)
		}
	}
	ids = slices.Clone(flatIDs)
	capped := len(flat.WorkItems) >= wiqlTop
	changedKid := map[int]bool{} // children that changed since: always returned
	parentOf := map[int][]int{}  // child -> the flat parents that brought it
	if a.me {
		addChildren := func(lq string, fromChanged bool) error {
			var links struct {
				WorkItemRelations []struct {
					Rel    *string           `json:"rel"`
					Source *struct{ ID int } `json:"source"`
					Target *struct{ ID int } `json:"target"`
				} `json:"workItemRelations"`
			}
			if err := a.wiql(ctx, lq, 0, &links); err != nil {
				return err
			}
			for _, r := range links.WorkItemRelations {
				if r.Rel == nil || r.Target == nil {
					continue
				}
				kid := r.Target.ID
				if fromChanged {
					changedKid[kid] = true
				} else if r.Source != nil {
					parentOf[kid] = append(parentOf[kid], r.Source.ID)
				}
				if !seen[kid] {
					seen[kid] = true
					ids = append(ids, kid)
				}
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
			if err := addChildren(lq, true); err != nil {
				return nil, since, err
			}
		}
		// Every child of the items that came back, changed or not (a parent just assigned
		// brings its old tasks).
		for chunk := range slices.Chunk(flatIDs, linkChunk) {
			in := make([]string, len(chunk))
			for i, id := range chunk {
				in[i] = strconv.Itoa(id)
			}
			cq := "SELECT [System.Id] FROM WorkItemLinks WHERE ([Source].[System.Id] IN (" + strings.Join(in, ",") +
				")) AND ([System.Links.LinkType] = '" + relChild + "') MODE (MustContain)"
			if err := addChildren(cq, false); err != nil {
				return nil, since, err
			}
		}
	}
	// Nothing after the flat query ran is known to be complete: the mark never passes it.
	ceiling := sent.Add(-clockSkew)
	asOf := ""
	if t, err := time.Parse(time.RFC3339Nano, flat.AsOf); err == nil {
		ceiling, asOf = t.UTC(), flat.AsOf
	}
	ws, err := a.batch(ctx, ids, asOf)
	if err != nil {
		return nil, since, err
	}
	byID := make(map[int]workItem, len(ws))
	for _, w := range ws {
		byID[w.ID] = w
	}

	// The flat items, in the query's order, while the comment budget lasts.
	var items []hubsync.Item
	kept := map[int]bool{}
	var last hubsync.Item // the last flat item kept
	reads := 0
	for _, id := range flatIDs {
		w, ok := byID[id]
		if !ok {
			continue // deleted since the query
		}
		if commentCount(w) > 0 {
			if reads >= a.commentBudget && len(kept) > 0 {
				capped = true
				break
			}
			reads++
		}
		it, err := a.full(ctx, w)
		if err != nil {
			return nil, since, err
		}
		items, kept[id], last = append(items, it), true, it
	}
	mark := since
	if capped {
		// Only the page's own items, read as of the query, move the mark. An item whose read
		// is newer than the query (an edit after it, when asOf was not honoured) does not:
		// a lower mark only re-reads, a higher one could skip the rest of the page.
		for _, it := range items {
			if it.UpdatedAt.After(mark) && !it.UpdatedAt.After(ceiling) {
				mark = it.UpdatedAt
			}
		}
	}
	// The children: the ones that changed since, and those of the flat items kept.
	for _, id := range ids[len(flatIDs):] {
		w, ok := byID[id]
		if !ok || capped && !changedKid[id] && !slices.ContainsFunc(parentOf[id], func(p int) bool { return kept[p] }) {
			continue
		}
		it, err := a.full(ctx, w)
		if err != nil {
			return nil, since, err
		}
		items = append(items, it)
	}
	if !capped {
		for _, it := range items {
			if it.UpdatedAt.After(mark) {
				mark = it.UpdatedAt
			}
		}
		if mark.After(ceiling) {
			mark = ceiling
		}
	}
	if mark.Before(since) {
		mark = since
	}
	a.mu.Lock()
	a.tie.at, a.tie.id = time.Time{}, 0
	if capped && last.ID != "" && last.UpdatedAt.Equal(mark) {
		a.tie.at, a.tie.id = mark, mustAtoi(last.ID)
	}
	a.mu.Unlock()
	sort.SliceStable(items, func(i, j int) bool { return items[i].UpdatedAt.Before(items[j].UpdatedAt) })
	return items, mark, nil
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// batch reads work items by id through workitemsbatch, 200 at a time, as of asOf when set (the
// flat query's snapshot). Deleted ids are skipped.
func (a *Adapter) batch(ctx context.Context, ids []int, asOf string) ([]workItem, error) {
	var out []workItem
	for chunk := range slices.Chunk(ids, batchSize) {
		var res struct {
			Value []*workItem `json:"value"`
		}
		in := map[string]any{"ids": chunk, "$expand": "Relations", "errorPolicy": "Omit"}
		if asOf != "" {
			in["asOf"] = asOf
		}
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
// name (ambiguous, and Azure DevOps may reject it) AssignedTo is left unset. in.Status is set by
// a patch after the create; when that fails the item exists, and the error is a
// *hubsync.PartialCreateError carrying it.
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
	var labels []string
	for _, l := range in.Labels {
		if l = strings.TrimSpace(l); l != "" {
			labels = append(labels, l)
		}
	}
	if len(labels) > 0 {
		ops = append(ops, set("/fields/System.Tags", joinTags(labels)))
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
	if w.ID <= 0 {
		return hubsync.Item{}, fmt.Errorf("%s: create: the reply names no work item", Provider)
	}
	it := a.toItem(ctx, w)
	// The state is set in its own patch: a process that refuses it as an initial state must not
	// block the create, and the item that now exists is reported with the failure.
	if st := strings.TrimSpace(in.Status); st != "" && !strings.EqualFold(st, it.Status) {
		var moved workItem
		if err := a.patch(ctx, it.ID, []patchOp{set("/fields/System.State", st)}, &moved); err != nil {
			return hubsync.Item{}, &hubsync.PartialCreateError{Item: it, Err: fmt.Errorf("set state %q: %w", st, err)}
		}
		if moved.ID == w.ID {
			it = a.toItem(ctx, moved)
		}
	}
	return it, nil
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
		set("/fields/System.Tags", joinTags(next))}, nil)
}
