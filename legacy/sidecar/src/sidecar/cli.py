"""Tentaqles CLI — single entry point for serve, activate, status, and board management."""

from __future__ import annotations

import argparse
import os
import platform
import webbrowser

# WMI deadlock workaround (same as main.py)
platform.system = lambda: "Windows"
platform.uname = lambda: platform.uname_result(
    system="Windows", node="", release="10", version="10.0.26200", machine="AMD64"
)

for _k in list(os.environ):
    if _k.startswith("CLAUDE"):
        del os.environ[_k]
os.environ.pop("VIRTUAL_ENV", None)


def cmd_serve(args: argparse.Namespace) -> None:
    """Start the Tentaqles server (API + optional static UI)."""
    import uvicorn

    from sidecar.main import app
    from sidecar.services.static_files import mount_static_files

    mount_static_files(app)

    if args.open:
        import threading

        def _open():
            import time

            time.sleep(1.5)
            webbrowser.open(f"http://127.0.0.1:{args.port}")

        threading.Thread(target=_open, daemon=True).start()

    print(f"Tentaqles server starting on http://127.0.0.1:{args.port}")
    uvicorn.run(app, host=args.host, port=args.port, log_level="info")


def cmd_activate(args: argparse.Namespace) -> None:
    """Activate a workspace."""

    from sidecar.services.activation import activate_workspace

    ws_path = args.workspace or os.getcwd()
    result = activate_workspace(ws_path)
    print(f"Activated: {result.client_name} ({result.workspace_path})")
    if result.warnings:
        for w in result.warnings:
            print(f"  Warning: {w}")


def cmd_status(args: argparse.Namespace) -> None:
    """Show current workspace status."""

    from sidecar.services.activation import get_active_workspace

    active = get_active_workspace()
    if active:
        print(f"Active workspace: {active.client_name}")
        print(f"  Path: {active.workspace_path}")
        print(f"  Since: {active.activated_at}")
    else:
        print("No workspace activated.")


def cmd_board_list(args: argparse.Namespace) -> None:
    """List boards."""
    from sidecar.services.board_service import list_boards

    scope = args.scope or "client"
    client_path = args.client or os.getcwd()
    boards = list_boards(scope, client_path)
    if not boards:
        print("No boards found.")
        return
    for b in boards:
        active = sum(1 for c in b.cards if c.status == "active")
        print(f"  {b.name} ({b.id}) — {active} cards")


def cmd_board_create(args: argparse.Namespace) -> None:
    """Create a board."""
    from sidecar.services.board_service import create_board

    client_path = args.client or os.getcwd()
    board = create_board(args.name, args.scope, client_path)
    print(f"Created board: {board.name} ({board.id})")


def cmd_board_show(args: argparse.Namespace) -> None:
    """Show board as ASCII table."""
    from sidecar.services.board_service import get_board, list_boards

    client_path = args.client or os.getcwd()
    boards = list_boards(args.scope, client_path)
    match = next((b for b in boards if b.name.lower() == args.name.lower()), None)
    if not match:
        print(f"Board '{args.name}' not found.")
        return

    board = get_board(match.id, args.scope, client_path)

    # Build ASCII kanban
    col_cards: dict[str, list[str]] = {}
    col_names: list[str] = []
    for col in board.columns:
        col_names.append(col.name)
        cards = sorted(
            [c for c in board.cards if c.column_id == col.id and c.status == "active"],
            key=lambda c: c.position,
        )
        col_cards[col.name] = [c.title for c in cards]

    # Calculate column widths
    col_width = max(20, *(len(n) + 4 for n in col_names))
    max_rows = max((len(v) for v in col_cards.values()), default=0)

    # Header
    header = " | ".join(f"{n:^{col_width}}" for n in col_names)
    sep = "-+-".join("-" * col_width for _ in col_names)
    print(header)
    print(sep)

    # Rows
    for i in range(max_rows):
        cells = []
        for name in col_names:
            cards = col_cards[name]
            if i < len(cards):
                title = cards[i]
                if len(title) > col_width - 2:
                    title = title[: col_width - 5] + "..."
                cells.append(f" {title:<{col_width - 1}}")
            else:
                cells.append(" " * col_width)
        print(" | ".join(cells))


