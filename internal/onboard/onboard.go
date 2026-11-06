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
	Name        string `yaml:"name"`
	Base        string `yaml:"base,omitempty"`
	DisplayName string `yaml:"display_name,omitempty"`
	Color       string `yaml:"color,omitempty"`
	Git         Git    `yaml:"git"`
	// Identities overrides the identities derived from the other answers.
	Identities   []string          `yaml:"identities,omitempty"`
	Cloud        *Cloud            `yaml:"cloud,omitempty"`
	DataPlatform *manifest.Service `yaml:"data_platform,omitempty"`
	Agents       []string          `yaml:"agents,omitempty"`
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

// retiredKeys are top-level answer keys an older tq wrote and this one no
// longer reads (permission_mode: PRD v2 §13, permission modes of any kind are
// a non-goal). An answer file that still carries one parses; the key is
// dropped before the strict decode and never read.
var retiredKeys = []string{"permission_mode"}

// dropRetiredKeys returns raw without the top-level retiredKeys, or raw as
// it is when it holds none (or does not parse; the strict decode reports it).
func dropRetiredKeys(raw []byte) []byte {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return raw
	}
	top := doc.Content[0]
	if top.Kind != yaml.MappingNode {
		return raw
	}
	kept := top.Content[:0:0]
	dropped := false
	for i := 0; i+1 < len(top.Content); i += 2 {
		if slices.Contains(retiredKeys, top.Content[i].Value) {
			dropped = true
			continue
		}
		kept = append(kept, top.Content[i], top.Content[i+1])
	}
	if !dropped {
		return raw
	}
	top.Content = kept
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return raw
	}
	return out
}

// Parse reads answers from YAML.
func Parse(raw []byte) (*Answers, error) {
	if manifest.LooksLikeSecret(string(raw)) {
		return nil, manifest.ErrSecretLike
	}
	var a Answers
	dec := yaml.NewDecoder(bytes.NewReader(dropRetiredKeys(raw)))
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
		Git:  manifest.Git{Name: a.Git.Name, Email: a.Git.Email, Provider: a.Git.Provider, User: a.Git.User},
		Chat: a.Chat, Email: a.Email, DataPlatform: a.DataPlatform, Agents: a.Agents,
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

// identities derives the identity list: the agents, the git host's CLI, the clouds and the data
// platform, each only when the catalog can isolate it. Explicit identities win; no agent answer
// (or nothing isolatable at all) means claude.
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
		Identities: a.identities(),
		Chat:       a.Chat, Email: a.Email, DataPlatform: a.DataPlatform,
		Agents: slices.Clone(a.Agents), Cloud: a.cloudBlock(),
	}, nil
}

// NextSteps are the lines tq prints after writing the manifest, in order: trust, CLI logins, the
// MCP connectors, then the Claude-side skill. They are commands for the
// human to run; tq runs none of them.
func NextSteps(ws string, m *manifest.Manifest) []string {
	cat := providers.MustLoad()
	out := []string{"Trust the manifest (a human step): tq allow " + ws}
	ids := m.IdentityNames()
	covered := map[string]string{} // env var -> the identity whose login sets it up
	loginOf := map[string]string{} // identity -> the identity whose printed login line covers it
	for _, id := range ids {
		p, ok := cat.Get(id)
		if !ok || !p.HasIdentity() || (p.Login == nil && p.CLI == nil) {
			continue
		}
		// One CLI under two ids (az and azure-devops) shares one config home: one login covers both.
		if owner, dup := sharedLogin(p, covered); dup {
			loginOf[id] = owner
			out = append(out, fmt.Sprintf("  (%s shares the %s login: one sign-in covers both)", id, owner))
			continue
		}
		for k := range p.Identity.Env {
			covered[k] = id
		}
		loginOf[id] = id
		out = append(out, "Log in: tq login "+ws+" "+id)
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
