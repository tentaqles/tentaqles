"""Board service — Vibe Kanban board CRUD and card management."""

from __future__ import annotations

import json
import logging
import uuid
from datetime import datetime
from pathlib import Path

from sidecar.models import (
    Board,
    BoardScope,
    Card,
    CardComment,
    CardExecution,
    CardPriority,
    CardRelationship,
    CardStatus,
    Column,
    RelationshipType,
)
from sidecar.services.safe_io import atomic_write

logger = logging.getLogger(__name__)

_DEFAULT_COLUMNS = ["Backlog", "In Progress", "Review", "Done"]
_DEFAULT_COLORS = ["#6b7280", "#3b82f6", "#f59e0b", "#22c55e"]


def _uid(prefix: str) -> str:
    return f"{prefix}-{uuid.uuid4().hex[:8]}"


def _now() -> str:
    return datetime.now().isoformat(timespec="seconds")


def _boards_dir(scope: str, client_path: str | None = None) -> Path:
    if scope == BoardScope.GLOBAL:
        return Path.home() / ".tentaqles" / "boards"
    if client_path:
        return Path(client_path) / ".tentaqles" / "boards"
    raise ValueError("client_path required for client-scoped boards")


def _board_path(board_id: str, scope: str, client_path: str | None = None) -> Path:
    return _boards_dir(scope, client_path) / f"{board_id}.json"


def _read_board(path: Path) -> Board:
    data = json.loads(path.read_text(encoding="utf-8"))
    return Board(**data)


def _write_board(path: Path, board: Board) -> None:
    atomic_write(path, board.model_dump_json(indent=2))


# --- Board CRUD ---


def list_boards(scope: str | None = None, client_path: str | None = None) -> list[Board]:
    """List all boards for the given scope(s)."""
    results: list[Board] = []
    scopes = [scope] if scope else [BoardScope.GLOBAL, BoardScope.CLIENT]

    for s in scopes:
        if s == BoardScope.CLIENT and not client_path:
            continue
        bdir = _boards_dir(s, client_path)
        if not bdir.is_dir():
            continue
        for f in sorted(bdir.glob("*.json")):
            try:
                results.append(_read_board(f))
            except (json.JSONDecodeError, ValueError) as e:
                logger.warning("Failed to load board %s: %s", f, e)
    return results


def get_board(board_id: str, scope: str, client_path: str | None = None) -> Board:
    """Get a single board by ID."""
    path = _board_path(board_id, scope, client_path)
    if not path.exists():
        raise ValueError(f"Board not found: {board_id}")
    return _read_board(path)


def create_board(
    name: str,
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
    columns: list[str] | None = None,
) -> Board:
    """Create a new board with default or custom columns."""
    col_names = columns or _DEFAULT_COLUMNS
    cols = [
        Column(
            id=_uid("col"),
            name=cn,
            color=_DEFAULT_COLORS[i % len(_DEFAULT_COLORS)],
        )
        for i, cn in enumerate(col_names)
    ]

    now = _now()
    board = Board(
        id=_uid("board"),
        name=name,
        scope=BoardScope(scope),
        client_name=Path(client_path).name if client_path else None,
        columns=cols,
        cards=[],
        created=now,
        updated=now,
    )

    path = _board_path(board.id, scope, client_path)
    _write_board(path, board)
    return board


def delete_board(board_id: str, scope: str, client_path: str | None = None) -> None:
    """Delete a board."""
    path = _board_path(board_id, scope, client_path)
    if path.exists():
        path.unlink()


# --- Card CRUD ---


