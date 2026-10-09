"""Bounded agent tool loop.

The guards, from failures seen in practice:
- **`max_steps`:** a hard cap on tool rounds. Local MoE models loop on tool calls.
- **Repeat guard:** the same tool with the same arguments twice in a row ends the loop.
- **Forced synthesis:** when the loop stops for any reason other than a final answer, one more
  model call runs with tools disabled. This avoids the "ended on a search with no answer" bug.

The model is any callable `llm(messages, tools_enabled) -> Step`, so the loop works
with whatever SDK you use. Tool errors become tool messages and never escape.
"""

from __future__ import annotations

import json
from collections.abc import Callable
from dataclasses import dataclass, field
from typing import Any


@dataclass
class ToolCall:
    name: str
    args: dict[str, Any] = field(default_factory=dict)

    def key(self) -> str:
        return self.name + json.dumps(self.args, sort_keys=True, default=str)


@dataclass
class Step:
    """One model turn: either a final `answer` or a list of `tool_calls`."""

    answer: str | None = None
    tool_calls: list[ToolCall] = field(default_factory=list)


@dataclass
class LoopResult:
    answer: str
    steps: int
    stop_reason: str  # "answer" | "max_steps" | "repeat" | "no_action"
    tool_log: list[dict[str, Any]] = field(default_factory=list)  # persist as JSONB


LLM = Callable[[list[dict[str, Any]], bool], Step]
Tool = Callable[..., Any]

FINAL_NUDGE = (
    "Tool budget exhausted. Answer now using only the information already gathered. "
    "If it is not enough, say what is missing."
)


def run_tool_loop(
    llm: LLM,
    tools: dict[str, Tool],
    messages: list[dict[str, Any]],
    max_steps: int = 10,
    max_result_chars: int = 8000,
) -> LoopResult:
    if max_steps < 1:
        raise ValueError("max_steps must be >= 1")
    messages = list(messages)
    log: list[dict[str, Any]] = []
    last_keys: list[str] | None = None
    stop = "max_steps"
    steps = 0
    while steps < max_steps:
        remaining = max_steps - steps
        if remaining == 1:
            messages.append({"role": "system", "content": "This is your last tool round. Prefer answering now."})
        step = llm(messages, True)
        steps += 1
        if step.answer is not None and not step.tool_calls:
            return LoopResult(step.answer, steps, "answer", log)
        if not step.tool_calls:
            stop = "no_action"
            break
        keys = [c.key() for c in step.tool_calls]
        if keys == last_keys:
            stop = "repeat"
            break
        last_keys = keys
        for call in step.tool_calls:
            fn = tools.get(call.name)
            try:
                if fn is None:
                    raise KeyError(f"unknown tool {call.name}")
                result = fn(**call.args)
                ok = True
            except Exception as e:  # surface to the model, never crash the loop
                result, ok = f"tool error: {type(e).__name__}: {e}", False
            text = result if isinstance(result, str) else json.dumps(result, default=str)
            text = text[:max_result_chars]
            log.append({"step": steps, "tool": call.name, "args": call.args, "ok": ok, "chars": len(text)})
            messages.append({"role": "tool", "name": call.name, "content": text})

    messages.append({"role": "system", "content": FINAL_NUDGE})
    final = llm(messages, False)
    return LoopResult(final.answer or "", steps, stop, log)
