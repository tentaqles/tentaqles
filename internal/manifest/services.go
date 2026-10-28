package manifest

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/providers"
)

// Company services (spec B §6): the tracker the board mirrors, the chat and email connectors the
// agent uses over MCP, the data platform and the agent CLIs this workspace logs into. Every block
// is optional, and like the rest of the manifest they hold names only, never secrets.

// TrackerProviders are the trackers board sync supports in release 1.
var TrackerProviders = []string{"github", "azure-devops", "jira", "linear", "asana"}

// ChatProviders and EmailProviders are reached through the agent's MCP connectors only.
var (
	ChatProviders  = []string{"slack", "teams", "none"}
	EmailProviders = []string{"gmail", "outlook", "none"}
)

// TrackerAuths are the credential sources; the fallback order is fixed (cli, keychain, env, mcp)
// and auth names the one to prefer.
var TrackerAuths = []string{"cli", "keychain", "env", "mcp"}

// TrackerAssignees: "me" mirrors items assigned to the logged-in account (plus children of
// mirrored cards); "anyone" mirrors the whole scope.
var TrackerAssignees = []string{"me", "anyone"}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Tracker is the company board to mirror.
type Tracker struct {
	Provider string `yaml:"provider"`
	// Scope is what to mirror, with provider-specific keys: org (ado org / jira site / github
	// owner / asana workspace / linear team), project, board (ado team, github project number,
	// asana project). Unknown keys are kept and passed to the adapter.
	Scope      map[string]string `yaml:"scope,omitempty"`
	AssignedTo string            `yaml:"assigned_to,omitempty"` // me | anyone; "" = me
	Auth       string            `yaml:"auth,omitempty"`        // cli | keychain | env | mcp; "" = cli
	Env        string            `yaml:"env,omitempty"`         // the variable tq dotenv resolves (auth env)
	// Lanes overrides the default status-category mapping: tracker status -> hub lane.
	Lanes map[string]string `yaml:"lanes,omitempty"`
}

func (t *Tracker) scope(k string) string {
	if t == nil {
		return ""
	}
	return t.Scope[k]
}

func (t *Tracker) Org() string     { return t.scope("org") }
func (t *Tracker) Project() string { return t.scope("project") }
func (t *Tracker) Board() string   { return t.scope("board") }

// AuthOrDefault is Auth, or "cli" when unset.
func (t *Tracker) AuthOrDefault() string {
	if t == nil || t.Auth == "" {
		return "cli"
	}
	return t.Auth
}

// AssignedToOrDefault is AssignedTo, or "me" when unset.
func (t *Tracker) AssignedToOrDefault() string {
	if t == nil || t.AssignedTo == "" {
		return "me"
	}
	return t.AssignedTo
}

// ScopeKey is the stable text form of Scope ("k=v;k=v", keys sorted): the scope column of the
// hub's sync_state and lane_map rows.
func (t *Tracker) ScopeKey() string {
	if t == nil {
		return ""
	}
	keys := make([]string, 0, len(t.Scope))
	for k := range t.Scope {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + t.Scope[k]
	}
	return strings.Join(parts, ";")
}

// Validate checks names against the supported lists. Values are names only; the whole file is
// already refused when anything in it looks like a secret.
func (t *Tracker) Validate() error {
	if t == nil {
		return nil
	}
	if t.Provider == "" {
		return fmt.Errorf("tracker.provider is required (%s)", strings.Join(TrackerProviders, ", "))
	}
	if !contains(TrackerProviders, t.Provider) {
		return fmt.Errorf("tracker.provider: unknown tracker %q (known: %s)", t.Provider, strings.Join(TrackerProviders, ", "))
	}
	for k, v := range t.Scope {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("tracker.scope: a key is empty")
		}
		if hasControl(k) || hasControl(v) {
			return fmt.Errorf("tracker.scope.%s: control characters are not allowed", k)
		}
	}
	if t.AssignedTo != "" && !contains(TrackerAssignees, t.AssignedTo) {
		return fmt.Errorf("tracker.assigned_to: use me or anyone, got %q", t.AssignedTo)
	}
	if t.Auth != "" && !contains(TrackerAuths, t.Auth) {
		return fmt.Errorf("tracker.auth: use cli, keychain, env or mcp, got %q", t.Auth)
	}
	if t.Env != "" && !envNameRe.MatchString(t.Env) {
		return fmt.Errorf("tracker.env: %q is not an environment variable name", t.Env)
	}
	if t.Auth == "env" && t.Env == "" {
		return fmt.Errorf("tracker.env is required with auth: env (the variable name tq dotenv resolves)")
	}
	for status, lane := range t.Lanes {
		if strings.TrimSpace(status) == "" || strings.TrimSpace(lane) == "" {
			return fmt.Errorf("tracker.lanes: %q -> %q: both the status and the lane are required", status, lane)
		}
		if hasControl(status) || hasControl(lane) {
			return fmt.Errorf("tracker.lanes: control characters are not allowed")
		}
	}
	return nil
}

// Service is a one-provider block (chat, email, data_platform).
type Service struct {
	Provider string `yaml:"provider"`
}

// Enabled is whether the block names a provider other than none.
func (s *Service) Enabled() bool { return s != nil && s.Provider != "" && s.Provider != "none" }

func (s *Service) validate(field string, known []string) error {
	if s == nil {
		return nil
	}
	if s.Provider == "" {
		return fmt.Errorf("%s.provider is required (%s)", field, strings.Join(known, ", "))
	}
	if !contains(known, s.Provider) {
		return fmt.Errorf("%s.provider: unknown provider %q (known: %s)", field, s.Provider, strings.Join(known, ", "))
	}
	return nil
}

// HasTracker is whether the manifest names a tracker (sync.enabled defaults to it).
func (m *Manifest) HasTracker() bool { return m != nil && m.Tracker != nil && m.Tracker.Provider != "" }

// catalogIDs lists the catalog providers of one category.
func catalogIDs(cat string) []string {
	var out []string
	for _, p := range providers.MustLoad().ByCategory(cat) {
		out = append(out, p.ID)
	}
	sort.Strings(out)
	return out
}

// DataPlatforms are the accepted data_platform providers: the catalog's data ids, plus none.
func DataPlatforms() []string { return append(catalogIDs("data"), "none") }

// AgentCLIs are the accepted agents entries: the catalog's agent ids.
func AgentCLIs() []string { return catalogIDs("agent") }

// ValidateServices checks the tracker, chat, email, data_platform and agents blocks (Load runs it;
// writers call it before writing).
func (m *Manifest) ValidateServices() error {
	if err := m.Tracker.Validate(); err != nil {
		return err
	}
	if err := m.Chat.validate("chat", ChatProviders); err != nil {
		return err
	}
	if err := m.Email.validate("email", EmailProviders); err != nil {
		return err
	}
	if m.DataPlatform != nil {
		if err := m.DataPlatform.validate("data_platform", DataPlatforms()); err != nil {
			return err
		}
	}
	if len(m.Agents) > 0 {
		known := AgentCLIs()
		seen := map[string]bool{}
		for _, a := range m.Agents {
			if !contains(known, a) {
				return fmt.Errorf("agents: unknown agent CLI %q (known: %s)", a, strings.Join(known, ", "))
			}
			if seen[a] {
				return fmt.Errorf("agents: %q is listed twice", a)
			}
			seen[a] = true
		}
	}
	return nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
