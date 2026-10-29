// Package manifest parses .tentaqles.yaml (schemas v1 and v2).
package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/tentaqles/tentaqles/internal/decide"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/providers"
	"gopkg.in/yaml.v3"
)

const FileName = ".tentaqles.yaml"

var (
	ErrUnsupportedSchema = errors.New("unsupported manifest schema (want tentaqles-client-v1 or -v2)")
	ErrSecretLike        = errors.New("manifest contains a secret-looking value; manifests hold names only")
)

var (
	knownIdentitiesOnce sync.Once
	knownIdentities     []string
)

// KnownIdentities are the CLI providers tq can isolate, derived from the
// provider catalog (internal/providers). Keep in sync with envplan.Providers,
// which is built from the same catalog. It is loaded lazily and memoized so
// that importing this package does not read the user's home directory.
func KnownIdentities() []string {
	knownIdentitiesOnce.Do(func() {
		knownIdentities = providers.MustLoad().IDs()
	})
	return knownIdentities
}

var PermissionModes = []string{"", "default", "acceptEdits", "plan", "auto", "dontAsk", "bypass"}

type Git struct {
	Name         string `yaml:"name"`
	Email        string `yaml:"email"`
	Provider     string `yaml:"provider"`
	User         string `yaml:"user"`
	Host         string `yaml:"host"`
	ExpectedUser string `yaml:"expected_user"`
	// BlockedCommands are extra command prefixes the guard refuses for this
	// workspace, unioned with Manifest.BlockedCommands and cloud.blocked_commands.
	BlockedCommands []string `yaml:"blocked_commands"`
}

type Identity struct {
	ShareCapabilities *bool `yaml:"share_capabilities"`
	// ExpectedAccount is the account this identity must be logged in as --
	// an email for Claude, a username for gh. Declaring it is what lets
	// `tq doctor` tell a correctly wired workspace signed into the wrong
	// client from a correct one; leaving it empty means that identity is
	// never verified, and costs nothing.
	//
	// For a git-provider identity it falls back to Git.ExpectedUser, which
	// predates this field and is already enforced inside Claude sessions.
	ExpectedAccount      string `yaml:"expected_account,omitempty"`
	ExpectedSubscription string `yaml:"expected_subscription"`
	Profile              string `yaml:"profile"`
}

type Claude struct {
	PermissionMode string       `yaml:"permission_mode"`
	Bundle         *Bundle      `yaml:"bundle"`
	Permissions    *Permissions `yaml:"permissions"`
	// Env is merged into the identity's settings.json env by
	// `tq settings render`. Values are checked like the rest of the manifest:
	// a secret-shaped value makes the manifest fail to load.
	Env map[string]string `yaml:"env"`
}

// Permissions are extra settings.json permission rules for this workspace,
// added on top of tq's baseline by `tq settings render`.
type Permissions struct {
	Allow []string `yaml:"allow"`
	Deny  []string `yaml:"deny"`
	Ask   []string `yaml:"ask"`
}

// Bundle lists the catalog entries this workspace's Claude identity dir
// should have materialized (plugins, skills, user-scope MCP servers).
// Nil means tq does not manage bundles for this workspace.
type Bundle struct {
	Marketplaces []string `yaml:"marketplaces"`
	Plugins      []string `yaml:"plugins"`
	Skills       []string `yaml:"skills"`
	MCP          []string `yaml:"mcp"`
}

// Guard tunes the tool-call policy (internal/policy) for this workspace. The
// manifest is hash-pinned by `tq allow`, so it is the one layer trusted to
// switch built-in rules off; project files can only add rules.
type Guard struct {
	Disable []string `yaml:"disable"`
	// Enable switches a rule back on for this workspace when the user's
	// global guard file (~/.tentaqles/guard.yaml) turned it off.
	Enable []string `yaml:"enable,omitempty"`
	// Actions overrides a rule's action (ask|deny) for this workspace; it
	// wins over the global file.
	Actions map[string]policy.Action `yaml:"actions,omitempty"`
	Rules   []policy.Rule            `yaml:"rules"`
	// Unattended is the legacy unattended block, read as guard modes
	// (ModeView). Deprecated: use mode and projects. Nil: absent.
	Unattended *Unattended `yaml:"unattended,omitempty"`

	// Guard mode (spec E §7), see guardmode.go. Values are guardcfg names
	// (manifest does not import guardcfg).
	Mode          string                  `yaml:"mode,omitempty"`
	Base          string                  `yaml:"base,omitempty"`
	Categories    map[string]string       `yaml:"categories,omitempty"`
	RulesOverride map[string]string       `yaml:"rules_override,omitempty"`
	Projects      map[string]GuardProject `yaml:"projects,omitempty"`
	HeadlessMode  string                  `yaml:"headless_mode,omitempty"`
	Codex         *GuardCodex             `yaml:"codex,omitempty"`
}

