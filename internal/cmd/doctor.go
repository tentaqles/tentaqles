package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/cli/internal/doctor"
	"github.com/tentaqles/tentaqles/cli/internal/gitcfg"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
)

func newDoctorCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Verify hooks, trust, git and env against the manifests (never mutates)",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := registry.Load()
			if err != nil {
				return err
			}
			cwd, _ := os.Getwd()
			fs := doctor.Run(cfg, doctor.Deps{Env: os.LookupEnv, Cwd: cwd, RunGit: gitcfg.RunGit, LookPath: exec.LookPath})
			if asJSON {
				json.NewEncoder(c.OutOrStdout()).Encode(fs)
			} else {
				for _, f := range fs {
					line := fmt.Sprintf("[%s] %s", f.Level, f.Msg)
					if f.Workspace != "" {
						line = fmt.Sprintf("[%s] %s: %s", f.Level, f.Workspace, f.Msg)
					}
					if f.Fix != "" {
						line += "  → " + f.Fix
					}
					fmt.Fprintln(c.OutOrStdout(), line)
				}
			}
			if doctor.Exit(fs) != 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}
