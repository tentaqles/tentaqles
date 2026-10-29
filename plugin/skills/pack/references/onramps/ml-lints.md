# ml: lints (leakage and reproducibility)

Checks: `ml/leakage`, `ml/seeds`, `ml/env-lock`, `ml/data-snapshot`, `ml/run-tracking`,
`ml/unsafe-deserialize`, `ml/notebook-outputs` (built-in, required), and `ml/ruff`,
`ml/lock-check` (uv), `ml/lock-check-poetry`, `ml/dvc-status` (external; required only
when the tool is in `packs.ml.tools`). Reference: `docs/packs/ml-lints.md` in the tq repo.

## kickoff

Only when the stack trains models: the repo has scikit-learn, PyTorch, XGBoost, LightGBM,
Keras or TensorFlow imports, a `dvc.yaml`, or training notebooks. Otherwise skip this
section. Never assume the ml pack is enabled.

- Propose these keys in the `packs.ml` block (headless: in
  `docs/privacy-manifest-proposal.yaml`, as for the other proposals). The human adds them to
  the manifest and runs `tq allow`:

```yaml
packs:
  ml:
    enabled: true
    tools: [ruff, uv]              # add dvc or poetry only when the project uses them
    paths:
      train: ["src/train/**", "notebooks/**"]
      features: ["src/features/**"]
      eval: ["src/eval/**"]
    seed_helper: src/common/seed.py:set_seed
    data_manifest: data/snapshots.yaml   # name -> {uri, sha256}, or dvc.lock
```

- Create the seed helper the key names (it seeds `random`, NumPy and torch when present).
- Create `data/snapshots.yaml` with one entry per training input: `uri` and `sha256`.
  Never put example rows in it.
- Commit a lock file (`uv.lock`, `poetry.lock`, `pixi.lock` or `conda-lock.yml`). Pin
  `requirements*.txt` with `==` and hashes.

## feature

When the slice touches training, feature or evaluation code:

- The spec slice gains **Data snapshot** (the `data_manifest` names it reads, each with its
  sha256) and **Evaluation plan** (metric, baseline, and the split: time, group or
  stratified). The split must match how the model is used. A grouped entity, such as a
  customer id, needs a `Group*` splitter.
- Preprocessing goes inside a `Pipeline`, fitted on the training split only.
- Acceptance checks name the lint results: `pack:ml/leakage` and `pack:ml/seeds` are ok,
  and `pack:ml/data-snapshot` and `pack:ml/env-lock` are ok when the slice adds data or
  dependencies.
- The change raises the `ml` triage flag. Take the crew from `tq pack crew --flags ...`.

## fix

- A leakage or reproducibility fix gets a lint fixture for the positive case: a minimal
  synthetic training file that the rule flags, with names like `tester` and `acme` only,
  plus the test that now passes.
- A finding the team accepts on purpose is suppressed on its line, never by editing the
  check: `# tq:allow L2: <reason>` (or `ml/seeds`, `random-state`, and so on). A
  suppression without a reason does not count. Suppress only when the human agrees; quote
  the reason in the fix spec.

## dod

Rows for the item table:

| Item | Who fixes it |
|---|---|
| `pack:ml/leakage` | the implementer: fit preprocessing on the training split or inside a Pipeline (L1, L3), never fit on test or holdout data (L2), drop the target from the features (L4). The L5–L8 warnings are judged by `ml-reviewer` |
| `pack:ml/seeds` | the implementer: call the seed helper or set the seeds, and pass `random_state` |
| `pack:ml/env-lock` | the implementer: re-lock and commit the lock, and pin `requirements*.txt` |
| `pack:ml/data-snapshot` | the implementer adds the input with its sha256 to `data_manifest` (or tracks it with DVC), and bounds table and SQL reads with an as-of version |
| `pack:ml/unsafe-deserialize` | the implementer: switch to `safetensors` or `skops`, or `torch.load(..., weights_only=True)`, or load a repo artifact |
| `pack:ml/notebook-outputs` | the implementer clears the outputs. An output that matched a detector must not be committed |
| `pack:ml/ruff`, `pack:ml/lock-check`, `pack:ml/lock-check-poetry`, `pack:ml/dvc-status` | the implementer. A declared but missing tool: install it, or the human removes it from `tools` |

`pack:ml/run-tracking` only warns; mention it in the review, it never blocks.

## ship

- The PR body quotes the `ml/*` lint lines of the pack results summary, and lists every
  `tq:allow` suppression the diff adds, with its reason.
