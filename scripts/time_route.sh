#!/usr/bin/env bash
# Send a prompt through the running e2e session and time Enter → switch
# confirmation. Matches only lines that were NOT on screen before Enter,
# so stale confirmations from earlier turns cannot satisfy the wait.
set -u
cd "$(dirname "$0")/.."
prompt=${1:?usage: time_route.sh "prompt" [pattern]}
pattern=${2:-'Set model to|Kept model|already active|routing failed'}

./scripts/e2e.sh send "$prompt"
sleep 0.3
baseline=$(./scripts/e2e.sh capture | grep -cE "$pattern")
./scripts/e2e.sh enter
start=$(date +%s%3N)
for _ in $(seq 1 120); do
  cap=$(./scripts/e2e.sh capture)
  count=$(echo "$cap" | grep -cE "$pattern")
  if [ "$count" -gt "$baseline" ]; then
    end=$(date +%s%3N)
    echo "matched after $(( end - start ))ms:"
    echo "$cap" | grep -E "$pattern" | tail -2
    exit 0
  fi
  sleep 0.25
done
echo "TIMEOUT waiting for: $pattern"
./scripts/e2e.sh capture | tail -15
exit 1
