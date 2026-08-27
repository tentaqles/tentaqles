"""Tests for board_service — Vibe Kanban CRUD, move, reorder, scope isolation."""

from __future__ import annotations

from pathlib import Path

import pytest

from sidecar.services.board_service import (
    add_card,
    add_column,
    create_board,
    delete_board,
    delete_card,
    get_board,
    list_boards,
    move_card,
    remove_column,
    reorder_columns,
    update_card,
)


# ---------------------------------------------------------------------------
# Board CRUD
# ---------------------------------------------------------------------------


def test_create_board_defaults(tmp_path: Path) -> None:
    board = create_board("Sprint 1", "client", str(tmp_path))
    assert board.name == "Sprint 1"
    assert board.scope == "client"
    assert len(board.columns) == 4
    assert [c.name for c in board.columns] == ["Backlog", "In Progress", "Review", "Done"]
    assert board.cards == []
    assert board.id.startswith("board-")


def test_create_board_custom_columns(tmp_path: Path) -> None:
    board = create_board("Kanban", "client", str(tmp_path), columns=["Todo", "Doing", "Done"])
    assert len(board.columns) == 3
    assert [c.name for c in board.columns] == ["Todo", "Doing", "Done"]


def test_list_boards_empty(tmp_path: Path) -> None:
    assert list_boards("client", str(tmp_path)) == []


def test_list_boards_returns_created(tmp_path: Path) -> None:
    create_board("A", "client", str(tmp_path))
    create_board("B", "client", str(tmp_path))
    boards = list_boards("client", str(tmp_path))
    assert len(boards) == 2
    names = {b.name for b in boards}
    assert names == {"A", "B"}


def test_get_board(tmp_path: Path) -> None:
    created = create_board("Test", "client", str(tmp_path))
    fetched = get_board(created.id, "client", str(tmp_path))
    assert fetched.id == created.id
    assert fetched.name == "Test"


def test_get_board_not_found(tmp_path: Path) -> None:
    with pytest.raises(ValueError, match="not found"):
        get_board("board-nonexistent", "client", str(tmp_path))


def test_delete_board(tmp_path: Path) -> None:
    board = create_board("To Delete", "client", str(tmp_path))
    delete_board(board.id, "client", str(tmp_path))
    assert list_boards("client", str(tmp_path)) == []


# ---------------------------------------------------------------------------
# Card CRUD
# ---------------------------------------------------------------------------


def test_add_card_defaults(tmp_path: Path) -> None:
    board = create_board("B", "client", str(tmp_path))
    updated = add_card(board.id, "Task 1", scope="client", client_path=str(tmp_path))
    assert len(updated.cards) == 1
    card = updated.cards[0]
    assert card.title == "Task 1"
    assert card.column_id == board.columns[0].id  # first column
    assert card.priority == "medium"
    assert card.status == "active"
    assert card.id.startswith("card-")


def test_add_card_to_specific_column(tmp_path: Path) -> None:
    board = create_board("B", "client", str(tmp_path))
    review_col = board.columns[2]  # "Review"
    updated = add_card(board.id, "Review Task", column_id=review_col.id, scope="client", client_path=str(tmp_path))
    assert updated.cards[0].column_id == review_col.id


def test_add_card_invalid_column(tmp_path: Path) -> None:
    board = create_board("B", "client", str(tmp_path))
    with pytest.raises(ValueError, match="Column not found"):
        add_card(board.id, "Bad", column_id="col-fake", scope="client", client_path=str(tmp_path))


def test_add_multiple_cards_position_increment(tmp_path: Path) -> None:
    board = create_board("B", "client", str(tmp_path))
    col_id = board.columns[0].id
    add_card(board.id, "First", scope="client", client_path=str(tmp_path))
    updated = add_card(board.id, "Second", scope="client", client_path=str(tmp_path))
    positions = [c.position for c in updated.cards if c.column_id == col_id]
    assert positions == [0, 1]


def test_update_card(tmp_path: Path) -> None:
    board = create_board("B", "client", str(tmp_path))
    board = add_card(board.id, "Original", scope="client", client_path=str(tmp_path))
    card_id = board.cards[0].id

    updated = update_card(board.id, card_id, scope="client", client_path=str(tmp_path), title="Renamed", priority="high")
    card = next(c for c in updated.cards if c.id == card_id)
    assert card.title == "Renamed"
    assert card.priority == "high"


def test_update_card_not_found(tmp_path: Path) -> None:
    board = create_board("B", "client", str(tmp_path))
    with pytest.raises(ValueError, match="Card not found"):
        update_card(board.id, "card-fake", scope="client", client_path=str(tmp_path), title="X")


