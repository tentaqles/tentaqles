// Package asana is the board-sync adapter for Asana (spec B §7), over the REST API at
// https://app.asana.com/api/1.0.
//
// Scope: tracker.scope.org is the Asana workspace gid and tracker.scope.board the project gid
// (tracker.scope.project is read when board is unset). A project's sections are its statuses:
// Move adds the task to the section named like the status and sets completed to whether that
// section is done; StatusCategory comes from the section name (see categoryOf) unless
// tracker.lanes overrides it; a completed task is always done. Comments are stories, children are subtasks, and labels are workspace tags.
//
// Auth: Asana has no CLI login, so the cred resolver chain yields the keychain entry
// (tentaqles / <ws>/asana) and then the personal access token in the manifest's tracker.env,
// sent as a Bearer token. The token is fetched per request and never logged.
package asana

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// Provider is the tracker id in the manifest and the adapter registry.
const Provider = "asana"

func init() {
	hubsync.Register(Provider, func(cfg hubsync.Config) (hubsync.Adapter, error) { return NewAdapter(cfg) })
}

// taskFields are the opt_fields every task read asks for.
const taskFields = "name,notes,html_notes,completed,modified_at,permalink_url,assignee.name,assignee.email," +
	"parent.name,tags.name,memberships.project.name,memberships.section.name," +
	"custom_fields.name,custom_fields.display_value,custom_fields.text_value,num_subtasks"

// Adapter talks to one Asana workspace and project.
type Adapter struct {
	cfg       hubsync.Config
	base      string
	http      *http.Client
	workspace string // scope.org: workspace gid ("" = none)
	project   string // scope.board (or scope.project): project gid ("" = none)
	now       func() time.Time
	maxPages  int // page cap per list call (tests lower it)

	mu      gosync.Mutex
	parents []string           // mirrored parent gids whose subtasks ListChanged polls (SetMirroredParents)
	cache   map[string]details // task gid -> comments and attachments read at a Rev
}

// details are a task's comments and attachments as read at one Rev (its modified_at). A new
// comment or attachment changes modified_at, so a task listed again at the same Rev reuses them.
type details struct {
	rev         string
	comments    []hubsync.Comment
	attachments []hubsync.Attachment
}

// maxCached bounds the details cache; past it an arbitrary entry is dropped.
const maxCached = 2000

func (a *Adapter) remember(it hubsync.Item) {
	if it.Rev == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cache == nil {
		a.cache = map[string]details{}
	}
	if _, ok := a.cache[it.ID]; !ok && len(a.cache) >= maxCached {
		for k := range a.cache {
			delete(a.cache, k)
			break
		}
	}
	a.cache[it.ID] = details{rev: it.Rev, comments: it.Comments, attachments: it.Attachments}
}

// cached fills it from the cache when it was read at the same Rev.
func (a *Adapter) cached(it *hubsync.Item) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	d, ok := a.cache[it.ID]
	if !ok || it.Rev == "" || d.rev != it.Rev {
		return false
	}
	it.Comments, it.Attachments = slices.Clone(d.comments), slices.Clone(d.attachments)
	return true
}

var _ hubsync.Adapter = (*Adapter)(nil)

// NewAdapter builds the adapter. Scope ids must be Asana gids. With no Transport in cfg, the
// credential is resolved through cred.Resolve on the first request.
func NewAdapter(cfg hubsync.Config) (*Adapter, error) {
	if cfg.Provider == "" {
		cfg.Provider = Provider
	}
	a := &Adapter{cfg: cfg, base: strings.TrimRight(cfg.BaseURL, "/"), now: time.Now, maxPages: maxPages}
	if a.base == "" {
		a.base = DefaultBaseURL
	}
	var err error
	if v := strings.TrimSpace(cfg.Scope["org"]); v != "" {
		if a.workspace, err = gid("tracker.scope.org (workspace)", v); err != nil {
			return nil, err
		}
	}
	p := strings.TrimSpace(cfg.Scope["board"])
	if p == "" {
		p = strings.TrimSpace(cfg.Scope["project"])
	}
	if p != "" {
		if a.project, err = gid("tracker.scope.board (project)", p); err != nil {
			return nil, err
		}
	}
	t := cfg.Transport
	if t == nil {
		req := cfg.CredRequest()
		req.Provider = Provider
		t = &lazyTransport{req: req}
	}
	a.http = cred.Client(t, nil)
	a.http.Timeout = requestTimeout
	return a, nil
}

