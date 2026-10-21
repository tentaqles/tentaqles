package notify

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("TQ_NOTIFY_HELPER") == "hang" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestBuildPerPlatformKeepsTextOutOfTheScript(t *testing.T) {
	evil := `x"; rm -rf / #$(whoami)`
	for _, goos := range []string{"windows", "darwin", "linux"} {
		c, ok := Build(goos, "tq hub: waiting on you", evil)
		if !ok {
			t.Fatalf("%s: no command", goos)
		}
		joined := c.Name + " " + strings.Join(c.Args, " ")
		if goos != "linux" && strings.Contains(joined, "whoami") {
			t.Fatalf("%s: body inside the command line: %s", goos, joined)
		}
		if !strings.Contains(strings.Join(c.Env, "\n"), "TQ_NOTIFY_BODY=") {
			t.Fatalf("%s: env %v", goos, c.Env)
		}
	}
	if c, _ := Build("windows", "t", "b"); c.Name != "powershell" {
		t.Fatal(c.Name)
	}
	if c, _ := Build("darwin", "t", "b"); c.Name != "osascript" {
		t.Fatal(c.Name)
	}
	if c, _ := Build("linux", "t", "b"); c.Name != "notify-send" || c.Args[len(c.Args)-1] != "b" {
		t.Fatalf("%+v", c)
	}
	if _, ok := Build("plan9", "t", "b"); ok {
		t.Fatal("unknown platforms send nothing")
	}
}

func TestSendIsBoundedToTwoSeconds(t *testing.T) {
	start := time.Now()
	err := SendWith(context.Background(), Command{Name: os.Args[0], Env: []string{"TQ_NOTIFY_HELPER=hang"}})
	if err == nil {
		t.Fatal("a hanging notifier returns an error")
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("took %s", d)
	}
}
