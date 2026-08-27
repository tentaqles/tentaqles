"""Claude CLI subprocess service — manages claude sessions via --output-format stream-json."""

from __future__ import annotations

import asyncio
import json
import os
import subprocess
import time
from dataclasses import dataclass, field
from enum import Enum
from pathlib import Path
from typing import Any, Callable


# ---------------------------------------------------------------------------
# Data models
# ---------------------------------------------------------------------------


class MessageRole(str, Enum):
    USER = "user"
    ASSISTANT = "assistant"
    SYSTEM = "system"


class ContentBlockType(str, Enum):
    TEXT = "text"
    TOOL_USE = "tool_use"
    TOOL_RESULT = "tool_result"
    THINKING = "thinking"


@dataclass
class ContentBlock:
    """A single content block within a message (text, tool_use, tool_result)."""

    type: ContentBlockType
    text: str = ""
    # tool_use fields
    tool_name: str = ""
    tool_id: str = ""
    tool_input: dict = field(default_factory=dict)
    # tool_result fields
    tool_result: str = ""
    is_error: bool = False


@dataclass
class ChatMessage:
    """A single chat message (user or assistant)."""

    role: MessageRole
    content: list[ContentBlock] = field(default_factory=list)
    model: str = ""
    timestamp: float = 0.0
    cost_usd: float = 0.0
    duration_ms: float = 0.0
    input_tokens: int = 0
    output_tokens: int = 0
    cache_read_tokens: int = 0
    cache_creation_tokens: int = 0
    session_id: str = ""

    @property
    def text(self) -> str:
        """Get concatenated text from all text content blocks."""
        return "".join(b.text for b in self.content if b.type == ContentBlockType.TEXT)

    @property
    def tool_uses(self) -> list[ContentBlock]:
        """Get all tool_use blocks."""
        return [b for b in self.content if b.type == ContentBlockType.TOOL_USE]

    @property
    def tool_results(self) -> list[ContentBlock]:
        """Get all tool_result blocks."""
        return [b for b in self.content if b.type == ContentBlockType.TOOL_RESULT]


@dataclass
class SessionInfo:
    """Info about a Claude session."""

    session_id: str = ""
    model: str = ""
    cwd: str = ""
    tools: list[str] = field(default_factory=list)
    mcp_servers: list[str] = field(default_factory=list)


# ---------------------------------------------------------------------------
# Stream event parser
# ---------------------------------------------------------------------------


def parse_stream_line(line: str) -> dict[str, Any] | None:
    """Parse a single NDJSON line from claude --output-format stream-json.

    Returns the parsed dict or None if the line is empty/invalid.
    """
    line = line.strip()
    if not line:
        return None
    try:
        return json.loads(line)
    except json.JSONDecodeError:
        return None


# ---------------------------------------------------------------------------
# Claude session (subprocess)
# ---------------------------------------------------------------------------


