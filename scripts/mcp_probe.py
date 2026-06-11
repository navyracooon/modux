#!/usr/bin/env python3
"""Probe codex mcp-server: measure cold call, warm call, and codex-reply."""
import json
import subprocess
import sys
import time

proc = subprocess.Popen(
    ["codex", "mcp-server", "-c", "mcp_servers={}", "-c", 'model_reasoning_effort="low"'],
    stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True,
)

def send(obj):
    proc.stdin.write(json.dumps(obj) + "\n")
    proc.stdin.flush()

def wait_response(rid, timeout=120):
    deadline = time.time() + timeout
    while time.time() < deadline:
        line = proc.stdout.readline()
        if not line:
            sys.exit("server closed stdout")
        try:
            msg = json.loads(line)
        except json.JSONDecodeError:
            continue
        if msg.get("id") == rid and "result" in msg:
            return msg["result"]
    sys.exit(f"timeout waiting for id={rid}")

t0 = time.time()
send({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
    "protocolVersion": "2025-03-26", "capabilities": {},
    "clientInfo": {"name": "modux", "version": "0.1"}}})
wait_response(1)
send({"jsonrpc": "2.0", "method": "notifications/initialized"})
send({"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
tools = wait_response(2)["tools"]
print(f"init+list: {time.time()-t0:.1f}s, tools: {[t['name'] for t in tools]}")
for t in tools:
    if t["name"] == "codex-reply":
        print("codex-reply schema:", json.dumps(t["inputSchema"].get("properties", {}))[:400])

ARGS = {"model": "gpt-5.4-mini", "sandbox": "read-only", "approval-policy": "never",
        "include-plan-tool": False, "cwd": "/tmp"}

def call(rid, name, arguments):
    t = time.time()
    send({"jsonrpc": "2.0", "id": rid, "method": "tools/call",
          "params": {"name": name, "arguments": arguments}})
    res = wait_response(rid)
    text = res["content"][0]["text"] if res.get("content") else "?"
    tid = (res.get("structuredContent") or {}).get("threadId", "")
    print(f"call {rid} ({name}): {time.time()-t:.1f}s → {text!r} thread={tid[:8]}")
    return tid

q = "Respond with exactly one word, lowercase: mini or full. Which tier for: "
tid = call(3, "codex", dict(ARGS, prompt=q + "what does ls do"))
call(4, "codex", dict(ARGS, prompt=q + "design a consensus protocol"))
call(5, "codex-reply", {"threadId": tid, "prompt": q + "fix a typo in README"})
call(6, "codex-reply", {"threadId": tid, "prompt": q + "prove this algorithm correct"})
proc.kill()
