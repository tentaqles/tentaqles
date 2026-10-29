package cmd

import (
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// readArgWriteRe is any command that may write: a write verb, a shell or an
// interpreter (which could write anything), as a command word.
var readArgWriteRe = regexp.MustCompile(`(^|[\s;&|(])(tee|cp|mv|rm|ln|mklink|dd|install|rsync|touch|truncate|chmod|chown|shred|unlink|rmdir|mkdir|tar|unzip|zip|sed|awk|perl|python3?|py|node|deno|bun|ruby|php|pwsh|powershell|bash|sh|zsh|cmd|sqlite3|del|erase|copy|move|ren|xcopy|robocopy|set-content|add-content|out-file|new-item|copy-item|move-item|remove-item|rename-item|clear-content|sc|ac|cpi|mi|ri|ni|iex|invoke-expression|start-process|eval|exec|xargs|find)(\.exe)?($|[\s;&|)])`)

// withoutReadArgs returns cmd with the exact StateDir paths a pack guard
// verified for this call (packs.GuardCtx.ReadArgs) replaced by a
// placeholder, so tq/hub-store-write checks everything else as usual. ok is
// false, and nothing is exempt, when the command could write (a redirect, a
// write verb, a shell or interpreter, a substitution) or names a verified
// path in more than one command segment. A token matches only when it is
// the path itself (quotes stripped) or a flag's =value, spelled as the
// absolute path or its final path; no variable, glob, ~ or .. spelling.
func withoutReadArgs(cmd string, readArgs []string) (string, bool) {
	if strings.ContainsAny(cmd, "<>`") || strings.Contains(cmd, "$(") || strings.Contains(cmd, "@(") {
		return cmd, false
	}
	if readArgWriteRe.MatchString(normShell(cmd)) {
		return cmd, false
	}
	forms := map[string]bool{}
	for _, a := range readArgs {
		forms[normPathForm(a)] = true
		if fp, err := packs.FinalPath(a); err == nil {
			forms[normPathForm(fp)] = true
		}
	}
	toks := strings.Fields(strings.NewReplacer("\r", " ", "\n", " ; ").Replace(cmd)) // a newline separates segments
	seg := 0
	segs := map[int]bool{}
	for i, t := range toks {
		prefix, val := "", strings.Trim(t, `"'`)
		if strings.HasPrefix(val, "-") {
			if k := strings.Index(val, "="); k > 0 {
				prefix, val = val[:k+1], strings.Trim(val[k+1:], `"'`)
			}
		}
		if val != "" && !strings.ContainsAny(val, "$%*?~;&|") && !strings.Contains(val, "..") && forms[normPathForm(val)] {
			segs[seg] = true
			toks[i] = prefix + "tq-read-arg"
		}
		seg += strings.Count(t, ";") + strings.Count(t, "&") + strings.Count(t, "|")
	}
	if len(segs) != 1 {
		return cmd, false
	}
	return strings.Join(toks, " "), true
}
