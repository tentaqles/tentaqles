package codexcfg

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// presetEff is the effective action of a preset table without the WS1
// engine: the category value applied to the rule's own action, locked
// categories at their own action.
func presetEff(t *testing.T, name string) func(*policy.Rule) policy.Action {
	t.Helper()
	tab, ok := guardcfg.Preset(name)
	if !ok {
		t.Fatalf("no preset %s", name)
	}
	return func(r *policy.Rule) policy.Action {
		c := guardcfg.CategoryOf(r)
		if c == "" || guardcfg.Locked(c) {
			return r.Action
		}
		return tab.Get(c).Apply(r.Action)
	}
}

func decisions(p Plan) map[string]string {
	out := map[string]string{}
	for _, r := range p.Rules {
		out[r.ID] = r.Decision
	}
	return out
}

func TestCompileDefault(t *testing.T) {
	p := Compile(guardcfg.ModeDefault, policy.Builtin(), presetEff(t, "default"), "danger-full-access")
	if p.Approval != "on-request" || p.Sandbox != "workspace-write" || p.Bypass {
		t.Fatalf("default flags: %+v", p)
	}
	d := decisions(p)
	if d["tq/gh-repo-delete"] != DecisionForbidden {
		t.Errorf("gh repo delete denies by default: %v", d)
	}
	// The sandbox is danger-full-access, so no prompt rules are written: a
	// launch may run in bypass, where a prompt rule may not prompt.
	for id, dec := range d {
		if dec == DecisionPrompt {
			t.Errorf("%s: no prompt rules with a danger-full-access sandbox", id)
		}
	}
	p = Compile("", policy.Builtin(), presetEff(t, "default"), "workspace-write")
	d = decisions(p)
	for _, id := range []string{"tq/iac-apply", "tq/iac-destroy", "tq/force-push", "tq/remote-migration", "tq/registry-write"} {
		if d[id] != DecisionPrompt {
			t.Errorf("%s: want prompt in default, got %q", id, d[id])
		}
	}
	if p.Mode != guardcfg.ModeDefault {
		t.Errorf("empty mode is default: %q", p.Mode)
	}
	// Asks without a prefix form are blocked in Codex.
	if !contains(p.Blocked, "tq/gh-admin-merge") || !contains(p.Blocked, "tq/cloud-delete") || contains(p.Blocked, "tq/iac-apply") {
		t.Errorf("blocked: %v", p.Blocked)
	}
}

func TestCompileRelaxed(t *testing.T) {
	p := Compile(guardcfg.ModeRelaxed, policy.Builtin(), presetEff(t, "relaxed"), "workspace-write")
	if p.Approval != "never" || p.Sandbox != "workspace-write" || p.Bypass {
		t.Fatalf("relaxed flags: %+v", p)
	}
	d := decisions(p)
	if d["tq/gh-repo-delete"] != DecisionPrompt || d["tq/force-push"] != DecisionPrompt || d["tq/registry-write"] != DecisionPrompt {
		t.Errorf("git-history and system prompt in relaxed: %v", d)
	}
	for _, id := range []string{"tq/iac-apply", "tq/iac-destroy", "tq/remote-migration"} {
		if _, ok := d[id]; ok {
			t.Errorf("%s: skipped in relaxed, no rule expected (got %s)", id, d[id])
		}
	}
	p = Compile(guardcfg.ModeRelaxed, policy.Builtin(), presetEff(t, "relaxed"), "danger-full-access")
	if !p.Bypass || p.Sandbox != "danger-full-access" {
		t.Fatalf("relaxed + danger-full-access is the bypass flag: %+v", p)
	}
	if got := strings.Join(p.Flags(), " "); got != "--dangerously-bypass-approvals-and-sandbox" {
		t.Fatalf("flags: %q", got)
	}
}

func TestCompileStrict(t *testing.T) {
	p := Compile(guardcfg.ModeStrict, policy.Builtin(), presetEff(t, "strict"), "read-only")
	if p.Approval != "on-request" || p.Sandbox != "workspace-write" {
		t.Fatalf("strict flags: %+v", p)
	}
	d := decisions(p)
	for _, id := range []string{"tq/iac-apply", "tq/iac-destroy", "tq/force-push", "tq/gh-repo-delete", "tq/remote-migration"} {
		if d[id] != DecisionForbidden {
			t.Errorf("%s: want forbidden in strict, got %q", id, d[id])
		}
	}
	if got := strings.Join(p.Flags(), " "); got != "-a on-request -s workspace-write" {
		t.Fatalf("flags: %q", got)
	}
}

