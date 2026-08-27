"""Memory decay functions based on the Ebbinghaus forgetting curve.

All functions are pure (stateless) except strengthen_memory which writes
to the database.  Import-safe — no heavy dependencies at module level.
"""

from __future__ import annotations

import math
import sqlite3


def ebbinghaus_score(
    strength: float,
    days_since_last_recall: float,
    n_recalls: int,
) -> float:
    """Compute a decayed memory strength score.

    Formula: strength * exp(-days / half_life) * (1 + 0.1 * n_recalls)

    The recall multiplier rewards memories that have been reinforced.
    Result is clamped to [0.0, 1.0].

    Args:
        strength: Current strength value (0.0–1.0).
        days_since_last_recall: Days elapsed since the memory was last recalled.
        n_recalls: Total number of times the memory has been recalled.

    Returns:
        Decayed strength score in [0.0, 1.0].
    """
    half_life = 30.0
    raw = strength * math.exp(-days_since_last_recall / half_life) * (1.0 + 0.1 * n_recalls)
    return max(0.0, min(1.0, raw))


def decay_sql_expr(
    half_life_days: float = 30.0,
    recalled_col: str = "last_recalled",
    strength_col: str = "strength",
) -> str:
    """Return a SQLite expression that computes a decayed strength score.

    Uses julianday arithmetic so it can be embedded in ORDER BY / WHERE
    clauses without any Python-side computation.

    The expression evaluates to:
        exp(-(days_elapsed / half_life)) * strength

    Note: The recall-count boost is omitted here because ORDER BY expressions
    cannot reference recall_count without joining — callers that need the full
    formula should add it explicitly.

    Args:
        half_life_days: Decay half-life in days.
        recalled_col: Column name that holds the last-recalled timestamp.
        strength_col: Column name that holds the current strength value.

    Returns:
        A SQL expression string suitable for ORDER BY or SELECT.
    """
    return (
        f"exp(-(julianday('now') - julianday({recalled_col})) / {half_life_days:.1f})"
        f" * {strength_col}"
    )


def strengthen_memory(conn: sqlite3.Connection, table: str, row_id: str) -> None:
    """Increment recall count, bump strength, and update last_recalled.

    Strength is capped at 1.0.  Uses parameterised query to prevent
    SQL injection even though table/row_id come from trusted internal code.

    Args:
        conn: Open sqlite3 connection to the target database.
        table: Name of the table (e.g. "semantic_memories").
        row_id: Primary key value of the row to strengthen.
    """
    # Table names cannot be parameterised in SQLite — use an allowlist-style
    # check to guard against accidental misuse.
    allowed_tables = {"semantic_memories", "procedural_memories"}
    if table not in allowed_tables:
        raise ValueError(f"strengthen_memory: unknown table {table!r}")

    conn.execute(
        f"""UPDATE {table}
            SET strength      = min(1.0, strength + 0.1),
                recall_count  = recall_count + 1,
                last_recalled = datetime('now')
            WHERE id = ?""",
        (row_id,),
    )
    conn.commit()