type Manifest struct {
	Schema          string              `yaml:"schema"`
	Client          string              `yaml:"client"`
	DisplayName     string              `yaml:"display_name"`
	Language        string              `yaml:"language"`
	Color           string              `yaml:"color"`
	Git             Git                 `yaml:"git"`
	Identities      map[string]Identity `yaml:"identities"`
	Claude          Claude              `yaml:"claude"`
	BlockedCommands []string            `yaml:"blocked_commands"`
	Cloud           map[string]any      `yaml:"cloud"`
	Database        map[string]any      `yaml:"database"`
	Stack           []string            `yaml:"stack"`
	Guard           Guard               `yaml:"guard"`
	// Decision configures the Jev decision layer (internal/decide). Off
	// unless backend is set; shadow mode unless mode is enforce.
	Decision decide.Policy `yaml:"decision"`
	Data     Data          `yaml:"data"`
	// Company services (spec B §6); each block is optional. See services.go.
	Tracker      *Tracker `yaml:"tracker,omitempty"`
	Chat         *Service `yaml:"chat,omitempty"`
	Email        *Service `yaml:"email,omitempty"`
	DataPlatform *Service `yaml:"data_platform,omitempty"`
	Agents       []string `yaml:"agents,omitempty"`
	Path         string   `yaml:"-"`
}

var secretRe = regexp.MustCompile(`(?i)\b(ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9\-]{8,}|AKIA[0-9A-Z]{16}|xox[baprs]-[A-Za-z0-9\-]+|Bearer\s+\S+|eyJ[A-Za-z0-9_\-]{20,}\.[A-Za-z0-9_\-]{10,})`)

// LooksLikeSecret reports whether s contains a token-shaped value (API key,
// bearer token, JWT, etc). Exported for use by other packages (e.g. the
// bundle catalog) that need the same heuristic outside a manifest file.
func LooksLikeSecret(s string) bool {
	return secretRe.MatchString(s)
}

