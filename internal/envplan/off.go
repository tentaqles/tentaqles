package envplan

// wsVars are the workspace markers Desired sets; an off launch drops them
// even when the shell state that recorded them is lost.
var wsVars = []string{"TQ_WS", "TQ_WS_ROOT"}

// OffEnviron is the environment of an off launch (tq run --off, the
// claude-off and codex-off functions): base cleaned of every tq change
// (Clean), without the workspace markers. No variable marks the launch as
// off: the launcher switches the plugin off and the cleaned environment
// carries no identity, so there is nothing for tq to act on.
func OffEnviron(base []string) []string {
	drop := map[string]bool{}
	for _, k := range wsVars {
		drop[envKey(k)] = true
	}
	cleaned := Clean(base)
	out := make([]string, 0, len(cleaned))
	for _, kv := range cleaned {
		k, _, _ := splitKV(kv)
		if drop[envKey(k)] {
			continue
		}
		out = append(out, kv)
	}
	return out
}
