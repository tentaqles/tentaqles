package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/cli/internal/envplan"
	"github.com/tentaqles/tentaqles/cli/internal/trust"
)

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login <workspace> <identity>",
		Short: "Run a CLI's own login flow inside the workspace's private config home",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			ws, err := findWorkspace(args[0])
			if err != nil {
				return err
			}
			if !trust.IsTrusted(ws.Hash) {
				return fmt.Errorf("workspace %s is untrusted; run: tq allow %s", ws.Name, ws.Name)
			}
			p, ok := envplan.Providers()[args[1]]
			if !ok || p.LoginCmd == "" {
				return fmt.Errorf("no login flow for identity %q", args[1])
			}
			fmt.Fprintf(c.ErrOrStderr(), "tq: running `%s %v` with %s's private config\n", p.LoginCmd, p.LoginArgs, ws.Name)
			return execIn(ws, p.LoginCmd, p.LoginArgs, false, func(s string) { fmt.Fprintln(c.ErrOrStderr(), "warning:", s) })
		},
	}
}