def add_card(
    board_id: str,
    title: str,
    description: str = "",
    column_id: str | None = None,
    priority: str = "medium",
    labels: list[str] | None = None,
    assignee: str | None = None,
    due_date: str | None = None,
    project_path: str | None = None,
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
) -> Board:
    """Add a card to a board. Returns updated board."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    if not board.columns:
        raise ValueError("Board has no columns")

    target_col = column_id or board.columns[0].id
    # Validate column exists
    if not any(c.id == target_col for c in board.columns):
        raise ValueError(f"Column not found: {target_col}")

    # Calculate position (append to end of column)
    max_pos = max((c.position for c in board.cards if c.column_id == target_col), default=-1)

    now = _now()
    card = Card(
        id=_uid("card"),
        column_id=target_col,
        title=title,
        description=description,
        labels=labels or [],
        priority=CardPriority(priority),
        assignee=assignee,
        due_date=due_date,
        project_path=project_path,
        position=max_pos + 1,
        status=CardStatus.ACTIVE,
        created=now,
        updated=now,
    )

    board.cards.append(card)
    board.updated = now
    _write_board(path, board)
    return board


def update_card(
    board_id: str,
    card_id: str,
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
    **kwargs,
) -> Board:
    """Update card fields. Returns updated board."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    card = next((c for c in board.cards if c.id == card_id), None)
    if not card:
        raise ValueError(f"Card not found: {card_id}")

    now = _now()
    for key, value in kwargs.items():
        if value is not None and hasattr(card, key):
            if key == "priority":
                setattr(card, key, CardPriority(value))
            elif key == "status":
                new_status = CardStatus(value)
                setattr(card, key, new_status)
                if new_status == CardStatus.ARCHIVED and not card.completed:
                    card.completed = now
            elif key == "execution":
                card.execution = CardExecution(**value) if isinstance(value, dict) else value
            else:
                setattr(card, key, value)

    card.updated = now
    board.updated = now
    _write_board(path, board)
    return board


def move_card(
    board_id: str,
    card_id: str,
    target_column_id: str,
    position: int = 0,
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
) -> Board:
    """Move a card to a different column/position. Returns updated board."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    card = next((c for c in board.cards if c.id == card_id), None)
    if not card:
        raise ValueError(f"Card not found: {card_id}")

    if not any(c.id == target_column_id for c in board.columns):
        raise ValueError(f"Column not found: {target_column_id}")

    # Enforce WIP limit
    target_col = next(c for c in board.columns if c.id == target_column_id)
    if target_col.wip_limit is not None:
        current_count = sum(1 for c in board.cards if c.column_id == target_column_id and c.id != card_id)
        if current_count >= target_col.wip_limit:
            raise ValueError(f"Column '{target_col.name}' has reached its WIP limit of {target_col.wip_limit}")

    # Remove from current column ordering
    old_col_cards = sorted(
        [c for c in board.cards if c.column_id == card.column_id and c.id != card_id],
        key=lambda c: c.position,
    )
    for i, c in enumerate(old_col_cards):
        c.position = i

    # Insert into target column at position
    card.column_id = target_column_id
    target_cards = sorted(
        [c for c in board.cards if c.column_id == target_column_id and c.id != card_id],
        key=lambda c: c.position,
    )
    target_cards.insert(min(position, len(target_cards)), card)
    for i, c in enumerate(target_cards):
        c.position = i

    # Auto-complete when moved to last column
    last_col = board.columns[-1]
    now = _now()
    if target_column_id == last_col.id and not card.completed:
        card.completed = now

    card.updated = now
    board.updated = now
    _write_board(path, board)
    return board


def delete_card(
    board_id: str,
    card_id: str,
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
) -> Board:
    """Delete a card from a board. Returns updated board."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    board.cards = [c for c in board.cards if c.id != card_id]
    board.updated = _now()
    _write_board(path, board)
    return board


# --- Column management ---


