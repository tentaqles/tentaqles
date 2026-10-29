package ml

// Registration of the promotion gate, model cards and drift (spec C
// §4.4-§4.6): config keys models, promotion and drift; the checks
// ml/model-card, ml/drift and ml/promotion-in-code; the guard
// tq/model-promote; the triage flag model_promotion; the doctor probes; and
// the commands tq pack ml promote and tq pack ml drift.

import (
	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/policy"
	"gopkg.in/yaml.v3"
)

func init() {
	packs.RegisterPack(packs.Pack{
		Name: "ml",
		ConfigKeys: map[string]func(yaml.Node) error{
			"models":    validateModels,
			"promotion": validatePromotion,
			"drift":     validateDrift,
		},
		Merge: map[string]packs.MergeKind{
			"models":                packs.MergeUnion,
			"promotion.protected":   packs.MergeUnion,
			"promotion.require":     packs.MergeUnion,
			"promotion.command":     packs.MergeRepo,
			"promotion.valid_for":   packs.MergeMin,
			"drift.current_command": packs.MergeRepo,
			"drift.warn":            packs.MergeMin,
			"drift.fail":            packs.MergeMin,
			"drift.max_warn_share":  packs.MergeMin,
			"drift.features.*.warn": packs.MergeMin,
			"drift.features.*.fail": packs.MergeMin,
			"drift.metrics.*.min":   packs.MergeMax,
			"drift.in_dod":          packs.MergeOr,
		},
		Flags: []packs.Flag{
			{Name: "model_promotion", Reviewers: []string{"ml-reviewer"}, Raise: raiseModelPromotion},
		},
		Doctor: promoteDoctor,
		Commands: func() []*cobra.Command {
			return []*cobra.Command{newPromoteCmd(), newDriftCmd()}
		},
	})
	packs.Register(packs.Check{ID: "ml/model-card", Pack: "ml", Kind: packs.Builtin, Applies: modelCardApplies, Run: runModelCard})
	packs.Register(packs.Check{ID: "ml/drift", Pack: "ml", Kind: packs.Builtin, Applies: driftApplies, Run: runDriftCheck})
	packs.Register(packs.Check{ID: "ml/promotion-in-code", Pack: "ml", Kind: packs.Builtin, Applies: promotionInCodeApplies, Run: runPromotionInCode})
	packs.RegisterGuard(packs.Guard{ID: "tq/model-promote", Pack: "ml", Events: []string{"Bash", "PowerShell", `mcp__.*`},
		Check: func(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
			return checkModelPromote(call, g)
		}})
}

// raiseModelPromotion: the promotion script, a model card, or tq-packs.yaml
// (which may carry packs.ml.promotion) changed.
func raiseModelPromotion(in *packs.Input) bool {
	st := loadIn(in)
	globs := append(promotionScripts(st), "tq-packs.yaml")
	for _, m := range st.Models {
		globs = append(globs, m.Card)
	}
	for _, p := range in.ChangedPaths() {
		for _, g := range globs {
			if p == g {
				return true
			}
		}
	}
	return false
}
