// Package workspace creates and registers workspaces.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"gopkg.in/yaml.v3"
)

// NameRe is the pattern workspace names must match: lowercase alphanumerics,
// dots, underscores, and hyphens, starting with an alphanumeric.
var NameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type AddOptions struct {
	Base, Name, GitName, GitEmail, GitUser string
	// GitProvider names the git host (github, gitlab, azure-devops, ...). It
	// decides which CLI an expected_user is checked against, so an empty value
	// on a GitLab workspace would have tq comparing a GitLab handle to whatever
	// gh reports.
	GitProvider string
	Identities  []string
	RunGit      func(args ...string) (string, error)
	// Email is the email provider block; nil leaves it out.
	Email *manifest.Service
	// Cloud is the cloud block ({provider, also}); nil leaves it out.
	Cloud map[string]any
	// ExtraScope, Browsers and OtherProviders are the scope:, browsers: and other_providers: blocks;
	// empty leaves them out. tentaqles reach sets them.
	// Language is the client's language tag; empty is manifest.DefaultLanguage.
	Language       string
	ExtraScope     []string
	Browsers       []manifest.Browser
	OtherProviders []string
}

func Add(o AddOptions) (*resolve.Workspace, error) {
	if !NameRe.MatchString(o.Name) {
		return nil, fmt.Errorf("workspace name %q must match %s", o.Name, NameRe)
	}
	if o.GitEmail == "" {
		return nil, fmt.Errorf("--git-email is required")
	}
	if err := gitcfg.ValidateValue(o.GitEmail); err != nil {
		return nil, err
	}
	if err := gitcfg.ValidateValue(o.GitName); err != nil {
		return nil, err
	}
	if len(o.Identities) == 0 {
		o.Identities = []string{"claude", "gh"}
	}
	base, err := registry.Normalize(o.Base)
	if err != nil {
		return nil, err
	}
	if o.Language == "" {
		o.Language = manifest.DefaultLanguage
	}
	if _, ok := manifest.LanguageOf(o.Language); !ok {
		return nil, fmt.Errorf("unknown language %q", o.Language)
	}
	services := manifest.Manifest{Email: o.Email}
	if err := services.ValidateServices(); err != nil {
		return nil, err
	}
	root := filepath.Join(base, o.Name)
	// The manifest lives under $TQ_HOME, a
	// name-only marker in the root. Either file already there is a workspace
	// tq will not overwrite.
	mp := filepath.Join(root, manifest.FileName)
	pp := paths.ManifestPath(o.Name)
	if _, err := os.Stat(mp); err == nil {
		return nil, fmt.Errorf("%s already exists", mp)
	}
	if _, err := os.Stat(pp); err == nil {
		return nil, fmt.Errorf("%s already exists (a previous %s workspace? remove it first)", pp, o.Name)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	m := manifest.Manifest{
		Schema: "tentaqles-client-v2", Client: o.Name,
		Git:        manifest.Git{Name: o.GitName, Email: o.GitEmail, User: o.GitUser, Provider: o.GitProvider},
		Language:   o.Language,
		Identities: map[string]manifest.Identity{},
		Email:      o.Email,
		Cloud:      o.Cloud, ExtraScope: o.ExtraScope, Browsers: o.Browsers, OtherProviders: o.OtherProviders,
	}
	for _, id := range o.Identities {
		m.Identities[id] = manifest.Identity{}
	}
	raw, err := yaml.Marshal(&m)
	if err != nil {
		return nil, err
	}
	// Scaffold everything else BEFORE the marker: the marker is what makes a
	// directory look like a workspace, so writing it last means an
	// interrupted run never leaves a half-created workspace behind.
	for _, id := range o.Identities {
		d := paths.IdentityDir(o.Name, id)
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
		if id == "claude" {
			s := filepath.Join(d, "settings.json")
			if _, err := os.Stat(s); os.IsNotExist(err) {
				_ = os.WriteFile(s, []byte("{}\n"), 0o600)
			}
		}
	}
	// 0700 / 0600: the state folder is private to the user.
	if err := os.MkdirAll(filepath.Dir(pp), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(pp, append([]byte("# tentaqles workspace manifest — names only, never secrets\n"), raw...), 0o600); err != nil {
		return nil, err
	}
	if err := gitcfg.WriteWorkspace(root, o.GitName, o.GitEmail); err != nil {
		return nil, err
	}
	if err := os.WriteFile(mp, manifest.MarkerBytes(o.Name), 0o644); err != nil {
		return nil, err
	}
	loaded, err := manifest.Load(pp)
	if err != nil {
		return nil, err
	}
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	if _, err := cfg.AddBase(base); err != nil {
		return nil, err
	}
	if err := cfg.Bind(o.Name, root); err != nil {
		return nil, err
	}
	if err := cfg.Save(); err != nil {
		return nil, err
	}
	if err := SyncGit(cfg); err != nil {
		return nil, err
	}
	if o.RunGit != nil {
		if err := gitcfg.EnsureGlobal(o.RunGit); err != nil {
			return nil, err
		}
	}
	return &resolve.Workspace{Name: o.Name, Root: root, Base: base, ManifestPath: pp, Manifest: loaded}, nil
}
