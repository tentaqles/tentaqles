"""Cross-workspace pattern detection — cluster decisions from all registered workspaces."""

from __future__ import annotations

import json
import sqlite3
import uuid
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path
from typing import TYPE_CHECKING

import numpy as np

from tentaqles.config import data_dir

if TYPE_CHECKING:
    from tentaqles.embeddings.service import EmbeddingService

PATTERNS_PATH_RELATIVE = "metagraph/patterns.json"


class CrossWorkspacePatternDetector:
    """Detect recurring decision patterns across registered client workspaces.

    Usage::

        from tentaqles.metagraph.config import list_workspaces
        detector = CrossWorkspacePatternDetector()
        result = detector.run(list_workspaces())
    """

    def __init__(self, emb_service: "EmbeddingService | None" = None) -> None:
        if emb_service is None:
            from tentaqles.embeddings.service import EmbeddingService
            emb_service = EmbeddingService()
        self._emb_service = emb_service
        self._data_dir = data_dir()

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def run(
        self,
        workspace_registry: dict[str, dict],
        min_cluster_size: int = 3,
        min_workspaces: int = 2,
        n_clusters: int | None = None,
    ) -> dict:
        """Detect cross-workspace patterns and write patterns.json.

        Args:
            workspace_registry: Mapping from workspace_id -> workspace config dict,
                as returned by ``metagraph.config.list_workspaces()``.
            min_cluster_size: Minimum number of decisions in a cluster to emit a pattern.
            min_workspaces: Minimum distinct workspaces a cluster must span.
            n_clusters: Override the automatic cluster count heuristic.

        Returns:
            ``{"patterns_found": int, "output_path": str}``
        """
        decisions = self._load_all_decisions(workspace_registry)

        if not decisions:
            result = self._write_patterns([])
            return {"patterns_found": 0, "output_path": result}

        # Build embedding matrix — prefer stored embeddings, fall back to live embed
        embeddings = self._build_embedding_matrix(decisions)

        n_decisions = len(decisions)
        k = n_clusters if n_clusters is not None else max(2, min(n_decisions // 10, 20))
        k = min(k, n_decisions)  # can't have more clusters than decisions

        assignments = self._cluster(embeddings, k)

        # Group decisions by cluster
        clusters: dict[int, list[dict]] = {}
        for i, label in enumerate(assignments):
            clusters.setdefault(int(label), []).append(decisions[i])

        patterns = []
        for cluster_id, members in clusters.items():
            # Filter: size threshold
            if len(members) < min_cluster_size:
                continue
            # Filter: workspace diversity threshold
            ws_set = {m["workspace_id"] for m in members}
            if len(ws_set) < min_workspaces:
                continue

            cluster_label = self._label_cluster(members)
            cluster_embeddings = np.stack([m["_vec"] for m in members])
            centroid = cluster_embeddings.mean(axis=0)
            # similarity_score = mean cosine similarity to centroid
            norms = np.linalg.norm(cluster_embeddings, axis=1, keepdims=True) + 1e-10
            cent_norm = np.linalg.norm(centroid) + 1e-10
            sims = (cluster_embeddings / norms) @ (centroid / cent_norm)
            similarity_score = float(sims.mean())

            # Representative decision = closest to centroid
            closest_idx = int(np.argmax(sims))
            rep_decision = members[closest_idx]["chosen"]

            patterns.append({
                "id": f"pat_{uuid.uuid4().hex[:8]}",
                "label": cluster_label,
                "workspaces": sorted(ws_set),
                "decision_count": len(members),
                "representative_decision": rep_decision,
                "similarity_score": round(similarity_score, 4),
            })

        output_path = self._write_patterns(patterns)
        return {"patterns_found": len(patterns), "output_path": output_path}

    def load_patterns(self) -> list[dict]:
        """Load patterns from patterns.json. Returns [] if the file does not exist."""
        path = self._data_dir / PATTERNS_PATH_RELATIVE
        if not path.exists():
            return []
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
            return data.get("patterns", [])
        except (json.JSONDecodeError, OSError):
            return []

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _load_all_decisions(self, workspace_registry: dict) -> list[dict]:
        """Load all active decisions from each workspace's memory.db (read-only).

        Returns a list of dicts with keys:
            workspace_id, decision_id, chosen, rationale, embedding (bytes | None)
        Workspaces whose memory.db is missing or has no decisions table are silently skipped.
        """
        all_decisions: list[dict] = []

        for workspace_id, ws_info in workspace_registry.items():
            root_path = ws_info.get("root_path", "")
            db_path = Path(root_path) / ".claude" / "memory.db"

            if not db_path.exists():
                continue

            try:
                conn = sqlite3.connect(
                    f"file:{db_path}?mode=ro", uri=True, check_same_thread=False
                )
                conn.row_factory = sqlite3.Row
                try:
                    rows = conn.execute(
                        "SELECT id, chosen, rationale, embedding "
                        "FROM decisions "
                        "WHERE status = 'active'"
                    ).fetchall()
                except sqlite3.OperationalError:
                    # Table doesn't exist yet — skip workspace
                    conn.close()
                    continue

                for row in rows:
                    all_decisions.append(
                        {
                            "workspace_id": workspace_id,
                            "decision_id": row["id"],
                            "chosen": row["chosen"] or "",
                            "rationale": row["rationale"] or "",
                            "embedding": row["embedding"],  # bytes or None
                        }
                    )
                conn.close()

            except sqlite3.OperationalError:
                # DB locked, corrupt, or inaccessible — skip gracefully
                continue

        return all_decisions

    def _build_embedding_matrix(self, decisions: list[dict]) -> np.ndarray:
        """Return (N, dim) float32 matrix. Uses stored embeddings where available,
        falls back to live embedding via EmbeddingService."""
        vecs: list[np.ndarray] = []
        texts_to_embed: list[int] = []  # indices needing live embedding

        for i, d in enumerate(decisions):
            raw = d.get("embedding")
            if raw is not None and isinstance(raw, (bytes, bytearray)) and len(raw) > 0:
                try:
                    vec = np.frombuffer(raw, dtype=np.float32).copy()
                    vecs.append(vec)
                    continue
                except Exception:
                    pass
            # Mark for live embedding
            vecs.append(None)  # type: ignore[arg-type]
            texts_to_embed.append(i)

        if texts_to_embed:
            texts = [
                f"{decisions[i]['chosen']} {decisions[i]['rationale']}".strip()
                for i in texts_to_embed
            ]
            live_embeddings = self._emb_service.embed(texts)
            for offset, idx in enumerate(texts_to_embed):
                vecs[idx] = live_embeddings[offset]

        matrix = np.stack(vecs).astype(np.float32)

        # Attach _vec to each decision for later use (centroid calc, rep decision)
        for i, d in enumerate(decisions):
            d["_vec"] = vecs[i]

        return matrix

    def _cluster(self, embeddings: np.ndarray, n_clusters: int) -> np.ndarray:
        """Simple numpy k-means with optional k-means++ initialisation.

        Args:
            embeddings: (N, dim) float32 array.
            n_clusters: Number of clusters k.

        Returns:
            Integer cluster assignment array of shape (N,).
        """
        n = embeddings.shape[0]
        if n <= n_clusters:
            return np.arange(n, dtype=np.int64)

        rng = np.random.default_rng(42)

        # k-means++ initialisation
        centroids = [embeddings[rng.integers(n)].copy()]
        for _ in range(1, n_clusters):
            dists = np.array([
                min(np.linalg.norm(e - c) ** 2 for c in centroids)
                for e in embeddings
            ])
            total = dists.sum()
            if total == 0:
                idx = rng.integers(n)
            else:
                probs = dists / total
                idx = int(rng.choice(n, p=probs))
            centroids.append(embeddings[idx].copy())
        centroids_arr = np.stack(centroids)  # (k, dim)

        for _ in range(20):
            # Assignment step
            diffs = embeddings[:, None, :] - centroids_arr[None, :, :]  # (N, k, dim)
            dists = np.linalg.norm(diffs, axis=2)                        # (N, k)
            assignments = np.argmin(dists, axis=1)                       # (N,)

            # Update step
            new_centroids = centroids_arr.copy()
            for k in range(n_clusters):
                members = embeddings[assignments == k]
                if len(members) > 0:
                    new_centroids[k] = members.mean(axis=0)

            if np.allclose(centroids_arr, new_centroids, atol=1e-6):
                break
            centroids_arr = new_centroids

        return assignments

    def _label_cluster(self, decisions: list[dict]) -> str:
        """Generate a short human-readable label for a cluster.

        Strategy:
        1. Find the most frequent meaningful token across all 'chosen' texts.
        2. Fall back to first 60 chars of the most common 'chosen' text.
        """
        STOPWORDS = {
            "the", "a", "an", "to", "of", "in", "for", "and", "or", "with",
            "use", "using", "used", "we", "i", "it", "is", "was", "be", "by",
            "on", "at", "this", "that", "are", "as", "from", "all", "our",
        }

        token_counts: Counter[str] = Counter()
        for d in decisions:
            tokens = d["chosen"].lower().split()
            for t in tokens:
                # Strip punctuation
                t = t.strip(".,;:!?\"'()")
                if len(t) >= 4 and t not in STOPWORDS:
                    token_counts[t] += 1

        if token_counts:
            best_token, _ = token_counts.most_common(1)[0]
            # Collect top 3 tokens for a richer label
            top_tokens = [tok for tok, _ in token_counts.most_common(3)]
            return " + ".join(top_tokens)

        # Fallback: first 60 chars of most common chosen text
        chosen_counts: Counter[str] = Counter(d["chosen"] for d in decisions)
        most_common_chosen = chosen_counts.most_common(1)[0][0]
        return most_common_chosen[:60]

    def _write_patterns(self, patterns: list[dict]) -> str:
        """Serialise patterns to {data_dir}/metagraph/patterns.json. Returns path string."""
        out_dir = self._data_dir / "metagraph"
        out_dir.mkdir(parents=True, exist_ok=True)
        out_path = out_dir / "patterns.json"

        payload = {
            "schema": "tentaqles-patterns-v1",
            "generated_at": datetime.now(timezone.utc).isoformat(),
            "patterns": patterns,
        }
        out_path.write_text(json.dumps(payload, indent=2), encoding="utf-8")
        return str(out_path)
