package tools

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func strs(fs []packs.Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.String())
	}
	return out
}

func eq(t *testing.T, what string, got []packs.Finding, want []string) {
	t.Helper()
	if g := strs(got); !reflect.DeepEqual(g, want) {
		t.Errorf("%s:\n got %q\nwant %q", what, g, want)
	}
}

func TestParseCheckov(t *testing.T) {
	root := t.TempDir()
	fs, err := ParseCheckov(fixture(t, "checkov.json"), root, "infra/terraform", "")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "checkov", fs, []string{
		"infra/terraform/main.tf:12 error checkov:CKV_AWS_24 aws_security_group.web: Ensure no security groups allow ingress from 0.0.0.0:0 to port 22",
		"infra/terraform/modules/db/main.tf:1 error checkov:CKV_AWS_16 module.db.aws_db_instance.main: Ensure all data stored in the RDS is securely encrypted at rest",
	})
	// A plan: the display name replaces the file, no line.
	fs, _ = ParseCheckov(fixture(t, "checkov.json"), root, "", "plan:p1")
	if fs[0].File != "plan:p1" || fs[0].Line != 0 {
		t.Errorf("plan finding = %+v", fs[0])
	}
	// The absolute path wins over the relative one.
	abs := filepath.Join(root, "infra", "x.tf")
	raw := []byte(`{"check_type":"terraform","results":{"failed_checks":[{"check_id":"CKV_X","check_name":"n","file_path":"/x.tf","file_abs_path":` + jsonStr(abs) + `,"file_line_range":[2,3]}]}}`)
	fs, err = ParseCheckov(raw, root, "infra", "")
	if err != nil || len(fs) != 1 || fs[0].File != "infra/x.tf" {
		t.Errorf("abs = %v %v", fs, err)
	}
	if fs, err := ParseCheckov(fixture(t, "checkov-empty.json"), root, "x", ""); err != nil || len(fs) != 0 {
		t.Errorf("empty = %v %v", fs, err)
	}
	for _, bad := range []string{"", "not json", "[{"} {
		if _, err := ParseCheckov([]byte(bad), root, "x", ""); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func jsonStr(s string) string { return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"` }

func TestParseTrivy(t *testing.T) {
	fs, err := ParseTrivy(fixture(t, "trivy.json"), t.TempDir(), "infra/terraform")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "trivy", fs, []string{
		"infra/terraform/main.tf:14 error trivy:AVD-AWS-0107 aws_security_group.web: Security group rule allows ingress from public internet.",
		"infra/terraform/main.tf:13 warn trivy:AVD-AWS-0124 aws_security_group.web: Security group rule does not have a description.",
		"infra/terraform/modules/db/main.tf:4 error trivy:AVD-AWS-0080 module.db.aws_db_instance.main: Instance does not have storage encryption enabled.",
		"infra/terraform/modules/db/main.tf:1 warn trivy:AVD-AWS-0176 RDS IAM Database Authentication Disabled",
	})
	fs, _ = ParseTrivy([]byte(`{"Results":[{"Target":"a.tf","Misconfigurations":[{"ID":"X","Severity":"WEIRD","Status":"FAIL"}]}]}`), "", "")
	if fs[0].Level != packs.LevelError {
		t.Errorf("unknown severity = %s, want error (fail closed)", fs[0].Level)
	}
	if _, err := ParseTrivy([]byte("oops"), "", ""); err == nil {
		t.Error("bad trivy output parsed")
	}
}

func TestParseConftest(t *testing.T) {
	loc := func(file, addr string) (string, int) {
		if file == "in0000.json" && addr == "aws_kms_key.acme" {
			return "plan:p1", 7
		}
		return "infra/x.json", 0
	}
	fs, err := ParseConftest(fixture(t, "conftest.json"), loc)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "conftest", fs, []string{
		"plan:p1:7 error conftest:TQR006 aws_kms_key.acme: automatic key rotation is off on a symmetric KMS key",
		"infra/x.json warn conftest:TQR002 aws_lb.acme: the load balancer has no access logs",
		"infra/x.json error conftest:main acme project rule",
	})
	if _, err := ParseConftest([]byte("{"), loc); err == nil {
		t.Error("bad conftest output parsed")
	}
}

