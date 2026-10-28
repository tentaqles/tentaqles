// Package github is the board sync adapter for GitHub Issues and Projects v2 (spec B §7).
//
// Scope keys (manifest tracker.scope): org is the owner (user or organization), project the
// repository name, board the Projects v2 number. status_field names the project's single-select
// status field ("Status" by default). With a board, an item's status is that field's option; with
// none it is Open or Closed. Item ids are "owner/repo#number". Writes stay inside the scope:
// the owner must be scope.org and the repository scope.project, or the issue a sub-issue of one
// there or (with a board) on the board or a sub-issue of a board item; anything else fails with
// ErrOutOfScope before the write is sent.
//
// Cost: a board tick walks a light page per 100 items, then reads in full, by node id, only what
// changed since the watermark. List queries ask for their rateLimit and stop the sync when less
// than a tenth of the hourly GraphQL budget is left (with the cli source it is the user's own).
//
// Auth: the cli source runs `gh api` with the workspace identity environment (GH_CONFIG_DIR), so
// the token stays in gh's own store. gh never inherits a gh login from the hub process
// (GH_CONFIG_DIR, GH_TOKEN, GITHUB_TOKEN, GH_HOST and the enterprise tokens are dropped), and
// it is not run at all when the identity sets no GH_CONFIG_DIR. When gh cannot be used (no
// config dir, not installed, logged out, or a 401 for its token), and for the keychain and env
// sources, the credential comes from the cred resolver and the adapter calls the API over HTTP.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	gosync "sync"
	"time"
	"unicode"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// Provider is the tracker id.
const Provider = "github"

func init() {
	hubsync.Register(Provider, New)
	cred.RegisterCLI(Provider, cliToken)
}

// cliToken is the cli credential hook: `gh auth token` for the identity's gh login. The resolver
// uses it when another component needs a raw token; this adapter itself runs `gh api` instead.
func cliToken(ctx context.Context, env []string) (cred.Token, error) {
	if !pinsConfigDir(env) { // gh would hand out the hub process's login, not this workspace's
		return cred.Token{}, &cred.SourceError{Source: cred.SourceCLI, Provider: Provider,
			Err: fmt.Errorf("%w: the workspace identity sets no GH_CONFIG_DIR", cred.ErrNoCredential)}
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, ghCommand, "auth", "token")
	cmd.Env = ghEnv(os.Environ(), env)
	out, err := cmd.Output()
	tok := strings.TrimSpace(string(out))
	if err != nil || tok == "" {
		if ctx.Err() != nil {
			return cred.Token{}, &cred.SourceError{Source: cred.SourceCLI, Provider: Provider, Err: ctx.Err()}
		}
		return cred.Token{}, &cred.SourceError{Source: cred.SourceCLI, Provider: Provider, Err: cred.ErrNoCredential}
	}
	return cred.Token{Scheme: "Bearer", Value: tok}, nil
}

// Adapter is the GitHub tracker.
type Adapter struct {
	cfg         hubsync.Config
	owner, repo string // scope org / project; repo may be empty with a board
	board       int    // Projects v2 number; 0 = none
	statusField string

	mu     gosync.Mutex
	be     backend
	login  string
	proj   *project
	lastWM time.Time         // the watermark the last ListChanged returned
	atWM   map[string]string // id -> rev of the items it returned at that watermark
}

type project struct {
	ID, Title, FieldID string
	Options            []option
}

type option struct{ ID, Name string }

// New builds the adapter from a sync config.
func New(cfg hubsync.Config) (hubsync.Adapter, error) {
	a := &Adapter{cfg: cfg, owner: strings.TrimSpace(cfg.Scope["org"]), repo: strings.TrimSpace(cfg.Scope["project"]),
		statusField: strings.TrimSpace(cfg.Scope["status_field"])}
	if a.statusField == "" {
		a.statusField = "Status"
	}
	if b := strings.TrimSpace(cfg.Scope["board"]); b != "" {
		n, err := strconv.Atoi(strings.TrimPrefix(b, "#"))
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("github: tracker.scope.board is the Projects v2 number, got %q", b)
		}
		a.board = n
	}
	if a.owner == "" {
		return nil, errors.New("github: tracker.scope.org (the repository or project owner) is required")
	}
	if strings.Contains(a.repo, "/") {
		return nil, fmt.Errorf("github: tracker.scope.project is the repository name without the owner, got %q", a.repo)
	}
	switch {
	case cfg.Transport != nil && cfg.Transport.Source() != cred.SourceCLI:
		a.be = newHTTPBackend(cfg.Transport, cfg.BaseURL)
	case cfg.Transport != nil, cfg.Auth == "", cfg.Auth == cred.SourceCLI:
		a.be = &ghBackend{env: cfg.Env}
	}
	return a, nil
}

func (a *Adapter) Name() string { return Provider }

// backend is the transport in use; nil until resolved (keychain/env/mcp with no Transport).
func (a *Adapter) backend(ctx context.Context) (backend, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.be != nil {
		return a.be, nil
	}
	return a.resolveLocked(ctx, a.cfg.CredRequest())
}

func (a *Adapter) resolveLocked(ctx context.Context, req cred.Request) (backend, error) {
	t, err := cred.Resolve(ctx, req)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, &cred.SourceError{Source: cred.SourceMCP, Provider: Provider, Err: cred.ErrNeedsSession}
	}
	a.be = newHTTPBackend(t, a.cfg.BaseURL)
	return a.be, nil
}

// fallBack asks the cred chain, from the keychain source on, for a credential when gh cannot be
// used. On success the HTTP backend replaces gh; on failure gh stays, so a later gh login works.
func (a *Adapter) fallBack(ctx context.Context, failed backend, cause error) (backend, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.be != failed {
		return a.be, nil // another call already fell back
	}
	req := a.cfg.CredRequest()
	req.Preferred = cred.SourceKeychain // the cli source just failed; the fixed order goes on from here
	be, err := a.resolveLocked(ctx, req)
	if err != nil {
		a.be = failed
		return nil, fmt.Errorf("github: %w; %w", cause, err)
	}
	return be, nil
}

// send runs one request, falling back from gh to the cred chain when gh cannot be used.
func (a *Adapter) send(ctx context.Context, method, path string, body []byte) (response, error) {
	be, err := a.backend(ctx)
	if err != nil {
		return response{}, err
	}
	r, err := be.do(ctx, method, path, body)
	if errors.Is(err, errCLIUnavailable) {
		if be, err = a.fallBack(ctx, be, err); err != nil {
			return response{}, err
		}
		r, err = be.do(ctx, method, path, body)
	}
	return r, err
}

// rest calls a REST endpoint; in and out are JSON (either may be nil).
func (a *Adapter) rest(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = b
	}
	r, err := a.send(ctx, method, path, body)
	if err != nil {
		return err
	}
	if rl := rateLimit(r); rl != nil {
		return rl
	}
	if r.status < 200 || r.status > 299 {
		return apiError(method, path, r)
	}
	if out != nil && len(r.body) > 0 {
		if err := json.Unmarshal(r.body, out); err != nil {
			return fmt.Errorf("github: %s %s: decoding the response: %w", method, path, err)
		}
	}
	return nil
}

type gqlError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// gql runs a GraphQL operation and decodes data into out.
func (a *Adapter) gql(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	op := opName(query)
	r, err := a.send(ctx, "POST", "graphql", body)
	if err != nil {
		return err
	}
	if rl := rateLimit(r); rl != nil {
		return rl
	}
	if r.status < 200 || r.status > 299 {
		return apiError("graphql", op, r)
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		return fmt.Errorf("github: graphql %s: decoding the response: %w", op, err)
	}
	if len(env.Errors) > 0 {
		var msgs []string
		notFound := false
		for _, e := range env.Errors {
			switch e.Type {
			case "RATE_LIMITED":
				return &hubsync.RateLimitError{Provider: Provider, RetryAfter: retryAfter(r.header, true)}
			case "NOT_FOUND":
				notFound = true
			}
			msgs = append(msgs, e.Message)
		}
		err := fmt.Errorf("github: graphql %s: %s", op, clip(strings.Join(msgs, "; ")))
		if notFound {
			return fmt.Errorf("%w: %w", hubsync.ErrNotFound, err)
		}
		return err
	}
	if rl := budgetLow(env.Data); rl != nil {
		return rl
	}
	if out != nil {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("github: graphql %s: decoding data: %w", op, err)
		}
	}
	return nil
}

