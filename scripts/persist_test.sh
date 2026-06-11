#!/usr/bin/env bash
# Measure per-turn latency of a persistent claude stream-json session.
set -u
msg() { printf '{"type":"user","message":{"role":"user","content":[{"type":"text","text":"%s"}]}}\n' "$1"; }
{
  msg "Respond with exactly one word, lowercase: haiku, sonnet, or opus. Which tier for: what does the ls command do"
  sleep 8
  msg "Respond with exactly one word, lowercase: haiku, sonnet, or opus. Which tier for: design a distributed consensus protocol with formal proofs"
  sleep 8
} | claude -p --model claude-haiku-4-5-20251001 --strict-mcp-config \
      --input-format stream-json --output-format stream-json --verbose 2>&1 |
  while IFS= read -r line; do
    printf '[%s] %s\n' "$(date +%H:%M:%S.%3N)" "$(echo "$line" | head -c 240)"
  done
