package setupapi

import (
	"context"
	"net/url"

	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
)

// Hub is a plain view of a running `tq hub serve` process (its serve record under
// $TQ_HOME/hub). The per-process local key stays inside this package.
type Hub struct {
	URL        string // http://127.0.0.1:<port>, loopback for wildcard binds
	Port       int
	PID        int
	Mode       string // single | all
	Workspaces []string
}

func fromInfo(i serveinfo.Info) Hub {
	return Hub{URL: serveinfo.BaseURL(i), Port: i.Port, PID: i.PID, Mode: i.Mode, Workspaces: append([]string(nil), i.Workspaces...)}
}

// LiveHubs lists the hubs whose serve record answers a ping with its own local key; stale
// records (the process is gone) and anything else on the port are left out.
func LiveHubs(ctx context.Context) []Hub {
	var out []Hub
	for _, i := range serveinfo.ReadAll() {
		if serveinfo.Ping(ctx, i) {
			out = append(out, fromInfo(i))
		}
	}
	return out
}

// FindHub returns a live hub that serves ws.
func FindHub(ctx context.Context, ws string) (Hub, bool) {
	for _, i := range serveinfo.ReadAll() {
		if i.Serves(ws) && serveinfo.Ping(ctx, i) {
			return fromInfo(i), true
		}
	}
	return Hub{}, false
}

// BoardURL is the board page of ws on hub h.
func BoardURL(h Hub, ws string) string {
	return h.URL + "/?ws=" + url.QueryEscape(ws)
}
