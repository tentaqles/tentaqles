package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/cli/internal/hooks"
)

func shellArgs(args []string, allDetected bool, profiles hooks.Profiles) []hooks.Shell {
	if allDetected {
		return hooks.Detect(profiles, hooks.LookPath)
	}
	shells := make([]hooks.Shell, 0, len(args))
	for _, a := range args {
		shells = append(shells, hooks.Shell(a))
	}
	return shells
}

func newHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Manage tq's shell activation hook in your profile files",
	}
	cmd.AddCommand(newHooksStatusCmd(), newHooksInstallCmd(), newHooksRemoveCmd())
	return cmd
}

func newHooksStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show hook install status for each known shell",
		RunE: func(c *cobra.Command, args []string) error {
			profiles := hooks.ProfilesFn()
			w := c.OutOrStdout()
			for _, sh := range hooks.Shells {
				st := hooks.StatusOf(sh, profiles)
				fmt.Fprintf(w, "%-12s %-24s %s\n", sh, st.Profile, st.State)
			}
			return nil
		},
	}
}

func newHooksInstallCmd() *cobra.Command {
	var allDetected bool
	cmd := &cobra.Command{
		Use:   "install [shell...]",
		Short: "Install tq's activation block into shell profile(s)",
		RunE: func(c *cobra.Command, args []string) error {
			profiles := hooks.ProfilesFn()
			shells := shellArgs(args, allDetected, profiles)
			if len(shells) == 0 {
				return fmt.Errorf("specify one or more shells, or pass --all-detected")
			}
			w := c.OutOrStdout()
			for _, sh := range shells {
				st, err := hooks.Install(sh, profiles)
				if err != nil {
					return err
				}
				fmt.Fprintf(w, "%-12s %-24s %s\n", sh, st.Profile, st.State)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&allDetected, "all-detected", false, "install for every detected shell")
	return cmd
}

func newHooksRemoveCmd() *cobra.Command {
	var allDetected bool
	cmd := &cobra.Command{
		Use:   "remove [shell...]",
		Short: "Remove tq's activation block from shell profile(s)",
		RunE: func(c *cobra.Command, args []string) error {
			profiles := hooks.ProfilesFn()
			shells := shellArgs(args, allDetected, profiles)
			if len(shells) == 0 {
				return fmt.Errorf("specify one or more shells, or pass --all-detected")
			}
			w := c.OutOrStdout()
			for _, sh := range shells {
				st, err := hooks.Remove(sh, profiles)
				if err != nil {
					return err
				}
				fmt.Fprintf(w, "%-12s %-24s %s\n", sh, st.Profile, st.State)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&allDetected, "all-detected", false, "remove for every detected shell")
	return cmd
}
