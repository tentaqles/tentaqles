// Package setup builds and applies a multi-company tq setup plan: it can
// register a base folder, scaffold one workspace per company, install shell
// hooks, and report what a dry run would do (Preview) or what actually
// happened (Apply).
package setup

import (
	"fmt"
	"os"

	"github.com/tentaqles/tentaqles/cli/internal/manifest"
	"github.com/tentaqles/tentaqles/cli/internal/providers"
	"github.com/tentaqles/tentaqles/cli/internal/workspace"
	"gopkg.in/yaml.v3"
)

// Company describes one client/company to scaffold as a tq workspace.
type Company struct {
	Name           string   `yaml:"name"`
	DisplayName    string   `yaml:"display_name,omitempty"`
	Color          string   `yaml:"color,omitempty"`
	GitName        string   `yaml:"git_name"`
	GitEmail       string   `yaml:"git_email"`
	GitUser        string   `yaml:"git_user,omitempty"`
	Identities     []string `yaml:"identities,omitempty"`
	PermissionMode string   `yaml:"permission_mode,omitempty"`
}

// SetupPlan is the full declarative setup: a base folder, the companies to
// scaffold under it, which shell hooks to install, and whether new
// workspaces should be auto-trusted.
type SetupPlan struct {
	Base      string    `yaml:"base"`
	Companies []Company `yaml:"companies"`
	Hooks     []string  `yaml:"hooks,omitempty"`
	Trust     bool      `yaml:"trust"`
}

// rawPlan mirrors SetupPlan but lets Trust default to true when the key is
// absent from the YAML document (a plain bool can't distinguish "absent"
// from "false").
type rawPlan struct {
	Base      string    `yaml:"base"`
	Companies []Company `yaml:"companies"`
	Hooks     []string  `yaml:"hooks,omitempty"`
	Trust     *bool     `yaml:"trust,omitempty"`
}

// defaultIdentities is used whenever a Company doesn't list identities.
var defaultIdentities = []string{"claude", "gh"}

func effectiveIdentities(c Company) []string {
	if len(c.Identities) == 0 {
		return append([]string(nil), defaultIdentities...)
	}
	return c.Identities
}

// LoadPlan reads and parses a setup plan YAML file at path.
func LoadPlan(path string) (*SetupPlan, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r rawPlan
	if err := yaml.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	trust := true
	if r.Trust != nil {
		trust = *r.Trust
	}
	return &SetupPlan{Base: r.Base, Companies: r.Companies, Hooks: r.Hooks, Trust: trust}, nil
}

// Save writes p as YAML to path.
func (p *SetupPlan) Save(path string) error {
	raw, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// Validate checks structural rules on the plan: a base is set, company
// names match the workspace naming rule and are unique, identities are
// known to cat, and permission modes are valid.
func (p *SetupPlan) Validate(cat *providers.Catalog) error {
	if p.Base == "" {
		return fmt.Errorf("base is required")
	}
	seen := map[string]bool{}
	for _, c := range p.Companies {
		if !workspace.NameRe.MatchString(c.Name) {
			return fmt.Errorf("company %q: name must match %s", c.Name, workspace.NameRe.String())
		}
		if seen[c.Name] {
			return fmt.Errorf("duplicate company name %q", c.Name)
		}
		seen[c.Name] = true

		for _, id := range effectiveIdentities(c) {
			if cat != nil {
				if _, ok := cat.Get(id); !ok {
					return fmt.Errorf("company %q: unknown identity %q", c.Name, id)
				}
			}
		}

		if !contains(manifest.PermissionModes, c.PermissionMode) {
			return fmt.Errorf("company %q: invalid permission_mode %q (must be one of %v)", c.Name, c.PermissionMode, manifest.PermissionModes)
		}
	}
	return nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// Example returns a two-company sample plan used to seed `tq setup init`.
func Example() *SetupPlan {
	return &SetupPlan{
		Base:  "C:/repos",
		Trust: true,
		Hooks: []string{"bash", "zsh", "pwsh"},
		Companies: []Company{
			{
				Name:           "acme",
				DisplayName:    "Acme Corp",
				Color:          "blue",
				GitName:        "Jane Doe",
				GitEmail:       "jane@acme.com",
				Identities:     []string{"claude", "gh"},
				PermissionMode: "acceptEdits",
			},
			{
				Name:           "globex",
				DisplayName:    "Globex Corporation",
				Color:          "green",
				GitName:        "Jane Doe",
				GitEmail:       "jane@globex.com",
				Identities:     []string{"claude", "gh"},
				PermissionMode: "default",
			},
		},
	}
}

// ExampleYAML renders Example() as a commented YAML document, suitable for
// writing out as a starting point for the user to edit.
func ExampleYAML() string {
	raw, err := yaml.Marshal(Example())
	if err != nil {
		return ""
	}
	header := "" +
		"# tq setup plan — edit this file, then run:\n" +
		"#   tq setup --from <this file> --dry-run\n" +
		"#   tq setup --from <this file> --yes\n" +
		"#\n" +
		"# base: the folder each company gets a subdirectory under.\n" +
		"# companies: one entry per client; git_name/git_email are required.\n" +
		"# identities: CLI tools to isolate per workspace (defaults to claude, gh).\n" +
		"# hooks: shells to install tq's activation hook into.\n" +
		"# trust: whether newly scaffolded workspaces are auto-trusted.\n"
	return header + string(raw)
}
