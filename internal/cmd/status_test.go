package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/audit"
)

func runStatus(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := NewRoot()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(append([]string{"status"}, args...))
	err := root.Execute()
	return out.String(), err
}

func seedDenials(t *testing.T) {
	t.Helper()
	for _, d := range []audit.Denial{
		{Category: "identity", Layer: audit.LayerHook, Caller: audit.CallerHarness, Harness: "claude", Tool: "Bash", Command: "aws ec2 run-instances", Rule: "wrong-cloud", Reason: "BLOCKED [identity/wrong-cloud]: aws is not azure\nScope of identity acme: cloud azure", Identity: "acme"},
		{Category: "secrets", Layer: audit.LayerHook, Caller: audit.CallerSubagent, Tool: "Bash", Command: "printenv", Rule: "tq/env-dump", Reason: "BLOCKED [secrets/tq/env-dump]: x", Identity: "acme"},
		{Category: "self-protection", Layer: audit.LayerFilesystem, Tool: "Write", Command: "/h/.tentaqles/config.yaml", Rule: "tq/protected-path", Reason: "BLOCKED [self-protection/tq/protected-path]: x", Identity: "globex"},
	} {
		if err := audit.AppendDenial(d); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStatusDenials(t *testing.T) {
	isolateHome(t)
	out, err := runStatus(t, "--denials")
	if err != nil || !strings.Contains(out, "no denials") {
		t.Fatalf("empty log: %v %q", err, out)
	}
	seedDenials(t)

	out, err = runStatus(t, "--denials")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"acme  identity/wrong-cloud  layer=hook  caller=harness claude  tool=Bash", "aws ec2 run-instances",
		"BLOCKED [identity/wrong-cloud]: aws is not azure", "globex  self-protection/tq/protected-path  layer=filesystem"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Scope of identity") {
		t.Errorf("only the reason's first line is printed:\n%s", out)
	}

	decode := func(args ...string) []audit.Denial {
		t.Helper()
		out, err := runStatus(t, append([]string{"--denials", "--json"}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		var ds []audit.Denial
		if err := json.Unmarshal([]byte(out), &ds); err != nil {
			t.Fatalf("%v: %q", err, out)
		}
		return ds
	}
	if ds := decode(); len(ds) != 3 {
		t.Errorf("all: %d", len(ds))
	}
	if ds := decode("--identity", "acme"); len(ds) != 2 || ds[0].Identity != "acme" {
		t.Errorf("--identity: %+v", ds)
	}
	if ds := decode("--category", "secrets"); len(ds) != 1 || ds[0].Rule != "tq/env-dump" {
		t.Errorf("--category: %+v", ds)
	}
	if ds := decode("--last", "1"); len(ds) != 1 || ds[0].Identity != "globex" {
		t.Errorf("--last 1 is the newest: %+v", ds)
	}
	if ds := decode("--identity", "nobody"); ds == nil || len(ds) != 0 {
		t.Errorf("no match is an empty array: %+v", ds)
	}
	if _, err := runStatus(t, "--identity", "acme"); err == nil {
		t.Error("--identity without --denials must fail")
	}
}

// A log line carrying terminal escapes (an OSC 52 clipboard write, a line
// erase, CR, BEL, a C1 CSI) prints them escaped, in text and in --json.
func TestStatusDenialsEscapesControlCharacters(t *testing.T) {
	isolateHome(t)
	if err := os.MkdirAll(filepath.Dir(audit.DenialsPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	evil := "\x1b]52;c;cHduZWQ=\x07\x1b[2K\r\x1b[1Afake ok line\u009b2J"
	b, _ := json.Marshal(map[string]any{"category": "secrets", "layer": "hook", "tool": evil, "command": evil, "subject": evil,
		"rule": evil, "reason": evil + "\nsecond", "identity": evil, "cwd": evil, "caller": evil, "harness": evil})
	if err := os.WriteFile(audit.DenialsPath(), append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--denials"}, {"--denials", "--json"}} {
		out, err := runStatus(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range out {
			if (r < 0x20 && r != '\n') || r == 0x7f || (r >= 0x80 && r < 0xa0) {
				t.Fatalf("%v: control character %U reaches the terminal: %q", args, r, out)
			}
		}
		if !strings.Contains(out, `\x1b]52`) && !strings.Contains(out, `\\x1b]52`) {
			t.Errorf("%v: the escape is not shown escaped: %q", args, out)
		}
	}
}

// An agent session cannot read the log (ADR-0006): the refusal is logged.
func TestStatusDenialsRefusesAgent(t *testing.T) {
	isolateHome(t)
	seedDenials(t)
	t.Setenv("CLAUDECODE", "1")
	out, err := runStatus(t, "--denials")
	if !errors.Is(err, errHumanOnly) || strings.Contains(out, "aws ec2") {
		t.Fatalf("want a human-only refusal, got %v %q", err, out)
	}
	ds, _ := audit.ReadDenials()
	last := ds[len(ds)-1]
	if last.Layer != audit.LayerHumanOnly || last.Command != "tq status --denials" {
		t.Errorf("refusal not logged: %+v", last)
	}
}
