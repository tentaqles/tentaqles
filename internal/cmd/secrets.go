package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/secrets"
)

func newSecretsCmd() *cobra.Command {
	c := &cobra.Command{Use: "secrets", Short: "Find where secrets sit in agent config (read-only)"}
	c.AddCommand(newSecretsAuditCmd())
	return c
}

func newSecretsAuditCmd() *cobra.Command {
	var asJSON bool
	var extra []string
	c := &cobra.Command{
		Use:   "audit",
		Short: "List agent-config files holding secret-shaped values, and .env files git would commit",
		Long: `Walks every registered workspace base, every tq identity dir, ~/.claude and
~/.claude.json, and the bundle catalog. It reports:

  - secret-shaped values in .mcp.json, settings*.json, .claude.json and
    catalog.yaml (path, line and detector name — never the value);
  - .env files inside a git repo that are not gitignored.

It only reads. Nothing is moved, rewritten or rotated.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			opts := secrets.AuditOptions{IsIgnored: secrets.GitIgnored}
			if cfg, err := registry.Load(); err == nil {
				opts.Roots = append(opts.Roots, cfg.Bases...)
			}
			opts.Roots = append(opts.Roots, paths.IdentitiesRoot())
			opts.Roots = append(opts.Roots, extra...)
			if home, err := os.UserHomeDir(); err == nil {
				opts.Roots = append(opts.Roots, filepath.Join(home, ".claude"))
				opts.Files = append(opts.Files, filepath.Join(home, ".claude.json"))
			}
			opts.Files = append(opts.Files, paths.Catalog())

			hits := secrets.Audit(opts)
			out := c.OutOrStdout()
			if asJSON {
				type row struct {
					Path    string `json:"path"`
					Line    int    `json:"line,omitempty"`
					Pattern string `json:"pattern"`
				}
				rows := make([]row, 0, len(hits))
				for _, h := range hits {
					rows = append(rows, row{h.Path, h.Line, h.Pattern})
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}
			files := map[string]bool{}
			for _, h := range hits {
				files[h.Path] = true
				if h.Line > 0 {
					fmt.Fprintf(out, "%s:%d  %s\n", h.Path, h.Line, h.Pattern)
				} else {
					fmt.Fprintf(out, "%s  %s\n", h.Path, h.Pattern)
				}
			}
			fmt.Fprintf(out, "\n%d finding(s) in %d file(s). Values are never printed.\n", len(hits), len(files))
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print findings as JSON")
	c.Flags().StringSliceVar(&extra, "root", nil, "extra directory to walk (repeatable)")
	return c
}
