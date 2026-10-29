package data

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/packs"
)

// runCostCmd runs `cost <args>` with a fixed environment and returns stdout,
// stderr and the exit code (0 when the command did not exit).
func runCostCmd(t *testing.T, cfg packs.Config, stdin string, args ...string) (string, string, int) {
	t.Helper()
	root := t.TempDir()
	oldEnv, oldExit := costCmdEnv, costExit
	t.Cleanup(func() { costCmdEnv, costExit = oldEnv, oldExit })
	costCmdEnv = func(*cobra.Command) (costEnv, error) {
		return costEnv{Root: root, Config: cfg, Tools: packs.NewToolRunner(root, cfg)}, nil
	}
	code := 0
	costExit = func(c int) { code = c }
	var out, errb bytes.Buffer
	cmd := newCostCmd()
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (stderr %s)", err, errb.String())
	}
	return out.String(), errb.String(), code
}

func TestCostEstimateCommand(t *testing.T) {
	costIsolate(t)
	state := costFakeCLIs(t, "bq", "psql")
	cfg := costConfig(t, costEstimateConfig)
	costCopyFixture(t, filepath.Join("testdata", "cost-estimate", "bigquery", "output.json"), filepath.Join(state, "bq.out"))
	query := "SELECT * FROM acme.customers WHERE email = 'tester@acme.test'"

	out, _, code := runCostCmd(t, cfg, query, "estimate", "--engine", "bigquery", "--stdin", "--json")
	var est CostEstimate
	if err := json.Unmarshal([]byte(out), &est); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if est.Schema != EstimateSchema || est.Verdict != VerdictOver || code != 1 {
		t.Fatalf("est %+v code %d", est, code)
	}
	if strings.Contains(out, "tester") || strings.Contains(out, "customers") {
		t.Fatal("output echoes the query")
	}

	// Text output, from a file, within the cap.
	q := filepath.Join(t.TempDir(), "q.sql")
	if err := os.WriteFile(q, []byte(query), 0o644); err != nil {
		t.Fatal(err)
	}
	big := costConfig(t, "packs:\n  data:\n    engines: [bigquery]\n    cost:\n      bigquery: {max_bytes: 50GB}\n")
	out, _, code = runCostCmd(t, big, "", "estimate", "--engine", "bigquery", "--file", q)
	if code != 0 || !strings.Contains(out, "verdict   within") || !strings.Contains(out, "1.2 GB") || strings.Contains(out, "tester") {
		t.Fatalf("text output (code %d):\n%s", code, out)
	}

	// No cap configured: ask (exit 4).
	nocap := costConfig(t, "packs:\n  data:\n    engines: [bigquery]\n")
	if _, _, code = runCostCmd(t, nocap, query, "estimate", "--engine", "bigquery", "--stdin"); code != 4 {
		t.Fatalf("no cap: exit %d", code)
	}
}

func TestCostEstimateCommandErrors(t *testing.T) {
	costIsolate(t)
	state := costFakeCLIs(t, "bq")
	cfg := costConfig(t, costEstimateConfig)
	cases := []struct {
		stdin string
		args  []string
		code  int
		msg   string
	}{
		{"SELECT 1", []string{"estimate", "--stdin"}, 2, "--engine"},
		{"SELECT 1", []string{"estimate", "--engine", "oracle", "--stdin"}, 2, "unknown engine"},
		{"SELECT 1", []string{"estimate", "--engine", "bigquery"}, 2, "--file or --stdin"},
		{"DELETE FROM t", []string{"estimate", "--engine", "bigquery", "--stdin"}, 2, "only SELECT"},
		{"SELECT 1", []string{"estimate", "--engine", "bigquery", "--file", "missing.sql", "--stdin"}, 2, "not both"},
	}
	for _, c := range cases {
		_, errOut, code := runCostCmd(t, cfg, c.stdin, c.args...)
		if code != c.code || !strings.Contains(errOut, c.msg) {
			t.Errorf("%v: exit %d stderr %q", c.args, code, errOut)
		}
	}
	// The engine CLI fails: exit 3, and its output (which may echo the query) is not shown.
	if err := os.WriteFile(filepath.Join(state, "bq.out"), []byte("syntax error near tester@acme.test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "bq.exit"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runCostCmd(t, cfg, "SELECT 'tester@acme.test'", "estimate", "--engine", "bigquery", "--stdin")
	if code != 3 || strings.Contains(out+errOut, "tester") {
		t.Fatalf("engine failure: exit %d out %q err %q", code, out, errOut)
	}
}
