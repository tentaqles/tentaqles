package policy

import "testing"

// Security re-review r2-M1: the forms that print state or secret outputs in
// clear are tq/plan-secret-print (locked secrets, ask); plain terraform
// show and output (sensitive values masked) stay tq/plan-print (packs).
func TestPlanSecretPrint(t *testing.T) {
	t.Setenv("TQ_HOME", "")
	s := planRuleSet(t, "cloud")
	for _, c := range []struct {
		cmd    string
		secret bool
		print  bool
	}{
		{"terraform state pull", true, true},
		{"terraform state show aws_db_instance.main", true, true},
		{"tofu state pull > /tmp/x", true, true},
		{"terraform -chdir=infra state pull", true, true},
		{"terraform show -json", true, true},
		{"terraform output -json", true, true},
		{"terraform output -raw db_password", true, true},
		{"tofu output -json", true, true},
		{"pulumi stack output --show-secrets -s acme-dev", true, false},
		{"pulumi stack export --show-secrets", true, false},
		{"pulumi config --show-secrets", true, false},
		{"pulumi stack --show-secrets", true, false},
		{"terraform show", false, true},
		{"terraform output", false, true},
		{"terraform output db_endpoint", false, true},
		{"terraform plan -out=x", false, false},
		{"pulumi stack output", false, false},
	} {
		d := s.Evaluate(ToolCall{Tool: "Bash", Command: c.cmd})
		gotS, gotP := matched(d.Matched, "tq/plan-secret-print"), matched(d.Matched, "tq/plan-print")
		if gotS != c.secret || (gotP != c.print && !c.secret) {
			t.Errorf("%q: secret-print %v, plan-print %v; want %v, %v", c.cmd, gotS, gotP, c.secret, c.print)
		}
		if c.secret && d.Action != Ask {
			t.Errorf("%q: action %q, want ask", c.cmd, d.Action)
		}
	}
}
