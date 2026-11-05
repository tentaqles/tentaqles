package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/shell"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func newEnvCmd() *cobra.Command {
	var sh string
	var asJSON bool
	c := &cobra.Command{
		Use:   "env",
		Short: "Print the env changes for the current directory (used by the shell hook)",
		Long: "Print the env changes for the current directory (used by the shell hook).\n" +
			"Under TQ_ENABLED=0 (the shell-activation kill switch) it prints nothing.",
		RunE: func(c *cobra.Command, _ []string) error {
			if os.Getenv("TQ_ENABLED") == "0" {
				// The kill switch, whose one meaning is shell activation:
				// the prompt hook is paused, and a direct call changes
				// nothing either.
				return nil
			}
			defer trust.MemoScope()()
			cfg, err := registry.Load()
			if err != nil {
				return err
			}
			cwd, _ := os.Getwd()
			res := resolve.Resolve(cwd, cfg)
			prev := envplan.DecodeState(os.Getenv(envplan.StateVar))
			// guard: v2 adds the PATH and environment layer (ADR-0007): the identity PATH
			// and the out-of-scope variables unset, both restored on leaving.
			senv := envplan.ShellEnv(sh)
			want, maskNotice := envplan.Plan(res.Workspace, prev, senv)
			if p, ok := want.Set["PATH"]; ok && sh == "cmd" && strings.ContainsAny(p, "\"%!") {
				// cmd.exe cannot quote these; its PATH is left as it is rather than the
				// whole line refused.
				delete(want.Set, "PATH")
			}
			ops, _ := envplan.DiffWant(want, senv.Lookup, prev, wsName(res))
			if ops.Changed && ops.From != ops.To {
				_ = audit.Append(audit.Event{Kind: "switch", From: ops.From, To: ops.To, Cwd: cwd, Reason: res.Reason})
			}
			// A workspace without its identity says so at every prompt, the
			// way direnv nags until `direnv allow`, and so does one trusted
			// through a marker of an older tq: one fixed line, the
			// workspace name and the command to run (shell.NoticeText; the
			// changed files are what tq hooks verify prints).
			ops.Notice = envNotice(res)
			if ops.Notice == nil {
				ops.Notice = maskNotice
			}
			if asJSON {
				notice := ""
				if ops.Notice != nil {
					notice, _ = shell.NoticeText(*ops.Notice)
				}
				return json.NewEncoder(c.OutOrStdout()).Encode(map[string]any{"workspace": wsName(res), "reason": res.Reason, "set": ops.Set, "unset": ops.Unset, "notice": notice})
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

// envNotice is the per-prompt notice for the cwd: untrusted (tq allow),
// integrity mismatch (tq hooks verify), or trusted through a legacy marker
// (tq allow signs the hooks). A trusted workspace with a signed record and
// a neutral folder have none.
func envNotice(r resolve.Result) *envplan.Notice {
	switch {
	case r.Workspace != nil && r.Legacy:
		return &envplan.Notice{Kind: envplan.NoticeLegacy, Workspace: r.Workspace.Name}
	case r.Workspace == nil && r.Untrusted != nil && r.Mismatch:
		return &envplan.Notice{Kind: envplan.NoticeMismatch, Workspace: r.Untrusted.Name}
	case r.Workspace == nil && r.Untrusted != nil:
		return &envplan.Notice{Kind: envplan.NoticeUntrusted, Workspace: r.Untrusted.Name}
	}
	return nil
}
