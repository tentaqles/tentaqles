package bundle

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/secrets"
)

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var nonIdent = regexp.MustCompile(`[^A-Z0-9]+`)

// Placeholderize returns a deep copy of srv in which every secret-shaped
// string is replaced by a ${VAR} reference, plus the variable names the user
// must now provide. Env entries keep their own key as the variable name
// (env.API_TOKEN -> ${API_TOKEN}); any other position gets TQ_<SERVER>_<FIELD>.
// Claude Code expands ${VAR} in MCP server command, args, env, url and
// headers, so the catalog never has to hold the literal value.
func Placeholderize(name string, srv MCPServer) (MCPServer, []string) {
	vars := map[string]bool{}
	prefix := "TQ_" + strings.Trim(nonIdent.ReplaceAllString(strings.ToUpper(name), "_"), "_")
	var walk func(path []string, v any) any
	walk = func(path []string, v any) any {
		switch val := v.(type) {
		case string:
			if secrets.ScanLine(val) == "" && !manifest.LooksLikeSecret(val) {
				return val
			}
			var vn string
			if len(path) == 2 && path[0] == "env" && envNameRe.MatchString(path[1]) {
				vn = path[1]
			} else {
				vn = prefix + "_" + strings.Trim(nonIdent.ReplaceAllString(strings.ToUpper(strings.Join(path, "_")), "_"), "_")
			}
			vars[vn] = true
			return "${" + vn + "}"
		case map[string]any:
			out := make(map[string]any, len(val))
			for k, vv := range val {
				out[k] = walk(append(append([]string{}, path...), k), vv)
			}
			return out
		case []any:
			out := make([]any, len(val))
			for i, vv := range val {
				out[i] = walk(append(append([]string{}, path...), fmt.Sprint(i)), vv)
			}
			return out
		}
		return v
	}
	out := MCPServer(walk(nil, map[string]any(srv)).(map[string]any))
	names := make([]string, 0, len(vars))
	for n := range vars {
		names = append(names, n)
	}
	sort.Strings(names)
	return out, names
}
