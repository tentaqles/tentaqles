package cloud

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/policy"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/tools"
	"gopkg.in/yaml.v3"
)

// D-CP: the IaC policy (spec D §7): config keys iac, conftest and tags; the
// flags iac and baseline_change; the checks cloud/iac-policy,
// cloud/no-plan-files and the six external tool checks.

// IaCReviewer is the reviewer the iac and baseline_change flags select.
const IaCReviewer = "iac-reviewer"

func init() {
	packs.RegisterPack(packs.Pack{
		Name: "cloud",
		ConfigKeys: map[string]func(yaml.Node) error{
			"iac":      policy.ValidateIaC,
			"conftest": policy.ValidateConftest,
			"tags":     policy.ValidateTags,
		},
		// More IaC paths, project policies and required tags only add checks.
		Merge: map[string]packs.MergeRule{
			"iac":             packs.Union(),
			"conftest.policy": packs.Union(),
			"tags":            packs.Union(),
		},
		Flags: []packs.Flag{
			{Name: "iac", Reviewers: []string{IaCReviewer}, Raise: raiseIaC},
			{Name: packs.FlagBaselineChange, Reviewers: []string{IaCReviewer}},
		},
	})

	packs.Register(packs.Check{
		ID: "cloud/iac-policy", Pack: "cloud", Kind: packs.Builtin,
		Applies: func(in *packs.Input) bool {
			cfg := policy.Load(in.Settings())
			return cfg.Changed(in) || policy.HasPlans(in)
		},
		Run: runIaCPolicy,
	})
	packs.Register(packs.Check{
		ID: "cloud/no-plan-files", Pack: "cloud", Kind: packs.Builtin,
		Run: func(_ context.Context, in *packs.Input) packs.Result { return policy.NoPlanFiles(in) },
	})

	ext := []struct {
		name, tool, min string
		timeout         time.Duration
		run             func(context.Context, *packs.Input) packs.Result
	}{
		{"checkov", tools.Checkov, "", 300 * time.Second, tools.RunCheckov},
		{"trivy", tools.Trivy, "", 0, tools.RunTrivy},
		{"conftest", tools.Conftest, tools.MinConftest, 0, tools.RunConftest},
		{"tflint", tools.Tflint, "", 0, tools.RunTflint},
		{"bicep-lint", tools.Bicep, "", 0, tools.RunBicep},
		{"cfn-lint", tools.CfnLint, "", 0, tools.RunCfnLint},
	}
	for _, e := range ext {
		packs.Register(packs.Check{
			ID: "cloud/" + e.name, Pack: "cloud", Kind: packs.External,
			Tool: e.tool, MinVersion: e.min, Timeout: e.timeout,
			Applies: tools.Applies(e.tool),
			Run:     e.run,
		})
	}
}

func runIaCPolicy(_ context.Context, in *packs.Input) packs.Result {
	cfg := policy.Load(in.Settings())
	docs, notes, err := policy.Collect(in, cfg)
	if err != nil {
		return packs.Result{Status: packs.Fail, Detail: err.Error(), Findings: []packs.Finding{}}
	}
	o := policy.Evaluate(docs)
	fs := append(notes, o.Findings...)
	for rule, n := range o.Suppressed {
		packs.Count("cloud", "suppression:"+strings.ToLower(rule), n)
	}
	plans := 0
	for _, d := range docs {
		if d.Plan != nil {
			plans++
		}
	}
	res := packs.Result{Status: packs.OK, Findings: fs}
	errs, warns := res.Counts()
	sup := 0
	for _, n := range o.Suppressed {
		sup += n
	}
	res.Detail = fmt.Sprintf("%d resource(s) in %d template(s) and %d plan(s): %d error(s), %d warning(s), %d suppressed",
		o.Resources, len(docs)-plans, plans, errs, warns, sup)
	if errs > 0 {
		res.Status = packs.Fail
	}
	return res
}

var cfnTemplateName = regexp.MustCompile(`(?i)(^|/)template\.ya?ml$`)

// raiseIaC: a changed path under packs.cloud.iac[].path, or an IaC file by
// name (spec D §15). A template.yaml counts when it declares
// AWSTemplateFormatVersion.
func raiseIaC(in *packs.Input) bool {
	cfg := policy.Load(in.Settings())
	if cfg.Changed(in) {
		return true
	}
	if in.AnyChanged("*.tf", "*.tf.json", "*.tofu", "*.bicep", "*.bicepparam", "cdk.json", "Pulumi.yaml", "Pulumi.*.yaml", "Pulumi.yml", "Pulumi.*.yml") {
		return true
	}
	for _, p := range in.ChangedPaths() {
		if !cfnTemplateName.MatchString(p) {
			continue
		}
		b, err := in.ReadNew(p, 64<<10)
		if err != nil || strings.Contains(string(b), "AWSTemplateFormatVersion") {
			return true // unreadable counts as raised (review more, never less)
		}
	}
	return false
}
