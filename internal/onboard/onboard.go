// Package onboard is the data side of `tq onboard` (spec B §8): the answers the wizard (or a
// --from YAML file) collects, their validation, the workspace.AddOptions they become, and the
// next-step lines tq prints for the human. The interactive form lives in internal/cmd.
//
// Like the manifest it writes, an answer set holds names only, never secrets: tokens go to the OS
// keychain (`tq secret set`), a CLI's own login, or an env var tq dotenv resolves.
package onboard

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/workspace"
	"gopkg.in/yaml.v3"
)

// GitProviders are the git hosts onboarding offers (git.provider).
var GitProviders = []string{"github", "gitlab", "azure-devops", "bitbucket"}

// Git is the git identity of the workspace.
type Git struct {
	Name     string `yaml:"name,omitempty"`
	Email    string `yaml:"email"`
	Provider string `yaml:"provider,omitempty"`
	User     string `yaml:"user,omitempty"`
}

// Cloud is the cloud block: one primary provider plus the also list.
type Cloud struct {
	Provider string   `yaml:"provider,omitempty"`
	Also     []string `yaml:"also,omitempty"`
}

// Answers is one onboarding run. Every block but name and git.email is optional: a nil block is a
// skipped question, and it stays out of the manifest.
type Answers struct {
	Name           string `yaml:"name"`
	Base           string `yaml:"base,omitempty"`
	DisplayName    string `yaml:"display_name,omitempty"`
	Color          string `yaml:"color,omitempty"`
	PermissionMode string `yaml:"permission_mode,omitempty"`
	Git            Git    `yaml:"git"`
	// Identities overrides the identities derived from the other answers.
	Identities   []string          `yaml:"identities,omitempty"`
	Cloud        *Cloud            `yaml:"cloud,omitempty"`
	DataPlatform *manifest.Service `yaml:"data_platform,omitempty"`
	Agents       []string          `yaml:"agents,omitempty"`
	Tracker      *manifest.Tracker `yaml:"tracker,omitempty"`
	Chat         *manifest.Service `yaml:"chat,omitempty"`
	Email        *manifest.Service `yaml:"email,omitempty"`
}

// Load reads an answer file. Unknown keys are refused (a typo would silently skip a block), and
// so is a secret-shaped value, as in a manifest.
func Load(path string) (*Answers, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// Parse reads answers from YAML.
func Parse(raw []byte) (*Answers, error) {
	if manifest.LooksLikeSecret(string(raw)) {
		return nil, manifest.ErrSecretLike
	}
	var a Answers
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&a); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("onboard answers: %w", err)
	}
	a.normalize()
	return &a, nil
}

