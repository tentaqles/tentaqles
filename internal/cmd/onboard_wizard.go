package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/onboard"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/workspace"
	"gopkg.in/yaml.v3"
)

// onboardWizardResult is what the interactive tq onboard collected.
type onboardWizardResult struct {
	Answers      *onboard.Answers
	OpenTerminal bool
}

// skipValue is the Select value of "skip this question": the block stays out of the manifest.
const skipValue = ""

// skipOptions puts a Skip entry first, then one option per value (labelled by the catalog name
// when there is one).
func skipOptions(cat *providers.Catalog, values ...string) []huh.Option[string] {
	opts := []huh.Option[string]{huh.NewOption("Skip", skipValue)}
	for _, v := range values {
		label := v
		if v == "none" {
			label = "None (the company does not use one)"
		} else if p, ok := cat.Get(v); ok {
			label = p.Name + " (" + v + ")"
		}
		opts = append(opts, huh.NewOption(label, v))
	}
	return opts
}

func idsOf(ps []providers.Provider) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

// trackerScopeTitles are the wizard's labels for tracker.scope keys, per provider (spec B §6).
func trackerScopeTitles(provider string) (org, project, board string) {
	switch provider {
	case "azure-devops":
		return "Azure DevOps organization", "Project", "Team (optional)"
	case "jira":
		return "Jira site (e.g. acme for acme.atlassian.net)", "Project key", "Board (optional)"
	case "github":
		return "GitHub owner (org or user)", "Repository (optional)", "Project number (optional, Projects v2)"
	case "linear":
		return "Linear team key", "Project (optional)", "Board (optional)"
	case "asana":
		return "Asana workspace", "Project (optional)", "Board (optional)"
	}
	return "Organization", "Project", "Board (optional)"
}

func notEmpty(field string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
}

