#!/usr/bin/env bash
# Time the codex headless classifier call with MCP disabled.
set -u
prompt='Respond with exactly one word: mini or full. Which tier for: what does ls do'
echo '--- with mcp_servers={} and low effort:'
time codex exec --skip-git-repo-check -c 'mcp_servers={}' -c 'model_reasoning_effort="low"' -m gpt-5.4-mini "$prompt" </dev/null 2>/dev/null | tail -2