// budgetLow reads the rateLimit a list query asked for (rateLimitSel) and stops the sync when
// what is left of the hourly GraphQL budget falls under a tenth of it: with the cli source the
// budget is the user's own, shared with their gh. The error asks to retry at the reset.
func budgetLow(data json.RawMessage) *hubsync.RateLimitError {
	var d struct {
		RateLimit *struct {
			Remaining int       `json:"remaining"`
			Limit     int       `json:"limit"`
			ResetAt   time.Time `json:"resetAt"`
		} `json:"rateLimit"`
	}
	if len(data) == 0 || json.Unmarshal(data, &d) != nil || d.RateLimit == nil {
		return nil
	}
	reserve := d.RateLimit.Limit / 10
	if reserve <= 0 {
		reserve = 500
	}
	if d.RateLimit.Remaining >= reserve {
		return nil
	}
	wait := time.Minute
	if !d.RateLimit.ResetAt.IsZero() {
		wait = min(max(d.RateLimit.ResetAt.Sub(now()), time.Second), time.Hour)
	}
	return &hubsync.RateLimitError{Provider: Provider, RetryAfter: wait}
}

// opName is a query's operation name, for error messages.
func opName(q string) string {
	f := strings.Fields(q)
	if len(f) >= 2 && (f[0] == "query" || f[0] == "mutation") {
		name, _, _ := strings.Cut(f[1], "(")
		return name
	}
	return "query"
}

// ---- ids ----

type ref struct {
	owner, repo string
	number      int
}

func (r ref) id() string  { return r.owner + "/" + r.repo + "#" + strconv.Itoa(r.number) }
func (r ref) key() string { return r.repo + "#" + strconv.Itoa(r.number) }
func (r ref) path() string {
	return "repos/" + url.PathEscape(r.owner) + "/" + url.PathEscape(r.repo) + "/issues/" + strconv.Itoa(r.number)
}

// parseID reads "owner/repo#N"; "#N" and "N" mean the scope repository.
func (a *Adapter) parseID(id string) (ref, error) {
	id = strings.TrimSpace(id)
	repoPart, num, hasHash := strings.Cut(id, "#")
	if !hasHash {
		repoPart, num = "", id
	}
	n, err := strconv.Atoi(num)
	if err != nil || n <= 0 {
		return ref{}, fmt.Errorf("github: item id %q: want owner/repo#number", id)
	}
	if repoPart == "" {
		if a.repo == "" {
			return ref{}, fmt.Errorf("github: item id %q has no repository and tracker.scope.project is not set", id)
		}
		return ref{owner: a.owner, repo: a.repo, number: n}, nil
	}
	o, r, ok := strings.Cut(repoPart, "/")
	if !ok || o == "" || r == "" || strings.Contains(r, "/") {
		return ref{}, fmt.Errorf("github: item id %q: want owner/repo#number", id)
	}
	return ref{owner: o, repo: r, number: n}, nil
}

// ErrOutOfScope: a write names an issue outside tracker.scope.
var ErrOutOfScope = errors.New("outside tracker.scope")

// qIssueParent reads what the write scope check needs: the issue's boards and its parent's
// repository and boards.
const qIssueParent = `query IssueParent($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) {
      projectItems(first: 20) { nodes { project { id } } }
      parent { repository { nameWithOwner } projectItems(first: 20) { nodes { project { id } } } }
    }
  }
}`

type projectRef struct {
	Project struct {
		ID string `json:"id"`
	} `json:"project"`
}

type projectRefs struct {
	Nodes []projectRef `json:"nodes"`
}

func (p projectRefs) has(projectID string) bool {
	return projectID != "" && slices.ContainsFunc(p.Nodes, func(n projectRef) bool { return n.Project.ID == projectID })
}

// writeRef parses an id for a write and keeps writes inside tracker.scope, as tight as what
// ListChanged mirrors. The owner must be scope.org, and the repository scope.project; an issue in
// another repository of the owner is in scope only as a sub-issue of a scope.project issue or,
// with a board, when it or its parent is on the board. With a board and no scope.project, every
// write needs the issue or its parent on the board. One lookup checks that; the scope repository
// needs none.
func (a *Adapter) writeRef(ctx context.Context, id string) (ref, error) {
	r, err := a.parseID(id)
	if err != nil {
		return ref{}, err
	}
	if !strings.EqualFold(r.owner, a.owner) {
		return ref{}, fmt.Errorf("github: %s: %w (owner %s)", r.id(), ErrOutOfScope, a.owner)
	}
	r.owner = a.owner // names are case-insensitive; keep the scope's spelling
	if strings.EqualFold(r.repo, a.repo) {
		r.repo = a.repo
	}
	if a.repo != "" && r.repo == a.repo {
		return r, nil
	}
	if a.repo == "" && a.board == 0 {
		return r, nil // Check refuses this scope; reads fail the same way
	}
	boardID := ""
	if a.board != 0 {
		p, err := a.project(ctx)
		if err != nil {
			return ref{}, err
		}
		boardID = p.ID
	}
	var out struct {
		Repository *struct {
			Issue *struct {
				ProjectItems projectRefs `json:"projectItems"`
				Parent       *struct {
					Repository struct {
						NameWithOwner string `json:"nameWithOwner"`
					} `json:"repository"`
					ProjectItems projectRefs `json:"projectItems"`
				} `json:"parent"`
			} `json:"issue"`
		} `json:"repository"`
	}
	if err := a.gql(ctx, qIssueParent, map[string]any{"owner": r.owner, "name": r.repo, "number": r.number}, &out); err != nil {
		return ref{}, err
	}
	if out.Repository == nil || out.Repository.Issue == nil {
		return ref{}, fmt.Errorf("%w: github: %s", hubsync.ErrNotFound, r.id())
	}
	is := out.Repository.Issue
	p := is.Parent
	switch {
	case is.ProjectItems.has(boardID):
	case p != nil && p.ProjectItems.has(boardID):
	case p != nil && a.repo != "" && strings.EqualFold(p.Repository.NameWithOwner, a.owner+"/"+a.repo):
	default:
		where := "repository " + a.owner + "/" + a.repo + ", or a sub-issue of one there"
		switch {
		case a.board != 0 && a.repo != "":
			where = fmt.Sprintf("repository %s/%s, project %d, or a sub-issue of an issue there", a.owner, a.repo, a.board)
		case a.board != 0:
			where = fmt.Sprintf("project %d of %s, or a sub-issue of an issue on it", a.board, a.owner)
		}
		return ref{}, fmt.Errorf("github: %s: %w (%s)", r.id(), ErrOutOfScope, where)
	}
	return r, nil
}

func refFrom(nameWithOwner string, n int) ref {
	o, r, _ := strings.Cut(nameWithOwner, "/")
	return ref{owner: o, repo: r, number: n}
}

// ---- Check / Statuses ----

const qViewer = `query Viewer { viewer { login } }`

const qRepo = `query Repo($owner: String!, $name: String!) { repository(owner: $owner, name: $name) { nameWithOwner } }`

const qProject = `query Project($owner: String!, $number: Int!, $field: String!) {
  repositoryOwner(login: $owner) {
    ... on ProjectV2Owner {
      projectV2(number: $number) {
        id title
        field(name: $field) { ... on ProjectV2SingleSelectField { id name options { id name } } }
      }
    }
  }
}`

func (a *Adapter) viewer(ctx context.Context) (string, error) {
	a.mu.Lock()
	l := a.login
	a.mu.Unlock()
	if l != "" {
		return l, nil
	}
	var out struct {
		Viewer struct{ Login string } `json:"viewer"`
	}
	if err := a.gql(ctx, qViewer, nil, &out); err != nil {
		return "", err
	}
	if out.Viewer.Login == "" {
		return "", errors.New("github: the API returned no login for this credential")
	}
	a.mu.Lock()
	a.login = out.Viewer.Login
	a.mu.Unlock()
	return out.Viewer.Login, nil
}

