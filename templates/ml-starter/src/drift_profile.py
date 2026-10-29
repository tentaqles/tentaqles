"""Write a drift profile (schema tq.drift-profile/v1) for `tq pack ml drift`.

Reference profile (from the training snapshot):

    uv run python -m drift_profile --data data/raw/churn.csv --exclude customer_id,churned \
        --source churn-train --out data/profiles/churn-train.json

Current profile (reuses the reference edges, so the bins line up):

    uv run python -m drift_profile --data data/raw/churn-current.csv --exclude customer_id,churned \
        --reference data/profiles/churn-train.json --out .tq/drift/churn-current.json

Profile shape:

    {"schema": "tq.drift-profile/v1", "rows": N, "source": "...", "created_at": "...Z",
     "features": {name: {"type": "numeric", "edges": [e0..ek], "counts": [k ints]}
                       | {"type": "categorical", "counts": {label: n}, "hashed": bool}}}

- Numeric: `edges` are the reference deciles (k+1 sorted values, duplicates dropped);
  value v goes to bin i when edges[i] <= v < edges[i+1]; values below e0 go to the first
  bin and values at or above ek to the last. Missing values are counted in `missing`.
- Categorical: labels are hashed (`h:` + first 16 hex of sha256 of the UTF-8 label) when
  the column is classified (`--classified`, `--hash-all`), when it has more than 50
  labels, or when the reference hashed it. Labels of hashed columns never appear in the
  output.
- `--model models/churn/model.skops` adds `__prediction__`, the score distribution.
- Reads CSV, or parquet with the `parquet` extra (pyarrow).
"""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

import numpy as np
import pandas as pd

SCHEMA = "tq.drift-profile/v1"
MAX_LABELS = 50
BINS = 10
PREDICTION = "__prediction__"


class ProfileError(ValueError):
    pass


def hash_label(label: str) -> str:
    return "h:" + hashlib.sha256(label.encode("utf-8")).hexdigest()[:16]


def read_frame(path: Path) -> pd.DataFrame:
    path = Path(path)
    if path.suffix == ".parquet":
        return pd.read_parquet(path)
    return pd.read_csv(path)


def _edges(values: np.ndarray, bins: int) -> list[float]:
    qs = np.quantile(values, np.linspace(0.0, 1.0, bins + 1))
    edges = sorted({round(float(q), 6) for q in qs})
    return edges if len(edges) >= 2 else [edges[0], edges[0]]


def _numeric(series: pd.Series, edges: list[float] | None, bins: int) -> dict:
    values = pd.to_numeric(series, errors="coerce")
    missing = int(values.isna().sum())
    arr = values.dropna().to_numpy(dtype=float)
    if edges is None:
        if arr.size == 0:
            raise ProfileError(f"{series.name}: no numeric values to build edges from")
        edges = _edges(arr, bins)
    k = len(edges) - 1
    idx = np.clip(np.searchsorted(np.asarray(edges), arr, side="right") - 1, 0, k - 1)
    counts = np.bincount(idx, minlength=k).tolist()
    out: dict = {"type": "numeric", "edges": edges, "counts": [int(c) for c in counts]}
    if missing:
        out["missing"] = missing
    return out


def _categorical(series: pd.Series, hashed: bool) -> dict:
    missing = int(series.isna().sum())
    labels = series.dropna().astype(str)
    if not hashed and labels.nunique() > MAX_LABELS:
        hashed = True
    if hashed:
        labels = labels.map(hash_label)
    counts = {str(k): int(v) for k, v in sorted(labels.value_counts().items())}
    out: dict = {"type": "categorical", "counts": counts, "hashed": hashed}
    if missing:
        out["missing"] = missing
    return out


def _is_numeric(series: pd.Series) -> bool:
    return pd.api.types.is_numeric_dtype(series) and not pd.api.types.is_bool_dtype(series)


def build_profile(frame: pd.DataFrame, reference: dict | None = None, *, source: str = "",
                  created_at: str | None = None, exclude: set[str] | frozenset[str] = frozenset(),
                  classified: set[str] | frozenset[str] = frozenset(), hash_all: bool = False,
                  bins: int = BINS, model_path: Path | None = None) -> dict:
    if reference is not None and reference.get("schema", SCHEMA) != SCHEMA:
        raise ProfileError(f"reference schema {reference.get('schema')!r} is not {SCHEMA}")
    ref_features = (reference or {}).get("features", {})
    if model_path is not None:
        from ml_starter.serve import load_model, predict_scores

        frame = frame.assign(**{PREDICTION: predict_scores(load_model(model_path), frame)})
    names = list(ref_features) if reference is not None else [c for c in frame.columns if c not in exclude]
    if reference is not None and model_path is not None and PREDICTION not in names:
        names.append(PREDICTION)
    feats: dict[str, dict] = {}
    for name in names:
        if name not in frame.columns:
            raise ProfileError(f"feature {name!r} is in the reference but not in the data")
        ref = ref_features.get(name)
        kind = ref["type"] if ref else ("numeric" if _is_numeric(frame[name]) else "categorical")
        if kind == "numeric":
            feats[name] = _numeric(frame[name], ref["edges"] if ref else None, bins)
        else:
            hashed = hash_all or name in classified or bool(ref and ref.get("hashed"))
            feats[name] = _categorical(frame[name], hashed)
    return {
        "schema": SCHEMA,
        "rows": int(len(frame)),
        "source": source,
        "created_at": created_at or datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "features": feats,
    }


def _csv_set(value: str) -> set[str]:
    return {v.strip() for v in value.split(",") if v.strip()}


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(prog="drift_profile", description="Write a tq.drift-profile/v1 profile.")
    ap.add_argument("--data", type=Path, required=True, help="CSV or parquet file to profile")
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--reference", type=Path, help="reference profile whose edges and hashing are reused")
    ap.add_argument("--exclude", default="", help="comma-separated columns to leave out (id, target)")
    ap.add_argument("--classified", default="", help="comma-separated classified columns (labels hashed)")
    ap.add_argument("--hash-all", action="store_true", help="hash every categorical label (classified file)")
    ap.add_argument("--model", type=Path, help="skops model file: adds __prediction__")
    ap.add_argument("--source", default="", help="snapshot name or a short description, no paths with secrets")
    ap.add_argument("--created-at", default=None, help="fixed timestamp for a reproducible reference")
    ap.add_argument("--bins", type=int, default=BINS)
    a = ap.parse_args(argv)
    try:
        reference = json.loads(a.reference.read_text(encoding="utf-8")) if a.reference else None
        prof = build_profile(read_frame(a.data), reference, source=a.source or a.data.stem,
                             created_at=a.created_at, exclude=_csv_set(a.exclude),
                             classified=_csv_set(a.classified), hash_all=a.hash_all, bins=a.bins,
                             model_path=a.model)
    except (ProfileError, OSError, ValueError, KeyError) as exc:
        print(f"drift_profile: {exc}", file=sys.stderr)
        return 2
    a.out.parent.mkdir(parents=True, exist_ok=True)
    a.out.write_text(json.dumps(prof, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(f"wrote {a.out}: {prof['rows']} rows, {len(prof['features'])} features")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
