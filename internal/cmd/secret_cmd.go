package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/secret"
)

// maxSecretBytes caps a token read from stdin.
const maxSecretBytes = 16 << 10

// readHidden reads one line from a terminal with echo off (a seam for tests).
var readHidden = func(r io.Reader) ([]byte, error) {
	f, ok := r.(*os.File)
	if !ok {
		return nil, errors.New("stdin is not a terminal")
	}
	return term.ReadPassword(f.Fd())
}

// secretHumanOnly refuses agent sessions (the hub's marker check) before anything else runs.
func secretHumanOnly() error {
	if hubAgent() {
		return runner.ErrHumanOnly
	}
	return nil
}

// secretWorkspace checks the names and that the workspace is registered (trusted or not: a token
// can be stored before tq allow).
func secretWorkspace(ws, provider string) (*resolve.Workspace, error) {
	if provider != "" {
		if err := secret.ValidateNames(ws, provider); err != nil {
			return nil, err
		}
	}
	w, err := findWorkspace(ws)
	if err != nil {
		return nil, fmt.Errorf("no workspace %q; see tq list", ws)
	}
	return w, nil
}

func newSecretCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "secret",
		Short: "Keep tracker tokens in the OS keychain (human-only; values are never printed)",
		Long: `Board sync reads a tracker token from, in order: the tracker CLI's own
login, the OS keychain, the manifest's tracker.env (resolved as tq dotenv
does), then the agent's MCP connector. These commands manage the keychain
entry: service "tentaqles", account "<workspace>/<provider>".

  tq secret set <ws> <provider>     store a token (hidden prompt, or stdin)
  tq secret rm <ws> <provider>      remove it
  tq secret status [<ws> [<prov>]]  present or absent, never the value

They are human-only: an agent session is refused.`,
	}
	c.AddCommand(newSecretSetCmd(), newSecretRmCmd(), newSecretStatusCmd())
	return c
}

func newSecretSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <workspace> <provider>",
		Short: "Store a tracker token in the OS keychain, from a hidden prompt or stdin",
		Long: `Reads the token from a hidden prompt on a terminal, or from stdin when it is
piped. The token is stored under service "tentaqles", account
"<workspace>/<provider>", replacing any earlier one. It is never printed.
Providers: ` + strings.Join(manifest.TrackerProviders, ", ") + ".",
		Args: cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			if err := secretHumanOnly(); err != nil {
				return err
			}
			ws, provider := args[0], args[1]
			if _, err := secretWorkspace(ws, provider); err != nil {
				return err
			}
			value, err := readSecretValue(c, ws, provider)
			if err != nil {
				return err
			}
			if err := secret.Set(ws, provider, value); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "Stored the %s token for %s in the OS keychain (service %s, account %s).\n",
				provider, ws, secret.Service, secret.Account(ws, provider))
			return nil
		},
	}
}

// readSecretValue is the token from a hidden prompt (terminal) or stdin (piped). Nothing echoes.
func readSecretValue(c *cobra.Command, ws, provider string) (string, error) {
	in := c.InOrStdin()
	if stdinIsTerminal(in) {
		fmt.Fprintf(c.ErrOrStderr(), "%s token for %s (input hidden): ", provider, ws)
		b, err := readHidden(in)
		fmt.Fprintln(c.ErrOrStderr())
		if err != nil {
			return "", fmt.Errorf("reading the token: %v", err)
		}
		return string(b), nil
	}
	b, err := io.ReadAll(io.LimitReader(in, maxSecretBytes+1))
	if err != nil {
		return "", fmt.Errorf("reading the token from stdin: %v", err)
	}
	if len(b) > maxSecretBytes {
		return "", fmt.Errorf("the token on stdin is longer than %d bytes; nothing was stored", maxSecretBytes)
	}
	return string(b), nil
}

func newSecretRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rm <workspace> <provider>",
		Aliases: []string{"remove", "delete"},
		Short:   "Remove a tracker token from the OS keychain",
		Args:    cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			if err := secretHumanOnly(); err != nil {
				return err
			}
			ws, provider := args[0], args[1]
			if _, err := secretWorkspace(ws, provider); err != nil {
				return err
			}
			if err := secret.Delete(ws, provider); err != nil {
				if errors.Is(err, secret.ErrNotFound) {
					return fmt.Errorf("no token for %s/%s in the OS keychain", ws, provider)
				}
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "Removed the %s token for %s from the OS keychain.\n", provider, ws)
			return nil
		},
	}
}

// secretRow is one status line: presence only, never a value.
type secretRow struct {
	Workspace string `json:"workspace"`
	Provider  string `json:"provider"`
	Account   string `json:"account"`
	Keychain  string `json:"keychain"`       // present | absent | error
	Auth      string `json:"auth,omitempty"` // the manifest's tracker.auth, for the workspace's tracker
	Error     string `json:"error,omitempty"`
}

func secretStatusRows(w resolve.Workspace, only string) []secretRow {
	var provs []string
	var tr *manifest.Tracker
	if w.Manifest != nil {
		tr = w.Manifest.Tracker
	}
	switch {
	case only != "":
		provs = []string{only}
	case tr != nil && tr.Provider != "":
		provs = []string{tr.Provider}
	default:
		provs = manifest.TrackerProviders
	}
	rows := make([]secretRow, 0, len(provs))
	for _, p := range provs {
		r := secretRow{Workspace: w.Name, Provider: p, Account: secret.Account(w.Name, p), Keychain: "absent"}
		if tr != nil && tr.Provider == p {
			r.Auth = tr.AuthOrDefault()
		}
		switch ok, err := secret.Has(w.Name, p); {
		case err != nil:
			r.Keychain, r.Error = "error", err.Error()
		case ok:
			r.Keychain = "present"
		}
		rows = append(rows, r)
	}
	return rows
}

func newSecretStatusCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status [<workspace> [<provider>]]",
		Short: "Show which tracker tokens the OS keychain holds (present or absent, never the value)",
		Long: `With no workspace: every registered workspace that has a tracker block, for
its tracker. With a workspace: its tracker, or every tracker when the
manifest names none. With a provider: just that one.`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			if err := secretHumanOnly(); err != nil {
				return err
			}
			var rows []secretRow
			if len(args) > 0 {
				only := ""
				if len(args) == 2 {
					only = args[1]
				}
				w, err := secretWorkspace(args[0], only)
				if err != nil {
					return err
				}
				rows = secretStatusRows(*w, only)
			} else {
				cfg, err := registry.Load()
				if err != nil {
					return err
				}
				all, _ := resolve.ListWorkspaces(cfg)
				for _, w := range all {
					if w.Manifest.HasTracker() {
						rows = append(rows, secretStatusRows(w, "")...)
					}
				}
			}
			out := c.OutOrStdout()
			if asJSON {
				if rows == nil {
					rows = []secretRow{}
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}
			if len(rows) == 0 {
				fmt.Fprintln(out, "No workspace has a tracker block. Pass a workspace to check every tracker.")
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "WORKSPACE\tPROVIDER\tKEYCHAIN\tAUTH")
			for _, r := range rows {
				state := r.Keychain
				if r.Error != "" {
					state += " (" + r.Error + ")"
				}
				auth := r.Auth
				if auth == "" {
					auth = "-"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Workspace, r.Provider, state, auth)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Fprintln(out, "\nValues are never printed. Store one with: tq secret set <workspace> <provider>")
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the rows as JSON")
	return c
}
