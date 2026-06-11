#!/usr/bin/env bash
# tmux-based E2E driver for modux. Runs `modux <target>` in a detached tmux
# session so output can be captured and keystrokes injected for verification.
#
# Usage:
#   e2e.sh start [target]   Launch modux in a detached session (default: claude)
#   e2e.sh send <text>      Type literal text into the session (no Enter)
#   e2e.sh enter            Press Enter
#   e2e.sh key <key>        Send a tmux key name (e.g. Escape, C-c)
#   e2e.sh capture          Dump the visible pane
#   e2e.sh stop             Kill the session
set -u

SESSION=modux-e2e

case "${1:-}" in
start)
	target="${2:-claude}"
	tmux kill-session -t "$SESSION" 2>/dev/null
	tmux new-session -d -s "$SESSION" -x 180 -y 45
	tmux send-keys -t "$SESSION" -l "cd ~/projects/modux && MODUX_DEBUG=1 ./modux $target"
	tmux send-keys -t "$SESSION" Enter
	;;
send)
	shift
	tmux send-keys -t "$SESSION" -l "$*"
	;;
enter)
	tmux send-keys -t "$SESSION" Enter
	;;
key)
	tmux send-keys -t "$SESSION" "$2"
	;;
capture)
	tmux capture-pane -p -t "$SESSION"
	;;
stop)
	tmux kill-session -t "$SESSION" 2>/dev/null
	;;
*)
	echo "usage: $0 {start|send|enter|key|capture|stop}" >&2
	exit 1
	;;
esac
