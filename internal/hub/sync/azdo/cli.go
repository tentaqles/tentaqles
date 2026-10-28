package azdo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// Resource is the Azure DevOps application id an Entra access token is requested for.
const Resource = "499b84ac-1321-427f-aa17-267ca6975798"

// azTimeout bounds one az call: a CLI that hangs (a login prompt, a slow proxy) must never stall
// the sync loop.
const azTimeout = 30 * time.Second

var lookPath = exec.LookPath

// AzToken is the cli credential source for azure-devops: `az account get-access-token` run with
// env (the workspace identity environment, which carries AZURE_CONFIG_DIR). nil env inherits the
// process environment. A missing or logged-out az is cred.ErrNoCredential. The token is returned
// for one request and never logged; az's stderr is quoted only as a short reason, and az never
// prints the token there.
func AzToken(ctx context.Context, env []string) (cred.Token, error) {
	path, err := lookPath("az")
	if err != nil {
		return cred.Token{}, fmt.Errorf("%w: az CLI not found (install the Azure CLI, then az login)", cred.ErrNoCredential)
	}
	ctx, cancel := context.WithTimeout(ctx, azTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "account", "get-access-token", "--resource", Resource,
		"--query", "accessToken", "--output", "tsv")
	if len(env) > 0 {
		cmd.Env = env
	}
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return cred.Token{}, fmt.Errorf("az account get-access-token timed out after %s", azTimeout)
		}
		if ctx.Err() != nil {
			return cred.Token{}, ctx.Err()
		}
		return cred.Token{}, fmt.Errorf("%w: az account get-access-token failed (run az login for this identity): %s",
			cred.ErrNoCredential, reason(stderr.String()))
	}
	tok := strings.TrimSpace(stdout.String())
	if tok == "" || strings.ContainsAny(tok, " \t\r\n") {
		return cred.Token{}, fmt.Errorf("%w: az account get-access-token printed no token (run az login)", cred.ErrNoCredential)
	}
	return cred.Token{Scheme: "Bearer", Value: tok}, nil
}

// reason is the first non-empty line of a CLI's stderr, capped.
func reason(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if len(l) > 200 {
				l = l[:200] + "..."
			}
			return l
		}
	}
	return "no detail"
}
