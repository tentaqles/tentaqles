package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
)

// The MCP fallback commands (spec B §8, §9, §12): when no background credential works, the
// /tentaqles:sync skill reads the tracker with the agent's MCP connector, feeds the items to
// tq hub sync import, lists the rows it may deliver with tq hub sync pending, sends each with the
// connector and reports back with tq hub sync ack. Agents may run all three, within limits: an
// import only into planning lanes and only of linked cards; an ack only of rows the policy sends
// (auto) or the human approved.

// maxImportBytes bounds what import reads.
const maxImportBytes = 8 << 20

func newHubSyncImportCmd(f *hubFlags) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "import [-|<file>] [--json]",
		Short: "Mirror tracker items read by an MCP connector into linked cards (the MCP fallback)",
		Long: `tq hub sync import reads tracker items as JSON (from stdin with -, or a file): a bare array of
items, or {"items": [...], "statuses": [{"name": "In Progress", "category": "active"}], "watermark": "<RFC 3339>"}.
An item has id, title and optionally key, url, body, status, status_category (backlog, todo,
active, review, done), parent_id, rev, acceptance and labels. Statuses rebuild the lane map.

An agent session may import only into planning lanes (backlog, todo, other): a new item whose
status maps to another lane starts in the backlog, and a move out of or into such a lane is held
for the human, and so is the text of a linked card outside a planning lane or held by a run.
Its statuses only fill gaps in the lane map (never a done lane). It creates or updates linked
cards only; it never changes a local card. The watermark is clamped to now at most.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			src := "-"
			if len(args) == 1 {
				src = args[0]
			}
			raw, err := readImport(c, src)
			if err != nil {
				return err
			}
			in, err := hubsync.ParseImport(raw)
			if err != nil {
				return err
			}
			e, _, _, done, err := openSync(c, f, false)
			if err != nil {
				return err
			}
			defer done()
			rep, ierr := e.Import(in, hubAgent())
			if asJSON {
				if err := printJSON(c, rep); err != nil {
					return err
				}
				return ierr
			}
			if errors.Is(ierr, hubsync.ErrBusy) || errors.Is(ierr, hubsync.ErrDisabled) {
				return ierr
			}
			w := c.OutOrStdout()
			fmt.Fprintf(w, "imported %s (%s): created %d, updated %d, moved %d, held %d, refused %d\n", rep.Provider, rep.Scope,
				rep.Created, rep.Updated, rep.Moved, rep.Held, rep.Refused)
			for _, it := range rep.Items {
				if it.Note != "" {
					fmt.Fprintf(w, "  %s %s: %s\n", it.ID, it.Result, it.Note)
				}
			}
			return ierr
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func readImport(c *cobra.Command, src string) ([]byte, error) {
	var r io.Reader
	if src == "-" {
		r = c.InOrStdin()
	} else {
		fh, err := os.Open(src)
		if err != nil {
			return nil, err
		}
		defer fh.Close()
		r = fh
	}
	raw, err := io.ReadAll(io.LimitReader(r, maxImportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxImportBytes {
		return nil, fmt.Errorf("import: more than %d bytes", maxImportBytes)
	}
	return raw, nil
}

func newHubSyncPendingCmd(f *hubFlags) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "pending [--json]",
		Short: "Apply the write policy and list the outbox rows that may be sent now, with their redacted text (the MCP fallback)",
		Long: `tq hub sync pending queues new hub events, applies the sync.policy.* settings to the pending
outbox rows (ask rows get their send/skip decision on the board, off rows are dropped) and lists
the rows that may be sent now: auto rows and ask rows the human answered send. The text is shown
exactly as it may leave the machine (redacted). It never calls the tracker. Send each row with
the connector, then run tq hub sync ack.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			e, _, _, done, err := openSync(c, f, true)
			if err != nil {
				return err
			}
			defer done()
			rows, rep, err := e.Sendable()
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(c, rows)
			}
			w := c.OutOrStdout()
			if rep.Asked > 0 {
				fmt.Fprintf(w, "%d write(s) wait for the human's send/skip decision on the board\n", rep.Asked)
			}
			if len(rows) == 0 {
				fmt.Fprintln(w, "nothing to send")
				return nil
			}
			tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tCARD\tACTION\tITEM\tTO")
			for _, s := range rows {
				to := s.Lane
				if len(s.Statuses) > 0 {
					to = strings.Join(s.Statuses, " | ")
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.CardID, s.Action, dash(s.ExternalID), dash(to))
			}
			return tw.Flush()
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func newHubSyncAckCmd(f *hubFlags) *cobra.Command {
	var asJSON, sent bool
	var failed, status, itemID, url string
	c := &cobra.Command{
		Use:   `ack <outbox-id> --sent|--failed "<why>" [--status S] [--item-id ID --url U] [--json]`,
		Short: "Record that the MCP fallback sent an outbox row, or why it failed",
		Long: `tq hub sync ack records the outcome of an outbox row the /tentaqles:sync skill delivered with
an MCP connector. --sent marks it sent: for a move, --status names the tracker status the item is
in now; for create_card, --item-id (and --url) name the new item, and the card is linked to it.
--failed "<why>" marks it failed (tq hub sync retry puts it back).

An agent session may ack only rows the policy sends (auto) or the human approved (an ask
answered send). It never answers a decision.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			failedSet := c.Flags().Changed("failed")
			if sent == failedSet {
				return errors.New("tq hub sync ack: give exactly one of --sent or --failed \"<why>\"")
			}
			if failedSet && strings.TrimSpace(failed) == "" {
				return errors.New("--failed: say why the write failed")
			}
			e, _, _, done, err := openSync(c, f, true)
			if err != nil {
				return err
			}
			defer done()
			row, err := e.Ack(args[0], hubsync.AckInput{Sent: sent, Why: failed, Status: status, ItemID: itemID, URL: url}, hubAgent())
			if errors.Is(err, hubsync.ErrNotAllowed) {
				return fmt.Errorf("%w; the human answers its send/skip decision on the board first", err)
			}
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(c, row)
			}
			fmt.Fprintf(c.OutOrStdout(), "%s (%s) is %s\n", row.ID, row.Action, row.Status)
			return nil
		},
	}
	c.Flags().BoolVar(&sent, "sent", false, "the write reached the tracker")
	c.Flags().StringVar(&failed, "failed", "", "the write failed: why")
	c.Flags().StringVar(&status, "status", "", "a move: the tracker status the item is in now")
	c.Flags().StringVar(&itemID, "item-id", "", "create_card: the id of the item the connector created")
	c.Flags().StringVar(&url, "url", "", "create_card: the url of the new item")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}
