// Package manifest parses .tentaqles.yaml (schemas v1 and v2).
package manifest

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

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
	Bundle *Bundle `yaml:"bundle"`
	// Env is merged into the identity's settings.json env by
	// `tq settings render`. Values are checked like the rest of the manifest:
	// a secret-shaped value makes the manifest fail to load.
	Env map[string]string `yaml:"env"`
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
	// Company services (spec B §6); each block is optional. See services.go.
	Chat         *Service `yaml:"chat,omitempty"`
	Email        *Service `yaml:"email,omitempty"`
	DataPlatform *Service `yaml:"data_platform,omitempty"`
	Agents       []string `yaml:"agents,omitempty"`
	// ExtraScope adds catalog ids to the derived provider scope (Scope).
	ExtraScope []string `yaml:"scope,omitempty"`
	// MCP is the mcp: block: allow lists the exact MCP server names kept though
	// no in-scope provider claims them and they are not general-purpose (scope.go, ADR-0008).
	MCP MCPBlock `yaml:"mcp,omitempty"`
	// Browsers are the browser profiles this client uses (browser.go). OtherProviders are names the
	// developer gave that the catalog does not know: never in the scope, since tq can neither
	// isolate nor guard them.
	Browsers       []Browser `yaml:"browsers,omitempty"`
	OtherProviders []string  `yaml:"other_providers,omitempty"`
	Path           string    `yaml:"-"`
	// Ignored lists the top-level blocks of DroppedBlocks, then the claude keys
	// of DroppedClaudeKeys (as "claude.<key>"), that the file still carries.
	// Load records them so `tq doctor` can warn that tq no longer reads them
	// (the cut of 2026-11-03, docs/prd.md D-29, and the permission settings
	// retired on 2026-11-05); loading never fails on them.
	Ignored []string `yaml:"-"`
	// OldPermissionMode is the claude.permission_mode an older manifest
	// still carries ("" when none or not a string). tq never applies it: it
	// is the evidence `tq settings render` needs to tell the defaultMode an
	// older render wrote from one the user set (ADR-0006).
	OldPermissionMode string `yaml:"-"`
}

// DroppedBlocks are the manifest blocks that tq 0.9.0 stops reading: the Jev
// decision layer, the guard modes and rules, the tracker sync, the packs and
// the data classification. A manifest that still carries one loads normally;
// the block is listed in Manifest.Ignored and reported by `tq doctor`.
var DroppedBlocks = []string{"decision", "guard", "tracker", "packs", "data"}

// DroppedClaudeKeys are the keys of the claude block tq no longer reads: the
// permission mode and the permission rules (PRD v2 §13: permission modes of
// any kind are a non-goal). A manifest that still carries one loads normally;
// the key is listed in Manifest.Ignored as "claude.<key>" and never decoded.
var DroppedClaudeKeys = []string{"permission_mode", "permissions"}

// droppedBlocksIn returns, in DroppedBlocks order, the dropped top-level keys
// present in raw. A file that does not parse as a mapping yields none; the
// typed decode reports the real error.
func droppedBlocksIn(raw []byte) []string {
	var top map[string]any
	if err := yaml.Unmarshal(raw, &top); err != nil {
		return nil
	}
	var out []string
	for _, k := range DroppedBlocks {
		v, ok := top[k]
		if !ok {
			continue
		}
		if _, scalar := v.(string); scalar && k == "guard" {
			continue // an older manifest's guard: v2 flag: there is one guard now, so it means nothing, and nothing is dropped
		}
		out = append(out, k)
	}
	if claude, ok := top["claude"].(map[string]any); ok {
		for _, k := range DroppedClaudeKeys {
			if _, ok := claude[k]; ok {
				out = append(out, "claude."+k)
			}
		}
	}
	return out
}

// oldPermissionMode reads claude.permission_mode out of raw, for evidence
// only (Manifest.OldPermissionMode).
func oldPermissionMode(raw []byte) string {
	var top struct {
		Claude map[string]any `yaml:"claude"`
	}
	if yaml.Unmarshal(raw, &top) != nil || top.Claude == nil {
		return ""
	}
	s, _ := top.Claude["permission_mode"].(string)
	return s
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
	return Parse(raw, path)
}

// Parse is Load for bytes already read: path only names them in errors and in Manifest.Path.
// tentaqles reach checks an edited manifest with it before writing the file.
func Parse(raw []byte, path string) (*Manifest, error) {
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
			return nil, fmt.Errorf("%s: unknown identity %q (known: %v)%s", path, name, known, unknownProviderHint(name))
		}
	}
	if err := m.validateCloud(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := m.ValidateServices(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := m.validateScope(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := m.validateMCP(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := m.ValidateBrowsers(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	m.Path = path
	m.Ignored = droppedBlocksIn(raw)
	m.OldPermissionMode = oldPermissionMode(raw)
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
// With no identities block it defaults to claude+gh. Every cloud the manifest
// declares (cloud.provider and cloud.also) is isolated too, added when the
// identities block leaves it out: the client's own cloud is always in scope,
// and the guard never allows a cloud CLI that has no private config home.
func (m *Manifest) IdentityNames() []string {
	var out []string
	var clouds []string
	if len(m.Identities) > 0 {
		for k := range m.Identities {
			out = append(out, k)
		}
	} else {
		out = []string{"claude", "gh"}
	}
	if p, _ := m.Cloud["provider"].(string); p != "" {
		clouds = append(clouds, p)
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
