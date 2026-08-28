package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/tentaqles/tentaqles/internal/detect"
	"github.com/tentaqles/tentaqles/internal/hooks"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/setup"
	"github.com/tentaqles/tentaqles/internal/workspace"
)

// companyAnswers holds one company's raw wizard answers before they are
// turned into a setup.Company.
type companyAnswers struct {
	Name           string
	DisplayName    string
	Color          string
	GitName        string
	GitEmail       string
	GitUser        string
	Identities     []string
	PermissionMode string
}

// wizardAnswers holds everything gathered across the wizard's steps before
// planFromAnswers turns it into a setup.SetupPlan.
type wizardAnswers struct {
	Base      string
	Companies []companyAnswers
	Hooks     []string
}

// buildProviderOptions renders the catalog as huh MultiSelect options,
// labeled "<Name> (<category>)" with the provider id as the value, sorted
// by category then name so related providers group together on screen.
func buildProviderOptions(cat *providers.Catalog) []huh.Option[string] {
	all := append([]providers.Provider(nil), cat.All()...)
	sort.Slice(all, func(i, j int) bool {
		if all[i].Category != all[j].Category {
			return all[i].Category < all[j].Category
		}
		return all[i].Name < all[j].Name
	})
	opts := make([]huh.Option[string], 0, len(all))
	for _, p := range all {
		label := fmt.Sprintf("%s (%s)", p.Name, p.Category)
		opts = append(opts, huh.NewOption(label, p.ID))
	}
	return opts
}

// permissionModeOptions returns manifest.PermissionModes with the empty
// ("unset") entry dropped, as huh Select options.
func permissionModeOptions() []huh.Option[string] {
	opts := make([]huh.Option[string], 0, len(manifest.PermissionModes))
	for _, m := range manifest.PermissionModes {
		if m == "" {
			continue
		}
		opts = append(opts, huh.NewOption(m, m))
	}
	return opts
}

// planFromAnswers is a pure function that turns wizardAnswers into a
// setup.SetupPlan. It does no I/O and applies no defaults beyond what the
// forms already validated.
func planFromAnswers(a wizardAnswers) *setup.SetupPlan {
	companies := make([]setup.Company, 0, len(a.Companies))
	for _, ca := range a.Companies {
		companies = append(companies, setup.Company{
			Name:           ca.Name,
			DisplayName:    ca.DisplayName,
			Color:          ca.Color,
			GitName:        ca.GitName,
			GitEmail:       ca.GitEmail,
			GitUser:        ca.GitUser,
			Identities:     ca.Identities,
			PermissionMode: ca.PermissionMode,
		})
	}
	return &setup.SetupPlan{
		Base:      a.Base,
		Companies: companies,
		Hooks:     a.Hooks,
		Trust:     true,
	}
}

