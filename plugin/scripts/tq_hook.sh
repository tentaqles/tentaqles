#!/usr/bin/env bash
# Tentaqles Claude Code hook bridge.
#
# Usage: bash tq_hook.sh <session-start|pre-tool-use>
#
# Resolves the `tq` binary and exec's `tq claude-hook <event>` (stdin passes
# straight through). If no binary is found, falls back to the dependency-free
# Python guard (pre-tool-use) or prints an install notice (session-start).
#
# POSIX sh compatible: no arrays, no bashisms beyond `local`-free plain code.

set -u

_this="${BASH_SOURCE:-$0}"
_self_dir="$(cd "$(dirname "$_this")" && pwd)"
CLAUDE_PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT:-$(dirname "$_self_dir")}"
export CLAUDE_PLUGIN_ROOT

_event="${1:-}"
case "$_event" in
  session-start|pre-tool-use) ;;
  *)
    echo "tq_hook.sh: unknown event '${_event}'" >&2
    exit 0
    ;;
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

TQ=""

_try() {
  [ -n "${1:-}" ] || return 1
  [ -x "$1" ] || return 1
  TQ="$1"
  return 0
}

# 1. $TQ_BIN wins, but only if executable.
if [ -n "${TQ_BIN:-}" ]; then
  _try "$TQ_BIN" || { [ "$_win" = "1" ] && _try "${TQ_BIN}.exe"; }
fi

# 2. Plugin-bundled binary.
if [ -z "$TQ" ] && [ -n "${CLAUDE_PLUGIN_ROOT:-}" ]; then
  _try "$CLAUDE_PLUGIN_ROOT/bin/tq" || { [ "$_win" = "1" ] && _try "$CLAUDE_PLUGIN_ROOT/bin/tq.exe"; }
fi

# 3. PATH.
if [ -z "$TQ" ]; then
  _found="$(command -v tq 2>/dev/null || true)"
  [ -n "$_found" ] && _try "$_found"
fi

# 4/5. Well-known install dirs.
if [ -z "$TQ" ] && [ -n "${HOME:-}" ]; then
  _try "$HOME/.tentaqles/bin/tq" || { [ "$_win" = "1" ] && _try "$HOME/.tentaqles/bin/tq.exe"; }
fi
if [ -z "$TQ" ] && [ -n "${HOME:-}" ]; then
  _try "$HOME/.local/bin/tq" || { [ "$_win" = "1" ] && _try "$HOME/.local/bin/tq.exe"; }
fi

# 6. Windows LocalAppData.
if [ -z "$TQ" ] && [ "$_win" = "1" ]; then
  if [ -n "${LOCALAPPDATA:-}" ]; then
    _lad="$(_upath "$LOCALAPPDATA")"
    _try "$_lad/tentaqles/bin/tq.exe" || _try "$_lad/tentaqles/bin/tq"
  fi
fi
if [ -z "$TQ" ] && [ "$_win" = "1" ]; then
  if [ -n "${USERPROFILE:-}" ]; then
    _up="$(_upath "$USERPROFILE")"
    _try "$_up/AppData/Local/tentaqles/bin/tq.exe" || _try "$_up/AppData/Local/tentaqles/bin/tq"
  fi
fi

if [ -n "$TQ" ]; then
  exec "$TQ" claude-hook "$_event"
fi

# --- Fallback: tq is not installed ---
if [ "$_event" = "pre-tool-use" ]; then
  TQ_FALLBACK=1
  export TQ_FALLBACK
  exec bash "$_self_dir/tq_run.sh" identity-guard.py
fi

echo "Tentaqles: tq is not installed — identity enforcement is in fallback mode (remote git/gh/cloud commands are blocked). Install: https://github.com/tentaqles/tentaqles#install"
exit 0
