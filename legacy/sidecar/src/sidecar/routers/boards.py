"""Boards router — Vibe Kanban board management."""

from fastapi import APIRouter

from sidecar.schemas import (
    BoardCreateRequest,
    BoardDeleteRequest,
    BoardGetRequest,
    BoardListRequest,
    CardCancelRequest,
    CardCommentCreateRequest,
    CardCommentDeleteRequest,
    CardCommentReactionRequest,
    CardCommentUpdateRequest,
    CardCreateRequest,
    CardDelegateRequest,
    CardDeleteRequest,
    CardMoveRequest,
    CardUpdateRequest,
    ColumnAddRequest,
    ColumnRemoveRequest,
    ColumnReorderRequest,
    ColumnUpdateRequest,
    RelationshipAddRequest,
    RelationshipRemoveRequest,
    RemoveParentCardRequest,
    ReorderSubCardsRequest,
    SetParentCardRequest,
    SubCardCreateRequest,
)
from sidecar.services.board_service import (
    add_card,
    add_card_comment,
    add_column,
    add_relationship,
    add_sub_card,
    create_board,
    delete_board,
    delete_card,
    delete_card_comment,
    get_board,
    list_boards,
    move_card,
    remove_column,
    remove_parent_card,
    remove_relationship,
    reorder_columns,
    reorder_sub_cards,
    set_parent_card,
    toggle_comment_reaction,
    update_card,
    update_card_comment,
    update_column,
)
from sidecar.services.card_executor import cancel_execution, execute_card

router = APIRouter()


@router.post("/list")
def boards_list(req: BoardListRequest):
    boards = list_boards(req.scope, req.client_path)
    return [b.model_dump() for b in boards]


@router.post("/get")
def boards_get(req: BoardGetRequest):
    board = get_board(req.board_id, req.scope, req.client_path)
    return board.model_dump()


@router.post("/create")
def boards_create(req: BoardCreateRequest):
    board = create_board(req.name, req.scope, req.client_path, req.columns)
    return board.model_dump()


@router.post("/delete")
def boards_delete(req: BoardDeleteRequest):
    delete_board(req.board_id, req.scope, req.client_path)
    return {"ok": True}


@router.post("/add-card")
def boards_add_card(req: CardCreateRequest):
    board = add_card(
        req.board_id,
        req.title,
        req.description,
        req.column_id,
        req.priority,
        req.labels,
        req.assignee,
        req.due_date,
        req.project_path,
        req.scope,
        req.client_path,
    )
    return board.model_dump()


@router.post("/update-card")
def boards_update_card(req: CardUpdateRequest):
    exclude = {"board_id", "card_id", "scope", "client_path"}
    kwargs = {k: v for k, v in req.model_dump().items() if k not in exclude and v is not None}
    board = update_card(req.board_id, req.card_id, req.scope, req.client_path, **kwargs)
    return board.model_dump()


@router.post("/move-card")
def boards_move_card(req: CardMoveRequest):
    board = move_card(req.board_id, req.card_id, req.target_column_id, req.position, req.scope, req.client_path)
    return board.model_dump()


@router.post("/delete-card")
def boards_delete_card(req: CardDeleteRequest):
    board = delete_card(req.board_id, req.card_id, req.scope, req.client_path)
    return board.model_dump()


@router.post("/add-column")
def boards_add_column(req: ColumnAddRequest):
    board = add_column(req.board_id, req.name, req.color, req.scope, req.client_path)
    return board.model_dump()


@router.post("/remove-column")
def boards_remove_column(req: ColumnRemoveRequest):
    board = remove_column(req.board_id, req.column_id, req.scope, req.client_path)
    return board.model_dump()


@router.post("/reorder-columns")
def boards_reorder_columns(req: ColumnReorderRequest):
    board = reorder_columns(req.board_id, req.column_ids, req.scope, req.client_path)
    return board.model_dump()


