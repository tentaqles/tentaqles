#!/bin/sh
# tq identity-isolation spike (written for macOS; runs anywhere).
#
# Answers the one question the roadmap is gated on: does a CLI on macOS
# isolate its credentials by config directory, or do all config directories
# share a single OS Keychain entry? If they share, then pointing
# CLAUDE_CONFIG_DIR at a per-workspace folder does NOT give you two Claude
# accounts on a Mac, and tq needs a different mechanism there.
#
# The test is simple and decisive: point a CLI at a brand-new EMPTY config
# directory and ask whether it is logged in.
#   - reports NOT logged in  -> credentials live in the config dir  -> tq's
#     mechanism isolates them, and macOS works like Windows.
#   - reports LOGGED IN      -> the credential came from somewhere else (the
#     Keychain) -> config-dir isolation does not isolate credentials.
#
# SAFETY: this script writes only inside a temporary directory that it
# removes on exit. It never logs in, never logs out, never modifies
# ~/.gitconfig, your shell profile, or any existing config directory. Pass
# --full to additionally run a contained end-to-end test with HOME redirected
# to a scratch directory, which is still isolated from your real setup.
#
# Usage:
#   sh spike-macos.sh          # probes only (about 15 seconds)
#   sh spike-macos.sh --full   # probes + a contained end-to-end test

set -u

FULL=0
[ "${1:-}" = "--full" ] && FULL=1

SCRATCH=$(mktemp -d 2>/dev/null || mktemp -d -t tqspike)
cleanup() { rm -rf "$SCRATCH" 2>/dev/null || true; }
trap cleanup EXIT INT TERM

say() { printf '%s\n' "$*"; }
hr() { say "------------------------------------------------------------"; }

# redact anything that looks like a token before it reaches the screen.
redact() {
  sed -E \
    -e 's/(gh[pousr]_[A-Za-z0-9]{6})[A-Za-z0-9_]*/\1<REDACTED>/g' \
    -e 's/(sk-[A-Za-z0-9]{4})[A-Za-z0-9_-]*/\1<REDACTED>/g' \
    -e 's/("(access_token|refresh_token|oauth_token|token)" *: *")[^"]*/\1<REDACTED>/g' \
    -e 's/(oauth_token: *).*/\1<REDACTED>/'
}

# verdict_of reads a CLI's status output and decides logged-in vs not.
verdict_of() {
  out=$1
  if printf '%s' "$out" | grep -Eqi '"loggedIn" *: *true|Logged in to|✓ Logged in'; then
    say "  RESULT: reports LOGGED IN from an empty config dir"
    say "  => credentials are NOT isolated by config dir (shared Keychain)"
    return 1
  fi
  if printf '%s' "$out" | grep -Eqi '"loggedIn" *: *false|not logged in|no accounts|You are not'; then
    say "  RESULT: reports NOT logged in from an empty config dir"
    say "  => credentials ARE isolated by config dir; tq's mechanism works"
    return 0
  fi
  say "  RESULT: could not classify the output; raw output below"
  return 2
}

hr
say "tq macOS spike"
hr
say "macOS:      $(sw_vers -productVersion 2>/dev/null || echo unknown)"
say "arch:       $(uname -m)"
say "shell:      ${SHELL:-unknown}"
for c in tq claude gh az git; do
  p=$(command -v "$c" 2>/dev/null || true)
  say "$(printf '%-11s' "$c:")${p:-NOT INSTALLED}"
done
say "tq version: $(tq version 2>/dev/null || echo 'n/a')"

# ---------------------------------------------------------------- claude ---
hr
say "1. Claude Code — the question this spike exists for"
hr
if command -v claude >/dev/null 2>&1; then
  mkdir -p "$SCRATCH/claude-empty"
  OUT=$(CLAUDE_CONFIG_DIR="$SCRATCH/claude-empty" claude auth status 2>&1 | redact)
  verdict_of "$OUT"; CLAUDE_RC=$?
  say ""
  say "  --- raw output (empty CLAUDE_CONFIG_DIR) ---"
  printf '%s\n' "$OUT" | sed 's/^/  /'
  say ""
  say "  --- files the CLI created in that empty dir ---"
  ls -a "$SCRATCH/claude-empty" 2>/dev/null | sed 's/^/  /'
else
  say "  claude not installed — skipping (install it first, this is the key test)"
  CLAUDE_RC=3
fi

