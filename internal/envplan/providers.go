package envplan

import "github.com/tentaqles/tentaqles/internal/providers"

// Provider describes how one CLI is pointed at a private config home.
type Provider struct {
	Name      string
	Vars      func(dir string) map[string]string
	LoginCmd  string // executable to run for `tq login`
	LoginArgs []string
}

// Providers is the single place the env-var mapping lives, built from the
// provider catalog (internal/providers). Only providers that define identity
// env vars are included. Keep manifest.KnownIdentities in sync (it is
// derived from the same catalog).
func Providers() map[string]Provider {
	out := make(map[string]Provider)
	for _, p := range providers.MustLoad().All() {
		if !p.HasIdentity() {
			continue
		}
		loginCmd := ""
		var loginArgs []string
		if p.Login != nil && p.Login.Command != "" {
			loginCmd = p.Login.Command
			loginArgs = p.Login.Args
		} else if p.CLI != nil {
			loginCmd = p.CLI.Command
			if p.Login != nil {
				loginArgs = p.Login.Args
			}
		}
		out[p.ID] = Provider{
			Name:      p.ID,
			Vars:      p.Vars,
			LoginCmd:  loginCmd,
			LoginArgs: loginArgs,
		}
	}
	return out
}
