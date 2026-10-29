"""Load the saved model safely and score frames with the same feature code as training."""

from __future__ import annotations

from pathlib import Path

import numpy as np
import pandas as pd
import skops.io as sio

from ml_starter.features import features

# Types skops does not trust by default that this model's file may contain. The current
# pipeline needs none. Add a type only after reviewing it, when the pipeline changes;
# never pass get_untrusted_types() straight to load().
ALLOWED_TYPES: frozenset[str] = frozenset()


class UntrustedModel(RuntimeError):
    """The model file holds a type nobody reviewed."""


def load_model(path: Path):
    path = Path(path)
    untrusted = sio.get_untrusted_types(file=path)
    unknown = sorted(set(untrusted) - ALLOWED_TYPES)
    if unknown:
        raise UntrustedModel(f"{path.name}: untrusted types {unknown}; review them before adding to ALLOWED_TYPES")
    return sio.load(path, trusted=list(untrusted))


def predict_scores(model, frame: pd.DataFrame) -> np.ndarray:
    return model.predict_proba(features(frame))[:, 1]
