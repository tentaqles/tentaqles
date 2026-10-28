package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/web"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

func init() { hubExtra = append(hubExtra, newHubServeCmd, newHubStatusCmd) }

func randomKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func isWildcard(host string) bool {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsUnspecified()
}

// serveToken: a non-loopback bind requires a token; one is generated (and printed once) when none was given.
func serveToken(host, token string) (string, bool) {
	if token != "" || isLoopback(host) {
		return token, false
	}
	return randomKey() + randomKey()[:8], true
}

// allowedHosts guards against DNS rebinding. The web guard fails closed on an empty list, so it is
// never empty: the loopback aliases always, plus, for a non-loopback bind, the bound address, or
// for a wildcard bind every interface address, and the machine's host name. Each carries the port;
// port 80 adds the portless forms. The token still protects a non-loopback bind.
func allowedHosts(host string, port int) []string {
	p := strconv.Itoa(port)
	names := []string{"127.0.0.1", "localhost", "::1"}
	if !isLoopback(host) {
		if isWildcard(host) {
			if addrs, err := net.InterfaceAddrs(); err == nil {
				for _, a := range addrs {
					if n, ok := a.(*net.IPNet); ok && n.IP != nil {
						names = append(names, n.IP.String())
					}
				}
			}
		} else {
			names = append(names, strings.Trim(host, "[]"))
		}
		if h, err := os.Hostname(); err == nil && h != "" {
			names = append(names, h)
		}
	}
	var hosts []string
	add := func(h string) {
		h = strings.ToLower(h)
		if !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	for _, n := range names {
		add(net.JoinHostPort(n, p))
	}
	if port == 80 {
		for _, n := range names {
			if strings.Contains(n, ":") {
				n = "[" + n + "]"
			}
			add(n)
		}
	}
	return hosts
}

func addrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	m := err.Error()
	return strings.Contains(m, "address already in use") || strings.Contains(m, "Only one usage of each socket address")
}

func portBusyError(ctx context.Context, port int) error {
	if info, ok := serveinfo.FindByPort(ctx, port); ok {
		return fmt.Errorf("port %d is in use by a running tq hub (pid %d, mode %s, workspaces %s). Use `tq hub serve --all` to serve several workspaces from one hub, or `--port N` for another one",
			port, info.PID, info.Mode, strings.Join(info.Workspaces, ", "))
	}
	return fmt.Errorf("port %d is in use by another program; pass --port N (or set hub.port in %s)", port, paths.Config())
}

// servedElsewhere keeps one runner per workspace: a workspace another live hub already serves is
// refused (single mode) or listed as unavailable (--all), so two hubs never run the same queue.
func servedElsewhere(ctx context.Context, entries []web.Entry, mode string) ([]web.Entry, error) {
	out := append([]web.Entry(nil), entries...)
	for i := range out {
		if !out[i].Info.Available {
			continue
		}
		if info, ok := serveinfo.Find(ctx, out[i].Info.Name); ok {
			if mode == "single" {
				return nil, fmt.Errorf("workspace %s is already served by the hub on port %d (pid %d); open it with tq hub open, or stop that hub first",
					out[i].Info.Name, info.Port, info.PID)
			}
			out[i].Info.Available = false
			out[i].Info.Reason = fmt.Sprintf("served by the hub on port %d", info.Port)
		}
	}
	return out, nil
}

const hubShutdownGrace = 5 * time.Second

// runHTTP serves ln until ctx ends (or Serve fails), then stops in one fixed order:
//  1. the HTTP server stops accepting and in-flight requests finish (grace; after it, open
//     connections such as SSE streams are closed and their handlers get one more grace to return);
//  2. closeBackend stops the runners (live rounds requeue) and closes the stores;
//  3. removeRecord drops serve-<port>.json, so no new hub takes the workspaces before 2 is done.
func runHTTP(ctx context.Context, ln net.Listener, h http.Handler, grace time.Duration, closeBackend, removeRecord func()) error {
	var inflight sync.WaitGroup
	hs := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inflight.Add(1)
		defer inflight.Done()
		h.ServeHTTP(w, r)
	})}
	served := make(chan error, 1)
	go func() { served <- hs.Serve(ln) }()
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-served:
	}
	sctx, cancel := context.WithTimeout(context.Background(), grace)
	if hs.Shutdown(sctx) != nil {
		_ = hs.Close()
	}
	cancel()
	handlers := make(chan struct{})
	go func() { inflight.Wait(); close(handlers) }()
	select {
	case <-handlers:
	case <-time.After(grace):
	}
	closeBackend()
	removeRecord()
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	return nil
}

