package frontend

import (
	"context"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/navyracooon/modux/internal/adapter"
	"github.com/navyracooon/modux/internal/router"
)

// state is the routing state machine:
// IDLE → ROUTING → SWITCHING → FORWARDING → IDLE
// (timeout/error during ROUTING skips straight to FORWARDING).
type state int

const (
	stateIdle state = iota
	stateRouting
	stateSwitching
	stateForwarding
)

const switchDoneTimeout = 5 * time.Second

// Interceptor sits between the user's stdin and the child PTY. Keystrokes are
// echoed to the PTY as they arrive while a shadow buffer tracks the pending
// prompt; on Enter the buffered prompt is routed before being submitted.
//
// Input arriving while a submission is being processed queues naturally: the
// stdin reader calls HandleInput synchronously and simply does not read more
// until the ROUTING/SWITCHING/FORWARDING cycle completes.
type Interceptor struct {
	ptmx    *os.File
	target  string
	adapter adapter.Adapter
	router  *router.Router
	monitor *Monitor

	state        state
	currentModel string

	buf          []byte
	passThruLine bool // slash command: forward until Enter without routing
	pasteMode    bool // inside a bracketed paste (CSI 200~ … CSI 201~)
	esc          escState
	escSeq       []byte // escape sequence bytes held until the sequence completes
	csiParams    []byte
}

// maxEscSeq bounds the held escape sequence; longer ones (e.g. OSC 52
// clipboard payloads) are forwarded in segments while the parse continues.
const maxEscSeq = 4096

// EscFlushTimeout is how long the input loop waits for the continuation of
// an escape sequence left incomplete at a chunk boundary before treating it
// as a bare Escape keypress. Terminal replies and key sequences arrive in
// one burst — only a human Escape produces a lone ESC followed by silence.
const EscFlushTimeout = 50 * time.Millisecond

// escState tracks progress through an escape sequence so its bytes bypass
// the prompt buffer. Terminal replies to the child's queries (cursor
// position, DA, XTVERSION, …) also arrive on stdin as CSI/DCS sequences and
// must not pollute the prompt.
type escState int

const (
	escNone      escState = iota
	escIntro              // got ESC, expecting the introducer byte
	escCSI                // inside CSI/SS3, ends at final byte 0x40–0x7e
	escString             // inside DCS/OSC/SOS/PM/APC, ends at BEL or ESC \
	escStringEsc          // got ESC inside a string sequence, expecting '\'
)

func NewInterceptor(ptmx *os.File, target string, ad adapter.Adapter, rt *router.Router, mon *Monitor) *Interceptor {
	return &Interceptor{
		ptmx:    ptmx,
		target:  target,
		adapter: ad,
		router:  rt,
		monitor: mon,
	}
}

// HandleInput processes a chunk of raw stdin bytes. If it leaves an escape
// sequence incomplete (PendingEscape), the caller must either feed the next
// chunk or call FlushEscape after EscFlushTimeout of silence.
func (it *Interceptor) HandleInput(chunk []byte) error {
	for i := 0; i < len(chunk); i++ {
		if err := it.handleByte(chunk[i]); err != nil {
			return err
		}
	}
	return nil
}

// PendingEscape reports whether an escape sequence is held incomplete at a
// chunk boundary — terminal replies (cursor position, color queries) can be
// split across reads, so the introducer alone is not enough to decide
// between "sequence start" and "bare Escape keypress".
func (it *Interceptor) PendingEscape() bool {
	return it.esc != escNone
}

// FlushEscape resolves an escape sequence that never got its continuation:
// a lone ESC was a bare Escape keypress — both claude and codex clear
// pending input on Escape, so the shadow buffer is cleared to mirror that —
// and a longer prefix is forwarded as-is.
func (it *Interceptor) FlushEscape() error {
	if it.esc == escNone {
		return nil
	}
	bare := it.esc == escIntro
	it.esc = escNone
	seq := it.escSeq
	it.escSeq = nil
	if bare {
		it.buf = it.buf[:0]
		it.passThruLine = false
	}
	return it.writePTY(seq)
}

