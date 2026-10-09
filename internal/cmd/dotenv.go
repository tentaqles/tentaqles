package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/dotenv"
)

func newDotenvCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "dotenv",
		Short: "Use a .env file without printing its values",
		Long: `Agent-safe .env access. Printing a .env (cat, Get-Content, grep) puts its
secrets in the session transcript; these commands never do:

  tq dotenv keys [FILE...]              names only, and whether each is set
  tq dotenv keys --require A --require B   exit 1 if any is missing or empty
  tq dotenv run [--file F]... -- CMD    run CMD with the file loaded; every
                                        loaded value is masked in its output

FILE defaults to .env in the current directory.`,
	}
	c.AddCommand(newDotenvKeysCmd(), newDotenvRunCmd())
	return c
}

func dotenvFiles(args []string) ([]string, error) {
	if len(args) == 0 {
		return []string{".env"}, nil
	}
	for _, f := range args {
		if !dotenv.IsEnvFile(f) {
			return nil, fmt.Errorf("%s is not a .env file (.env, .env.<name> or <name>.env)", f)
		}
	}
	return args, nil
}

func newDotenvKeysCmd() *cobra.Command {
	var require []string
	c := &cobra.Command{
		Use:   "keys [FILE...]",
		Short: "List the variable names in .env files (never the values)",
		RunE: func(c *cobra.Command, args []string) error {
			out := c.OutOrStdout()
			state := map[string]string{} // key -> set|empty (last file wins)
			files, err := dotenvFiles(args)
			if err != nil {
				return err
			}
			for _, f := range files {
				es, err := dotenv.Parse(f)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "# %s (%d keys)\n", f, len(es))
				seen := map[string]int{}
				for _, e := range es {
					s := "set"
					if e.Value == "" {
						s = "empty"
					}
					if prev, dup := seen[e.Key]; dup {
						s += fmt.Sprintf(" (duplicate of line %d; this one wins)", prev)
					}
					seen[e.Key] = e.Line
					state[e.Key] = strings.Fields(s)[0]
					fmt.Fprintf(out, "%s\t%s\n", e.Key, s)
				}
			}
			var missing []string
			for _, k := range require {
				if state[k] != "set" {
					missing = append(missing, k)
				}
			}
			if len(missing) > 0 {
				sort.Strings(missing)
				fmt.Fprintf(c.ErrOrStderr(), "missing or empty: %s\n", strings.Join(missing, ", "))
				exitFunc(1)
			}
			return nil
		},
	}
	c.Flags().StringArrayVar(&require, "require", nil, "fail (exit 1) unless this key is set (repeatable)")
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

CMD is run directly, not through a shell; use bash -c / pwsh -Command for
pipelines.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			files, err := dotenvFiles(files)
			if err != nil {
				return err
			}
			loaded := map[string]string{}
			for _, f := range files {
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
