package cloud

// Workstream D-CI (spec D §8, §15): the IAM least-privilege lint.
//
// Check:  cloud/iam (built-in): IAM001-IAM007, AZR001-AZR004, GCP001, plus
//         SUP001 for a tq-allow without a reason.
// Keys:   accounts, tenants (trusted layer only: a repo file cannot widen
//         what counts as "ours").
// Flag:   iam (iac-reviewer, security-reviewer).

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/iam"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/readers"
	"gopkg.in/yaml.v3"
)

func init() {
	packs.RegisterPack(packs.Pack{
		Name: "cloud",
		ConfigKeys: map[string]func(yaml.Node) error{
			"accounts": iam.ValidateAccounts,
			"tenants":  iam.ValidateTenants,
		},
		// accounts and tenants get no Merge rule: the default
		// (MergeIgnoreRepo) keeps them trusted-layer only, as spec D §4
		// requires. Adding an account to "ours" would loosen IAM006.
		Flags: []packs.Flag{
			{Name: "iam", Reviewers: []string{"iac-reviewer", "security-reviewer"}, Raise: raiseIAM},
		},
	})
	packs.Register(packs.Check{ID: "cloud/iam", Pack: "cloud", Kind: packs.Builtin,
		Applies: func(in *packs.Input) bool { return len(iamFiles(in)) > 0 },
		Run:     runIAM})
}

// raiseIAM: a changed path under an IaC path whose name says IAM
// (*iam*, *role*, *policy*). Paths and config only (spec C §2.9).
func raiseIAM(in *packs.Input) bool {
	paths := iam.IaCPaths(in.Settings())
	for _, p := range in.ChangedPaths() {
		if iam.Under(p, paths) && iam.IAMNamed(p) {
			return true
		}
	}
	return false
}

// iamFiles are the changed JSON and YAML files under the IaC paths.
func iamFiles(in *packs.Input) []string {
	paths := iam.IaCPaths(in.Settings())
	if len(paths) == 0 {
		return nil
	}
	var out []string
	for _, p := range in.ChangedPaths() {
		if iam.Under(p, paths) && iam.Candidate(p) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func runIAM(ctx context.Context, in *packs.Input) packs.Result {
	s := in.Settings()
	o := iam.Options{Accounts: iam.Accounts(s), Tenants: iam.Tenants(s)}
	var all iam.Result
	read := 0
	for _, p := range iamFiles(in) {
		if ctx.Err() != nil {
			return packs.Result{Status: packs.Fail, Detail: "timed out"}
		}
		raw, err := in.ReadNew(p, int64(readers.MaxInput))
		if err != nil {
			return packs.Result{Status: packs.Fail, Detail: "cannot read " + p}
		}
		res, handled, err := iam.File(p, raw, o)
		if err != nil {
			// A file that looked like a plan or template but did not read:
			// its IAM is unknown, so fail closed. Only the error class is
			// shown, never the bytes.
			all.Findings = append(all.Findings, packs.Finding{File: p, Line: 1, Level: packs.LevelError, Rule: "IAM000",
				Message: "not read as an IaC input (" + readErrClass(err) + "); its IAM was not checked"})
			read++
			continue
		}
		if !handled {
			continue
		}
		read++
		mergeResult(&all, res)
	}
	rules := make([]string, 0, len(all.Suppressed))
	total := 0
	for r, n := range all.Suppressed {
		rules = append(rules, r)
		total += n
	}
	sort.Strings(rules)
	for _, r := range rules {
		packs.Count("cloud", "suppression:"+strings.ToLower(r), all.Suppressed[r])
	}
	detail := fmt.Sprintf("%d file(s), %d IAM document(s)", read, all.Documents)
	if total > 0 {
		detail += fmt.Sprintf(", %d suppressed", total)
	}
	if len(o.Accounts) == 0 {
		detail += "; packs.cloud.accounts not set, cross-account trust not judged"
	}
	if len(o.Tenants) == 0 {
		detail += "; packs.cloud.tenants not set, AZR004 not judged"
	}
	return packs.Result{Status: packs.OK, Detail: detail, Findings: all.Findings}
}

func mergeResult(all *iam.Result, r iam.Result) {
	all.Findings = append(all.Findings, r.Findings...)
	all.Documents += r.Documents
	for k, v := range r.Suppressed {
		if all.Suppressed == nil {
			all.Suppressed = map[string]int{}
		}
		all.Suppressed[k] += v
	}
}

func readErrClass(err error) string {
	switch {
	case errors.Is(err, readers.ErrIncomplete):
		return "incomplete"
	case errors.Is(err, readers.ErrWhatIfFailed):
		return "failed what-if"
	case errors.Is(err, readers.ErrTooLarge):
		return "too large"
	case errors.Is(err, readers.ErrTooDeep):
		return "nested too deep"
	case errors.Is(err, readers.ErrUnknownFormat):
		return "unknown format"
	}
	return "invalid"
}
