package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func newLoginCmd() *cobra.Command {
	var sso bool
	var ssoSession string
	cmd := &cobra.Command{
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
			loginArgs := p.LoginArgs
			if args[1] == "aws" {
				cfg := awsConfigFile(envplan.Environ(ws, os.Environ()))
				loginArgs, err = awsLoginArgs(cfg, sso, ssoSession)
				if err != nil {
					return err
				}
				if isAWSSSOArgs(loginArgs) {
					fmt.Fprintln(c.ErrOrStderr(), awsSSONote)
				}
			} else if sso || ssoSession != "" {
				return fmt.Errorf("--sso and --sso-session apply to the aws identity only")
			}
			fmt.Fprintf(c.ErrOrStderr(), "tq: running `%s %v` with %s's private config\n", p.LoginCmd, loginArgs, ws.Name)
			return execIn(ws, p.LoginCmd, loginArgs, false, func(s string) { fmt.Fprintln(c.ErrOrStderr(), "warning:", s) })
		},
	}
	cmd.Flags().BoolVar(&sso, "sso", false, "aws only: run `aws configure sso` (first-time SSO setup)")
	cmd.Flags().StringVar(&ssoSession, "sso-session", "", "aws only: run `aws sso login --sso-session NAME`")
	return cmd
}
