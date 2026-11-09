#!/usr/bin/env bash
# Tentaqles memory hook bridge.
#
# Usage: bash lobe_hook.sh <event>
#
# Resolves the `tq` binary and runs `tentaqles lobe-hook claude <event>` with stdin and
# stdout passed straight through. Its own stderr goes to /dev/null.
#
# FAIL-OPEN: this hook never blocks anything. An unknown event, a missing tq, a tq that
# crashes or exits non-zero: the script exits 0 and prints nothing of its own, so a broken
# memory cannot deny a tool call or stall a session. The identity guard (tq_hook.sh) is a
# different hook with the opposite rule, and it stays fail-closed.
#
# No watchdog and no background job: the binary has its own deadline and watchdog.

_this="${BASH_SOURCE:-$0}"
_self_dir="$(cd "$(dirname "$_this")" && pwd)"
CLAUDE_PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT:-$(dirname "$_self_dir")}"
export CLAUDE_PLUGIN_ROOT

# TQ_LOBE_NO_HOOKS=1 turns the memory off for this shell.
if [ "${TQ_LOBE_NO_HOOKS:-}" = "1" ]; then
  exit 0
fi

_event="${1:-}"
case "$_event" in
  session-start|user-prompt-submit|pre-tool-use|post-tool-use|post-tool-use-failure|stop|session-end) ;;
  *) exit 0 ;;
esac

_uname="$(uname -s 2>/dev/null || echo Unknown)"
case "$_uname" in
  MINGW*|MSYS*|CYGWIN*) _win=1 ;;
  *) _win=0 ;;
esac

# Convert a Windows-style path to a unix path when possible.
_upath() {
  if [ "$_win" = "1" ] && command -v cygpath >/dev/null 2>&1; then
    cygpath -u "$1" 2>/dev/null || printf '%s' "$1"
  else
    printf '%s' "$1"
  fi
}

# --- Resolve the tq binary (same search order as tq_hook.sh) ---
TQ=""

_try() {
  [ -n "${1:-}" ] || return 1
  [ -x "$1" ] || return 1
  TQ="$1"
  return 0
}

# 1. PATH.
_found="$(command -v tq 2>/dev/null || true)"
[ -n "$_found" ] && _try "$_found"

# 2/3. Well-known install dirs.
if [ -z "$TQ" ] && [ -n "${HOME:-}" ]; then
  _try "$HOME/.tentaqles/bin/tq" || { [ "$_win" = "1" ] && _try "$HOME/.tentaqles/bin/tq.exe"; }
fi
if [ -z "$TQ" ] && [ -n "${HOME:-}" ]; then
  _try "$HOME/.local/bin/tq" || { [ "$_win" = "1" ] && _try "$HOME/.local/bin/tq.exe"; }
fi

# 4. Windows LocalAppData.
if [ -z "$TQ" ] && [ "$_win" = "1" ] && [ -n "${LOCALAPPDATA:-}" ]; then
  _lad="$(_upath "$LOCALAPPDATA")"
  _try "$_lad/tentaqles/bin/tq.exe" || _try "$_lad/tentaqles/bin/tq"
fi
if [ -z "$TQ" ] && [ "$_win" = "1" ] && [ -n "${USERPROFILE:-}" ]; then
  _up="$(_upath "$USERPROFILE")"
  _try "$_up/AppData/Local/tentaqles/bin/tq.exe" || _try "$_up/AppData/Local/tentaqles/bin/tq"
fi

# No tq found: nothing to record, and nothing to say.
if [ -z "$TQ" ]; then
  exit 0
fi

# Whatever the binary does, the hook exits 0.
"$TQ" lobe-hook claude "$_event" 2>/dev/null
exit 0
