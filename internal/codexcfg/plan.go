package codexcfg

import "strings"

// BypassFlag is Codex's full-bypass flag (DISCOVERY.md §4, Codex 0.154.0).
const BypassFlag = "--dangerously-bypass-approvals-and-sandbox"

// sessionSubcommands are the codex 0.154 subcommands that start a session
// and take BypassFlag themselves (`codex exec --help`, `resume --help`,
// `fork --help`). For them the flag goes after the subcommand: a root flag
// before `exec` parses, but nothing says exec honours it.
var sessionSubcommands = map[string]bool{"exec": true, "e": true, "resume": true, "fork": true}

// valueFlags are the codex options that take the next argument as their
// value, so that argument is not the subcommand.
var valueFlags = map[string]bool{
	"-c": true, "--config": true, "-m": true, "--model": true, "-p": true, "--profile": true,
	"-s": true, "--sandbox": true, "-a": true, "--ask-for-approval": true, "-C": true, "--cd": true,
	"-i": true, "--image": true, "--add-dir": true, "--enable": true, "--disable": true,
	"--remote": true, "--remote-auth-token-env": true, "--local-provider": true,
}

// LaunchArgs is the codex command line the launcher runs: BypassFlag (PRD
// v2 §7.2, the owner's decision of 2026-11-05) plus what the developer
// typed. The flag goes right after a session subcommand (exec, e, resume,
// fork) when there is one, first otherwise. tq never sets how Codex asks:
// when the developer typed a flag that does (-a/--ask-for-approval,
// -s/--sandbox, their glued forms -anever and -sread-only, --yolo, the
// bypass flag itself, or a -c/--config override of approval_policy or
// sandbox_mode, glued -capproval_policy=... included) the line goes through
// exactly as typed. Arguments after `--` are not flags. (--full-auto is not
// listed: codex 0.154 rejects it.)
func LaunchArgs(args []string) []string {
	sub := -1
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		if typedPermission(a, args, i) {
			return args
		}
		if !strings.HasPrefix(a, "-") && sub < 0 {
			sub = i
		}
		if valueFlags[a] {
			i++ // its value
		}
	}
	if sub >= 0 && sessionSubcommands[args[sub]] {
		out := append([]string{}, args[:sub+1]...)
		out = append(out, BypassFlag)
		return append(out, args[sub+1:]...)
	}
	return append([]string{BypassFlag}, args...)
}

// typedPermission reports whether args[i] says how Codex asks or sandboxes.
func typedPermission(a string, args []string, i int) bool {
	switch {
	case a == BypassFlag || a == "--yolo":
		return true
	case a == "-a" || a == "--ask-for-approval" || strings.HasPrefix(a, "--ask-for-approval="):
		return true
	case a == "-s" || a == "--sandbox" || strings.HasPrefix(a, "--sandbox="):
		return true
	case len(a) > 2 && !strings.HasPrefix(a, "--") && (a[:2] == "-a" || a[:2] == "-s"):
		return true // -anever, -sread-only, -a=never
	case a == "-c" || a == "--config":
		return i+1 < len(args) && (configKey(args[i+1], "approval_policy") || configKey(args[i+1], "sandbox_mode"))
	case strings.HasPrefix(a, "--config="):
		v := strings.TrimPrefix(a, "--config=")
		return configKey(v, "approval_policy") || configKey(v, "sandbox_mode")
	case len(a) > 2 && strings.HasPrefix(a, "-c") && !strings.HasPrefix(a, "--"):
		v := strings.TrimPrefix(strings.TrimPrefix(a, "-c"), "=")
		return configKey(v, "approval_policy") || configKey(v, "sandbox_mode")
	}
	return false
}

func configKey(kv, key string) bool {
	k, _, ok := strings.Cut(kv, "=")
	return ok && strings.TrimSpace(k) == key
}
