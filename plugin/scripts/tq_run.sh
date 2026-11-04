#!/usr/bin/env bash
# Tentaqles plugin script runner — resolves interpreter + deps, then exec's.
# Usage: bash tq_run.sh memory-bridge.py [args...]
#   or:  bash tq_run.sh scripts/memory-bridge.py [args...]
#
# Automatically finds the right Python, sets PYTHONPATH, and installs
# deps on first run — so hooks/skills never depend on bare `python`.

# Resolve own directory (POSIX-safe: no array subscript on BASH_SOURCE)
_this="${BASH_SOURCE:-$0}"
_self_dir="$(cd "$(dirname "$_this")" && pwd)"
export CLAUDE_PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT:-$(dirname "$_self_dir")}"

# shellcheck source=tq_env.sh
. "$_self_dir/tq_env.sh"

_script="$1"; shift

# Accept either "scripts/foo.py" or "foo.py" (relative to plugin root)
case "$_script" in
  scripts/*) _target="$CLAUDE_PLUGIN_ROOT/$_script" ;;
  /*) _target="$_script" ;;
  *) _target="$CLAUDE_PLUGIN_ROOT/scripts/$_script" ;;
esac

exec "$TENTAQLES_PY" "$_target" "$@"