func (a *Adapter) Name() string { return Provider }

func (a *Adapter) anyone() bool { return a.cfg.AssignedTo == "anyone" }

type ref struct {
	GID   string `json:"gid"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type membership struct {
	Project ref  `json:"project"`
	Section *ref `json:"section"`
}

type task struct {
	GID          string       `json:"gid"`
	Name         string       `json:"name"`
	Notes        string       `json:"notes"`
	HTMLNotes    string       `json:"html_notes"`
	Completed    bool         `json:"completed"`
	ModifiedAt   string       `json:"modified_at"`
	Permalink    string       `json:"permalink_url"`
	Assignee     *ref         `json:"assignee"`
	Parent       *ref         `json:"parent"`
	Tags         []ref        `json:"tags"`
	Memberships  []membership `json:"memberships"`
	CustomFields []struct {
		Name         string  `json:"name"`
		DisplayValue *string `json:"display_value"`
		TextValue    *string `json:"text_value"`
	} `json:"custom_fields"`
	NumSubtasks int  `json:"num_subtasks"`
	Workspace   *ref `json:"workspace"`
}

func (t task) modified() time.Time {
	ts, err := time.Parse(time.RFC3339Nano, t.ModifiedAt)
	if err != nil {
		return time.Time{}
	}
	return ts.UTC()
}

func (a *Adapter) inProject(t task) bool {
	return slices.ContainsFunc(t.Memberships, func(m membership) bool { return m.Project.GID == a.project })
}

// section is the task's section name in the scope project (any project's when none is set).
func (a *Adapter) section(t task) string {
	for _, m := range t.Memberships {
		if m.Section != nil && (a.project == "" || m.Project.GID == a.project) {
			return m.Section.Name
		}
	}
	return ""
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

// htmlToText is a plain-text reading of Asana rich text, for tasks whose notes are empty.
func htmlToText(s string) string {
	s = strings.NewReplacer("<li>", "\n- ", "<br>", "\n", "<br/>", "\n", "</p>", "\n").Replace(s)
	return strings.TrimSpace(html.UnescapeString(tagRe.ReplaceAllString(s, "")))
}

func (a *Adapter) toItem(t task) hubsync.Item {
	it := hubsync.Item{ID: t.GID, Key: t.GID, URL: t.Permalink, Title: t.Name, Body: t.Notes, Rev: t.ModifiedAt, UpdatedAt: t.modified()}
	if it.Body == "" && t.HTMLNotes != "" {
		it.Body = htmlToText(t.HTMLNotes)
	}
	it.Status = a.section(t)
	switch {
	case t.Completed:
		it.StatusCategory = hubsync.CategoryDone
	case it.Status != "":
		it.StatusCategory = categoryOf(it.Status, a.cfg.Lanes)
	default:
		it.StatusCategory = hubsync.CategoryTodo // a task in no section of the project: "My tasks"
	}
	if t.Assignee != nil {
		it.Assignee = t.Assignee.Email
		if it.Assignee == "" {
			it.Assignee = t.Assignee.Name
		}
	}
	if t.Parent != nil {
		it.ParentID = t.Parent.GID
	}
	for _, tg := range t.Tags {
		if tg.Name != "" {
			it.Labels = append(it.Labels, tg.Name)
		}
	}
	for _, cf := range t.CustomFields {
		if !strings.Contains(strings.ToLower(cf.Name), "acceptance") {
			continue
		}
		switch {
		case cf.TextValue != nil && *cf.TextValue != "":
			it.Acceptance = *cf.TextValue
		case cf.DisplayValue != nil:
			it.Acceptance = *cf.DisplayValue
		}
		break
	}
	return it
}

type story struct {
	GID       string `json:"gid"`
	Type      string `json:"type"`
	Subtype   string `json:"resource_subtype"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
	CreatedBy *ref   `json:"created_by"`
}

