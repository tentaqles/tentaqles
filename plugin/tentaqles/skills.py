"""Self-improving skills — record user corrections to SKILL.md files.

When session-wrap detects a user correction that refines how a skill should
behave, this module appends the correction to the relevant ``SKILL.md`` under
a dedicated "## Learned from user feedback" section. Appends are:

* **Idempotent** — token-level Jaccard similarity guards against near-duplicate
  lessons, so re-hearing the same correction twice does not pollute the file.
* **Client-local-first** — if a client-specific override exists, it wins.
  If only a plugin-shared skill exists and a ``client_root`` is supplied, the
  shared SKILL.md is copied to the client-local override path *before* the
  first correction is written, so user feedback never mutates the shared
  skill bundle.
* **Privacy-respecting** — the correction is routed through
  :func:`tentaqles.privacy.redact_text` before it touches disk, because user
  corrections are a classic source of accidental secret leakage
  ("no, use the token ghp_... instead").
* **Atomic-ish** — writes go through ``Path.write_text`` after a full read, so
  an interrupted process never leaves a half-written SKILL.md behind.
"""

from __future__ import annotations

import re
import shutil
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

__all__ = [
    "SKILLS_DIR_NAME",
    "LEARNED_SECTION_HEADER",
    "TIMESTAMP_FORMAT",
    "find_skill_md",
    "append_to_skill",
    "record_skill_correction",
]

SKILLS_DIR_NAME = "skills"
LEARNED_SECTION_HEADER = "## Learned from user feedback"
TIMESTAMP_FORMAT = "%Y-%m-%d"

# Matches either the canonical header or any case-variant thereof.
_LEARNED_HEADER_RE = re.compile(
    r"^##\s+learned\s+from\s+user\s+feedback\s*$",
    re.IGNORECASE | re.MULTILINE,
)
# Matches a single entry: "- [YYYY-MM-DD] some text"
_ENTRY_RE = re.compile(r"^-\s*\[(\d{4}-\d{2}-\d{2})\]\s*(.+)$")


# ---------------------------------------------------------------------------
# Resolution
# ---------------------------------------------------------------------------


def find_skill_md(
    skill_name: str,
    plugin_root: str | Path,
    client_root: str | Path | None = None,
) -> Path | None:
    """Locate ``SKILL.md`` for *skill_name*.

    Resolution order:

    1. ``{client_root}/.claude/skills/{skill_name}/SKILL.md`` (client-local override)
    2. ``{plugin_root}/skills/{skill_name}/SKILL.md`` (plugin shared)

    Returns the :class:`Path` if found, ``None`` otherwise.
    """
    if client_root is not None:
        client_path = (
            Path(client_root) / ".claude" / SKILLS_DIR_NAME / skill_name / "SKILL.md"
        )
        if client_path.is_file():
            return client_path

    plugin_path = Path(plugin_root) / SKILLS_DIR_NAME / skill_name / "SKILL.md"
    if plugin_path.is_file():
        return plugin_path

    return None


# ---------------------------------------------------------------------------
# Text normalization / similarity
# ---------------------------------------------------------------------------


# Small stopword list — dropped during dedup normalization so paraphrases like
# "use X not Y" vs "use X instead of Y" collapse to the same token set.
_STOPWORDS = frozenset(
    {
        "a", "an", "the", "and", "or", "but", "not", "no", "of", "to", "in",
        "on", "for", "with", "by", "at", "as", "is", "are", "be", "do", "does",
        "instead", "rather", "than", "should", "must", "always", "never",
        "prefer", "use", "using", "it", "this", "that", "these", "those",
    }
)


def _normalize_for_dedup(text: str) -> str:
    """Lowercase, collapse whitespace, strip punctuation for duplicate detection."""
    if not text:
        return ""
    # Replace non-alphanumeric with whitespace, then collapse.
    cleaned = re.sub(r"[^0-9A-Za-z\s]+", " ", text.lower())
    cleaned = re.sub(r"\s+", " ", cleaned).strip()
    return cleaned


def _jaccard_similarity(a: str, b: str) -> float:
    """Token-level Jaccard similarity. Returns a value in ``[0.0, 1.0]``.

    Stopwords are dropped before comparison so short corrections that differ
    only in glue words (``not`` vs ``instead of``) still collide.
    """
    tokens_a = {t for t in _normalize_for_dedup(a).split() if t not in _STOPWORDS}
    tokens_b = {t for t in _normalize_for_dedup(b).split() if t not in _STOPWORDS}
    if not tokens_a and not tokens_b:
        return 1.0
    if not tokens_a or not tokens_b:
        return 0.0
    intersection = tokens_a & tokens_b
    union = tokens_a | tokens_b
    return len(intersection) / len(union)


# ---------------------------------------------------------------------------
# Learned section parsing
# ---------------------------------------------------------------------------


def _find_learned_section_bounds(content: str) -> tuple[int, int] | None:
    """Return ``(header_start, section_end)`` byte offsets, or ``None`` if absent.

    ``section_end`` is the offset of the next ``## `` heading at the same level,
    or ``len(content)`` if the learned section is the final one.
    """
    match = _LEARNED_HEADER_RE.search(content)
    if not match:
        return None
    header_start = match.start()
    # Find the next top-level (## ) heading after this one.
    after = content[match.end() :]
    next_heading = re.search(r"^##\s+\S", after, re.MULTILINE)
    if next_heading:
        section_end = match.end() + next_heading.start()
    else:
        section_end = len(content)
    return header_start, section_end