func Load(path string) (*Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if loc := secretRe.FindIndex(raw); loc != nil {
		return nil, fmt.Errorf("%w (at byte %d of %s)", ErrSecretLike, loc[0], path)
	}
	var m Manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m.Schema != "tentaqles-client-v1" && m.Schema != "tentaqles-client-v2" {
		return nil, fmt.Errorf("%w: got %q in %s", ErrUnsupportedSchema, m.Schema, path)
	}
	if m.Client == "" {
		return nil, fmt.Errorf("%s: 'client' is required", path)
	}
	for name := range m.Identities {
		known := KnownIdentities()
		if !contains(known, name) {
			return nil, fmt.Errorf("%s: unknown identity %q (known: %v)", path, name, known)
		}
	}
	if !contains(PermissionModes, m.Claude.PermissionMode) {
		return nil, fmt.Errorf("%s: claude.permission_mode must be one of default|acceptEdits|plan|auto|dontAsk|bypass", path)
	}
	for id, a := range m.Guard.Actions {
		if a != policy.Ask && a != policy.Deny {
			return nil, fmt.Errorf("%s: guard.actions[%s] must be ask or deny, got %q", path, id, a)
		}
	}
	if err := m.Decision.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := m.Data.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := m.Guard.Unattended.Validate(); err != nil {
		return nil, fmt.Errorf("%s: guard.unattended: %w", path, err)
	}
	if err := m.Guard.ValidateMode(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, r := range m.Guard.Rules {
		if r.Unattended != "" && r.Unattended != policy.UnattendedEnforce {
			return nil, fmt.Errorf("%s: guard.rules[%s].unattended must be %q or absent, got %q", path, r.ID, policy.UnattendedEnforce, r.Unattended)
		}
		if r.Category != "" && !contains(GuardCategories, r.Category) {
			return nil, fmt.Errorf("%s: guard.rules[%s].category must be one of %s, got %q", path, r.ID, strings.Join(GuardCategories, "|"), r.Category)
		}
	}
	if err := m.validateCloud(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := m.ValidateServices(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	m.Path = path
	return &m, nil
}

// CloudProviders are the canonical provider names cloud.provider and
// cloud.also accept (after CloudProviderName normalisation).
var CloudProviders = []string{"azure", "aws", "gcp", "digitalocean"}

// CloudProviderName normalises a cloud provider name: trimmed, lowercased,
// with the CLI-flavoured aliases mapped to the canonical names the guard
// compares against (gcloud -> gcp, az -> azure, do/doctl -> digitalocean).
// Unknown names come back lowercased and trimmed, otherwise unchanged.
func CloudProviderName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "gcloud":
		return "gcp"
	case "az":
		return "azure"
	case "do", "doctl":
		return "digitalocean"
	}
	return s
}

// cloudIdentity maps a canonical cloud provider to its catalog identity.
var cloudIdentity = map[string]string{"azure": "az", "aws": "aws", "gcp": "gcloud", "digitalocean": "doctl"}

// validateCloud checks the cloud block: cloud.provider is empty, "none" or a
// known provider (aliases allowed); cloud.also is a list of known providers
// with no repeats, none equal to the primary, and needs a real primary.
func (m *Manifest) validateCloud() error {
	primary := ""
	if raw, present := m.Cloud["provider"]; present && raw != nil {
		p, ok := raw.(string)
		if !ok {
			return fmt.Errorf("cloud.provider: must be a string, got %v", raw)
		}
		primary = CloudProviderName(p)
		if primary != "" && primary != "none" && !contains(CloudProviders, primary) {
			return fmt.Errorf("cloud.provider: unknown provider %q (known: %s, none)", p, strings.Join(CloudProviders, ", "))
		}
	}
	raw, present := m.Cloud["also"]
	if !present || raw == nil {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("cloud.also: must be a list of provider names (e.g. [aws]), got %v", raw)
	}
	if len(list) > 0 && (primary == "" || primary == "none") {
		return fmt.Errorf("cloud.also: needs cloud.provider to name a primary cloud (not empty or none)")
	}
	seen := map[string]bool{}
	for _, v := range list {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("cloud.also: entries must be strings, got %v", v)
		}
		name := CloudProviderName(s)
		if !contains(CloudProviders, name) {
			return fmt.Errorf("cloud.also: unknown provider %q (known: %s)", s, strings.Join(CloudProviders, ", "))
		}
		if name == primary {
			return fmt.Errorf("cloud.also: %q is already the primary cloud.provider", s)
		}
		if seen[name] {
			return fmt.Errorf("cloud.also: %q is listed twice", s)
		}
		seen[name] = true
	}
	return nil
}

