package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// clientTimeout bounds one API call; an answer waits on the gate (itself bounded to 60 s).
const clientTimeout = 90 * time.Second

// Client is runner.Ops over a running hub's API (so the page updates live).
type Client struct {
	Base, WS, LocalKey string
	Agent              bool
	HTTP               *http.Client
}

var _ runner.Ops = (*Client)(nil)

func NewClient(info serveinfo.Info, ws string, agent bool) *Client {
	return &Client{Base: serveinfo.BaseURL(info), WS: ws, LocalKey: info.LocalKey, Agent: agent, HTTP: &http.Client{Timeout: clientTimeout}}
}

type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string { return e.Message }

func (e *APIError) Unwrap() error {
	switch e.Code {
	case "human_only":
		return runner.ErrHumanOnly
	case "locked":
		return runner.ErrLocked
	case "not_found":
		return store.ErrNotFound
	case "unavailable":
		return ErrUnavailable
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, in, out any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("ws", c.WS)
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path+"?"+q.Encode(), body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(serveinfo.HeaderLocal, c.LocalKey)
	if c.Agent || runner.IsAgent(ctx) {
		req.Header.Set(HeaderAgent, "1")
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: clientTimeout}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("tq hub at %s: %w", c.Base, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode >= 400 {
		var e struct {
			Error   string          `json:"error"`
			Code    string          `json:"code"`
			Run     *store.Run      `json:"run"`
			Outcome json.RawMessage `json:"outcome"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Code == "run_active" && e.Run != nil {
			return &runner.RunActiveError{Run: *e.Run}
		}
		if len(e.Outcome) > 0 && out != nil {
			_ = json.Unmarshal(e.Outcome, out) // an answer the gate recorded before failing
		}
		if e.Error == "" {
			e.Error = fmt.Sprintf("tq hub answered %d: %s", resp.StatusCode, bytes.TrimSpace(raw))
		}
		return &APIError{Status: resp.StatusCode, Code: e.Code, Message: e.Error}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/api/ping", nil, nil, nil)
}

func (c *Client) Add(ctx context.Context, nc store.NewCard, budget *store.Budget) (store.Card, error) {
	var out store.Card
	err := c.do(ctx, http.MethodPost, "/api/cards", nil, addRequest{Card: nc, Budget: budget}, &out)
	return out, err
}

func (c *Client) List(ctx context.Context, f store.CardFilter, waitingOnly bool) ([]runner.CardView, error) {
	q := url.Values{}
	for k, v := range map[string]string{"lane": f.Lane, "kind": f.Kind, "tag": f.Tag} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if waitingOnly {
		q.Set("waiting", "1")
	}
	var out []runner.CardView
	err := c.do(ctx, http.MethodGet, "/api/cards", q, nil, &out)
	return out, err
}

func (c *Client) Show(ctx context.Context, id string) (runner.CardDetail, error) {
	var out runner.CardDetail
	err := c.do(ctx, http.MethodGet, "/api/cards/"+url.PathEscape(id), nil, nil, &out)
	return out, err
}

func (c *Client) Move(ctx context.Context, id, lane string, cancelRun bool) (runner.MoveOutcome, error) {
	var out runner.MoveOutcome
	err := c.do(ctx, http.MethodPost, "/api/cards/"+url.PathEscape(id)+"/move", nil, map[string]any{"lane": lane, "cancel_run": cancelRun}, &out)
	return out, err
}

func (c *Client) Run(ctx context.Context, id string) (store.Run, error) {
	var out store.Run
	err := c.do(ctx, http.MethodPost, "/api/cards/"+url.PathEscape(id)+"/run", nil, nil, &out)
	return out, err
}

func (c *Client) Answer(ctx context.Context, cardID, decisionID, choice, note string) (runner.AnswerOutcome, error) {
	var out runner.AnswerOutcome
	err := c.do(ctx, http.MethodPost, "/api/cards/"+url.PathEscape(cardID)+"/decisions/"+url.PathEscape(decisionID)+"/answer", nil,
		map[string]string{"choice": choice, "note": note}, &out)
	return out, err
}

func (c *Client) Cancel(ctx context.Context, runID string) (store.Run, error) {
	var out store.Run
	err := c.do(ctx, http.MethodPost, "/api/runs/"+url.PathEscape(runID)+"/cancel", nil, nil, &out)
	return out, err
}

func (c *Client) Comment(ctx context.Context, cardID, body string) (store.Comment, error) {
	var out store.Comment
	err := c.do(ctx, http.MethodPost, "/api/cards/"+url.PathEscape(cardID)+"/comments", nil, map[string]string{"body": body}, &out)
	return out, err
}

func (c *Client) Waiting(ctx context.Context) ([]runner.WaitingItem, error) {
	var out []runner.WaitingItem
	err := c.do(ctx, http.MethodGet, "/api/waiting", nil, nil, &out)
	return out, err
}
