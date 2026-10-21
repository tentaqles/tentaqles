package doctor

import (
	"context"
	"fmt"
	"net"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

const hubDefaultPort = 7470

// hubProbeBudget bounds every hub ping of one doctor run together.
const hubProbeBudget = 300 * time.Millisecond

// hubProbe finds the live hub once per doctor run: the serve records are pinged in turn under one
// shared deadline (started at the first ping) and the search stops at the first hub that answers.
type hubProbe struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   bool
	live   *serveinfo.Info
	pinged map[int]bool
}

func newHubProbe() *hubProbe { return &hubProbe{pinged: map[int]bool{}} }

func (p *hubProbe) deadline() context.Context {
	if p.ctx == nil {
		p.ctx, p.cancel = context.WithTimeout(context.Background(), hubProbeBudget)
	}
	return p.ctx
}

// close releases the shared deadline.
func (p *hubProbe) close() {
	if p.cancel != nil {
		p.cancel()
	}
}

func (p *hubProbe) find() {
	if p.done {
		return
	}
	p.done = true
	ctx := p.deadline()
	for _, i := range serveinfo.ReadAll() {
		if ctx.Err() != nil {
			return
		}
		p.pinged[i.Port] = true
		if serveinfo.Ping(ctx, i) {
			info := i
			p.live = &info
			return
		}
	}
}

// serving reports whether the live hub serves ws.
func (p *hubProbe) serving(ws string) bool {
	p.find()
	return p.live != nil && p.live.Serves(ws)
}

// hubOnPort reports whether the hub recorded on port answers, within the shared deadline.
func (p *hubProbe) hubOnPort(port int) bool {
	if p.live != nil && p.live.Port == port {
		return true
	}
	if p.pinged[port] {
		return false
	}
	p.pinged[port] = true
	ctx := p.deadline()
	if ctx.Err() != nil {
		return false
	}
	_, ok := serveinfo.FindByPort(ctx, port)
	return ok
}

// hubChecks reports, informationally, a hub database that cannot be opened and cards locked by
// runs that are not running (spec A §13.5). It opens the database read-only and writes nothing.
// It returns whether the workspace has a hub database at all.
// Hubs are pinged (through probe) only when a lock belongs to a run that claims to be running.
func hubChecks(ws string, probe *hubProbe, add func(level, code, ws, msg, fix string)) bool {
	if _, err := os.Stat(store.DBPath(ws)); err != nil {
		return false
	}
	st, err := store.OpenReadOnly(ws)
	if err != nil {
		add("info", "hub-db-unreadable", ws, "hub database cannot be opened: "+err.Error(),
			"move "+store.DBPath(ws)+" aside; tq hub serve starts a fresh board")
		return true
	}
	defer st.Close()
	locks, err := st.Locks()
	if err != nil {
		add("info", "hub-db-unreadable", ws, "hub database cannot be read: "+err.Error(),
			"move "+store.DBPath(ws)+" aside; tq hub serve starts a fresh board")
		return true
	}
	for _, l := range locks {
		ended := !slices.Contains(store.ActiveStatuses, l.RunStatus)
		if !ended && !(l.RunStatus == store.StatusRunning && !probe.serving(ws)) {
			continue
		}
		fix := "start tq hub serve: it fails interrupted runs and releases their locks"
		if !ended {
			fix = "tq hub cancel " + l.RunID + ", or start tq hub serve (it recovers interrupted runs)"
		}
		add("info", "hub-stale-lock", ws, fmt.Sprintf("card %s is locked by run %s, which is %s", l.CardID, l.RunID, l.RunStatus), fix)
	}
	return true
}

// hubPortCheck reports another program on the hub's port (a live tq hub there is fine).
func hubPortCheck(port int, probe *hubProbe, add func(level, code, ws, msg, fix string)) {
	if port == 0 {
		port = hubDefaultPort
	}
	if probe.hubOnPort(port) {
		return
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond)
	if err != nil {
		return
	}
	conn.Close()
	add("info", "hub-port-busy", "", fmt.Sprintf("port %d (tq hub's) is in use by another program", port),
		"tq hub serve --port <free port>, or set hub.port in config.yaml")
}
