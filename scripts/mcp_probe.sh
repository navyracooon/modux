#!/usr/bin/env bash
# Probe codex mcp-server: handshake, list tools, and time two tool calls.
set -u
{
  printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"modux","version":"0.1"}}}'
  sleep 1
  printf '%s\n' '{"jsonrpc":"2.0","method":"notifications/initialized"}'
  printf '%s\n' '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
  sleep 2
  printf '%s\n' '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"codex","arguments":{"prompt":"Respond with exactly one word, lowercase: mini or full. Which tier for: what does the ls command do","model":"gpt-5.4-mini","sandbox":"read-only","approval-policy":"never","include-plan-tool":false,"cwd":"/tmp"}}}'
  sleep 30
  printf '%s\n' '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"codex","arguments":{"prompt":"Respond with exactly one word, lowercase: mini or full. Which tier for: architect a byzantine fault tolerant consensus protocol","model":"gpt-5.4-mini","sandbox":"read-only","approval-policy":"never","include-plan-tool":false,"cwd":"/tmp"}}}'
  sleep 30
} | codex mcp-server -c 'mcp_servers={}' -c 'model_reasoning_effort="low"' 2>/dev/null |
  while IFS= read -r line; do
    printf '[%s] %s\n' "$(date +%H:%M:%S.%3N)" "$(echo "$line" | head -c 400)"
  done
