package audit

import (
	"os"
	"strings"
	"testing"
)

// The log rotates at MaxDenialLog and keeps one previous file; the reader
// streams both, oldest first, and skips a runaway line.
func TestDenialLogRotatesAndStreams(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	prev := MaxDenialLog
	MaxDenialLog = 2048
	t.Cleanup(func() { MaxDenialLog = prev })
	for i := 0; i < 60; i++ {
		if err := AppendDenial(Denial{Category: "secrets", Layer: LayerHook, Command: "printenv", Rule: "tq/env-dump", Reason: strings.Repeat("r", 40), PID: i}); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{DenialsPath(), DenialsPath() + ".1"} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if fi.Size() > MaxDenialLog {
			t.Errorf("%s is %d bytes, over the cap %d", p, fi.Size(), MaxDenialLog)
		}
	}
	ds, err := ReadDenials()
	if err != nil || len(ds) == 0 || len(ds) >= 60 {
		t.Fatalf("%d denials, %v: the oldest are dropped, the newest kept", len(ds), err)
	}
	for i := 1; i < len(ds); i++ {
		if ds[i].PID != ds[i-1].PID+1 {
			t.Fatalf("not in order: %d after %d", ds[i].PID, ds[i-1].PID)
		}
	}
	if ds[len(ds)-1].PID != 59 {
		t.Errorf("newest = %d", ds[len(ds)-1].PID)
	}
	// A runaway line is skipped; the next one still reads.
	if err := appendBytes(DenialsPath(), []byte(strings.Repeat("x", 200<<10)+"\n")); err != nil {
		t.Fatal(err)
	}
	if err := AppendDenial(Denial{Category: "identity", Layer: LayerHook, Command: "git push", PID: 99}); err != nil {
		t.Fatal(err)
	}
	ds, _ = ReadDenials()
	if len(ds) == 0 || ds[len(ds)-1].PID != 99 {
		t.Errorf("the line after a runaway one was lost")
	}
}
