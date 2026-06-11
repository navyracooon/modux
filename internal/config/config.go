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

// Classifier configures the routing model.
type Classifier struct {
	Model   string `toml:"model"`
	Timeout int    `toml:"timeout"` // milliseconds
}

// TimeoutDuration returns the classifier timeout as a time.Duration.
func (c Classifier) TimeoutDuration() time.Duration {
	return time.Duration(c.Timeout) * time.Millisecond
}

func defaultConfig() *Config {
	return &Config{
		Classifier: Classifier{
			Model:   "claude-haiku-4-5-20251001",
			Timeout: 3000,
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
		cfg.Classifier.Model = file.Classifier.Model
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
