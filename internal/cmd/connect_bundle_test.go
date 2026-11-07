package cmd

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// L-12: a server `tentaqles connect` wrote for a provider in the identity's own scope is not
// denied by tq/cross-client-mcp, even when another client's bundle lists the same server name.
// Both clients here have a bundle, which the older tests never did.

// twoBundled sets up client acme (acmeBody, appended to the schema and client lines) and client
// beta with the bundle betaMCP, both trusted, under one base. It returns acme's and beta's roots.
func twoBundled(t *testing.T, acmeBody, betaMCP string) (root, beta string) {
	t.Helper()
	root, beta = setupScoped(t, "acme", acmeBody)
	body := "schema: tentaqles-client-v2\nclient: beta\nidentities: { claude: {} }\n" +
		"claude: { bundle: { mcp: [" + betaMCP + "] } }\n"
	if err := os.WriteFile(paths.ManifestPath("beta"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := trust.HashFile(paths.ManifestPath("beta"))
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.Allow("beta", h); err != nil {
		t.Fatal(err)
	}
	return root, beta
}

// The four catalog entries: each provider in acme's scope, connected by `tentaqles connect`, its
// server called by name from acme, while beta's bundle lists the same server.
func TestHook_ConnectedServerNotCrossClientDenied(t *testing.T) {
	emb, err := providers.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"gh", "linear", "asana", "jira"} {
		p, ok := emb.Get(id)
		if !ok || p.Connect == nil {
			t.Fatalf("%s has no connect:", id)
		}
		name := p.Connect.Name
		t.Run(id, func(t *testing.T) {
			acme := "identities: { claude: {} }\nscope: [" + id + "]\nclaude: { bundle: { mcp: [memory] } }\n"
			root, _ := twoBundled(t, acme, name)
			chdir(t, root)
			if out, err := runTq(t, "connect", id); err != nil || !strings.Contains(out, "connected as "+name) {
				t.Fatalf("connect %s: %v %q", id, err, out)
			}
			code, category, rule, errOut := hookDecision(t, root, "mcp__"+name+"__list_issues", "{}")
			if code != 0 {
				t.Errorf("acme's own connected %s server denied: %s/%s %s", name, category, rule, errOut)
			}
		})
	}
}

// Must stay denied: a server name listed by another client's bundle that no provider in acme's
// scope owns (a plain server, not a connect name). The cross-client rule denies it.
func TestHook_OtherClientsBundleServerStaysDenied(t *testing.T) {
	root, _ := twoBundled(t, "identities: { claude: {} }\nclaude: { bundle: { mcp: [memory] } }\n", "notes-server")
	code, category, rule, errOut := hookDecision(t, root, "mcp__notes-server__search", "{}")
	if code != 2 || category != policy.CategoryIdentity || rule != "tq/cross-client-mcp" ||
		!strings.Contains(errOut, `MCP server "notes-server" belongs to client "beta"`) {
		t.Errorf("code=%d %s/%s %q", code, category, rule, errOut)
	}
}

// Must stay denied: a provider's connect name when that provider is out of acme's scope. Here
// acme's scope has no jira, and beta's bundle lists jira's name, so the call is refused (by the
// identity rules, which name the server as out of scope, or by the cross-client rule).
func TestHook_OutOfScopeProviderNameStaysDenied(t *testing.T) {
	jira, _ := providers.Embedded()
	p, _ := jira.Get("jira")
	name := p.Connect.Name
	root, _ := twoBundled(t, "identities: { claude: {} }\nclaude: { bundle: { mcp: [memory] } }\n", name)
	chdir(t, root)
	if out, err := runTq(t, "connect", "linear"); err == nil {
		t.Fatalf("connect of an out-of-scope provider was accepted: %q", out)
	}
	code, category, rule, errOut := hookDecision(t, root, fmt.Sprintf("mcp__%s__list_issues", name), "{}")
	if code != 2 || category != policy.CategoryIdentity || (rule != "tq/cross-client-mcp" && rule != policy.OutOfScopeMCPID) {
		t.Errorf("code=%d %s/%s %q", code, category, rule, errOut)
	}
}
