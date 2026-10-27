package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/web"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

const hubDefaultPort = 7470

type hubFlags struct {
	workspace string
	timeout   time.Duration
}

// hubExtra lets hub_serve.go add `serve` and `status`.
var hubExtra []func(*hubFlags) *cobra.Command

func newHubCmd() *cobra.Command {
	f := &hubFlags{}
	c := &cobra.Command{
		Use:   "hub",
		Short: "A local board that runs agent-ready cards through the build engine (docs/HUB.md)",
		Long: `tq hub keeps one board per client workspace ($TQ_HOME/hub/<workspace>/), runs agent-ready
cards headless through /tentaqles:build with the workspace's own environment, and shows every
decision a run waits on. You answer from the page, from tq hub answer, or in the terminal with
build-gate answer. Commands talk to a running tq hub serve when there is one, else to the store.`,
	}
	c.PersistentFlags().StringVar(&f.workspace, "workspace", "", "workspace name (default: the one containing the current folder)")
	c.PersistentFlags().DurationVar(&f.timeout, "timeout", 0, "give up after this long (skills pass 2s; 0 means no limit)")
	c.AddCommand(newHubAddCmd(f), newHubListCmd(f), newHubShowCmd(f), newHubMoveCmd(f), newHubRunCmd(f), newHubAnswerCmd(f),
		newHubReviewCmd(f), newHubOpenCmd(f), newHubCancelCmd(f), newHubCommentCmd(f))
	for _, mk := range hubExtra {
		c.AddCommand(mk(f))
	}
	return c
}

type hubConn struct {
	ws   *resolve.Workspace
	ops  runner.Ops
	svc  *runner.Service // direct mode
	info *serveinfo.Info // a hub serves this workspace
}

func (h *hubConn) close() {
	if h.svc != nil {
		h.svc.Store().Close()
	}
}

func warnTo(c *cobra.Command) func(string) {
	return func(s string) { fmt.Fprintln(c.ErrOrStderr(), "warning:", s) }
}

func openHub(c *cobra.Command, f *hubFlags) (*hubConn, context.Context, func(), error) {
	ctx := c.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	cancel := func() {}
	if f.timeout > 0 {
		var cf context.CancelFunc
		ctx, cf = context.WithTimeout(ctx, f.timeout)
		cancel = cf
	}
	if hubAgent() {
		ctx = runner.WithAgent(ctx)
	}
	ws, err := hubWorkspace(f.workspace)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	if info, ok := serveinfo.Find(ctx, ws.Name); ok {
		return &hubConn{ws: ws, ops: web.NewClient(info, ws.Name, hubAgent()), info: &info}, ctx, cancel, nil
	}
	svc, err := newHubService(ws, warnTo(c), true)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return &hubConn{ws: ws, ops: svc, svc: svc}, ctx, cancel, nil
}

