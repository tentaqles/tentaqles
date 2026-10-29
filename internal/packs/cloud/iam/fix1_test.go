package iam

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

func diffAddrs(t *testing.T, label string, got, want map[string][]string) {
	t.Helper()
	for k, v := range want {
		if !reflect.DeepEqual(got[k], v) {
			t.Errorf("%s %s: got %v, want %v", label, k, got[k], v)
		}
	}
	for k, v := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("%s: unexpected findings on %s: %v", label, k, v)
		}
	}
}

// M1, M2, M5 and minors 2 and 4 on a CDK-shaped template: ARNs and
// principals written as Fn::Join with Ref AWS::Partition / AWS::AccountId.
func TestCDKTemplate(t *testing.T) {
	got := byAddr(lintFixture(t, "cdk.out/AcmeStack.template.json").Findings)
	diffAddrs(t, "cdk", got, map[string][]string{
		"AdminRole":            {"error IAM004", "error IAM006"},
		"GhRefRole":            {"error IAM006", "error IAM006"},
		"GhSubRole":            {"error IAM006", "error IAM006"},
		"GhJoinRole":           {"error IAM006"},
		"ForeignBucketPolicy":  {"error IAM001"},
		"NotActionQueuePolicy": {"error IAM003"},
		"ForeignKey":           {"error IAM001"},
		"CondBucketPolicy":     {"warn IAM007"},
		"PassServiceRoles":     {"error IAM005"},
	})
}

func TestIntrinsicString(t *testing.T) {
	join := map[string]any{"Fn::Join": []any{"", []any{"arn:", map[string]any{"Ref": "AWS::Partition"}, ":iam::aws:policy/AdministratorAccess"}}}
	if s := intrinsicString(join); s != "arn:aws:iam::aws:policy/AdministratorAccess" {
		t.Errorf("join = %q", s)
	}
	sub := map[string]any{"Fn::Sub": "arn:${AWS::Partition}:iam::${AWS::AccountId}:root"}
	s := intrinsicString(sub)
	if acct, known := accountOf(s); known {
		t.Errorf("own account placeholder read as account %q (%q)", acct, s)
	}
	if !strings.HasPrefix(s, "arn:aws:iam::") {
		t.Errorf("sub = %q", s)
	}
	if s := intrinsicString(map[string]any{"Ref": "GithubProvider"}); !unresolved(s) {
		t.Errorf("a Ref must stay unresolved: %q", s)
	}
}

// M4: a policy unknown at plan time is a warn, not a silent pass, and a
// deferred aws_iam_policy_document data read is linted.
func TestUnknownAtPlanTime(t *testing.T) {
	res := lintFixture(t, "unknown-plan.json")
	got := byAddr(res.Findings)
	diffAddrs(t, "unknown-plan", got, map[string][]string{
		"aws_iam_policy.later":              {"warn IAM000"},
		"aws_iam_role.later":                {"warn IAM000"},
		"aws_iam_role_policy.garbled":       {"warn IAM000"},
		"data.aws_iam_policy_document.star": {"error IAM001"},
		"data.aws_iam_policy_document.gh":   {"error IAM006"},
	})
	if res.Documents < 3 {
		t.Errorf("documents = %d", res.Documents)
	}
}

// M3: a GitHub sub condition that the reader redacted cannot be verified:
// it fails closed.
func TestRedactedGitHubSubFailsClosed(t *testing.T) {
	got := byAddr(lintFixture(t, "plan.json").Findings)
	if want := []string{"error IAM006"}; !reflect.DeepEqual(got["aws_iam_role.gh_wild"], want) {
		t.Errorf("gh_wild: %v", got["aws_iam_role.gh_wild"])
	}
}

