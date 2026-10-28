package cmd

import (
	"context"
	"io"
	"os"
)

// hubParentStdinEnv is set by an app that starts `tq hub serve` as its child and holds the
// child's stdin open (the desktop's Board tab). EOF on stdin then means the parent closed it or
// is gone, and the hub shuts down the way Ctrl-C would: it drains HTTP, stops its runners (and
// their agent process trees) and removes its serve record. Windows has no signal a GUI parent can
// send a console-less child, so this is the polite stop there. An env var, not a flag: an older
// tq ignores it instead of refusing an unknown flag.
const hubParentStdinEnv = "TQ_HUB_PARENT_STDIN"

// parentStdinWanted: only with the env, since a hub started with stdin from /dev/null (a
// service, a script) would otherwise read EOF and exit at once.
func parentStdinWanted() bool { return os.Getenv(hubParentStdinEnv) == "1" }

// parentStdinContext is cancelled when r reaches EOF (or fails), or when ctx is.
func parentStdinContext(ctx context.Context, r io.Reader) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		_, _ = io.Copy(io.Discard, r)
		cancel()
	}()
	return ctx, cancel
}
