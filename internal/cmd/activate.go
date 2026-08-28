package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/shell"
)

func newActivateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "activate <shell>",
		Short: "Print the hook to add to your shell profile, e.g. eval \"$(tq activate bash)\"",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			h, err := shell.Hook(args[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(c.OutOrStdout(), h)
			return err
		},
	}
}
