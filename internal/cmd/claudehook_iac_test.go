package cmd

import (
	"strings"
	"testing"
)

// The pack's example guard (pack_test.go) is switched off so these tests see
// the real cloud guards only.
const iacManifest = `guard: { disable: [tq/example-cmd-apply] }
packs:
  cloud:
    environments:
      dev: {match: {dirs: [infra/envs/dev]}, apply: auto}
      staging: {match: {dirs: [infra/envs/staging]}}
      prod: {match: {dirs: [infra/envs/prod], subscriptions: [22222222-2222-2222-2222-222222222222]}}
`

func TestHookIaCApplyEnv(t *testing.T) {
	clearAgentMarkers(t)
	ws := setupTrustedWorkspaceWithManifest(t, "acme", iacManifest)
	for _, k := range []string{"ARM_SUBSCRIPTION_ID", "AWS_PROFILE", "TF_WORKSPACE", "AZURE_CONFIG_DIR"} {
		t.Setenv(k, "")
	}
	// auto really allows: the core tq/iac-apply ask is superseded
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "terraform -chdir=infra/envs/dev apply -auto-approve"))
	if code != 0 || strings.Contains(out, "tq/iac-apply") || strings.Contains(out, `"ask"`) || strings.Contains(out, `"deny"`) {
		t.Fatalf("dev auto: %d %s", code, out)
	}
	// staging asks under the gate's id
	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "terraform -chdir=infra/envs/staging apply"))
	if code != 0 || !strings.Contains(out, "tq/iac-apply-env") {
		t.Fatalf("staging ask: %d %s", code, out)
	}
	// prod without an approval is denied, not asked
	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "terraform -chdir=infra/envs/prod apply"))
	if code != 2 || !strings.Contains(out, "tq/iac-apply-env") {
		t.Fatalf("prod deny: %d %s", code, out)
	}
	// kubectl is not the gate's: the core ask stays
	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "terraform -chdir=infra/envs/dev apply && kubectl apply -f x.yaml"))
	if code != 0 || !strings.Contains(out, "tq/iac-apply") {
		t.Fatalf("kubectl keeps the core rule: %d %s", code, out)
	}
	// identity drift: dev's apply into prod's subscription
	t.Setenv("ARM_SUBSCRIPTION_ID", "22222222-2222-2222-2222-222222222222")
	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "terraform -chdir=infra/envs/dev apply"))
	if code != 2 || !strings.Contains(out, "tq/iac-identity-drift") {
		t.Fatalf("drift: %d %s", code, out)
	}
}

func TestHookIaCGuardsOffWithoutThePack(t *testing.T) {
	clearAgentMarkers(t)
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "guard: { disable: [tq/example-cmd-apply] }\n")
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "terraform -chdir=infra/envs/prod apply"))
	if code != 0 || strings.Contains(out, "tq/iac-apply-env") || !strings.Contains(out, "tq/iac-apply") {
		t.Fatalf("no pack: the core ask: %d %s", code, out)
	}
}