// M5: IAM001 and IAM003 apply to statements with a Principal; the default
// KMS key policy (kms:* for the own root) is the only exemption.
func TestPrincipalStatementsGetIAM001And003(t *testing.T) {
	for _, c := range []struct {
		name, doc string
		o         Options
		want      []string
	}{
		{"bucket s3:* to foreign", `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::222222222222:root"},"Action":"s3:*","Resource":"arn:aws:s3:::acme/*"}]}`, testOpts, []string{"error IAM001"}},
		{"notaction resource", `{"Statement":[{"Effect":"Allow","Principal":{"Service":"sns.amazonaws.com"},"NotAction":"sqs:DeleteQueue","Resource":"arn:aws:sqs:eu-west-1:111111111111:q"}]}`, testOpts, []string{"error IAM003"}},
		{"trust sts:*", `{"Statement":[{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:*"}]}`, testOpts, []string{"error IAM001"}},
		// kms:* on a standalone document is not known to be a key policy.
		{"kms in standalone", `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::111111111111:root"},"Action":"kms:*","Resource":"*"}]}`, testOpts, []string{"error IAM001"}},
	} {
		res, _, _ := Standalone("t.json", []byte(c.doc), c.o)
		var got []string
		for _, f := range res.Findings {
			got = append(got, f.Level+" "+f.Rule)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestKeyPolicyExemption(t *testing.T) {
	own := `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::%s:root"},"Action":"kms:*","Resource":"*"}]}`
	for _, c := range []struct {
		acct string
		o    Options
		want bool // a finding
	}{
		{"111111111111", testOpts, false},
		{"222222222222", testOpts, true},
		{"222222222222", Options{}, false}, // accounts unset: any root
	} {
		iss, ok := lintDoc(strings.Replace(own, "%s", c.acct, 1), docKeyPolicy, c.o)
		if !ok {
			t.Fatal("not decoded")
		}
		if (len(iss) > 0) != c.want {
			t.Errorf("%s %v: %v", c.acct, c.o.Accounts, iss)
		}
	}
	// kms:* for a role, not the root, is not the default key policy.
	iss, _ := lintDoc(`{"Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::111111111111:role/app"},"Action":"kms:*","Resource":"*"}]}`, docKeyPolicy, testOpts)
	if len(iss) != 1 || iss[0].Rule != "IAM001" {
		t.Errorf("role kms:*: %v", iss)
	}
}

// Minor 1: without packs.cloud.accounts, an AWS account principal in a trust
// policy without an ExternalId is a warn, not silence.
func TestNoAccountsWarnsOnAccountPrincipal(t *testing.T) {
	doc := `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::222222222222:root"},"Action":"sts:AssumeRole"}]}`
	res, _, _ := Standalone("t.json", []byte(doc), Options{})
	if len(res.Findings) != 1 || res.Findings[0].Level != packs.LevelWarn || res.Findings[0].Rule != "IAM006" {
		t.Errorf("without accounts: %s", render(res.Findings))
	}
	withExt := `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"222222222222"},"Action":"sts:AssumeRole","Condition":{"StringEquals":{"sts:ExternalId":"x"}}}]}`
	if res, _, _ := Standalone("t.json", []byte(withExt), Options{}); len(res.Findings) != 0 {
		t.Errorf("with ExternalId: %s", render(res.Findings))
	}
}

// Minor 2: a path-prefixed role wildcard is a wildcard resource for IAM005.
func TestIAM005PathWildcard(t *testing.T) {
	for r, want := range map[string]bool{
		"arn:aws:iam::111111111111:role/service-role/*": true,
		"arn:aws:iam::111111111111:role/*":              true,
		"arn:aws:iam::111111111111:role/app":            false,
		"arn:aws:iam::111111111111:role/app-*":          false,
	} {
		if got := wildResource([]string{r}); got != want {
			t.Errorf("wildResource(%q) = %v", r, got)
		}
	}
}

// Minor 4: a Condition that is an intrinsic or redacted is not resolvable:
// IAM005 and IAM007 warn instead of passing.
func TestUnresolvableCondition(t *testing.T) {
	for _, c := range []string{
		`{"Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::acme/*","Condition":"[REDACTED:secret_key]"}]}`,
		`{"Statement":[{"Effect":"Allow","Action":"iam:PassRole","Resource":"arn:aws:iam::111111111111:role/*","Condition":{"Fn::If":["c",{},{}]}}]}`,
	} {
		res, _, _ := Standalone("t.json", []byte(c), testOpts)
		if len(res.Findings) != 1 || res.Findings[0].Level != packs.LevelWarn {
			t.Errorf("%s: %s", c, render(res.Findings))
		}
	}
}

// Minor 5: a role assignment inside a nested deployment is judged at the
// nested deployment's scope.
func TestNestedDeploymentScope(t *testing.T) {
	got := byAddr(lintFixture(t, "nested.json").Findings)
	diffAddrs(t, "nested", got, map[string][]string{
		"[guid(subscription().id, 'outer-owner')]":  {"error AZR001"},
		"[guid(resourceGroup().id, 'inner-owner')]": {"warn AZR001"},
		"[guid(resourceGroup().id, 'inner-uaa')]":   {"warn AZR001"},
	})
}
