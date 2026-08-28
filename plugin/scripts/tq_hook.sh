#!/usr/bin/env bash
# Tentaqles Claude Code hook bridge.
#
# Usage: bash tq_hook.sh <session-start|pre-tool-use>
#
# Resolves the `tq` binary and runs `tq claude-hook <event>` (stdin passes
# straight through, exit status is propagated). If no binary is found, falls
# back to the dependency-free Python guard next to this script
# (pre-tool-use) or prints an install notice (session-start).
#
# The fallback deliberately does NOT go through tq_run.sh/tq_env.sh: that path
# can synchronously pip-install plugin deps, which would blow the PreToolUse
# timeout and fail OPEN. identity-guard.py needs nothing but a stdlib Python,
# so an interpreter is resolved here instead.
#
# FAIL-CLOSED: if neither `tq` nor any Python interpreter can be found, the
# pre-tool-use hook blocks (exit 2) rather than allowing an unverified remote
# command. On such a box every Bash tool call is blocked until `tq` or Python
# is installed. A missing/unknown event also exits 2, so a misconfigured hook
# cannot silently disable enforcement.
#
# This is a bash script (not POSIX sh) — but it avoids arrays and other
# constructs that break under older bashes on Windows/macOS.

set -u

_this="${BASH_SOURCE:-$0}"
_self_dir="$(cd "$(dirname "$_this")" && pwd)"
CLAUDE_PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT:-$(dirname "$_self_dir")}"
export CLAUDE_PLUGIN_ROOT

INSTALL_URL="https://github.com/tentaqles/tentaqles#install"

_event="${1:-}"
case "$_event" in
  session-start|pre-tool-use) ;;
  *)
    echo "tq_hook.sh: unknown event '${_event}'" >&2
    exit 2
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

# --- Resolve the tq binary ---
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
if [ -z "$TQ" ] && [ "$_win" = "1" ] && [ -n "${LOCALAPPDATA:-}" ]; then
  _lad="$(_upath "$LOCALAPPDATA")"
  _try "$_lad/tentaqles/bin/tq.exe" || _try "$_lad/tentaqles/bin/tq"
fi
if [ -z "$TQ" ] && [ "$_win" = "1" ] && [ -n "${USERPROFILE:-}" ]; then
  _up="$(_upath "$USERPROFILE")"
  _try "$_up/AppData/Local/tentaqles/bin/tq.exe" || _try "$_up/AppData/Local/tentaqles/bin/tq"
fi

# Run tq. Not exec'd so that a broken binary (126 "cannot execute" / 127 "not
# found") can still fall through to the Python fallback; the child inherits
# stdin, so the hook payload passes through unchanged.
if [ -n "$TQ" ]; then
  "$TQ" claude-hook "$_event"
  _status=$?
  if [ "$_status" != "126" ] && [ "$_status" != "127" ]; then
    exit "$_status"
  fi
fi

# --- Fallback: tq is not installed (or could not be executed) ---
if [ "$_event" != "pre-tool-use" ]; then
  echo "Tentaqles: tq is not installed — identity enforcement is in fallback mode (remote git/gh/cloud commands are blocked). Install: $INSTALL_URL"
  exit 0
fi

# Resolve a stdlib-only Python interpreter for identity-guard.py.
PY=""
PY_LAUNCHER=0

if [ -n "${TENTAQLES_PY:-}" ] && [ -x "${TENTAQLES_PY}" ]; then
  if "$TENTAQLES_PY" -c "import sys" >/dev/null 2>&1; then
    PY="$TENTAQLES_PY"
  fi
fi

if [ -z "$PY" ] && [ "$_win" = "1" ]; then
  if py -3 -c "import sys" >/dev/null 2>&1; then
    PY="py"
    PY_LAUNCHER=1
  fi
fi

if [ -z "$PY" ]; then
  for _probe in python3 python; do
    if command -v "$_probe" >/dev/null 2>&1 && "$_probe" -c "import sys" >/dev/null 2>&1; then
      PY="$_probe"
      break
    fi
  done
fi

if [ -z "$PY" ]; then
  echo "BLOCKED: tq is not installed and no python interpreter was found; refusing without a verified identity. Install tq: $INSTALL_URL" >&2
  exit 2
fi

TQ_FALLBACK=1
export TQ_FALLBACK
if [ "$PY_LAUNCHER" = "1" ]; then
  exec "$PY" -3 "$_self_dir/identity-guard.py"
fi
exec "$PY" "$_self_dir/identity-guard.py"
