#!/usr/bin/env bash
# This repo's round gate: fast, and only what the ticket touched (the owner's speed
# rule). The full suites run once, at the release gate. See gates.release in project.json.
set -u
fail=0
run() { echo "+ $*"; "$@" || fail=1; }

base=$(node -e 'try{process.stdout.write(JSON.parse(require("fs").readFileSync(".job/ticket.json","utf8")).base)}catch{}')
[ -n "$base" ] || base=$(git merge-base HEAD main)
changed=$(git diff --name-only "$base"...HEAD)

run go build ./...
run go vet ./...

gofiles=$(echo "$changed" | grep '\.go$' || true)
if [ -n "$gofiles" ]; then
  # shellcheck disable=SC2086
  existing=$(ls $gofiles 2>/dev/null || true)
  if [ -n "$existing" ]; then
    # shellcheck disable=SC2086
    bad=$(gofmt -l $existing)
    if [ -n "$bad" ]; then echo "gofmt needed: $bad"; fail=1; fi
  fi
  # A package whose files were all deleted has no folder left to test.
  pkgs=$(echo "$gofiles" | xargs -n1 dirname | sort -u | while read -r p; do [ -d "$p" ] && echo "./$p"; done)
  # internal/cmd holds the hook and every command: always in the round.
  # shellcheck disable=SC2086
  run go test $pkgs ./internal/cmd -count=1
fi

pytests=$(echo "$changed" | grep -E '^plugin/tests/.*\.py$' || true)
if [ -n "$pytests" ]; then
  # shellcheck disable=SC2086
  PYTHONPATH=plugin run python -m pytest $pytests -q
fi

packtests=$(echo "$changed" | grep -E '^\.claude/' || true)
if [ -n "$packtests" ]; then
  run node --test .claude/tests/gate.test.mjs .claude/tests/blind-guard.test.mjs .claude/tests/check.test.mjs
fi

exit $fail
