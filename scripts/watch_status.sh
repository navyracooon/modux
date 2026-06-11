#!/usr/bin/env bash
# Send a prompt to the modux e2e session and poll the pane for [modux]
# status lines, printing every distinct one observed.
# Usage: watch_status.sh <prompt> [poll-seconds]
set -u
cd "$(dirname "$0")" || exit 1

prompt="$1"
duration="${2:-12}"

./e2e.sh send "$prompt"
sleep 1
./e2e.sh enter

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
