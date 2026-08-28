package envplan

import (
	"strings"

	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Environ returns base with the workspace's desired vars applied and __TQ_STATE removed.
func Environ(ws *resolve.Workspace, base []string) []string {
	desired := Desired(ws)
	out := make([]string, 0, len(base)+len(desired))
	for _, kv := range base {
		k := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			k = kv[:i]
		}
		if k == StateVar {
			continue
		}
		if _, replaced := desired[k]; replaced {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range desired {
		out = append(out, k+"="+v)
	}
	return out
}