def cmd_board_add_card(args: argparse.Namespace) -> None:
    """Add a card to a board."""
    from sidecar.services.board_service import add_card, list_boards

    client_path = args.client or os.getcwd()
    boards = list_boards(args.scope, client_path)
    match = next((b for b in boards if b.name.lower() == args.board.lower()), None)
    if not match:
        print(f"Board '{args.board}' not found.")
        return

    col_id = None
    if args.column:
        col = next((c for c in match.columns if c.name.lower() == args.column.lower()), None)
        if col:
            col_id = col.id

    add_card(
        match.id,
        args.title,
        column_id=col_id,
        priority=args.priority,
        scope=args.scope,
        client_path=client_path,
    )
    print(f"Added: {args.title}")


def cmd_board_move_card(args: argparse.Namespace) -> None:
    """Move a card to a different column."""
    from sidecar.services.board_service import get_board, list_boards, move_card

    client_path = args.client or os.getcwd()
    boards = list_boards(args.scope, client_path)
    match = next((b for b in boards if b.name.lower() == args.board.lower()), None)
    if not match:
        print(f"Board '{args.board}' not found.")
        return

    board = get_board(match.id, args.scope, client_path)
    card = next((c for c in board.cards if args.card.lower() in c.title.lower()), None)
    if not card:
        print(f"Card matching '{args.card}' not found.")
        return

    col = next((c for c in board.columns if c.name.lower() == args.to.lower()), None)
    if not col:
        print(f"Column '{args.to}' not found.")
        return

    move_card(board.id, card.id, col.id, scope=args.scope, client_path=client_path)
    print(f"Moved '{card.title}' -> {col.name}")


def main() -> None:
    parser = argparse.ArgumentParser(
        prog="tentaqles",
        description="Tentaqles — multi-client workspace management for Claude Code",
    )
    sub = parser.add_subparsers(dest="command")

    # serve
    p_serve = sub.add_parser("serve", help="Start the Tentaqles server")
    p_serve.add_argument("--port", type=int, default=9160)
    p_serve.add_argument("--host", default="127.0.0.1")
    p_serve.add_argument("--open", action="store_true", help="Open browser automatically")

    # activate
    p_activate = sub.add_parser("activate", help="Activate a workspace")
    p_activate.add_argument("workspace", nargs="?", help="Workspace path (default: cwd)")

    # status
    sub.add_parser("status", help="Show active workspace status")

    # board
    p_board = sub.add_parser("board", help="Kanban board management")
    board_sub = p_board.add_subparsers(dest="board_command")

    # board list
    p_bl = board_sub.add_parser("list", help="List boards")
    p_bl.add_argument("--scope", default="client", choices=["global", "client"])
    p_bl.add_argument("--client", help="Client path (default: cwd)")

    # board create
    p_bc = board_sub.add_parser("create", help="Create a board")
    p_bc.add_argument("name", help="Board name")
    p_bc.add_argument("--scope", default="client", choices=["global", "client"])
    p_bc.add_argument("--client", help="Client path (default: cwd)")

    # board show
    p_bs = board_sub.add_parser("show", help="Show board as ASCII kanban")
    p_bs.add_argument("name", help="Board name")
    p_bs.add_argument("--scope", default="client", choices=["global", "client"])
    p_bs.add_argument("--client", help="Client path (default: cwd)")

    # board add-card
    p_ba = board_sub.add_parser("add-card", help="Add a card")
    p_ba.add_argument("board", help="Board name")
    p_ba.add_argument("title", help="Card title")
    p_ba.add_argument("--priority", default="medium", choices=["low", "medium", "high", "urgent"])
    p_ba.add_argument("--column", help="Target column name")
    p_ba.add_argument("--scope", default="client", choices=["global", "client"])
    p_ba.add_argument("--client", help="Client path (default: cwd)")

    # board move-card
    p_bm = board_sub.add_parser("move-card", help="Move a card")
    p_bm.add_argument("board", help="Board name")
    p_bm.add_argument("card", help="Card title (partial match)")
    p_bm.add_argument("--to", required=True, help="Target column name")
    p_bm.add_argument("--scope", default="client", choices=["global", "client"])
    p_bm.add_argument("--client", help="Client path (default: cwd)")

    args = parser.parse_args()

    dispatch = {
        "serve": cmd_serve,
        "activate": cmd_activate,
        "status": cmd_status,
    }

    if args.command == "board":
        board_dispatch = {
            "list": cmd_board_list,
            "create": cmd_board_create,
            "show": cmd_board_show,
            "add-card": cmd_board_add_card,
            "move-card": cmd_board_move_card,
        }
        fn = board_dispatch.get(args.board_command)
        if fn:
            fn(args)
        else:
            p_board.print_help()
    elif args.command in dispatch:
        dispatch[args.command](args)
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
