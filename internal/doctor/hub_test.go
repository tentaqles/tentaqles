package doctor

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/testutil"
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
	if hubChecks("nohub", add) {
		t.Fatal("no database means the hub is not in use")
	}
	if len(*fs) != 0 {
		t.Fatalf("no database, no finding: %+v", *fs)
	}
	st, err := store.Open("acme", []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := st.AddCard(store.NewCard{Title: "x"}, "renato")
	st.Checkout(c.ID, "r-gonegonegone")
	st.Close()
	if !hubChecks("acme", add) {
		t.Fatal("acme uses the hub")
	}
	if len(*fs) != 1 || (*fs)[0].code != "hub-stale-lock" || (*fs)[0].level != "info" || !strings.Contains((*fs)[0].msg, c.ID) || (*fs)[0].fix == "" {
		t.Fatalf("%+v", *fs)
	}
	os.MkdirAll(store.Dir("bad"), 0o700)
	os.WriteFile(store.DBPath("bad"), []byte("this is not a database"), 0o600)
	*fs = nil
	hubChecks("bad", add)
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
	hubPortCheck(port, add)
	if len(*fs) != 1 || (*fs)[0].code != "hub-port-busy" || (*fs)[0].level != "info" {
		t.Fatalf("another program on the port: %+v", *fs)
	}
	serveinfo.Write(serveinfo.Info{PID: 1, Addr: ln.Addr().String(), Port: port, Workspaces: []string{"acme"}})
	*fs = nil
	hubPortCheck(port, add)
	if len(*fs) != 0 {
		t.Fatalf("our own hub on the port is fine: %+v", *fs)
	}
}
