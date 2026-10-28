package setupapi

import (
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

// Tracker is the plain view of the manifest's tracker block (spec B §6): the company board the
// hub mirrors. Names only, never a token.
type Tracker struct {
	Provider   string            // github | azure-devops | jira | linear | asana
	Scope      map[string]string // org, project, board, plus any provider-specific keys
	AssignedTo string            // me | anyone; "" = me
	Auth       string            // cli | keychain | env | mcp; "" = cli
	Env        string            // the variable tq dotenv resolves, for auth env
	Lanes      map[string]string // tracker status -> hub lane overrides
}

// Services lists the accepted names for the Services step, straight from the manifest's own
// lists so the wizard and the loader can never disagree.
type Services struct {
	Trackers     []string
	TrackerAuths []string
	Assignees    []string
	Chat         []string
	Email        []string
}

// ServiceCatalog returns the accepted tracker, auth, chat and email names.
func ServiceCatalog() Services {
	cp := func(xs []string) []string { return append([]string(nil), xs...) }
	return Services{
		Trackers:     cp(manifest.TrackerProviders),
		TrackerAuths: cp(manifest.TrackerAuths),
		Assignees:    cp(manifest.TrackerAssignees),
		Chat:         cp(manifest.ChatProviders),
		Email:        cp(manifest.EmailProviders),
	}
}

// toInternal converts to the manifest block. A nil tracker or one with no provider is skipped;
// empty scope values and blank lane rows are dropped (the form sends every field it shows).
func (t *Tracker) toInternal() *manifest.Tracker {
	if t == nil || strings.TrimSpace(t.Provider) == "" {
		return nil
	}
	out := &manifest.Tracker{
		Provider:   strings.TrimSpace(t.Provider),
		AssignedTo: strings.TrimSpace(t.AssignedTo),
		Auth:       strings.TrimSpace(t.Auth),
		Env:        strings.TrimSpace(t.Env),
	}
	for k, v := range t.Scope {
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		if out.Scope == nil {
			out.Scope = map[string]string{}
		}
		out.Scope[k] = v
	}
	for status, lane := range t.Lanes {
		status, lane = strings.TrimSpace(status), strings.TrimSpace(lane)
		if status == "" && lane == "" {
			continue
		}
		if out.Lanes == nil {
			out.Lanes = map[string]string{}
		}
		out.Lanes[status] = lane
	}
	if out.Auth != "env" {
		out.Env = ""
	}
	return out
}

// serviceBlock turns a Chat/Email provider name into its block; "" skips it, "none" is kept
// (the company has no such tool, said explicitly).
func serviceBlock(provider string) *manifest.Service {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil
	}
	return &manifest.Service{Provider: provider}
}
