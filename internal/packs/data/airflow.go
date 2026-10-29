package data

import (
	"context"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
)

// data/airflow-dag (spec C §3.4): Airflow DAG file rules.
const (
	ruleAirflowStartDate = "airflow-dynamic-start-date"
	ruleAirflowCatchup   = "airflow-catchup-unset"
	ruleAirflowRetries   = "airflow-no-retries"
	ruleAirflowTopIO     = "airflow-top-level-io"
)

var (
	airflowImportRe = regexp.MustCompile(`(?m)^[ \t]*(?:from|import)[ \t]+airflow\b`)
	dynamicTimeRe   = regexp.MustCompile(`\b(?:datetime\s*\.\s*(?:now|utcnow|today)|date\s*\.\s*today|days_ago|pendulum\s*\.\s*(?:now|today)|timezone\s*\.\s*utcnow)\s*\(`)
	readIORe        = regexp.MustCompile(`^(?:pd|pandas|pl|polars)\.read_`)
)

func runAirflowDAG(_ context.Context, in *packs.Input) packs.Result {
	sel := loadUnits(in)
	var fs findings
	n := 0
	for _, u := range sel.units {
		if u.kind != "python" || !airflowImportRe.Match(u.src) {
			continue
		}
		n++
		airflowRules(u, &fs)
	}
	return result(n, "DAG file", sel.skipped, &fs)
}

func airflowRules(u *unit, fs *findings) {
	lines := scan.PythonLines(u.src)
	dagLine := 0
	catchup, retries := false, false
	tainted := wallClockNames(lines)
	for _, l := range lines {
		hasStart := strings.Contains(l.Code, "start_date") || containsString(l.Strings, "start_date")
		if hasStart && (dynamicTimeRe.MatchString(l.Code) || mentionsAny(l.Code, tainted)) {
			fs.add(u, l.Line, packs.LevelError, ruleAirflowStartDate,
				"start_date is computed from the wall clock; use a fixed date (pendulum.datetime(2026, 1, 1, tz=\"UTC\")) so runs and backfills are stable")
		}
		if containsString(l.Strings, "retries") {
			retries = true
		}
		if containsString(l.Strings, "catchup") {
			catchup = true
		}
		for _, c := range scan.LineCalls(l) {
			if c.Name == "DAG" || c.Name == "dag" {
				if dagLine == 0 {
					dagLine = l.Line
				}
				if _, ok := c.Kwargs["catchup"]; ok {
					catchup = true
				}
			}
			if _, ok := c.Kwargs["retries"]; ok {
				retries = true
			}
			if l.TopLevel() && topLevelIO(c) {
				fs.add(u, l.Line, packs.LevelWarn, ruleAirflowTopIO,
					"I/O at module level ("+c.Func+") runs every time the scheduler parses the DAG file; move it into a task")
			}
		}
	}
	if dagLine == 0 {
		return // helpers and plugins that import airflow but define no DAG
	}
	if !catchup {
		fs.add(u, dagLine, packs.LevelWarn, ruleAirflowCatchup, "catchup is not set explicitly; set catchup=False (or True with a reviewed backfill plan)")
	}
	if !retries {
		fs.add(u, dagLine, packs.LevelWarn, ruleAirflowRetries, "no retries in default_args or on any task; set retries and retry_delay")
	}
}

// wallClockNames returns the names assigned from the wall clock
// (`YESTERDAY = datetime.now() - timedelta(days=1)`), transitively
// (`START = YESTERDAY.replace(hour=0)`), so a start_date that references
// one is dynamic too. A line naming start_date is not an assignment source.
func wallClockNames(lines []scan.PyLine) map[string]bool {
	tainted := map[string]bool{}
	// A few passes catch chains written in any order; each pass is linear.
	for pass, changed := 0, true; changed && pass < 4; pass++ {
		changed = false
		for _, l := range lines {
			m := pyAssignRe.FindStringSubmatch(l.Code)
			if m == nil || tainted[m[1]] || strings.Contains(l.Code, "start_date") || containsString(l.Strings, "start_date") {
				continue
			}
			rhs := l.Code[len(m[0])-1:]
			if dynamicTimeRe.MatchString(rhs) || mentionsAny(rhs, tainted) {
				tainted[m[1]] = true
				changed = true
			}
		}
	}
	return tainted
}

// mentionsAny reports whether code holds any of the names as a whole word.
func mentionsAny(code string, names map[string]bool) bool {
	for v := range names {
		if wordIn(code, v) {
			return true
		}
	}
	return false
}

func topLevelIO(c scan.Call) bool {
	f := c.Func
	switch {
	case strings.HasPrefix(f, "requests."), f == "urlopen", strings.HasSuffix(f, ".urlopen"):
		return true
	case readIORe.MatchString(f):
		return true
	case c.Name == "connect" && c.Receiver != "" && !strings.HasPrefix(c.Receiver, "."):
		return true
	case c.Name == "create_engine":
		return true
	}
	return false
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
