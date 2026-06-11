package router

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestParseAlias(t *testing.T) {
	names := []string{"haiku", "sonnet", "opus"}

	tests := []struct {
		output string
		want   string
		ok     bool
	}{
		{"sonnet", "sonnet", true},
		{"  Opus  \n", "opus", true},
		{"banner line\nworkdir: /tmp\nhaiku\n", "haiku", true},
		{"The best tier is sonnet.", "sonnet", true},
		{"haiku（ハイク）", "haiku", true},
		{"haiku and sonnet both fit\nopus（オーパス）", "opus", true},
		{"no answer here", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := parseAlias(tt.output, names)
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseAlias(%q) = (%q, %v), want (%q, %v)", tt.output, got, ok, tt.want, tt.ok)
		}
	}
}

func TestParseAliasPrefersLaterLines(t *testing.T) {
	// Headers may echo model IDs; the answer at the end must win.
	names := []string{"mini", "full"}
	got, ok := parseAlias("model: codex-mini-latest\n--------\nfull\n", names)
	if !ok || got != "full" {
		t.Fatalf("parseAlias = (%q, %v), want (full, true)", got, ok)
	}
}

func TestAliasNamesOrderedCheapestFirst(t *testing.T) {
	names := aliasNames(map[string]string{
		"opus":   "claude-opus-4-6",
		"haiku":  "claude-haiku-4-5-20251001",
		"sonnet": "claude-sonnet-4-6",
	})
	if !reflect.DeepEqual(names, []string{"haiku", "sonnet", "opus"}) {
		t.Fatalf("aliasNames = %v", names)
	}
}

func TestClassifierCommandPicksCLIByModelVendor(t *testing.T) {
	cmd := classifierCommand(context.Background(), "claude-haiku-4-5-20251001", "p")
	if filepath.Base(cmd.Args[0]) != "claude" {
		t.Fatalf("claude model should run via claude CLI, got %v", cmd.Args)
	}
	cmd = classifierCommand(context.Background(), "gpt-5.4-mini", "p")
	if filepath.Base(cmd.Args[0]) != "codex" {
		t.Fatalf("gpt model should run via codex CLI, got %v", cmd.Args)
	}
}

func TestRememberKeepsRecentHistory(t *testing.T) {
	r := New("m", time.Second)
	for i := 0; i < historySize+2; i++ {
		r.Remember("prompt")
	}
	if len(r.history) != historySize {
		t.Fatalf("history length = %d, want %d", len(r.history), historySize)
	}
}
