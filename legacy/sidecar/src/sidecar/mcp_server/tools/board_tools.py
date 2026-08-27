"""MCP tools for Vibe Kanban board management."""

from __future__ import annotations

from pathlib import Path

from mcp.server.fastmcp import FastMCP

from sidecar.services.board_service import add_card as svc_add_card
from sidecar.services.board_service import create_board, get_board, list_boards
from sidecar.services.board_service import move_card as svc_move_card
from sidecar.services.board_service import update_card as svc_update_card
from sidecar.services.card_executor import execute_card


def register_board_tools(mcp: FastMCP, get_workspace_path: callable) -> None:
    """Register board management tools with the MCP server."""

    def _find_client_path(workspace_path: str) -> str | None:
        current = Path(workspace_path)
        for parent in [current] + list(current.parents):
            if (parent / ".workspace-profile.json").exists():
                return str(parent)
            if (parent / ".tentaqles.json").exists():
                return str(parent)
        return None

    @mcp.tool()
    async def board_list() -> str:
        """List all kanban boards for the current workspace.

        Returns board names, IDs, card counts, and column names.
        """
        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)
        boards = list_boards("client", client_path)
        if not boards:
            return "No boards found. Create one with board_create."
        lines = []
        for b in boards:
            active_cards = sum(1 for c in b.cards if c.status == "active")
            cols = ", ".join(c.name for c in b.columns)
            lines.append(f"- **{b.name}** (id: {b.id}) — {active_cards} cards | Columns: {cols}")
        return "\n".join(lines)

    @mcp.tool()
    async def board_get(board_name: str) -> str:
        """Get full board view by name. Shows all columns and cards.

        Args:
            board_name: Name of the board to view.
        """
        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)
        boards = list_boards("client", client_path)
        match = next((b for b in boards if b.name.lower() == board_name.lower()), None)
        if not match:
            return f"Board '{board_name}' not found. Available: {', '.join(b.name for b in boards)}"
        board = get_board(match.id, "client", client_path)

        lines = [f"# {board.name}\n"]
        for col in board.columns:
            cards = sorted(
                [c for c in board.cards if c.column_id == col.id and c.status == "active"],
                key=lambda c: c.position,
            )
            lines.append(f"## {col.name} ({len(cards)})")
            for card in cards:
                priority_icon = {"urgent": "!!!", "high": "!!", "medium": "!", "low": ""}[card.priority]
                labels = f" [{', '.join(card.labels)}]" if card.labels else ""
                assignee = f" @{card.assignee}" if card.assignee else ""
                lines.append(f"  - {priority_icon} {card.title}{labels}{assignee}")
            if not cards:
                lines.append("  (empty)")
            lines.append("")
        return "\n".join(lines)

    @mcp.tool()
    async def board_add_card(
        board_name: str,
        title: str,
        description: str = "",
        column: str | None = None,
        priority: str = "medium",
    ) -> str:
        """Add a card to a kanban board.

        Args:
            board_name: Name of the board.
            title: Card title.
            description: Card description (markdown).
            column: Target column name (default: first column).
            priority: Card priority — low, medium, high, or urgent.
        """
        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)
        boards = list_boards("client", client_path)
        match = next((b for b in boards if b.name.lower() == board_name.lower()), None)
        if not match:
            return f"Board '{board_name}' not found."

        col_id = None
        if column:
            col = next((c for c in match.columns if c.name.lower() == column.lower()), None)
            if not col:
                return f"Column '{column}' not found. Available: {', '.join(c.name for c in match.columns)}"
            col_id = col.id

        svc_add_card(match.id, title, description, col_id, priority, scope="client", client_path=client_path)
        return f"Added card '{title}' to {board_name}"

    @mcp.tool()
    async def board_move_card(board_name: str, card_title: str, target_column: str) -> str:
        """Move a card to a different column.

        Args:
            board_name: Name of the board.
            card_title: Title of the card to move (partial match supported).
            target_column: Name of the target column.
        """
        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)
        boards = list_boards("client", client_path)
        match = next((b for b in boards if b.name.lower() == board_name.lower()), None)
        if not match:
            return f"Board '{board_name}' not found."

        board = get_board(match.id, "client", client_path)
        card = next((c for c in board.cards if card_title.lower() in c.title.lower()), None)
        if not card:
            return f"Card matching '{card_title}' not found."

        col = next((c for c in board.columns if c.name.lower() == target_column.lower()), None)
        if not col:
            return f"Column '{target_column}' not found."

        svc_move_card(board.id, card.id, col.id, scope="client", client_path=client_path)
        return f"Moved '{card.title}' to {col.name}"

    @mcp.tool()
    async def board_complete_card(board_name: str, card_title: str) -> str:
        """Mark a card as complete by moving it to the last column (Done).

        Args:
            board_name: Name of the board.
            card_title: Title of the card to complete (partial match supported).
        """
        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)
        boards = list_boards("client", client_path)
        match = next((b for b in boards if b.name.lower() == board_name.lower()), None)
        if not match:
            return f"Board '{board_name}' not found."

        board = get_board(match.id, "client", client_path)
        card = next((c for c in board.cards if card_title.lower() in c.title.lower()), None)
        if not card:
            return f"Card matching '{card_title}' not found."

        done_col = board.columns[-1]
        svc_move_card(board.id, card.id, done_col.id, scope="client", client_path=client_path)
        return f"Completed '{card.title}' (moved to {done_col.name})"

    @mcp.tool()
    async def board_create(board_name: str) -> str:
        """Create a new kanban board with default columns (Backlog, In Progress, Review, Done).

        Args:
            board_name: Name for the new board.
        """
        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)
        board = create_board(board_name, "client", client_path)
        return f"Created board '{board.name}' (id: {board.id})"

    @mcp.tool()
    async def board_delegate_card(
        board_name: str,
        card_title: str,
        model: str = "sonnet",
    ) -> str:
        """Delegate a card to Claude Code. Assigns the card to 'claude', moves it to
        In Progress, and fires a Claude Code session with --dangerously-skip-permissions
        to execute the task autonomously. The card auto-moves to Review when done.

        Args:
            board_name: Name of the board.
            card_title: Title of the card to delegate (partial match supported).
            model: Claude model to use (default: sonnet).
        """
        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)
        boards = list_boards("client", client_path)
        match = next((b for b in boards if b.name.lower() == board_name.lower()), None)
        if not match:
            return f"Board '{board_name}' not found."

        board = get_board(match.id, "client", client_path)
        card = next((c for c in board.cards if card_title.lower() in c.title.lower()), None)
        if not card:
            return f"Card matching '{card_title}' not found."

        # Assign to claude and move to In Progress
        svc_update_card(board.id, card.id, scope="client", client_path=client_path, assignee="claude")
        in_progress = board.columns[1] if len(board.columns) > 1 else board.columns[0]
        svc_move_card(board.id, card.id, in_progress.id, scope="client", client_path=client_path)

        # Build prompt and fire execution
        from sidecar.services.card_executor import _build_prompt

        prompt = _build_prompt(card.title, card.description)
        msg = execute_card(board.id, card.id, "client", client_path, ws_path, prompt, model)

        return f"Delegated '{card.title}' to Claude ({model}). {msg}"
