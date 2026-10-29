"""Holdout metrics: AUC with a bootstrap interval, Brier score (calibration), a baseline, slices."""

from __future__ import annotations

import numpy as np
import pandas as pd
from sklearn.metrics import accuracy_score, brier_score_loss, roc_auc_score

BOOTSTRAP = 200


def _r(x: float) -> float:
    return round(float(x), 4)


def evaluate(scores: np.ndarray, y_test: pd.Series, prevalence: float, slices: pd.Series,
             rng: np.random.Generator) -> dict:
    """`prevalence` is the training-split churn rate: the baseline predicts it for everyone."""
    y = y_test.to_numpy()
    auc = roc_auc_score(y, scores)
    boots = []
    n = len(y)
    for _ in range(BOOTSTRAP):
        idx = rng.integers(0, n, n)
        if len(np.unique(y[idx])) == 2:
            boots.append(roc_auc_score(y[idx], scores[idx]))
    lo, hi = np.quantile(boots, [0.025, 0.975])
    baseline = np.full(n, prevalence)
    out = {
        "auc": _r(auc),
        "auc_ci_low": _r(lo),
        "auc_ci_high": _r(hi),
        "brier": _r(brier_score_loss(y, scores)),
        "accuracy": _r(accuracy_score(y, scores >= 0.5)),
        "baseline_auc": 0.5,
        "baseline_brier": _r(brier_score_loss(y, baseline)),
        "rows_test": int(n),
        "slices": {},
    }
    for value in sorted(slices.unique()):
        mask = (slices == value).to_numpy()
        if len(np.unique(y[mask])) == 2:
            out["slices"][f"{slices.name}={value}"] = {"auc": _r(roc_auc_score(y[mask], scores[mask])),
                                                        "rows": int(mask.sum())}
    return out
