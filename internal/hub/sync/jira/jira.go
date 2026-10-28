// Package jira is the Jira Cloud board sync adapter (spec B §7), over REST API v3.
//
// Scope keys (manifest tracker.scope): org (or site) is the Atlassian site, "acme" or
// "acme.atlassian.net"; project is the project key (optional: without it the whole site's
// assigned items are mirrored and Create needs a parent); email is the account email when the
// stored token is the API token alone; issue_type is the type Create uses for a top-level item
// (default Task); acceptance_field is the custom field holding acceptance criteria
// ("customfield_10050"), else acceptance goes into the description.
//
// Credentials come from the cred resolver: a keychain or env token is the account email and an
// API token ("email:token", or the token with scope email), sent as Basic auth. The cli source is
// acli (the Atlassian CLI), used only when it is installed and logged in; tq then drives acli
// instead of calling REST itself.
package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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

// Provider is the tracker id.
const Provider = "jira"

func init() {
	hubsync.Register(Provider, func(cfg hubsync.Config) (hubsync.Adapter, error) { return newAdapter(cfg) })
	cred.RegisterCLI(Provider, cliHook)
}

const (
	pageSize     = 100
	maxPages     = 50 // 5000 items per tick, then the next tick continues from the watermark
	maxBody      = 16 << 20
	maxErrText   = 400
	reqTimeout   = 30 * time.Second
	defaultType  = "Task"
	subtaskType  = "Subtask"
	jiraTimeForm = "2006-01-02T15:04:05.000-0700"
)

// Adapter is the Jira Cloud adapter.
type Adapter struct {
	cfg             hubsync.Config
	base            string // https://<site>.atlassian.net, or cfg.BaseURL
	project         string
	email           string
	issueType       string
	acceptanceField string

	mu gosync.Mutex
	tr cred.Transport

	now func() time.Time
	run runFunc // acli
}

var _ hubsync.Adapter = (*Adapter)(nil)

var siteRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// siteURL is the https origin of an Atlassian Cloud site. Anything else is refused: the token is
// only ever sent to *.atlassian.net.
func siteURL(org string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(org))
	s = strings.TrimSuffix(strings.TrimPrefix(s, "https://"), "/")
	if siteRe.MatchString(s) {
		return "https://" + s + ".atlassian.net", nil
	}
	if host, ok := strings.CutSuffix(s, ".atlassian.net"); ok && siteRe.MatchString(host) {
		return "https://" + s, nil
	}
	if org == "" {
		return "", errors.New("jira: tracker.scope.org (the site: acme or acme.atlassian.net) is required")
	}
	return "", fmt.Errorf("jira: tracker.scope.org %q is not an Atlassian Cloud site (acme or acme.atlassian.net)", org)
}

func newAdapter(cfg hubsync.Config) (*Adapter, error) {
	sc := cfg.Scope
	a := &Adapter{cfg: cfg, project: strings.TrimSpace(sc["project"]), email: strings.TrimSpace(sc["email"]),
		issueType: strings.TrimSpace(sc["issue_type"]), acceptanceField: strings.TrimSpace(sc["acceptance_field"]),
		tr: cfg.Transport, now: time.Now, run: runACLI}
	if a.issueType == "" {
		a.issueType = defaultType
	}
	org := sc["org"]
	if org == "" {
		org = sc["site"]
	}
	if cfg.BaseURL != "" {
		a.base = strings.TrimSuffix(cfg.BaseURL, "/")
	} else {
		u, err := siteURL(org)
		if err != nil {
			return nil, err
		}
		a.base = u
	}
	return a, nil
}

func (a *Adapter) Name() string { return Provider }

// transport is the configured credential, else the cred resolver's answer (kept for the
// adapter's life: a Transport fetches a fresh token per request).
func (a *Adapter) transport(ctx context.Context) (cred.Transport, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tr != nil {
		return a.tr, nil
	}
	req := a.cfg.CredRequest()
	req.Provider = Provider
	tr, err := cred.Resolve(ctx, req)
	if err != nil {
		return nil, err
	}
	a.tr = tr
	return tr, nil
}

