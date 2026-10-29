"""One place that sets every seed (declared as packs.ml.seed_helper)."""

from __future__ import annotations

import os
import random

import numpy as np


def set_seed(seed: int) -> np.random.Generator:
    """Seed Python, NumPy and (when installed) PyTorch, and return a NumPy Generator.

    Code that needs randomness takes the returned Generator, or passes `seed` as
    `random_state=` to scikit-learn. The legacy global `np.random.seed` is not used
    (ruff NPY002).
    """
    os.environ["PYTHONHASHSEED"] = str(seed)
    random.seed(seed)
    try:  # optional: only when the project uses PyTorch
        import torch  # type: ignore[import-not-found]
    except ImportError:
        pass
    else:
        torch.manual_seed(seed)
        if torch.cuda.is_available():
            torch.cuda.manual_seed_all(seed)
    return np.random.default_rng(seed)
