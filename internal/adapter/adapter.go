package adapter

import (
	"fmt"
	"os"
	"time"
)

// SubmitDelay separates injected text from the Enter that submits it. TUI
// inputs (ink etc.) group bytes arriving in one burst as a paste, turning a
// trailing \r into an inserted newline instead of a submit keypress.
const SubmitDelay = 150 * time.Millisecond

// Adapter abstracts model switching for a wrapped CLI tool.
type Adapter interface {
	// SwitchModel writes the tool-specific model switch command to the PTY.
	SwitchModel(ptmx *os.File, model string) error
	// DetectSwitchDone reports whether the accumulated PTY output since the
	// switch command contains the tool's switch-completion pattern.
	DetectSwitchDone(output []byte) bool
	// Models maps router aliases (e.g. "haiku", "sonnet") to model IDs.
	Models() map[string]string
}

// OutputWaiter lets an adapter wait for patterns in the child's output while
// driving a multi-step switch interaction (e.g. Codex's model picker).
type OutputWaiter interface {
	WaitFor(detect func([]byte) bool, timeout time.Duration) ([]byte, bool)
}

// New returns the adapter for the given target tool. output may be used by
// adapters whose switch flow needs to react to the child's output.
func New(target string, models map[string]string, output OutputWaiter) (Adapter, error) {
	switch target {
	case "claude":
		return &ClaudeAdapter{models: models}, nil
	case "codex":
		return &CodexAdapter{models: models, output: output}, nil
	default:
		return nil, fmt.Errorf("unsupported target %q", target)
	}
}
