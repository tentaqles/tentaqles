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
		// Bounds default to the pack defaults (approval.PackLimits["ml"],
		// DefaultWarn, DefaultFail, DefaultMaxWarnShare); a metric with no
		// minimum has none (0).
		Merge: map[string]packs.MergeRule{
			"models":                packs.Union(),
			"promotion.protected":   packs.Union(),
			"promotion.require":     packs.Union(),
			"promotion.command":     packs.RepoValue(),
			"promotion.valid_for":   packs.Min("7d"),
			"drift.current_command": packs.RepoValue(),
			"drift.warn":            packs.Min("0.1"),
			"drift.fail":            packs.Min("0.25"),
			"drift.max_warn_share":  packs.Min("0.3"),
			"drift.features.*.warn": packs.Min("0.1"),
			"drift.features.*.fail": packs.Min("0.25"),
			"drift.metrics.*.min":   packs.Max("0"),
			"drift.in_dod":          packs.Or(),
			"drift.reference":       packs.TrustedAdd(),
			"drift.current":         packs.TrustedAdd(),
			"drift.metric":          packs.TrustedAdd(),
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
