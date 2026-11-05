package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/humanonly"
)

// tq settings anchors: the terminal processes the human-only check accepts
// as a session root (ADR-0003). The built-in list covers the common
// terminals, multiplexers, session managers and editors; an unlisted
// terminal is added by name, once, in ~/.tentaqles/anchors.yaml.
func newSettingsAnchorsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "anchors",
		Short: "List the terminal processes the human-only check accepts as a session root (add and remove are human only)",
		Long: `A human-only command refuses unless its process tree reaches an anchor: a
process only a human session starts (a terminal emulator, a login shell, the
desktop shell, sshd, tmux, an editor's integrated terminal). When a refusal
says "cannot establish a human ancestry" and names your terminal, add it:

  tq settings anchors add <exe-name>

Anchors are stored in ~/.tentaqles/anchors.yaml and matched by executable
name (case and .exe do not matter). A shell, a runtime or a harness is never
accepted as an anchor.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			out := c.OutOrStdout()
			yours, err := humanonly.LoadUserAnchors()
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "yours (%s): ", humanonly.UserAnchorsPath())
			if len(yours) == 0 {
				fmt.Fprintln(out, "(none)")
			} else {
				fmt.Fprintln(out, strings.Join(yours, ", "))
			}
			fmt.Fprintf(out, "built-in: %s\n", strings.Join(humanonly.BuiltinAnchors(), ", "))
			fmt.Fprintln(out, "also: a login shell (-bash, zsh -l), systemd --user, WSL's init, a JetBrains IDE running as java")
			return nil
		},
	}
	c.AddCommand(newSettingsAnchorsAddCmd(), newSettingsAnchorsRemoveCmd())
	return c
}

func newSettingsAnchorsAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <exe-name>",
		Short: "Accept a terminal process by name as a session root (human only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := refuseAgent("tq settings anchors add"); err != nil {
				return err
			}
			name, err := humanonly.ValidateAnchor(args[0])
			if err != nil {
				return err
			}
			about := fmt.Sprintf("tq settings anchors add: about to add %s to %s (a process by this name will count as a terminal for the human-only check)", name, humanonly.UserAnchorsPath())
			if err := humanGate(c, "tq settings anchors add", about); err != nil {
				return err
			}
			name, added, err := humanonly.AddUserAnchor(args[0])
			if err != nil {
				return err
			}
			if !added {
				fmt.Fprintf(c.OutOrStdout(), "%s is already an anchor (%s)\n", name, humanonly.UserAnchorsPath())
				return nil
			}
			fmt.Fprintf(c.OutOrStdout(), "Added %s to %s\n", name, humanonly.UserAnchorsPath())
			return nil
		},
	}
}

func newSettingsAnchorsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <exe-name>",
		Aliases: []string{"rm"},
		Short:   "Stop accepting a terminal process you added (human only)",
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := refuseAgent("tq settings anchors remove"); err != nil {
				return err
			}
			name := humanonly.NormalizeAnchor(args[0])
			about := fmt.Sprintf("tq settings anchors remove: about to remove %s from %s", name, humanonly.UserAnchorsPath())
			if err := humanGate(c, "tq settings anchors remove", about); err != nil {
				return err
			}
			name, removed, err := humanonly.RemoveUserAnchor(args[0])
			if err != nil {
				return err
			}
			if !removed {
				return fmt.Errorf("%s is not in %s (built-in anchors cannot be removed)", name, humanonly.UserAnchorsPath())
			}
			fmt.Fprintf(c.OutOrStdout(), "Removed %s from %s\n", name, humanonly.UserAnchorsPath())
			return nil
		},
	}
}