// useACLI is whether the credential is the acli login.
func (a *Adapter) useACLI(ctx context.Context) (bool, error) {
	tr, err := a.transport(ctx)
	if err != nil {
		return false, err
	}
	return tr.Source() == cred.SourceCLI, nil
}

// basicAuth turns the stored "email:token" (or the token plus scope email) into Basic auth.
type basicAuth struct {
	inner cred.Transport
	email string
}

func (b basicAuth) Source() cred.Source { return b.inner.Source() }

func (b basicAuth) Token(ctx context.Context) (cred.Token, error) {
	t, err := b.inner.Token(ctx)
	if err != nil {
		return cred.Token{}, err
	}
	switch {
	case strings.EqualFold(t.Scheme, "Basic") && !strings.Contains(t.Value, ":"):
		return cred.Token{Scheme: "Basic", Value: t.Value}, nil // already encoded
	case strings.Contains(t.Value, ":"):
		return cred.Token{Scheme: "Basic", Value: base64.StdEncoding.EncodeToString([]byte(t.Value))}, nil
	case b.email != "":
		return cred.Token{Scheme: "Basic", Value: base64.StdEncoding.EncodeToString([]byte(b.email + ":" + t.Value))}, nil
	}
	return cred.Token{}, &cred.SourceError{Source: b.inner.Source(), Provider: Provider,
		Err: errors.New("a Jira API token needs the account email: store it as email:token, or set tracker.scope.email")}
}

// apiError is a non-2xx answer.
type apiError struct {
	Method, Path string
	Status       int
	Source       cred.Source
	Text         string
}

func (e *apiError) Error() string {
	msg := fmt.Sprintf("jira: %s %s: %d %s", e.Method, e.Path, e.Status, http.StatusText(e.Status))
	if e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden {
		msg += fmt.Sprintf(" (check the jira credential from %s)", e.Source)
	}
	if e.Text != "" {
		msg += ": " + e.Text
	}
	return msg
}

// errorText is the bounded errorMessages/errors text of a Jira error body.
func errorText(body []byte) string {
	var e struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	var parts []string
	if json.Unmarshal(body, &e) == nil {
		parts = append(parts, e.ErrorMessages...)
		for _, k := range slices.Sorted(func(yield func(string) bool) {
			for k := range e.Errors {
				if !yield(k) {
					return
				}
			}
		}) {
			parts = append(parts, k+": "+e.Errors[k])
		}
	}
	s := strings.Join(parts, "; ")
	if len(s) > maxErrText {
		s = s[:maxErrText] + "..."
	}
	return s
}

// retryAfter reads Retry-After (seconds or an HTTP date), else X-RateLimit-Reset (RFC 3339).
func retryAfter(h http.Header, now time.Time) time.Duration {
	if v := strings.TrimSpace(h.Get("Retry-After")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
		if t, err := http.ParseTime(v); err == nil && t.After(now) {
			return t.Sub(now).Round(time.Second)
		}
	}
	if v := strings.TrimSpace(h.Get("X-RateLimit-Reset")); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil && t.After(now) {
			return t.Sub(now).Round(time.Second)
		}
	}
	return 0
}

// do sends one REST request. in is JSON-encoded when non-nil; out is decoded when non-nil.
func (a *Adapter) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	tr, err := a.transport(ctx)
	if err != nil {
		return err
	}
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	u := a.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	ctx, cancel := context.WithTimeout(ctx, reqTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := cred.Client(basicAuth{inner: tr, email: a.email}, nil).Do(req)
	if err != nil {
		var se *cred.SourceError
		if errors.As(err, &se) {
			return se
		}
		return fmt.Errorf("jira: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("jira: %s %s: reading the answer: %w", method, path, err)
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == http.StatusServiceUnavailable && resp.Header.Get("Retry-After") != "":
		return &hubsync.RateLimitError{Provider: Provider, RetryAfter: retryAfter(resp.Header, a.now())}
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: jira %s %s", hubsync.ErrNotFound, method, path)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return &apiError{Method: method, Path: path, Status: resp.StatusCode, Source: tr.Source(), Text: errorText(raw)}
	}
	if out != nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("jira: %s %s: unexpected answer: %w", method, path, err)
		}
	}
	return nil
}

