package asana

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	gosync "sync"
	"testing"
	"time"
)

// The fake Asana API: an in-memory workspace "100" (acme) with one project "200" and its sections,
// served the way https://app.asana.com/api/1.0 answers (every body wrapped in {"data": ...},
// lists paginated with next_page.offset). Fixtures use the names tester and acme only.

const (
	testToken = "tok-acme-s3cr3t" // a fake fixture token, tq:allow-secret
	wsGID     = "100"
	projGID   = "200"
)

type fakeTask struct {
	GID, Name, Notes, HTML string
	Completed              bool
	Modified               time.Time
	Assignee               string // "" or "me"
	Parent                 string
	Tags                   []string // tag gids
	Section                string   // section gid in projGID; "" = not in the project
	Acceptance             string
}

type fakeStory struct {
	GID, Task, Subtype, Text, HTML, Author string
	Created                                time.Time
}

type fakeAsana struct {
	t          *testing.T
	srv        *httptest.Server
	mu         gosync.Mutex
	tasks      map[string]*fakeTask
	sections   [][2]string // gid, name, in order
	tags       map[string]string
	stories    []fakeStory
	attach     map[string][][2]string // task -> name, permanent_url
	pageSize   int
	next       int
	limited    int    // answer this many requests with 429 first
	limitAt    int    // answer the request with this 1-based number with 429
	failOn     string // "METHOD /api/1.0/path": answer it with a 500
	limitOn    string // "METHOD /api/1.0/path": answer its next request with a 429, once
	reqs       []string
	queries    []string
	bodies     []map[string]any
	typeaheads []string // the query of each tag typeahead
}

func newFake(t *testing.T) *fakeAsana {
	t.Helper()
	f := &fakeAsana{t: t, tasks: map[string]*fakeTask{}, tags: map[string]string{"900": "bug", "901": "agent-ready"},
		attach: map[string][][2]string{}, pageSize: 2, next: 5000}
	f.sections = [][2]string{{"300", "Backlog"}, {"301", "To do"}, {"302", "Doing"}, {"303", "In Review"}, {"304", "QA"}, {"305", "Done"}}
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	f.tasks["1001"] = &fakeTask{GID: "1001", Name: "Login page", Notes: "Build the login page.", Modified: base, Assignee: "me",
		Section: "302", Tags: []string{"900"}, Acceptance: "Given a user, when they log in, then they see the board."}
	f.tasks["1002"] = &fakeTask{GID: "1002", Name: "Old task", Modified: base.Add(-48 * time.Hour), Assignee: "me", Section: "301"}
	f.tasks["1003"] = &fakeTask{GID: "1003", Name: "Other project", Modified: base.Add(time.Hour), Assignee: "me"}
	f.tasks["1004"] = &fakeTask{GID: "1004", Name: "Shipped", Modified: base.Add(2 * time.Hour), Assignee: "me", Section: "303", Completed: true}
	f.tasks["1005"] = &fakeTask{GID: "1005", Name: "Child step", Modified: base.Add(3 * time.Hour), Parent: "1001"}
	f.tasks["1006"] = &fakeTask{GID: "1006", Name: "Rich notes", HTML: "<body>Line <strong>one</strong></body>", Modified: base.Add(30 * time.Minute), Section: "300"}
	// completed long ago: a first sync (completed_since = now) never sees it
	f.tasks["1007"] = &fakeTask{GID: "1007", Name: "Ancient", Modified: base.Add(-30 * 24 * time.Hour), Assignee: "me", Section: "305", Completed: true}
	// a subtask assigned to me that is not itself a member of the project
	f.tasks["1008"] = &fakeTask{GID: "1008", Name: "My child step", Modified: base.Add(4 * time.Hour), Assignee: "me", Parent: "1001"}
	f.stories = []fakeStory{
		{GID: "7001", Task: "1001", Subtype: "comment_added", Text: "first", Author: "tester", Created: base.Add(-time.Hour)},
		{GID: "7002", Task: "1001", Subtype: "section_changed", Text: "moved", Author: "tester", Created: base.Add(-time.Minute * 30)},
		{GID: "7003", Task: "1001", Subtype: "comment_added", Text: "second", Author: "tester", Created: base},
	}
	f.attach["1001"] = [][2]string{{"mock.png", "https://app.asana.com/app/asana/-/get_asset?asset_id=1"}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAsana) section(gid string) string {
	for _, s := range f.sections {
		if s[0] == gid {
			return s[1]
		}
	}
	return ""
}

func (f *fakeAsana) taskJSON(tk *fakeTask) map[string]any {
	m := map[string]any{"gid": tk.GID, "resource_type": "task", "name": tk.Name, "notes": tk.Notes, "html_notes": tk.HTML,
		"completed": tk.Completed, "modified_at": tk.Modified.Format("2006-01-02T15:04:05.000Z"),
		"permalink_url": "https://app.asana.com/0/" + projGID + "/" + tk.GID}
	if tk.Assignee == "me" {
		m["assignee"] = map[string]any{"gid": "1", "name": "tester", "email": "tester@acme.test"}
	} else {
		m["assignee"] = nil
	}
	if tk.Parent != "" {
		m["parent"] = map[string]any{"gid": tk.Parent, "name": f.tasks[tk.Parent].Name}
	} else {
		m["parent"] = nil
	}
	tags := []any{}
	for _, g := range tk.Tags {
		tags = append(tags, map[string]any{"gid": g, "name": f.tags[g]})
	}
	m["tags"] = tags
	mem := []any{}
	if tk.Section != "" {
		mem = append(mem, map[string]any{"project": map[string]any{"gid": projGID, "name": "Platform"},
			"section": map[string]any{"gid": tk.Section, "name": f.section(tk.Section)}})
	}
	if tk.GID == "1003" {
		mem = append(mem, map[string]any{"project": map[string]any{"gid": "299"}, "section": map[string]any{"gid": "399", "name": "Doing"}})
	}
	m["memberships"] = mem
	cf := []any{map[string]any{"gid": "800", "name": "Priority", "display_value": "High", "text_value": nil}}
	if tk.Acceptance != "" {
		cf = append(cf, map[string]any{"gid": "801", "name": "Acceptance Criteria", "display_value": tk.Acceptance, "text_value": tk.Acceptance})
	}
	m["custom_fields"] = cf
	n := 0
	for _, o := range f.tasks {
		if o.Parent == tk.GID {
			n++
		}
	}
	m["num_subtasks"] = n
	return m
}

func (f *fakeAsana) write(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		f.t.Errorf("encode: %v", err)
	}
}