// project is the board's project, field and status options, cached for the adapter's life.
// Statuses and an option miss reload it, so options added on the board show up without a restart.
func (a *Adapter) project(ctx context.Context) (*project, error) {
	a.mu.Lock()
	p := a.proj
	a.mu.Unlock()
	if p != nil {
		return p, nil
	}
	return a.loadProject(ctx)
}

// projectOption is the project and its option for status, reloading the project once when the
// cached one has no such option.
func (a *Adapter) projectOption(ctx context.Context, status string) (*project, option, error) {
	p, err := a.project(ctx)
	if err != nil {
		return nil, option{}, err
	}
	opt, err := p.option(status)
	if errors.Is(err, errNoOption) {
		if p, err = a.loadProject(ctx); err != nil {
			return nil, option{}, err
		}
		opt, err = p.option(status)
	}
	if err != nil {
		return nil, option{}, err
	}
	return p, opt, nil
}

// loadProject reads the project and replaces the cache.
func (a *Adapter) loadProject(ctx context.Context) (*project, error) {
	var out struct {
		Owner *struct {
			Project *struct {
				ID, Title string
				Field     *struct {
					ID, Name string
					Options  []option
				} `json:"field"`
			} `json:"projectV2"`
		} `json:"repositoryOwner"`
	}
	if err := a.gql(ctx, qProject, map[string]any{"owner": a.owner, "number": a.board, "field": a.statusField}, &out); err != nil {
		return nil, err
	}
	if out.Owner == nil || out.Owner.Project == nil {
		return nil, fmt.Errorf("%w: github: project %d of %s", hubsync.ErrNotFound, a.board, a.owner)
	}
	pr := out.Owner.Project
	if pr.Field == nil || pr.Field.ID == "" {
		return nil, fmt.Errorf("github: project %d of %s has no single-select field %q (set tracker.scope.status_field)", a.board, a.owner, a.statusField)
	}
	p := &project{ID: pr.ID, Title: pr.Title, FieldID: pr.Field.ID, Options: pr.Field.Options}
	a.mu.Lock()
	a.proj = p
	a.mu.Unlock()
	return p, nil
}

// Check verifies the credential and that the scope resolves.
func (a *Adapter) Check(ctx context.Context) (string, error) {
	login, err := a.viewer(ctx)
	if err != nil {
		return "", err
	}
	if a.repo == "" && a.board == 0 {
		return login, fmt.Errorf("github: tracker.scope needs project (a repository) or board (a project number)")
	}
	if a.repo != "" {
		var out struct {
			Repository *struct{ NameWithOwner string } `json:"repository"`
		}
		if err := a.gql(ctx, qRepo, map[string]any{"owner": a.owner, "name": a.repo}, &out); err != nil {
			return login, err
		}
		if out.Repository == nil {
			return login, fmt.Errorf("%w: github: repository %s/%s", hubsync.ErrNotFound, a.owner, a.repo)
		}
	}
	if a.board != 0 {
		if _, err := a.project(ctx); err != nil {
			return login, err
		}
	}
	return login, nil
}

const (
	statusOpen   = "Open"
	statusClosed = "Closed"
)

// Statuses is the project's status options with a board (read fresh), else Open and Closed.
func (a *Adapter) Statuses(ctx context.Context) ([]hubsync.Status, error) {
	if a.board == 0 {
		return []hubsync.Status{{Name: statusOpen, Category: hubsync.CategoryTodo}, {Name: statusClosed, Category: hubsync.CategoryDone}}, nil
	}
	p, err := a.loadProject(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]hubsync.Status, 0, len(p.Options))
	for _, o := range p.Options {
		out = append(out, hubsync.Status{Name: o.Name, Category: categoryFor(o.Name)})
	}
	return out, nil
}

// categoryFor guesses a status category from a project option's name. It matches whole words
// (split on non-letters and camel case), or a word's start for the stems ending in "*", so
// "Inactive", "Latest" or "Renewal" do not match "active", "test" or "new".
func categoryFor(name string) string {
	words := nameWords(name)
	has := func(keys ...string) bool {
		for _, k := range keys {
			stem, prefix := strings.CutSuffix(k, "*")
			for _, w := range words {
				if w == stem || (prefix && strings.HasPrefix(w, stem)) {
					return true
				}
			}
		}
		return false
	}
	notStarted := false
	for i := 0; i+1 < len(words); i++ {
		if words[i] == "not" && strings.HasPrefix(words[i+1], "start") {
			notStarted = true
		}
	}
	switch {
	case notStarted:
		return hubsync.CategoryTodo
	case has("done", "closed", "complet*", "ship*", "releas*", "merg*", "resolv*", "finish*"):
		return hubsync.CategoryDone
	case has("review*", "qa", "test*", "verif*", "approv*", "staging"):
		return hubsync.CategoryReview
	case has("progress", "doing", "active", "start*", "develop*", "wip", "working", "block*"):
		return hubsync.CategoryActive
	case has("backlog", "icebox", "triag*", "later", "someday", "inbox", "new"):
		return hubsync.CategoryBacklog
	}
	return hubsync.CategoryTodo
}