func issuePath(id string, rest ...string) string {
	p := "/rest/api/3/issue/" + url.PathEscape(id)
	for _, r := range rest {
		p += "/" + r
	}
	return p
}

// Category maps a Jira status category key to a hub lane kind.
func Category(key string) string {
	switch key {
	case "new":
		return hubsync.CategoryTodo
	case "indeterminate":
		return hubsync.CategoryActive
	case "done":
		return hubsync.CategoryDone
	}
	return hubsync.CategoryBacklog
}

type jiraStatus struct {
	Name           string `json:"name"`
	StatusCategory struct {
		Key string `json:"key"`
	} `json:"statusCategory"`
}

type user struct {
	AccountID    string `json:"accountId"`
	EmailAddress string `json:"emailAddress"`
	DisplayName  string `json:"displayName"`
}

func (u *user) name() string {
	if u == nil {
		return ""
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.AccountID
}

// Check verifies the credential: the account's email, else its display name.
func (a *Adapter) Check(ctx context.Context) (string, error) {
	if cli, err := a.useACLI(ctx); err != nil {
		return "", err
	} else if cli {
		return a.cliCheck(ctx)
	}
	var me user
	if err := a.do(ctx, "GET", "/rest/api/3/myself", nil, nil, &me); err != nil {
		return "", err
	}
	switch {
	case me.EmailAddress != "":
		return me.EmailAddress, nil
	case me.DisplayName != "":
		return me.DisplayName, nil
	}
	return me.AccountID, nil
}

// Statuses lists the project's statuses (or the site's, with no project), deduplicated by name.
func (a *Adapter) Statuses(ctx context.Context) ([]hubsync.Status, error) {
	if cli, err := a.useACLI(ctx); err != nil {
		return nil, err
	} else if cli {
		return nil, fmt.Errorf("%w: acli cannot list statuses; map lanes with tracker.lanes or use a keychain or env token", hubsync.ErrUnsupported)
	}
	var flat []jiraStatus
	if a.project != "" {
		var types []struct {
			Statuses []jiraStatus `json:"statuses"`
		}
		if err := a.do(ctx, "GET", "/rest/api/3/project/"+url.PathEscape(a.project)+"/statuses", nil, nil, &types); err != nil {
			return nil, err
		}
		for _, t := range types {
			flat = append(flat, t.Statuses...)
		}
	} else if err := a.do(ctx, "GET", "/rest/api/3/status", nil, nil, &flat); err != nil {
		return nil, err
	}
	var out []hubsync.Status
	seen := map[string]bool{}
	for _, s := range flat {
		k := strings.ToLower(s.Name)
		if s.Name == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, hubsync.Status{Name: s.Name, Category: Category(s.StatusCategory.Key)})
	}
	return out, nil
}

// jqlQuote is a JQL string literal.
func jqlQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// jql is the ListChanged query. "updated" uses a relative minute offset, which JQL reads the same
// whatever the account's time zone, with a minute of overlap; repeats carry the same Rev. The
// first sync (zero since) mirrors every open item instead.
func (a *Adapter) jql(since time.Time) string {
	var cl []string
	if a.project != "" {
		cl = append(cl, "project = "+jqlQuote(a.project))
	}
	if a.cfg.AssignedTo != "anyone" {
		cl = append(cl, "assignee = currentUser()")
	}
	if since.IsZero() {
		cl = append(cl, "statusCategory != Done")
	} else {
		mins := int(math.Ceil(a.now().Sub(since).Minutes())) + 1
		if mins < 1 {
			mins = 1
		}
		cl = append(cl, fmt.Sprintf(`updated >= "-%dm"`, mins))
	}
	return strings.Join(cl, " AND ") + " ORDER BY updated ASC"
}