func (f *fakeAsana) fail(w http.ResponseWriter, code int, msg string) {
	f.write(w, code, map[string]any{"errors": []any{map[string]any{"message": msg}}})
}

// page serves items[offset:offset+pageSize] with a next_page when more remain.
func (f *fakeAsana) page(w http.ResponseWriter, r *http.Request, items []any) {
	off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	end := min(off+f.pageSize, len(items))
	if off > len(items) {
		off = len(items)
	}
	out := map[string]any{"data": items[off:end], "next_page": nil}
	if end < len(items) {
		out["next_page"] = map[string]any{"offset": strconv.Itoa(end), "path": r.URL.Path, "uri": f.srv.URL + r.URL.Path}
	}
	f.write(w, 200, out)
}

func (f *fakeAsana) sortedTasks(keep func(*fakeTask) bool) []any {
	var ids []string
	for id, tk := range f.tasks {
		if keep(tk) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := []any{}
	for _, id := range ids {
		out = append(out, f.taskJSON(f.tasks[id]))
	}
	return out
}

func (f *fakeAsana) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, r.Method+" "+r.URL.Path)
	f.queries = append(f.queries, r.URL.RawQuery)
	var body map[string]any
	if r.Body != nil && (r.Method == "POST" || r.Method == "PUT") {
		var env struct {
			Data map[string]any `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
			f.fail(w, 400, "bad json")
			return
		}
		body = env.Data
		f.bodies = append(f.bodies, body)
	}
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		f.fail(w, 401, "Not Authorized")
		return
	}
	if f.limited > 0 || f.limitAt > 0 && len(f.reqs) == f.limitAt {
		if f.limited > 0 {
			f.limited--
		}
		w.Header().Set("Retry-After", "7")
		f.fail(w, 429, "You have made too many requests recently.")
		return
	}
	if f.limitOn != "" && f.limitOn == r.Method+" "+r.URL.Path {
		f.limitOn = ""
		w.Header().Set("Retry-After", "7")
		f.fail(w, 429, "You have made too many requests recently.")
		return
	}
	if f.failOn != "" && f.failOn == r.Method+" "+r.URL.Path {
		f.fail(w, 500, "Server Error")
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/1.0/") {
		f.fail(w, 404, "no route")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/1.0/"), "/")
	q := r.URL.Query()
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/1.0/users/me":
		f.write(w, 200, map[string]any{"data": map[string]any{"gid": "1", "name": "tester", "email": "tester@acme.test",
			"workspaces": []any{map[string]any{"gid": wsGID, "name": "acme"}}}})
	case r.Method == "GET" && len(parts) == 2 && parts[0] == "projects":
		if parts[1] != projGID {
			f.fail(w, 404, "project: Unknown object")
			return
		}
		f.write(w, 200, map[string]any{"data": map[string]any{"gid": projGID, "name": "Platform"}})
	case r.Method == "GET" && len(parts) == 3 && parts[0] == "projects" && parts[2] == "sections":
		if parts[1] != projGID {
			f.fail(w, 404, "project: Unknown object")
			return
		}
		items := []any{}
		for _, s := range f.sections {
			items = append(items, map[string]any{"gid": s[0], "name": s[1]})
		}
		f.page(w, r, items)
	case r.Method == "GET" && r.URL.Path == "/api/1.0/tasks":
		var since time.Time
		if s := q.Get("modified_since"); s != "" {
			var err error
			if since, err = time.Parse(time.RFC3339, s); err != nil {
				f.fail(w, 400, "modified_since: bad time")
				return
			}
		}
		var doneSince time.Time
		if s := q.Get("completed_since"); s != "" {
			var err error
			if doneSince, err = time.Parse(time.RFC3339, s); err != nil {
				f.fail(w, 400, "completed_since: bad time")
				return
			}
		}
		// the fake has no completion time: a completed task counts as completed when last modified
		mod := func(tk *fakeTask) bool {
			return (since.IsZero() || !tk.Modified.Before(since)) &&
				(!tk.Completed || q.Get("completed_since") == "" || !tk.Modified.Before(doneSince))
		}
		switch {
		case q.Get("assignee") == "me" && q.Get("workspace") == wsGID:
			f.page(w, r, f.sortedTasks(func(tk *fakeTask) bool { return tk.Assignee == "me" && mod(tk) }))
		case q.Get("project") == projGID:
			f.page(w, r, f.sortedTasks(func(tk *fakeTask) bool { return tk.Section != "" && mod(tk) }))
		default:
			f.fail(w, 400, "Must specify exactly one of project, tag, section, user task list, or assignee + workspace")
		}
	case r.Method == "GET" && len(parts) == 2 && parts[0] == "tasks":
		tk, ok := f.tasks[parts[1]]
		if !ok {
			f.fail(w, 404, "task: Unknown object: "+parts[1])
			return
		}
		f.write(w, 200, map[string]any{"data": f.taskJSON(tk)})
	case r.Method == "GET" && len(parts) == 3 && parts[0] == "tasks" && parts[2] == "subtasks":
		f.page(w, r, f.sortedTasks(func(tk *fakeTask) bool { return tk.Parent == parts[1] }))
	case r.Method == "GET" && len(parts) == 3 && parts[0] == "tasks" && parts[2] == "stories":
		items := []any{}
		for _, s := range f.stories {
			if s.Task == parts[1] {
				items = append(items, map[string]any{"gid": s.GID, "resource_subtype": s.Subtype, "text": s.Text,
					"created_at": s.Created.Format(time.RFC3339), "created_by": map[string]any{"gid": "1", "name": s.Author}})
			}
		}
		f.page(w, r, items)
	case r.Method == "GET" && r.URL.Path == "/api/1.0/attachments":
		items := []any{}
		for i, a := range f.attach[q.Get("parent")] {
			items = append(items, map[string]any{"gid": fmt.Sprint(600 + i), "name": a[0], "permanent_url": a[1],
				"download_url": "https://s3.example.test/signed?X-Amz-Signature=abc"})
		}
		f.page(w, r, items)
	case r.Method == "POST" && len(parts) == 3 && parts[0] == "sections" && parts[2] == "addTask":
		tk, ok := f.tasks[fmt.Sprint(body["task"])]
		if !ok || f.section(parts[1]) == "" {
			f.fail(w, 404, "Unknown object")
			return
		}
		tk.Section = parts[1]
		f.write(w, 200, map[string]any{"data": map[string]any{}})
	case r.Method == "POST" && len(parts) == 3 && parts[0] == "tasks" && parts[2] == "stories":
		if _, ok := f.tasks[parts[1]]; !ok {
			f.fail(w, 404, "Unknown object")
			return
		}
		f.next++
		st := fakeStory{GID: strconv.Itoa(f.next), Task: parts[1], Subtype: "comment_added", HTML: fmt.Sprint(body["html_text"]), Author: "tester", Created: time.Now()}
		f.stories = append(f.stories, st)
		f.write(w, 201, map[string]any{"data": map[string]any{"gid": st.GID}})
	case r.Method == "POST" && (r.URL.Path == "/api/1.0/tasks" || len(parts) == 3 && parts[0] == "tasks" && parts[2] == "subtasks"):
		f.next++
		tk := &fakeTask{GID: strconv.Itoa(f.next), Name: fmt.Sprint(body["name"]), Notes: fmt.Sprint(body["notes"]), Modified: time.Now().UTC()}
		if body["assignee"] == "me" {
			tk.Assignee = "me"
		}
		tk.Completed = body["completed"] == true
		if len(parts) == 3 {
			if _, ok := f.tasks[parts[1]]; !ok {
				f.fail(w, 404, "Unknown object")
				return
			}
			tk.Parent = parts[1]
		} else {
			if ms, ok := body["memberships"].([]any); ok && len(ms) > 0 {
				m := ms[0].(map[string]any)
				tk.Section = fmt.Sprint(m["section"])
			} else if ps, ok := body["projects"].([]any); ok && len(ps) > 0 {
				tk.Section = f.sections[0][0]
			}
		}
		f.tasks[tk.GID] = tk
		f.write(w, 201, map[string]any{"data": f.taskJSON(tk)})
	case r.Method == "PUT" && len(parts) == 2 && parts[0] == "tasks":
		tk, ok := f.tasks[parts[1]]
		if !ok {
			f.fail(w, 404, "Unknown object")
			return
		}
		if v, ok := body["name"]; ok {
			tk.Name = fmt.Sprint(v)
		}
		if v, ok := body["notes"]; ok {
			tk.Notes = fmt.Sprint(v)
		}
		if v, ok := body["completed"].(bool); ok && v != tk.Completed {
			tk.Completed = v
			tk.Modified = time.Now().UTC()
		}
		f.write(w, 200, map[string]any{"data": f.taskJSON(tk)})
	case r.Method == "GET" && r.URL.Path == "/api/1.0/workspaces/"+wsGID+"/typeahead":
		// Asana's typeahead: up to count (default 20, max 100) objects whose name contains query,
		// in no particular order. The fake answers in gid order, prefix matches only.
		if q.Get("resource_type") != "tag" {
			f.fail(w, 400, "resource_type")
			return
		}
		f.typeaheads = append(f.typeaheads, q.Get("query"))
		ids := make([]string, 0, len(f.tags))
		for id := range f.tags {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		items := []any{}
		for _, id := range ids {
			if strings.Contains(strings.ToLower(f.tags[id]), strings.ToLower(q.Get("query"))) {
				items = append(items, map[string]any{"gid": id, "name": f.tags[id]})
			}
		}
		f.write(w, 200, map[string]any{"data": items})
	case r.Method == "POST" && r.URL.Path == "/api/1.0/tags":
		if body["workspace"] != wsGID {
			f.fail(w, 400, "workspace required")
			return
		}
		f.next++
		id := strconv.Itoa(f.next)
		f.tags[id] = fmt.Sprint(body["name"])
		f.write(w, 201, map[string]any{"data": map[string]any{"gid": id, "name": f.tags[id]}})
	case r.Method == "POST" && len(parts) == 3 && parts[0] == "tasks" && (parts[2] == "addTag" || parts[2] == "removeTag"):
		tk, ok := f.tasks[parts[1]]
		tag := fmt.Sprint(body["tag"])
		if !ok || f.tags[tag] == "" {
			f.fail(w, 404, "Unknown object")
			return
		}
		var keep []string
		for _, g := range tk.Tags {
			if g != tag {
				keep = append(keep, g)
			}
		}
		if parts[2] == "addTag" {
			keep = append(keep, tag)
		}
		tk.Tags = keep
		f.write(w, 200, map[string]any{"data": map[string]any{}})
	default:
		f.fail(w, 404, "no route "+r.Method+" "+r.URL.Path)
	}
}

func (f *fakeAsana) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reqs...)
}

func (f *fakeAsana) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs, f.queries, f.bodies, f.typeaheads = nil, nil, nil, nil
}
