package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/web"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Board sync commands (spec B §8): tq hub sync now|status|outbox and tq hub push. They work on the
// workspace's store directly; a lease in the store keeps them and a serving hub from sending the
// same outbox row twice.

func init() { hubExtra = append(hubExtra, newHubSyncCmd, newHubPushCmd) }

// syncAdapterHook replaces the tracker adapter (tests); nil = the credential chain and the
// registered provider.
var syncAdapterHook func(ctx context.Context, ws *resolve.Workspace) (hubsync.Adapter, error)

// syncExitClasses are the classes outbound tracker text is redacted with: the classes the board
// masks stored text with (every detector without a data block; the data block's classes plus
// secrets with one). The engine strips the tq:allow-pii marker, so a line the marker kept
// unmasked in the store is redacted with these on its way to the tracker.
func syncExitClasses(ws *resolve.Workspace) []string {
	return hubClasses(ws)
}

func newSyncEngine(ws *resolve.Workspace, st *store.Store, warn func(string)) (*hubsync.Engine, error) {
	if ws == nil || !ws.Manifest.HasTracker() {
		return nil, hubsync.ErrNoTracker
	}
	cfg := hubsync.EngineConfig{Workspace: ws.Name, Store: st, Tracker: ws.Manifest.Tracker, ExitClasses: syncExitClasses(ws),
		Env:  func() []string { return envplan.Environ(ws, os.Environ()) },
		Logf: func(f string, a ...any) { warn(fmt.Sprintf(f, a...)) }}
	if syncAdapterHook != nil {
		cfg.Adapter = func(ctx context.Context) (hubsync.Adapter, error) { return syncAdapterHook(ctx, ws) }
	}
	return hubsync.NewEngine(cfg)
}

// syncSibling runs the engine beside each opened workspace's runner (tq hub serve). Approvals in
// this process wake it at once; everything else it reads from the activity log.
func syncSibling(warn func(string)) web.Sibling {
	return func(ws *resolve.Workspace, svc *runner.Service) func(context.Context) {
		if ws == nil || !ws.Manifest.HasTracker() {
			return nil
		}
		e, err := newSyncEngine(ws, svc.Store(), warn)
		if err != nil {
			warn(ws.Name + ": sync: " + err.Error())
			return nil
		}
		prev := svc.OnApprove
		svc.OnApprove = func(c store.Card, r store.Run) {
			if prev != nil {
				prev(c, r)
			}
			e.Wake()
		}
		return e.Run
	}
}

// openSync resolves the workspace and opens its store and engine.
func openSync(c *cobra.Command, f *hubFlags, existing bool) (*hubsync.Engine, *store.Store, context.Context, func(), error) {
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
	fail := func(err error) (*hubsync.Engine, *store.Store, context.Context, func(), error) {
		cancel()
		return nil, nil, nil, nil, err
	}
	ws, err := hubWorkspace(f.workspace)
	if err != nil {
		return fail(err)
	}
	if !ws.Manifest.HasTracker() {
		return fail(fmt.Errorf("workspace %s has no tracker block in its manifest; add one (docs/HUB.md) or run tq onboard", ws.Name))
	}
	var st *store.Store
	if existing {
		st, err = store.OpenExisting(ws.Name, hubClasses(ws))
		if errors.Is(err, store.ErrNoHub) {
			return fail(fmt.Errorf("workspace %s has no board yet; run tq hub sync now first", ws.Name))
		}
	} else {
		st, err = store.Open(ws.Name, hubClasses(ws))
	}
	if err != nil {
		return fail(err)
	}
	e, err := newSyncEngine(ws, st, warnTo(c))
	if err != nil {
		st.Close()
		return fail(err)
	}
	return e, st, ctx, func() { st.Close(); cancel() }, nil
}

func newHubSyncCmd(f *hubFlags) *cobra.Command {
	c := &cobra.Command{
		Use:   "sync now|status|outbox",
		Short: "Mirror the company tracker into the board and send queued writes back (spec B)",
		Long: `tq hub sync mirrors the workspace's tracker (the manifest's tracker block) into the board. A
serving hub polls every sync.interval seconds; tq hub sync now runs one pass here. Writes back to
the tracker go through the outbox under the sync.policy.* settings: auto sends, ask waits for a
send/skip decision on the board, off drops the write.`,
	}
	c.AddCommand(newHubSyncNowCmd(f), newHubSyncStatusCmd(f), newHubSyncOutboxCmd(f), newHubSyncRetryCmd(f))
	return c
}

