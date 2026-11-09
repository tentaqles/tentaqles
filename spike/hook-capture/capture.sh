#!/usr/bin/env bash
# Hook capture: append one hook payload (read from stdin) as one line to
# <capture-dir>/<EventName>.jsonl.
# Usage: capture.sh <EventName> <capture-dir>
# Always exits 0 and prints nothing, so a capture never disturbs a session.

export LC_ALL=C

payload=$(cat 2>/dev/null)
event="$1"
dir="$2"

case "$event" in
  ""|*[!A-Za-z0-9]*) exit 0 ;;
esac
[ -n "$dir" ] || exit 0
mkdir -p -- "$dir" >/dev/null 2>&1 || exit 0

payload=${payload//$'\r'/ }
payload=${payload//$'\n'/ }
printf '%s\n' "$payload" 2>/dev/null >> "$dir/$event.jsonl"
exit 0