def add_column(
    board_id: str,
    name: str,
    color: str | None = None,
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
) -> Board:
    """Add a column to a board. Returns updated board."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    col = Column(id=_uid("col"), name=name, color=color)
    board.columns.append(col)
    board.updated = _now()
    _write_board(path, board)
    return board


def remove_column(
    board_id: str,
    column_id: str,
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
) -> Board:
    """Remove a column, moving its cards to the first column. Returns updated board."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    if len(board.columns) <= 1:
        raise ValueError("Cannot remove the last column")

    first_col = next((c for c in board.columns if c.id != column_id), None)
    if not first_col:
        raise ValueError("No fallback column available")

    # Move orphaned cards to first column
    for card in board.cards:
        if card.column_id == column_id:
            card.column_id = first_col.id

    board.columns = [c for c in board.columns if c.id != column_id]
    board.updated = _now()
    _write_board(path, board)
    return board


def reorder_columns(
    board_id: str,
    column_ids: list[str],
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
) -> Board:
    """Reorder columns by providing the full list of column IDs in desired order."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    col_map = {c.id: c for c in board.columns}
    board.columns = [col_map[cid] for cid in column_ids if cid in col_map]
    board.updated = _now()
    _write_board(path, board)
    return board


# --- Comments ---


def add_card_comment(
    board_id: str,
    card_id: str,
    author: str,
    text: str,
    parent_id: str | None = None,
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
) -> Board:
    """Add a comment to a card."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    card = next((c for c in board.cards if c.id == card_id), None)
    if not card:
        raise ValueError(f"Card not found: {card_id}")

    now = _now()
    comment = CardComment(
        id=_uid("cmt"),
        card_id=card_id,
        author=author,
        text=text,
        parent_id=parent_id,
        created=now,
        updated=now,
    )
    card.comments.append(comment)
    card.updated = now
    board.updated = now
    _write_board(path, board)
    return board


def update_card_comment(
    board_id: str,
    card_id: str,
    comment_id: str,
    text: str,
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
) -> Board:
    """Update a comment's text."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    card = next((c for c in board.cards if c.id == card_id), None)
    if not card:
        raise ValueError(f"Card not found: {card_id}")

    comment = next((cm for cm in card.comments if cm.id == comment_id), None)
    if not comment:
        raise ValueError(f"Comment not found: {comment_id}")

    comment.text = text
    comment.updated = _now()
    board.updated = comment.updated
    _write_board(path, board)
    return board


def delete_card_comment(
    board_id: str,
    card_id: str,
    comment_id: str,
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
) -> Board:
    """Delete a comment from a card."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    card = next((c for c in board.cards if c.id == card_id), None)
    if not card:
        raise ValueError(f"Card not found: {card_id}")

    card.comments = [cm for cm in card.comments if cm.id != comment_id]
    card.updated = _now()
    board.updated = card.updated
    _write_board(path, board)
    return board


def toggle_comment_reaction(
    board_id: str,
    card_id: str,
    comment_id: str,
    emoji: str,
    author: str,
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
) -> Board:
    """Toggle a reaction on a comment."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    card = next((c for c in board.cards if c.id == card_id), None)
    if not card:
        raise ValueError(f"Card not found: {card_id}")

    comment = next((cm for cm in card.comments if cm.id == comment_id), None)
    if not comment:
        raise ValueError(f"Comment not found: {comment_id}")

    if emoji not in comment.reactions:
        comment.reactions[emoji] = []

    if author in comment.reactions[emoji]:
        comment.reactions[emoji].remove(author)
        if not comment.reactions[emoji]:
            del comment.reactions[emoji]
    else:
        comment.reactions[emoji].append(author)

    board.updated = _now()
    _write_board(path, board)
    return board


# --- Sub-cards ---


def add_sub_card(
    board_id: str,
    parent_card_id: str,
    title: str,
    priority: str = "medium",
    assignee: str | None = None,
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
) -> Board:
    """Create a sub-card under a parent card."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    parent = next((c for c in board.cards if c.id == parent_card_id), None)
    if not parent:
        raise ValueError(f"Parent card not found: {parent_card_id}")

    # Place sub-card in same column as parent
    existing_subs = [c for c in board.cards if c.parent_card_id == parent_card_id]
    max_sort = max((c.sub_card_sort_order or 0 for c in existing_subs), default=-1)
    max_pos = max((c.position for c in board.cards if c.column_id == parent.column_id), default=-1)

    now = _now()
    card = Card(
        id=_uid("card"),
        column_id=parent.column_id,
        title=title,
        priority=CardPriority(priority),
        assignee=assignee,
        parent_card_id=parent_card_id,
        sub_card_sort_order=max_sort + 1,
        position=max_pos + 1,
        status=CardStatus.ACTIVE,
        created=now,
        updated=now,
    )

    board.cards.append(card)
    board.updated = now
    _write_board(path, board)
    return board


