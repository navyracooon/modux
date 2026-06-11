package config

import (
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func TestParseConfigTOML(t *testing.T) {
	src := `
[classifier]
timeout = 3000

[classifier.models]
claude = "claude-haiku-4-5-20251001"
codex  = "gpt-5.4-mini"

[models.claude]
haiku  = "claude-haiku-4-5-20251001"
sonnet = "claude-sonnet-4-6"
opus   = "claude-opus-4-6"

[models.codex]
mini = "gpt-5.4-mini"
full = "gpt-5.5"
`
	var cfg Config
	if err := toml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Classifier.Models["codex"] != "gpt-5.4-mini" {
		t.Fatalf("classifier models = %v", cfg.Classifier.Models)
	}
	if cfg.Classifier.TimeoutDuration() != 3*time.Second {
		t.Fatalf("timeout = %v", cfg.Classifier.TimeoutDuration())
	}
	if cfg.Models["claude"]["opus"] != "claude-opus-4-6" {
		t.Fatalf("claude models = %v", cfg.Models["claude"])
	}
	if cfg.Models["codex"]["full"] != "gpt-5.5" {
		t.Fatalf("codex models = %v", cfg.Models["codex"])
	}
}

func TestDefaultsApplied(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Classifier.Timeout != 15000 {
		t.Fatalf("default timeout = %d", cfg.Classifier.Timeout)
	}
	if cfg.Models["claude"]["sonnet"] == "" || cfg.Models["codex"]["mini"] == "" {
		t.Fatalf("default models missing: %v", cfg.Models)
	}
}

func TestClassifierModelResolution(t *testing.T) {
	// Defaults: each target classifies with its own vendor.
	cfg := defaultConfig()
	if got := cfg.ClassifierModel("claude"); got != "claude-haiku-4-5-20251001" {
		t.Fatalf("claude classifier = %q", got)
	}
	if got := cfg.ClassifierModel("codex"); got != "gpt-5.4-mini" {
		t.Fatalf("codex classifier = %q", got)
	}

	// A global model override applies to every target (e.g. a user with both
	// subscriptions routing codex through the faster claude classifier).
	cfg = defaultConfig()
	cfg.Classifier.Model = "claude-haiku-4-5-20251001"
	cfg.Classifier.Models = map[string]string{}
	if got := cfg.ClassifierModel("codex"); got != "claude-haiku-4-5-20251001" {
		t.Fatalf("global override not applied: %q", got)
	}

	// A per-target entry beats the global override.
	cfg.Classifier.Models["codex"] = "gpt-5.4-mini"
	if got := cfg.ClassifierModel("codex"); got != "gpt-5.4-mini" {
		t.Fatalf("per-target override not applied: %q", got)
	}

	// Unknown target falls back to the safest default.
	cfg = defaultConfig()
	if got := cfg.ClassifierModel("other"); got != "claude-haiku-4-5-20251001" {
		t.Fatalf("fallback = %q", got)
	}
}