def test_delete_card(tmp_path: Path) -> None:
    board = create_board("B", "client", str(tmp_path))
    board = add_card(board.id, "To Delete", scope="client", client_path=str(tmp_path))
    card_id = board.cards[0].id

    updated = delete_card(board.id, card_id, scope="client", client_path=str(tmp_path))
    assert len(updated.cards) == 0


# ---------------------------------------------------------------------------
# Card move
# ---------------------------------------------------------------------------


def test_move_card_between_columns(tmp_path: Path) -> None:
    board = create_board("B", "client", str(tmp_path))
    board = add_card(board.id, "Move Me", scope="client", client_path=str(tmp_path))
    card_id = board.cards[0].id
    target_col = board.columns[1].id  # "In Progress"

    updated = move_card(board.id, card_id, target_col, scope="client", client_path=str(tmp_path))
    card = next(c for c in updated.cards if c.id == card_id)
    assert card.column_id == target_col


def test_move_card_to_done_auto_completes(tmp_path: Path) -> None:
    board = create_board("B", "client", str(tmp_path))
    board = add_card(board.id, "Finish Me", scope="client", client_path=str(tmp_path))
    card_id = board.cards[0].id
    done_col = board.columns[-1].id  # "Done"

    updated = move_card(board.id, card_id, done_col, scope="client", client_path=str(tmp_path))
    card = next(c for c in updated.cards if c.id == card_id)
    assert card.completed is not None


def test_move_card_invalid_column(tmp_path: Path) -> None:
    board = create_board("B", "client", str(tmp_path))
    board = add_card(board.id, "Card", scope="client", client_path=str(tmp_path))
    with pytest.raises(ValueError, match="Column not found"):
        move_card(board.id, board.cards[0].id, "col-fake", scope="client", client_path=str(tmp_path))


# ---------------------------------------------------------------------------
# Column management
# ---------------------------------------------------------------------------


def test_add_column(tmp_path: Path) -> None:
    board = create_board("B", "client", str(tmp_path))
    updated = add_column(board.id, "QA", "#9333ea", scope="client", client_path=str(tmp_path))
    assert len(updated.columns) == 5
    assert updated.columns[-1].name == "QA"
    assert updated.columns[-1].color == "#9333ea"


def test_remove_column_moves_cards(tmp_path: Path) -> None:
    board = create_board("B", "client", str(tmp_path))
    second_col = board.columns[1].id
    board = add_card(board.id, "Orphan", column_id=second_col, scope="client", client_path=str(tmp_path))

    updated = remove_column(board.id, second_col, scope="client", client_path=str(tmp_path))
    assert len(updated.columns) == 3
    # Card should have moved to first remaining column
    card = updated.cards[0]
    assert card.column_id == updated.columns[0].id


def test_remove_last_column_raises(tmp_path: Path) -> None:
    board = create_board("B", "client", str(tmp_path), columns=["Only"])
    with pytest.raises(ValueError, match="last column"):
        remove_column(board.id, board.columns[0].id, scope="client", client_path=str(tmp_path))


def test_reorder_columns(tmp_path: Path) -> None:
    board = create_board("B", "client", str(tmp_path))
    original_ids = [c.id for c in board.columns]
    reversed_ids = list(reversed(original_ids))

    updated = reorder_columns(board.id, reversed_ids, scope="client", client_path=str(tmp_path))
    assert [c.id for c in updated.columns] == reversed_ids


# ---------------------------------------------------------------------------
# Scope isolation
# ---------------------------------------------------------------------------


def test_global_and_client_boards_isolated(tmp_path: Path) -> None:
    home_boards = tmp_path / "home"
    home_boards.mkdir()
    client_path = tmp_path / "client"
    client_path.mkdir()

    # Monkey-patch home for global scope
    import sidecar.services.board_service as svc
    original = svc._boards_dir

    def patched_boards_dir(scope, cp=None):
        if scope == "global":
            return home_boards / ".tentaqles" / "boards"
        return original(scope, cp)

    svc._boards_dir = patched_boards_dir
    try:
        create_board("Global Board", "global")
        create_board("Client Board", "client", str(client_path))

        global_boards = list_boards("global")
        client_boards = list_boards("client", str(client_path))

        assert len(global_boards) == 1
        assert global_boards[0].name == "Global Board"
        assert len(client_boards) == 1
        assert client_boards[0].name == "Client Board"
    finally:
        svc._boards_dir = original
