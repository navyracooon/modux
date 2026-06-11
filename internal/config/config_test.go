package config

import (
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func TestParseConfigTOML(t *testing.T) {
	src := `
[classifier]
model   = "claude-haiku-4-5-20251001"
timeout = 3000

[models.claude]
haiku  = "claude-haiku-4-5-20251001"
sonnet = "claude-sonnet-4-6"
opus   = "claude-opus-4-6"

[models.codex]
mini = "codex-mini-latest"
full = "gpt-5"
`
	var cfg Config
	if err := toml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Classifier.Model != "claude-haiku-4-5-20251001" {
		t.Fatalf("classifier model = %q", cfg.Classifier.Model)
	}
	if cfg.Classifier.TimeoutDuration() != 3*time.Second {
		t.Fatalf("timeout = %v", cfg.Classifier.TimeoutDuration())
	}
	if cfg.Models["claude"]["opus"] != "claude-opus-4-6" {
		t.Fatalf("claude models = %v", cfg.Models["claude"])
	}
	if cfg.Models["codex"]["full"] != "gpt-5" {
		t.Fatalf("codex models = %v", cfg.Models["codex"])
	}
}

func TestDefaultsApplied(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Classifier.Timeout != 3000 {
		t.Fatalf("default timeout = %d", cfg.Classifier.Timeout)
	}
	if cfg.Models["claude"]["sonnet"] == "" || cfg.Models["codex"]["mini"] == "" {
		t.Fatalf("default models missing: %v", cfg.Models)
	}
}
