package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/navyracooon/modux/internal/config"
	"github.com/navyracooon/modux/internal/frontend"
)

const usage = `modux — model-optimizing wrapper for claude and codex

Usage:
  modux claude [args...]   Run Claude Code with automatic model selection
  modux codex  [args...]   Run Codex CLI with automatic model selection

Remaining arguments are forwarded to the child CLI unchanged.

Configuration: ~/.config/modux/config.toml
Routing uses the wrapped CLI itself in headless mode, so the CLI's
existing authentication is reused — no separate API key is needed.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return

	case "claude", "codex":
		target := os.Args[1]
		if _, err := exec.LookPath(target); err != nil {
			fmt.Fprintf(os.Stderr, "error: %s not found in PATH\n", target)
			os.Exit(1)
		}
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		code, err := frontend.Run(cfg, target, os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "modux: %v\n", err)
		}
		os.Exit(code)

	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(1)
	}
}
