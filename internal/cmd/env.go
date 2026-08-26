package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/cli/internal/audit"
	"github.com/tentaqles/tentaqles/cli/internal/envplan"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
	"github.com/tentaqles/tentaqles/cli/internal/resolve"
	"github.com/tentaqles/tentaqles/cli/internal/shell"
)

func newEnvCmd() *cobra.Command {
	var sh string
	var asJSON bool
	c := &cobra.Command{
		Use:   "env",
		Short: "Print the env changes for the current directory (used by the shell hook)",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := registry.Load()
			if err != nil {
				return err
			}
			cwd, _ := os.Getwd()
			res := resolve.Resolve(cwd, cfg)
			prev := envplan.DecodeState(os.Getenv(envplan.StateVar))
			ops, _ := envplan.Diff(envplan.Desired(res.Workspace), os.LookupEnv, prev, wsName(res))
			if ops.Changed && ops.From != ops.To {
				_ = audit.Append(audit.Event{Kind: "switch", From: ops.From, To: ops.To, Cwd: cwd, Reason: res.Reason})
			}
			if asJSON {
				return json.NewEncoder(c.OutOrStdout()).Encode(map[string]any{"workspace": wsName(res), "reason": res.Reason, "set": ops.Set, "unset": ops.Unset})
			}
			out, err := shell.Emit(sh, ops)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(c.OutOrStdout(), out)
			return err
		},
	}
	c.Flags().StringVar(&sh, "shell", "bash", "one of "+fmt.Sprint(shell.Shells))
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON instead of shell code")
	return c
}

func wsName(r resolve.Result) string {
	if r.Workspace == nil {
		return ""
	}
	return r.Workspace.Name
}
