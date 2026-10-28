package web

import (
	"context"
	"net/http"
	"net/url"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// UpdateCard edits a card over POST /api/cards/<id>; the hub applies the runner's rules (an agent
// may change only the tags, and only on a card in a planning lane).
func (c *Client) UpdateCard(ctx context.Context, id string, u store.CardUpdate) (store.Card, error) {
	var out store.Card
	err := c.do(ctx, http.MethodPost, "/api/cards/"+url.PathEscape(id), nil,
		updateRequest{Title: u.Title, Body: u.Body, ProjectDir: u.ProjectDir, Tags: u.Tags}, &out)
	return out, err
}