func cmdCtx(c *cobra.Command) context.Context {
	if ctx := c.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

func hasHubDB(ws string) bool {
	_, err := os.Stat(store.DBPath(ws))
	return err == nil
}

func newHubServeCmd(f *hubFlags) *cobra.Command {
	var all, noNotify, drain bool
	var port int
	var bind, token string
	c := &cobra.Command{
		Use:   "serve [--all] [--port N] [--bind ADDR] [--token T] [--no-notify]",
		Short: "Serve the board on 127.0.0.1:7470 for this workspace, or every trusted one with --all",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			// An agent must never be handed the LAN token: refuse before anything starts.
			if bind != "" && !isLoopback(bind) && hubAgent() {
				return fmt.Errorf("refusing --bind %s from an agent session: a non-loopback hub needs a token an agent must never see; run tq hub serve --bind in your own terminal", bind)
			}
			cfg, err := registry.Load()
			if err != nil {
				return err
			}
			mode := "single"
			if all || strings.EqualFold(cfg.Hub.Mode, "all") {
				mode = "all"
			}
			if port == 0 {
				port = cfg.Hub.Port
			}
			if port == 0 {
				port = hubDefaultPort
			}
			var entries []web.Entry
			switch {
			case mode == "all":
				entries = web.DiscoverAll(cfg)
			case f.workspace != "":
				ws, err := hubWorkspace(f.workspace)
				if err != nil {
					return err
				}
				entries = []web.Entry{{Info: web.WorkspaceInfo{Name: ws.Name, Available: true}, WS: ws}}
			default:
				cwd, _ := os.Getwd()
				if entries, err = web.DiscoverCwd(cwd, cfg); err != nil {
					return err
				}
			}
			ctx, stop := signal.NotifyContext(cmdCtx(c), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if entries, err = servedElsewhere(ctx, entries, mode); err != nil {
				return err
			}
			warn := warnTo(c)
			out := c.OutOrStdout()
			host := bind
			if host == "" {
				host = "127.0.0.1"
			}
			tok, generated := serveToken(host, token)
			tokenHash := ""
			if tok != "" {
				tokenHash = web.HashToken(tok)
			}
			// Every workspace the hub opens, at start or lazily, stores the LAN token's hash.
			factory := func(ws *resolve.Workspace) (*runner.Service, error) {
				svc, err := newHubService(ws, warn, !noNotify)
				if err == nil && tokenHash != "" {
					if serr := svc.Store().SetSetting(store.SettingWebTokenHash, tokenHash); serr != nil {
						warn(ws.Name + ": token: " + serr.Error())
					}
				}
				return svc, err
			}
			if drain {
				n := 0
				for _, e := range entries {
					// --all never creates a board for a workspace that has none (it has no queue).
					if !e.Info.Available || (mode == "all" && !hasHubDB(e.Info.Name)) {
						continue
					}
					svc, err := factory(e.WS)
					if err != nil {
						return err
					}
					err = svc.Drain(ctx)
					svc.Store().Close()
					if err != nil {
						return err
					}
					n++
				}
				fmt.Fprintf(out, "drained %d workspace(s) (mode %s)\n", n, mode)
				return nil
			}
			ln, err := net.Listen("tcp", net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port)))
			if err != nil {
				if addrInUse(err) {
					return portBusyError(ctx, port)
				}
				return err
			}
			actual := ln.Addr().(*net.TCPAddr).Port
			// The runners outlive the signal: runHTTP stops them only after the HTTP server drained.
			backend := web.NewMultiBackend(context.WithoutCancel(ctx), entries, factory)
			backend.SetSyncSibling(syncEngines(warn)) // the board sync engine beside each runner (spec B §4), shown on /connections
			var served, unavailable []string
			for _, e := range entries {
				if !e.Info.Available {
					unavailable = append(unavailable, e.Info.Name+" ("+e.Info.Reason+")")
					continue
				}
				served = append(served, e.Info.Name)
				// A queue that exists must run from the start; --all opens the rest on first use.
				if mode == "single" || hasHubDB(e.Info.Name) {
					if _, err := backend.Service(e.Info.Name); err != nil {
						warn(e.Info.Name + ": " + err.Error())
					}
				}
			}
			localKey := randomKey()
			srv, err := web.New(web.Options{Backend: backend, Mode: mode, TokenHash: tokenHash, LocalKey: localKey,
				AllowedHosts: allowedHosts(host, actual), Bind: ln.Addr().String()})
			if err != nil {
				ln.Close()
				backend.Close()
				return err
			}
			if err := serveinfo.Write(serveinfo.Info{PID: os.Getpid(), Addr: ln.Addr().String(), Port: actual, Mode: mode,
				Workspaces: served, Unavailable: unavailable, LocalKey: localKey, StartedAt: store.Now()}); err != nil {
				warn("serve record: " + err.Error())
			}
			shown := host
			if isWildcard(host) {
				shown = "127.0.0.1"
			}
			fmt.Fprintf(out, "tq hub (%s) serving %s on http://%s/\n", mode, strings.Join(served, ", "), net.JoinHostPort(strings.Trim(shown, "[]"), strconv.Itoa(actual)))
			for _, u := range unavailable {
				fmt.Fprintf(out, "unavailable: %s\n", u)
			}
			if generated {
				fmt.Fprintf(out, "token (shown once; browsers on other machines open /login?token=<token> once): %s\n", tok)
			}
			return runHTTP(ctx, ln, srv.Handler(), hubShutdownGrace, backend.Close, func() { _ = serveinfo.Remove(actual) })
		},
	}
	c.Flags().BoolVar(&all, "all", false, "serve every registered, trusted workspace (or set hub.mode: all in config.yaml)")
	c.Flags().IntVar(&port, "port", 0, "port (default 7470, or hub.port in config.yaml)")
	c.Flags().StringVar(&bind, "bind", "", "address to bind (default 127.0.0.1; any other bind requires a token)")
	c.Flags().StringVar(&token, "token", "", "token for a non-loopback bind (generated and printed once when omitted)")
	c.Flags().BoolVar(&noNotify, "no-notify", false, "no desktop notifications")
	c.Flags().BoolVar(&drain, "drain", false, "run every queued run to its next stop, then exit (no web server)")
	_ = c.Flags().MarkHidden("drain")
	return c
}

