package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/redact"
)

// defaultRedactClasses are the manifest's declared classes minus prompts.allow,
// plus secrets; "all" outside a classified workspace.
func defaultRedactClasses(cwd string) []string {
	ws, _ := resolveTrusted(cwd)
	if ws == nil || ws.Manifest == nil || !ws.Manifest.Data.Enabled() {
		return []string{"all"}
	}
	allow := map[string]bool{}
	for _, c := range ws.Manifest.Data.Prompts.Allow {
		allow[c] = true
	}
	var out []string
	for _, c := range ws.Manifest.Data.Classes {
		if !allow[c] && c != "public" && c != "internal" {
			out = append(out, c)
		}
	}
	return append(out, "secrets")
}

func newRedactCmd() *cobra.Command {
	var classesFlag string
	var files []string
	var count bool
	c := &cobra.Command{
		Use:   "redact [--classes C] [--file F]... [--count]",
		Short: "Mask personal, financial and secret values in text (stdin or files) by data class",
		Long: `Reads stdin (or --file) and writes the text with CPF, CNPJ, e-mail, phone,
card and IBAN values (validated by checksum where one exists) and secrets
replaced by [REDACTED:<kind>]. Classes default to the workspace manifest's
data.classes minus data.prompts.allow, plus secrets; outside a classified
workspace every detector runs.

  tq redact --classes pii < export.csv
  tq redact run --classes pii,financial -- psql "$DATABASE_URL" -c "select * from customers limit 5"

Reading personal data locally is allowed; the guard refuses to record it in
commits, pushes and PR or issue text. Use this command to make a shareable
copy (a bug report, a PR body) without the values.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cwd, _ := os.Getwd()
			classes, err := resolveClasses(classesFlag, cwd)
			if err != nil {
				return err
			}
			var in io.Reader = c.InOrStdin()
			if len(files) > 0 {
				var rs []io.Reader
				for _, f := range files {
					fh, err := os.Open(f)
					if err != nil {
						return err
					}
					defer fh.Close()
					rs = append(rs, fh)
				}
				in = io.MultiReader(rs...)
			}
			raw, err := io.ReadAll(in)
			if err != nil {
				return err
			}
			if count {
				n := redact.Count(string(raw), classes)
				keys := make([]string, 0, len(n))
				for k := range n {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					fmt.Fprintf(c.OutOrStdout(), "%s: %d\n", k, n[k])
				}
				if len(keys) == 0 {
					fmt.Fprintln(c.OutOrStdout(), "no findings")
				}
				return nil
			}
			_, err = io.WriteString(c.OutOrStdout(), redact.Redact(string(raw), classes))
			return err
		},
	}
	c.Flags().StringVar(&classesFlag, "classes", "", "comma-separated data classes: pii, financial, sensitive, children, secrets, all")
	c.Flags().StringArrayVar(&files, "file", nil, "file to read instead of stdin (repeatable)")
	c.Flags().BoolVar(&count, "count", false, "print findings per detector instead of the masked text")
	c.AddCommand(newRedactRunCmd())
	return c
}

func resolveClasses(flag, cwd string) ([]string, error) {
	if strings.TrimSpace(flag) != "" {
		return redact.ParseClasses(flag)
	}
	return defaultRedactClasses(cwd), nil
}

func newRedactRunCmd() *cobra.Command {
	var classesFlag string
	c := &cobra.Command{
		Use:   "run [--classes C] -- CMD [ARGS...]",
		Short: "Run a command with its stdout and stderr masked by data class",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			classes, err := resolveClasses(classesFlag, cwd)
			if err != nil {
				return err
			}
			path, err := exec.LookPath(args[0])
			if err != nil {
				return err
			}
			stdout := redact.NewWriter(c.OutOrStdout(), classes)
			stderr := redact.NewWriter(c.ErrOrStderr(), classes)
			cmd := exec.Command(path, args[1:]...)
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
	c.Flags().StringVar(&classesFlag, "classes", "", "comma-separated data classes (default: from the manifest, plus secrets)")
	c.Flags().SetInterspersed(false)
	return c
}
