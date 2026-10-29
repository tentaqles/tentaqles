package packs

import (
	"errors"
	"testing"
)

func TestHumanOnlyMarkers(t *testing.T) {
	markers := [][2]string{{"TENTAQLES_HEADLESS", "1"}, {"CLAUDECODE", "1"}, {"CLAUDE_PLUGIN_ROOT", "/p"}, {"TQ_HUB_TASK", `{"hub":"c1"}`}}
	clear := func() {
		for _, m := range markers {
			t.Setenv(m[0], "")
		}
	}
	clear()
	if err := HumanOnly(); err != nil {
		t.Fatalf("a human shell: %v", err)
	}
	for _, m := range markers {
		clear()
		t.Setenv(m[0], m[1])
		if err := HumanOnly(); !errors.Is(err, ErrHumanOnly) {
			t.Fatalf("%s must refuse", m[0])
		}
	}
}