type attachment struct {
	Name         string `json:"name"`
	PermanentURL string `json:"permanent_url"`
	ViewURL      string `json:"view_url"`
}

// details adds comments (stories, newest first, capped) and attachments (name and permanent
// link; the signed download_url is never read), and remembers them at the item's Rev. Stories come
// oldest first, so a task with more stories than the page cap gets no comments (the pages read
// would be the oldest ones); one with more attachments keeps the ones read.
func (a *Adapter) details(ctx context.Context, it *hubsync.Item) error {
	stories, err := list[story](ctx, a, "/tasks/"+it.ID+"/stories",
		url.Values{"opt_fields": {"type,resource_subtype,text,created_at,created_by.name"}})
	if errors.Is(err, errTruncated) {
		stories = nil
	} else if err != nil {
		return err
	}
	it.Comments = nil
	for i := len(stories) - 1; i >= 0 && len(it.Comments) < hubsync.MaxComments; i-- {
		s := stories[i]
		if s.Subtype != "comment_added" && (s.Subtype != "" || s.Type != "comment") {
			continue
		}
		c := hubsync.Comment{ID: s.GID, Body: s.Text}
		if s.CreatedBy != nil {
			c.Author = s.CreatedBy.Name
		}
		if ts, err := time.Parse(time.RFC3339Nano, s.CreatedAt); err == nil {
			c.CreatedAt = ts.UTC()
		}
		it.Comments = append(it.Comments, c)
	}
	atts, err := list[attachment](ctx, a, "/attachments", url.Values{"parent": {it.ID}, "opt_fields": {"name,permanent_url,view_url"}})
	if err != nil && !errors.Is(err, errTruncated) {
		return err
	}
	it.Attachments = nil
	for _, at := range atts {
		u := at.PermanentURL
		if u == "" {
			u = at.ViewURL
		}
		it.Attachments = append(it.Attachments, hubsync.Attachment{Name: at.Name, URL: u})
	}
	a.remember(*it)
	return nil
}

// Check verifies the token (GET /users/me) and that the scope workspace and project are visible.
// The account is the user's email (name when Asana hides it).
func (a *Adapter) Check(ctx context.Context) (string, error) {
	var me struct {
		ref
		Workspaces []ref `json:"workspaces"`
	}
	if _, err := a.do(ctx, http.MethodGet, "/users/me", url.Values{"opt_fields": {"name,email,workspaces.name"}}, nil, &me); err != nil {
		return "", err
	}
	account := me.Email
	if account == "" {
		account = me.Name
	}
	if a.workspace != "" && !slices.ContainsFunc(me.Workspaces, func(w ref) bool { return w.GID == a.workspace }) {
		return account, fmt.Errorf("asana: workspace %s (tracker.scope.org) is not visible to this account", a.workspace)
	}
	if a.project != "" {
		if _, err := a.do(ctx, http.MethodGet, "/projects/"+a.project, url.Values{"opt_fields": {"name"}}, nil, nil); err != nil {
			return account, fmt.Errorf("asana: project %s (tracker.scope.board): %w", a.project, err)
		}
	}
	return account, nil
}

func (a *Adapter) needProject(what string) error {
	if a.project == "" {
		return fmt.Errorf("asana: %s needs tracker.scope.board (the project gid)", what)
	}
	return nil
}

func (a *Adapter) sections(ctx context.Context) ([]ref, error) {
	return list[ref](ctx, a, "/projects/"+a.project+"/sections", url.Values{"opt_fields": {"name"}})
}

