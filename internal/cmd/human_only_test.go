package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/approval"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// walk visits every command under c.
func walk(c *cobra.Command, f func(*cobra.Command)) {
	f(c)
	for _, s := range c.Commands() {
		walk(s, f)
	}
}

// R5-4: tq/human-answer (the static rule and the hook's folded check)
// matches every pack command that declares it executes an approved action,
// in plain and flagged spellings, and no approve command that only asks.
func TestHumanAnswerCoversEveryExecutor(t *testing.T) {
	set, _ := policy.Build(policy.Layers{Builtin: policy.ForPacks(policy.Builtin(), nil)})
	denied := func(line string) bool {
		d := set.Evaluate(policy.ToolCall{Tool: "Bash", Command: line})
		static := d.Action == policy.Deny && len(d.Matched) > 0 && d.Matched[0].ID == "tq/human-answer"
		return static && humanAnswerCommand(line)
	}
	executors, askers := 0, 0
	walk(NewRoot(), func(c *cobra.Command) {
		kind := c.Annotations[packs.AnnotationHumanOnly]
		if kind == "" {
			return
		}
		path := strings.Fields(c.CommandPath()) // tq pack <x> ... <verb>
		line := "tq " + strings.Join(path[1:], " ")
		switch kind {
		case packs.HumanOnlyExecute:
			executors++
			flagged := "tq " + strings.Join(path[1:3], " ") + " --workspace acme " + strings.Join(path[3:], " ") + " --yes d1"
			for _, l := range []string{line + " d1", flagged} {
				if !denied(l) {
					t.Errorf("%s executes an approved action but tq/human-answer does not deny %q (name executors ... apply)", c.CommandPath(), l)
				}
			}
			// R6-2: line continuations (bash, PowerShell, cmd) fold back.
			head, verb := "tq "+strings.Join(path[1:len(path)-1], " "), path[len(path)-1]
			for _, cont := range []string{"\\\n", "\\\r\n", "`\n", "`\r\n", "^\n", "^\r\n"} {
				l := head + " " + cont + "  " + verb + " d1"
				if !humanAnswerCommand(l) {
					t.Errorf("%s: a continued line %q is not denied", c.CommandPath(), l)
				}
			}
			// R6-5: prose that runs the executor is still denied.
			for _, l := range []string{`git commit -m "$(` + line + ` d1)"`, "echo `" + line + " d1`", `echo $(` + line + ` d1)`} {
				if !humanAnswerCommand(l) {
					t.Errorf("%s: %q runs the executor but is not denied", c.CommandPath(), l)
				}
			}
		case packs.ApprovalAsk:
			askers++
			if c.Name() != "approve" && c.Name() != "request" {
				t.Errorf("%s: an asking command is named approve or request", c.CommandPath())
			}
			if denied(line + " d1") {
				t.Errorf("%s only asks; agents may run it, but tq/human-answer denies it", c.CommandPath())
			}
		default:
			t.Errorf("%s: unknown %s annotation %q", c.CommandPath(), packs.AnnotationHumanOnly, kind)
		}
		if c.Name() == "approve" && kind == packs.HumanOnlyExecute {
			t.Errorf("%s: an approve command must only ask", c.CommandPath())
		}
	})
	if executors == 0 || askers == 0 {
		t.Fatalf("the walk found %d executors and %d asking commands; the test registrations are missing", executors, askers)
	}
}

// assertOnlyAsks runs fn (a pack approve command) and fails unless every
// ledger line it added to pack is an asked line and every approval it
// created is still pending. Pack approve commands reuse it.
func assertOnlyAsks(t *testing.T, ws, pack string, fn func()) {
	t.Helper()
	before, _ := ledger.Read(ws, pack)
	rowsBefore, _ := approval.List(ws, pack)
	known := map[string]bool{}
	for _, r := range rowsBefore {
		known[r.Decision] = true
	}
	fn()
	after, err := ledger.Read(ws, pack)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) <= len(before) {
		t.Fatalf("the approve command asked nothing")
	}
	for _, e := range after[len(before):] {
		if e.Event != ledger.Asked {
			t.Fatalf("the approve command wrote a %s line (%+v); it may only ask", e.Event, e)
		}
	}
	rows, err := approval.List(ws, pack)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if !known[r.Decision] && r.State != "pending" {
			t.Fatalf("the approve command left %s %s; it may only ask", r.Decision, r.State)
		}
	}
}

// R5-4: an approve command only asks (the test-registered db approve).
func TestApproveCommandOnlyAsks(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "packs:\n  db: { enabled: true }\n")
	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	os.Chdir(ws)
	assertOnlyAsks(t, "acme", "db", func() {
		if out, err := runRoot(t, "", "pack", "db", "approve"); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	})
}

// R6-5: prose that only mentions an executor is not denied.
func TestHumanAnswerIgnoresProse(t *testing.T) {
	for _, l := range []string{
		`git commit -m "pack ml: apply the promote after review"`,
		`git commit -am 'fix(pack): cloud apply uses the stored plan'`,
		`tq pack ml promote request churn@7 --note "will apply after review"`,
		`echo "run tq pack cloud apply d1 in your terminal"`,
	} {
		if humanAnswerCommand(l) {
			t.Errorf("prose denied: %s", l)
		}
	}
	for _, l := range []string{
		`git commit -m "x" && tq pack cloud apply d1`,
		`echo ok; tq pack ml promote apply churn@7`,
		`echo "x" > /tmp/o; tq hub answer c1 d1 --choice approve`,
	} {
		if !humanAnswerCommand(l) {
			t.Errorf("not denied: %s", l)
		}
	}
}
