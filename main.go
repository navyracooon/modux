package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/navyracooon/modux/internal/config"
	"github.com/navyracooon/modux/internal/terminal"
)

const usage = `modux — model-optimizing wrapper for claude and codex

Usage:
  modux                    Launch using saved config (runs setup on first use)
  modux claude [args...]   Run claude with automatic model selection
  modux codex  [args...]   Run codex with automatic model selection
  modux config             Re-run the interactive setup wizard

Environment variables:
  ANTHROPIC_API_KEY        Required for claude and the classifier
  OPENAI_API_KEY           Required for codex
  MODUX_CLASSIFIER_MODEL   Override the classifier model
`

func main() {
	// No args → use saved config, or run setup if first time.
	if len(os.Args) < 2 {
		runWithSavedConfig()
		return
	}

	switch os.Args[1] {
	case "config":
		if _, err := config.RunSetup(); err != nil {
			fmt.Fprintf(os.Stderr, "setup failed: %v\n", err)
			os.Exit(1)
		}
		return

	case "claude", "codex":
		target := os.Args[1]
		if _, err := findBinary(target); err != nil {
			fmt.Fprintf(os.Stderr, "error: %s not found in PATH\n", target)
			os.Exit(1)
		}
		cfg := config.Load(target, os.Args[2:])
		run(cfg)

	case "-h", "--help", "help":
		fmt.Print(usage)

	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(1)
	}
}

func runWithSavedConfig() {
	saved := config.LoadFile()
	if saved == nil {
		// First run — prompt for setup.
		var err error
		saved, err = config.RunSetup()
		if err != nil {
			fmt.Fprintf(os.Stderr, "setup failed: %v\n", err)
			os.Exit(1)
		}
	}

	if _, err := findBinary(saved.Target); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s not found in PATH\n", saved.Target)
		os.Exit(1)
	}

	cfg := config.Load(saved.Target, nil)
	run(cfg)
}

func run(cfg *config.Config) {
	if cfg.AnthropicAPIKey == "" {
		fmt.Fprintln(os.Stderr, "error: ANTHROPIC_API_KEY not set (required for the classifier)")
		os.Exit(1)
	}
	if err := terminal.Run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "modux: %v\n", err)
		os.Exit(1)
	}
}

func findBinary(name string) (string, error) {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s not found", name)
}
