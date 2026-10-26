package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// events is the page's one SSE stream: store changes of the served workspaces (ids only, never
// content), a poll that catches writes by other processes (a CLI writing the store directly),
// and, when ?run= names a run, the tail of its (masked) log for the run page.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	q := r.URL.Query()
	if q.Get("ws") != "" {
		if _, _, ok := s.service(w, r); !ok {
			return
		}
	}
	svcs := map[string]*runner.Service{}
	for _, info := range s.opts.Backend.Workspaces() {
		if !info.Available || (q.Get("ws") != "" && info.Name != q.Get("ws")) {
			continue
		}
		if svc, err := s.opts.Backend.Service(info.Name); err == nil {
			svcs[info.Name] = svc
		}
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	fl.Flush()
	send := func(event string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		fl.Flush()
	}
	// change frames put data first: field order inside an SSE event is free, and a line reader
	// (tests, curl | grep) that stops at the event name has already seen which ids changed.
	sendChange := func(c store.Change) {
		b, _ := json.Marshal(store.Change{Workspace: c.Workspace, Kind: c.Kind, CardID: c.CardID, RunID: c.RunID})
		fmt.Fprintf(w, "data: %s\nevent: change\n\n", b)
		fl.Flush()
	}
	merged := make(chan store.Change, 64)
	stamps := map[string]string{}
	for name, svc := range svcs {
		ch, cancel := svc.Store().Subscribe()
		defer cancel()
		go func(ch <-chan store.Change) {
			for c := range ch {
				select {
				case merged <- c:
				default:
				}
			}
		}(ch)
		stamps[name], _ = svc.Store().ChangeStamp()
	}
	logPath, offset := "", int64(0)
	if runID := q.Get("run"); store.ValidID("r", runID) {
		for _, svc := range svcs {
			if run, err := svc.Store().Run(runID); err == nil {
				logPath = run.LogPath
				break
			}
		}
		offset, _ = strconv.ParseInt(q.Get("from"), 10, 64)
		if offset < 0 {
			offset = 0
		}
	}
	poll := time.NewTicker(s.opts.PollEvery)
	defer poll.Stop()
	beat := time.NewTicker(15 * time.Second)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case c := <-merged:
			sendChange(c)
		case <-poll.C:
			for name, svc := range svcs {
				if st, err := svc.Store().ChangeStamp(); err == nil && st != stamps[name] {
					stamps[name] = st
					sendChange(store.Change{Workspace: name, Kind: "external"})
				}
			}
			if logPath != "" {
				if chunk, next := readFrom(logPath, offset, 64<<10); len(chunk) > 0 {
					offset = next
					send("log", map[string]any{"text": chunk, "offset": offset})
				}
			}
		case <-beat.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

func readFrom(path string, offset, max int64) (string, int64) {
	f, err := os.Open(path)
	if err != nil {
		return "", offset
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", offset
	}
	if fi.Size() < offset { // truncated or replaced: start over
		offset = 0
	}
	if fi.Size() <= offset {
		return "", offset
	}
	if fi.Size()-offset > max {
		offset = fi.Size() - max
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", offset
	}
	b, _ := io.ReadAll(io.LimitReader(f, max))
	return string(b), offset + int64(len(b))
}