# -------------------------------------------------------------------- gh ---
hr
say "2. GitHub CLI — same test, for comparison"
hr
if command -v gh >/dev/null 2>&1; then
  mkdir -p "$SCRATCH/gh-empty"
  OUT=$(GH_CONFIG_DIR="$SCRATCH/gh-empty" gh auth status 2>&1 | redact)
  verdict_of "$OUT"; GH_RC=$?
  say ""
  printf '%s\n' "$OUT" | sed 's/^/  /'
else
  say "  gh not installed — skipping"
  GH_RC=3
fi

# -------------------------------------------------------------------- az ---
hr
say "3. Azure CLI — same test"
hr
if command -v az >/dev/null 2>&1; then
  mkdir -p "$SCRATCH/az-empty"
  OUT=$(AZURE_CONFIG_DIR="$SCRATCH/az-empty" az account show 2>&1 | redact | head -20)
  if printf '%s' "$OUT" | grep -qi 'please run .az login.\|not logged in\|No subscription'; then
    say "  RESULT: NOT logged in from an empty config dir => isolated"
  else
    say "  RESULT: appears to have an account => check the output below"
  fi
  printf '%s\n' "$OUT" | sed 's/^/  /'
else
  say "  az not installed — skipping"
fi

# -------------------------------------------------------------- keychain ---
hr
say "4. Keychain entries mentioning these tools (names only, no secrets)"
hr
say "  This lists service names only. It never prints a password: -d is not"
say "  used, so macOS does not prompt and no secret is read."
security dump-keychain 2>/dev/null \
  | grep -Eio '"(svce|labl)"<blob>="[^"]*(claude|anthropic|github|gh|azure)[^"]*"' \
  | sed -E 's/.*="(.*)"/  - \1/' | sort -u | head -20
say "  (an empty list here usually means nothing matched, not that none exist)"

# ------------------------------------------------------------ contained ----
if [ "$FULL" = "1" ]; then
  hr
  say "5. Contained end-to-end test (HOME redirected; your real setup untouched)"
  hr
  if ! command -v tq >/dev/null 2>&1; then
    say "  tq not installed — skipping. Install with:"
    say "    brew install tentaqles/tap/tq"
  else
    export HOME="$SCRATCH/home"
    export GIT_CONFIG_GLOBAL="$SCRATCH/home/.gitconfig"
    mkdir -p "$HOME" "$SCRATCH/work"
    : > "$GIT_CONFIG_GLOBAL"
    say "  HOME is now $HOME (scratch)"
    say ""
    say "  tq init:"
    tq init "$SCRATCH/work" 2>&1 | sed 's/^/    /'
    say "  tq add acme:"
    tq add acme --git-name "Acme Dev" --git-email dev@acme.test 2>&1 | sed 's/^/    /'
    say "  tq add globex:"
    tq add globex --git-name "Globex Dev" --git-email dev@globex.test 2>&1 | sed 's/^/    /'
    say "  tq list:"
    tq list 2>&1 | sed 's/^/    /'
    say ""
    say "  identity resolution by directory (the core feature):"
    for d in "$SCRATCH/work/acme" "$SCRATCH/work/globex" "$SCRATCH"; do
      say "    cd $d"
      ( cd "$d" && tq env --shell bash 2>&1 | sed 's/^/      /' )
    done
    say ""
    say "  git identity inside each workspace:"
    for d in "$SCRATCH/work/acme" "$SCRATCH/work/globex"; do
      ( cd "$d" && git init -q . 2>/dev/null
        printf '    %-30s %s\n' "$(basename "$d")" "$(git config user.email 2>/dev/null || echo '(none)')" )
    done
    say ""
    say "  tq doctor:"
    tq doctor 2>&1 | sed 's/^/    /'
  fi
fi

# ------------------------------------------------------------- summary -----
hr
say "VERDICT"
hr
case "${CLAUDE_RC:-3}" in
  0) say "Claude: credentials ARE isolated by CLAUDE_CONFIG_DIR."
     say "        macOS behaves like Windows. No Keychain work needed." ;;
  1) say "Claude: credentials are NOT isolated by CLAUDE_CONFIG_DIR."
     say "        Multiple Claude accounts per workspace does NOT work on macOS"
     say "        through config dirs alone. tq needs the Keychain fallback"
     say "        (store a per-workspace token, export CLAUDE_CODE_OAUTH_TOKEN)." ;;
  2) say "Claude: inconclusive — paste the raw output above." ;;
  *) say "Claude: not tested (CLI not installed)." ;;
esac
case "${GH_RC:-3}" in
  0) say "gh:     selection is isolated by GH_CONFIG_DIR (expected)." ;;
  1) say "gh:     reports logged in from an empty dir — worth a closer look." ;;
esac
hr
say "Paste this whole output back into the conversation."