class ClaudeSession:
    """Manages a single Claude CLI subprocess with streaming JSON output."""

    def __init__(
        self,
        workspace_path: str,
        model: str = "sonnet",
        resume_session_id: str | None = None,
        permission_mode: str = "default",
    ):
        self.workspace_path = workspace_path
        self.model = model
        self.resume_session_id = resume_session_id
        self.permission_mode = permission_mode

        self._process: subprocess.Popen | None = None
        self._session_info: SessionInfo = SessionInfo()
        self._current_message: ChatMessage | None = None
        self._current_block_index: int = -1
        self._messages: list[ChatMessage] = []
        self._is_streaming = False
        self._stop_requested = False

    @property
    def session_id(self) -> str:
        return self._session_info.session_id

    @property
    def messages(self) -> list[ChatMessage]:
        return list(self._messages)

    @property
    def is_streaming(self) -> bool:
        return self._is_streaming

    @property
    def session_info(self) -> SessionInfo:
        return self._session_info

    def _build_cli_args(self, prompt: str) -> list[str]:
        """Build the claude CLI command arguments."""
        args = ["claude", "-p", "--output-format", "stream-json", "--verbose"]

        if self.model:
            args.extend(["--model", self.model])

        if self.resume_session_id:
            args.extend(["--resume", self.resume_session_id])

        # Permission mode flags
        if self.permission_mode == "bypass":
            args.append("--dangerously-skip-permissions")
        elif self.permission_mode == "auto-edit":
            args.extend(["--allowedTools", "Edit,Write,MultiEdit,Read,Glob,Grep"])
        elif self.permission_mode == "plan":
            args.extend(["--allowedTools", "Read,Glob,Grep"])

        # Prompt goes as the last positional argument
        args.append(prompt)
        return args

    def _clean_env(self) -> dict[str, str]:
        """Build a clean environment without CLAUDE* vars to avoid nested session errors."""
        return {k: v for k, v in os.environ.items() if not k.startswith("CLAUDE")}

    async def send_message(
        self,
        prompt: str,
        on_text_delta: Callable[[str], Any] | None = None,
        on_tool_use: Callable[[ContentBlock], Any] | None = None,
        on_tool_result: Callable[[ContentBlock], Any] | None = None,
        on_message_complete: Callable[[ChatMessage], Any] | None = None,
        on_system_init: Callable[[SessionInfo], Any] | None = None,
        on_error: Callable[[str], Any] | None = None,
    ) -> ChatMessage | None:
        """Send a message to claude CLI and stream the response.

        Returns the completed assistant ChatMessage, or None on error.
        """
        # Add user message to history
        user_msg = ChatMessage(
            role=MessageRole.USER,
            content=[ContentBlock(type=ContentBlockType.TEXT, text=prompt)],
            timestamp=time.time(),
        )
        self._messages.append(user_msg)

        # Build command
        args = self._build_cli_args(prompt)

        self._is_streaming = True
        self._stop_requested = False

        # Start assistant message
        self._current_message = ChatMessage(
            role=MessageRole.ASSISTANT,
            timestamp=time.time(),
        )
        self._current_block_index = -1

        try:
            # Start subprocess
            self._process = await asyncio.create_subprocess_exec(
                *args,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                cwd=self.workspace_path,
                env=self._clean_env(),
            )

            # Read NDJSON lines from stdout
            assert self._process.stdout is not None
            while True:
                if self._stop_requested:
                    self._process.terminate()
                    break

                line_bytes = await self._process.stdout.readline()
                if not line_bytes:
                    break

                line = line_bytes.decode("utf-8", errors="replace")
                event = parse_stream_line(line)
                if event is None:
                    continue

                await self._handle_event(
                    event,
                    on_text_delta=on_text_delta,
                    on_tool_use=on_tool_use,
                    on_tool_result=on_tool_result,
                    on_message_complete=on_message_complete,
                    on_system_init=on_system_init,
                )

            # Wait for process to finish
            await self._process.wait()

            # Check stderr for errors
            if self._process.stderr:
                stderr = await self._process.stderr.read()
                stderr_text = stderr.decode("utf-8", errors="replace").strip()
                if self._process.returncode != 0:
                    error_msg = stderr_text or f"Claude CLI exited with code {self._process.returncode}"
                    if on_error:
                        await _maybe_await(on_error(error_msg))
                    return None

        except Exception as e:
            if on_error:
                await _maybe_await(on_error(str(e)))
            return None
        finally:
            self._is_streaming = False
            self._process = None

        # Add completed message to history
        if self._current_message and self._current_message.content:
            self._messages.append(self._current_message)
            if on_message_complete:
                await _maybe_await(on_message_complete(self._current_message))
        elif self._current_message:
            # Message exists but has no content — empty response
            if on_error:
                await _maybe_await(on_error("Empty response from Claude CLI"))

        result = self._current_message
        self._current_message = None
        return result

    async def _handle_event(
        self,
        event: dict[str, Any],
        on_text_delta: Callable | None = None,
        on_tool_use: Callable | None = None,
        on_tool_result: Callable | None = None,
        on_message_complete: Callable | None = None,
        on_system_init: Callable | None = None,
    ):
        """Handle a parsed stream event."""
        event_type = event.get("type", "")
        msg = self._current_message
        if msg is None:
            return

        # --- system init ---
        if event_type == "system":
            self._session_info.session_id = event.get("session_id", "")
            self._session_info.tools = event.get("tools", [])
            self._session_info.mcp_servers = event.get("mcp_servers", [])
            if self.resume_session_id is None:
                self.resume_session_id = self._session_info.session_id
            if on_system_init:
                await _maybe_await(on_system_init(self._session_info))
            return

        # --- message_start ---
        if event_type == "message_start":
            message = event.get("message", {})
            msg.model = message.get("model", self.model)
            return

        # --- content_block_start ---
        if event_type == "content_block_start":
            block_data = event.get("content_block", {})
            block_type = block_data.get("type", "text")
            try:
                cb_type = ContentBlockType(block_type)
            except ValueError:
                # Unknown block type (e.g. future types) — skip
                self._current_block_index = -1
                return
            block = ContentBlock(type=cb_type)
            if block_type == "tool_use":
                block.tool_name = block_data.get("name", "")
                block.tool_id = block_data.get("id", "")
            msg.content.append(block)
            self._current_block_index = len(msg.content) - 1
            if block_type == "tool_use" and on_tool_use:
                await _maybe_await(on_tool_use(block))
            return

        # --- content_block_delta ---
        if event_type == "content_block_delta":
            delta = event.get("delta", {})
            delta_type = delta.get("type", "")
            if self._current_block_index < 0 or self._current_block_index >= len(msg.content):
                return
            block = msg.content[self._current_block_index]

            if delta_type == "text_delta":
                text = delta.get("text", "")
                block.text += text
                if on_text_delta:
                    await _maybe_await(on_text_delta(text))

            elif delta_type == "input_json_delta":
                # Tool input arrives as partial JSON string
                partial = delta.get("partial_json", "")
                block.text += partial  # Accumulate for later parsing
            return

        # --- content_block_stop ---
        if event_type == "content_block_stop":
            if self._current_block_index < 0 or self._current_block_index >= len(msg.content):
                return
            block = msg.content[self._current_block_index]
            if block.type == ContentBlockType.TOOL_USE and block.text:
                # Parse accumulated JSON input
                try:
                    block.tool_input = json.loads(block.text)
                except json.JSONDecodeError:
                    block.tool_input = {"_raw": block.text}
                block.text = ""  # Clear the temp accumulation
            return

        # --- message_delta ---
        if event_type == "message_delta":
            # Contains usage info
            usage = event.get("usage", {})
            msg.output_tokens = usage.get("output_tokens", 0)
            return

        # --- result ---
        if event_type == "result":
            msg.session_id = event.get("session_id", self._session_info.session_id)
            msg.cost_usd = event.get("cost_usd", 0.0)
            msg.duration_ms = event.get("duration_ms", 0.0)
            msg.input_tokens = event.get("input_tokens", 0)
            msg.output_tokens = event.get("output_tokens", msg.output_tokens)
            msg.cache_read_tokens = event.get("cache_read_tokens", 0)
            msg.cache_creation_tokens = event.get("cache_creation_tokens", 0)
            msg.model = event.get("model", msg.model)
            if not self.resume_session_id:
                self.resume_session_id = msg.session_id
            self._session_info.session_id = msg.session_id
            return

        # --- assistant (non-streaming final) ---
        if event_type == "assistant":
            # Full message in one shot (fallback)
            message = event.get("message", {})
            content_blocks = message.get("content", [])
            for cb in content_blocks:
                cb_type = cb.get("type", "text")
                try:
                    block_enum = ContentBlockType(cb_type)
                except ValueError:
                    continue  # Skip unknown block types
                block = ContentBlock(type=block_enum)
                if cb_type == "text":
                    block.text = cb.get("text", "")
                    if on_text_delta:
                        await _maybe_await(on_text_delta(block.text))
                elif cb_type == "tool_use":
                    block.tool_name = cb.get("name", "")
                    block.tool_id = cb.get("id", "")
                    block.tool_input = cb.get("input", {})
                    if on_tool_use:
                        await _maybe_await(on_tool_use(block))
                elif cb_type == "tool_result":
                    block.tool_result = cb.get("content", "")
                    block.is_error = cb.get("is_error", False)
                    if on_tool_result:
                        await _maybe_await(on_tool_result(block))
                msg.content.append(block)
            return

    def stop(self):
        """Request the current stream to stop."""
        self._stop_requested = True
        if self._process and self._process.returncode is None:
            try:
                self._process.terminate()
            except Exception:
                pass

    def reset(self):
        """Start a new conversation (clear session ID and messages)."""
        self.stop()
        self.resume_session_id = None
        self._messages.clear()
        self._current_message = None
        self._session_info = SessionInfo()


