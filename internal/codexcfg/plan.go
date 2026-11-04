package codexcfg

import "strings"

// Codex values tq writes.
const (
	ApprovalOnRequest = "on-request"
	SandboxWorkspace  = "workspace-write"
	// BypassFlag is Codex's own bypass flag; tq never adds it, but a user who
	// typed it keeps it (LaunchArgs).
	BypassFlag = "--dangerously-bypass-approvals-and-sandbox"
)

// Plan is what tq pins in Codex: the approval policy and the sandbox.
type Plan struct {
	Approval string // approval_policy / -a
	Sandbox  string // sandbox_mode / -s
}

// Pinned is the one plan tq renders into every Codex identity: Codex asks
// on request and writes inside the workspace. tq's guard (the identity and
// secret rules) runs through the PreToolUse hook; everything else is
// Codex's own permission system (docs/prd.md D-30).
var Pinned = Plan{Approval: ApprovalOnRequest, Sandbox: SandboxWorkspace}

// Flags are the launch flags for p (nil for a zero Plan).
func (p Plan) Flags() []string {
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
	if p.Approval == "" && p.Sandbox == "" {
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
	if !userA && p.Approval != "" {
		pre = append(pre, "-a", p.Approval)
	}
	if !userS && p.Sandbox != "" {
		pre = append(pre, "-s", p.Sandbox)
	}
	return append(pre, args...)
}

func configKey(kv, key string) bool {
	k, _, ok := strings.Cut(kv, "=")
	return ok && strings.TrimSpace(k) == key
}
