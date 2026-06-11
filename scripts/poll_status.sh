#!/usr/bin/env bash
# Poll the modux e2e pane for [modux] status lines without sending anything.
# Usage: poll_status.sh [seconds]
set -u
duration="${1:-10}"
out=$(mktemp)
end=$((SECONDS + duration))
while [ $SECONDS -lt "$end" ]; do
	tmux capture-pane -p -t modux-e2e | grep -a 'modux\]' >>"$out" 2>/dev/null
	sleep 0.3
done
echo "--- observed [modux] lines:"
sort -u "$out"
rm -f "$out"
echo "--- final screen tail:"
tmux capture-pane -p -t modux-e2e | grep -v '^\s*$' | tail -4
