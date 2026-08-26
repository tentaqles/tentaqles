---
name: emit-signal
description: Broadcast a named signal from the current workspace to another registered workspace. Use when the user says "send signal", "broadcast to workspace", "notify workspace", or "/tentaqles:signal". Arguments: target workspace ID, event type, message.
---

# Emit Signal

Broadcast a workspace-level event (signal) to another workspace. The signal will appear in the target workspace's next session preamble.

**Requires `signals: enabled: true` in the current workspace's `.tentaqles.yaml` manifest.** If `signals.enabled` is not set to `true`, stop and inform the user: "Signals are disabled for this workspace. Add `signals:\n  enabled: true` to `.tentaqles.yaml` to enable them."

Arguments: `$ARGUMENTS` — expected format: `<target_workspace_id> <event_type> <message>`

## Step 1: Detect current workspace

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" -c "
import os
from tentaqles.manifest.loader import load_manifest
manifest = load_manifest(os.getcwd())
if manifest:
    print(f'client={manifest[\"client\"]}')
    signals_cfg = manifest.get('signals', {})
    print(f'signals_enabled={signals_cfg.get(\"enabled\", False)}')
else:
    print('client=unknown')
    print('signals_enabled=False')
"
```

If `signals_enabled=False`, stop and tell the user to enable signals in `.tentaqles.yaml`.

## Step 2: Parse arguments

From `$ARGUMENTS`, extract:
- `TARGET_WORKSPACE` — the workspace_id to send the signal to (e.g. `dirtybird`, `acme-corp`)
- `EVENT_TYPE` — one of: `deploy`, `ci`, `pr`, `alert`, `custom`
- `MESSAGE` — human-readable description of the event

If any field is missing, ask the user for the missing value before proceeding.

## Step 3: Emit the signal

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" -c "
from tentaqles.memory.signals import SignalBus

bus = SignalBus()
signal_id = bus.emit(
    from_workspace='{from_workspace}',
    to_workspace='{target_workspace}',
    event_type='{event_type}',
    message='{message}',
)
print(f'signal_id={signal_id}')
print('ok')
"
```

Replace `{from_workspace}`, `{target_workspace}`, `{event_type}`, and `{message}` with the values from Steps 1 and 2. Escape any quotes in the message.

## Step 4: Report

On success, report:

```
Signal emitted:
  From:    {from_workspace}
  To:      {target_workspace}
  Type:    {event_type}
  Message: {message}
  ID:      {signal_id}

The signal will appear in {target_workspace}'s next session preamble (expires in 48 hours).
```

## Error Handling

- **Unknown target workspace**: if `emit()` raises `ValueError`, the target workspace is not registered. Tell the user: "Workspace `{target_workspace}` is not registered in meta.db. It must run at least one session with the plugin before it can receive signals."
- **signals.enabled not set**: remind the user to add `signals:\n  enabled: true` to their `.tentaqles.yaml`.
- **Payload guidance**: signal messages must not contain file paths, credentials, API keys, or client-specific implementation details. Signals are workspace-level notifications only — cross-client data isolation applies.
