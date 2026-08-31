package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/registry"
)

var exitFunc = os.Exit

func newDoctorCmd() *cobra.Command {
	var asJSON bool
	var verifyMode string
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Verify hooks, trust, git and env against the manifests (never mutates)",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := registry.Load()
			if err != nil {
				return err
			}
			cwd, _ := os.Getwd()
			switch verifyMode {
			case doctor.VerifyAuto, doctor.VerifyAll, doctor.VerifyOff:
			default:
				return fmt.Errorf("--verify must be %s, %s or %s", doctor.VerifyAuto, doctor.VerifyAll, doctor.VerifyOff)
			}
			fs := doctor.Run(cfg, doctor.Deps{
				Env: os.LookupEnv, Cwd: cwd,
				RunGit: gitcfg.RunGit, RunGitIn: gitcfg.RunGitIn, LookPath: exec.LookPath,
				RunCLI: runVerifyCmd, VerifyMode: verifyMode,
			})
			if asJSON {
				if err := json.NewEncoder(c.OutOrStdout()).Encode(fs); err != nil {
					return err
				}
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
				exitFunc(1)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	c.Flags().StringVar(&verifyMode, "verify", doctor.VerifyAuto,
		"ask each CLI which account is signed in: auto (only where the manifest declares an expectation, or where tq has no cheaper way to tell), all, or off")
	return c
}

// runVerifyCmd runs a provider's verify command with a workspace's identity in
// env. Output is returned even when the command fails: several of these CLIs
// exit non-zero precisely because they are logged out, and that text is the
// answer rather than an error.
func runVerifyCmd(ctx context.Context, env []string, name string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	return string(out), err
}