func (a *Adapter) fields() []string {
	f := []string{"summary", "status", "assignee", "parent", "labels", "updated", "description", "comment", "attachment", "issuetype", "project"}
	if a.acceptanceField != "" {
		f = append(f, a.acceptanceField)
	}
	return f
}

// ListChanged lists the mirrored items changed since the watermark, all pages; the new watermark
// is the latest updated time seen (since when nothing changed).
func (a *Adapter) ListChanged(ctx context.Context, since time.Time) ([]hubsync.Item, time.Time, error) {
	cli, err := a.useACLI(ctx)
	if err != nil {
		return nil, since, err
	}
	var issues []issue
	if cli {
		issues, err = a.cliSearch(ctx, a.jql(since))
	} else {
		issues, err = a.search(ctx, a.jql(since))
	}
	if err != nil {
		return nil, since, err
	}
	mark := since
	items := make([]hubsync.Item, 0, len(issues))
	for _, is := range issues {
		it := a.item(is)
		if it.UpdatedAt.After(mark) {
			mark = it.UpdatedAt
		}
		items = append(items, it)
	}
	return items, mark, nil
}

func (a *Adapter) search(ctx context.Context, jql string) ([]issue, error) {
	var all []issue
	token := ""
	for page := 0; page < maxPages; page++ {
		in := map[string]any{"jql": jql, "fields": a.fields(), "maxResults": pageSize}
		if token != "" {
			in["nextPageToken"] = token
		}
		var out struct {
			Issues        []issue `json:"issues"`
			NextPageToken string  `json:"nextPageToken"`
			IsLast        *bool   `json:"isLast"`
		}
		if err := a.do(ctx, "POST", "/rest/api/3/search/jql", nil, in, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Issues...)
		if out.NextPageToken == "" || (out.IsLast != nil && *out.IsLast) || out.NextPageToken == token {
			break
		}
		token = out.NextPageToken
	}
	return all, nil
}

// Get reads one item with its rendered fields (the body fallback when the ADF has no text).
func (a *Adapter) Get(ctx context.Context, id string) (hubsync.Item, error) {
	if cli, err := a.useACLI(ctx); err != nil {
		return hubsync.Item{}, err
	} else if cli {
		is, err := a.cliView(ctx, id)
		if err != nil {
			return hubsync.Item{}, err
		}
		return a.item(is), nil
	}
	var is issue
	q := url.Values{"expand": {"renderedFields"}, "fields": {strings.Join(a.fields(), ",")}}
	if err := a.do(ctx, "GET", issuePath(id), q, nil, &is); err != nil {
		return hubsync.Item{}, err
	}
	return a.item(is), nil
}

type transition struct {
	ID   string     `json:"id"`
	Name string     `json:"name"`
	To   jiraStatus `json:"to"`
}

// Move runs the transition whose target status is status (case-insensitive). With none, the error
// names the transitions available from the item's current status.
func (a *Adapter) Move(ctx context.Context, id, status string) error {
	if cli, err := a.useACLI(ctx); err != nil {
		return err
	} else if cli {
		_, err := a.acli(ctx, "jira", "workitem", "transition", "--key", id, "--status", status, "--yes")
		return err
	}
	var out struct {
		Transitions []transition `json:"transitions"`
	}
	if err := a.do(ctx, "GET", issuePath(id, "transitions"), nil, nil, &out); err != nil {
		return err
	}
	var avail []string
	for _, t := range out.Transitions {
		if strings.EqualFold(strings.TrimSpace(t.To.Name), strings.TrimSpace(status)) {
			return a.do(ctx, "POST", issuePath(id, "transitions"), nil, map[string]any{"transition": map[string]any{"id": t.ID}}, nil)
		}
		avail = append(avail, t.Name+" -> "+t.To.Name)
	}
	if len(avail) == 0 {
		return fmt.Errorf("jira: %s has no transitions to %q (none available from its current status)", id, status)
	}
	return fmt.Errorf("jira: %s has no transition to %q (available: %s)", id, status, strings.Join(avail, ", "))
}

