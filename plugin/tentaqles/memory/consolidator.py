"""Memory consolidation: compaction, procedural pattern detection, and decay eviction.

MemoryConsolidator ties together the four-tier memory model:
  - Working  (live session)
  - Episodic (ended sessions — already handled by store.end_session)
  - Semantic (LLM-extracted facts, written here)
  - Procedural (repeated decision patterns, detected here)

Usage (no LLM — MVP path):
    consolidator = MemoryConsolidator(store)
    result = consolidator.maybe_compact()   # evicts stale + detects patterns

Usage (with LLM):
    consolidator = MemoryConsolidator(store, llm_fn=claude_caller)
    result = consolidator.maybe_compact()
"""

from __future__ import annotations

import json
import sqlite3
import uuid
from datetime import datetime, timezone
from typing import Callable

from tentaqles.memory.decay import ebbinghaus_score


def _now() -> str:
    return datetime.now(timezone.utc).isoformat()


class MemoryConsolidator:
    """Manages promotion of episodic sessions into semantic and procedural tiers."""

    def __init__(
        self,
        store,  # MemoryStore — typed loosely to avoid circular import
        llm_fn: Callable[[str], str] | None = None,
    ) -> None:
        self._store = store
        self._llm_fn = llm_fn
        self._conn: sqlite3.Connection = store._conn

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def maybe_compact(self, every_n_sessions: int = 10) -> dict:
        """Check session count and run compaction if threshold is crossed.

        Compaction fires when the number of episodic sessions that have
        *not yet been source-referenced in any semantic_memory* is a
        multiple of ``every_n_sessions``.  This prevents re-compacting
        sessions that already contributed to semantic facts.

        Returns:
            {
                "compacted": bool,
                "facts_added": int,
                "patterns_found": int,
                "evicted": int,
            }
        """
        # Count episodic sessions not yet referenced in semantic memories
        already_sourced: set[str] = set()
        rows = self._conn.execute(
            "SELECT source_sessions FROM semantic_memories"
        ).fetchall()
        for (src_json,) in rows:
            try:
                already_sourced.update(json.loads(src_json or "[]"))
            except (ValueError, TypeError):
                pass

        # Treat all completed sessions as episodic — the memory_tier column
        # is set by the migration backfill and by session-end.py (Wave 3).
        # We also match sessions where ended_at IS NOT NULL but memory_tier
        # is still 'working' (newly created sessions before Wave 3 ships).
        episodic_rows = self._conn.execute(
            "SELECT id FROM sessions "
            "WHERE ended_at IS NOT NULL "
            "ORDER BY ended_at ASC"
        ).fetchall()
        unsourced = [r[0] for r in episodic_rows if r[0] not in already_sourced]

        compacted = False
        facts_added = 0
        patterns_found = 0

        # Fire only when we have exactly N, 2N, 3N … unsourced sessions
        if len(unsourced) > 0 and len(unsourced) % every_n_sessions == 0:
            new_ids = self.run_compaction(unsourced[-every_n_sessions:])
            facts_added = len(new_ids)
            compacted = True

        # Always run procedural detection and eviction on every call
        patterns = self.detect_procedural_patterns()
        patterns_found = len(patterns)
        evicted = self.evict_stale()

        return {
            "compacted": compacted,
            "facts_added": facts_added,
            "patterns_found": patterns_found,
            "evicted": evicted,
        }

    def run_compaction(self, session_ids: list[str]) -> list[str]:
        """Extract semantic facts from the given episodic session summaries.

        If ``llm_fn`` is provided, calls it with a structured prompt and
        parses the response for newline-separated facts.  Without a LLM,
        this is a no-op (returns empty list) so the MVP path is safe.

        Args:
            session_ids: IDs of episodic sessions to compact.

        Returns:
            List of new semantic_memory IDs that were written.
        """
        if not session_ids:
            return []

        if self._llm_fn is None:
            # MVP path — semantic tier pre-wired but populated via skill only
            return []

        # Pull summaries for the requested sessions
        placeholders = ",".join("?" * len(session_ids))
        rows = self._conn.execute(
            f"SELECT id, summary FROM sessions WHERE id IN ({placeholders})",
            session_ids,
        ).fetchall()

        summaries = [(r[0], r[1] or "") for r in rows if r[1]]
        if not summaries:
            return []

        # Build compaction prompt
        prompt_parts = [
            "You are a memory consolidation assistant. "
            "Extract concise, reusable facts or patterns from these session summaries. "
            "Return one fact per line. "
            "Each fact should be self-contained and useful for future sessions. "
            "Do not include session-specific details like exact filenames unless they are architectural.\n"
        ]
        for sid, summary in summaries:
            prompt_parts.append(f"---\nSession {sid}:\n{summary}")
        prompt_parts.append("---\nFacts:")
        prompt = "\n".join(prompt_parts)

        try:
            response = self._llm_fn(prompt)
        except Exception:
            return []

        # Parse response — one fact per non-empty line
        facts = [line.strip("- •\t ") for line in response.splitlines() if line.strip()]
        facts = [f for f in facts if len(f) > 10]  # ignore trivially short lines

        new_ids: list[str] = []
        source_ids = [s[0] for s in summaries]
        for fact in facts:
            try:
                fid = self._store.record_semantic_fact(
                    fact=fact,
                    source_sessions=source_ids,
                    category="general",
                )
                new_ids.append(fid)
            except Exception:
                pass

        return new_ids

    def detect_procedural_patterns(self, min_occurrences: int = 3) -> list[dict]:
        """Find repeated decision patterns and promote them to procedural_memories.

        Groups decisions by (lowercased chosen token signature, tags JSON) and
        finds groups with >= min_occurrences entries.  For each qualifying group,
        upserts a row in procedural_memories (updates occurrence_count + last_seen
        if the trigger_pattern already exists, otherwise inserts a new row).

        Args:
            min_occurrences: Minimum times a pattern must appear.

        Returns:
            List of procedural pattern dicts that were written/updated.
        """
        # Build a token signature for each decision's chosen text:
        # lowercase, split on whitespace, take first 3 tokens joined by "_"
        rows = self._conn.execute(
            "SELECT id, chosen, tags, created_at FROM decisions WHERE status='active'"
        ).fetchall()

        # Group by (token_sig, tags_json)
        groups: dict[tuple[str, str], list[dict]] = {}
        for did, chosen, tags_json, created_at in rows:
            sig = "_".join((chosen or "").lower().split()[:3])
            norm_tags = tags_json or "[]"
            key = (sig, norm_tags)
            groups.setdefault(key, []).append(
                {"id": did, "chosen": chosen, "created_at": created_at}
            )

        patterns_written: list[dict] = []
        for (sig, tags_json), entries in groups.items():
            if len(entries) < min_occurrences:
                continue

            occurrence_count = len(entries)
            last_seen = max(e["created_at"] for e in entries)
            # Use the most common chosen text in the group as the workflow name
            chosen_example = entries[0]["chosen"]
            workflow_name = (chosen_example or sig)[:80]
            steps_json = json.dumps([e["chosen"] for e in entries[:10]])
            tags = tags_json

            try:
                existing = self._conn.execute(
                    "SELECT id FROM procedural_memories WHERE trigger_pattern = ?",
                    (sig,),
                ).fetchone()

                if existing:
                    self._conn.execute(
                        "UPDATE procedural_memories "
                        "SET occurrence_count=?, last_seen=? WHERE id=?",
                        (occurrence_count, last_seen, existing[0]),
                    )
                    pid = existing[0]
                else:
                    pid = uuid.uuid4().hex
                    self._conn.execute(
                        "INSERT INTO procedural_memories "
                        "(id, created_at, workflow_name, steps, trigger_pattern, "
                        " occurrence_count, last_seen, strength, tags) "
                        "VALUES (?, ?, ?, ?, ?, ?, ?, 1.0, ?)",
                        (pid, _now(), workflow_name, steps_json, sig,
                         occurrence_count, last_seen, tags),
                    )
                self._conn.commit()

                patterns_written.append({
                    "id": pid,
                    "trigger_pattern": sig,
                    "workflow_name": workflow_name,
                    "occurrence_count": occurrence_count,
                })
            except Exception:
                pass

        return patterns_written

    def evict_stale(
        self,
        min_score: float = 0.01,
        older_than_days: int = 180,
    ) -> int:
        """Delete semantic_memories with a decayed score below threshold.

        A row is evicted only if BOTH conditions hold:
        1. ebbinghaus_score(strength, days_since_last_recalled, recall_count) < min_score
        2. created_at is older than older_than_days

        Args:
            min_score: Ebbinghaus score floor below which a fact is considered forgotten.
            older_than_days: Minimum age (days since created_at) required for eviction.

        Returns:
            Number of rows deleted.
        """
        rows = self._conn.execute(
            "SELECT id, strength, recall_count, last_recalled, created_at "
            "FROM semantic_memories"
        ).fetchall()

        now = datetime.now(timezone.utc)
        to_delete: list[str] = []

        for row_id, strength, recall_count, last_recalled, created_at in rows:
            # Age check
            try:
                created = datetime.fromisoformat(created_at)
                if created.tzinfo is None:
                    created = created.replace(tzinfo=timezone.utc)
                age_days = (now - created).total_seconds() / 86400
            except (ValueError, TypeError):
                continue  # skip rows with unparseable dates

            if age_days < older_than_days:
                continue

            # Decay score check
            try:
                if last_recalled:
                    last = datetime.fromisoformat(last_recalled)
                    if last.tzinfo is None:
                        last = last.replace(tzinfo=timezone.utc)
                    days_since = (now - last).total_seconds() / 86400
                else:
                    days_since = age_days  # never recalled → use full age
            except (ValueError, TypeError):
                days_since = age_days

            score = ebbinghaus_score(
                strength or 1.0,
                days_since,
                recall_count or 0,
            )

            if score < min_score:
                to_delete.append(row_id)

        if to_delete:
            placeholders = ",".join("?" * len(to_delete))
            self._conn.execute(
                f"DELETE FROM semantic_memories WHERE id IN ({placeholders})",
                to_delete,
            )
            self._conn.commit()

        return len(to_delete)
