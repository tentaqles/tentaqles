#!/usr/bin/env bash
# Tentaqles Claude Code hook bridge.
#
# Usage: bash tq_hook.sh <session-start|pre-tool-use>
#
# Resolves the `tq` binary and runs `tentaqles claude-hook <event>` (stdin passes
# straight through, exit status is propagated).
#
# FAIL-CLOSED: if tq cannot be found or run, pre-tool-use denies every shell command
# (exit 2). A missing or unknown event also exits 2, so a misconfigured hook
# cannot silently disable enforcement.
#
# This is a bash script (not POSIX sh), but it avoids arrays and other
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
# No environment variable picks the binary: an agent can set variables (a
# settings env block, an export in the same shell), and the tq that
# enforces must not be its choice. Nor does the plugin folder: the plugin
# ships no binary, so nothing under it is ever run as tq. The order is PATH,
# then the well-known install dirs; the tq found there is the user's and the
# OS's to protect.
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

# Run tq. Not exec'd so that a broken binary (126 "cannot execute" / 127 "not
# found") still reaches the deny below; the child inherits stdin, so the hook
# payload passes through unchanged.
#
# pre-tool-use fails closed: tq answers 0 (allow)
# or 2 (deny). Any other status (a crash the Go runtime could not recover,
# a killed process, a usage error) is a call tq did not judge, and Claude
# Code would run it on a non-2 exit, so it becomes a deny here.
#
# A tq that hangs is stopped: Claude Code kills a hook at its timeout (15 s
# in hooks.json) and then RUNS the call, so tq runs in the background under
# a watchdog that stops it after _tq_deadline seconds (7, then a KILL at 8; tq's own decision
# deadline is 6 s), and the stop is a deny. Only bash builtins, `sleep` and
# `kill` are used (Git Bash, macOS, Linux; no GNU timeout). The background
# job reads the hook's stdin through an explicit 0<&0, since bash gives an
# asynchronous command /dev/null otherwise. TQ_HOOK_WRAPPER_DEADLINE may
# only lower the deadline (tests); a larger value is ignored.
_tq_deadline=7
case "${TQ_HOOK_WRAPPER_DEADLINE:-}" in
  ''|*[!0-9]*) ;;
  *) [ "$TQ_HOOK_WRAPPER_DEADLINE" -ge 1 ] && [ "$TQ_HOOK_WRAPPER_DEADLINE" -lt "$_tq_deadline" ] && _tq_deadline="$TQ_HOOK_WRAPPER_DEADLINE" ;;
esac

if [ -n "$TQ" ]; then
  "$TQ" claude-hook "$_event" 0<&0 &
  _tq_pid=$!
  (
    _i=0
    while [ "$_i" -lt "$_tq_deadline" ]; do
      sleep 1
      kill -0 "$_tq_pid" 2>/dev/null || exit 0
      _i=$((_i + 1))
    done
    kill "$_tq_pid" 2>/dev/null
    sleep 1
    kill -9 "$_tq_pid" 2>/dev/null
  ) </dev/null >/dev/null 2>&1 &
  _tq_watch=$!
  wait "$_tq_pid"
  _status=$?
  kill "$_tq_watch" 2>/dev/null
  if [ "$_status" != "126" ] && [ "$_status" != "127" ]; then
    if [ "$_status" != "0" ] && [ "$_status" != "2" ]; then
      if [ "$_event" = "pre-tool-use" ]; then
        echo "BLOCKED [tq/guard-error]: tq exited with status ${_status} without a decision (a tq still running after ${_tq_deadline}s is stopped); tq denies a call it cannot judge. Report this to the developer instead of retrying; \`tentaqles doctor\` shows what is wrong" >&2
        exit 2
      fi
      echo "Tentaqles: tq did not print the session preamble (status ${_status}); run \`tentaqles doctor\`."
      exit 0
    fi
    exit "$_status"
  fi
fi

# --- tq is not installed (or could not be executed) ---
if [ "$_event" != "pre-tool-use" ]; then
  echo "Tentaqles: tq is not installed, so every shell command is blocked. Install: $INSTALL_URL"
  exit 0
fi

echo "BLOCKED [tq/guard-error]: tq is not installed or cannot run, so nothing can judge this call; tq denies a call it cannot judge. Report this to the developer instead of retrying. Install: $INSTALL_URL" >&2
exit 2