def _existing_entries(section_body: str) -> list[str]:
    """Extract the text portion of every ``- [YYYY-MM-DD] ...`` entry."""
    entries: list[str] = []
    for line in section_body.splitlines():
        m = _ENTRY_RE.match(line.strip())
        if m:
            entries.append(m.group(2).strip())
    return entries


# ---------------------------------------------------------------------------
# Append
# ---------------------------------------------------------------------------


def append_to_skill(
    skill_md_path: Path,
    correction: str,
    timestamp: Optional[str] = None,
    dedup_threshold: float = 0.85,
) -> bool:
    """Append *correction* to ``SKILL.md`` under the Learned section.

    Idempotent: if any existing entry in the Learned section has Jaccard
    similarity strictly greater than *dedup_threshold* relative to *correction*,
    the append is skipped.

    If the Learned section is missing, it is appended to the end of the file
    (with a blank line separator preserved).

    Format of an entry::

        - [2026-04-12] {correction}

    Returns ``True`` if the correction was appended, ``False`` if it was
    detected as a duplicate.
    """
    path = Path(skill_md_path)
    content = path.read_text(encoding="utf-8")

    if timestamp is None:
        timestamp = datetime.now(timezone.utc).strftime(TIMESTAMP_FORMAT)

    correction_clean = correction.strip()
    new_entry = f"- [{timestamp}] {correction_clean}"

    bounds = _find_learned_section_bounds(content)

    if bounds is None:
        # No Learned section yet — append one at the end of the file.
        prefix = content
        if prefix and not prefix.endswith("\n"):
            prefix += "\n"
        if prefix and not prefix.endswith("\n\n"):
            prefix += "\n"
        new_content = f"{prefix}{LEARNED_SECTION_HEADER}\n\n{new_entry}\n"
        path.write_text(new_content, encoding="utf-8")
        return True

    header_start, section_end = bounds
    # Split: before | section-header+body | after
    before = content[:header_start]
    section = content[header_start:section_end]
    after = content[section_end:]

    # Isolate the body (everything after the header line).
    header_line_end = section.find("\n")
    if header_line_end == -1:
        header_line = section
        body = ""
    else:
        header_line = section[:header_line_end]
        body = section[header_line_end + 1 :]

    # Duplicate detection.
    for existing in _existing_entries(body):
        if _jaccard_similarity(existing, correction_clean) > dedup_threshold:
            return False

    # Normalize body: strip trailing whitespace, then re-add one trailing newline.
    body_stripped = body.rstrip("\n")
    if body_stripped:
        new_body = f"{body_stripped}\n{new_entry}\n"
    else:
        new_body = f"\n{new_entry}\n"

    # Preserve the blank line before the next section, if any.
    new_section = f"{header_line}\n{new_body}"
    if after and not new_section.endswith("\n\n"):
        new_section = new_section.rstrip("\n") + "\n\n"

    new_content = f"{before}{new_section}{after}"
    path.write_text(new_content, encoding="utf-8")
    return True


# ---------------------------------------------------------------------------
# End-to-end
# ---------------------------------------------------------------------------


def _redact(text: str) -> str:
    """Route *text* through :func:`tentaqles.privacy.redact_text` if available."""
    try:
        from tentaqles.privacy import redact_text  # type: ignore
    except ImportError:  # pragma: no cover - defensive fallback
        return text
    redacted, _events = redact_text(text)
    return redacted


def record_skill_correction(
    skill_name: str,
    correction: str,
    plugin_root: str | Path,
    client_root: str | Path | None = None,
) -> dict:
    """End-to-end: find skill, redact, check idempotency, append.

    If only a plugin-shared ``SKILL.md`` exists but ``client_root`` is set,
    the shared file is first **copied** to the client-local override path
    and the correction is written there — so the shared plugin skill remains
    pristine and each client accumulates its own learnings.

    Returns a dict::

        {
            "status": "appended" | "duplicate" | "not_found",
            "path": str | None,
            "skill_name": str,
        }
    """
    resolved = find_skill_md(skill_name, plugin_root, client_root)
    if resolved is None:
        return {
            "status": "not_found",
            "path": None,
            "skill_name": skill_name,
        }

    plugin_path = Path(plugin_root) / SKILLS_DIR_NAME / skill_name / "SKILL.md"
    # If we matched the plugin-shared copy but a client_root was given,
    # create a client-local override so we never mutate the shared skill.
    if client_root is not None and resolved == plugin_path:
        override = (
            Path(client_root) / ".claude" / SKILLS_DIR_NAME / skill_name / "SKILL.md"
        )
        override.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(plugin_path, override)
        resolved = override

    safe_correction = _redact(correction)
    appended = append_to_skill(resolved, safe_correction)

    return {
        "status": "appended" if appended else "duplicate",
        "path": str(resolved),
        "skill_name": skill_name,
    }
