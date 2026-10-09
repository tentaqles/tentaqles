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
#
# The probe costs one interpreter startup (~300ms), so the answer is cached on
# disk: every hook invocation is a fresh process, and SessionEnd hooks share a
# single ~1500ms budget (see step 5). Re-probing on every hook spent a third of
# that budget before the hook script even started.
_py_cache="$CLAUDE_PLUGIN_DATA/.interpreter"

TENTAQLES_PY=""
if [ -r "$_py_cache" ]; then
  # `read` is a shell builtin — no process spawn, unlike `cat`
  read -r _cached < "$_py_cache" 2>/dev/null || _cached=""
  # A cached path is only trusted while it still points at something runnable
  # (interpreter upgraded, venv deleted, drive unmounted → fall through).
  if [ -n "$_cached" ] && [ -x "$_cached" ]; then
    TENTAQLES_PY="$_cached"
  fi
  unset _cached
fi

if [ -z "$TENTAQLES_PY" ]; then
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
  # Cache only a real resolved path — never the "python3" fallback below, which
  # is a PATH lookup rather than an answer.
  if [ -n "$TENTAQLES_PY" ]; then
    mkdir -p "$CLAUDE_PLUGIN_DATA" 2>/dev/null || true
    printf '%s\n' "$TENTAQLES_PY" > "$_py_cache" 2>/dev/null || true
  fi
fi

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
# This check is another interpreter startup (~300ms). Claude Code gives *all*
# SessionEnd hooks one shared AbortSignal — 1500ms by default, and a plugin's
# declared `timeout` in hooks.json does NOT raise it (only the
# CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS env var does). Paying for this check
# on every hook pushed session-end.py past the deadline, so once the deps are
# known good for a given plugin root we stamp it and skip the check.
#
# The stamp records the plugin root, which contains the version
# (…/tentaqles/<version>/), so a plugin upgrade invalidates it automatically.
# It also records whether the deps were satisfied from the bootstrapped lib dir
# or from the interpreter itself — so deleting the lib dir invalidates the
# stamp and bootstrap.py self-heals, as it did before the stamp existed.
_dep_stamp="$CLAUDE_PLUGIN_DATA/.deps-ok"
if [ -d "$_lib" ]; then _dep_key="$CLAUDE_PLUGIN_ROOT|lib"; else _dep_key="$CLAUDE_PLUGIN_ROOT|sys"; fi
_dep_seen=""
if [ -r "$_dep_stamp" ]; then
  read -r _dep_seen < "$_dep_stamp" 2>/dev/null || _dep_seen=""
fi

if [ -n "$CLAUDE_PLUGIN_ROOT" ] && [ "$_dep_seen" != "$_dep_key" ]; then
  if "$TENTAQLES_PY" -c "import yaml, pathspec" >/dev/null 2>&1; then
    mkdir -p "$CLAUDE_PLUGIN_DATA" 2>/dev/null || true
    printf '%s\n' "$_dep_key" > "$_dep_stamp" 2>/dev/null || true
  else
    # Portable null device
    _null=/dev/null
    [ -e "$_null" ] || _null=NUL
    # bootstrap.py never installs inline: it starts a detached background
    # install and returns at once. Its one-line notice goes to stdout, which
    # here would land in the calling hook's output (and corrupt a JSON
    # response), so it is discarded; the SessionStart bootstrap hook shows it.
    "$TENTAQLES_PY" "$CLAUDE_PLUGIN_ROOT/scripts/bootstrap.py" <"$_null" >"$_null" 2>"$_null" || true
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