func newHubSyncRetryCmd(f *hubFlags) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "retry <outbox-id> [--json]",
		Short: "Put a failed outbox row back to pending and send it (human only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if hubAgent() {
				return fmt.Errorf("tq hub sync retry: %w; ask the human to retry the write", runner.ErrHumanOnly)
			}
			e, st, ctx, done, err := openSync(c, f, true)
			if err != nil {
				return err
			}
			defer done()
			row, err := e.Retry(args[0])
			if err != nil {
				return err
			}
			// Send now when no other process holds the lease (else the serving hub does).
			_, ferr := e.Flush(ctx)
			if cur, err := st.OutboxRow(row.ID); err == nil {
				row = cur
			}
			if asJSON {
				return printJSON(c, row)
			}
			w := c.OutOrStdout()
			switch {
			case row.Status == store.OutboxSent:
				fmt.Fprintf(w, "sent %s (%s)\n", row.ID, row.Action)
			case row.DecisionID != "" && row.Status == store.OutboxPending:
				fmt.Fprintf(w, "%s is pending: waiting for the send/skip decision %s\n", row.ID, row.DecisionID)
			default:
				fmt.Fprintf(w, "%s is %s", row.ID, row.Status)
				if ferr != nil {
					fmt.Fprintf(w, ": %v", ferr)
				} else if row.LastError != "" {
					fmt.Fprintf(w, ": %s", row.LastError)
				}
				fmt.Fprintln(w)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func printSyncReport(c *cobra.Command, rep hubsync.Report) {
	w := c.OutOrStdout()
	fmt.Fprintf(w, "synced %s (%s): pulled %d, created %d, updated %d, moved %d, conflicts %d\n",
		rep.Provider, rep.Scope, rep.Pulled, rep.Created, rep.Updated, rep.Moved, rep.Conflicts)
	fmt.Fprintf(w, "outbox: queued %d, sent %d, asked %d, skipped %d, dropped %d, failed %d", rep.Queued, rep.Sent, rep.Asked,
		rep.Skipped, rep.Dropped, rep.Failed)
	if rep.Retried > 0 {
		fmt.Fprintf(w, ", retried %d", rep.Retried)
	}
	fmt.Fprintln(w)
	if rep.Asked > 0 {
		fmt.Fprintln(w, "answer the send/skip decisions on the board, or with tq hub answer <card> <decision> --choice send|skip")
	}
}

func newHubSyncNowCmd(f *hubFlags) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "now [--json]",
		Short: "Run one sync pass now: pull changed items, queue hub events, send the outbox",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			e, _, ctx, done, err := openSync(c, f, false)
			if err != nil {
				return err
			}
			defer done()
			rep, err := e.Tick(ctx)
			if asJSON {
				if err != nil && rep.Error == "" {
					rep.Error = err.Error()
				}
				if perr := printJSON(c, rep); perr != nil {
					return perr
				}
				return err
			}
			if err != nil && (errors.Is(err, hubsync.ErrBusy) || errors.Is(err, hubsync.ErrDisabled)) {
				return err
			}
			printSyncReport(c, rep)
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func newHubSyncStatusCmd(f *hubFlags) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status [--json]",
		Short: "Show the sync state: provider, last poll, last error, outbox counts, policy and lane map",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			e, _, _, done, err := openSync(c, f, true)
			if err != nil {
				return err
			}
			defer done()
			ov, err := e.Overview()
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(c, ov)
			}
			w := c.OutOrStdout()
			fmt.Fprintf(w, "%s: %s (%s), auth %s, sync %s, every %ds\n", ov.Workspace, ov.Provider, ov.Scope, ov.Auth,
				map[bool]string{true: "on", false: "off"}[ov.Enabled], ov.Settings.Interval)
			if len(ov.States) == 0 {
				fmt.Fprintln(w, "never synced: run tq hub sync now")
			}
			for _, s := range ov.States {
				fmt.Fprintf(w, "last ok %s", dash(s.LastOKAt))
				if s.LastError != "" {
					fmt.Fprintf(w, "; last error %s: %s", s.LastErrorAt, s.LastError)
				}
				fmt.Fprintln(w)
			}
			if ov.NextAt != "" {
				fmt.Fprintf(w, "next poll after %s (when a hub is serving)\n", ov.NextAt)
			}
			var parts []string
			for _, s := range store.OutboxStatuses {
				parts = append(parts, fmt.Sprintf("%s %d", s, ov.Outbox[s]))
			}
			fmt.Fprintf(w, "outbox: %s\n", strings.Join(parts, ", "))
			parts = parts[:0]
			for _, a := range store.OutboxActions {
				parts = append(parts, a+"="+ov.Settings.Policy.Get(a))
			}
			fmt.Fprintf(w, "policy: %s\n", strings.Join(parts, " "))
			if len(ov.LaneMap) > 0 {
				parts = parts[:0]
				for _, m := range ov.LaneMap {
					parts = append(parts, fmt.Sprintf("%s -> %s", m.ExternalStatus, m.Lane))
				}
				fmt.Fprintf(w, "lanes: %s\n", strings.Join(parts, ", "))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func newHubSyncOutboxCmd(f *hubFlags) *cobra.Command {
	var asJSON bool
	var status, card string
	var limit int
	c := &cobra.Command{
		Use:   "outbox [--status S] [--card C] [--json]",
		Short: "List the writes queued for the tracker (pending, sent, skipped, failed)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if status != "" && !slices.Contains(store.OutboxStatuses, status) {
				return fmt.Errorf("--status: use one of %s", strings.Join(store.OutboxStatuses, ", "))
			}
			_, st, _, done, err := openSync(c, f, true)
			if err != nil {
				return err
			}
			defer done()
			rows, err := st.ListOutbox(store.OutboxFilter{Status: status, CardID: card, Limit: limit})
			if err != nil {
				return err
			}
			if rows == nil {
				rows = []store.OutboxRow{}
			}
			if asJSON {
				return printJSON(c, rows)
			}
			if len(rows) == 0 {
				fmt.Fprintln(c.OutOrStdout(), "the outbox is empty")
				return nil
			}
			tw := tabwriter.NewWriter(c.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tCARD\tACTION\tSTATUS\tTRIES\tDECISION\tCREATED\tLAST ERROR")
			for _, o := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n", o.ID, o.CardID, o.Action, o.Status, o.Attempts, dash(o.DecisionID),
					o.CreatedAt, o.LastError)
			}
			return tw.Flush()
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	c.Flags().StringVar(&status, "status", "", "only rows with this status (pending, sent, skipped, failed)")
	c.Flags().StringVar(&card, "card", "", "only rows of this card")
	c.Flags().IntVar(&limit, "limit", 0, "at most this many rows (0 = all)")
	return c
}