func (it *Interceptor) handleByte(b byte) error {
	// Escape sequence bytes bypass the buffer and are held until the
	// sequence completes, then forwarded in a single write: the child's own
	// input parser would misread a sequence split across writes (a lone ESC
	// followed by a pause renders the rest as literal text).
	if it.esc != escNone {
		it.escSeq = append(it.escSeq, b)
		it.advanceEscape(b)
		if it.esc == escNone || len(it.escSeq) >= maxEscSeq {
			seq := it.escSeq
			it.escSeq = it.escSeq[:0]
			return it.writePTY(seq)
		}
		return nil
	}

	// Slash-command line: forward as-is until Enter, skipping routing.
	if it.passThruLine {
		if isEnter(b) {
			it.passThruLine = false
		}
		return it.writePTY([]byte{b})
	}

	// Pasted bytes are content, never commands: \r is a newline in the
	// prompt, not a submit; a leading '/' is text, not a slash command.
	if it.pasteMode && b != 0x1b {
		it.buf = append(it.buf, b)
		return it.writePTY([]byte{b})
	}

	switch {
	case b == 0x1b:
		it.esc = escIntro
		it.escSeq = append(it.escSeq[:0], b)
		return nil

	case isEnter(b):
		return it.handleEnter()

	case isBackspace(b):
		it.dropLastRune()
		return it.writePTY([]byte{b})

	case b == 0x03, b == 0x15: // Ctrl+C / Ctrl+U clear the child's input; mirror that.
		it.buf = it.buf[:0]
		return it.writePTY([]byte{b})

	case b < 0x20: // Ctrl+D and other control bytes: pass through untouched.
		return it.writePTY([]byte{b})

	case len(it.buf) == 0 && b == '/':
		it.passThruLine = true
		return it.writePTY([]byte{b})

	default:
		it.buf = append(it.buf, b)
		return it.writePTY([]byte{b})
	}
}

// handleEnter runs the IDLE → ROUTING → SWITCHING → FORWARDING cycle.
func (it *Interceptor) handleEnter() error {
	prompt := string(it.buf)
	it.buf = it.buf[:0]

	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return it.writePTY([]byte{'\r'})
	}
	if isDigits(trimmed) {
		// Bare digits are picker selections (e.g. Codex's /model list), not
		// a prompt worth routing; they were already echoed — just submit.
		return it.writePTY([]byte{'\r'})
	}

	// ROUTING: ask the classifier which model should handle this prompt.
	// The spinner gives immediate feedback that the Enter was accepted.
	it.state = stateRouting
	msg := "Classifying…"
	warming := !it.router.Ready()
	if warming {
		// The classifier session is still warming up. Route waits for it
		// (the wait is excluded from the classification timeout), so tell
		// the user why this first submission takes longer.
		msg = "Initializing classifier…"
	}
	spinner := startSpinner(os.Stderr, terminalRows(), msg)
	if warming {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			it.router.AwaitReady(ctx)
			spinner.SetMessage("Classifying…")
		}()
	}
	alias, err := it.router.Route(context.Background(), it.target, it.adapter.Models(), prompt)
	it.router.Remember(prompt)

	if err != nil {
		// Timeout or error: keep the current model, forward the prompt as-is.
		spinner.StopWith(1500*time.Millisecond, "routing failed (%v) — keeping current model", err)
		it.state = stateForwarding
		defer func() { it.state = stateIdle }()
		return it.writePTY([]byte{'\r'})
	}

	model := it.adapter.Models()[alias]
	if model == it.currentModel {
		spinner.StopWith(800*time.Millisecond, "model: %s (%s) — already active", alias, model)
		it.state = stateForwarding
		defer func() { it.state = stateIdle }()
		return it.writePTY([]byte{'\r'})
	}

	// Clear the spinner before switching: the child's own /model echo and
	// "Set model to …" confirmation are the durable record of the decision,
	// and anything we leave on screen would be shredded by its repaints.
	spinner.Stop()

	// SWITCHING: clear the echoed prompt from the child's input line, issue
	// the switch command, and wait for the completion pattern. The child's
	// own /model echo and confirmation show the progress from here on.
	it.state = stateSwitching
	if err := it.clearChildInput(prompt); err != nil {
		return err
	}

	// The adapter may drive a multi-step picker that uses the monitor
	// itself, so the completion watch is armed only after SwitchModel
	// returns — the confirmation needs a render cycle, so it cannot appear
	// before the watch is in place.
	if err := it.adapter.SwitchModel(it.ptmx, model); err != nil {
		// Switch failed (e.g. picker did not open): stay on the current
		// model and submit the prompt anyway.
		transientStatus(os.Stderr, terminalRows(), 1500*time.Millisecond,
			"switch failed (%v) — keeping current model", err)
		it.state = stateForwarding
		defer func() { it.state = stateIdle }()
		time.Sleep(adapter.SubmitDelay)
		return it.forwardPrompt(prompt)
	}
	done := it.monitor.Arm(it.adapter.DetectSwitchDone)
	timer := time.NewTimer(switchDoneTimeout)
	defer timer.Stop()
	select {
	case <-done:
		it.currentModel = model
	case <-timer.C:
		if os.Getenv("MODUX_DEBUG") != "" {
			_ = os.WriteFile("/tmp/modux-switch-timeout.bin", it.monitor.Snapshot(), 0o600)
		}
		it.currentModel = model
	}
	it.monitor.Disarm()

	// FORWARDING: re-send the prompt and submit it.
	it.state = stateForwarding
	defer func() { it.state = stateIdle }()
	return it.forwardPrompt(prompt)
}

