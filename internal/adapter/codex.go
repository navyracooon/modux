package adapter

import (
	"fmt"
	"os"
	"regexp"
	"time"
)

// codexSwitchDoneRE matches Codex CLI's confirmation after the model picker
// closes, e.g. "• Model changed to gpt-5.4-mini medium". \s* between words
// because the TUI renderer may emit cursor moves instead of literal spaces.
var (
	codexSwitchDoneRE   = regexp.MustCompile(`(?i)model\s*changed`)
	codexEffortScreenRE = regexp.MustCompile(`Select\s*Reasoning\s*Level`)
)

const codexPickerTimeout = 3 * time.Second

// CodexAdapter switches models in Codex CLI. Codex's /model does not accept
// an argument (a "/model name" line is sent to the LLM as chat), so the
// switch walks the interactive picker instead:
//
//	/model⏎ → "Select Model and Effort" list → digit selects the target row
//	→ "Select Reasoning Level" list → ⏎ keeps the highlighted effort
//	→ "Model changed to …" confirms.
type CodexAdapter struct {
	models map[string]string
	output OutputWaiter
}

func (a *CodexAdapter) SwitchModel(ptmx *os.File, model string) error {
	if a.output == nil {
		return fmt.Errorf("codex adapter needs an output waiter to drive the model picker")
	}

	// Open the picker. The Enter is paced so the TUI sees a keypress, not a
	// paste.
	if _, err := ptmx.WriteString("/model"); err != nil {
		return err
	}
	time.Sleep(SubmitDelay)
	if _, err := ptmx.Write([]byte{'\r'}); err != nil {
		return err
	}

	// Wait until the picker row for the target model is on screen, then jump
	// to it by its number (digits select and advance immediately).
	out, ok := a.output.WaitFor(func(b []byte) bool {
		_, found := codexModelDigit(b, model)
		return found
	}, codexPickerTimeout)
	if !ok {
		debugDump("codex-picker", out)
		_, _ = ptmx.Write([]byte{0x1b})
		return fmt.Errorf("model %s not offered by the picker", model)
	}
	digit, _ := codexModelDigit(out, model)
	if _, err := ptmx.Write([]byte{digit}); err != nil {
		return err
	}

	// Keep the highlighted reasoning effort on the follow-up screen.
	if out, ok := a.output.WaitFor(func(b []byte) bool {
		return codexEffortScreenRE.Match(stripANSI(b))
	}, codexPickerTimeout); !ok {
		debugDump("codex-effort", out)
		_, _ = ptmx.Write([]byte{0x1b})
		return fmt.Errorf("reasoning level screen did not appear")
	}
	time.Sleep(SubmitDelay)
	_, err := ptmx.Write([]byte{'\r'})
	return err
}

// debugDump writes the raw output a failed stage was matching against to
// /tmp for diagnosis. Active only with MODUX_DEBUG set.
func debugDump(stage string, raw []byte) {
	if os.Getenv("MODUX_DEBUG") == "" {
		return
	}
	_ = os.WriteFile("/tmp/modux-"+stage+".bin", raw, 0o600)
}

func (a *CodexAdapter) DetectSwitchDone(output []byte) bool {
	return codexSwitchDoneRE.Match(stripANSI(output))
}

func (a *CodexAdapter) Models() map[string]string {
	return a.models
}

// codexModelDigit finds the picker row number for the given model in raw TUI
// output, e.g. "5. gpt-5.4-mini   Small, fast, …" → '5'. Only single-digit
// rows are supported, which covers the picker's handful of entries.
//
// The picker positions columns with cursor jumps, so after stripping ANSI
// the name and its description are joined without spaces ("2.gpt-5.5Frontier
// model …"). The terminating boundary is therefore "anything that cannot
// continue a model ID" (IDs are lowercase/digits/dots/dashes; descriptions
// start with an uppercase letter or parenthesis).
func codexModelDigit(raw []byte, model string) (byte, bool) {
	re := regexp.MustCompile(`(\d+)\.\s*` + regexp.QuoteMeta(model) + `([^a-z0-9.-]|$)`)
	m := re.FindSubmatch(stripANSI(raw))
	if m == nil || len(m[1]) != 1 {
		return 0, false
	}
	return m[1][0], true
}
