package cmd

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/approval"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/plans"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
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

// iacRecord stores a terraform plan for env under the cloud state dir.
func iacRecord(t *testing.T, env, dir, identity string) (plans.Record, string) {
	t.Helper()
	sd := packs.StateDir("acme", "cloud")
	id := plans.NewID(time.Now())
	name := plans.PlanName(id, "tfplan")
	body := []byte("tfplan-binary-" + id)
	if err := plans.WriteFile(sd, name, body); err != nil {
		t.Fatal(err)
	}
	r := plans.Record{ID: id, Env: env, Tool: "terraform", Dir: dir, Subject: dir, PlanFile: name, PlanSHA256: packs.HashBytes(body),
		InputsHash: "sha256:inputs", Identity: identity}
	if err := plans.Save(sd, r); err != nil {
		t.Fatal(err)
	}
	// the apply form tq pack cloud plan prints: an absolute slash path
	return r, filepath.ToSlash(filepath.Join(sd, filepath.FromSlash(name)))
}

// The stored plan an approved (or auto) apply names is declared with
// GuardCtx.ReadArgs, so the core state-dir rule (tq/hub-store-write) lets
// that one path through; the gap that stopped the approve flow end to end
// (round 1, C2.4) is closed through the real hook.
func TestHookIaCStoredPlanApply(t *testing.T) {
	clearAgentMarkers(t)
	ws := setupTrustedWorkspaceWithManifest(t, "acme", iacManifest)
	for _, k := range []string{"AWS_PROFILE", "TF_WORKSPACE", "AZURE_CONFIG_DIR"} {
		t.Setenv(k, "")
	}
	t.Setenv("ARM_SUBSCRIPTION_ID", "22222222-2222-2222-2222-222222222222")
	hook := func(cmd string) (int, string) {
		code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, cmd))
		return code, out
	}
	r, plan := iacRecord(t, "prod", "infra/envs/prod", "subscription=22222222-2222-2222-2222-222222222222")
	cmd := "terraform -chdir=infra/envs/prod apply -auto-approve " + plan
	// not approved: the gate denies (and so would the state-dir rule)
	if code, out := hook(cmd); code != 2 || !strings.Contains(out, "tq/iac-apply-env") {
		t.Fatalf("no approval: %d %s", code, out)
	}
	id, _, err := approval.Ask("acme", approval.Request{Pack: "cloud", Action: "apply", Subject: r.Subject, Target: "prod",
		Evidence: r.Evidence(""), Summary: "plan " + r.ID, Options: []string{"apply", "reject"}})
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open("acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ledger.SigningKey("acme")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AnswerPackApproval(id, "apply", "ok", "hub:tester", store.PackSigner(ledger.AnswerSigner("acme", key))); err != nil {
		t.Fatal(err)
	}
	st.Close()
	// approved: allowed through the real hook, the plan path included
	if code, out := hook(cmd); code != 0 || strings.Contains(out, "tq/hub-store-write") || strings.Contains(out, `"deny"`) {
		t.Fatalf("approved stored plan: %d %s", code, out)
	}
	// single use: the hook claimed it
	if code, out := hook(cmd); code != 2 || !strings.Contains(out, "used") {
		t.Fatalf("second use: %d %s", code, out)
	}
	// an auto environment's stored plan passes too
	t.Setenv("ARM_SUBSCRIPTION_ID", "")
	_, devPlan := iacRecord(t, "dev", "infra/envs/dev", "none")
	if code, out := hook("terraform -chdir=infra/envs/dev apply " + devPlan); code != 0 || strings.Contains(out, "tq/hub-store-write") || strings.Contains(out, `"deny"`) {
		t.Fatalf("auto stored plan: %d %s", code, out)
	}
	// the read argument never covers a write next to it
	if code, out := hook("terraform -chdir=infra/envs/dev apply " + devPlan + " > " + devPlan); code != 2 || !strings.Contains(out, "tq/hub-store-write") {
		t.Fatalf("redirect onto the plan: %d %s", code, out)
	}
	// an undeclared flag before the plan keeps the fail-closed reading
	if code, out := hook("terraform -chdir=infra/envs/dev apply -backup " + devPlan); code != 2 || !strings.Contains(out, "tq/hub-store-write") {
		t.Fatalf("undeclared flag: %d %s", code, out)
	}
}

// tq pack cloud approve only asks (spec D §9.8, terminal-only answers): the
// hook lets an agent run it (tq/human-answer is narrowed to executors), and the
// pack has no executor an agent could reach (any `pack cloud ... apply` is
// denied). The command itself only asking is TestApproveOnlyAsks in the
// cloud package (this test binary does not link the cloud pack).
func TestHookIaCApproveOnlyAsks(t *testing.T) {
	clearAgentMarkers(t)
	ws := setupTrustedWorkspaceWithManifest(t, "acme", `guard: { disable: [tq/example-cmd-apply] }
packs:
  cloud:
    environments:
      dev: {match: {dirs: [infra/envs/dev]}, apply: auto}
      qa: {match: {dirs: [infra/envs/qa]}, apply: approve}
`)
	r, _ := iacRecord(t, "qa", "infra/envs/qa", "none")
	hook := func(cmd string) (int, string) {
		code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, cmd))
		return code, out
	}
	if code, out := hook("tq pack cloud approve " + r.ID); code != 0 || strings.Contains(out, "tq/human-answer") {
		t.Fatalf("an agent may ask: %d %s", code, out)
	}
	for _, c := range []string{"tq pack cloud apply " + r.ID, "tq pack cloud --yes approve " + r.ID + " && tq pack cloud apply " + r.ID} {
		if code, out := hook(c); code != 2 || !strings.Contains(out, "human") {
			t.Fatalf("an executor form is denied: %s: %d %s", c, code, out)
		}
	}
}