// forwardPrompt re-sends the prompt and submits it. Multi-line prompts are
// wrapped in bracketed-paste markers so embedded \r insert newlines instead
// of submitting. The Enter is sent as a separate, delayed write so the TUI
// registers it as a keypress rather than part of a paste.
func (it *Interceptor) forwardPrompt(prompt string) error {
	resend := []byte(prompt)
	if strings.ContainsAny(prompt, "\r\n") {
		resend = append(append([]byte("\x1b[200~"), resend...), []byte("\x1b[201~")...)
	}
	if err := it.writePTY(resend); err != nil {
		return err
	}
	time.Sleep(adapter.SubmitDelay)
	return it.writePTY([]byte{'\r'})
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// advanceEscape steps the escape-sequence state machine for one byte.
func (it *Interceptor) advanceEscape(b byte) {
	switch it.esc {
	case escIntro:
		switch b {
		case '[', 'O':
			it.esc = escCSI
			it.csiParams = it.csiParams[:0]
		case 'P', ']', 'X', '^', '_':
			it.esc = escString
		default:
			it.esc = escNone // two-byte sequence (ESC + one char)
		}
	case escCSI:
		if b >= 0x40 && b <= 0x7e {
			it.esc = escNone
			// Track bracketed-paste boundaries (CSI 200~ / CSI 201~).
			if b == '~' {
				switch string(it.csiParams) {
				case "200":
					it.pasteMode = true
				case "201":
					it.pasteMode = false
				}
			}
		} else {
			it.csiParams = append(it.csiParams, b)
		}
	case escString:
		switch b {
		case 0x07: // BEL terminator
			it.esc = escNone
		case 0x1b:
			it.esc = escStringEsc
		}
	case escStringEsc:
		if b == '\\' { // ST = ESC \
			it.esc = escNone
		} else {
			it.esc = escString
		}
	}
}

// clearChildInput erases the prompt that was already echoed into the child's
// input box. Ctrl+U kills the current line atomically — counting backspaces
// per rune is fragile (a single dropped byte leaves a stray character that
// corrupts the /model command). For multi-line input (from a paste), each
// additional line is joined with a backspace and killed in turn.
func (it *Interceptor) clearChildInput(prompt string) error {
	if prompt == "" {
		return nil
	}
	seq := []byte{0x15}
	for i := 0; i < strings.Count(prompt, "\r")+strings.Count(prompt, "\n"); i++ {
		seq = append(seq, 0x7f, 0x15)
	}
	if err := it.writePTY(seq); err != nil {
		return err
	}
	time.Sleep(adapter.SubmitDelay)
	return nil
}

func (it *Interceptor) dropLastRune() {
	if len(it.buf) == 0 {
		return
	}
	_, size := utf8.DecodeLastRune(it.buf)
	if size == 0 {
		size = 1
	}
	it.buf = it.buf[:len(it.buf)-size]
}

func (it *Interceptor) writePTY(p []byte) error {
	_, err := it.ptmx.Write(p)
	return err
}

func isEnter(b byte) bool {
	return b == '\r' || b == '\n'
}

func isBackspace(b byte) bool {
	return b == 0x08 || b == 0x7f
}
