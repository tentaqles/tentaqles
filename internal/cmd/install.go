package cmd

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/install"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// installRegistryPath is the Windows PATH read from the registry after an install; a seam for
// tests, which never read the real registry.
var installRegistryPath = install.RegistryPath

// providerArgRe is what a provider id looks like (providers.Provider.Validate); anything else
// is refused before the catalog is read.
var providerArgRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// newInstallCmd is `tentaqles install <provider>` (ADR-0015): the CLI of a provider in the
// current identity's scope, through the first command-line package manager of the embedded
// catalog that is on the identity's PATH. It is not human-only: an agent may run it for a
// provider the identity names, and the scope check is the command's own.
func newInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install <provider>",
		Short: "Install a provider's CLI through a package manager the catalog names (winget, scoop, brew, apt, npm, pip)",
		Long: "Install the CLI of a provider in this identity's scope. The catalog's install methods are tried in\n" +
			"the order winget, scoop, brew, apt, npm, pip, and the first whose package manager is on the PATH\n" +
			"runs (ten minutes at most, then it is stopped). A provider that already resolves on the identity\n" +
			"PATH runs nothing. A desktop installer is never run by tq: with no usable method the line says\n" +
			"where to download it. A provider outside the identity's scope is refused and logged.\n" +
			"Takes the provider id and nothing else.",
		Example: "  tentaqles install az",
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			defer trust.MemoScope()()
			ws, err := cwdWorkspace()
			if err != nil {
				return err
			}
			if ws == nil || ws.Manifest == nil {
				return errors.New("tentaqles install needs a client identity: this folder is not inside a trusted client (a person runs tentaqles reach to make one)")
			}
			id := args[0]
			if !providerArgRe.MatchString(id) {
				return fmt.Errorf("unknown provider %q: tentaqles install takes a provider id of the catalog (tentaqles providers list)", id)
			}
			// The catalog entry is the embedded one: a user provider file can add a provider
			// or extend what the guard denies, never name a package tq runs (ADR-0007, ADR-0015).
			cat, err := providers.Embedded()
			if err != nil {
				return err
			}
			p, ok := cat.Get(id)
			if !ok {
				return fmt.Errorf("unknown provider %q: it is not in the built-in provider catalog (tentaqles providers list)", id)
			}
			if !slices.Contains(ws.Manifest.Scope(), id) {
				msg := fmt.Sprintf("%s is outside the scope of identity %s: %s", id, ws.Name, identityScope(ws.Manifest))
				logInstallRefusal(ws.Name, id, msg)
				return errors.New(msg)
			}
			res, err := installProvider(ws, p)
			if err != nil {
				return err
			}
			fmt.Fprintln(c.OutOrStdout(), strings.TrimSpace(res.Line()))
			return nil
		},
	}
}

// installProvider installs p's CLI for ws, with ws's environment. The caller has checked that p
// is a built-in provider in ws's scope.
func installProvider(ws *resolve.Workspace, p providers.Provider) (install.Result, error) {
	layer := envplan.LayerFor(ws)
	return install.Install(p, install.Deps{
		GOOS:     runtime.GOOS,
		Env:      envplan.Environ(ws, os.Environ()),
		LookPath: envplan.LookPath,
		Masked: func(name string) bool {
			return layer != nil && layer.IsMasked(name)
		},
		Run:          install.DefaultRun,
		RegistryPath: installRegistryPath,
	})
}

// logInstallRefusal appends the refusal of an out-of-scope install to the denial log
// (category identity, like the hook's deny of the same provider's CLI). A log that cannot be
// written changes nothing: the refusal stands.
func logInstallRefusal(identity, provider, reason string) {
	cwd, _ := os.Getwd()
	caller := ""
	if _, agent := agentSession(); agent {
		caller = audit.CallerHarness
	}
	_ = audit.AppendDenial(audit.Denial{
		Category: "identity", Layer: audit.LayerCommand, Caller: caller,
		Tool: "tentaqles install", Command: "tentaqles install " + provider, Subject: provider,
		Rule: policy.OutOfScopeCLIID, Reason: reason, Identity: identity, PID: os.Getpid(), Cwd: cwd,
	})
}