func TestCompileCustomApproval(t *testing.T) {
	// A custom table that only asks in git-history and system is relaxed's
	// shape: approval never, with the configured sandbox.
	relaxed := presetEff(t, "relaxed")
	p := Compile(guardcfg.ModeCustom, policy.Builtin(), relaxed, "read-only")
	if p.Approval != "never" || p.Sandbox != "read-only" {
		t.Fatalf("custom from relaxed: %+v", p)
	}
	// One more ask anywhere else turns approvals on.
	eff := func(r *policy.Rule) policy.Action {
		if r.ID == "tq/iac-apply" {
			return policy.Ask
		}
		return relaxed(r)
	}
	p = Compile(guardcfg.ModeCustom, policy.Builtin(), eff, "read-only")
	if p.Approval != "on-request" || p.Sandbox != "workspace-write" {
		t.Fatalf("custom with an ask: %+v", p)
	}
}

// Locked rules keep their deny as forbidden in every mode, and a locked ask
// is never a prompt (the hook denies it in Codex).
func TestCompileLockedNeverPrompt(t *testing.T) {
	rules := []policy.Rule{
		{ID: "tq/guard-change", Action: policy.Ask, Command: "x"},
		{ID: "tq/iac-apply", Action: policy.Ask, Command: "x"},
	}
	saved := prefixes["tq/guard-change"]
	prefixes["tq/guard-change"] = []Pattern{{{"tq"}, {"guard"}}}
	defer func() {
		if saved == nil {
			delete(prefixes, "tq/guard-change")
		} else {
			prefixes["tq/guard-change"] = saved
		}
	}()
	p := Compile(guardcfg.ModeDefault, rules, func(r *policy.Rule) policy.Action { return r.Action }, "workspace-write")
	if d := decisions(p); d["tq/guard-change"] != "" || d["tq/iac-apply"] != DecisionPrompt {
		t.Fatalf("locked ask must not be a prompt: %v", d)
	}
	rules[0].Action = policy.Deny
	p = Compile(guardcfg.ModeRelaxed, rules, func(r *policy.Rule) policy.Action { return r.Action }, "workspace-write")
	if d := decisions(p); d["tq/guard-change"] != DecisionForbidden {
		t.Fatalf("locked deny is forbidden: %v", d)
	}
}

func TestLaunchArgs(t *testing.T) {
	p := Plan{Approval: "on-request", Sandbox: "workspace-write"}
	bp := Plan{Approval: "never", Sandbox: "danger-full-access", Bypass: true}
	for _, tc := range []struct {
		p    Plan
		args []string
		want string
	}{
		{p, []string{"hi"}, "-a on-request -s workspace-write hi"},
		{p, []string{"exec", "hi"}, "-a on-request -s workspace-write exec hi"},
		{p, []string{"-a", "never", "hi"}, "-s workspace-write -a never hi"},
		{p, []string{"--ask-for-approval=never"}, "-s workspace-write --ask-for-approval=never"},
		{p, []string{"-s", "read-only"}, "-a on-request -s read-only"},
		{p, []string{"--sandbox=read-only"}, "-a on-request --sandbox=read-only"},
		{p, []string{"-c", "sandbox_mode=\"read-only\""}, "-a on-request -c sandbox_mode=\"read-only\""},
		{p, []string{"--config=approval_policy=\"never\""}, "-s workspace-write --config=approval_policy=\"never\""},
		{p, []string{"--yolo"}, "--yolo"},
		{p, []string{"--dangerously-bypass-approvals-and-sandbox", "x"}, "--dangerously-bypass-approvals-and-sandbox x"},
		{bp, []string{"hi"}, "--dangerously-bypass-approvals-and-sandbox hi"},
		{bp, []string{"-s", "read-only"}, "-a never -s read-only"},
		{p, []string{"--", "-a"}, "-a on-request -s workspace-write -- -a"},
		{Plan{}, []string{"hi"}, "hi"},
	} {
		if got := strings.Join(LaunchArgs(tc.p, tc.args), " "); got != tc.want {
			t.Errorf("%+v %v: got %q want %q", tc.p, tc.args, got, tc.want)
		}
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
