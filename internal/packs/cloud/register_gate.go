package cloud

// The apply and destroy gate's registration (spec D §9, workstream D-CG):
// the config keys environments, default_environment and require_plan, their
// tq-packs.yaml merge rules, the tq pack cloud plan/show/approve/prune
// commands and the cloud/plan-fresh check. The guards tq/iac-apply-env and
// tq/iac-identity-drift are registered from internal/cmd/claudehook_iac.go;
// their bodies live in internal/packs/cloud/gate.

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/plans"
	"github.com/tentaqles/tentaqles/internal/packs/envgate"
	"gopkg.in/yaml.v3"
)

const planFreshID = "cloud/plan-fresh"

func init() {
	merge := envgate.Cloud.MergeRules()
	merge["require_plan"] = packs.Or()
	packs.RegisterPack(packs.Pack{
		Name: "cloud",
		ConfigKeys: map[string]func(yaml.Node) error{
			"environments":        envgate.Cloud.Validate,
			"default_environment": validateName,
			"require_plan":        validateBool,
		},
		Merge:    merge,
		Commands: func() []*cobra.Command { return commands() },
	})
	packs.Register(packs.Check{ID: planFreshID, Pack: "cloud", Kind: packs.Builtin,
		Applies: func(in *packs.Input) bool { return requirePlan(in) && len(changedRoots(in)) > 0 },
		Run:     runPlanFresh,
	})
}

func validateName(n yaml.Node) error {
	if n.Kind != yaml.ScalarNode || strings.TrimSpace(n.Value) == "" || n.Tag == "!!null" {
		return fmt.Errorf("line %d: default_environment must name an environment", n.Line)
	}
	return nil
}

func validateBool(n yaml.Node) error {
	var b bool
	if n.Kind != yaml.ScalarNode || n.Decode(&b) != nil {
		return fmt.Errorf("line %d: require_plan must be true or false", n.Line)
	}
	return nil
}

func requirePlan(in *packs.Input) bool {
	return strings.EqualFold(in.Settings().String("require_plan"), "true")
}

// iacGlobs are IaC files when packs.cloud.iac names no path.
var iacGlobs = []string{"*.tf", "*.tfvars", "*.bicep", "*.bicepparam", "Pulumi*.yaml", "cdk.json"}

// iacPaths reads packs.cloud.iac[].path (the key D-CP validates).
func iacPaths(in *packs.Input) []string {
	n := in.Settings().Node("iac")
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	var out []string
	for _, it := range n.Content {
		if it.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(it.Content); i += 2 {
			if it.Content[i].Value == "path" && it.Content[i+1].Value != "" {
				out = append(out, path.Clean(strings.TrimPrefix(strings.ReplaceAll(it.Content[i+1].Value, `\`, "/"), "./")))
			}
		}
	}
	return out
}

// changedRoots are the IaC roots with a changed path: the configured
// packs.cloud.iac paths, or the folders of changed IaC files.
func changedRoots(in *packs.Input) []string {
	roots := iacPaths(in)
	set := map[string]bool{}
	for _, p := range in.ChangedPaths() {
		if len(roots) > 0 {
			for _, r := range roots {
				if r == "." || p == r || strings.HasPrefix(p, r+"/") {
					set[r] = true
				}
			}
			continue
		}
		for _, g := range iacGlobs {
			if packs.MatchGlob(g, p) {
				set[path.Dir(p)] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

func related(root, dir string) bool {
	return root == "." || dir == root || strings.HasPrefix(dir, root+"/") || strings.HasPrefix(root, dir+"/") || dir == "."
}

// runPlanFresh: with require_plan, every changed IaC root needs a recorded
// plan whose inputs hash matches the current tree (§5, §9.4).
func runPlanFresh(ctx context.Context, in *packs.Input) packs.Result {
	res := packs.Result{Status: packs.OK, Findings: []packs.Finding{}}
	sd := in.StateDir()
	if sd == "" {
		res.Status, res.Detail = packs.Fail, "no workspace: plan records cannot be read"
		return res
	}
	recs, err := plans.All(sd)
	if err != nil {
		res.Status, res.Detail = packs.Fail, "plan records unreadable: "+err.Error()
		return res
	}
	roots := changedRoots(in)
	fresh := 0
	for _, root := range roots {
		ok := false
		tried := 0
		for _, r := range recs {
			if !related(root, r.Dir) || r.InputsHash == "" {
				continue
			}
			if tried++; tried > 10 {
				break
			}
			inputs := r.Inputs
			if len(inputs) == 0 {
				inputs = append([]string{r.Dir}, r.VarFiles...)
			}
			h, err := plans.InputsHash(ctx, in.Root, inputs)
			if err == nil && h == r.InputsHash {
				ok = true
				break
			}
		}
		if ok {
			fresh++
			continue
		}
		res.Findings = append(res.Findings, packs.Finding{File: root, Level: packs.LevelError, Rule: "plan-fresh",
			Message: "no recorded plan matches the current IaC tree; record one with `tq pack cloud plan --env <env> -- <plan command>`"})
	}
	if len(res.Findings) > 0 {
		res.Status = packs.Fail
		res.Detail = fmt.Sprintf("%d of %d changed IaC roots have no fresh plan", len(res.Findings), len(roots))
		return res
	}
	res.Detail = fmt.Sprintf("%d changed IaC roots, each with a fresh plan", fresh)
	return res
}
