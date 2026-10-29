package codexcfg

import (
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// Codex values tq writes.
const (
	ApprovalOnRequest = "on-request"
	ApprovalNever     = "never"
	SandboxWorkspace  = "workspace-write"
	SandboxFull       = "danger-full-access"
	BypassFlag        = "--dangerously-bypass-approvals-and-sandbox"
)

// Plan is what one tq guard mode compiles to in Codex (spec E §12).
type Plan struct {
	Mode     string
	Approval string // approval_policy / -a
	Sandbox  string // sandbox_mode / -s
	// Bypass: relaxed-like with a danger-full-access sandbox, launched with
	// --dangerously-bypass-approvals-and-sandbox instead of -a/-s.
	Bypass bool
	// Rules are rules/tq.rules: asks with a prefix form as prompt, denies as
	// forbidden.
	Rules []PrefixRule
	// Blocked lists the ids whose ask has no prompt rule: in Codex the hook
	// denies them (the accepted limit of spec E §12, Q5). Sorted.
	Blocked []string
}

// Compile maps mode to Codex settings. rules is the workspace's rule set
// (built-ins after guard.disable/actions), eff the mode's effective action
// per rule (guardcfg.Resolved.Effective), sandbox the configured
// guard.codex.sandbox.
//
//   - default and strict: -a on-request -s workspace-write;
//   - relaxed: -a never -s <sandbox>, and the bypass flag for danger-full-access;
//   - custom: -a on-request -s workspace-write when any toggleable rule asks
//     outside git-history and system (the asks relaxed keeps), else as relaxed.
//
// With a danger-full-access sandbox no prompt rule is written: a launch may
// then run in bypass, where Codex may not prompt (A9), and the hook only lets
// an ask through when a prompt rule covers it.
func Compile(mode string, rules []policy.Rule, eff func(*policy.Rule) policy.Action, sandbox string) Plan {
	if mode == "" {
		mode = guardcfg.ModeDefault
	}
	if sandbox == "" {
		sandbox = SandboxWorkspace
	}
	p := Plan{Mode: mode}

	byID := map[string]policy.Action{}
	locked := map[string]bool{}
	var order []string
	extraAsk := false
	for i := range rules {
		r := &rules[i]
		a := eff(r)
		c := guardcfg.CategoryOf(r)
		if prev, ok := byID[r.ID]; !ok {
			order = append(order, r.ID)
			byID[r.ID] = a
		} else if a.StricterThan(prev) {
			byID[r.ID] = a
		}
		if guardcfg.Locked(c) {
			locked[r.ID] = true
		} else if a == policy.Ask && c != guardcfg.CatGitHistory && c != guardcfg.CatSystem {
			extraAsk = true
		}
	}

	never := false
	switch mode {
	case guardcfg.ModeRelaxed:
		never = true
	case guardcfg.ModeCustom:
		never = !extraAsk
	}
	if never {
		p.Approval, p.Sandbox = ApprovalNever, sandbox
		p.Bypass = sandbox == SandboxFull
	} else {
		p.Approval, p.Sandbox = ApprovalOnRequest, SandboxWorkspace
	}

	reason := map[string]string{}
	for i := range rules {
		if _, ok := reason[rules[i].ID]; !ok {
			reason[rules[i].ID] = rules[i].Reason
		}
	}
	prompted := map[string]bool{}
	sort.Strings(order)
	for _, id := range order {
		a := byID[id]
		dec := ""
		switch {
		case a == policy.Deny:
			dec = DecisionForbidden
		case a == policy.Ask && !locked[id] && sandbox != SandboxFull:
			dec = DecisionPrompt
		}
		if dec != "" {
			for _, pat := range Prefixes(id) {
				p.Rules = append(p.Rules, PrefixRule{ID: id, Decision: dec, Pattern: pat, Justification: reason[id]})
				if dec == DecisionPrompt {
					prompted[id] = true
				}
			}
		}
		if a == policy.Ask && !prompted[id] {
			p.Blocked = append(p.Blocked, id)
		}
	}
	return p
}

// Flags are the launch flags for p (nil for a zero Plan).
func (p Plan) Flags() []string {
	switch {
	case p.Bypass:
		return []string{BypassFlag}
	case p.Approval == "" && p.Sandbox == "":
		return nil
	}
	var out []string
	if p.Approval != "" {
		out = append(out, "-a", p.Approval)
	}
	if p.Sandbox != "" {
		out = append(out, "-s", p.Sandbox)
	}
	return out
}

// LaunchArgs prepends p's flags to a codex command line, leaving alone what
// the user typed: an explicit -a/--ask-for-approval or approval_policy
// override keeps its approval, an explicit -s/--sandbox or sandbox_mode
// override keeps its sandbox, and a typed bypass flag (or --yolo) gets
// nothing added.
func LaunchArgs(p Plan, args []string) []string {
	if p.Approval == "" && p.Sandbox == "" && !p.Bypass {
		return args
	}
	userA, userS := false, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		switch {
		case a == BypassFlag || a == "--yolo":
			return args
		case a == "-a" || a == "--ask-for-approval" || strings.HasPrefix(a, "--ask-for-approval="):
			userA = true
		case a == "-s" || a == "--sandbox" || strings.HasPrefix(a, "--sandbox="):
			userS = true
		case a == "-c" || a == "--config":
			if i+1 < len(args) {
				userA = userA || configKey(args[i+1], "approval_policy")
				userS = userS || configKey(args[i+1], "sandbox_mode")
			}
		case strings.HasPrefix(a, "--config="):
			v := strings.TrimPrefix(a, "--config=")
			userA = userA || configKey(v, "approval_policy")
			userS = userS || configKey(v, "sandbox_mode")
		}
	}
	var pre []string
	switch {
	case p.Bypass && !userA && !userS:
		pre = []string{BypassFlag}
	default:
		if !userA && p.Approval != "" {
			pre = append(pre, "-a", p.Approval)
		}
		if !userS && p.Sandbox != "" {
			pre = append(pre, "-s", p.Sandbox)
		}
	}
	return append(pre, args...)
}

func configKey(kv, key string) bool {
	k, _, ok := strings.Cut(kv, "=")
	return ok && strings.TrimSpace(k) == key
}
