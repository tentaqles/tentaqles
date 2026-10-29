# Model card: churn

## Model details

- **Name:** churn
- **Version:** 7
- **Registry URI:** models:/churn/7
- **Owner role:** ML lead
- **Date:** 2026-10-01
- **Licence:** proprietary (acme)

## Intended use / Out of scope

- **Intended use:** the retention team ranks accounts for a renewal call.
- **Out of scope:** pricing decisions, credit decisions.

## Training data

- **Snapshot:** churn-2026-09
- **Snapshot sha256:** 1111111111111111111111111111111111111111111111111111111111111111
- **Date range:** 2025-09-01 to 2026-08-31
- **Data classes:** internal
- **Sampling:** every active account, one row per account and month.

## Evaluation data

- **Holdout:** time split, the last two months.
- **Size:** 12000 rows
- **Kept untouched by:** the split runs before any fit; tuning uses folds of the training months.

## Metrics

| Metric | Value | Confidence interval | Baseline |
|---|---|---|---|

## Fairness and slices

- **Slices checked:** tenure band, plan type.
- **Gaps found:** NEEDS-DECISION (plan type gap of 0.04 auc)
- **Mitigations:** none yet.

## Limitations and risks

- **Failure modes:** new plans with no history.
- **Drift sensitivity:** tenure_days.
- **Feedback loops:** called accounts churn less, which shifts later labels.

## Reproducibility

- **Git commit:** recorded by the training run.
- **Lock file sha256:** recorded by the training run.
- **Seeds:** 42 through src/common/seed.py.
- **Training command:** uv run python -m train.churn
- **Environment:** Python 3.12, CPU.

## Monitoring and drift

- **Reference profile:** data/profiles/churn-train.json
- **Monitored features:** tenure_days, plan
- **Thresholds:** psi, warn 0.1, fail 0.25
- **Alert route:** ML lead

## Promotion history

| Date | Version | Target | Decision | Actor |
|---|---|---|---|---|
