package data

import (
	"path/filepath"
	"regexp"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// maxScriptRead bounds the read of a shell script a command runs.
const maxScriptRead = 64 << 10

// scriptNamesTool reports a shell script file the command runs (bash x.sh,
// ./x.sh, source x.sh) whose content names a tool re matches. The script is
// read through ReadLocal (its roots and refusals, at most 64 KiB); a script
// that cannot be read counts only when the command line itself names the
// tool. The guards ask on it: they do not parse scripts.
func scriptNamesTool(g packs.GuardCtx, cmd string, re *regexp.Regexp) (string, bool) {
	for _, p := range scan.ScriptFiles(cmd) {
		abs := p
		if !filepath.IsAbs(abs) {
			cwd := g.Cwd()
			if cwd == "" {
				continue
			}
			abs = filepath.Join(cwd, filepath.FromSlash(p))
		}
		b, err := g.ReadLocal(abs, maxScriptRead)
		if err != nil {
			if re.MatchString(cmd) {
				return p, true
			}
			continue
		}
		if re.Match(b) {
			return p, true
		}
	}
	return "", false
}

// The tools each data guard looks for in a script.
var (
	pipelineToolRe = regexp.MustCompile(`(?i)\b(airflow|dagster|prefect)\b|\bdg\s+launch\b|\bcreate-run\b`)
	dbtToolRe      = regexp.MustCompile(`(?i)\bdbt\b`)
	bqToolRe       = regexp.MustCompile(`(?i)\bbq(\.exe|\.cmd)?\b`)
	resizeToolRe   = regexp.MustCompile(`(?i)\b(snow|snowsql|databricks|az)(\.exe|\.cmd)?\b`)
	queryToolRe    = regexp.MustCompile(`(?i)\b(snow|snowsql|psql|databricks|sqlcmd)(\.exe|\.cmd)?\b`)
)

// withScripts runs a guard, and when it lets the call through, asks on a
// script file the command runs whose content names the guard's tool. on
// reports whether the guard's engines are enabled (nil: always).
func withScripts(check func(policy.ToolCall, packs.GuardCtx) (policy.Action, string, bool), re *regexp.Regexp, on func(costSettings) bool, what string) func(policy.ToolCall, packs.GuardCtx) (policy.Action, string, bool) {
	return func(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
		a, reason, handled := check(call, g)
		if handled || !call.IsShell() || (on != nil && !on(costOf(g.Settings()))) {
			return a, reason, handled
		}
		if p, ok := scriptNamesTool(g, call.Command, re); ok {
			return policy.Ask, "the script " + p + " names " + what + ", and the guard does not read scripts; confirm what it runs", true
		}
		return a, reason, handled
	}
}
