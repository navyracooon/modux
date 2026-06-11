#!/usr/bin/env bash
# Quick health check used during development.
cd ~/projects/modux || exit 1
echo "--- gofmt"
gofmt -l .
echo "--- go test"
go test ./... 2>&1 | tail -6
echo "--- modux process env"
p=$(pgrep -f './modux claude' | head -1)
if [ -n "$p" ]; then
	tr '\0' '\n' </proc/"$p"/environ | grep MODUX || echo "MODUX_DEBUG not set"
else
	echo "modux not running"
fi
echo "--- classifier log"
tail -6 /tmp/modux-classifier.log 2>/dev/null || echo "no classifier log"
