package cloud

import (
	"reflect"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/policy"
)

// TestPolicyKeysMerge: every key of the iac-policy workstream the repo file
// may set has its own rule (F round 3), and each only adds checks: the repo
// adds IaC paths, project policies and required tags, and never drops or
// changes the manifest's.
func TestPolicyKeysMerge(t *testing.T) {
	trusted := iamPacks(t, `
cloud:
  enabled: true
  iac: [{path: infra/terraform, kind: terraform}]
  conftest: {policy: [policy/org]}
  tags: [owner]
`)
	repo := iamPacks(t, `
cloud:
  iac: [{path: infra/aws, kind: cloudformation}]
  conftest: {policy: [policy/team]}
  tags: [cost-center]
`)
	cfg := policy.Load(packs.Merge(trusted, repo).Pack("cloud"))
	wantIaC := []policy.Entry{{Path: "infra/terraform", Kind: "terraform"}, {Path: "infra/aws", Kind: "cloudformation"}}
	if !reflect.DeepEqual(cfg.IaC, wantIaC) {
		t.Errorf("iac = %v", cfg.IaC)
	}
	if want := []string{"policy/org", "policy/team"}; !reflect.DeepEqual(cfg.Conftest.Policy, want) {
		t.Errorf("conftest.policy = %v", cfg.Conftest.Policy)
	}
	if want := []string{"owner", "cost-center"}; !reflect.DeepEqual(cfg.Tags, want) {
		t.Errorf("tags = %v", cfg.Tags)
	}

	// An empty repo list drops nothing.
	empty := iamPacks(t, "cloud: {iac: [], conftest: {policy: []}, tags: []}")
	cfg = policy.Load(packs.Merge(trusted, empty).Pack("cloud"))
	if len(cfg.IaC) != 1 || len(cfg.Conftest.Policy) != 1 || len(cfg.Tags) != 1 {
		t.Errorf("an empty repo list dropped manifest entries: %+v", cfg)
	}

	// With a silent manifest the repo still adds.
	cfg = policy.Load(packs.Merge(iamPacks(t, "cloud: {enabled: true}"), repo).Pack("cloud"))
	if len(cfg.IaC) != 1 || len(cfg.Conftest.Policy) != 1 || len(cfg.Tags) != 1 {
		t.Errorf("silent manifest: %+v", cfg)
	}
}