func (h *hubConn) url(path string) (string, bool) {
	base := fmt.Sprintf("http://127.0.0.1:%d", hubDefaultPort)
	if h.info != nil {
		base = serveinfo.BaseURL(*h.info)
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return base + path + sep + "ws=" + url.QueryEscape(h.ws.Name), h.info != nil
}

func printJSON(c *cobra.Command, v any) error {
	enc := json.NewEncoder(c.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func newHubAddCmd(f *hubFlags) *cobra.Command {
	var body, bodyFile, kind, lane, parent, project, budget string
	var tags []string
	var asJSON bool
	c := &cobra.Command{
		Use:   `add "<title>"`,
		Short: "Add a card (agents may add cards; setting a budget is human-only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			// An agent's budget is refused by the service (or the hub it talks to), not here.
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			if bodyFile != "" {
				raw, err := os.ReadFile(bodyFile)
				if err != nil {
					return err
				}
				body = string(raw)
			}
			var b *store.Budget
			if budget != "" {
				bb, err := store.ParseBudget(budget, store.DefaultBudget)
				if err != nil {
					return err
				}
				b = &bb
			}
			card, err := h.ops.Add(ctx, store.NewCard{Title: args[0], Body: body, Kind: kind, Lane: lane, Tags: tags, ParentID: parent, ProjectDir: project}, b)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(c, card)
			}
			fmt.Fprintf(c.OutOrStdout(), "%s  %s  (%s, %s)\n", card.ID, card.Title, card.Kind, card.Lane)
			return nil
		},
	}
	c.Flags().StringVar(&body, "body", "", "card body (markdown)")
	c.Flags().StringVar(&bodyFile, "body-file", "", "read the body from a file")
	c.Flags().StringVar(&kind, "kind", "feature", "project | feature | fix | analysis | hotfix | chore")
	c.Flags().StringVar(&lane, "lane", "", "lane name (default: backlog)")
	c.Flags().StringArrayVar(&tags, "tag", nil, "tag (repeatable; agent-ready makes the card run when it enters in progress)")
	c.Flags().StringVar(&parent, "parent", "", "parent card id")
	c.Flags().StringVar(&project, "project", "", "project folder relative to the workspace root (the run's folder)")
	c.Flags().StringVar(&budget, "budget", "", "per-run budget: tokens=…,usd=…,minutes=…,rounds=…")
	c.Flags().BoolVar(&asJSON, "json", false, "print the card as JSON")
	return c
}

func newHubListCmd(f *hubFlags) *cobra.Command {
	var lane, kind, tag string
	var waiting, asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List cards, or with --waiting every decision waiting on you",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			if waiting {
				items, err := h.ops.Waiting(ctx)
				if err != nil {
					return err
				}
				if asJSON {
					return printJSON(c, items)
				}
				if len(items) == 0 {
					fmt.Fprintln(c.OutOrStdout(), "nothing is waiting on you")
				}
				for _, it := range items {
					fmt.Fprintf(c.OutOrStdout(), "%s  %s  %-13s %s: %s\n    options: %s   answer: %s\n",
						it.Card.ID, it.Decision.ID, it.Decision.Kind, it.Card.Title, firstLine(it.Decision.Text),
						strings.Join(it.Decision.Options, " | "), answerHint(it.Card.ID, it.Decision))
				}
				return nil
			}
			views, err := h.ops.List(ctx, store.CardFilter{Lane: lane, Kind: kind, Tag: tag}, false)
			if err != nil {
				return err
			}
			if asJSON {
				if views == nil {
					views = []runner.CardView{}
				}
				return printJSON(c, views)
			}
			tw := tabwriter.NewWriter(c.OutOrStdout(), 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "card\tlane\tkind\trun\twaiting\tcost\ttitle")
			for _, v := range views {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t$%.2f\t%s\n", v.ID, v.Lane, v.Kind, dash(v.RunStatus), v.Waiting, v.CostUSD, v.Title)
			}
			return tw.Flush()
		},
	}
	c.Flags().StringVar(&lane, "lane", "", "only this lane")
	c.Flags().StringVar(&kind, "kind", "", "only this kind")
	c.Flags().StringVar(&tag, "tag", "", "only cards with this tag")
	c.Flags().BoolVar(&waiting, "waiting", false, "list the decisions waiting on you instead of cards")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// answerHint is the tq hub answer line printed for a pending decision. The hub's own question
// needs the answer as its note, so its line carries --choice answer --note.
func answerHint(cardID string, d store.Decision) string {
	if d.Kind == store.KindQuestion && d.HubOwned() {
		return "tq hub answer " + cardID + " " + d.ID + ` --choice answer --note "<your answer>"   (or --choice stop)`
	}
	return "tq hub answer " + cardID + " " + d.ID + " --choice <option>"
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return l
}

func newHubShowCmd(f *hubFlags) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show <card>",
		Short: "Show a card: body, decisions, runs, comments",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			d, err := h.ops.Show(ctx, args[0])
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(c, d)
			}
			w := c.OutOrStdout()
			fmt.Fprintf(w, "%s  %s\n%s in %s", d.ID, d.Title, d.Kind, d.Lane)
			if d.ProjectDir != "" {
				fmt.Fprintf(w, " · project %s", d.ProjectDir)
			}
			if len(d.Tags) > 0 {
				fmt.Fprintf(w, " · tags %s", strings.Join(d.Tags, ", "))
			}
			fmt.Fprintf(w, " · cost $%.2f\n\n%s\n", d.CostUSD, strings.TrimSpace(store.StripFrontMatter(d.Body)))
			for _, dec := range d.Decisions {
				if dec.Status == store.DecisionPending {
					fmt.Fprintf(w, "\nwaiting on you: %s %s: %s\n  options: %s\n  answer: %s\n",
						dec.Kind, dec.ID, firstLine(dec.Text), strings.Join(dec.Options, " | "), answerHint(d.ID, dec))
				}
			}
			for _, r := range d.Runs {
				fmt.Fprintf(w, "\nrun %s %s: %d rounds, $%.2f", r.ID, r.Status, r.Rounds, r.CostUSD)
				if r.StopReason != "" {
					fmt.Fprintf(w, ", %s", r.StopReason)
				}
			}
			if len(d.Runs) > 0 {
				fmt.Fprintln(w)
			}
			start := 0
			if len(d.Comments) > 5 {
				start = len(d.Comments) - 5
			}
			for _, cm := range d.Comments[start:] {
				fmt.Fprintf(w, "\n[%s] %s: %s\n", cm.AuthorKind, cm.Author, cm.Body)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func newHubMoveCmd(f *hubFlags) *cobra.Command {
	var cancelRun bool
	c := &cobra.Command{
		Use:   `move <card> "<lane>"`,
		Short: "Move a card; into an active lane an agent-ready card starts a run",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			out, err := h.ops.Move(ctx, args[0], args[1], cancelRun)
			if err != nil {
				return err
			}
			w := c.OutOrStdout()
			if out.Cancelled != nil {
				fmt.Fprintf(w, "cancelled run %s\n", out.Cancelled.ID)
			}
			fmt.Fprintf(w, "moved %s to %s\n", out.Card.ID, out.Card.Lane)
			if out.Enqueued != nil {
				fmt.Fprintf(w, "queued run %s\n", out.Enqueued.ID)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&cancelRun, "cancel-run", false, "cancel the card's running or waiting run to move it")
	return c
}

func newHubRunCmd(f *hubFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "run <card>",
		Short: "Enqueue (or resume) a run for the card now",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			r, err := h.ops.Run(ctx, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "run %s %s\n", r.ID, r.Status)
			if h.svc != nil {
				fmt.Fprintln(c.OutOrStdout(), "no hub is serving this workspace: start one with tq hub serve to run it")
			}
			return nil
		},
	}
}

