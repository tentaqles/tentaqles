package jira

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
)

// issue is a Jira issue as REST v3 (and acli --json) returns it.
type issue struct {
	ID             string                     `json:"id"`
	Key            string                     `json:"key"`
	Fields         json.RawMessage            `json:"fields"`
	RenderedFields map[string]json.RawMessage `json:"renderedFields"`
}

type issueFields struct {
	Summary  string      `json:"summary"`
	Status   *jiraStatus `json:"status"`
	Assignee *user       `json:"assignee"`
	Parent   *struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	} `json:"parent"`
	Labels      []string        `json:"labels"`
	Updated     string          `json:"updated"`
	Description json.RawMessage `json:"description"`
	Comment     *struct {
		Comments []struct {
			ID      string          `json:"id"`
			Author  *user           `json:"author"`
			Body    json.RawMessage `json:"body"`
			Created string          `json:"created"`
		} `json:"comments"`
	} `json:"comment"`
	Attachment []struct {
		Filename string `json:"filename"`
		Content  string `json:"content"`
	} `json:"attachment"`
}

func parseTime(s string) time.Time {
	for _, f := range []string{jiraTimeForm, time.RFC3339Nano, "2006-01-02T15:04:05-0700"} {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func renderedText(rf map[string]json.RawMessage, k string) string {
	var s string
	if raw, ok := rf[k]; ok && json.Unmarshal(raw, &s) == nil {
		return htmlText(s)
	}
	return ""
}

// item maps an issue. The item id is the issue key: REST and acli both take it, and Jira still
// resolves a key after the issue moves project.
func (a *Adapter) item(is issue) hubsync.Item {
	var f issueFields
	_ = json.Unmarshal(is.Fields, &f)
	key := is.Key
	if key == "" {
		key = is.ID
	}
	it := hubsync.Item{ID: key, Key: key, URL: a.base + "/browse/" + key, Title: f.Summary,
		Assignee: f.Assignee.name(), Rev: f.Updated, UpdatedAt: parseTime(f.Updated), Labels: f.Labels}
	if it.Labels == nil {
		it.Labels = []string{}
	}
	if f.Status != nil {
		it.Status = f.Status.Name
		it.StatusCategory = Category(f.Status.StatusCategory.Key)
	}
	if f.Parent != nil {
		it.ParentID = f.Parent.Key
		if it.ParentID == "" {
			it.ParentID = f.Parent.ID
		}
	}
	it.Body = adfToMarkdown(f.Description)
	if it.Body == "" {
		it.Body = renderedText(is.RenderedFields, "description")
	}
	if a.acceptanceField != "" {
		var all map[string]json.RawMessage
		if json.Unmarshal(is.Fields, &all) == nil {
			it.Acceptance = adfToMarkdown(all[a.acceptanceField])
		}
		if it.Acceptance == "" {
			it.Acceptance = renderedText(is.RenderedFields, a.acceptanceField)
		}
	}
	if f.Comment != nil {
		cs := f.Comment.Comments
		for i := len(cs) - 1; i >= 0; i-- { // Jira returns comments oldest first
			c := cs[i]
			it.Comments = append(it.Comments, hubsync.Comment{ID: c.ID, Author: c.Author.name(),
				Body: adfToMarkdown(c.Body), CreatedAt: parseTime(c.Created)})
		}
		slices.SortStableFunc(it.Comments, func(x, y hubsync.Comment) int { return y.CreatedAt.Compare(x.CreatedAt) })
		if len(it.Comments) > hubsync.MaxComments {
			it.Comments = it.Comments[:hubsync.MaxComments]
		}
	}
	for _, at := range f.Attachment {
		if strings.TrimSpace(at.Filename) != "" {
			it.Attachments = append(it.Attachments, hubsync.Attachment{Name: at.Filename, URL: at.Content})
		}
	}
	return it
}