// Statuses are the project's sections, in board order.
func (a *Adapter) Statuses(ctx context.Context) ([]hubsync.Status, error) {
	if err := a.needProject("statuses"); err != nil {
		return nil, err
	}
	secs, err := a.sections(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]hubsync.Status, 0, len(secs))
	for _, s := range secs {
		out = append(out, hubsync.Status{Name: s.Name, Category: categoryOf(s.Name, a.cfg.Lanes)})
	}
	return out, nil
}

// sectionFor finds the project section named like status (case and spacing ignored): its gid,
// and whether it is a done section (categoryOf, with the lanes override).
func (a *Adapter) sectionFor(ctx context.Context, status string) (string, bool, error) {
	if err := a.needProject("a status change"); err != nil {
		return "", false, err
	}
	secs, err := a.sections(ctx)
	if err != nil {
		return "", false, err
	}
	want := normalise(status)
	names := make([]string, 0, len(secs))
	for _, s := range secs {
		if normalise(s.Name) == want {
			g, err := gid("section", s.GID) // a server id still never reshapes a request path
			return g, categoryOf(s.Name, a.cfg.Lanes) == hubsync.CategoryDone, err
		}
		names = append(names, s.Name)
	}
	return "", false, fmt.Errorf("asana: no section %q in project %s (sections: %s)", status, a.project, strings.Join(names, ", "))
}

// SetMirroredParents names the tracker tasks that have mirrored children on the board. Each
// ListChanged then also polls their subtasks (one GET per parent), so a child that changed under
// an unchanged parent is still seen; GET /tasks with modified_since only returns the parent when
// the parent itself changed. The engine calls it before a tick with the parent ids of its linked
// cards (an optional interface: interface{ SetMirroredParents([]string) error }). Ids must be gids;
// on a bad one nothing changes.
func (a *Adapter) SetMirroredParents(ids []string) error {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		g, err := gid("parent task", id)
		if err != nil {
			return err
		}
		if !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	a.mu.Lock()
	a.parents = out
	a.mu.Unlock()
	return nil
}

func (a *Adapter) mirroredParents() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.parents)
}