# ---------------------------------------------------------------------------
# Session history discovery
# ---------------------------------------------------------------------------


def list_recent_sessions(workspace_path: str, limit: int = 20) -> list[dict[str, Any]]:
    """List recent Claude sessions for the given workspace.

    Uses `claude sessions list --output json` if available,
    otherwise returns empty.
    """
    clean_env = {k: v for k, v in os.environ.items() if not k.startswith("CLAUDE")}
    try:
        result = subprocess.run(
            ["claude", "sessions", "list", "--output", "json"],
            cwd=workspace_path,
            capture_output=True,
            text=True,
            timeout=10,
            env=clean_env,
        )
        if result.returncode == 0 and result.stdout.strip():
            sessions = json.loads(result.stdout)
            if isinstance(sessions, list):
                return sessions[:limit]
    except Exception:
        pass
    return []


# ---------------------------------------------------------------------------
# Available models
# ---------------------------------------------------------------------------

AVAILABLE_MODELS = [
    {"id": "sonnet", "name": "Sonnet 4.6", "description": "Fast, balanced"},
    {"id": "opus", "name": "Opus 4.6", "description": "Most capable"},
    {"id": "haiku", "name": "Haiku 4.5", "description": "Fastest, lightweight"},
]


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


async def _maybe_await(result):
    """Await the result if it's a coroutine."""
    if asyncio.iscoroutine(result) or asyncio.isfuture(result):
        return await result
    return result
