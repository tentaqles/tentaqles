"""Pluggable embeddings.

Pick ONE model and dimension per project and never mix them in a table. The
`chunks.embedding` column is `halfvec(N)`, and pgvector's HNSW index caps at
2,000 dims for `vector` and 4,000 dims for `halfvec`. `MAX_HNSW_HALFVEC_DIMS`
enforces that limit here, so a misconfigured model fails at startup, not at
index build time.
"""

from __future__ import annotations

import hashlib
import json
import math
import os
import re
import urllib.request
from dataclasses import dataclass, field
from typing import Protocol

MAX_HNSW_VECTOR_DIMS = 2000
MAX_HNSW_HALFVEC_DIMS = 4000


class Embedder(Protocol):
    dim: int

    def embed(self, texts: list[str]) -> list[list[float]]: ...


def check_dim(dim: int) -> int:
    if not 0 < dim <= MAX_HNSW_HALFVEC_DIMS:
        raise ValueError(
            f"embedding dim {dim} is over the HNSW halfvec limit ({MAX_HNSW_HALFVEC_DIMS}); "
            "request fewer dimensions from the model (e.g. `dimensions=1536`)"
        )
    return dim


def _l2(v: list[float]) -> list[float]:
    n = math.sqrt(sum(x * x for x in v)) or 1.0
    return [x / n for x in v]


@dataclass
class HashEmbedder:
    """Deterministic bag-of-words feature hashing. Offline demo and tests only."""

    dim: int = 256

    def __post_init__(self) -> None:
        check_dim(self.dim)

    def embed(self, texts: list[str]) -> list[list[float]]:
        out = []
        for text in texts:
            v = [0.0] * self.dim
            for tok in re.findall(r"[a-z0-9]+", text.lower()):
                h = int.from_bytes(hashlib.blake2b(tok.encode(), digest_size=8).digest(), "big")
                v[h % self.dim] += 1.0 if (h >> 63) & 1 else -1.0
            out.append(_l2(v))
        return out


@dataclass
class OpenAICompatibleEmbedder:
    """Any `/embeddings` endpoint that speaks the OpenAI format (OpenAI, Azure, LM Studio, vLLM...).

    The key is read from the `api_key_env` variable at call time and is never stored in logs.
    """

    model: str = "text-embedding-3-small"
    dim: int = 1536
    base_url: str = "https://api.openai.com/v1"
    api_key_env: str = "OPENAI_API_KEY"
    batch_size: int = 128
    timeout: float = 30.0
    _post: object = field(default=None, repr=False)

    def __post_init__(self) -> None:
        check_dim(self.dim)

    def _request(self, texts: list[str]) -> list[list[float]]:
        key = os.environ.get(self.api_key_env, "")
        if not key:
            raise RuntimeError(f"{self.api_key_env} is not set (run with `tq dotenv run -- ...`)")
        body = json.dumps({"model": self.model, "input": texts, "dimensions": self.dim}).encode()
        req = urllib.request.Request(
            self.base_url.rstrip("/") + "/embeddings",
            data=body,
            headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=self.timeout) as resp:
            data = json.loads(resp.read())
        rows = sorted(data["data"], key=lambda r: r["index"])
        return [r["embedding"] for r in rows]

    def embed(self, texts: list[str]) -> list[list[float]]:
        out: list[list[float]] = []
        post = self._post or self._request
        for i in range(0, len(texts), self.batch_size):
            vecs = post(texts[i : i + self.batch_size])  # type: ignore[operator]
            for v in vecs:
                if len(v) != self.dim:
                    raise ValueError(f"model returned {len(v)} dims, table expects {self.dim}")
            out.extend(vecs)
        return out