// IdentityNames returns the sorted providers this workspace isolates.
// v1 manifests (no identities block) default to claude+gh plus the cloud CLI.
// Every cloud.also cloud is isolated too: with an identities block, one that
// block does not declare is added implicitly, so the guard never allows a
// cloud CLI that has no private config home.
func (m *Manifest) IdentityNames() []string {
	var out []string
	var clouds []string
	if len(m.Identities) > 0 {
		for k := range m.Identities {
			out = append(out, k)
		}
	} else {
		out = []string{"claude", "gh"}
		if p, _ := m.Cloud["provider"].(string); p != "" {
			clouds = append(clouds, p)
		}
	}
	// cloud.also: further clouds this workspace may use (see guard.Input.ExtraClouds).
	if also, ok := m.Cloud["also"].([]any); ok {
		for _, v := range also {
			if s, ok := v.(string); ok {
				clouds = append(clouds, s)
			}
		}
	}
	seen := map[string]bool{}
	for _, n := range out {
		seen[n] = true
	}
	for _, p := range clouds {
		id := cloudIdentity[CloudProviderName(p)]
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func (m *Manifest) HasCloudIdentity() bool {
	for _, n := range m.IdentityNames() {
		// azure-devops runs a full `az login` in its own config dir: the same ARM access as az.
		if n == "az" || n == "azure-devops" || n == "aws" || n == "gcloud" || n == "doctl" {
			return true
		}
	}
	return false
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// DataClasses are the classification labels a manifest may use (spec §11.1).
var DataClasses = []string{"public", "internal", "pii", "sensitive", "financial", "children", "secrets"}

// DataPath classifies a repository path glob or a database table.
type DataPath struct {
	Path    string   `yaml:"path,omitempty"`
	Table   string   `yaml:"table,omitempty"`
	Classes []string `yaml:"classes"`
}

type Prompts struct {
	Allow []string `yaml:"allow"`
}

type Incident struct {
	Authority string `yaml:"authority"` // ANPD | GDPR | none
	Contact   string `yaml:"contact"`
}

// Data is the manifest's data-protection block. Empty means "not classified".
type Data struct {
	Classes          []string   `yaml:"classes"`
	Regions          []string   `yaml:"regions"`
	Prompts          Prompts    `yaml:"prompts"`
	Paths            []DataPath `yaml:"paths"`
	RetentionDefault string     `yaml:"retention_default"`
	Incident         Incident   `yaml:"incident"`
}

var (
	retentionRe = regexp.MustCompile(`^\d{1,4}[dmy]$`)
	regionRe    = regexp.MustCompile(`^[A-Z]{2}$`)
)

func (d Data) Enabled() bool { return len(d.Classes) > 0 }

// Validate rejects unknown classes, inconsistent paths and malformed values so
// a typo cannot silently leave data unclassified.
func (d Data) Validate() error {
	if !d.Enabled() && (len(d.Paths) > 0 || len(d.Prompts.Allow) > 0) {
		return fmt.Errorf("data.classes is required when data.paths or data.prompts are set")
	}
	for _, c := range d.Classes {
		if !contains(DataClasses, c) {
			return fmt.Errorf("data.classes: unknown class %q (known: %v)", c, DataClasses)
		}
	}
	for _, c := range d.Prompts.Allow {
		if c != "public" && c != "internal" {
			return fmt.Errorf("data.prompts.allow: %q is not allowed in prompts (only public or internal)", c)
		}
	}
	for i, p := range d.Paths {
		if (p.Path == "") == (p.Table == "") {
			return fmt.Errorf("data.paths[%d]: exactly one of path or table is required", i)
		}
		if len(p.Classes) == 0 {
			return fmt.Errorf("data.paths[%d]: classes is required", i)
		}
		for _, c := range p.Classes {
			if !contains(DataClasses, c) {
				return fmt.Errorf("data.paths[%d].classes: unknown class %q", i, c)
			}
		}
	}
	if d.RetentionDefault != "" && !retentionRe.MatchString(d.RetentionDefault) {
		return fmt.Errorf("data.retention_default: use <n>d, <n>m or <n>y, got %q", d.RetentionDefault)
	}
	for _, r := range d.Regions {
		if !regionRe.MatchString(r) {
			return fmt.Errorf("data.regions: %q is not a two-letter ISO country code", r)
		}
	}
	switch d.Incident.Authority {
	case "", "ANPD", "GDPR", "none":
	default:
		return fmt.Errorf("data.incident.authority must be ANPD, GDPR or none, got %q", d.Incident.Authority)
	}
	if LooksLikeSecret(d.Incident.Contact) {
		return fmt.Errorf("data.incident.contact looks like a secret")
	}
	return nil
}

// Classified returns the sorted, de-duplicated classes of every path glob
// matching the repo-relative path (slash or backslash), or nil.
func (d Data) Classified(path string) []string {
	rel := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(path), `\`, "/"), "./")
	set := map[string]bool{}
	for _, p := range d.Paths {
		if p.Path == "" {
			continue
		}
		if GlobMatch(strings.ReplaceAll(p.Path, `\`, "/"), rel) {
			for _, c := range p.Classes {
				set[c] = true
			}
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// Tables maps lower-cased table names (with and without their schema) to classes.
func (d Data) Tables() map[string][]string {
	out := map[string][]string{}
	for _, p := range d.Paths {
		if p.Table == "" {
			continue
		}
		name := strings.ToLower(strings.Trim(p.Table, `"`))
		out[name] = append(out[name], p.Classes...)
		if i := strings.LastIndex(name, "."); i >= 0 {
			out[name[i+1:]] = append(out[name[i+1:]], p.Classes...)
		}
	}
	return out
}

// GlobMatch matches a slash path against a glob where * is one segment and
// ** any number of segments (including none). Both are anchored at the root.
func GlobMatch(pattern, path string) bool {
	pattern = strings.Trim(pattern, "/")
	path = strings.Trim(path, "/")
	return globSegs(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

func globSegs(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(segs); i++ {
				if globSegs(pat[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, _ := filepath.Match(pat[0], segs[0]); !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}
