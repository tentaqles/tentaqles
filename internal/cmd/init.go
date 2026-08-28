package cmd

import (
	"fmt"
	"io"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/hooks"
	"github.com/tentaqles/tentaqles/internal/registry"
)

func newInitCmd() *cobra.Command {
	var installHook bool
	cmd := &cobra.Command{
		Use:   "init <base-folder>",
		Short: "Register a base folder; each first-level subfolder becomes a terminal identity",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			cfg, err := registry.Load()
			if err != nil {
				return err
			}
			added, err := cfg.AddBase(args[0])
			if err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			if err := gitcfg.EnsureGlobal(gitcfg.RunGit); err != nil {
				return err
			}
			w := c.OutOrStdout()
			if added {
				fmt.Fprintf(w, "Registered base: %s\n", args[0])
			} else {
				fmt.Fprintf(w, "Base already registered: %s\n", args[0])
			}
			fmt.Fprintln(w, "Git: global user.useConfigOnly=true and include of ~/.gitconfig-tentaqles ensured.")
			installed := 0
			if installHook {
				profiles := hooks.ProfilesFn()
				shells := hooks.Detect(profiles, hooks.LookPath)
				fmt.Fprintln(w, "\nInstalling shell hooks:")
				for _, sh := range shells {
					st, err := hooks.Install(sh, profiles)
					if err != nil {
						return err
					}
					if st.State == "installed" {
						installed++
					}
					fmt.Fprintf(w, "  %-12s %-24s %s\n", sh, st.Profile, st.State)
				}
			}

			// The copy-paste instructions only help when tq did not wire the
			// hook up itself.
			if installed == 0 {
				printManualHookLines(w)
			}

			fmt.Fprintln(w, "\nNext: tq add <name> --git-email you@client.com --identities claude,gh")
			return nil
		},
	}
	cmd.Flags().BoolVar(&installHook, "install-hook", false, "install tq's activation hook into detected shell profiles")
	return cmd
}

// printManualHookLines prints the copy-paste activation line for each
// supported shell.
func printManualHookLines(w io.Writer) {
	fmt.Fprintln(w, "\nAdd ONE of these lines to your shell profile, then open a new terminal:")
	fmt.Fprintln(w, `  bash  (~/.bashrc):                    eval "$(tq activate bash)"`)
	fmt.Fprintln(w, `  zsh   (~/.zshrc):                     eval "$(tq activate zsh)"`)
	fmt.Fprintln(w, `  fish  (~/.config/fish/config.fish):   tq activate fish | source`)
	if runtime.GOOS == "windows" {
		fmt.Fprintln(w, `  pwsh  ($PROFILE):                     tq activate pwsh | Out-String | Invoke-Expression`)
		fmt.Fprintln(w, `  PS5.1 ($PROFILE):                     tq activate powershell | Out-String | Invoke-Expression`)
	}
}
