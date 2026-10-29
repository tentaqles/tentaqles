# Model card: <model name>

<!--
One card per model, at the path in packs.ml.models[].card. tq's ml/model-card
check reads it: every "##" heading below is required, a section holding only
TODO fails, the Metrics table needs at least one row, the Training data
snapshot sha256 must match packs.ml.data_manifest, and the text must pass the
redaction detectors. Never paste rows of personal data, user ids, e-mails or
labels of classified columns into this card. Fairness results come from an
analysis that was run, never from a guess: write NEEDS-DECISION when one is
still open. tq pack ml promote apply appends rows to Promotion history.
This template is part of the tentaqles plugin (MIT licence).
-->

## Model details

- **Name:** TODO
- **Version:** TODO (the registry version this card describes)
- **Registry URI:** TODO (no credentials, no tokens in the URI)
- **Owner role:** TODO (a role, for example "ML lead", not a person)
- **Date:** TODO (YYYY-MM-DD)
- **Licence:** TODO

## Intended use / Out of scope

- **Intended use:** TODO (who uses the predictions, for which decision)
- **Out of scope:** TODO (uses this model must not serve, known misuse)

## Training data

- **Snapshot:** TODO (the name in the data manifest)
- **Snapshot sha256:** TODO (64 hex characters, as in the data manifest)
- **Date range:** TODO
- **Data classes:** TODO (manifest class names only, for example pii, financial)
- **Sampling:** TODO (filters, balancing, de-duplication)

## Evaluation data

- **Holdout:** TODO (time split or group split, and why it matches how the model is used)
- **Size:** TODO
- **Kept untouched by:** TODO (how the holdout stayed out of training and tuning)

## Metrics

| Metric | Value | Confidence interval | Baseline |
|---|---|---|---|
| TODO | TODO | TODO | TODO |

<!-- Add per-slice rows ("auc (tenure < 90 days)") for the slices that matter. -->

## Fairness and slices

- **Slices checked:** TODO
- **Gaps found:** TODO (or NEEDS-DECISION)
- **Mitigations:** TODO

## Limitations and risks

- **Failure modes:** TODO
- **Drift sensitivity:** TODO
- **Feedback loops:** TODO

## Reproducibility

- **Git commit:** TODO
- **Lock file sha256:** TODO
- **Seeds:** TODO
- **Training command:** TODO
- **Environment:** TODO (Python version, accelerator)

## Monitoring and drift

- **Reference profile:** TODO (packs.ml.drift.reference)
- **Monitored features:** TODO
- **Thresholds:** TODO (from packs.ml.drift: metric, warn, fail)
- **Alert route:** TODO (a role or a channel name)

## Promotion history

| Date | Version | Target | Decision | Actor |
|---|---|---|---|---|
