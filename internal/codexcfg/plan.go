package codexcfg

import "strings"

// BypassFlag is Codex's full-bypass flag (DISCOVERY.md §4, Codex 0.154.0).
const BypassFlag = "--dangerously-bypass-approvals-and-sandbox"

// LaunchArgs is the codex command line the launcher runs: BypassFlag first
// (PRD v2 §7.2, the owner's decision of 2026-11-05), then what the
// developer typed. tq never sets how Codex asks, so when the developer typed
// a flag that does (-a/--ask-for-approval, -s/--sandbox, --full-auto,
// --yolo, the bypass flag itself, or a -c/--config override of
// approval_policy or sandbox_mode) the line goes through exactly as typed.
// Arguments after `--` are not flags.
func LaunchArgs(args []string) []string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		switch {
		case a == BypassFlag || a == "--yolo" || a == "--full-auto",
			a == "-a" || a == "--ask-for-approval" || strings.HasPrefix(a, "--ask-for-approval="),
			a == "-s" || a == "--sandbox" || strings.HasPrefix(a, "--sandbox="):
			return args
		case a == "-c" || a == "--config":
			if i+1 < len(args) && (configKey(args[i+1], "approval_policy") || configKey(args[i+1], "sandbox_mode")) {
				return args
			}
		case strings.HasPrefix(a, "--config="):
			v := strings.TrimPrefix(a, "--config=")
			if configKey(v, "approval_policy") || configKey(v, "sandbox_mode") {
				return args
			}
		}
	}
	return append([]string{BypassFlag}, args...)
}

func configKey(kv, key string) bool {
	k, _, ok := strings.Cut(kv, "=")
	return ok && strings.TrimSpace(k) == key
}
