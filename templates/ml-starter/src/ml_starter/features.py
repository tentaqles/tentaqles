"""Feature definitions shared by training and serving (one copy: no training/serving skew)."""

from __future__ import annotations

import pandas as pd
from sklearn.compose import ColumnTransformer
from sklearn.preprocessing import OneHotEncoder, StandardScaler

TARGET = "churned"
ID = "customer_id"
NUMERIC = ["tenure_days", "monthly_spend", "support_tickets"]
CATEGORICAL = ["plan", "region"]
FEATURES = NUMERIC + CATEGORICAL


def features(frame: pd.DataFrame) -> pd.DataFrame:
    """The model inputs: the target and the id are never features."""
    return frame.loc[:, FEATURES]


def target(frame: pd.DataFrame) -> pd.Series:
    return frame[TARGET]


def build_preprocessor() -> ColumnTransformer:
    """Unfitted. It is only ever fitted inside the Pipeline, on the training split."""
    return ColumnTransformer([
        ("num", StandardScaler(), NUMERIC),
        ("cat", OneHotEncoder(handle_unknown="ignore"), CATEGORICAL),
    ])
