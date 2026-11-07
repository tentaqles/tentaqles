package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/bundle"
	"github.com/tentaqles/tentaqles/internal/connect"
	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// newConnectCmd is tentaqles connect <provider> (ADR-0017): it registers the provider's hosted
// MCP server, as the embedded catalog defines it, in the client's .claude.json.
//
// It is the one command an agent may run that writes the identity's Claude config, so it is
// not human-only (no refuseAgent, no confirmation) and it is narrow instead: it takes the
// provider and nothing else (no flag, no second argument, no user file can change the entry),
// refuses a provider outside the client's scope, refuses a neutral or untrusted folder, adds one
// member to mcpServers and logs the change to $TQ_HOME/connections.jsonl. It stores no
// credential: a human authenticates the server with /mcp.
func newConnectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "connect <provider>",
		Short: "Register a provider's MCP server (from the catalog) in this client's Claude config",
		Long: `tentaqles connect <provider> adds the provider's hosted MCP server to mcpServers in the
client's .claude.json, as the embedded provider catalog defines it (gh, linear, asana and jira
have one), and keeps every other key of the file. It takes the provider and nothing else:
the entry is the catalog's, never an argument or a user file's.

The provider must be in the client's scope; a provider outside it is refused and logged. The
folder must belong to a trusted client. Running it again prints "already connected" and writes
nothing. Each change is one line in ~/.tentaqles/connections.jsonl (time, identity, provider,
server name, caller; no value of the entry).

It does not sign in: authenticate the server with /mcp in a Claude session.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return runConnect(c, args[0])
		},
	}
}

func runConnect(c *cobra.Command, id string) error {
	defer trust.MemoScope()()
	cfg, err := registry.Load()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	res := resolve.Resolve(cwd, cfg)
	switch {
	case res.Workspace == nil && res.Untrusted != nil:
		return fmt.Errorf("connect %s: identity %s is untrusted: %s", id, res.Untrusted.Name, res.Reason)
	case res.Workspace == nil:
		return fmt.Errorf("connect %s: this folder is neutral, it belongs to no trusted identity (%s)", id, res.Reason)
	}
	w := res.Workspace
	m := w.Manifest

	cat := providers.MustLoad()
	if _, ok := cat.Get(id); !ok {
		return fmt.Errorf("connect: unknown provider %q (tentaqles providers list)", id)
	}
	if !containsID(m.Scope(), id) {
		msg := fmt.Sprintf("%s is outside the scope of identity %s: %s", id, w.Name, identityScope(m))
		caller := ""
		if humanonly.DetectAgent() != nil {
			caller = audit.CallerHarness
		}
		if lerr := audit.AppendDenial(audit.Denial{
			Category: policy.CategoryIdentity, Layer: audit.LayerCommand, Caller: caller,
			Tool: "tentaqles connect", Command: "tentaqles connect " + id, Subject: id,
			Rule: policy.OutOfScopeMCPID, Reason: msg, Identity: w.Name, PID: os.Getpid(), Cwd: cwd,
		}); lerr != nil {
			return fmt.Errorf("%s (the refusal could not be logged: %v)", msg, lerr)
		}
		return fmt.Errorf("%s", msg)
	}

	// The entry is the embedded catalog's, never the merged catalog's (a user file may not
	// choose it: providers.loadUserFile refuses a connect: key, and this reads none).
	emb, err := providers.Embedded()
	if err != nil {
		return err
	}
	ep, ok := emb.Get(id)
	if !ok || ep.Connect == nil {
		fmt.Fprintf(c.OutOrStdout(), "%s: no MCP server in the catalog\n", id)
		return nil
	}
	if !containsID(m.IdentityNames(), "claude") {
		return fmt.Errorf("connect %s: identity %s has no claude identity, so there is no Claude config to register a server in", id, w.Name)
	}
	name := ep.Connect.Name
	caller := connect.CallerHuman
	if humanonly.DetectAgent() != nil {
		caller = connect.CallerHarness
	}
	path := filepath.Join(paths.IdentityDir(w.Name, "claude"), ".claude.json")
	added, err := connect.Add(path, name, ep.Connect.Entry(), func() error {
		return connect.Log(connect.Record{Identity: w.Name, Provider: id, Server: name, Caller: caller})
	})
	if err != nil {
		return fmt.Errorf("connect %s: %w", id, err)
	}
	if !added {
		fmt.Fprintf(c.OutOrStdout(), "%s: already connected\n", id)
		return nil
	}
	fmt.Fprintf(c.OutOrStdout(), "%s: connected as %s\n", id, name)
	// Claude Code rewrites .claude.json while it runs and does not honour connect's lock, as with
	// `tq settings render`: say so, and how to check.
	if bundle.InUse(filepath.Dir(path)) {
		fmt.Fprintf(c.ErrOrStderr(), "warning: Claude Code looks open with this config home; it may write .claude.json back and undo this. Restart the session, then run tentaqles status --json to check %s is connected, and tentaqles connect %s again if it is not\n", id, id)
	}
	return nil
}

func containsID(xs []string, x string) bool {
	for _, v := range xs {
		if strings.EqualFold(v, x) {
			return true
		}
	}
	return false
}
