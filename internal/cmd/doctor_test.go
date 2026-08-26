package cmd

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDoctor_JSONFlagPrintsFindings(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	prevExit := exitFunc
	exitFunc = func(int) {}
	t.Cleanup(func() { exitFunc = prevExit })

	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"doctor", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	var findings []struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(out.Bytes(), &findings); err != nil {
		t.Fatalf("output did not parse as JSON: %v\noutput: %s", err, out.String())
	}
	found := false
	for _, f := range findings {
		if f.Code == "no-bases" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected code no-bases in %+v", findings)
	}
}
