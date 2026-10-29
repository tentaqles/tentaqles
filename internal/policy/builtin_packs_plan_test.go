package policy

import "testing"

// planRuleSet builds the built-in rules with only the given packs enabled.
func planRuleSet(t *testing.T, packs ...string) *Set {
	t.Helper()
	on := map[string]bool{}
	for _, p := range packs {
		on[p] = true
	}
	s, errs := Build(Layers{Builtin: ForPacks(Builtin(), func(p string) bool { return on[p] })})
	if len(errs) > 0 {
		t.Fatalf("builtin rules do not compile: %v", errs)
	}
	return s
}

// tq/plan-read, tq/plan-print, tq/plan-commit (spec D §16): plans and state
// stay out of the transcript and out of git while the cloud pack is on.
func TestPlanRules(t *testing.T) {
	cases := []struct {
		name string
		call ToolCall
		id   string // "" = no plan rule
		act  Action
	}{
		// tq/plan-read: file tools
		{"read tfplan", ToolCall{Tool: "Read", Path: "infra/envs/prod/prod.tfplan"}, "tq/plan-read", Deny},
		{"read tfstate", ToolCall{Tool: "Read", Path: "infra/terraform.tfstate"}, "tq/plan-read", Deny},
		{"read tfstate backup", ToolCall{Tool: "Read", Path: `C:\r\acme\infra\terraform.tfstate.backup`}, "tq/plan-read", Deny},
		{"grep tfstate", ToolCall{Tool: "Grep", Path: "infra/terraform.tfstate"}, "tq/plan-read", Deny},
		{"read plan store", ToolCall{Tool: "Read", Path: "/home/tester/.tentaqles/packs/acme/cloud/plans/p1.json"}, "tq/plan-read", Deny},
		{"read cdk assets", ToolCall{Tool: "Read", Path: "infra/cdk/cdk.out/AcmeStack.assets.json"}, "tq/plan-read", Deny},
		{"read tf source ok", ToolCall{Tool: "Read", Path: "infra/main.tf"}, "", ""},
		{"read lock file ok", ToolCall{Tool: "Read", Path: "infra/.terraform.lock.hcl"}, "", ""},
		{"read cdk template ok", ToolCall{Tool: "Read", Path: "infra/cdk/cdk.out/AcmeStack.template.json"}, "", ""},
		{"read other pack state ok", ToolCall{Tool: "Read", Path: "/home/tester/.tentaqles/packs/acme/db/drills.jsonl"}, "", ""},
		// tq/plan-read: shell prints
		{"cat tfstate", ToolCall{Tool: "Bash", Command: "cat infra/terraform.tfstate"}, "tq/plan-read", Deny},
		{"grep plan", ToolCall{Tool: "Bash", Command: "grep -a password prod.tfplan"}, "tq/plan-read", Deny},
		{"jq state", ToolCall{Tool: "Bash", Command: "jq '.resources' terraform.tfstate"}, "tq/plan-read", Deny},
		{"get-content plan store", ToolCall{Tool: "PowerShell", Command: `Get-Content $env:TQ_HOME\packs\acme\cloud\plans\p1.json`}, "tq/plan-read", Deny},
		{"type plan store", ToolCall{Tool: "PowerShell", Command: `type C:\Users\tester\.tentaqles\packs\acme\cloud\plans\p1.json`}, "tq/plan-read", Deny},
		{"ls plans ok", ToolCall{Tool: "Bash", Command: "ls infra/*.tfplan"}, "", ""},
		{"cat main.tf ok", ToolCall{Tool: "Bash", Command: "cat infra/main.tf"}, "", ""},
		// tq/plan-print
		{"terraform show", ToolCall{Tool: "Bash", Command: "terraform show -json prod.tfplan"}, "tq/plan-read", Deny},
		{"terraform show state", ToolCall{Tool: "Bash", Command: "terraform show"}, "tq/plan-print", Ask},
		{"terraform chdir show", ToolCall{Tool: "Bash", Command: "terraform -chdir=infra show -json"}, "tq/plan-print", Ask},
		{"tofu show", ToolCall{Tool: "Bash", Command: "tofu show -json"}, "tq/plan-print", Ask},
		{"terraform output", ToolCall{Tool: "Bash", Command: "terraform output -json"}, "tq/plan-print", Ask},
		{"terraform state pull", ToolCall{Tool: "PowerShell", Command: "terraform state pull"}, "tq/plan-print", Ask},
		{"pulumi show secrets", ToolCall{Tool: "Bash", Command: "pulumi stack output --show-secrets -s acme-dev"}, "tq/plan-print", Ask},
		{"pulumi output ok", ToolCall{Tool: "Bash", Command: "pulumi stack output -s acme-dev"}, "", ""},
		{"terraform plan ok", ToolCall{Tool: "Bash", Command: "terraform plan -out=prod.tfplan"}, "", ""},
		{"tq pack cloud show ok", ToolCall{Tool: "Bash", Command: "tq pack cloud show p1"}, "", ""},
		{"git show ok", ToolCall{Tool: "Bash", Command: "git show HEAD"}, "", ""},
		// tq/plan-commit
		{"git add tfplan", ToolCall{Tool: "Bash", Command: "git add infra/prod.tfplan"}, "tq/plan-commit", Deny},
		{"git add tfstate", ToolCall{Tool: "Bash", Command: "git add -f terraform.tfstate"}, "tq/plan-commit", Deny},
		{"git add .terraform", ToolCall{Tool: "Bash", Command: "git add infra/.terraform/"}, "tq/plan-commit", Deny},
		{"git -C add .terraform", ToolCall{Tool: "PowerShell", Command: `git -C infra add .terraform\providers`}, "tq/plan-commit", Deny},
		{"git add plan store", ToolCall{Tool: "Bash", Command: "git add ~/.tentaqles/packs/acme/cloud/plans/p1.json"}, "tq/plan-commit", Deny},
		{"git add dump", ToolCall{Tool: "Bash", Command: "git add backups/main.dump"}, "tq/plan-commit", Deny},
		{"git add bacpac", ToolCall{Tool: "Bash", Command: "git add db/acme.bacpac"}, "tq/plan-commit", Deny},
		{"git add lock file ok", ToolCall{Tool: "Bash", Command: "git add infra/.terraform.lock.hcl infra/main.tf"}, "", ""},
		{"git add -A ok (gitignore and no-plan-files)", ToolCall{Tool: "Bash", Command: "git add -A"}, "", ""},
		{"commit message mentioning tfplan ok", ToolCall{Tool: "Bash", Command: "git commit -m \"ignore *.tfplan\""}, "", ""},
	}
	s := planRuleSet(t, "cloud")
	for _, c := range cases {
		d := s.Evaluate(c.call)
		var got Action
		var id string
		for _, m := range d.Matched {
			if m.Pack == "cloud" && (m.ID == "tq/plan-read" || m.ID == "tq/plan-print" || m.ID == "tq/plan-commit") {
				if m.Action.rank() > got.rank() {
					got, id = m.Action, m.ID
				}
			}
		}
		if id != c.id || got != c.act {
			t.Errorf("%s: got %q %q, want %q %q", c.name, id, got, c.id, c.act)
		}
	}
	// Off while the cloud pack is disabled.
	off := planRuleSet(t, "db")
	for _, c := range cases {
		for _, m := range off.Evaluate(c.call).Matched {
			if m.Pack == "cloud" && m.ID != "tq/hub-store-write" {
				t.Errorf("%s: %s active without the cloud pack", c.name, m.ID)
			}
		}
	}
	// Every plan rule names its pack and points to tq pack cloud where it asks.
	for _, r := range PackRules() {
		switch r.ID {
		case "tq/plan-read", "tq/plan-print", "tq/plan-commit":
			if r.Pack != "cloud" || r.Reason == "" {
				t.Errorf("%s: pack %q reason %q", r.ID, r.Pack, r.Reason)
			}
		}
	}
}