// nameWords splits a status name into lower-case words at non-letters and camel-case humps.
func nameWords(name string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	rs := []rune(strings.TrimSpace(name))
	for i, r := range rs {
		switch {
		case !unicode.IsLetter(r):
			flush()
		case unicode.IsUpper(r) && i > 0 && unicode.IsLower(rs[i-1]):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return words
}

// ---- reading items ----

const issueFields = `fragment issueFields on Issue {
  id databaseId number title body url state updatedAt
  repository { nameWithOwner }
  assignees(first: 10) { nodes { login } }
  labels(first: 50) { nodes { name } }
  parent { number repository { nameWithOwner } assignees(first: 10) { nodes { login } } }
  comments(last: 20) { nodes { id body createdAt author { login } } }
}`

// boardItemsSel is an issue's project items with their status, so an issue read anywhere (as a
// sub-issue too) carries its board status and the board item's time.
const boardItemsSel = `projectItems(first: 20) {
  nodes {
    id updatedAt project { id }
    fieldValueByName(name: $field) { ... on ProjectV2ItemFieldSingleSelectValue { name optionId } }
  }
}`

// qIssues has no assignee filter: a sub-issue of the viewer's issue is kept on the client, and
// its own changes do not touch its parent's updatedAt.
const qIssues = `query Issues($owner: String!, $name: String!, $since: DateTime, $cursor: String) {
  ` + rateLimitSel + `
  repository(owner: $owner, name: $name) {
    issues(first: 50, after: $cursor, orderBy: {field: UPDATED_AT, direction: ASC},
           filterBy: {since: $since, states: [OPEN, CLOSED]}) {
      pageInfo { hasNextPage endCursor }
      nodes { ...issueFields subIssues(first: 25) { pageInfo { hasNextPage endCursor } nodes { ...issueFields } } }
    }
  }
}
` + issueFields

// rateLimitSel is the query's cost and what is left of the hourly GraphQL budget; the list
// queries ask for it so a sync stops before it starves the user's own gh (see listReserve).
const rateLimitSel = `rateLimit { cost remaining limit resetAt }`

// qProjectItems is one light board page: per item only what decides whether it changed and is
// mirrored (times, status, repository, assignees, parent, sub-issue count). Its only nested
// connection is assignees(first: 10), so a page of 100 costs about 2 points. Full fields and
// sub-issues are read afterwards, by node id, only for what changed.
const qProjectItems = `query ProjectItems($id: ID!, $field: String!, $cursor: String) {
  ` + rateLimitSel + `
  node(id: $id) {
    ... on ProjectV2 {
      items(first: 100, after: $cursor) {
        pageInfo { hasNextPage endCursor }
        nodes {
          id updatedAt
          fieldValueByName(name: $field) { ... on ProjectV2ItemFieldSingleSelectValue { name optionId } }
          content { __typename ... on Issue {
            id number updatedAt repository { nameWithOwner }
            assignees(first: 10) { nodes { login } }
            parent { id }
            subIssuesSummary { total }
          } }
        }
      }
    }
  }
}`

// qIssueNodes reads changed board issues that have sub-issues, with the first sub-issues inline.
const qIssueNodes = `query IssueNodes($ids: [ID!]!, $field: String!) {
  ` + rateLimitSel + `
  nodes(ids: $ids) {
    ... on Issue { ...issueFields subIssues(first: 25) { pageInfo { hasNextPage endCursor } nodes { ...issueFields ` + boardItemsSel + ` } } }
  }
}
` + issueFields

// qChildNodes reads changed issues without their sub-issues: board leaves and changed sub-issues
// of unchanged parents.
const qChildNodes = `query ChildNodes($ids: [ID!]!, $field: String!) {
  ` + rateLimitSel + `
  nodes(ids: $ids) { ... on Issue { ...issueFields ` + boardItemsSel + ` } }
}
` + issueFields

// qSubIssueProbe lists the sub-issues of unchanged parents by id and time only, so a sub-issue
// changed on its own (which does not touch its parent's updatedAt) is still seen.
const qSubIssueProbe = `query SubIssueProbe($ids: [ID!]!) {
  ` + rateLimitSel + `
  nodes(ids: $ids) {
    ... on Issue { id subIssues(first: 50) { pageInfo { hasNextPage endCursor } nodes { id number updatedAt repository { nameWithOwner } } } }
  }
}`

const qSubIssueIDs = `query SubIssueIDs($id: ID!, $cursor: String) {
  ` + rateLimitSel + `
  node(id: $id) {
    ... on Issue { subIssues(first: 100, after: $cursor) { pageInfo { hasNextPage endCursor } nodes { id number updatedAt repository { nameWithOwner } } } }
  }
}`

// Batch sizes for the node reads, keeping each query's cost to a few points.
const (
	parentBatch = 10
	leafBatch   = 50
	probeBatch  = 50
)

const qSubIssues = `query SubIssues($id: ID!, $cursor: String) {
  node(id: $id) {
    ... on Issue { subIssues(first: 50, after: $cursor) { pageInfo { hasNextPage endCursor } nodes { ...issueFields } } }
  }
}
` + issueFields

const qSubIssuesOnBoard = `query SubIssues($id: ID!, $field: String!, $cursor: String) {
  node(id: $id) {
    ... on Issue {
      subIssues(first: 50, after: $cursor) { pageInfo { hasNextPage endCursor } nodes { ...issueFields ` + boardItemsSel + ` } }
    }
  }
}
` + issueFields

const qIssue = `query Issue($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) { issue(number: $number) { ...issueFields } }
}
` + issueFields

const qIssueOnBoard = `query Issue($owner: String!, $name: String!, $number: Int!, $field: String!) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) { ...issueFields ` + boardItemsSel + ` }
  }
}
` + issueFields

// maxPages bounds one repository walk (50 issues a page). The walk is in update order, so a cut
// walk is resumed from its watermark on the next tick.
const maxPages = 40

// maxBoardPages bounds one board walk (boardPageSize items a page). Board items come in position
// order with no since filter, so a cut walk is an error: items past the cut would never be seen.
const (
	maxBoardPages = 100
	boardPageSize = 100
)

// maxSubPages bounds the extra sub-issue pages read for one parent. A list query reads the first
// 25 sub-issues inline (keeping its node cost down) and SubIssues pages the rest, 50 at a time.
const maxSubPages = 10

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type ghIssue struct {
	Typename   string    `json:"__typename"`
	ID         string    `json:"id"`
	DatabaseID int64     `json:"databaseId"`
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	URL        string    `json:"url"`
	State      string    `json:"state"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Assignees struct {
		Nodes []struct{ Login string } `json:"nodes"`
	} `json:"assignees"`
	Labels struct {
		Nodes []struct{ Name string } `json:"nodes"`
	} `json:"labels"`
	Parent *struct {
		Number     int `json:"number"`
		Repository struct {
			NameWithOwner string `json:"nameWithOwner"`
		} `json:"repository"`
		Assignees struct {
			Nodes []struct{ Login string } `json:"nodes"`
		} `json:"assignees"`
	} `json:"parent"`
	Comments struct {
		Nodes []struct {
			ID        string                  `json:"id"`
			Body      string                  `json:"body"`
			CreatedAt time.Time               `json:"createdAt"`
			Author    *struct{ Login string } `json:"author"`
		} `json:"nodes"`
	} `json:"comments"`
	SubIssues    subIssues `json:"subIssues"`
	ProjectItems struct {
		Nodes []struct {
			ID        string    `json:"id"`
			UpdatedAt time.Time `json:"updatedAt"`
			Project   struct {
				ID string `json:"id"`
			} `json:"project"`
			Field *statusValue `json:"fieldValueByName"`
		} `json:"nodes"`
	} `json:"projectItems"`
}

type subIssues struct {
	PageInfo pageInfo  `json:"pageInfo"`
	Nodes    []ghIssue `json:"nodes"`
}

type statusValue struct {
	Name     string `json:"name"`
	OptionID string `json:"optionId"`
}

func (g ghIssue) ref() ref { return refFrom(g.Repository.NameWithOwner, g.Number) }

func hasLogin(nodes []struct{ Login string }, login string) bool {
	return slices.ContainsFunc(nodes, func(n struct{ Login string }) bool { return strings.EqualFold(n.Login, login) })
}

func (g ghIssue) assignedTo(login string) bool { return hasLogin(g.Assignees.Nodes, login) }

// parentAssignedTo: the issue is a sub-issue of one assigned to login.
func (g ghIssue) parentAssignedTo(login string) bool {
	return g.Parent != nil && hasLogin(g.Parent.Assignees.Nodes, login)
}

// boardStatus is the issue's status and item time on the project (nil, zero when not on it).
func (g ghIssue) boardStatus(projectID string) (*statusValue, time.Time) {
	if projectID == "" {
		return nil, time.Time{}
	}
	for _, pi := range g.ProjectItems.Nodes {
		if pi.Project.ID == projectID {
			return pi.Field, pi.UpdatedAt
		}
	}
	return nil, time.Time{}
}

// item maps an issue; status is the project status (nil = none or no board).
func (a *Adapter) item(g ghIssue, status *statusValue, updated time.Time) hubsync.Item {
	r := g.ref()
	it := hubsync.Item{ID: r.id(), Key: r.key(), URL: g.URL, Title: g.Title, Body: g.Body}
	a.setStatus(&it, status, strings.EqualFold(g.State, "CLOSED"))
	if len(g.Assignees.Nodes) > 0 {
		it.Assignee = g.Assignees.Nodes[0].Login
	}
	if g.Parent != nil && g.Parent.Number > 0 {
		it.ParentID = refFrom(g.Parent.Repository.NameWithOwner, g.Parent.Number).id()
	}
	for _, l := range g.Labels.Nodes {
		it.Labels = append(it.Labels, l.Name)
	}
	cs := g.Comments.Nodes
	for i := len(cs) - 1; i >= 0 && len(it.Comments) < hubsync.MaxComments; i-- { // newest first
		c := hubsync.Comment{ID: cs[i].ID, Body: cs[i].Body, CreatedAt: cs[i].CreatedAt}
		if cs[i].Author != nil {
			c.Author = cs[i].Author.Login
		}
		it.Comments = append(it.Comments, c)
	}
	if g.UpdatedAt.After(updated) {
		updated = g.UpdatedAt
	}
	it.UpdatedAt = updated.UTC()
	it.Rev = it.UpdatedAt.Format(time.RFC3339)
	return it
}

// setStatus sets Status and StatusCategory from the board status (nil = none or no board) and
// the open/closed state.
func (a *Adapter) setStatus(it *hubsync.Item, status *statusValue, closed bool) {
	switch {
	case a.board != 0 && status != nil && status.Name != "":
		it.Status, it.StatusCategory = status.Name, categoryFor(status.Name)
	case a.board != 0: // not on the board, or no status set: the category follows open/closed
		it.Status, it.StatusCategory = "", hubsync.CategoryTodo
		if closed {
			it.StatusCategory = hubsync.CategoryDone
		}
	case closed:
		it.Status, it.StatusCategory = statusClosed, hubsync.CategoryDone
	default:
		it.Status, it.StatusCategory = statusOpen, hubsync.CategoryTodo
	}
}

// collector gathers changed items, dedups them and tracks the watermark. An issue listed in its
// own right (with its board status) replaces the copy seen earlier as a sub-issue.
//
// The since filter includes the boundary (GitHub's filterBy.since does too, and timestamps are
// to the second, so an item changed in the watermark's second after the last call must not be
// lost). An item at the boundary that the previous call returned with the same Rev (atSince) is
// skipped, so an unchanged item is not handed back on every tick.
type collector struct {
	since   time.Time
	wm      time.Time
	atSince map[string]string // id -> rev returned at the watermark by the previous call
	seen    map[string]int    // id -> index in items
	direct  map[string]bool
	items   []hubsync.Item
}

func (c *collector) add(it hubsync.Item, direct bool) {
	if it.UpdatedAt.Before(c.since) {
		return
	}
	if rev, ok := c.atSince[it.ID]; ok && rev == it.Rev && it.UpdatedAt.Equal(c.since) {
		return
	}
	if i, dup := c.seen[it.ID]; dup {
		if direct && !c.direct[it.ID] {
			c.items[i], c.direct[it.ID] = it, true
			c.mark(it)
		}
		return
	}
	c.seen[it.ID], c.direct[it.ID] = len(c.items), direct
	c.items = append(c.items, it)
	c.mark(it)
}

func (c *collector) mark(it hubsync.Item) { c.markTime(it.UpdatedAt) }

// markTime moves the watermark to t, for an item read but not kept.
func (c *collector) markTime(t time.Time) {
	if t.After(c.wm) {
		c.wm = t
	}
}

// ListChanged lists the issues changed since the watermark: with assigned_to me, those assigned
// to the logged-in account plus their sub-issues. With a board, the board's issue items (in the
// scope repository when one is set), every one carrying its board status, sub-issues included;
// otherwise the repository's issues. A board too large to walk in one tick is an error, and the
// watermark does not move; so is a GraphQL budget running low (a RateLimitError until the reset).
func (a *Adapter) ListChanged(ctx context.Context, since time.Time) ([]hubsync.Item, time.Time, error) {
	login := ""
	if a.cfg.AssignedTo != "anyone" {
		l, err := a.viewer(ctx)
		if err != nil {
			return nil, since, err
		}
		login = l
	}
	a.mu.Lock()
	var atSince map[string]string
	if !since.IsZero() && since.Equal(a.lastWM) {
		atSince = a.atWM
	}
	a.mu.Unlock()
	c := &collector{since: since, wm: since, atSince: atSince, seen: map[string]int{}, direct: map[string]bool{}}
	var err error
	if a.board != 0 {
		err = a.listBoard(ctx, login, c)
	} else {
		err = a.listRepo(ctx, login, c)
	}
	if err != nil {
		return nil, since, err
	}
	// remember what was handed back at the new watermark (and, when it did not move, what was
	// skipped there), for the next call from that watermark
	at := map[string]string{}
	if c.wm.Equal(since) {
		for id, rev := range atSince {
			at[id] = rev
		}
	}
	for _, it := range c.items {
		if it.UpdatedAt.Equal(c.wm) {
			at[it.ID] = it.Rev
		}
	}
	a.mu.Lock()
	a.lastWM, a.atWM = c.wm, at
	a.mu.Unlock()
	return c.items, c.wm, nil
}

// addWithChildren adds an issue and its sub-issues, reading the sub-issues past the first page.
// projectID is the board's id ("" without a board); a sub-issue's status comes from its own
// project item.
func (a *Adapter) addWithChildren(ctx context.Context, g ghIssue, status *statusValue, updated time.Time, projectID string, c *collector) error {
	c.add(a.item(g, status, updated), true)
	children, err := a.allSubIssues(ctx, g, projectID)
	for _, ch := range children {
		st, up := ch.boardStatus(projectID)
		c.add(a.item(ch, st, up), false)
	}
	return err
}

// allSubIssues is the issue's sub-issues, paging past the first page. A parent with more than
// the cap (25 + maxSubPages*50) keeps the ones read: one huge epic must not stop the sync. Its
// later sub-issues still come in on their own when they are the viewer's (or anyone's).
func (a *Adapter) allSubIssues(ctx context.Context, g ghIssue, projectID string) ([]ghIssue, error) {
	out := g.SubIssues.Nodes
	pi := g.SubIssues.PageInfo
	q, vars := qSubIssues, map[string]any{"id": g.ID}
	if projectID != "" {
		q, vars["field"] = qSubIssuesOnBoard, a.statusField
	}
	for page := 0; pi.HasNextPage && pi.EndCursor != ""; page++ {
		if page == maxSubPages {
			return out, nil
		}
		vars["cursor"] = pi.EndCursor
		var resp struct {
			Node *struct {
				SubIssues subIssues `json:"subIssues"`
			} `json:"node"`
		}
		if err := a.gql(ctx, q, vars, &resp); err != nil {
			return out, err
		}
		if resp.Node == nil {
			return out, fmt.Errorf("%w: github: issue %s", hubsync.ErrNotFound, g.ref().id())
		}
		out = append(out, resp.Node.SubIssues.Nodes...)
		pi = resp.Node.SubIssues.PageInfo
	}
	return out, nil
}

func (a *Adapter) listRepo(ctx context.Context, login string, c *collector) error {
	if a.repo == "" {
		return errors.New("github: tracker.scope needs project (a repository) or board (a project number)")
	}
	vars := map[string]any{"owner": a.owner, "name": a.repo}
	if !c.since.IsZero() {
		vars["since"] = c.since.UTC().Format(time.RFC3339)
	}
	top := c.since // the newest top-level issue read: the walk is in update order
	for page := 0; page < maxPages; page++ {
		var out struct {
			Repository *struct {
				Issues struct {
					PageInfo pageInfo  `json:"pageInfo"`
					Nodes    []ghIssue `json:"nodes"`
				} `json:"issues"`
			} `json:"repository"`
		}
		if err := a.gql(ctx, qIssues, vars, &out); err != nil {
			return err
		}
		if out.Repository == nil {
			return fmt.Errorf("%w: github: repository %s/%s", hubsync.ErrNotFound, a.owner, a.repo)
		}
		for _, g := range out.Repository.Issues.Nodes {
			if g.UpdatedAt.After(top) {
				top = g.UpdatedAt
			}
			if login != "" && !g.assignedTo(login) && !g.parentAssignedTo(login) {
				c.markTime(g.UpdatedAt) // read, not kept: the next tick need not read it again
				continue
			}
			if err := a.addWithChildren(ctx, g, nil, time.Time{}, "", c); err != nil {
				return err
			}
		}
		pi := out.Repository.Issues.PageInfo
		if !pi.HasNextPage || pi.EndCursor == "" {
			return nil
		}
		vars["cursor"] = pi.EndCursor
	}
	// Cut at the page cap: resume after the last top-level issue read, not after a newer
	// sub-issue, or the issues between them would be skipped.
	c.wm = top
	return nil
}

// boardEntry is one issue item of a light board page.
type boardEntry struct {
	itemUpdated time.Time
	status      *statusValue
	issue       boardIssue
}

type boardIssue struct {
	Typename   string    `json:"__typename"`
	ID         string    `json:"id"`
	Number     int       `json:"number"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Assignees struct {
		Nodes []struct{ Login string } `json:"nodes"`
	} `json:"assignees"`
	Parent *struct {
		ID string `json:"id"`
	} `json:"parent"`
	SubIssuesSummary *struct {
		Total int `json:"total"`
	} `json:"subIssuesSummary"`
}

func (e boardEntry) id() string {
	return refFrom(e.issue.Repository.NameWithOwner, e.issue.Number).id()
}

// updated is the later of the issue's and its card's times, as item() computes it.
func (e boardEntry) updated() time.Time {
	if e.issue.UpdatedAt.After(e.itemUpdated) {
		return e.issue.UpdatedAt
	}
	return e.itemUpdated
}

// hasChildren: the issue has sub-issues (or the count is unknown).
func (e boardEntry) hasChildren() bool {
	return e.issue.SubIssuesSummary == nil || e.issue.SubIssuesSummary.Total > 0
}

// subRef is a sub-issue as the probe reads it.
type subRef struct {
	ID         string    `json:"id"`
	Number     int       `json:"number"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

type subRefs struct {
	PageInfo pageInfo `json:"pageInfo"`
	Nodes    []subRef `json:"nodes"`
}

// changed says whether an item at time t is past the watermark and not the copy the previous
// call already returned at it.
func (c *collector) changed(id string, t time.Time) bool {
	if t.Before(c.since) {
		return false
	}
	rev, ok := c.atSince[id]
	return !(ok && t.Equal(c.since) && rev == t.UTC().Format(time.RFC3339))
}

// listBoard mirrors the board's issues in three steps, so a tick costs a few points even on a
// large board. (1) It walks the board with a light page (boardEntry). (2) It keeps the items
// assigned to login (all with "anyone"), in the scope repository when one is set, plus board
// items whose parent is a kept one. (3) It reads in full, by node id in batches, only the kept
// items changed since the watermark (with their sub-issues), and for the unchanged kept parents
// it probes the sub-issues by id and time and reads only the changed ones.
func (a *Adapter) listBoard(ctx context.Context, login string, c *collector) error {
	p, err := a.project(ctx)
	if err != nil {
		return err
	}
	entries, err := a.boardEntries(ctx, p)
	if err != nil {
		return err
	}
	onBoard := map[string]boardEntry{} // issue node id -> its board entry, any repository
	assigned := map[string]bool{}
	for _, e := range entries {
		onBoard[e.issue.ID] = e
		inRepo := a.repo == "" || strings.EqualFold(e.issue.Repository.NameWithOwner, a.owner+"/"+a.repo)
		if inRepo && (login == "" || hasLogin(e.issue.Assignees.Nodes, login)) {
			assigned[e.issue.ID] = true
		}
	}
	var kept []boardEntry
	keptIDs := map[string]bool{}
	for _, e := range entries {
		inRepo := a.repo == "" || strings.EqualFold(e.issue.Repository.NameWithOwner, a.owner+"/"+a.repo)
		if assigned[e.issue.ID] || (inRepo && e.issue.Parent != nil && assigned[e.issue.Parent.ID]) {
			kept = append(kept, e)
			keptIDs[e.issue.ID] = true
		}
	}

	var parents, leaves, probe []string
	for _, e := range kept {
		switch {
		case c.changed(e.id(), e.updated()) && e.hasChildren():
			parents = append(parents, e.issue.ID)
		case c.changed(e.id(), e.updated()):
			leaves = append(leaves, e.issue.ID)
		case e.hasChildren():
			probe = append(probe, e.issue.ID)
		}
	}
	probed, err := a.probeSubIssues(ctx, probe, c, keptIDs, onBoard)
	if err != nil {
		return err
	}
	for _, id := range probe {
		leaves = append(leaves, probed[id]...)
	}
	full := map[string]ghIssue{}
	if err := a.readNodes(ctx, qIssueNodes, parents, parentBatch, full); err != nil {
		return err
	}
	if err := a.readNodes(ctx, qChildNodes, leaves, leafBatch, full); err != nil {
		return err
	}

	for _, e := range kept {
		if g, ok := full[e.issue.ID]; ok {
			if err := a.addWithChildren(ctx, g, e.status, e.itemUpdated, p.ID, c); err != nil {
				return err
			}
		}
		for _, id := range probed[e.issue.ID] {
			if g, ok := full[id]; ok {
				st, up := g.boardStatus(p.ID)
				c.add(a.item(g, st, up), false)
			}
		}
	}
	return nil
}

// boardEntries walks the board's light pages and returns its issue items in board order.
func (a *Adapter) boardEntries(ctx context.Context, p *project) ([]boardEntry, error) {
	vars := map[string]any{"id": p.ID, "field": a.statusField}
	var entries []boardEntry
	for page := 0; page < maxBoardPages; page++ {
		var out struct {
			Node *struct {
				Items struct {
					PageInfo pageInfo `json:"pageInfo"`
					Nodes    []struct {
						UpdatedAt time.Time    `json:"updatedAt"`
						Field     *statusValue `json:"fieldValueByName"`
						Content   *boardIssue  `json:"content"`
					} `json:"nodes"`
				} `json:"items"`
			} `json:"node"`
		}
		if err := a.gql(ctx, qProjectItems, vars, &out); err != nil {
			return nil, err
		}
		if out.Node == nil {
			return nil, fmt.Errorf("%w: github: project %d of %s", hubsync.ErrNotFound, a.board, a.owner)
		}
		for _, n := range out.Node.Items.Nodes {
			if n.Content == nil || n.Content.Typename != "Issue" || n.Content.ID == "" {
				continue // draft issues and pull requests are not mirrored
			}
			entries = append(entries, boardEntry{itemUpdated: n.UpdatedAt, status: n.Field, issue: *n.Content})
		}
		pi := out.Node.Items.PageInfo
		if !pi.HasNextPage || pi.EndCursor == "" {
			return entries, nil
		}
		vars["cursor"] = pi.EndCursor
	}
	return nil, fmt.Errorf("github: project %d of %s has more than %d items; the sync reads a board in one walk, so it stopped without moving the watermark (narrow the board or use tracker.scope.project without a board)",
		a.board, a.owner, maxBoardPages*boardPageSize)
}

// probeSubIssues lists the sub-issues of unchanged parents by id and time and returns, per
// parent node id, the node ids of the sub-issues changed since the watermark. Kept board items
// are skipped (they are read on their own); a sub-issue on the board counts its card's time too.
// A parent with more sub-issues than the cap keeps the ones read, as allSubIssues does.
func (a *Adapter) probeSubIssues(ctx context.Context, parents []string, c *collector, skip map[string]bool, onBoard map[string]boardEntry) (map[string][]string, error) {
	out := map[string][]string{}
	for batch := range slices.Chunk(parents, probeBatch) {
		var resp struct {
			Nodes []*struct {
				ID        string  `json:"id"`
				SubIssues subRefs `json:"subIssues"`
			} `json:"nodes"`
		}
		if err := a.gql(ctx, qSubIssueProbe, map[string]any{"ids": batch}, &resp); err != nil {
			return nil, err
		}
		for _, n := range resp.Nodes {
			if n == nil || n.ID == "" {
				continue
			}
			kids, pi := n.SubIssues.Nodes, n.SubIssues.PageInfo
			for page := 0; pi.HasNextPage && pi.EndCursor != "" && page < maxSubPages; page++ {
				var more struct {
					Node *struct {
						SubIssues subRefs `json:"subIssues"`
					} `json:"node"`
				}
				if err := a.gql(ctx, qSubIssueIDs, map[string]any{"id": n.ID, "cursor": pi.EndCursor}, &more); err != nil {
					return nil, err
				}
				if more.Node == nil {
					break
				}
				kids, pi = append(kids, more.Node.SubIssues.Nodes...), more.Node.SubIssues.PageInfo
			}
			for _, k := range kids {
				if k.ID == "" || skip[k.ID] {
					continue
				}
				t := k.UpdatedAt
				if e, ok := onBoard[k.ID]; ok && e.itemUpdated.After(t) {
					t = e.itemUpdated
				}
				if c.changed(refFrom(k.Repository.NameWithOwner, k.Number).id(), t) {
					out[n.ID] = append(out[n.ID], k.ID)
				}
			}
		}
	}
	return out, nil
}

// readNodes reads issues in full by node id, in batches, into full. An id that no longer
// resolves (deleted since the walk) is left out.
func (a *Adapter) readNodes(ctx context.Context, query string, ids []string, size int, full map[string]ghIssue) error {
	var todo []string
	for _, id := range ids {
		if _, done := full[id]; !done && !slices.Contains(todo, id) {
			todo = append(todo, id)
		}
	}
	for batch := range slices.Chunk(todo, size) {
		var resp struct {
			Nodes []*ghIssue `json:"nodes"`
		}
		if err := a.gql(ctx, query, map[string]any{"ids": batch, "field": a.statusField}, &resp); err != nil {
			return err
		}
		for _, g := range resp.Nodes {
			if g != nil && g.ID != "" {
				full[g.ID] = *g
			}
		}
	}
	return nil
}

// Get reads one issue.
func (a *Adapter) Get(ctx context.Context, id string) (hubsync.Item, error) {
	r, err := a.parseID(id)
	if err != nil {
		return hubsync.Item{}, err
	}
	vars := map[string]any{"owner": r.owner, "name": r.repo, "number": r.number}
	q := qIssue
	var p *project
	if a.board != 0 {
		if p, err = a.project(ctx); err != nil {
			return hubsync.Item{}, err
		}
		q, vars["field"] = qIssueOnBoard, a.statusField
	}
	var out struct {
		Repository *struct {
			Issue *ghIssue `json:"issue"`
		} `json:"repository"`
	}
	if err := a.gql(ctx, q, vars, &out); err != nil {
		return hubsync.Item{}, err
	}
	if out.Repository == nil || out.Repository.Issue == nil {
		return hubsync.Item{}, fmt.Errorf("%w: github: %s", hubsync.ErrNotFound, r.id())
	}
	g := *out.Repository.Issue
	pid := ""
	if p != nil {
		pid = p.ID
	}
	status, updated := g.boardStatus(pid)
	return a.item(g, status, updated), nil
}

// ---- writes ----

const qIssueItems = `query IssueItems($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) { id state projectItems(first: 20) { nodes { id project { id } } } }
  }
}`

const mAddToProject = `mutation AddToProject($project: ID!, $content: ID!) {
  addProjectV2ItemById(input: {projectId: $project, contentId: $content}) { item { id } }
}`

const mSetStatus = `mutation SetStatus($project: ID!, $item: ID!, $field: ID!, $option: String!) {
  updateProjectV2ItemFieldValue(input: {projectId: $project, itemId: $item, fieldId: $field, value: {singleSelectOptionId: $option}}) {
    projectV2Item { id }
  }
}`

// Move sets the project status with a board (adding the issue to the board if needed), else
// opens or closes the issue. status is a status name or a category. With a board the issue's
// state follows too: a done status closes an open issue and any other reopens a closed one, as
// the project's built-in workflows may be off. The state goes first, so a workflow that sets a
// status on close or reopen does not override the status asked for.
func (a *Adapter) Move(ctx context.Context, id, status string) error {
	r, err := a.writeRef(ctx, id)
	if err != nil {
		return err
	}
	return a.move(ctx, r, status)
}

// repoState is the issue state ("open" or "closed") a status asks for without a board.
func repoState(status string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "open", "reopen", hubsync.CategoryBacklog, hubsync.CategoryTodo, hubsync.CategoryActive, hubsync.CategoryReview:
		return "open", nil
	case "closed", "close", hubsync.CategoryDone:
		return "closed", nil
	}
	return "", fmt.Errorf("github: status %q: without tracker.scope.board an issue is Open or Closed", status)
}

// move is Move for an id already checked against the scope.
func (a *Adapter) move(ctx context.Context, r ref, status string) error {
	if a.board == 0 {
		state, err := repoState(status)
		if err != nil {
			return err
		}
		return a.rest(ctx, "PATCH", r.path(), map[string]any{"state": state}, nil)
	}
	p, opt, err := a.projectOption(ctx, status)
	if err != nil {
		return err
	}
	itemID, state, err := a.ensureItem(ctx, r, p, "")
	if err != nil {
		return err
	}
	done := categoryFor(opt.Name) == hubsync.CategoryDone
	switch {
	case done && strings.EqualFold(state, "OPEN"):
		err = a.rest(ctx, "PATCH", r.path(), map[string]any{"state": "closed", "state_reason": "completed"}, nil)
	case !done && strings.EqualFold(state, "CLOSED"):
		err = a.rest(ctx, "PATCH", r.path(), map[string]any{"state": "open"}, nil)
	}
	if err != nil {
		return err
	}
	return a.gql(ctx, mSetStatus, map[string]any{"project": p.ID, "item": itemID, "field": p.FieldID, "option": opt.ID}, nil)
}

// option finds a status by name (any case), then by category (the first option in it).
func (p *project) option(status string) (option, error) {
	s := strings.TrimSpace(status)
	for _, o := range p.Options {
		if strings.EqualFold(o.Name, s) {
			return o, nil
		}
	}
	if hubsync.ValidCategory(strings.ToLower(s)) {
		for _, o := range p.Options {
			if categoryFor(o.Name) == strings.ToLower(s) {
				return o, nil
			}
		}
	}
	names := make([]string, len(p.Options))
	for i, o := range p.Options {
		names[i] = o.Name
	}
	return option{}, fmt.Errorf("github: project %q %w %q (statuses: %s)", p.Title, errNoOption, status, strings.Join(names, ", "))
}

// errNoOption: the project has no status by that name or category.
var errNoOption = errors.New("has no status")

// ensureItem is the issue's item on the project and its state (OPEN or CLOSED; "" when the
// lookup was skipped), adding the issue when it is not there. nodeID skips the lookup when the
// caller knows the issue's node id and that it is not on the board.
func (a *Adapter) ensureItem(ctx context.Context, r ref, p *project, nodeID string) (itemID, state string, err error) {
	if nodeID == "" {
		var out struct {
			Repository *struct {
				Issue *struct {
					ID           string `json:"id"`
					State        string `json:"state"`
					ProjectItems struct {
						Nodes []struct {
							ID      string `json:"id"`
							Project struct {
								ID string `json:"id"`
							} `json:"project"`
						} `json:"nodes"`
					} `json:"projectItems"`
				} `json:"issue"`
			} `json:"repository"`
		}
		if err := a.gql(ctx, qIssueItems, map[string]any{"owner": r.owner, "name": r.repo, "number": r.number}, &out); err != nil {
			return "", "", err
		}
		if out.Repository == nil || out.Repository.Issue == nil {
			return "", "", fmt.Errorf("%w: github: %s", hubsync.ErrNotFound, r.id())
		}
		state = out.Repository.Issue.State
		for _, it := range out.Repository.Issue.ProjectItems.Nodes {
			if it.Project.ID == p.ID {
				return it.ID, state, nil
			}
		}
		nodeID = out.Repository.Issue.ID
	}
	var added struct {
		Add struct {
			Item struct {
				ID string `json:"id"`
			} `json:"item"`
		} `json:"addProjectV2ItemById"`
	}
	if err := a.gql(ctx, mAddToProject, map[string]any{"project": p.ID, "content": nodeID}, &added); err != nil {
		return "", "", err
	}
	if added.Add.Item.ID == "" {
		return "", "", fmt.Errorf("github: adding %s to project %q returned no item", r.id(), p.Title)
	}
	return added.Add.Item.ID, state, nil
}

// Comment adds a comment; GitHub takes markdown as is.
func (a *Adapter) Comment(ctx context.Context, id, markdown string) error {
	r, err := a.writeRef(ctx, id)
	if err != nil {
		return err
	}
	return a.rest(ctx, "POST", r.path()+"/comments", map[string]any{"body": markdown}, nil)
}

// Create opens an issue in the scope repository (or the parent's), links it as a sub-issue of
// in.ParentID, adds it to the board and sets in.Status. in.Type is not used: GitHub issue types
// are per organization.
//
// Everything that can be checked first (the parent's scope, in.Status against the board's
// options or Open/Closed) is checked before the issue is opened, and fails with a plain error
// and an empty Item. Once the issue exists, Create never hides it: when a later step fails (the
// sub-issue link, the board add, the status, a rate limit on any of them) it returns the created
// issue (ID, Key, URL and what it was asked to set) and a *hubsync.PartialCreateError holding the
// same item and the cause. When only the final read-back fails, Create succeeds with that item.
// When the POST itself was sent but its outcome is unknown (a timeout, a dropped connection, a
// 5xx, an answer without an issue number) the error wraps hubsync.ErrCreateUncertain, so the
// engine looks the issue up before creating it again. The item id always uses GitHub's spelling
// of the owner and repository (from the create answer), the one ListChanged and Get return.
func (a *Adapter) Create(ctx context.Context, in hubsync.NewItem) (hubsync.Item, error) {
	var parent *ref
	target := ref{owner: a.owner, repo: a.repo}
	if strings.TrimSpace(in.ParentID) != "" {
		pr, err := a.writeRef(ctx, in.ParentID)
		if err != nil {
			return hubsync.Item{}, err
		}
		parent, target = &pr, ref{owner: pr.owner, repo: pr.repo}
	}
	if target.repo == "" {
		return hubsync.Item{}, errors.New("github: creating an issue needs tracker.scope.project (a repository) or a parent item")
	}
	wantStatus := strings.TrimSpace(in.Status) != ""
	var proj *project
	var opt option
	closed := false
	if a.board != 0 {
		var err error
		if wantStatus {
			proj, opt, err = a.projectOption(ctx, in.Status)
		} else {
			proj, err = a.project(ctx)
		}
		if err != nil {
			return hubsync.Item{}, err
		}
	} else if wantStatus {
		state, err := repoState(in.Status)
		if err != nil {
			return hubsync.Item{}, err
		}
		closed = state == "closed"
	}
	body := in.Body
	if strings.TrimSpace(in.Acceptance) != "" {
		if body != "" {
			body += "\n\n"
		}
		body += "## Acceptance criteria\n\n" + in.Acceptance
	}
	req := map[string]any{"title": in.Title, "body": body}
	if len(in.Labels) > 0 {
		req["labels"] = in.Labels
	}
	created, decodeErr, err := a.postIssue(ctx, target, req)
	if err != nil {
		return hubsync.Item{}, err
	}
	target.number = created.Number
	// the id uses GitHub's spelling of the names, as ListChanged and Get do
	canon := target
	if o, r, ok := repoFromURLs(created.RepositoryURL, created.HTMLURL); ok &&
		strings.EqualFold(o, target.owner) && strings.EqualFold(r, target.repo) {
		canon.owner, canon.repo = o, r
	}
	made := hubsync.Item{ID: canon.id(), Key: canon.key(), URL: created.HTMLURL, Title: in.Title, Body: body,
		Labels: slices.Clone(in.Labels), UpdatedAt: now().UTC()}
	a.setStatus(&made, nil, false)
	if parent != nil {
		made.ParentID = parent.id()
		if given, err := a.parseID(in.ParentID); err == nil {
			made.ParentID = given.id() // the engine's own spelling of its link
		}
	}
	partial := func(step string, err error) (hubsync.Item, error) {
		return made, &hubsync.PartialCreateError{Item: made, Err: fmt.Errorf("github: %s: %w", step, err)}
	}
	if decodeErr != nil || created.ID == 0 || created.NodeID == "" {
		if decodeErr == nil {
			decodeErr = errors.New("the answer has no issue id or node id")
		}
		return partial("reading the create answer", decodeErr)
	}
	if parent != nil {
		if err := a.rest(ctx, "POST", parent.path()+"/sub_issues", map[string]any{"sub_issue_id": created.ID}, nil); err != nil {
			return partial("linking it under "+parent.id(), err)
		}
	}
	if proj != nil {
		if _, _, err := a.ensureItem(ctx, target, proj, created.NodeID); err != nil {
			return partial("adding it to the board", err)
		}
	}
	if wantStatus {
		if err := a.move(ctx, target, in.Status); err != nil {
			return partial("setting its status", err)
		}
		if proj != nil {
			a.setStatus(&made, &statusValue{Name: opt.Name, OptionID: opt.ID}, false)
		} else {
			a.setStatus(&made, nil, closed)
		}
	}
	if got, err := a.Get(ctx, target.id()); err == nil {
		return got, nil
	}
	return made, nil // created and set up; the next ListChanged reads it in full
}

type createdIssue struct {
	ID            int64  `json:"id"`
	Number        int    `json:"number"`
	NodeID        string `json:"node_id"`
	HTMLURL       string `json:"html_url"`
	RepositoryURL string `json:"repository_url"`
}

// postIssue sends the create. err is set when no issue number is known: a plain error when
// GitHub refused the request (or it was never sent), hubsync.ErrCreateUncertain when it was sent
// and the issue may exist (a transport failure, a timeout, a 5xx, an answer without a number).
// decodeErr is set when the answer names the issue but could not be read in full.
func (a *Adapter) postIssue(ctx context.Context, target ref, req map[string]any) (created createdIssue, decodeErr, err error) {
	path := "repos/" + url.PathEscape(target.owner) + "/" + url.PathEscape(target.repo) + "/issues"
	in, err := json.Marshal(req)
	if err != nil {
		return created, nil, err
	}
	uncertain := func(cause error) error {
		return fmt.Errorf("github: creating an issue in %s/%s: %w (look for it before creating again): %w",
			target.owner, target.repo, hubsync.ErrCreateUncertain, cause)
	}
	r, err := a.send(ctx, "POST", path, in)
	if err != nil {
		var se *cred.SourceError
		if errors.Is(err, errCLIUnavailable) || errors.As(err, &se) {
			return created, nil, err // no credential: nothing was sent
		}
		return created, nil, uncertain(err)
	}
	if rl := rateLimit(r); rl != nil {
		return created, nil, rl
	}
	if r.status >= 500 {
		return created, nil, uncertain(apiError("POST", path, r))
	}
	if r.status < 200 || r.status > 299 {
		return created, nil, apiError("POST", path, r)
	}
	decodeErr = json.Unmarshal(r.body, &created) // a type error still fills the other fields
	if created.Number <= 0 {
		cause := errors.New("the answer has no issue number")
		if decodeErr != nil {
			cause = fmt.Errorf("decoding the answer: %w", decodeErr)
		}
		return createdIssue{}, nil, uncertain(cause)
	}
	return created, decodeErr, nil
}

// repoFromURLs is the owner and repository named by a REST issue's repository_url
// (".../repos/<owner>/<repo>") or, failing that, its html_url (".../<owner>/<repo>/issues/<n>").
func repoFromURLs(repositoryURL, htmlURL string) (owner, repo string, ok bool) {
	if u, err := url.Parse(repositoryURL); err == nil && repositoryURL != "" {
		seg := strings.Split(strings.Trim(u.Path, "/"), "/")
		if n := len(seg); n >= 3 && seg[n-3] == "repos" && seg[n-2] != "" && seg[n-1] != "" {
			return seg[n-2], seg[n-1], true
		}
	}
	if u, err := url.Parse(htmlURL); err == nil && htmlURL != "" {
		seg := strings.Split(strings.Trim(u.Path, "/"), "/")
		if n := len(seg); n >= 4 && seg[n-2] == "issues" && seg[n-4] != "" && seg[n-3] != "" {
			return seg[n-4], seg[n-3], true
		}
	}
	return "", "", false
}

// Update edits the title and/or body; nil leaves a field as it is.
func (a *Adapter) Update(ctx context.Context, id string, title, body *string) error {
	req := map[string]any{}
	if title != nil {
		req["title"] = *title
	}
	if body != nil {
		req["body"] = *body
	}
	if len(req) == 0 {
		return nil
	}
	r, err := a.writeRef(ctx, id)
	if err != nil {
		return err
	}
	return a.rest(ctx, "PATCH", r.path(), req, nil)
}

// SetLabels adds and removes labels. Removing a label the issue does not have is not an error.
func (a *Adapter) SetLabels(ctx context.Context, id string, add, remove []string) error {
	r, err := a.writeRef(ctx, id)
	if err != nil {
		return err
	}
	if len(add) > 0 {
		if err := a.rest(ctx, "POST", r.path()+"/labels", map[string]any{"labels": add}, nil); err != nil {
			return err
		}
	}
	for _, l := range remove {
		err := a.rest(ctx, "DELETE", r.path()+"/labels/"+url.PathEscape(l), nil, nil)
		if err != nil && !errors.Is(err, hubsync.ErrNotFound) {
			return err
		}
	}
	return nil
}

var _ hubsync.Adapter = (*Adapter)(nil)
