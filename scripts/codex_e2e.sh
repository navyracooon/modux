#!/usr/bin/env bash
# E2E: send a prompt to the running modux codex session, report the spinner
# message seen during routing and the eventual model switch.
set -u
cd "$(dirname "$0")/.."
prompt=${1:?usage: codex_e2e.sh "prompt"}

./scripts/e2e.sh send "$prompt"
sleep 0.3
base=$(./scripts/e2e.sh capture | grep -ciE 'model changed')
./scripts/e2e.sh enter
start=$(date +%s%3N)

spin=""
for _ in $(seq 1 200); do
  cap=$(./scripts/e2e.sh capture)
  s=$(echo "$cap" | grep -oiE 'initializing classifier|classifying' | head -1)
  [ -n "$s" ] && spin="$s"
  now=$(echo "$cap" | grep -ciE 'model changed')
  if [ "$now" -gt "$base" ]; then
    end=$(date +%s%3N)
    echo "spinner seen: ${spin:-none}"
    echo "switched after $(( end - start ))ms:"
    echo "$cap" | grep -iE 'model changed' | tail -1
    exit 0
  fi
  if echo "$cap" | grep -qiE 'already active|routing failed|switch failed'; then
    end=$(date +%s%3N)
    echo "spinner seen: ${spin:-none}"
    echo "status after $(( end - start ))ms:"
    echo "$cap" | grep -iE 'already active|routing failed|switch failed' | tail -1
    exit 0
  fi
  sleep 0.2
done
echo "TIMEOUT; last screen:"
./scripts/e2e.sh capture | tail -12
exit 1
