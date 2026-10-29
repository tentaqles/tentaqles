# ML review checklist (answer-key rows)

Judge each row from the diff, the files it touches and the `tq pack check --pack ml`
results. Cite file:line. A row that does not apply is written `n/a` with the reason.

| # | Row | Question | Pack checks to quote |
|---|---|---|---|
| M1 | Leakage | The L1 to L8 findings of `ml/leakage` judged one by one (L1 fit-before-split, L2 test-in-fit, L3 cv-outside-pipeline, L4 target-in-features, L5 lookahead, L6 timeseries-shuffle, L7 group-leak, L8 torch-eval). Plus leakage the lint can't see: duplicates or the same entity across splits, features computed after the label time, target encoding without folds, test-set statistics used for thresholds or early stopping (a validation set passed as `eval_set=` is fine) | `ml/leakage` |
| M2 | Split strategy | A time, group or stratified split that matches how the model is used (a forecast gets a time split, many rows per entity get a group split, a rare class gets stratification). The holdout stays untouched until the final evaluation and is not reused for model selection | `ml/leakage` L6, L7 |
| M3 | Metrics | The metric matches the decision and the cost of errors; a baseline (prevalence, a dummy model or the current champion) is compared; a confidence interval is given; calibration (Brier score, a reliability curve) is checked when scores are used as probabilities | `ml/model-card` |
| M4 | Reproducibility | Seeds (or the declared `seed_helper`), `random_state` on every estimator and splitter, the data snapshot pinned by sha256 in `data_manifest` or DVC, a lock file that moved with the dependencies, and a recorded training command. The `ml/*` repro results judged | `ml/seeds`, `ml/env-lock`, `ml/data-snapshot`, `ml/run-tracking`, `ml/lock-check`, `ml/dvc-status` |
| M5 | Training/serving skew | The same feature code runs at training and at inference (one `Pipeline` or one feature module, not two copies); no notebook-only transforms; categories unseen at training are handled | — |
| M6 | Personal data as features | Every feature from a column or table classified in the manifest `data:` block has a purpose and legal basis in `docs/privacy.md`; proxies for protected attributes are named; the model card has the slice analysis. Findings name columns, never values | `ml/model-card` (redaction) |
| M7 | Model card | `MODEL_CARD.md` was updated with the change, every number matches the metrics file, the snapshot sha256 matches `data_manifest`, the required headings hold real content, and it carries no personal data or example rows | `ml/model-card` |
| M8 | Serialization and supply chain | Safe formats (`safetensors`, `skops`); `torch.load(..., weights_only=True)`; no `pickle`/`joblib`/`cloudpickle` load of a downloaded or non-repo path; pretrained weights pinned by hash or revision | `ml/unsafe-deserialize` |
| M9 | Promotion path | Promotion only through `tq pack ml promote request` and the human's `apply`; no registry promotion API (`transition_model_version_stage`, `set_registered_model_alias`, an approved model package) in code outside the promotion script; no promotion from CI | `ml/promotion-in-code` |
| M10 | Cost and resources | GPU or cluster hours bounded, early stopping, a bounded hyperparameter search (`n_iter`, a time budget), no unbounded retries | — |
| M11 | PyTorch specifics | `model.eval()` with `torch.no_grad()` or `inference_mode()` in validation and test loops, device determinism noted (`torch.use_deterministic_algorithms`), DataLoader workers seeded (`worker_init_fn`, `generator`) | `ml/leakage` L8, `ml/seeds` |

Severity guide:

- BLOCKER: the reported metric is not trustworthy (confirmed L1 to L4, the test set used for
  fitting or selection), a promotion path outside `tq pack ml promote`, an unsafe load of a
  downloaded file, or a `sensitive`/`children` feature with no privacy record.
- MAJOR: a confirmed `error` finding of any `ml/*` check, a card number that disagrees with the
  metrics file, a split that does not match the use, no baseline, a classified feature with no
  privacy record.
- MINOR: a confirmed `warn` finding, a missing interval or calibration check on a model whose
  scores are not read as probabilities, missing determinism notes.