// ListChanged lists tasks modified since the watermark: assigned to me in the workspace (kept to
// the project when one is set, plus my subtasks of project tasks), or every task of the project
// for assigned_to anyone, plus the subtasks of those tasks and of SetMirroredParents. Only open
// tasks and tasks completed since the watermark come back (completed_since; on the first sync,
// since now), so a first sync never walks a whole history of finished work. The new watermark is
// the newest modified_at seen (since when nothing changed). Any failure, a 429 or a list past the
// page cap included, returns no items so the watermark never skips work. Each listed task costs
// two more reads (stories, attachments) unless the adapter already read them at the task's current
// modified_at, so a tick cut short by a 429 keeps what it read and the engine's retry with the
// same Adapter resumes instead of starting over (Asana's free tier allows about 150 requests a
// minute: a first sync of about 70 open tasks needs more than one attempt).
func (a *Adapter) ListChanged(ctx context.Context, since time.Time) ([]hubsync.Item, time.Time, error) {
	q := url.Values{"opt_fields": {taskFields}}
	doneSince := since
	if since.IsZero() {
		doneSince = a.now()
	} else {
		q.Set("modified_since", since.UTC().Format(time.RFC3339))
	}
	q.Set("completed_since", doneSince.UTC().Format(time.RFC3339))
	if a.anyone() {
		if err := a.needProject("assigned_to anyone"); err != nil {
			return nil, since, err
		}
		q.Set("project", a.project)
	} else {
		if a.workspace == "" {
			return nil, since, errors.New("asana: assigned_to me needs tracker.scope.org (the workspace gid)")
		}
		q.Set("assignee", "me")
		q.Set("workspace", a.workspace)
	}
	tasks, err := list[task](ctx, a, "/tasks", q)
	if err != nil {
		return nil, since, err
	}
	watched := a.mirroredParents()
	seen := map[string]bool{}
	var keep, orphans []task
	for _, t := range tasks {
		if seen[t.GID] {
			continue
		}
		if !a.anyone() && a.project != "" && !a.inProject(t) {
			if t.Parent != nil && t.Parent.GID != "" {
				orphans = append(orphans, t) // my subtask: kept when its parent is in the project
			}
			continue
		}
		seen[t.GID] = true
		keep = append(keep, t)
	}
	parentIn := map[string]bool{}
	for _, t := range orphans {
		if seen[t.GID] {
			continue
		}
		p := t.Parent.GID
		in, known := parentIn[p]
		if !known {
			if in, err = a.parentInProject(ctx, p, seen[p] || slices.Contains(watched, p)); err != nil {
				return nil, since, err
			}
			parentIn[p] = in
		}
		if in {
			seen[t.GID] = true
			keep = append(keep, t)
		}
	}
	var polls []string
	for _, p := range keep {
		if p.NumSubtasks > 0 {
			polls = append(polls, p.GID)
		}
	}
	for _, p := range watched {
		if !seen[p] && !slices.Contains(polls, p) {
			polls = append(polls, p)
		}
	}
	for _, p := range polls {
		pg, err := gid("parent task", p)
		if err != nil {
			return nil, since, err
		}
		subs, err := list[task](ctx, a, "/tasks/"+pg+"/subtasks", url.Values{"opt_fields": {taskFields}})
		if errors.Is(err, hubsync.ErrNotFound) && slices.Contains(watched, p) {
			continue // a mirrored parent deleted in Asana: the engine sees that through Get
		}
		if err != nil {
			return nil, since, err
		}
		for _, s := range subs {
			if seen[s.GID] || !since.IsZero() && s.modified().Before(since) || s.Completed && s.modified().Before(doneSince) {
				continue
			}
			seen[s.GID] = true
			keep = append(keep, s)
		}
	}
	wm := since
	items := make([]hubsync.Item, 0, len(keep))
	for _, t := range keep {
		if _, err := gid("task", t.GID); err != nil {
			return nil, since, err
		}
		it := a.toItem(t)
		if !a.cached(&it) {
			if err := a.details(ctx, &it); err != nil {
				return nil, since, err
			}
		}
		if it.UpdatedAt.After(wm) {
			wm = it.UpdatedAt
		}
		items = append(items, it)
	}
	return items, wm, nil
}

