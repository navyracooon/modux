package adapter

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStripANSI(t *testing.T) {
	in := []byte("\x1b[1m⎿ Set model to \x1b[36msonnet\x1b[0m (claude-sonnet-4-6)\x1b]0;title\x07 done")
	got := string(stripANSI(in))
	want := "⎿ Set model to sonnet (claude-sonnet-4-6) done"
	if got != want {
		t.Fatalf("stripANSI = %q, want %q", got, want)
	}
}

func TestClaudeDetectSwitchDone(t *testing.T) {
	a := &ClaudeAdapter{}
	if a.DetectSwitchDone([]byte("/model claude-sonnet-4-6")) {
		t.Fatal("command echo alone must not count as completion")
	}
	if !a.DetectSwitchDone([]byte("\x1b[2m⎿ \x1b[0mSet model to \x1b[1msonnet\x1b[0m (claude-sonnet-4-6)")) {
		t.Fatal("styled confirmation should be detected")
	}
	if !a.DetectSwitchDone([]byte("Kept model as opus")) {
		t.Fatal("kept-model confirmation should be detected")
	}
}

func TestCodexDetectSwitchDone(t *testing.T) {
	a := &CodexAdapter{}
	if a.DetectSwitchDone([]byte("/model gpt-5.4-mini")) {
		t.Fatal("command echo alone must not count as completion")
	}
	if !a.DetectSwitchDone([]byte("• Model changed to gpt-5.4-mini medium")) {
		t.Fatal("confirmation should be detected")
	}
}

func TestCodexModelDigit(t *testing.T) {
	// Real picker output: columns are placed with cursor jumps, so the
	// stripped text concatenates name and description without spaces.
	picker := []byte("Select Model and Effort" +
		"\x1b[23;3HAccess legacy models by running codex -m <model_name> or in your config.toml" +
		"\x1b[25;3H1.\x1b[25;6Hgpt-5.3-codex\x1b[25;20H(default)\x1b[25;31HCoding-optimized model." +
		"\x1b[26;3H2.\x1b[26;6Hgpt-5.5\x1b[26;31HFrontier model for complex coding, research, and real-world work." +
		"\x1b[27;3H3.\x1b[27;6Hgpt-5.2\x1b[27;31HOptimized for professional work and long-running agents." +
		"\x1b[28;3H4.\x1b[28;6Hgpt-5.4\x1b[28;31HStrong model for everyday coding." +
		"\x1b[29;1H› 5. gpt-5.4-mini (current)   Small, fast, and cost-efficient model for simpler coding tasks." +
		"\x1b[31;3HPress enter to confirm or esc to go back")
	tests := []struct {
		model string
		digit byte
	}{
		{"gpt-5.3-codex", '1'},
		{"gpt-5.5", '2'},
		{"gpt-5.2", '3'},
		{"gpt-5.4", '4'}, // must not match the gpt-5.4-mini row
		{"gpt-5.4-mini", '5'},
	}
	for _, tt := range tests {
		if d, ok := codexModelDigit(picker, tt.model); !ok || d != tt.digit {
			t.Errorf("%s → (%q, %v), want (%q, true)", tt.model, d, ok, tt.digit)
		}
	}
	if _, ok := codexModelDigit(picker, "gpt-9"); ok {
		t.Error("unknown model must not match")
	}
}

// fakeWaiter returns scripted screens in order; WaitFor reports whether the
// detector matched the screen it returned.
type fakeWaiter struct {
	screens [][]byte
}

func (f *fakeWaiter) WaitFor(detect func([]byte) bool, _ time.Duration) ([]byte, bool) {
	if len(f.screens) == 0 {
		return nil, false
	}
	s := f.screens[0]
	f.screens = f.screens[1:]
	return s, detect(s)
}

func TestCodexSwitchModelWalksPicker(t *testing.T) {
	picker := []byte("Select Model and Effort" +
		"\x1b[26;3H2.\x1b[26;6Hgpt-5.5\x1b[26;31HFrontier model for complex coding.")
	effort := []byte("Select Reasoning Level\n› 2. high")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	a := &CodexAdapter{output: &fakeWaiter{screens: [][]byte{picker, effort}}}
	if err := a.SwitchModel(w, "gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	w.Close()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	// Open the picker, jump to the row by digit, accept the highlighted effort.
	if want := "/model\r2\r"; string(got) != want {
		t.Fatalf("keystrokes = %q, want %q", got, want)
	}
}

func TestCodexSwitchModelBusyDoesNotEscape(t *testing.T) {
	// Codex rejects /model while generating. No picker opened, so Esc must
	// NOT be sent — it would cancel the running task.
	busy := []byte("\x1b[38;5;1m⚠  '/model' is disabled while a task is in progress.\x1b[39m")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	a := &CodexAdapter{output: &fakeWaiter{screens: [][]byte{busy}}}
	err = a.SwitchModel(w, "gpt-5.5")
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("err = %v, want busy", err)
	}
	w.Close()

	got, rerr := io.ReadAll(r)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if want := "/model\r"; string(got) != want {
		t.Fatalf("keystrokes = %q, want %q (no Esc while busy)", got, want)
	}
}

func TestCodexSwitchModelRejectsUnknownModel(t *testing.T) {
	picker := []byte("Select Model and Effort\x1b[26;3H2.\x1b[26;6Hgpt-5.5\x1b[26;31HFrontier")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	a := &CodexAdapter{output: &fakeWaiter{screens: [][]byte{picker}}}
	if err := a.SwitchModel(w, "gpt-9"); err == nil {
		t.Fatal("unknown model must fail (and Esc out of the picker)")
	}
}

func TestNew(t *testing.T) {
	models := map[string]string{"haiku": "claude-haiku-4-5-20251001"}
	a, err := New("claude", models, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Models()["haiku"] != "claude-haiku-4-5-20251001" {
		t.Fatalf("Models() = %v", a.Models())
	}
	if _, err := New("unknown", nil, nil); err == nil {
		t.Fatal("unknown target should error")
	}
}

func TestSwitchModelWritesSlashCommand(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	a := &ClaudeAdapter{}
	if err := a.SwitchModel(w, "claude-opus-4-6"); err != nil {
		t.Fatal(err)
	}
	w.Close()

	buf := make([]byte, 64)
	n, _ := r.Read(buf)
	if got, want := string(buf[:n]), "/model claude-opus-4-6\r"; got != want {
		t.Fatalf("wrote %q, want %q", got, want)
	}
}