def set_parent_card(
    board_id: str,
    card_id: str,
    parent_card_id: str,
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
) -> Board:
    """Set a parent for an existing card."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    card = next((c for c in board.cards if c.id == card_id), None)
    if not card:
        raise ValueError(f"Card not found: {card_id}")

    if not any(c.id == parent_card_id for c in board.cards):
        raise ValueError(f"Parent card not found: {parent_card_id}")

    existing_subs = [c for c in board.cards if c.parent_card_id == parent_card_id]
    max_sort = max((c.sub_card_sort_order or 0 for c in existing_subs), default=-1)

    card.parent_card_id = parent_card_id
    card.sub_card_sort_order = max_sort + 1
    card.updated = _now()
    board.updated = card.updated
    _write_board(path, board)
    return board


def remove_parent_card(
    board_id: str,
    card_id: str,
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
) -> Board:
    """Make a sub-card independent."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    card = next((c for c in board.cards if c.id == card_id), None)
    if not card:
        raise ValueError(f"Card not found: {card_id}")

    card.parent_card_id = None
    card.sub_card_sort_order = None
    card.updated = _now()
    board.updated = card.updated
    _write_board(path, board)
    return board


def reorder_sub_cards(
    board_id: str,
    parent_card_id: str,
    card_ids: list[str],
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
) -> Board:
    """Reorder sub-cards by providing ordered list of card IDs."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    for i, cid in enumerate(card_ids):
        card = next((c for c in board.cards if c.id == cid and c.parent_card_id == parent_card_id), None)
        if card:
            card.sub_card_sort_order = i

    board.updated = _now()
    _write_board(path, board)
    return board


# --- Relationships ---


def add_relationship(
    board_id: str,
    source_card_id: str,
    target_card_id: str,
    rel_type: str = "related",
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
) -> Board:
    """Add a relationship between two cards."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    if not any(c.id == source_card_id for c in board.cards):
        raise ValueError(f"Source card not found: {source_card_id}")
    if not any(c.id == target_card_id for c in board.cards):
        raise ValueError(f"Target card not found: {target_card_id}")

    rel = CardRelationship(
        id=_uid("rel"),
        source_card_id=source_card_id,
        target_card_id=target_card_id,
        type=RelationshipType(rel_type),
    )
    board.relationships.append(rel)
    board.updated = _now()
    _write_board(path, board)
    return board


def remove_relationship(
    board_id: str,
    relationship_id: str,
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
) -> Board:
    """Remove a relationship."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    board.relationships = [r for r in board.relationships if r.id != relationship_id]
    board.updated = _now()
    _write_board(path, board)
    return board


# --- Column updates ---


def update_column(
    board_id: str,
    column_id: str,
    scope: str = BoardScope.CLIENT,
    client_path: str | None = None,
    **kwargs,
) -> Board:
    """Update column properties (name, color, wip_limit, hidden)."""
    path = _board_path(board_id, scope, client_path)
    board = _read_board(path)

    col = next((c for c in board.columns if c.id == column_id), None)
    if not col:
        raise ValueError(f"Column not found: {column_id}")

    for key, value in kwargs.items():
        if hasattr(col, key) and value is not None:
            setattr(col, key, value)

    board.updated = _now()
    _write_board(path, board)
    return board