func newHubPushCmd(f *hubFlags) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "push <card> [--json]",
		Short: "Push a local card to the company board (a create_card write under sync.policy.create_card; human only)",
		Long: `tq hub push queues the card for the company tracker as a create_card outbox row. It becomes a
child of the parent card's item when the parent is linked. Under the default policy (ask) the
board shows a send/skip decision first. The title and body are redacted on the way out.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if hubAgent() {
				return fmt.Errorf("tq hub push: %w; ask the human to push the card", runner.ErrHumanOnly)
			}
			e, st, ctx, done, err := openSync(c, f, false)
			if err != nil {
				return err
			}
			defer done()
			row, err := e.Push(ctx, args[0])
			if err != nil {
				return err
			}
			// Apply the policy now when no other process holds the lease (else the serving hub does).
			_, ferr := e.Flush(ctx)
			if cur, err := st.OutboxRow(row.ID); err == nil {
				row = cur
			}
			if asJSON {
				return printJSON(c, row)
			}
			w := c.OutOrStdout()
			switch {
			case row.Status == store.OutboxSent:
				cd, _ := st.Card(row.CardID)
				fmt.Fprintf(w, "pushed %s: %s %s %s\n", row.CardID, cd.ExternalProvider, cd.ExternalID, cd.ExternalURL)
			case row.DecisionID != "":
				fmt.Fprintf(w, "queued %s (outbox %s): waiting for the send/skip decision %s on the board\n", row.CardID, row.ID, row.DecisionID)
			case row.Status == store.OutboxSkipped:
				fmt.Fprintf(w, "not pushed (outbox %s): %s\n", row.ID, row.LastError)
			default:
				fmt.Fprintf(w, "queued %s (outbox %s)", row.CardID, row.ID)
				if ferr != nil {
					fmt.Fprintf(w, ": %v", ferr)
				}
				fmt.Fprintln(w)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}
