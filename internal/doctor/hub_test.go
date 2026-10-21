package doctor

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

type hubFinding struct{ level, code, ws, msg, fix string }

func collectHub() (*[]hubFinding, func(level, code, ws, msg, fix string)) {
	var fs []hubFinding
	return &fs, func(level, code, ws, msg, fix string) { fs = append(fs, hubFinding{level, code, ws, msg, fix}) }
}

func isolateHub(t *testing.T) {
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestHubChecks(t *testing.T) {
	isolateHub(t)
	fs, add := collectHub()
	if hubChecks("nohub", newHubProbe(), add) {
		t.Fatal("no database means the hub is not in use")
	}
	if len(*fs) != 0 {
		t.Fatalf("no database, no finding: %+v", *fs)
	}
	st, err := store.Open("acme", []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := st.AddCard(store.NewCard{Title: "x"}, "tester")
	st.Checkout(c.ID, "r-gonegonegone")
	st.Close()
	if !hubChecks("acme", newHubProbe(), add) {
		t.Fatal("acme uses the hub")
	}
	if len(*fs) != 1 || (*fs)[0].code != "hub-stale-lock" || (*fs)[0].level != "info" || !strings.Contains((*fs)[0].msg, c.ID) || (*fs)[0].fix == "" {
		t.Fatalf("%+v", *fs)
	}
	os.MkdirAll(store.Dir("bad"), 0o700)
	os.WriteFile(store.DBPath("bad"), []byte("this is not a database"), 0o600)
	*fs = nil
	hubChecks("bad", newHubProbe(), add)
	if len(*fs) != 1 || (*fs)[0].code != "hub-db-unreadable" || (*fs)[0].level != "info" {
		t.Fatalf("%+v", *fs)
	}
}

func TestHubPortCheck(t *testing.T) {
	isolateHub(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true}`)) }))
	fs, add := collectHub()
	hubPortCheck(port, newHubProbe(), add)
	if len(*fs) != 1 || (*fs)[0].code != "hub-port-busy" || (*fs)[0].level != "info" {
		t.Fatalf("another program on the port: %+v", *fs)
	}
	serveinfo.Write(serveinfo.Info{PID: 1, Addr: ln.Addr().String(), Port: port, Workspaces: []string{"acme"}})
	*fs = nil
	hubPortCheck(port, newHubProbe(), add)
	if len(*fs) != 0 {
		t.Fatalf("our own hub on the port is fine: %+v", *fs)
	}
}

// silentHub accepts connections and never answers, so a ping waits for its own timeout.
func silentHub(t *testing.T) *net.TCPAddr {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
	})
	return ln.Addr().(*net.TCPAddr)
}

// TestDoctorHubProbesAreBounded: three serve records that never answer and two hub workspaces
// with running-run locks cost one shared 300 ms probe budget, not a ping per record per workspace.
func TestDoctorHubProbesAreBounded(t *testing.T) {
	isolateHub(t)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	for _, name := range []string{"acme", "globex"} {
		root := filepath.Join(base, name)
		os.MkdirAll(root, 0o755)
		mp := filepath.Join(root, manifest.FileName)
		os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: "+name+"\nidentities: { claude: {} }\n"), 0o600)
		h, _ := trust.HashFile(mp)
		trust.Allow(h)
		st, err := store.Open(name, []string{"all"})
		if err != nil {
			t.Fatal(err)
		}
		c, _ := st.AddCard(store.NewCard{Title: "x"}, "tester")
		r, err := st.CreateRun(store.Run{CardID: c.ID, Adapter: store.AdapterClaude, OnRamp: store.OnRampFeature, Status: store.StatusRunning})
		if err != nil {
			t.Fatal(err)
		}
		st.Checkout(c.ID, r.ID)
		st.Close()
	}
	for i := 0; i < 3; i++ {
		a := silentHub(t)
		serveinfo.Write(serveinfo.Info{PID: 1, Addr: a.String(), Port: a.Port, Mode: "all", Workspaces: []string{"acme", "globex"}, LocalKey: "k"})
	}
	cfg.Hub.Port = silentHub(t).Port
	start := time.Now()
	fs := Run(cfg, deps(map[string]string{}, base, map[string]string{"user.useConfigOnly": "true"}))
	if el := time.Since(start); el >= time.Second {
		t.Fatalf("doctor took %v with unanswering hubs", el)
	}
	n := 0
	for _, f := range fs {
		if f.Code == "hub-stale-lock" {
			n++
			if f.Level != "info" {
				t.Fatalf("%+v", f)
			}
		}
	}
	if n != 2 {
		t.Fatalf("want a stale-lock finding per workspace: %+v", fs)
	}
}
