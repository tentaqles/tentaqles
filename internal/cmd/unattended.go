package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/setup"
)

// `tq unattended` is the deprecated alias layer of `tq mode` (spec E §11.1):
//
//	tq unattended on [--project D]   = tq mode set relaxed [--project D]
//	tq unattended off [--project D]  = tq mode set default (for the scope)
//	tq unattended status             = tq mode
//	tq unattended log                = tq mode log
//
// Each prints a deprecation line to stderr. on and off are human-only, like
// tq mode set.
func newUnattendedCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "unattended",
		Short: "Deprecated: use tq mode (relaxed replaces unattended mode)",
		Long: `Deprecated: unattended mode is now the relaxed guard mode. See tq mode --help.

  tq unattended on [--project D]   is tq mode set relaxed [--project D]
  tq unattended off [--project D]  is tq mode set default (for that scope)
  tq unattended status             is tq mode
  tq unattended log                is tq mode log`,
	}
	var ws, project, sandbox, since string
	on := &cobra.Command{
		Use: "on", Short: "Deprecated: tq mode set relaxed", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := requireHuman(c.InOrStdin(), c.OutOrStdout(), "tq unattended on"); err != nil {
				return err
			}
			deprecated(c, "mode set relaxed", project)
			if sandbox != "" && !validSandbox(sandbox) {
				return fmt.Errorf("--codex-sandbox must be one of %s, got %q", strings.Join(manifest.CodexSandboxes, "|"), sandbox)
			}
			w, sc, err := modeScopeFlags{ws: ws, project: project}.target()
			if err != nil {
				return err
			}
			if sandbox != "" {
				if _, err := setup.ApplyGuardOp(w, setup.GuardScope{}, setup.GuardOp{Kind: setup.GuardOpCodexSandbox, Sandbox: sandbox}); err != nil {
					return err
				}
				if w, err = guardTarget(w.Name, false); err != nil {
					return err
				}
			}
			res, err := setup.ApplyGuardOp(w, sc, setup.GuardOp{Kind: setup.GuardOpMode, Mode: "relaxed"})
			if err != nil {
				return err
			}
			return reportModeWrite(c, w, res, "mode relaxed")
		},
	}
	on.Flags().StringVar(&project, "project", "", "a folder inside the workspace (default: the whole workspace)")
	on.Flags().StringVar(&sandbox, "codex-sandbox", "", "Codex sandbox mode: "+strings.Join(manifest.CodexSandboxes, "|"))
	off := &cobra.Command{
		Use: "off", Short: "Deprecated: tq mode set default", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := requireHuman(c.InOrStdin(), c.OutOrStdout(), "tq unattended off"); err != nil {
				return err
			}
			deprecated(c, "mode set default", project)
			return modeWrite(c, modeScopeFlags{ws: ws, project: project}, setup.GuardOp{Kind: setup.GuardOpUnattendedOff}, "mode default")
		},
	}
	off.Flags().StringVar(&project, "project", "", "only this folder (default: the workspace and every relaxed project)")
	status := &cobra.Command{
		Use: "status", Short: "Deprecated: tq mode", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			deprecated(c, "mode", "")
			name := ws
			if name == "" {
				w, err := guardTarget("", false)
				if err != nil {
					return err
				}
				name = w.Name
			}
			return modeShow(c, name, false, hookAgentClaude)
		},
	}
	logc := &cobra.Command{
		Use: "log", Short: "Deprecated: tq mode log", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			deprecated(c, "mode log", "")
			return modeLog(c, ws, since)
		},
	}
	logc.Flags().StringVar(&since, "since", "24h", "how far back to list")
	for _, s := range []*cobra.Command{on, off, status, logc} {
		s.Flags().StringVar(&ws, "ws", "", "workspace (default: the current folder's)")
	}
	c.AddCommand(on, off, status, logc)
	return c
}

// deprecated prints the tq mode command that replaces an unattended verb.
func deprecated(c *cobra.Command, verb, project string) {
	line := "deprecated: use tq " + verb
	if project != "" {
		line += " --project " + project
	}
	fmt.Fprintln(c.ErrOrStderr(), line)
}

func validSandbox(s string) bool {
	for _, v := range manifest.CodexSandboxes {
		if v == s {
			return true
		}
	}
	return false
}
