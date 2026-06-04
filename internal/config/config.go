package config

import (
	"os"
)

type Config struct {
	Target          string
	Args            []string
	ClassifierModel string
	AnthropicAPIKey string
	OpenAIAPIKey    string
}

// Load builds a runtime Config from saved file + env overrides.
// target/args can be empty strings to signal "use saved config".
func Load(target string, args []string) *Config {
	saved := LoadFile()

	// Resolve target: explicit arg > saved config > ""
	if target == "" && saved != nil {
		target = saved.Target
	}

	classifierModel := os.Getenv("MODUX_CLASSIFIER_MODEL")
	if classifierModel == "" && saved != nil {
		classifierModel = saved.ClassifierModel
	}
	if classifierModel == "" {
		classifierModel = "claude-haiku-4-5-20251001"
	}

	return &Config{
		Target:          target,
		Args:            args,
		ClassifierModel: classifierModel,
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		OpenAIAPIKey:    os.Getenv("OPENAI_API_KEY"),
	}
}
