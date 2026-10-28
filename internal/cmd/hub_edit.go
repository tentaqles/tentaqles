package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func init() { hubExtra = append(hubExtra, newHubEditCmd) }

// cardUpdater is the edit half of a hub connection: the runner's Service (direct mode) and the web
// Client (a hub serves the workspace) both have it.
type cardUpdater interface {
	UpdateCard(ctx context.Context, id string, u store.CardUpdate) (store.Card, error)
}

// newHubEditCmd is `tq hub edit`. The runner decides who may change what: an agent session only the
// tags, and only on a card in a planning lane (backlog, todo, other); title, body and project folder
// are the human's. Adding agent-ready starts nothing; a run starts when a card enters an active lane.
func newHubEditCmd(f *hubFlags) *cobra.Command {
	var title, body, bodyFile, project string
	var addTags, removeTags []string
	var asJSON bool
	c := &cobra.Command{
		Use:   "edit <card> [--title T] [--body TEXT|--body-file F] [--project DIR] [--add-tag T]... [--remove-tag T]...",
		Short: "Edit a card (agents may change only the tags, and only on a card in a planning lane)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			fl := c.Flags()
			if fl.Changed("body") && fl.Changed("body-file") {
				return errors.New("tq hub edit: pass --body or --body-file, not both")
			}
			addTags, removeTags = cleanTags(addTags), cleanTags(removeTags)
			var u store.CardUpdate
			if fl.Changed("title") {
				u.Title = &title
			}
			if fl.Changed("body") {
				u.Body = &body
			}
			if fl.Changed("project") {
				u.ProjectDir = &project
			}
			if u.Title == nil && u.Body == nil && !fl.Changed("body-file") && u.ProjectDir == nil && len(addTags) == 0 && len(removeTags) == 0 {
				return errors.New("tq hub edit: nothing to edit: pass --title, --body, --body-file, --project, --add-tag or --remove-tag")
			}
			h, ctx, cancel, err := openHub(c, f)
			if err != nil {
				return err
			}
			defer cancel()
			defer h.close()
			if fl.Changed("body-file") {
				raw, err := os.ReadFile(bodyFile)
				if err != nil {
					return err
				}
				s := string(raw)
				u.Body = &s
			}
			up, ok := h.ops.(cardUpdater)
			if !ok {
				return errors.New("tq hub edit: this hub connection cannot edit cards")
			}
			if len(addTags) > 0 || len(removeTags) > 0 {
				// Edit against the stored list, so tags someone else added meanwhile are kept.
				d, err := h.ops.Show(ctx, args[0])
				if err != nil {
					return err
				}
				tags := []string{}
				for _, t := range d.Tags {
					if !slices.Contains(removeTags, t) {
						tags = append(tags, t)
					}
				}
				for _, t := range addTags {
					if !slices.Contains(tags, t) {
						tags = append(tags, t)
					}
				}
				u.Tags = &tags
			}
			card, err := up.UpdateCard(ctx, args[0], u)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(c, card)
			}
			fmt.Fprintf(c.OutOrStdout(), "edited %s  %s  (tags: %s)\n", card.ID, card.Title, dash(strings.Join(card.Tags, ", ")))
			return nil
		},
	}
	c.Flags().StringVar(&title, "title", "", "new title (human-only)")
	c.Flags().StringVar(&body, "body", "", "new body, markdown (human-only)")
	c.Flags().StringVar(&bodyFile, "body-file", "", "read the new body from a file (human-only)")
	c.Flags().StringVar(&project, "project", "", "new project folder relative to the workspace root (human-only)")
	c.Flags().StringArrayVar(&addTags, "add-tag", nil, "add a tag (repeatable; agents may, on a card in a planning lane)")
	c.Flags().StringArrayVar(&removeTags, "remove-tag", nil, "remove a tag (repeatable; agents may, on a card in a planning lane)")
	c.Flags().BoolVar(&asJSON, "json", false, "print the card as JSON")
	return c
}

func cleanTags(in []string) []string {
	out := []string{}
	for _, t := range in {
		if t = strings.TrimSpace(t); t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}
