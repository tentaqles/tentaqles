// Package serveinfo records which `tq hub serve` process serves which workspaces on which port
// ($TQ_HOME/hub/serve-<port>.json, user-only), so the CLI can talk to a running hub instead of
// writing the store behind its back.
package serveinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// HeaderLocal carries the per-process key that lets the CLI (same OS user) call the API.
const HeaderLocal = "X-TQ-Hub-Local"

type Info struct {
	PID         int      `json:"pid"`
	Addr        string   `json:"addr"` // host:port as bound
	Port        int      `json:"port"`
	Mode        string   `json:"mode"` // single | all
	Workspaces  []string `json:"workspaces"`
	Unavailable []string `json:"unavailable,omitempty"`
	LocalKey    string   `json:"local_key"`
	StartedAt   string   `json:"started_at"`
}

func (i Info) Serves(ws string) bool { return slices.Contains(i.Workspaces, ws) }

func dir() string          { return filepath.Join(paths.Home(), "hub") }
func Path(port int) string { return filepath.Join(dir(), fmt.Sprintf("serve-%d.json", port)) }

func Write(i Info) error {
	if err := os.MkdirAll(dir(), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(i.Port), raw, 0o600)
}

func Remove(port int) error {
	err := os.Remove(Path(port))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func ReadAll() []Info {
	ents, err := os.ReadDir(dir())
	if err != nil {
		return nil
	}
	var out []Info
	for _, e := range ents {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "serve-") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir(), e.Name()))
		if err != nil {
			continue
		}
		var i Info
		if json.Unmarshal(raw, &i) == nil && i.Port > 0 {
			out = append(out, i)
		}
	}
	return out
}

// BaseURL dials loopback for wildcard binds, else the bound host.
func BaseURL(i Info) string {
	host, _, err := net.SplitHostPort(i.Addr)
	if err != nil || host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(i.Port))
}

func Ping(ctx context.Context, i Info) bool {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL(i)+"/api/ping", nil)
	if err != nil {
		return false
	}
	req.Header.Set(HeaderLocal, i.LocalKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Find returns a live hub serving ws.
func Find(ctx context.Context, ws string) (Info, bool) {
	for _, i := range ReadAll() {
		if i.Serves(ws) && Ping(ctx, i) {
			return i, true
		}
	}
	return Info{}, false
}

func FindByPort(ctx context.Context, port int) (Info, bool) {
	raw, err := os.ReadFile(Path(port))
	if err != nil {
		return Info{}, false
	}
	var i Info
	if json.Unmarshal(raw, &i) != nil {
		return Info{}, false
	}
	return i, Ping(ctx, i)
}
