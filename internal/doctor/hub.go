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

// hubChecks reports, informationally, a hub database that cannot be opened and cards locked by
// runs that are not running (spec A §13.5). It opens the database read-only and writes nothing.
// It returns whether the workspace has a hub database at all.
func hubChecks(ws string, add func(level, code, ws, msg, fix string)) bool {
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
	serving := false
	for _, i := range serveinfo.ReadAll() {
		if i.Serves(ws) && serveinfo.Ping(context.Background(), i) {
			serving = true
		}
	}
	for _, l := range locks {
		ended := !slices.Contains(store.ActiveStatuses, l.RunStatus)
		if !ended && !(l.RunStatus == store.StatusRunning && !serving) {
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
func hubPortCheck(port int, add func(level, code, ws, msg, fix string)) {
	if port == 0 {
		port = hubDefaultPort
	}
	if _, ok := serveinfo.FindByPort(context.Background(), port); ok {
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