func TestParseTflint(t *testing.T) {
	fs, errs, err := ParseTflint(fixture(t, "tflint.json"), t.TempDir(), "infra/terraform")
	if err != nil || len(errs) != 0 {
		t.Fatal(err, errs)
	}
	eq(t, "tflint", fs, []string{
		`infra/terraform/variables.tf:3 warn tflint:terraform_unused_declarations variable "acme_unused" is declared but not used`,
		`infra/terraform/main.tf:7 error tflint:aws_instance_invalid_type "t1.2xlarge" is an invalid value as instance_type`,
	})
	_, errs, err = ParseTflint(fixture(t, "tflint-error.json"), "", "x")
	if err != nil || len(errs) != 1 || !strings.Contains(errs[0], "tflint --init") {
		t.Errorf("errors = %v %v", errs, err)
	}
}

func TestParseSARIF(t *testing.T) {
	root := t.TempDir()
	raw := strings.ReplaceAll(string(fixture(t, "bicep.sarif")), "/ROOT", "/"+strings.TrimPrefix(filepath.ToSlash(root), "/"))
	fs, err := ParseSARIF([]byte(raw), "bicep", root, "infra/bicep/main.bicep")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "bicep", fs, []string{
		`infra/bicep/main.bicep:1 warn bicep:no-unused-params Parameter "unusedParam" is declared but never used. [https://aka.ms/bicep/linter-diagnostics#no-unused-params]`,
		`infra/bicep/main.bicep:3 error bicep:BCP029 The resource type is not valid. Specify a valid resource type of format "<type-name>@<apiVersion>". [https://aka.ms/bicep/core-diagnostics#BCP029]`,
	})
	fs, _ = ParseSARIF([]byte(`{"runs":[{"results":[{"ruleId":"x","level":"note","message":{"text":"m"}}]}]}`), "bicep", root, "a.bicep")
	if fs[0].File != "a.bicep" || fs[0].Level != packs.LevelWarn {
		t.Errorf("no location = %+v", fs[0])
	}
}

func TestParseCfnLint(t *testing.T) {
	fs, err := ParseCfnLint(fixture(t, "cfn-lint.json"), t.TempDir(), 2)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "cfn-lint", fs, []string{
		"infra/cfn/template.yaml:8 error cfn-lint:E3002 Additional properties are not allowed ('BucketNam' was unexpected)",
		"infra/cfn/template.yaml:3 warn cfn-lint:W2001 Parameter AcmeUnused not used.",
	})
	if fs, err := ParseCfnLint(nil, "", 0); err != nil || len(fs) != 0 {
		t.Errorf("empty = %v %v", fs, err)
	}
	for c, want := range map[int]bool{0: true, 2: true, 4: true, 6: true, 8: true, 14: true, 1: false, 16: false, 32: false, -1: false} {
		if cfnLintOK(c) != want {
			t.Errorf("cfnLintOK(%d) = %v", c, !want)
		}
	}
}

func TestMessagesAreRedacted(t *testing.T) {
	// A tool that echoes a credential into its message never puts it in a finding.
	raw := []byte(`[{"Filename":"t.yaml","Level":"Error","Location":{"Start":{"LineNumber":1}},"Message":"bad value AKIAABCDEFGHIJKLMNOP here","Rule":{"Id":"E1"}}]`) // tq:allow-secret (fake fixture)
	fs, err := ParseCfnLint(raw, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fs[0].Message, "AKIAABCDEFGHIJKLMNOP") { // tq:allow-secret (fake fixture)
		t.Errorf("message not redacted: %s", fs[0].Message)
	}
}

type blobGit map[string]string

func (g blobGit) Show(ref, p string) ([]byte, error) {
	if b, ok := g[p]; ok && ref == ":" {
		return []byte(b), nil
	}
	return nil, os.ErrNotExist
}

func (g blobGit) ListFiles() ([]string, error) {
	var out []string
	for p := range g {
		out = append(out, p)
	}
	return out, nil
}

// TestStagedTree: with --staged, the dir tools run over a scratch copy of
// the index versions of the files under the IaC paths, not the working tree.
func TestStagedTree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "infra"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, "infra", "main.tf"), []byte("working tree"), 0o644)
	in := &packs.Input{Root: root, Staged: true, Git: blobGit{"infra/main.tf": "staged", "docs/x.md": "other"}}
	in.SetWorkspace("acme", root)
	dir, cleanup, err := tree(in, []string{"infra"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "infra", "main.tf"))
	if err != nil || string(b) != "staged" {
		t.Errorf("staged copy = %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", "x.md")); err == nil {
		t.Error("a file outside the IaC paths was staged")
	}
	cleanup()
	if _, err := os.Stat(dir); err == nil {
		t.Error("scratch tree not removed")
	}
	in.Staged = false
	if d, _, _ := tree(in, nil); d != root {
		t.Errorf("unstaged tree = %s", d)
	}
}