func newHubAnswerCmd(f *hubFlags) *cobra.Command {
	var choice, note string
	c := &cobra.Command{
		Use:   "answer <card> <decision> --choice C [--note TEXT]",
		Short: "Answer a decision a run is waiting on (human only; signed with a hub token)",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			// An answer the gate recorded can still come back with an error: print both.
			out, err := h.ops.Answer(ctx, args[0], args[1], choice, note)
			if out.Message != "" {
				fmt.Fprintln(c.OutOrStdout(), out.Message)
			}
			if out.Resumed != nil {
				fmt.Fprintf(c.OutOrStdout(), "resumed run %s\n", out.Resumed.ID)
			}
			return err
		},
	}
	c.Flags().StringVar(&choice, "choice", "", "one of the decision's options")
	c.Flags().StringVar(&note, "note", "", "note recorded with the answer")
	_ = c.MarkFlagRequired("choice")
	return c
}

func newHubReviewCmd(f *hubFlags) *cobra.Command {
	var open, html bool
	c := &cobra.Command{
		Use:   "review <card> [--open] [--html]",
		Short: "Print the review page URL, open it, or render it (--html) from gate records",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			if html {
				svc := h.svc
				if svc == nil {
					if svc, err = newHubService(h.ws, warnTo(c), false); err != nil {
						return err
					}
					defer svc.Store().Close()
				}
				d, err := web.BuildReview(ctx, svc, args[0])
				if err != nil {
					return err
				}
				d.Standalone = true
				return web.RenderReview(c.OutOrStdout(), d)
			}
			u, serving := h.url("/cards/" + url.PathEscape(args[0]) + "/review")
			fmt.Fprintln(c.OutOrStdout(), u)
			if !serving {
				fmt.Fprintf(c.ErrOrStderr(), "no hub is serving %s; start one with: tq hub serve\n", h.ws.Name)
			}
			if open && serving {
				return openBrowser(u)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&open, "open", false, "open the page in the browser")
	c.Flags().BoolVar(&html, "html", false, "render the review page to stdout (standalone HTML)")
	return c
}

func newHubOpenCmd(f *hubFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "open [<card>]",
		Short: "Open the board or a card in the browser",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			h, _, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			path := "/"
			if len(args) == 1 {
				path = "/cards/" + url.PathEscape(args[0])
			}
			u, serving := h.url(path)
			if !serving {
				return fmt.Errorf("no hub is serving %s; start one with: tq hub serve", h.ws.Name)
			}
			fmt.Fprintln(c.OutOrStdout(), u)
			return openBrowser(u)
		},
	}
}

func newHubCancelCmd(f *hubFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <run>",
		Short: "Cancel a run (the gate state stays; tq hub run resumes it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			r, err := h.ops.Cancel(ctx, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "run %s %s\n", r.ID, r.Status)
			return nil
		},
	}
}

// stdinIsTerminal reports whether r is an interactive terminal (a seam for tests).
var stdinIsTerminal = func(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func newHubCommentCmd(f *hubFlags) *cobra.Command {
	return &cobra.Command{
		Use:   `comment <card|-> ["<text>"|-]`,
		Short: "Comment on a card (- is the card in TQ_HUB_TASK; ship posts the PR link this way); no text or - reads it from stdin",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			card := args[0]
			if card == "-" {
				id, _, ok := hubCardFromTask()
				if !ok {
					return errors.New("tq hub comment -: TQ_HUB_TASK is not set (this session was not launched by the hub)")
				}
				card = id
			}
			// The workspace and its trust are settled before stdin is touched.
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			body := ""
			switch {
			case len(args) == 2 && args[1] != "-":
				body = args[1]
			case len(args) == 1 && stdinIsTerminal(c.InOrStdin()):
				return errors.New("no comment text: pass it as an argument or pipe it on stdin")
			default:
				raw, err := io.ReadAll(io.LimitReader(c.InOrStdin(), 1<<20))
				if err != nil {
					return err
				}
				body = strings.TrimSpace(string(raw))
			}
			if strings.TrimSpace(body) == "" {
				return errors.New("tq hub comment: the comment is empty")
			}
			cm, err := h.ops.Comment(ctx, card, body)
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "commented on %s (%s)\n", card, cm.AuthorKind)
			return nil
		},
	}
}
