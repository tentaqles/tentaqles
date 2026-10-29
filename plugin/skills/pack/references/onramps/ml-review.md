# ml: review

The `ml-reviewer` agent and the `ml-starter` template. The lint rules are in
`ml-lints.md` and the promotion gate, model card and drift check in `ml-promotion.md`.

## build

- A checkpoint whose `changed_files` touch training, feature or eval code
  (`packs.ml.paths`, or a `.py`/`.ipynb` that calls `.fit(`, `optimizer.step(` or
  `trainer.train(`) raises `ml`; one that touches the promotion script,
  `packs.ml.promotion` or a model card raises `model_promotion`. Step d.3 adds what
  `tq pack crew --flags <the flags triage printed>` prints, which is `ml-reviewer` for
  both. Run it through the `ml-review` skill so the reviewer gets the
  `tq pack check --pack ml --json` results with the bundle.
- The checkpoint plan never contains a promotion `apply`. A checkpoint may end at
  `tq pack ml promote request` and its decision id; the human applies.

## feature

- The spec slice gains three fields for ML work:
  - **Models touched:** each `packs.ml.models[].name` the change feeds, with its card path.
  - **Data snapshot:** the `data_manifest` entry (name and sha256) the training reads, or
    the new entry the feature adds.
  - **Evaluation plan:** the metric and why it fits the decision, the baseline, the split
    (time, group or stratified) and the holdout rule.
- Name the flags the work will raise (`ml`, and `model_promotion` when the card or the
  promotion path changes) and take the expected crew from `tq pack crew`; it adds
  `ml-reviewer`.
- Acceptance checks are binary and name a test: "the pipeline test proves the scaler is
  fitted on the training split only", "the metrics file has `auc_ci`", never "the model
  is good".

## fix

- A leakage or reproducibility bug gets its regression test at the lowest layer: a
  pytest over a small synthetic frame (people named `tester`, company `acme`), seeded.
- A model-quality fix records the before and after metrics (same snapshot, same seed) in
  the fix spec, and the card is updated from the new metrics file.
- The fix raises `ml`, so `ml-reviewer` reviews it like any other ML change.

## kickoff

Only when the stack trains or serves a model in Python (scikit-learn, PyTorch, an
MLflow-style registry) do you propose the ML pack. Never assume it is enabled.

- Propose the `packs.ml` block for the human (headless: in
  `docs/privacy-manifest-proposal.yaml`, as today): `paths`, `seed_helper`,
  `data_manifest`, `models`, `promotion` and `drift`. The human puts it in the manifest
  and runs `tq allow`; you never edit the manifest.
- For a new repo, offer the `ml-starter` template in tq's `templates/ml-starter/`: a
  scikit-learn `Pipeline`, a seed helper, `uv.lock`, `data/snapshots.yaml`, a filled
  `MODEL_CARD.md`, `drift_profile.py` and `scripts/promote.py` for MLflow aliases. Copy
  it, rename the model, and keep its `tq-packs.yaml` as the starting repo layer.
- Add an ADR for the model registry and the promotion targets (`promotion.protected`).

## ship

- When `ml` or `model_promotion` was raised, the PR body states the `ml-reviewer` result
  that the gate recorded for the current code. Without a recorded PASS (or the human's
  judge), it says the review is pending, never "reviewed".
- Quote the `ml/*` results summary and link the model card diff.
