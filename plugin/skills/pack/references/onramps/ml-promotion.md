# ml: promotion, model cards and drift

## kickoff

Only when the stack trains or serves a model (scikit-learn, PyTorch, an MLflow-style
registry, SageMaker, Vertex AI, Databricks or Azure ML): propose these `packs.ml`
keys to the human, in `docs/privacy-manifest-proposal.yaml` when headless. Never
assume the pack is enabled and never edit the manifest.

```yaml
packs:
  ml:
    data_manifest: data/snapshots.yaml        # name -> uri + sha256
    models:
      - { name: <model>, card: models/<model>/MODEL_CARD.md, metrics: reports/<model>/metrics.json }
    promotion:
      protected: [production, champion]       # stages or aliases that need a human approval
      command: "uv run python scripts/promote.py {model} {version} {target}"
      valid_for: 7d
      require: [model_card, drift, repro]
    drift:
      reference: data/profiles/<model>-train.json
      current: .tq/drift/<model>-current.json
      metric: psi
      warn: 0.1
      fail: 0.25
      max_warn_share: 0.3
      metrics: { <metric>: { min: <floor> } }
```

- Write each model's card from `templates/process/MODEL_CARD.md` (skill `model-card`).
- Plan the drift reference: which snapshot builds it, and which script produces the
  current profile (aggregates only, hashed labels for classified columns).
- Add `.tq/drift/` to `.gitignore`.
- Write an ADR for the registry (which one, which stages or aliases are protected,
  and who may answer a promotion approval).

## feature

- The spec slice gains **Models touched** (each `packs.ml.models` name), **Data
  snapshot** (name and sha256 from the data manifest) and **Evaluation plan** (metric,
  baseline, split).
- A change to a model's code, a model card or the promotion script raises the triage
  flags `ml` and `model_promotion`; take the reviewers from `tq pack crew`.
- An acceptance check for model work names the card update (`ml/model-card` passes)
  and, when thresholds matter, `tq pack ml drift <model>` not failing.
- When the feature ends in a promotion, the last checkpoint ends at
  `tq pack ml promote request` (skill `ml-promote`), never at apply. The human
  answers and applies.

## fix

- A model-quality fix records the before and after metrics in the fix spec, and
  updates the model card's Metrics table in the same change (`card-not-updated`
  otherwise).
- A fix for a promotion API call in code (`ml/promotion-in-code`) moves the call into
  the declared promotion script; the regression test covers the script, not a direct
  registry call.

## dod

| Item | Who fixes it |
|---|---|
| `pack:ml/model-card` | the agent: fill the card (skill `model-card`); `card-snapshot-mismatch` that the data manifest cannot explain goes to the human |
| `pack:ml/drift` (only with `drift.in_dod: true`) | the agent refreshes the profiles (skill `drift-check`); a real drift fail goes to the human (retrain or a threshold review) |
| `pack:ml/promotion-in-code` | the agent: move the registry call into the promotion script, or route it through `tq pack ml promote` |

## ship

- The PR body links the model card diff and names any pending promotion decision id
  from `tq pack ml promote status`.
- Never write "promoted" unless `tq pack ml promote status` shows the request as
  `used`. A request that is `pending` or `approved` is "awaiting the human's apply".
