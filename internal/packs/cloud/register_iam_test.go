package cloud

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

func isolateIAM(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func iamPacks(t *testing.T, body string) map[string]manifest.PackConfig {
	t.Helper()
	var m map[string]manifest.PackConfig
	if err := yaml.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

type iamGit struct{}

func (iamGit) Show(ref, p string) ([]byte, error) { return nil, os.ErrNotExist }
func (iamGit) ListFiles() ([]string, error)       { return nil, nil }

func iamWrite(t *testing.T, root, rel string, body []byte) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

const iamConfig = `
cloud:
  enabled: true
  iac:
    - {path: infra/terraform, kind: terraform}
    - {path: infra/aws, kind: cloudformation}
  accounts: [111111111111]
  tenants: [00000000-0000-0000-0000-000000000000]
`

func iamInput(t *testing.T, root, config string, changed ...string) *packs.Input {
	t.Helper()
	cfg := packs.Merge(iamPacks(t, config), nil)
	var cs []packs.ChangedFile
	for _, c := range changed {
		cs = append(cs, packs.ChangedFile{Path: c, Status: 'A'})
	}
	in := &packs.Input{Root: root, Base: "HEAD", Changed: cs, Config: cfg, Git: iamGit{}, Budget: time.Now().Add(time.Minute)}
	in.SetWorkspace("acme", root)
	return in
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("iam", "testdata", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func iamResult(t *testing.T, in *packs.Input) (packs.Result, bool) {
	t.Helper()
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"cloud"}})
	for _, r := range rep.Results {
		if r.ID == "cloud/iam" {
			return r, true
		}
	}
	return packs.Result{}, false
}

func TestIAMRegistered(t *testing.T) {
	var ids []string
	for _, c := range packs.Checks("cloud") {
		ids = append(ids, c.ID)
	}
	found := false
	for _, id := range ids {
		if id == "cloud/iam" {
			found = true
		}
	}
	if !found {
		t.Fatalf("cloud/iam not registered: %v", ids)
	}
	var flag *packs.Flag
	for _, f := range packs.Flags("cloud") {
		if f.Name == "iam" {
			f := f
			flag = &f
		}
	}
	if flag == nil || flag.Raise == nil {
		t.Fatal("flag iam not registered with a Raise")
	}
	if want := []string{"iac-reviewer", "security-reviewer"}; !reflect.DeepEqual(flag.Reviewers, want) {
		t.Errorf("iam reviewers = %v", flag.Reviewers)
	}
}

func TestIAMConfigValidation(t *testing.T) {
	ok := "cloud: {accounts: [111111111111, \"222222222222\"], tenants: [00000000-0000-0000-0000-000000000000]}"
	if err := manifest.ValidatePacks(iamPacks(t, ok)); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	for name, body := range map[string]string{
		"short":     "cloud: {accounts: [1234]}",
		"scalar":    "cloud: {accounts: 111111111111}",
		"tenant":    "cloud: {tenants: [acme]}",
		"duplicate": "cloud: {tenants: [00000000-0000-0000-0000-000000000000, 00000000-0000-0000-0000-000000000000]}",
		"map":       "cloud: {accounts: {a: 111111111111}}",
	} {
		if err := manifest.ValidatePacks(iamPacks(t, body)); err == nil {
			t.Errorf("%s: invalid config accepted", name)
		}
	}
}

// TestIAMRepoCannotAddAccounts: accounts and tenants are honoured from the
// manifest only (spec D §4); a repo tq-packs.yaml must not widen "ours".
func TestIAMRepoCannotAddAccounts(t *testing.T) {
	trusted := iamPacks(t, "cloud: {enabled: true, accounts: [111111111111]}")
	repo := iamPacks(t, "cloud: {accounts: [222222222222], tenants: [44444444-4444-4444-4444-444444444444]}")
	cfg := packs.Merge(trusted, repo)
	s := cfg.Pack("cloud")
	if got := s.Strings("accounts"); !reflect.DeepEqual(got, []string{"111111111111"}) {
		t.Errorf("accounts = %v", got)
	}
	if got := s.Strings("tenants"); len(got) != 0 {
		t.Errorf("tenants = %v", got)
	}
}

func TestIAMCheck(t *testing.T) {
	isolateIAM(t)
	root := t.TempDir()
	iamWrite(t, root, "infra/terraform/plan.json", fixture(t, "plan.json"))
	iamWrite(t, root, "infra/aws/template.yaml", fixture(t, "template.yaml"))
	iamWrite(t, root, "infra/aws/policies/ci-trust.json", fixture(t, "policies/github-trust.json"))
	iamWrite(t, root, "infra/aws/package.json", fixture(t, "policies/not-a-policy.json"))
	iamWrite(t, root, "app/policy.json", fixture(t, "policies/identity.json")) // outside the IaC paths
	in := iamInput(t, root, iamConfig, "infra/terraform/plan.json", "infra/aws/template.yaml", "infra/aws/policies/ci-trust.json", "infra/aws/package.json", "app/policy.json")
	r, ok := iamResult(t, in)
	if !ok {
		t.Fatal("cloud/iam did not run")
	}
	if r.Status != packs.Fail || !r.Required {
		t.Errorf("status %s required %v: %s", r.Status, r.Required, r.Detail)
	}
	files := map[string]bool{}
	for _, f := range r.Findings {
		files[f.File] = true
	}
	var got []string
	for f := range files {
		got = append(got, f)
	}
	sort.Strings(got)
	if want := []string{"infra/aws/policies/ci-trust.json", "infra/aws/template.yaml", "infra/terraform/plan.json"}; !reflect.DeepEqual(got, want) {
		t.Errorf("files with findings = %v", got)
	}
	if !strings.Contains(r.Detail, "3 file(s)") || !strings.Contains(r.Detail, "2 suppressed") {
		t.Errorf("detail = %q", r.Detail)
	}
}

func TestIAMCheckSkipsOutsideIaC(t *testing.T) {
	isolateIAM(t)
	root := t.TempDir()
	iamWrite(t, root, "app/policy.json", fixture(t, "policies/identity.json"))
	in := iamInput(t, root, iamConfig, "app/policy.json")
	if r, ok := iamResult(t, in); ok && r.Status != packs.Skip {
		t.Errorf("ran outside the IaC paths: %s %s", r.Status, r.Detail)
	}
	// No iac paths configured: nothing applies.
	in = iamInput(t, root, "cloud: {enabled: true}", "app/policy.json")
	if r, ok := iamResult(t, in); ok && r.Status != packs.Skip {
		t.Errorf("ran without iac paths: %s %s", r.Status, r.Detail)
	}
}

func TestIAMCheckCleanAndBroken(t *testing.T) {
	isolateIAM(t)
	root := t.TempDir()
	iamWrite(t, root, "infra/aws/policies/safe.json", fixture(t, "policies/safe.json"))
	in := iamInput(t, root, iamConfig, "infra/aws/policies/safe.json")
	r, _ := iamResult(t, in)
	if r.Status != packs.OK || len(r.Findings) != 0 {
		t.Errorf("clean: %s %s %v", r.Status, r.Detail, r.Findings)
	}
	// A plan that is truncated: the reader refuses it, and the check fails
	// closed without echoing the bytes.
	iamWrite(t, root, "infra/terraform/broken.json", []byte(`{"format_version":"1.2","terraform_version":"1.15.2","resource_changes":[{"address":"aws_iam_policy.x","change":{"after":{"policy":"tester-secret-0000`))
	in = iamInput(t, root, iamConfig, "infra/terraform/broken.json")
	r, _ = iamResult(t, in)
	if r.Status != packs.Fail || len(r.Findings) != 1 || r.Findings[0].Level != packs.LevelError {
		t.Fatalf("broken: %s %s %v", r.Status, r.Detail, r.Findings)
	}
	if strings.Contains(r.Findings[0].Message, "tester-secret") || strings.Contains(r.Detail, "tester-secret") {
		t.Errorf("broken input echoed: %v", r.Findings[0])
	}
}

func TestIAMFlag(t *testing.T) {
	root := t.TempDir()
	for paths, want := range map[string]bool{
		"infra/terraform/iam.tf":                true,
		"infra/aws/roles/deploy.yaml":           false, // the directory, not the name
		"infra/aws/deploy-role.yaml":            true,
		"infra/aws/policies/bucket-policy.json": true,
		"infra/terraform/network.tf":            false,
		"app/iam.go":                            false,
	} {
		in := iamInput(t, root, iamConfig, paths)
		got := false
		for _, f := range packs.RaiseFlags(in) {
			if f == "iam" {
				got = true
			}
		}
		if got != want {
			t.Errorf("%s: iam raised = %v", paths, got)
		}
	}
}
