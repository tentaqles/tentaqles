package insights

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// WriteJSON writes the stable JSON schema.
func WriteJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func kTok(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%dk", (n+500)/1000)
	}
	return fmt.Sprint(n)
}

func pct(f float64) string { return fmt.Sprintf("%.1f%%", f*100) }

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func countList(m map[string]int) string {
	if len(m) == 0 {
		return "-"
	}
	ks := sortedKeys(m)
	sort.SliceStable(ks, func(i, j int) bool { return m[ks[i]] > m[ks[j]] })
	parts := make([]string, 0, len(ks))
	for _, k := range ks {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

// WriteText renders the human report.
func WriteText(w io.Writer, r *Report) {
	fmt.Fprintf(w, "tq insights  %s .. %s (%.1f days)\n", r.Since.Format("2006-01-02"), r.Until.Format("2006-01-02"), r.WindowDays)
	rows := append(append([]IdentityReport(nil), r.Identities...), r.Total)

	fmt.Fprintln(w, "\n== Cost and tokens (estimate from list prices; recorded = Claude Code's cost-state)")
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "identity\tsessions\tinteractive\treviews\tsubagents\tcost est\trecorded\tin\tout\tcache read\tcache write\t>400k\tpeak ctx\t")
	for _, id := range rows {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t$%.2f\t$%.2f\t%s\t%s\t%s\t%s\t%d\t%s\t\n", id.Name, id.Sessions,
			id.SessionsByClass[ClassInteractive], id.SessionsByClass[ClassSecurityReview], id.SubagentTranscripts,
			id.CostUSD, id.RecordedCostUSD, kTok(id.Tokens.Input), kTok(id.Tokens.Output), kTok(id.Tokens.CacheRead),
			kTok(id.Tokens.CacheWrite), id.SessionsOver400k, kTok(id.PeakContextMax))
	}
	tw.Flush()
	if len(r.Total.CostByTier) > 0 {
		var parts []string
		for _, t := range sortedKeys(r.Total.CostByTier) {
			parts = append(parts, fmt.Sprintf("%s $%.2f", t, r.Total.CostByTier[t]))
		}
		fmt.Fprintf(w, "cost by tier: %s\n", strings.Join(parts, ", "))
	}

	writeTop := func(title string, s []SessionRow) {
		fmt.Fprintf(w, "\n== %s\n", title)
		if len(s) == 0 {
			fmt.Fprintln(w, "(none)")
			return
		}
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "identity\tsession\tproject\tclass\tpeak ctx\tcost\tduration\tsubagents")
		for _, x := range s {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t$%.2f\t%s\t%d\n", x.Identity, x.SessionID, short(x.Project, 48), x.Class,
				kTok(x.PeakContext), x.CostUSD, dur(x.DurationMin), x.Subagents)
		}
		tw.Flush()
	}
	writeTop("Largest sessions by peak context", r.TopByContext)
	writeTop("Largest sessions by cost", r.TopByCost)

	fmt.Fprintln(w, "\n== Shell health")
	tw = tabwriter.NewWriter(w, 0, 2, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "identity\tBash\tfail\tPowerShell\tfail\tall fail\theredoc EOF\twt refusals\texit 127\t")
	for _, id := range rows {
		s := id.Shell
		fmt.Fprintf(tw, "%s\t%d\t%s\t%d\t%s\t%s\t%d\t%d\t%d\t\n", id.Name, s.Bash.Calls, pct(s.BashFailureRate),
			s.PowerShell.Calls, pct(s.PowerShellFailureRate), pct(s.FailureRate), s.HeredocEOF, s.WorktreeRefusals, s.Exit127)
	}
	tw.Flush()

	g := r.Total.Guard
	fmt.Fprintln(w, "\n== Guard (from transcripts)")
	fmt.Fprintf(w, "denies: %s\n", countList(g.Deny))
	fmt.Fprintf(w, "asks:   %s\n", countList(g.Ask))
	fmt.Fprintf(w, "Stop-hook blocks: %d   hook errors: %s   hook timeouts: %s\n", g.StopBlocks, countList(g.HookErrors), countList(g.HookTimeouts))
	if len(r.Total.HookLatency) > 0 {
		var parts []string
		for _, ev := range sortedKeys(r.Total.HookLatency) {
			h := r.Total.HookLatency[ev]
			parts = append(parts, fmt.Sprintf("%s n=%d p50=%.0fms p95=%.0fms", ev, h.N, h.P50MS, h.P95MS))
		}
		fmt.Fprintf(w, "hook latency: %s\n", strings.Join(parts, "; "))
	}

	j := r.Jev
	fmt.Fprintln(w, "\n== Jev")
	fmt.Fprintf(w, "calls %d (cache hits %d, %s)  errors %d  cost $%.4f  latency p50 %dms p95 %dms  by purpose: %s\n",
		j.Log.Calls, j.Log.Cached, pct(j.Log.CacheHitRate), j.Log.Errors, j.Log.CostUSD, j.Log.P50MS, j.Log.P95MS, countList(j.Log.ByPurpose))
	for _, k := range sortedKeys(j.Kinds) {
		kj := j.Kinds[k]
		fmt.Fprintf(w, "%s: %d judgments, %d errors, modes: %s\n", k, kj.Lines, kj.Errors, countList(kj.Modes))
	}
	if len(j.Rules) > 0 {
		tw = tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "kind\trule\tevaluated\twould ask\twould deny\tapplied ask\tapplied deny\tenforced")
		for _, rj := range j.Rules {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%v\n", rj.Kind, rj.Rule, rj.Evaluated, rj.WouldAsk, rj.WouldDeny, rj.AppliedAsk, rj.AppliedDeny, rj.Enforced)
		}
		tw.Flush()
	}
	if j.Route.Decisions > 0 {
		fmt.Fprintf(w, "route: %d decisions, %d errors, picks: %s; cheaper than parent %d, applied %d, est. savings if applied $%.2f (avg subagent $%.2f)\n",
			j.Route.Decisions, j.Route.Errors, countList(j.Route.Picks), j.Route.Cheaper, j.Route.Applied, j.Route.EstSavingsUSD, j.Route.AvgSubagentCostUSD)
	}
	if j.Triage.Low+j.Triage.High > 0 {
		fmt.Fprintf(w, "triage: low %d, high %d, flags: %s\n", j.Triage.Low, j.Triage.High, countList(j.Triage.Flags))
	}

	fmt.Fprintln(w, "\n== Background review sessions")
	fmt.Fprintf(w, "security-review sessions: %d (%d on Opus), cost $%.2f\n", r.Total.Reviews.Sessions, r.Total.Reviews.Opus, r.Total.Reviews.CostUSD)

	fmt.Fprintln(w, "\n== Worktrees")
	if r.Worktrees == nil {
		fmt.Fprintln(w, "(not scanned)")
	} else {
		fmt.Fprintf(w, "%d linked worktrees, %d prunable (tq worktrees list)\n", r.Worktrees.Total, r.Worktrees.Prunable)
	}

	fmt.Fprintln(w, "\n== 30-day targets")
	tw = tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "status\ttarget\tnow\tbaseline")
	for _, t := range r.Targets {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", t.Status, t.Goal, t.Display, t.Baseline)
	}
	tw.Flush()
}

func short(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n+1:]
}

func dur(min float64) string {
	switch {
	case min >= 48*60:
		return fmt.Sprintf("%.1fd", min/60/24)
	case min >= 60:
		return fmt.Sprintf("%.1fh", min/60)
	}
	return fmt.Sprintf("%.0fm", min)
}
