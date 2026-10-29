package packs

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestListGoldenJSON pins `tq pack list --enabled --json`, which the build
// gate freezes at approval: schema, config_hash (the same hash tq pack check
// reports) and packs[].name/enabled.
func TestListGoldenJSON(t *testing.T) {
	isolate(t)
	in := goldenInput(t)
	rep := List(in.Config, "acme", true)
	if rep.ConfigHash != RunChecks(t.Context(), in, RunOptions{}).ConfigHash {
		t.Fatal("list and check must report the same config_hash")
	}
	var buf bytes.Buffer
	if err := EncodeListJSON(&buf, rep); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "list-golden.json")
	if *update {
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(buf.String(), "\r\n", "\n") != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("list output changed:\n%s", buf.String())
	}
	for _, p := range rep.Packs {
		if !p.Enabled {
			t.Fatalf("--enabled listed %s", p.Name)
		}
	}
	if all := List(in.Config, "acme", false); len(all.Packs) != 4 {
		t.Fatalf("without --enabled every pack is listed: %d", len(all.Packs))
	}
}