func trimAll(in []string) []string {
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (a *Answers) normalize() {
	a.Name = strings.TrimSpace(a.Name)
	a.Git.Email = strings.TrimSpace(a.Git.Email)
	a.Git.Name = strings.TrimSpace(a.Git.Name)
	a.Git.Provider = strings.TrimSpace(a.Git.Provider)
	a.Git.User = strings.TrimSpace(a.Git.User)
	a.Agents = trimAll(a.Agents)
	a.Identities = trimAll(a.Identities)
	if a.Cloud != nil {
		a.Cloud.Provider = manifest.CloudProviderName(a.Cloud.Provider)
		a.Cloud.Also = trimAll(a.Cloud.Also)
		for i, s := range a.Cloud.Also {
			a.Cloud.Also[i] = manifest.CloudProviderName(s)
		}
		if a.Cloud.Provider == "" && len(a.Cloud.Also) == 0 {
			a.Cloud = nil
		}
	}
	for _, s := range []**manifest.Service{&a.DataPlatform, &a.Chat, &a.Email} {
		if *s != nil {
			(*s).Provider = strings.TrimSpace((*s).Provider)
			if (*s).Provider == "" {
				*s = nil
			}
		}
	}
	if a.Tracker != nil && strings.TrimSpace(a.Tracker.Provider) == "" && len(a.Tracker.Scope) == 0 {
		a.Tracker = nil
	}
	if a.Tracker != nil {
		for k, v := range a.Tracker.Scope {
			if strings.TrimSpace(v) == "" {
				delete(a.Tracker.Scope, k)
			}
		}
		if len(a.Tracker.Scope) == 0 {
			a.Tracker.Scope = nil
		}
	}
}

// Validate checks the answers against the same rules the manifest loader applies, before
// anything is written.
func (a *Answers) Validate() error {
	if a.Name == "" {
		return fmt.Errorf("name is required")
	}
	if !workspace.NameRe.MatchString(a.Name) {
		return fmt.Errorf("name %q must match %s", a.Name, workspace.NameRe)
	}
	if a.Git.Email == "" || !strings.Contains(a.Git.Email, "@") {
		return fmt.Errorf("git.email: a valid email is required")
	}
	if a.Git.Provider != "" && !slices.Contains(GitProviders, a.Git.Provider) {
		return fmt.Errorf("git.provider: unknown git host %q (known: %s)", a.Git.Provider, strings.Join(GitProviders, ", "))
	}
	if !slices.Contains(manifest.PermissionModes, a.PermissionMode) {
		return fmt.Errorf("permission_mode %q: use default, acceptEdits, plan, auto, dontAsk or bypass", a.PermissionMode)
	}
	if err := a.validateCloud(); err != nil {
		return err
	}
	known := manifest.KnownIdentities()
	for _, id := range a.Identities {
		if !slices.Contains(known, id) {
			return fmt.Errorf("identities: unknown identity %q", id)
		}
	}
	m := a.Manifest()
	return m.ValidateServices()
}

func (a *Answers) validateCloud() error {
	if a.Cloud == nil {
		return nil
	}
	p := a.Cloud.Provider
	if p != "" && p != "none" && !slices.Contains(manifest.CloudProviders, p) {
		return fmt.Errorf("cloud.provider: unknown provider %q (known: %s, none)", p, strings.Join(manifest.CloudProviders, ", "))
	}
	if len(a.Cloud.Also) > 0 && (p == "" || p == "none") {
		return fmt.Errorf("cloud.also: needs cloud.provider to name a primary cloud")
	}
	seen := map[string]bool{}
	for _, s := range a.Cloud.Also {
		if !slices.Contains(manifest.CloudProviders, s) {
			return fmt.Errorf("cloud.also: unknown provider %q (known: %s)", s, strings.Join(manifest.CloudProviders, ", "))
		}
		if s == p {
			return fmt.Errorf("cloud.also: %q is already the primary cloud.provider", s)
		}
		if seen[s] {
			return fmt.Errorf("cloud.also: %q is listed twice", s)
		}
		seen[s] = true
	}
	return nil
}

// Manifest is the manifest these answers describe (services, git, cloud, identities), for
// validation and next steps. workspace.Add writes the real one.
func (a *Answers) Manifest() *manifest.Manifest {
	m := &manifest.Manifest{
		Schema: "tentaqles-client-v2", Client: a.Name, DisplayName: a.DisplayName,
		Git:     manifest.Git{Name: a.Git.Name, Email: a.Git.Email, Provider: a.Git.Provider, User: a.Git.User},
		Tracker: a.Tracker, Chat: a.Chat, Email: a.Email, DataPlatform: a.DataPlatform, Agents: a.Agents,
		Cloud:      a.cloudBlock(),
		Identities: map[string]manifest.Identity{},
	}
	for _, id := range a.identities() {
		m.Identities[id] = manifest.Identity{}
	}
	return m
}

func (a *Answers) cloudBlock() map[string]any {
	if a.Cloud == nil {
		return nil
	}
	out := map[string]any{}
	if a.Cloud.Provider != "" {
		out["provider"] = a.Cloud.Provider
	}
	if len(a.Cloud.Also) > 0 {
		out["also"] = append([]string(nil), a.Cloud.Also...)
	}
	return out
}

// cloudCLI is the catalog id of each cloud's CLI.
var cloudCLI = map[string]string{"azure": "az", "aws": "aws", "gcp": "gcloud", "digitalocean": "doctl"}

// gitCLI is the catalog id of each git host's CLI.
var gitCLI = map[string]string{"github": "gh", "gitlab": "glab", "azure-devops": "azure-devops", "bitbucket": "bitbucket"}

// trackerCLI is the identity whose login a tracker's cli auth uses.
var trackerCLI = map[string]string{"github": "gh", "azure-devops": "azure-devops"}

// identities derives the identity list: the agents, the git host's CLI, the clouds, the data
// platform and the tracker's CLI, each only when the catalog can isolate it. Explicit identities
// win; no agent answer (or nothing isolatable at all) means claude.
func (a *Answers) identities() []string {
	if len(a.Identities) > 0 {
		out := slices.Clone(a.Identities)
		sort.Strings(out)
		return slices.Compact(out)
	}
	cat := providers.MustLoad()
	set := map[string]bool{}
	add := func(id string) {
		if p, ok := cat.Get(id); ok && p.HasIdentity() {
			set[id] = true
		}
	}
	if len(a.Agents) == 0 {
		add("claude")
	}
	for _, ag := range a.Agents {
		add(ag)
	}
	if a.Git.Provider == "" {
		add("gh")
	} else {
		add(gitCLI[a.Git.Provider])
	}
	if a.Cloud != nil {
		add(cloudCLI[a.Cloud.Provider])
		for _, c := range a.Cloud.Also {
			add(cloudCLI[c])
		}
	}
	if a.DataPlatform.Enabled() {
		add(a.DataPlatform.Provider)
	}
	if a.Tracker != nil && a.Tracker.AuthOrDefault() == "cli" {
		add(trackerCLI[a.Tracker.Provider])
	}
	if len(set) == 0 {
		// Nothing isolatable was named: claude keeps it a Claude Code workspace rather than
		// falling through to tq add's claude+gh default.
		set["claude"] = true
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// AddOptions is the workspace.Add call for these answers. Trust is always false: `tq allow` is
// the human's step. Base, RunGit and the git name default are the caller's.
func (a *Answers) AddOptions() (workspace.AddOptions, error) {
	if err := a.Validate(); err != nil {
		return workspace.AddOptions{}, err
	}
	return workspace.AddOptions{
		Base: a.Base, Name: a.Name, DisplayName: a.DisplayName, Color: a.Color,
		GitName: a.Git.Name, GitEmail: a.Git.Email, GitUser: a.Git.User, GitProvider: a.Git.Provider,
		Identities: a.identities(), PermissionMode: a.PermissionMode,
		Tracker: a.Tracker, Chat: a.Chat, Email: a.Email, DataPlatform: a.DataPlatform,
		Agents: slices.Clone(a.Agents), Cloud: a.cloudBlock(),
	}, nil
}

// SuggestEnvName is the default tracker.env name: <WS>_<PROVIDER>_TOKEN.
func SuggestEnvName(ws, provider string) string {
	up := strings.ToUpper(ws + "_" + provider + "_TOKEN")
	return strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, up)
}

// DefaultAuth is the auth source to suggest for a tracker: its CLI where tq knows one, else the
// keychain.
func DefaultAuth(provider string) string {
	switch provider {
	case "github", "azure-devops", "jira":
		return "cli"
	}
	return "keychain"
}

// NextSteps are the lines tq prints after writing the manifest, in order: trust, CLI logins, the
// tracker credential, the MCP connectors, then the Claude-side skill. They are commands for the
// human to run; tq runs none of them.
func NextSteps(ws string, m *manifest.Manifest) []string {
	cat := providers.MustLoad()
	out := []string{"Trust the manifest (a human step): tq allow " + ws}
	ids := m.IdentityNames()
	covered := map[string]string{} // env var -> the identity whose login sets it up
	for _, id := range ids {
		p, ok := cat.Get(id)
		if !ok || !p.HasIdentity() || (p.Login == nil && p.CLI == nil) {
			continue
		}
		// One CLI under two ids (az and azure-devops) shares one config home: one login covers both.
		if owner, dup := sharedLogin(p, covered); dup {
			out = append(out, fmt.Sprintf("  (%s shares the %s login: one sign-in covers both)", id, owner))
			continue
		}
		for k := range p.Identity.Env {
			covered[k] = id
		}
		out = append(out, "Log in: tq login "+ws+" "+id)
	}
	if t := m.Tracker; t != nil && t.Provider != "" {
		out = append(out, trackerSteps(ws, t, ids, cat)...)
	}
	for _, s := range []struct {
		kind string
		svc  *manifest.Service
	}{{"chat", m.Chat}, {"email", m.Email}} {
		if !s.svc.Enabled() {
			continue
		}
		name, mcp := s.svc.Provider, s.svc.Provider
		if p, ok := cat.Get(s.svc.Provider); ok {
			name, mcp = p.Name, p.MCPName()
		}
		out = append(out, fmt.Sprintf("Connect %s (%s, MCP only): add the %q MCP entry to the tq bundle catalog and to claude.bundle.mcp, run tq bundle sync %s, then authenticate it with /mcp in a Claude session", name, s.kind, mcp, ws))
	}
	out = append(out, "Then open Claude Code in the folder and run /tentaqles:onboard to verify every connection")
	return out
}

// sharedLogin reports whether every config-home variable of p is already set up by an earlier
// identity's login, and which one.
func sharedLogin(p providers.Provider, covered map[string]string) (string, bool) {
	owner := ""
	for k := range p.Identity.Env {
		o, ok := covered[k]
		if !ok {
			return "", false
		}
		owner = o
	}
	return owner, owner != ""
}

func trackerSteps(ws string, t *manifest.Tracker, ids []string, cat *providers.Catalog) []string {
	p, _ := cat.Get(t.Provider)
	label := t.Provider
	if p.Name != "" {
		label = p.Name
	}
	keychain := fmt.Sprintf("Tracker %s: store a token in the OS keychain with tq secret set %s %s (it reads the token from a hidden prompt)", label, ws, t.Provider)
	switch t.AuthOrDefault() {
	case "cli":
		if id, ok := trackerCLI[t.Provider]; ok {
			if slices.Contains(ids, id) {
				return []string{fmt.Sprintf("Tracker %s: uses the %s CLI login above (tq login %s %s)", label, id, ws, id)}
			}
			return []string{fmt.Sprintf("Tracker %s: log in with tq login %s %s", label, ws, id)}
		}
		if p.CLI != nil && p.Login != nil {
			line := fmt.Sprintf("Tracker %s: run %s %s yourself", label, p.CLI.Command, strings.Join(p.Login.Args, " "))
			if !p.HasIdentity() {
				line += " (it keeps one global login, not one per workspace)"
			}
			return []string{line, "  or, without the CLI: " + strings.TrimPrefix(keychain, "Tracker "+label+": ")}
		}
		return []string{fmt.Sprintf("Tracker %s has no CLI login; ", label) + strings.TrimPrefix(keychain, "Tracker "+label+": ")}
	case "keychain":
		return []string{keychain}
	case "env":
		return []string{fmt.Sprintf("Tracker %s: put the token in %s where tq dotenv resolves it (never in the manifest)", label, t.Env)}
	default: // mcp
		return []string{fmt.Sprintf("Tracker %s: MCP only, so no background sync; run /tentaqles:sync in a Claude session", label)}
	}
}
