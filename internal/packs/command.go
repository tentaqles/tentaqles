package packs

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
)

type inputKey struct{}

type commandInput struct {
	in  *Input
	err error
}

// WithInput stores the run context of a pack subcommand in ctx. The CLI's
// `tq pack <pack>` parent calls it before any subcommand runs.
func WithInput(ctx context.Context, in *Input, err error) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, inputKey{}, commandInput{in, err})
}

// ErrNoCommandInput is returned by FromCommand outside `tq pack <pack>`.
var ErrNoCommandInput = errors.New("packs: the command is not running under tq pack <pack>")

// FromCommand returns the run context of a pack subcommand: the Input bound
// to the pack (in.Workspace(), in.WorkspaceRoot(), in.StateDir(),
// in.Settings(), in.Config, in.Manifest, in.Classes(), in.Root, in.Git,
// in.Tools). The changed set is empty (subcommands select their own files).
//
// It returns an error, and the command must stop, when the folder's
// manifest is invalid or untrusted, or tq-packs.yaml is invalid (a config
// error: the CLI exits 2). Outside a workspace it returns an Input with no
// workspace and no pack enabled. Count lines are attributed to the
// workspace.
func FromCommand(cmd *cobra.Command) (*Input, error) {
	for c := cmd; c != nil; c = c.Parent() {
		if ctx := c.Context(); ctx != nil {
			if v, ok := ctx.Value(inputKey{}).(commandInput); ok {
				return v.in, v.err
			}
		}
	}
	return nil, ErrNoCommandInput
}
