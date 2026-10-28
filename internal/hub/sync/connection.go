package sync

import (
	"context"
	"encoding/json"
	"errors"
	gosync "sync"

	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// settingConnection holds the last Check: the account the tracker named and the credential source
// that worked. Names only; a token never reaches it.
const settingConnection = "sync.connection"

// Connection is the last auth check as the Connections page shows it (spec B §10).
type Connection struct {
	Provider  string `json:"provider"`
	Source    string `json:"source,omitempty"`  // the credential source that worked: cli, keychain, env, mcp; "" = not known
	Account   string `json:"account,omitempty"` // what Adapter.Check returned
	CheckedAt string `json:"checked_at,omitempty"`
	Error     string `json:"error,omitempty"` // masked; says which source failed, never the token
}

// sourceSeen is the credential source the default adapter resolved last (this process).
type sourceSeen struct {
	mu  gosync.Mutex
	src cred.Source
}

func (s *sourceSeen) set(v cred.Source) {
	s.mu.Lock()
	s.src = v
	s.mu.Unlock()
}

func (s *sourceSeen) get() cred.Source {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.src
}

// Check verifies auth: it builds the adapter (the credential chain) and calls Adapter.Check, then
// records the result for Connection. It calls the tracker once and never writes to it.
func (e *Engine) Check(ctx context.Context) (Connection, error) {
	c := Connection{Provider: e.provider, CheckedAt: store.Now()}
	e.source.set("")
	ad, err := e.cfg.Adapter(ctx)
	if err == nil {
		c.Source = string(e.source.get())
		c.Account, err = ad.Check(ctx)
	}
	var se *cred.SourceError
	switch {
	case errors.Is(err, cred.ErrNeedsSession):
		c.Source = string(cred.SourceMCP)
	case errors.As(err, &se) && c.Source == "":
		c.Source = string(se.Source)
	}
	if err != nil {
		c.Account = ""
		c.Error = e.st.Mask(err.Error())
	}
	if b, merr := json.Marshal(c); merr == nil {
		if e.st.SetSetting(settingConnection, string(b)) == nil {
			e.st.Publish(store.Change{Kind: "sync"})
		}
	}
	return c, err
}

// Connection is the last recorded Check; ok is false when none ran in this workspace.
func (e *Engine) Connection() (Connection, bool) {
	v, ok, err := e.st.Setting(settingConnection)
	if err != nil || !ok {
		return Connection{Provider: e.provider}, false
	}
	var c Connection
	if json.Unmarshal([]byte(v), &c) != nil || c.Provider != e.provider {
		return Connection{Provider: e.provider}, false
	}
	return c, true
}