func newHubStatusCmd(f *hubFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show running hubs: mode, port, workspaces and counts",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := cmdCtx(c)
			if f.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, f.timeout)
				defer cancel()
			}
			out := c.OutOrStdout()
			var live []serveinfo.Info
			for _, info := range serveinfo.ReadAll() {
				if !serveinfo.Ping(ctx, info) {
					fmt.Fprintf(out, "stale record for port %d (pid %d): no hub answers; it is removed when a hub starts there\n", info.Port, info.PID)
					continue
				}
				live = append(live, info)
				fmt.Fprintf(out, "hub on %s  pid %d  mode %s\n", serveinfo.BaseURL(info), info.PID, info.Mode)
				for _, ws := range info.Workspaces {
					cl := web.NewClient(info, ws, false)
					views, err := cl.List(ctx, store.CardFilter{}, false)
					if err != nil {
						fmt.Fprintf(out, "  %s: %v\n", ws, err)
						continue
					}
					running, waiting := 0, 0
					for _, v := range views {
						if v.RunStatus == store.StatusRunning {
							running++
						}
						waiting += v.Waiting
					}
					fmt.Fprintf(out, "  %s: %d cards, %d running, %d decisions waiting\n", ws, len(views), running, waiting)
				}
				for _, u := range info.Unavailable {
					fmt.Fprintf(out, "  unavailable: %s\n", u)
				}
			}
			if len(live) == 0 {
				fmt.Fprintln(out, "no hub is running; start one with: tq hub serve")
			}
			// The current workspace: which hub serves it, or how to start one.
			if ws, err := hubWorkspace(f.workspace); err == nil {
				i := slices.IndexFunc(live, func(info serveinfo.Info) bool { return info.Serves(ws.Name) })
				if i >= 0 {
					fmt.Fprintf(out, "%s is served by the hub on port %d (%s)\n", ws.Name, live[i].Port, serveinfo.BaseURL(live[i]))
				} else {
					fmt.Fprintf(out, "%s is not served; start a hub with: tq hub serve (or tq hub serve --all for every workspace)\n", ws.Name)
				}
			}
			return nil
		},
	}
}