// Comment adds a comment, converted from markdown to ADF.
func (a *Adapter) Comment(ctx context.Context, id, markdown string) error {
	if strings.TrimSpace(markdown) == "" {
		return errors.New("jira: an empty comment is not sent")
	}
	if cli, err := a.useACLI(ctx); err != nil {
		return err
	} else if cli {
		_, err := a.acli(ctx, "jira", "workitem", "comment", "create", "--key", id, "--body", markdown)
		return err
	}
	return a.do(ctx, "POST", issuePath(id, "comment"), nil, map[string]any{"body": markdownToADF(markdown)}, nil)
}

// projectOf is the project key of an issue key ("ACME-12" -> "ACME").
func projectOf(key string) string {
	i := strings.LastIndex(key, "-")
	if i <= 0 {
		return ""
	}
	if _, err := strconv.Atoi(key[i+1:]); err != nil {
		return ""
	}
	return key[:i]
}

// subtaskTypeID is the project's sub-task issue type id ("" when it cannot be read).
func (a *Adapter) subtaskTypeID(ctx context.Context, project string) string {
	var out struct {
		IssueTypes []struct {
			ID      string `json:"id"`
			Subtask bool   `json:"subtask"`
		} `json:"issueTypes"`
		Values []struct {
			ID      string `json:"id"`
			Subtask bool   `json:"subtask"`
		} `json:"values"`
	}
	if err := a.do(ctx, "GET", "/rest/api/3/issue/createmeta/"+url.PathEscape(project)+"/issuetypes", nil, nil, &out); err != nil {
		return ""
	}
	for _, t := range append(out.IssueTypes, out.Values...) {
		if t.Subtask {
			return t.ID
		}
	}
	return ""
}

// Create makes an item; with ParentID it is a sub-task of that item, in the parent's project.
func (a *Adapter) Create(ctx context.Context, in hubsync.NewItem) (hubsync.Item, error) {
	if strings.TrimSpace(in.Title) == "" {
		return hubsync.Item{}, errors.New("jira: an item needs a title")
	}
	project := a.project
	if in.ParentID != "" {
		if p := projectOf(in.ParentID); p != "" {
			project = p
		}
	}
	if project == "" {
		return hubsync.Item{}, errors.New("jira: creating a top-level item needs tracker.scope.project")
	}
	if err := checkLabels(in.Labels); err != nil {
		return hubsync.Item{}, err
	}
	body := in.Body
	var acceptance any
	if strings.TrimSpace(in.Acceptance) != "" {
		if a.acceptanceField != "" {
			acceptance = markdownToADF(in.Acceptance)
		} else {
			body = strings.TrimSpace(body + "\n\n## Acceptance\n\n" + in.Acceptance)
		}
	}
	cli, err := a.useACLI(ctx)
	if err != nil {
		return hubsync.Item{}, err
	}
	var key string
	if cli {
		key, err = a.cliCreate(ctx, project, in, body)
	} else {
		key, err = a.restCreate(ctx, project, in, body, acceptance)
	}
	if err != nil {
		return hubsync.Item{}, err
	}
	if in.Status != "" {
		if err := a.Move(ctx, key, in.Status); err != nil {
			return hubsync.Item{}, fmt.Errorf("jira: created %s, but setting its status: %w", key, err)
		}
	}
	return a.Get(ctx, key)
}

