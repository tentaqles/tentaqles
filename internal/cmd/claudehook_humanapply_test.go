package cmd

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/policy"
)

// Final review, correctness m5: tq/human-answer matches apply only as a
// subcommand word of tq pack <pack> ..., before any --, never inside a path
// or a flag value. Both halves (the static core rule and the hook-time
// check) agree.
func TestHumanAnswerApplyIsAWord(t *testing.T) {
	core, errs := policy.Build(policy.Layers{Builtin: policy.ForPacks(policy.Builtin(), nil)})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	static := func(cmd string) bool {
		for _, r := range core.Evaluate(policy.ToolCall{Tool: "Bash", Command: cmd}).Matched {
			if r.ID == "tq/human-answer" {
				return true
			}
		}
		return false
	}
	for _, cmd := range []string{
		"tq pack ml promote apply churn@7",
		"tq pack cloud apply d1",
		"tq pack db migrate apply d1",
		"tq pack ml --workspace acme promote apply churn@7",
		"tq pack cloud --yes destroy --then-apply d1 apply",
	} {
		if !humanAnswerCommand(cmd) || !static(cmd) {
			t.Errorf("%q: hook %v, static %v; want both to deny", cmd, humanAnswerCommand(cmd), static(cmd))
		}
	}
	// an argv list: the hook-time half reads it (the static rule needs tq
	// followed by a space)
	if argv := `python -c "import subprocess; subprocess.run(['tq','pack','ml','promote','apply'])"`; !humanAnswerCommand(argv) {
		t.Errorf("%q: not denied at hook time", argv)
	}
	for _, cmd := range []string{
		"tq pack check --paths k8s/apply.yaml",
		"tq pack check --paths db/apply-fees.sql",
		"tq pack cloud plan --env dev -- terraform plan -var-file=apply.tfvars",
		"tq pack cloud plan --env dev -- terraform apply",
		"tq pack data check --name=apply",
		"tq pack ml promote request churn@7",
	} {
		if humanAnswerCommand(cmd) || static(cmd) {
			t.Errorf("%q: hook %v, static %v; want neither", cmd, humanAnswerCommand(cmd), static(cmd))
		}
	}
}
