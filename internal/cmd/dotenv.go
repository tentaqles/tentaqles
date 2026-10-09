package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/dotenv"
)

func newDotenvCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "dotenv",
		Short: "Use a .env file without printing its values",
		Long: `Agent-safe .env access. Printing a .env (cat, Get-Content, grep, Read)
puts its secrets in the session transcript, so tq's guard refuses it and
points here instead:

  tq dotenv run [--file F]... -- CMD [ARGS...]

CMD runs with the file's variables in its environment; reference them as
$NAME. Every loaded value is masked in CMD's output.`,
	}
	c.AddCommand(newDotenvRunCmd())
	return c
}

func newDotenvRunCmd() *cobra.Command {
	var files []string
	var override bool
	c := &cobra.Command{
		Use:   "run [--file F]... -- CMD [ARGS...]",
		Short: "Run a command with .env files loaded, masking their values in its output",
		Long: `Loads each --file (default .env) into the environment of CMD and runs it.
Variables already set in the environment win unless --override. Every
loaded value of 6+ characters is replaced by [REDACTED:NAME] in CMD's
stdout and stderr, so a command that echoes a secret does not leak it.

CMD is run directly, not through a shell; use bash -c / pwsh -Command when
the command needs $NAME expansion or a pipeline:

  tq dotenv run -- bash -c 'psql "$DATABASE_URL" -c "select 1"'`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if len(files) == 0 {
				files = []string{".env"}
			}
			loaded := map[string]string{}
			for _, f := range files {
				if !dotenv.IsEnvFile(f) {
					return fmt.Errorf("%s is not a .env file (.env, .env.<name> or <name>.env)", f)
				}
				es, err := dotenv.Parse(f)
				if err != nil {
					return err
				}
				for _, e := range es {
					loaded[e.Key] = e.Value
				}
			}
			env := os.Environ()
			masked := map[string]string{}
			for k, v := range loaded {
				if _, set := os.LookupEnv(k); set && !override {
					continue
				}
				env = append(env, k+"="+v)
				masked[k] = v
			}
			path, err := exec.LookPath(args[0])
			if err != nil {
				return err
			}
			stdout := dotenv.NewMasker(c.OutOrStdout(), masked)
			stderr := dotenv.NewMasker(c.ErrOrStderr(), masked)
			cmd := exec.Command(path, args[1:]...)
			cmd.Env = env
			cmd.Stdin = c.InOrStdin()
			cmd.Stdout = stdout
			cmd.Stderr = stderr
			runErr := cmd.Run()
			_ = stdout.Close()
			_ = stderr.Close()
			var exit *exec.ExitError
			if errors.As(runErr, &exit) {
				exitFunc(exit.ExitCode())
				return nil
			}
			return runErr
		},
	}
	c.Flags().StringArrayVar(&files, "file", nil, "dotenv file to load (repeatable; default .env)")
	c.Flags().BoolVar(&override, "override", false, "let file values replace variables already in the environment")
	c.Flags().SetInterspersed(false)
	return c
}