// runWizard drives the interactive `tq setup` flow described in the task
// brief: welcome, base folder, company loop (providers + permission mode),
// tool check, hooks, preview, and apply.
func runWizard(cat *providers.Catalog, profiles hooks.Profiles) (*setup.SetupPlan, error) {
	var consent bool
	if err := huh.NewForm(huh.NewGroup(
		huh.NewNote().
			Title("tq setup").
			Description("This wizard scaffolds one or more client workspaces: a base folder, "+
				"per-company git identity and provider isolation, shell hooks, and a preview "+
				"before anything is written."),
		huh.NewConfirm().
			Title("Continue?").
			Value(&consent),
	)).Run(); err != nil {
		return nil, err
	}
	if !consent {
		return nil, fmt.Errorf("setup cancelled")
	}

	base := "~/work"
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Base folder").
			Description("Each company gets a subdirectory under this folder.").
			Value(&base).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("base folder is required")
				}
				return nil
			}),
	)).Run(); err != nil {
		return nil, err
	}
	base = setup.ExpandHome(strings.TrimSpace(base))

	if _, err := os.Stat(base); os.IsNotExist(err) {
		var create bool
		if err := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("%s does not exist. Create it?", base)).
				Value(&create),
		)).Run(); err != nil {
			return nil, err
		}
		if create {
			if err := os.MkdirAll(base, 0o755); err != nil {
				return nil, fmt.Errorf("create base folder: %w", err)
			}
		}
	}

	var companies []companyAnswers
	for {
		ca := companyAnswers{}
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Company name (short id)").Value(&ca.Name).
				Validate(func(s string) error {
					if !workspace.NameRe.MatchString(s) {
						return fmt.Errorf("must match %s", workspace.NameRe.String())
					}
					for _, prev := range companies {
						if prev.Name == s {
							return fmt.Errorf("company %q was already added", s)
						}
					}
					return nil
				}),
			huh.NewInput().Title("Display name").Value(&ca.DisplayName),
			huh.NewInput().Title("Color (optional)").Value(&ca.Color),
			huh.NewInput().Title("Git name").Value(&ca.GitName).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("git name is required")
					}
					return nil
				}),
			huh.NewInput().Title("Git email").Value(&ca.GitEmail).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" || !strings.Contains(s, "@") {
						return fmt.Errorf("a valid email is required")
					}
					return nil
				}),
			huh.NewInput().Title("Git user (optional, e.g. GitHub username)").Value(&ca.GitUser),
		)).Run(); err != nil {
			return nil, err
		}

		if err := huh.NewForm(huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Providers for "+ca.Name).
				Options(buildProviderOptions(cat)...).
				Value(&ca.Identities),
			huh.NewSelect[string]().
				Title("Permission mode for "+ca.Name).
				Options(permissionModeOptions()...).
				Value(&ca.PermissionMode),
		)).Run(); err != nil {
			return nil, err
		}

		companies = append(companies, ca)

		var another bool
		if err := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().Title("Add another company?").Value(&another),
		)).Run(); err != nil {
			return nil, err
		}
		if !another {
			break
		}
	}

	// Tool check: build a provisional plan so ToolCheck can run against it.
	toolCheckPlan := planFromAnswers(wizardAnswers{Base: base, Companies: companies})
	tc := setup.ToolCheck(toolCheckPlan, cat, detect.DefaultDeps())
	var toolLines []string
	firstHint := ""
	deps := detect.DefaultDeps()
	for company, results := range tc {
		toolLines = append(toolLines, company+":")
		for _, r := range results {
			p, ok := cat.Get(r.ID)
			switch {
			case !ok || p.CLI == nil:
				toolLines = append(toolLines, fmt.Sprintf("  [n/a] %s — no CLI", r.ID))
			case r.Installed:
				version := r.Version
				if version == "" {
					version = "unknown version"
				}
				toolLines = append(toolLines, fmt.Sprintf("  [ok] %s %s", r.ID, version))
			default:
				hints := detect.Hints(p, deps.GOOS)
				hint := "no install hint available"
				if len(hints) > 0 && hints[0] != "no CLI to install" {
					hint = hints[0]
				}
				if firstHint == "" {
					firstHint = hint
				}
				toolLines = append(toolLines, fmt.Sprintf("  [missing] %s -> %s", r.ID, hint))
			}
		}
	}
	toolDesc := strings.Join(toolLines, "\n")
	if firstHint != "" {
		toolDesc += "\n\nHint: " + firstHint
	}
	var continueAfterToolCheck bool = true
	if len(toolLines) > 0 {
		if err := huh.NewForm(huh.NewGroup(
			huh.NewNote().Title("Tool check").Description(toolDesc),
			huh.NewConfirm().Title("Continue?").Value(&continueAfterToolCheck),
		)).Run(); err != nil {
			return nil, err
		}
		if !continueAfterToolCheck {
			return nil, fmt.Errorf("setup cancelled after tool check")
		}
	}

	detected := hooks.Detect(profiles, hooks.LookPath)
	var selectedHooks []string
	hookOpts := make([]huh.Option[string], 0, len(profiles))
	shells := make([]string, 0, len(profiles))
	for sh := range profiles {
		shells = append(shells, string(sh))
	}
	sort.Strings(shells)
	detectedSet := map[string]bool{}
	for _, sh := range detected {
		detectedSet[string(sh)] = true
	}
	for _, sh := range shells {
		hookOpts = append(hookOpts, huh.NewOption(sh, sh).Selected(detectedSet[sh]))
	}
	if len(hookOpts) > 0 {
		if err := huh.NewForm(huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Shell hooks to install").
				Options(hookOpts...).
				Value(&selectedHooks),
		)).Run(); err != nil {
			return nil, err
		}
	}

	plan := planFromAnswers(wizardAnswers{Base: base, Companies: companies, Hooks: selectedHooks})

	changes, err := setup.Preview(plan, profiles)
	if err != nil {
		return nil, err
	}
	var previewLines []string
	for _, ch := range changes {
		previewLines = append(previewLines, fmt.Sprintf("%s  %s  %s", ch.Kind, ch.Target, ch.Detail))
	}
	var apply bool
	if err := huh.NewForm(huh.NewGroup(
		huh.NewNote().Title("Preview").Description(strings.Join(previewLines, "\n")),
		huh.NewConfirm().Title("Apply these changes?").Value(&apply),
	)).Run(); err != nil {
		return nil, err
	}
	if !apply {
		return nil, fmt.Errorf("setup cancelled before apply")
	}

	if err := plan.Validate(cat); err != nil {
		return nil, err
	}

	return plan, nil
}

// saveWizardPlan writes plan to ~/.tentaqles/last-setup.yaml (always) and,
// when writePlanPath is non-empty, to that path as well.
func saveWizardPlan(plan *setup.SetupPlan, writePlanPath string) error {
	lastPath := filepath.Join(paths.Home(), "last-setup.yaml")
	if err := os.MkdirAll(filepath.Dir(lastPath), 0o700); err != nil {
		return err
	}
	if err := plan.Save(lastPath); err != nil {
		return err
	}
	if writePlanPath != "" {
		if err := plan.Save(writePlanPath); err != nil {
			return err
		}
	}
	return nil
}
