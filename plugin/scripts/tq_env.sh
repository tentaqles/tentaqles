#!/usr/bin/env bash
# Tentaqles plugin runtime bootstrap — source this to get:
#   CLAUDE_PLUGIN_ROOT  absolute path to plugin directory
#   TENTAQLES_PY        absolute path to a working Python interpreter
#   PYTHONPATH           includes plugin root + plugin data lib dir
#
# POSIX-compatible (works in bash, zsh, dash, sh).
# Sourced by skill bash blocks and tq_run.sh.

# Guard: if already resolved, skip entirely
if [ -n "$TENTAQLES_PY" ] && [ -x "$TENTAQLES_PY" ]; then
  # Already set up — nothing to do
  :
else

# --- 1. Resolve plugin root ---
if [ -z "$CLAUDE_PLUGIN_ROOT" ]; then
  # Try BASH_SOURCE (only available in bash; no array subscript for POSIX safety)
  _tq_self=""
  if [ -n "${BASH_VERSION:-}" ] && [ -n "${BASH_SOURCE:-}" ]; then
    _tq_self="$BASH_SOURCE"
  fi

  if [ -n "$_tq_self" ]; then
    CLAUDE_PLUGIN_ROOT="$(cd "$(dirname "$_tq_self")/.." && pwd)"
  else
    # Search the Claude Code plugin cache (marketplace-agnostic)
    for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do
      [ -f "${_d}.claude-plugin/plugin.json" ] && CLAUDE_PLUGIN_ROOT="${_d%/}" && break
    done
  fi
fi
export CLAUDE_PLUGIN_ROOT

# --- 2. Resolve plugin data dir ---
if [ -z "${CLAUDE_PLUGIN_DATA:-}" ]; then
  _uname="$(uname -s 2>/dev/null || echo Unknown)"
  case "$_uname" in
    Darwin)
      CLAUDE_PLUGIN_DATA="$HOME/Library/Application Support/tentaqles" ;;
    MINGW*|MSYS*|CYGWIN*)
      CLAUDE_PLUGIN_DATA="$HOME/.tentaqles" ;;
    *)
      _xdg="${XDG_DATA_HOME:-$HOME/.local/share}"
      CLAUDE_PLUGIN_DATA="$_xdg/tentaqles" ;;
  esac
fi
export CLAUDE_PLUGIN_DATA

# --- 3. Find a working Python interpreter ---
# Probe in order: py -3 (Windows launcher), python3 (Unix standard), python (legacy)
# Each candidate is validated by asking it to print sys.executable,
# which resolves broken venv shims (they fail the -c check).
TENTAQLES_PY=""
for _probe in py python3 python; do
  case "$_probe" in
    py)
      # "py -3" is two words — handle explicitly to avoid word-splitting issues
      _exe=$(py -3 -c "import sys; print(sys.executable)" 2>/dev/null) || continue ;;
    *)
      _exe=$("$_probe" -c "import sys; print(sys.executable)" 2>/dev/null) || continue ;;
  esac
  if [ -n "$_exe" ]; then
    TENTAQLES_PY="$_exe"
    break
  fi
done
# Last resort: python3 is more likely to exist cross-platform than python
if [ -z "$TENTAQLES_PY" ]; then
  TENTAQLES_PY="python3"
fi
export TENTAQLES_PY

# --- 4. Set PYTHONPATH for tentaqles imports + bootstrap deps ---
_lib="$CLAUDE_PLUGIN_DATA/lib"
_pp="${PYTHONPATH:-}"

# Add lib dir if it exists and is not already in PYTHONPATH
case ":$_pp:" in
  *":$_lib:"*) ;;
  *) [ -d "$_lib" ] && _pp="${_lib}${_pp:+:$_pp}" ;;
esac

# Add plugin root if not already in PYTHONPATH
if [ -n "$CLAUDE_PLUGIN_ROOT" ]; then
  case ":$_pp:" in
    *":$CLAUDE_PLUGIN_ROOT:"*) ;;
    *) _pp="${CLAUDE_PLUGIN_ROOT}${_pp:+:$_pp}" ;;
  esac
fi
export PYTHONPATH="$_pp"

# --- 5. Ensure deps are installed (runs bootstrap.py if needed) ---
if [ -n "$CLAUDE_PLUGIN_ROOT" ]; then
  if ! "$TENTAQLES_PY" -c "import yaml, pathspec" >/dev/null 2>&1; then
    # Portable null device
    _null=/dev/null
    [ -e "$_null" ] || _null=NUL
    "$TENTAQLES_PY" "$CLAUDE_PLUGIN_ROOT/scripts/bootstrap.py" <"$_null" 2>"$_null" || true
    # Re-add lib dir if bootstrap just created it
    if [ -d "$_lib" ]; then
      case ":${PYTHONPATH:-}:" in
        *":$_lib:"*) ;;
        *) export PYTHONPATH="$_lib${PYTHONPATH:+:$PYTHONPATH}" ;;
      esac
    fi
  fi
fi

fi  # end guard