// parentInProject says whether parent task p is in the scope project; known short-cuts the lookup
// (a parent listed this tick, or a mirrored one). A parent that is gone is not in the project.
func (a *Adapter) parentInProject(ctx context.Context, p string, known bool) (bool, error) {
	if known {
		return true, nil
	}
	pg, err := gid("parent task", p)
	if err != nil {
		return false, nil
	}
	var pt task
	if _, err := a.do(ctx, http.MethodGet, "/tasks/"+pg, url.Values{"opt_fields": {"memberships.project.name"}}, nil, &pt); err != nil {
		if errors.Is(err, hubsync.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return a.inProject(pt), nil
}

// Get reads one task with its notes, custom fields, comments and attachments.
func (a *Adapter) Get(ctx context.Context, id string) (hubsync.Item, error) {
	id, err := gid("task", id)
	if err != nil {
		return hubsync.Item{}, err
	}
	var t task
	if _, err := a.do(ctx, http.MethodGet, "/tasks/"+id, url.Values{"opt_fields": {taskFields}}, nil, &t); err != nil {
		return hubsync.Item{}, err
	}
	it := a.toItem(t)
	if err := a.details(ctx, &it); err != nil {
		return hubsync.Item{}, err
	}
	return it, nil
}

// Move adds the task to the project section named like status, then sets completed to whether
// that section is done (categoryOf, with the lanes override). A completed task always reads as
// done, so without the second write a move out of done would be undone by the next ListChanged,
// and a move to Done would leave the task open in My Tasks. Either write failing fails the move;
// both are idempotent, so the engine can retry it.
func (a *Adapter) Move(ctx context.Context, id, status string) error {
	id, err := gid("task", id)
	if err != nil {
		return err
	}
	sec, done, err := a.sectionFor(ctx, status)
	if err != nil {
		return err
	}
	if _, err := a.do(ctx, http.MethodPost, "/sections/"+sec+"/addTask", nil, map[string]any{"task": id}, nil); err != nil {
		return err
	}
	_, err = a.do(ctx, http.MethodPut, "/tasks/"+id, nil, map[string]any{"completed": done}, nil)
	return err
}

// Comment adds a comment story, converting the Markdown to Asana rich text.
func (a *Adapter) Comment(ctx context.Context, id, markdown string) error {
	id, err := gid("task", id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(markdown) == "" {
		return errors.New("asana: empty comment")
	}
	_, err = a.do(ctx, http.MethodPost, "/tasks/"+id+"/stories", nil, map[string]any{"html_text": markdownToHTML(markdown)}, nil)
	return err
}

func notesFor(body, acceptance string) string {
	body, acceptance = strings.TrimSpace(body), strings.TrimSpace(acceptance)
	if acceptance == "" {
		return body
	}
	if body == "" {
		return "Acceptance criteria:\n" + acceptance
	}
	return body + "\n\nAcceptance criteria:\n" + acceptance
}

// Create makes a task in the project (in the section named in.Status, else the first one; a done
// section creates it completed), or a subtask of in.ParentID. Asana has no acceptance field of its own, so acceptance checks follow
// the notes. With assigned_to me the task is assigned to me, so it is mirrored back.
// in.Type is ignored, and so is in.Status for a subtask. An error means no task was created.
func (a *Adapter) Create(ctx context.Context, in hubsync.NewItem) (hubsync.Item, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return hubsync.Item{}, errors.New("asana: a task needs a title")
	}
	data := map[string]any{"name": title, "notes": notesFor(in.Body, in.Acceptance)}
	if !a.anyone() {
		data["assignee"] = "me"
	}
	path := "/tasks"
	if in.ParentID != "" {
		parent, err := gid("parent task", in.ParentID)
		if err != nil {
			return hubsync.Item{}, err
		}
		path = "/tasks/" + parent + "/subtasks"
	} else {
		if a.project == "" && a.workspace == "" {
			return hubsync.Item{}, errors.New("asana: creating a task needs tracker.scope.board or tracker.scope.org")
		}
		if a.workspace != "" {
			data["workspace"] = a.workspace
		}
		if a.project != "" {
			if strings.TrimSpace(in.Status) != "" {
				sec, done, err := a.sectionFor(ctx, in.Status)
				if err != nil {
					return hubsync.Item{}, err
				}
				data["memberships"] = []any{map[string]any{"project": a.project, "section": sec}}
				if done {
					data["completed"] = true
				}
			} else {
				data["projects"] = []any{a.project}
			}
		}
	}
	var created task
	if _, err := a.do(ctx, http.MethodPost, path, nil, data, &created); err != nil {
		return hubsync.Item{}, err
	}
	if created.GID == "" {
		return hubsync.Item{}, errors.New("asana: create returned no task id")
	}
	if _, err := gid("created task", created.GID); err != nil {
		return hubsync.Item{}, err
	}
	// From here on the task exists in Asana, so Create never returns an error: an engine that
	// retried a failed create would make a duplicate. A label or read-back failure leaves the
	// returned Item without those labels (Item.Labels is what Asana has): the engine reconciles
	// by diffing in.Labels against Item.Labels and calling SetLabels for the missing ones.
	if len(in.Labels) > 0 {
		_ = a.SetLabels(ctx, created.GID, in.Labels, nil)
	}
	it, err := a.Get(ctx, created.GID)
	if err != nil {
		return a.toItem(created), nil
	}
	return it, nil
}

// Update sets the name and/or notes; nil leaves a field as it is.
func (a *Adapter) Update(ctx context.Context, id string, title, body *string) error {
	if title == nil && body == nil {
		return nil
	}
	id, err := gid("task", id)
	if err != nil {
		return err
	}
	data := map[string]any{}
	if title != nil {
		t := strings.TrimSpace(*title)
		if t == "" {
			return errors.New("asana: a task title cannot be empty")
		}
		data["name"] = t
	}
	if body != nil {
		data["notes"] = *body
	}
	_, err = a.do(ctx, http.MethodPut, "/tasks/"+id, nil, data, nil)
	return err
}

func cleanNames(in []string) []string {
	var out []string
	for _, n := range in {
		n = strings.TrimSpace(n)
		if n != "" && !slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, n) }) {
			out = append(out, n)
		}
	}
	return out
}

