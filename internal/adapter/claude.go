package adapter

import (
	"fmt"
	"os"
	"regexp"
	"time"
)

// claudeSwitchDoneRE matches Claude Code's confirmation after /model, e.g.
// "⎿ Set model to Haiku 4.5" or "Kept model as opus". \s* between words
// because the TUI renderer may emit cursor moves instead of literal spaces.
var claudeSwitchDoneRE = regexp.MustCompile(`(?i)set\s*model\s*to|kept\s*model\s*as|model\s*set\s*to|switched\s*to`)

// ClaudeAdapter switches models in Claude Code via the /model slash command.
type ClaudeAdapter struct {
	models map[string]string
}

func (a *ClaudeAdapter) SwitchModel(ptmx *os.File, model string) error {
	if _, err := fmt.Fprintf(ptmx, "/model %s", model); err != nil {
		return err
	}
	time.Sleep(SubmitDelay)
	_, err := ptmx.Write([]byte{'\r'})
	return err
}

func (a *ClaudeAdapter) DetectSwitchDone(output []byte) bool {
	return claudeSwitchDoneRE.Match(stripANSI(output))
}

func (a *ClaudeAdapter) Models() map[string]string {
	return a.models
}
