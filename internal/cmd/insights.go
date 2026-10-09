package cmd

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/insights"
)

func newInsightsCmd() *cobra.Command {
	var (
		since       string
		wss         []string
		asJSON      bool
		top         int
		noWorktrees bool
	)
	c := &cobra.Command{
		Use:   "insights",
		Short: "Report Claude Code cost, shell health, guard and Jev outcomes across identities (read-only)",
		Long: `Streams the Claude Code transcripts of every tq identity (and ~/.claude
for the default setup) plus $TQ_HOME/decide/{log,judgments}.jsonl, and
reports aggregates only: cost and tokens, the largest sessions, shell
failure rates, guard denies/asks per rule, Stop-hook blocks, Jev calls and
judgments, background security-review sessions and worktrees, next to the
workflow plan's 30-day targets. Nothing is written and no transcript
content is printed. See docs/INSIGHTS.md.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			now := time.Now().UTC()
			from, err := parseSince(since, now)
			if err != nil {
				return err
			}
			rep, err := insights.Run(insights.Options{Since: from, Until: now, Workspaces: wss, Top: top})
			if err != nil {
				return err
			}
			if !noWorktrees {
				keep := wsFilter(wss)
				if wts, err := scanWorktreesIn(keep); err == nil {
					wc := &insights.WorktreeCount{Total: len(wts)}
					for _, w := range wts {
						if w.prunable(3 * 24 * time.Hour) {
							wc.Prunable++
						}
					}
					rep.Worktrees = wc
				}
			}
			if asJSON {
				return insights.WriteJSON(c.OutOrStdout(), rep)
			}
			insights.WriteText(c.OutOrStdout(), rep)
			return nil
		},
	}
	c.Flags().StringVar(&since, "since", "30d", "window start: a duration like 7d, 30d, 12h, or a date YYYY-MM-DD")
	c.Flags().StringSliceVar(&wss, "ws", nil, "only these identities/workspaces (repeatable or comma-separated; \"default\" is ~/.claude)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the stable JSON schema (tq.insights/v1)")
	c.Flags().IntVar(&top, "top", 10, "how many sessions to list in the largest-session tables")
	c.Flags().BoolVar(&noWorktrees, "no-worktrees", false, "skip the git worktree scan")
	return c
}

func wsFilter(wss []string) func(string) bool {
	set := map[string]bool{}
	for _, w := range wss {
		for _, p := range strings.Split(w, ",") {
			if p = strings.TrimSpace(p); p != "" {
				set[strings.ToLower(p)] = true
			}
		}
	}
	if len(set) == 0 {
		return nil
	}
	return func(ws string) bool { return set[strings.ToLower(ws)] }
}

var reSinceDur = regexp.MustCompile(`^(\d+)([hdw])$`)

// parseSince accepts 7d / 30d / 12h / 2w or a YYYY-MM-DD date (UTC).
func parseSince(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if m := reSinceDur.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		unit := map[string]time.Duration{"h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour}[m[2]]
		return now.Add(-time.Duration(n) * unit), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		if t.After(now) {
			return time.Time{}, fmt.Errorf("--since %s is in the future", s)
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("--since %q: use a duration like 7d, 30d, 12h, 2w or a date YYYY-MM-DD", s)
}