// SetLabels removes then adds workspace tags by name (case-insensitive). Each name is looked up
// with the workspace tag typeahead (one request per name, so a workspace of any size works). A
// tag to add that the workspace lacks is created; a tag to remove that does not exist is skipped.
func (a *Adapter) SetLabels(ctx context.Context, id string, add, remove []string) error {
	add, remove = cleanNames(add), cleanNames(remove)
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	id, err := gid("task", id)
	if err != nil {
		return err
	}
	ws := a.workspace
	if ws == "" {
		var t task
		if _, err := a.do(ctx, http.MethodGet, "/tasks/"+id, url.Values{"opt_fields": {"workspace"}}, nil, &t); err != nil {
			return err
		}
		if t.Workspace == nil || t.Workspace.GID == "" {
			return fmt.Errorf("asana: task %s has no workspace", id)
		}
		if ws, err = gid("workspace", t.Workspace.GID); err != nil {
			return err
		}
	}
	for _, n := range remove {
		g, err := a.findTag(ctx, ws, n)
		if err != nil {
			return err
		}
		if g == "" {
			continue
		}
		if _, err := a.do(ctx, http.MethodPost, "/tasks/"+id+"/removeTag", nil, map[string]any{"tag": g}, nil); err != nil {
			return err
		}
	}
	for _, n := range add {
		g, err := a.findTag(ctx, ws, n)
		if err != nil {
			return err
		}
		if g == "" {
			var t ref
			if _, err := a.do(ctx, http.MethodPost, "/tags", nil, map[string]any{"name": n, "workspace": ws}, &t); err != nil {
				return err
			}
			if t.GID == "" {
				return fmt.Errorf("asana: tag %q was not created", n)
			}
			g = t.GID
		}
		if _, err := a.do(ctx, http.MethodPost, "/tasks/"+id+"/addTag", nil, map[string]any{"tag": g}, nil); err != nil {
			return err
		}
	}
	return nil
}

// typeaheadCount is the most results Asana's typeahead returns.
const typeaheadCount = 100

// findTag is the gid of the workspace tag named name (case-insensitive), "" when there is none.
// The typeahead matches loosely and returns at most typeaheadCount results, so a full page with no
// exact match is an error rather than "none": creating the tag then could add a duplicate on
// every call.
func (a *Adapter) findTag(ctx context.Context, ws, name string) (string, error) {
	var tags []ref
	q := url.Values{"resource_type": {"tag"}, "query": {name}, "count": {strconv.Itoa(typeaheadCount)}, "opt_fields": {"name"}}
	if _, err := a.do(ctx, http.MethodGet, "/workspaces/"+ws+"/typeahead", q, nil, &tags); err != nil {
		return "", err
	}
	for _, t := range tags {
		if t.GID != "" && strings.EqualFold(strings.TrimSpace(t.Name), name) {
			return t.GID, nil
		}
	}
	if len(tags) >= typeaheadCount {
		return "", fmt.Errorf("asana: tag %q: %d or more tags match the name and none exactly; rename the label", name, typeaheadCount)
	}
	return "", nil
}