func (a *Adapter) restCreate(ctx context.Context, project string, in hubsync.NewItem, body string, acceptance any) (string, error) {
	fields := map[string]any{"project": map[string]any{"key": project}, "summary": in.Title}
	switch {
	case in.Type != "":
		fields["issuetype"] = map[string]any{"name": in.Type}
	case in.ParentID != "":
		if id := a.subtaskTypeID(ctx, project); id != "" {
			fields["issuetype"] = map[string]any{"id": id}
		} else {
			fields["issuetype"] = map[string]any{"name": subtaskType}
		}
	default:
		fields["issuetype"] = map[string]any{"name": a.issueType}
	}
	if in.ParentID != "" {
		ref := "key"
		if _, err := strconv.Atoi(in.ParentID); err == nil {
			ref = "id"
		}
		fields["parent"] = map[string]any{ref: in.ParentID}
	}
	if strings.TrimSpace(body) != "" {
		fields["description"] = markdownToADF(body)
	}
	if acceptance != nil {
		fields[a.acceptanceField] = acceptance
	}
	if len(in.Labels) > 0 {
		fields["labels"] = in.Labels
	}
	var out struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	if err := a.do(ctx, "POST", "/rest/api/3/issue", nil, map[string]any{"fields": fields}, &out); err != nil {
		return "", err
	}
	if out.Key == "" {
		return out.ID, nil
	}
	return out.Key, nil
}

// Update sets the title and/or the body (markdown, sent as ADF). Both nil sends nothing.
func (a *Adapter) Update(ctx context.Context, id string, title, body *string) error {
	if title == nil && body == nil {
		return nil
	}
	if title != nil && strings.TrimSpace(*title) == "" {
		return errors.New("jira: the title cannot be empty")
	}
	if cli, err := a.useACLI(ctx); err != nil {
		return err
	} else if cli {
		args := []string{"jira", "workitem", "edit", "--key", id}
		if title != nil {
			args = append(args, "--summary", *title)
		}
		if body != nil {
			args = append(args, "--description", *body)
		}
		_, err := a.acli(ctx, append(args, "--yes")...)
		return err
	}
	fields := map[string]any{}
	if title != nil {
		fields["summary"] = *title
	}
	if body != nil {
		if strings.TrimSpace(*body) == "" {
			fields["description"] = nil
		} else {
			fields["description"] = markdownToADF(*body)
		}
	}
	return a.do(ctx, "PUT", issuePath(id), nil, map[string]any{"fields": fields}, nil)
}

// checkLabels refuses what Jira refuses: an empty label or one with whitespace.
func checkLabels(ls ...[]string) error {
	for _, l := range ls {
		for _, s := range l {
			if s == "" || strings.ContainsFunc(s, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }) {
				return fmt.Errorf("jira: label %q: labels cannot be empty or contain spaces", s)
			}
		}
	}
	return nil
}

// SetLabels adds and removes labels in one update. Nothing to change sends nothing.
func (a *Adapter) SetLabels(ctx context.Context, id string, add, remove []string) error {
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	if err := checkLabels(add, remove); err != nil {
		return err
	}
	if cli, err := a.useACLI(ctx); err != nil {
		return err
	} else if cli {
		args := []string{"jira", "workitem", "edit", "--key", id}
		if len(add) > 0 {
			args = append(args, "--labels", strings.Join(add, ","))
		}
		if len(remove) > 0 {
			args = append(args, "--remove-labels", strings.Join(remove, ","))
		}
		_, err := a.acli(ctx, append(args, "--yes")...)
		return err
	}
	ops := make([]any, 0, len(add)+len(remove))
	for _, l := range add {
		ops = append(ops, map[string]any{"add": l})
	}
	for _, l := range remove {
		ops = append(ops, map[string]any{"remove": l})
	}
	return a.do(ctx, "PUT", issuePath(id), nil, map[string]any{"update": map[string]any{"labels": ops}}, nil)
}