// runOnboardWizard asks every tq onboard question; each block can be skipped.
func runOnboardWizard(name, base string) (onboardWizardResult, error) {
	cat := providers.MustLoad()
	a := &onboard.Answers{Name: name, Base: base}
	run := func(fields ...huh.Field) error { return huh.NewForm(huh.NewGroup(fields...)).Run() }

	// 1. Identity and git.
	intro := huh.NewNote().Title("tq onboard").Description(
		"Sets up one client folder and records the company's tools in its manifest.\n" +
			"Every question but the name and email can be skipped. Names only: tq never asks for a token.")
	fields := []huh.Field{intro}
	if a.Name == "" {
		fields = append(fields, huh.NewInput().Title("Workspace name (short id, e.g. acme)").Value(&a.Name).
			Validate(func(s string) error {
				if !workspace.NameRe.MatchString(strings.TrimSpace(s)) {
					return fmt.Errorf("must match %s", workspace.NameRe)
				}
				return nil
			}))
	}
	if a.Base == "" {
		fields = append(fields, huh.NewInput().Title("Base folder (the workspace is created under it)").Value(&a.Base).Validate(notEmpty("the base folder")))
	}
	fields = append(fields,
		huh.NewInput().Title("Your email at this company (git user.email)").Value(&a.Git.Email).
			Validate(func(s string) error {
				if !strings.Contains(s, "@") {
					return fmt.Errorf("a valid email is required")
				}
				return nil
			}),
		huh.NewInput().Title("Your name for commits (optional)").Value(&a.Git.Name),
		huh.NewInput().Title("Display name of the company (optional)").Value(&a.DisplayName),
		huh.NewSelect[string]().Title("Git provider").Options(skipOptions(cat, onboard.GitProviders...)...).Value(&a.Git.Provider),
		huh.NewInput().Title("Git username (optional; checked against the git CLI's login)").Value(&a.Git.User),
	)
	if err := run(fields...); err != nil {
		return onboardWizardResult{}, err
	}

	// 2. Cloud (+ also) and the data platform.
	cloud := skipValue
	if err := run(huh.NewSelect[string]().Title("Primary cloud").Options(skipOptions(cat, manifest.CloudProviders...)...).Value(&cloud)); err != nil {
		return onboardWizardResult{}, err
	}
	if cloud != skipValue {
		a.Cloud = &onboard.Cloud{Provider: cloud}
		var others []huh.Option[string]
		for _, c := range manifest.CloudProviders {
			if c != cloud {
				others = append(others, huh.NewOption(c, c))
			}
		}
		if err := run(huh.NewMultiSelect[string]().Title("Other clouds this company also uses (optional)").Options(others...).Value(&a.Cloud.Also)); err != nil {
			return onboardWizardResult{}, err
		}
	}
	data := skipValue
	agentOpts := []huh.Option[string]{}
	for _, id := range manifest.AgentCLIs() {
		label := id
		if p, ok := cat.Get(id); ok {
			label = p.Name + " (" + id + ")"
		}
		agentOpts = append(agentOpts, huh.NewOption(label, id).Selected(id == "claude"))
	}
	if err := run(
		huh.NewSelect[string]().Title("Data platform").Options(skipOptions(cat, append(idsOf(cat.ByCategory("data")), "none")...)...).Value(&data),
		huh.NewMultiSelect[string]().Title("Agent CLIs this workspace logs into").Options(agentOpts...).Value(&a.Agents),
	); err != nil {
		return onboardWizardResult{}, err
	}
	if data != skipValue {
		a.DataPlatform = &manifest.Service{Provider: data}
	}

	// 3. Tracker: provider, scope, auth (and the env name for auth env).
	tracker := skipValue
	if err := run(huh.NewSelect[string]().Title("Company tracker (the board tq hub mirrors)").
		Options(skipOptions(cat, manifest.TrackerProviders...)...).Value(&tracker)); err != nil {
		return onboardWizardResult{}, err
	}
	if tracker != skipValue {
		t := &manifest.Tracker{Provider: tracker, Auth: onboard.DefaultAuth(tracker)}
		var org, project, board string
		to, tp, tb := trackerScopeTitles(tracker)
		authOpts := []huh.Option[string]{
			huh.NewOption("cli: the tracker CLI's own login (gh, az, acli)", "cli"),
			huh.NewOption("keychain: a token in the OS keychain (tq secret set)", "keychain"),
			huh.NewOption("env: an env var resolved by tq dotenv", "env"),
			huh.NewOption("mcp: the agent's MCP connector, from a Claude session only", "mcp"),
		}
		if err := run(
			huh.NewInput().Title(to).Value(&org).Validate(notEmpty("this")),
			huh.NewInput().Title(tp).Value(&project),
			huh.NewInput().Title(tb).Value(&board),
			huh.NewSelect[string]().Title("Mirror which items").Options(
				huh.NewOption("Assigned to me (plus children of mirrored cards)", "me"),
				huh.NewOption("Anyone's, in the whole scope", "anyone")).Value(&t.AssignedTo),
			huh.NewSelect[string]().Title("Preferred credential source (the fallback order stays cli, keychain, env, mcp)").
				Options(authOpts...).Value(&t.Auth),
		); err != nil {
			return onboardWizardResult{}, err
		}
		t.Scope = map[string]string{"org": strings.TrimSpace(org)}
		if s := strings.TrimSpace(project); s != "" {
			t.Scope["project"] = s
		}
		if s := strings.TrimSpace(board); s != "" {
			t.Scope["board"] = s
		}
		if t.Auth == "env" {
			t.Env = onboard.SuggestEnvName(a.Name, tracker)
			if err := run(huh.NewInput().Title("Env var name tq dotenv resolves (the NAME only, never the token)").Value(&t.Env).
				Validate(func(s string) error {
					if !providers.EnvKeyRe.MatchString(strings.TrimSpace(s)) {
						return fmt.Errorf("not an environment variable name")
					}
					return nil
				})); err != nil {
				return onboardWizardResult{}, err
			}
			t.Env = strings.TrimSpace(t.Env)
		}
		a.Tracker = t
	}

	// 4. Chat and email (MCP connectors only).
	chat, mail := skipValue, skipValue
	if err := run(
		huh.NewSelect[string]().Title("Chat (an MCP connector the agent uses)").Options(skipOptions(cat, manifest.ChatProviders...)...).Value(&chat),
		huh.NewSelect[string]().Title("Email (an MCP connector the agent uses)").Options(skipOptions(cat, manifest.EmailProviders...)...).Value(&mail),
	); err != nil {
		return onboardWizardResult{}, err
	}
	if chat != skipValue {
		a.Chat = &manifest.Service{Provider: chat}
	}
	if mail != skipValue {
		a.Email = &manifest.Service{Provider: mail}
	}

	// 5. Preview, write, terminal.
	normalized, err := reparse(a)
	if err != nil {
		return onboardWizardResult{}, err
	}
	if err := normalized.Validate(); err != nil {
		return onboardWizardResult{}, err
	}
	preview, _ := yaml.Marshal(normalized)
	write, openTerm := true, true
	if err := run(
		huh.NewNote().Title("Preview of your answers").Description(string(preview)),
		huh.NewConfirm().Title("Write the workspace and its manifest?").Value(&write),
		huh.NewConfirm().Title("Then open a new terminal in the folder?").Value(&openTerm),
	); err != nil {
		return onboardWizardResult{}, err
	}
	if !write {
		return onboardWizardResult{}, fmt.Errorf("onboard cancelled; nothing was written")
	}
	return onboardWizardResult{Answers: normalized, OpenTerminal: openTerm}, nil
}

// reparse runs the wizard's answers through the same parser --from uses, so both paths normalise
// and refuse alike.
func reparse(a *onboard.Answers) (*onboard.Answers, error) {
	raw, err := yaml.Marshal(a)
	if err != nil {
		return nil, err
	}
	return onboard.Parse(raw)
}
