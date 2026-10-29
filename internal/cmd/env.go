package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/shell"
)

func newEnvCmd() *cobra.Command {
	var sh string
	var asJSON, off, on bool
	c := &cobra.Command{
		Use:   "env",
		Short: "Print the env changes for the current directory (used by the shell hook)",
		Long: "Print the env changes for the current directory (used by the shell hook).\n" +
			"Under TQ_MODE=off or TQ_ENABLED=0 it prints nothing. --off prints the code of `tq off` (identity\n" +
			"cleared, TQ_MODE=off; human only), --on the code of `tq on` (TQ_MODE unset).",
		RunE: func(c *cobra.Command, _ []string) error {
			if off && on {
				return errors.New("--off and --on are exclusive")
			}
			if off || on {
				var ops envplan.Ops
				if off {
					if err := refuseAgent("tq env --off"); err != nil {
						return err
					}
					ops = envplan.OffOps(os.LookupEnv)
				} else {
					ops = envplan.OnOps()
				}
				if asJSON {
					return json.NewEncoder(c.OutOrStdout()).Encode(map[string]any{"set": ops.Set, "unset": ops.Unset})
				}
				out, err := shell.Emit(sh, ops)
				if err != nil {
					return err
				}
				_, err = fmt.Fprint(c.OutOrStdout(), out)
				return err
			}
			if envOff() || os.Getenv("TQ_ENABLED") == "0" {
				// tq off in this window, or the legacy kill switch (whose one
				// meaning is shell activation): the prompt hook is paused, and
				// a direct call changes nothing either.
				return nil
			}
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
	c.Flags().BoolVar(&off, "off", false, "print the code of `tq off`: clear the identity, set TQ_MODE=off (human only)")
	c.Flags().BoolVar(&on, "on", false, "print the code of `tq on`: unset TQ_MODE")
	return c
}

func wsName(r resolve.Result) string {
	if r.Workspace == nil {
		return ""
	}
	return r.Workspace.Name
}
