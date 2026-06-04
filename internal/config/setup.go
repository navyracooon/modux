package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// File holds the persisted user configuration.
type File struct {
	Target          string `json:"target"`            // "claude" | "codex"
	ClassifierModel string `json:"classifier_model"`
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "modux", "config.json")
}

// LoadFile reads the saved config. Returns nil if not found.
func LoadFile() *File {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return nil
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil
	}
	return &f
}

// SaveFile writes the config to disk.
func SaveFile(f *File) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// RunSetup runs the interactive first-time configuration wizard.
func RunSetup() (*File, error) {
	r := bufio.NewReader(os.Stdin)

	printBanner()

	target, err := choose(r,
		"Which CLI do you want to use?",
		[]string{"claude", "codex"},
		[]string{"Claude Code (Anthropic)", "Codex CLI (OpenAI)"},
	)
	if err != nil {
		return nil, err
	}

	var defaultClassifier string
	var classifierChoices, classifierLabels []string

	if target == "claude" {
		defaultClassifier = "claude-haiku-4-5-20251001"
		classifierChoices = []string{
			"claude-haiku-4-5-20251001",
			"claude-sonnet-4-6",
		}
		classifierLabels = []string{
			"Haiku 4.5  (fastest / cheapest — recommended)",
			"Sonnet 4.6 (more accurate classification)",
		}
	} else {
		defaultClassifier = "gpt-4o-mini"
		classifierChoices = []string{"gpt-4o-mini", "gpt-4o"}
		classifierLabels = []string{
			"gpt-4o-mini (fastest / cheapest — recommended)",
			"gpt-4o      (more accurate classification)",
		}
	}

	_ = defaultClassifier
	classifierModel, err := choose(r,
		"Which model should be used for prompt classification?",
		classifierChoices,
		classifierLabels,
	)
	if err != nil {
		return nil, err
	}

	f := &File{Target: target, ClassifierModel: classifierModel}
	if err := SaveFile(f); err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("\n\x1b[92m✓\x1b[0m Config saved to %s\n\n", configPath())
	return f, nil
}

func printBanner() {
	fmt.Print("\x1b[2J\x1b[H") // clear screen
	fmt.Println("\x1b[96;1m╭──────────────────────────────────╮\x1b[0m")
	fmt.Println("\x1b[96;1m│\x1b[0m  \x1b[97;1mmodux\x1b[0m  model-optimizing wrapper \x1b[96;1m│\x1b[0m")
	fmt.Println("\x1b[96;1m╰──────────────────────────────────╯\x1b[0m")
	fmt.Println()
}

// choose presents numbered options and returns the selected value.
func choose(r *bufio.Reader, question string, values, labels []string) (string, error) {
	fmt.Printf("\x1b[97;1m%s\x1b[0m\n", question)
	for i, label := range labels {
		fmt.Printf("  \x1b[36m%d\x1b[0m  %s\n", i+1, label)
	}
	fmt.Print("\n\x1b[37m›\x1b[0m ")

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		for i := range values {
			if line == fmt.Sprintf("%d", i+1) || strings.EqualFold(line, values[i]) {
				fmt.Println()
				return values[i], nil
			}
		}
		fmt.Printf("\x1b[33m  Enter a number (1–%d):\x1b[0m ", len(values))
	}
}
