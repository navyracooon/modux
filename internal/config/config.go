package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the persisted user configuration loaded from
// ~/.config/modux/config.toml.
type Config struct {
	Classifier Classifier                   `toml:"classifier"`
	Models     map[string]map[string]string `toml:"models"`
}

// Classifier configures the routing model. Each wrapped tool classifies with
// its own vendor's model by default, so a codex-only user never needs a
// claude installation (and vice versa). `model` is a global override applied
// to every target; `[classifier.models]` entries override per target.
type Classifier struct {
	Model   string            `toml:"model"`
	Models  map[string]string `toml:"models"`
	Timeout int               `toml:"timeout"` // milliseconds
}

// TimeoutDuration returns the classifier timeout as a time.Duration.
func (c Classifier) TimeoutDuration() time.Duration {
	return time.Duration(c.Timeout) * time.Millisecond
}

// ClassifierModel resolves the classifier model for a target tool:
// [classifier.models] entry → global classifier.model → built-in default.
func (c *Config) ClassifierModel(target string) string {
	if m := c.Classifier.Models[target]; m != "" {
		return m
	}
	if c.Classifier.Model != "" {
		return c.Classifier.Model
	}
	return "claude-haiku-4-5-20251001"
}

func defaultConfig() *Config {
	return &Config{
		Classifier: Classifier{
			Models: map[string]string{
				"claude": "claude-haiku-4-5-20251001",
				"codex":  "gpt-5.4-mini",
			},
			// Generous default: a codex classification is ~4s warm and
			// ~12s on the very first (still-initializing) call.
			Timeout: 15000,
		},
		Models: map[string]map[string]string{
			"claude": {
				"haiku":  "claude-haiku-4-5-20251001",
				"sonnet": "claude-sonnet-4-6",
				"opus":   "claude-opus-4-6",
			},
			"codex": {
				"mini": "gpt-5.4-mini",
				"full": "gpt-5.5",
			},
		},
	}
}

// Path returns the config file location.
func Path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "modux", "config.toml")
}

// Load reads the config file, falling back to defaults for anything unset.
// A missing file is not an error; a malformed file is.
func Load() (*Config, error) {
	cfg := defaultConfig()

	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read %s: %w", Path(), err)
	}

	var file Config
	if err := toml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", Path(), err)
	}

	if file.Classifier.Model != "" {
		// An explicit global model replaces the built-in per-target defaults;
		// explicit [classifier.models] entries below still win per target.
		cfg.Classifier.Model = file.Classifier.Model
		cfg.Classifier.Models = map[string]string{}
	}
	for target, model := range file.Classifier.Models {
		if model != "" {
			cfg.Classifier.Models[target] = model
		}
	}
	if file.Classifier.Timeout > 0 {
		cfg.Classifier.Timeout = file.Classifier.Timeout
	}
	for target, models := range file.Models {
		if len(models) > 0 {
			cfg.Models[target] = models
		}
	}

	return cfg, nil
}
