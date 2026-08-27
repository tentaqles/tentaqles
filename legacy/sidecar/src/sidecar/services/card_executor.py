"""Card executor — spawns Claude Code sessions for delegated cards."""

from __future__ import annotations

import asyncio
import json
import logging
import time
from datetime import datetime

from sidecar.models import CardExecution, ExecutionState
from sidecar.services.board_service import get_board, move_card, update_card

logger = logging.getLogger(__name__)

# Track running executions: card_id -> asyncio.Task
_running: dict[str, asyncio.Task] = {}


def _build_prompt(title: str, description: str) -> str:
    """Build a Claude prompt from card title and description."""
    parts = [f"Task: {title}"]
    if description:
        parts.append(f"\nDetails:\n{description}")
    parts.append("\nComplete this task. Commit your changes when done.")
    return "\n".join(parts)


async def _execute_card(
    board_id: str,
    card_id: str,
    scope: str,
    client_path: str | None,
    workspace_path: str,
    prompt: str,
    model: str = "sonnet",
) -> None:
    """Run Claude Code for a card and update the board on completion."""
    start = time.monotonic()
    started_at = datetime.now().isoformat(timespec="seconds")

    # Mark card as running
    update_card(
        board_id,
        card_id,
        scope=scope,
        client_path=client_path,
        execution=CardExecution(state=ExecutionState.RUNNING, started_at=started_at).model_dump(),
    )

    try:
        cmd = [
            "claude",
            "-p",
            "--dangerously-skip-permissions",
            "--output-format", "json",
            "--model", model,
            prompt,
        ]

        logger.info("Executing card %s: claude -p in %s", card_id, workspace_path)

        proc = await asyncio.create_subprocess_exec(
            *cmd,
            cwd=workspace_path,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=600)  # 10 min timeout

        duration = (time.monotonic() - start) * 1000
        finished_at = datetime.now().isoformat(timespec="seconds")

        output_text = stdout.decode("utf-8", errors="replace")[:20000] if stdout else ""
        error_text = stderr.decode("utf-8", errors="replace")[:5000] if stderr else None

        # Parse JSON output for token/cost info
        cost_usd = 0.0
        input_tokens = 0
        output_tokens = 0
        session_id = None
        result_text = output_text

        try:
            data = json.loads(output_text)
            if isinstance(data, dict):
                cost_usd = data.get("cost_usd", 0.0)
                input_tokens = data.get("input_tokens", 0)
                output_tokens = data.get("output_tokens", 0)
                session_id = data.get("session_id")
                if "result" in data:
                    result_text = data["result"]
        except (json.JSONDecodeError, ValueError):
            pass

        state = ExecutionState.SUCCESS if proc.returncode == 0 else ExecutionState.ERROR
        execution = CardExecution(
            state=state,
            started_at=started_at,
            finished_at=finished_at,
            output=result_text,
            error=error_text if state == ExecutionState.ERROR else None,
            duration_ms=duration,
            cost_usd=cost_usd,
            input_tokens=input_tokens,
            output_tokens=output_tokens,
            session_id=session_id,
        )

    except TimeoutError:
        duration = (time.monotonic() - start) * 1000
        execution = CardExecution(
            state=ExecutionState.ERROR,
            started_at=started_at,
            finished_at=datetime.now().isoformat(timespec="seconds"),
            error="Execution timed out after 10 minutes",
            duration_ms=duration,
        )
    except Exception as e:
        duration = (time.monotonic() - start) * 1000
        execution = CardExecution(
            state=ExecutionState.ERROR,
            started_at=started_at,
            finished_at=datetime.now().isoformat(timespec="seconds"),
            error=str(e),
            duration_ms=duration,
        )

    # Update card with execution result
    update_card(
        board_id,
        card_id,
        scope=scope,
        client_path=client_path,
        execution=execution.model_dump(),
    )

    # Auto-move to Review on success, stay in place on error
    if execution.state == ExecutionState.SUCCESS:
        board = get_board(board_id, scope, client_path)
        # Find the "Review" column (3rd column by convention) or second-to-last
        review_col = None
        if len(board.columns) >= 3:
            review_col = board.columns[2]  # Review
        elif len(board.columns) >= 2:
            review_col = board.columns[-1]  # fallback to last

        if review_col:
            move_card(board_id, card_id, review_col.id, scope=scope, client_path=client_path)
            logger.info("Card %s completed, moved to %s", card_id, review_col.name)

    # Clean up tracking
    _running.pop(card_id, None)
    logger.info("Card %s execution finished: %s (%.0fms)", card_id, execution.state, execution.duration_ms)


def execute_card(
    board_id: str,
    card_id: str,
    scope: str,
    client_path: str | None,
    workspace_path: str,
    prompt: str,
    model: str = "sonnet",
) -> str:
    """Fire a Claude Code session for a card. Returns immediately."""
    if card_id in _running:
        return f"Card {card_id} is already executing"

    task = asyncio.create_task(
        _execute_card(board_id, card_id, scope, client_path, workspace_path, prompt, model)
    )
    _running[card_id] = task
    return f"Execution started for card {card_id}"


def get_execution_status(card_id: str) -> str:
    """Check if a card is currently executing."""
    if card_id in _running:
        task = _running[card_id]
        return "running" if not task.done() else "finished"
    return "idle"


def cancel_execution(card_id: str) -> bool:
    """Cancel a running card execution."""
    if card_id in _running:
        task = _running[card_id]
        if not task.done():
            task.cancel()
            _running.pop(card_id, None)
            return True
    return False