@router.post("/delegate-card")
async def boards_delegate_card(req: CardDelegateRequest):
    """Delegate a card to Claude Code — assigns to 'claude', moves to In Progress, fires execution."""
    board = get_board(req.board_id, req.scope, req.client_path)
    card = next((c for c in board.cards if c.id == req.card_id), None)
    if not card:
        return {"ok": False, "error": "Card not found"}

    # Move to In Progress (second column)
    in_progress_col = board.columns[1] if len(board.columns) > 1 else board.columns[0]
    update_card(req.board_id, req.card_id, req.scope, req.client_path, assignee="claude")
    move_card(req.board_id, req.card_id, in_progress_col.id, scope=req.scope, client_path=req.client_path)

    # Build prompt from card content
    from sidecar.services.card_executor import _build_prompt
    prompt = _build_prompt(card.title, card.description)

    # Fire execution
    execute_card(
        req.board_id, req.card_id, req.scope, req.client_path,
        req.workspace_path, prompt, req.model,
    )

    board = get_board(req.board_id, req.scope, req.client_path)
    return board.model_dump()


@router.post("/cancel-execution")
async def boards_cancel_execution(req: CardCancelRequest):
    """Cancel a running card execution."""
    cancelled = cancel_execution(req.card_id)
    board = get_board(req.board_id, req.scope, req.client_path)
    return {"ok": cancelled, "board": board.model_dump()}


# --- Comments ---


@router.post("/add-comment")
def boards_add_comment(req: CardCommentCreateRequest):
    board = add_card_comment(req.board_id, req.card_id, req.author, req.text, req.parent_id, req.scope, req.client_path)
    return board.model_dump()


@router.post("/update-comment")
def boards_update_comment(req: CardCommentUpdateRequest):
    board = update_card_comment(req.board_id, req.card_id, req.comment_id, req.text, req.scope, req.client_path)
    return board.model_dump()


@router.post("/delete-comment")
def boards_delete_comment(req: CardCommentDeleteRequest):
    board = delete_card_comment(req.board_id, req.card_id, req.comment_id, req.scope, req.client_path)
    return board.model_dump()


@router.post("/toggle-reaction")
def boards_toggle_reaction(req: CardCommentReactionRequest):
    board = toggle_comment_reaction(
        req.board_id, req.card_id, req.comment_id, req.emoji, req.author, req.scope, req.client_path,
    )
    return board.model_dump()


# --- Sub-cards ---


@router.post("/add-sub-card")
def boards_add_sub_card(req: SubCardCreateRequest):
    board = add_sub_card(
        req.board_id, req.parent_card_id, req.title, req.priority, req.assignee, req.scope, req.client_path,
    )
    return board.model_dump()


@router.post("/set-parent-card")
def boards_set_parent(req: SetParentCardRequest):
    board = set_parent_card(req.board_id, req.card_id, req.parent_card_id, req.scope, req.client_path)
    return board.model_dump()


@router.post("/remove-parent-card")
def boards_remove_parent(req: RemoveParentCardRequest):
    board = remove_parent_card(req.board_id, req.card_id, req.scope, req.client_path)
    return board.model_dump()


@router.post("/reorder-sub-cards")
def boards_reorder_sub_cards(req: ReorderSubCardsRequest):
    board = reorder_sub_cards(req.board_id, req.parent_card_id, req.card_ids, req.scope, req.client_path)
    return board.model_dump()


# --- Relationships ---


@router.post("/add-relationship")
def boards_add_relationship(req: RelationshipAddRequest):
    board = add_relationship(req.board_id, req.source_card_id, req.target_card_id, req.type, req.scope, req.client_path)
    return board.model_dump()


@router.post("/remove-relationship")
def boards_remove_relationship(req: RelationshipRemoveRequest):
    board = remove_relationship(req.board_id, req.relationship_id, req.scope, req.client_path)
    return board.model_dump()


# --- Column update ---


@router.post("/update-column")
def boards_update_column(req: ColumnUpdateRequest):
    exclude = {"board_id", "column_id", "scope", "client_path"}
    kwargs = {k: v for k, v in req.model_dump().items() if k not in exclude and v is not None}
    board = update_column(req.board_id, req.column_id, req.scope, req.client_path, **kwargs)
    return board.model_dump()
