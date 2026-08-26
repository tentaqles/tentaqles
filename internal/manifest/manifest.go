// Package manifest parses .tentaqles.yaml (schemas v1 and v2).
package manifest

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"
)

const FileName = ".tentaqles.yaml"

var (
	ErrUnsupportedSchema = errors.New("unsupported manifest schema (want tentaqles-client-v1 or -v2)")
	ErrSecretLike        = errors.New("manifest contains a secret-looking value; manifests hold names only")
)

// KnownIdentities are the CLI providers tq can isolate. Keep in sync with envplan.providers.
var KnownIdentities = []string{"claude", "codex", "gemini", "cursor", "gh", "az", "aws", "gcloud", "kube", "npm"}

var PermissionModes = []string{"", "default", "acceptEdits", "plan", "bypass"}

type Git struct {
	Name         string `yaml:"name"`
	Email        string `yaml:"email"`
	Provider     string `yaml:"provider"`
	User         string `yaml:"user"`
	Host         string `yaml:"host"`
	ExpectedUser string `yaml:"expected_user"`
}

type Identity struct {
	ShareCapabilities    *bool  `yaml:"share_capabilities"`
	ExpectedSubscription string `yaml:"expected_subscription"`
	Profile              string `yaml:"profile"`
}

type Claude struct {
	PermissionMode string `yaml:"permission_mode"`
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
	Path            string              `yaml:"-"`
}

var secretRe = regexp.MustCompile(`(?i)\b(ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9\-]{8,}|AKIA[0-9A-Z]{16}|xox[baprs]-[A-Za-z0-9\-]+|Bearer\s+\S+|eyJ[A-Za-z0-9_\-]{20,}\.[A-Za-z0-9_\-]{10,})`)

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
		if !contains(KnownIdentities, name) {
			return nil, fmt.Errorf("%s: unknown identity %q (known: %v)", path, name, KnownIdentities)
		}
	}
	if !contains(PermissionModes, m.Claude.PermissionMode) {
		return nil, fmt.Errorf("%s: claude.permission_mode must be one of default|acceptEdits|plan|bypass", path)
	}
	m.Path = path
	return &m, nil
}

// IdentityNames returns the sorted providers this workspace isolates.
// v1 manifests (no identities block) default to claude+gh plus the cloud CLI.
func (m *Manifest) IdentityNames() []string {
	if len(m.Identities) > 0 {
		out := make([]string, 0, len(m.Identities))
		for k := range m.Identities {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	out := []string{"claude", "gh"}
	if p, _ := m.Cloud["provider"].(string); p != "" {
		switch p {
		case "azure":
			out = append(out, "az")
		case "aws":
			out = append(out, "aws")
		case "gcp", "gcloud":
			out = append(out, "gcloud")
		}
	}
	sort.Strings(out)
	return out
}

func (m *Manifest) HasCloudIdentity() bool {
	for _, n := range m.IdentityNames() {
		if n == "az" || n == "aws" || n == "gcloud" {
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
