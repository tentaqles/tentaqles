package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/detect"
	"github.com/tentaqles/tentaqles/internal/providers"
	"gopkg.in/yaml.v3"
)

func newProvidersCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "providers",
		Short: "Inspect and manage the provider catalog (CLI, install hints, identity env)",
	}
	c.AddCommand(newProvidersListCmd(), newProvidersShowCmd(), newProvidersCheckCmd(), newProvidersAddCmd())
	return c
}

func newProvidersListCmd() *cobra.Command {
	var category string
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List providers in the catalog",
		RunE: func(c *cobra.Command, _ []string) error {
			cat, err := providers.Load()
			if err != nil {
				return err
			}
			var ps []providers.Provider
			if category != "" {
				ps = cat.ByCategory(category)
			} else {
				ps = cat.All()
			}
			if asJSON {
				return json.NewEncoder(c.OutOrStdout()).Encode(ps)
			}
			tw := tabwriter.NewWriter(c.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tNAME\tCATEGORY\tCLI\tIDENTITY\tSOURCE")
			for _, p := range ps {
				cliCol := "-"
				if p.CLI != nil {
					cliCol = p.CLI.Command
				}
				identity := "no"
				if len(p.Identity.Env) > 0 {
					identity = "yes"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", p.ID, p.Name, p.Category, cliCol, identity, p.Source)
			}
			return tw.Flush()
		},
	}
	c.Flags().StringVar(&category, "category", "", "filter by category")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func newProvidersShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Print a provider's full definition as YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			cat, err := providers.Load()
			if err != nil {
				return err
			}
			p, ok := cat.Get(args[0])
			if !ok {
				return fmt.Errorf("no provider %q", args[0])
			}
			data, err := yaml.Marshal(p)
			if err != nil {
				return err
			}
			_, err = c.OutOrStdout().Write(data)
			return err
		},
	}
}

func newProvidersCheckCmd() *cobra.Command {
	var all bool
	var workspaceName string
	var asJSON bool
	c := &cobra.Command{
		Use:   "check [<id>...]",
		Short: "Probe whether provider CLIs are installed",
		RunE: func(c *cobra.Command, args []string) error {
			cat, err := providers.Load()
			if err != nil {
				return err
			}

			var ids []string
			switch {
			case workspaceName != "":
				ws, err := findWorkspace(workspaceName)
				if err != nil {
					return err
				}
				ids = ws.Manifest.IdentityNames()
			case len(args) > 0:
				ids = args
			default:
				all = true
			}
			if all {
				ids = cat.IDs()
			}

			var ps []providers.Provider
			for _, id := range ids {
				p, ok := cat.Get(id)
				if !ok {
					return fmt.Errorf("no provider %q", id)
				}
				ps = append(ps, p)
			}

			deps := detect.DefaultDeps()
			results := detect.CheckAll(ps, deps)

			if asJSON {
				if err := json.NewEncoder(c.OutOrStdout()).Encode(results); err != nil {
					return err
				}
			} else {
				for i, r := range results {
					p := ps[i]
					switch {
					case p.CLI == nil:
						fmt.Fprintf(c.OutOrStdout(), "[n/a] %s — no CLI\n", r.ID)
					case r.Installed:
						version := r.Version
						if version == "" {
							version = "unknown version"
						}
						fmt.Fprintf(c.OutOrStdout(), "[ok] %s %s (%s)\n", r.ID, version, r.Path)
					default:
						hint := "no install hint available"
						hints := detect.Hints(p, deps.GOOS)
						if len(hints) > 0 && hints[0] != "no CLI to install" {
							hint = hints[0]
						}
						fmt.Fprintf(c.OutOrStdout(), "[missing] %s — not installed → %s\n", r.ID, hint)
					}
				}
			}

			missing := false
			for i, r := range results {
				if ps[i].CLI != nil && !r.Installed {
					missing = true
				}
			}
			if missing {
				exitFunc(1)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "check every provider in the catalog")
	c.Flags().StringVar(&workspaceName, "workspace", "", "check only the providers used by this workspace's identities")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func newProvidersAddCmd() *cobra.Command {
	var name, category, command string
	var versionArgs, envPairs []string
	var loginArgs, verifyArgs, docs string
	var force bool
	c := &cobra.Command{
		Use:   "add <id>",
		Short: "Add or override a provider as a user file under providers/",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id := args[0]

			cat, err := providers.Load()
			if err != nil {
				return err
			}
			if existing, ok := cat.Get(id); ok && !force {
				if existing.Source == "embedded" {
					return fmt.Errorf("provider %q is a built-in (embedded) provider; pass --force to override it with a user file", id)
				}
				return fmt.Errorf("provider %q already has a user file; pass --force to overwrite it", id)
			}

			p := providers.Provider{
				ID:       id,
				Name:     name,
				Category: category,
				Docs:     docs,
			}
			if command != "" {
				p.CLI = &providers.CLI{Command: command, VersionArgs: versionArgs}
			}
			if len(envPairs) > 0 {
				env := make(map[string]string, len(envPairs))
				for _, kv := range envPairs {
					k, v, ok := strings.Cut(kv, "=")
					if !ok {
						return fmt.Errorf("--env %q must be in KEY=VALUE form", kv)
					}
					if err := providers.ValidateEnvPair(k, v); err != nil {
						return err
					}
					env[k] = v
				}
				p.Identity = providers.Identity{Env: env}
			}
			if loginArgs != "" {
				fields, err := splitArgs(loginArgs)
				if err != nil {
					return err
				}
				p.Login = &providers.Cmd{Args: fields}
			}
			if verifyArgs != "" {
				fields, err := splitArgs(verifyArgs)
				if err != nil {
					return err
				}
				p.Verify = &providers.Cmd{Args: fields}
			}

			path, err := providers.WriteUser(p)
			if err != nil {
				return err
			}
			fmt.Fprintln(c.OutOrStdout(), path)
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "", "display name (required)")
	c.Flags().StringVar(&category, "category", "", "one of: cloud, vcs, data, deploy, pm, agent, other (required)")
	c.Flags().StringVar(&command, "command", "", "CLI command name")
	c.Flags().StringSliceVar(&versionArgs, "version-args", nil, "args to print the CLI's version")
	c.Flags().StringArrayVar(&envPairs, "env", nil, "identity env var, KEY=VALUE (repeatable)")
	c.Flags().StringVar(&loginArgs, "login", "", "login command args, e.g. \"auth login\"")
	c.Flags().StringVar(&verifyArgs, "verify", "", "verify command args, e.g. \"auth status\"")
	c.Flags().StringVar(&docs, "docs", "", "docs URL")
	c.Flags().BoolVar(&force, "force", false, "overwrite an embedded provider or an existing user file")
	_ = c.MarkFlagRequired("name")
	_ = c.MarkFlagRequired("category")
	return c
}

// splitArgs splits a shell-like argument string on whitespace. It does not
// support quoting; that's sufficient for the simple login/verify args tq
// providers add expects.
func splitArgs(s string) ([]string, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty argument string")
	}
	return fields, nil
}
