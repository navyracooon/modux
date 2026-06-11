package frontend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRouter scripts the classifier's answer.
type fakeRouter struct {
	alias string
	err   error

	mu         sync.Mutex
	routed     []string
	remembered []string
}

func (f *fakeRouter) Route(_ context.Context, _ string, _ map[string]string, prompt string) (string, error) {
	f.mu.Lock()
	f.routed = append(f.routed, prompt)
	f.mu.Unlock()
	return f.alias, f.err
}
func (f *fakeRouter) Remember(p string) {
	f.mu.Lock()
	f.remembered = append(f.remembered, p)
	f.mu.Unlock()
}
func (f *fakeRouter) Ready() bool                  { return true }
func (f *fakeRouter) AwaitReady(_ context.Context) {}

// fakeSwitchAdapter mimics a CLI whose switch confirmation is the marker
// string in the child's output stream.
type fakeSwitchAdapter struct {
	models     map[string]string
	failSwitch bool

	mu       sync.Mutex
	switched []string
}

const switchMarker = "SWITCH-CONFIRMED"

func (f *fakeSwitchAdapter) SwitchModel(ptmx *os.File, model string) error {
	if f.failSwitch {
		return errors.New("picker did not open")
	}
	f.mu.Lock()
	f.switched = append(f.switched, model)
	f.mu.Unlock()
	_, err := fmt.Fprintf(ptmx, "/model %s\r", model)
	return err
}
func (f *fakeSwitchAdapter) DetectSwitchDone(out []byte) bool {
	return bytes.Contains(out, []byte(switchMarker))
}
func (f *fakeSwitchAdapter) Models() map[string]string { return f.models }

// routingHarness wires an Interceptor to pipes standing in for the child
// PTY (written by the interceptor) and its output stream (read by the
// monitor), with all user-visible status output discarded and hold timers
// shrunk so a full cycle runs in well under a second.
type routingHarness struct {
	it      *Interceptor
	router  *fakeRouter
	adapter *fakeSwitchAdapter

	childIn  *os.File // read side of what the interceptor writes to the child
	childOut *os.File // write side of the child's output, feeding the monitor
}

func newRoutingHarness(t *testing.T, rt *fakeRouter, ad *fakeSwitchAdapter) *routingHarness {
	t.Helper()

	for _, v := range []struct {
		v    *time.Duration
		fast time.Duration
	}{
		{&switchDoneTimeout, 500 * time.Millisecond},
		{&routeFailHold, time.Millisecond},
		{&alreadyActiveHold, time.Millisecond},
		{&switchFailHold, time.Millisecond},
	} {
		orig := *v.v
		*v.v = v.fast
		t.Cleanup(func() { *v.v = orig })
	}

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inR.Close(); inW.Close(); outR.Close(); outW.Close() })

	mon := NewMonitor()
	mon.out = io.Discard
	go mon.Run(outR)

	it := NewInterceptor(inW, "claude", ad, rt, mon)
	it.statusW = io.Discard
	it.rows = func() int { return 24 }

	return &routingHarness{it: it, router: rt, adapter: ad, childIn: inR, childOut: outW}
}

// submit types the prompt and presses Enter, emitting the switch
// confirmation into the child's output stream while the cycle runs.
func (h *routingHarness) submit(t *testing.T, prompt string) {
	t.Helper()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_, _ = h.childOut.WriteString(switchMarker)
			}
		}
	}()

	if err := h.it.HandleInput(append([]byte(prompt), '\r')); err != nil {
		t.Fatal(err)
	}
	close(stop)
	wg.Wait()
}

// drain returns everything the interceptor wrote to the child so far.
func (h *routingHarness) drain(t *testing.T) string {
	t.Helper()
	var out []byte
	buf := make([]byte, 4096)
	for {
		if err := h.childIn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		n, err := h.childIn.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			break
		}
	}
	return string(out)
}

func TestRoutingCycleSwitchesAndForwards(t *testing.T) {
	rt := &fakeRouter{alias: "sonnet"}
	ad := &fakeSwitchAdapter{models: map[string]string{
		"haiku": "model-haiku", "sonnet": "model-sonnet",
	}}
	h := newRoutingHarness(t, rt, ad)

	h.submit(t, "explain this bug")

	if got := rt.routed; len(got) != 1 || got[0] != "explain this bug" {
		t.Fatalf("routed prompts = %v", got)
	}
	if got := rt.remembered; len(got) != 1 || got[0] != "explain this bug" {
		t.Fatalf("remembered = %v", got)
	}
	if got := ad.switched; len(got) != 1 || got[0] != "model-sonnet" {
		t.Fatalf("switched = %v", got)
	}
	if h.it.currentModel != "model-sonnet" {
		t.Fatalf("currentModel = %q", h.it.currentModel)
	}

	out := h.drain(t)
	// Echo while typing → Ctrl+U clear → adapter's /model → prompt resent → Enter.
	for i, want := range []string{"explain this bug", "\x15", "/model model-sonnet\r", "explain this bug\r"} {
		idx := strings.Index(out, want)
		if idx < 0 {
			t.Fatalf("child input missing step %d (%q); got %q", i, want, out)
		}
		out = out[idx+len(want):]
	}
}

func TestRoutingFailureKeepsModelAndSubmits(t *testing.T) {
	rt := &fakeRouter{err: errors.New("classifier timed out")}
	ad := &fakeSwitchAdapter{models: map[string]string{"haiku": "model-haiku"}}
	h := newRoutingHarness(t, rt, ad)

	h.submit(t, "hello there")

	if len(ad.switched) != 0 {
		t.Fatalf("must not switch on routing failure, switched %v", ad.switched)
	}
	if h.it.currentModel != "" {
		t.Fatalf("currentModel = %q, want unchanged", h.it.currentModel)
	}
	// The prompt is already typed into the child; a bare Enter submits it.
	if out := h.drain(t); !strings.HasSuffix(out, "hello there\r") {
		t.Fatalf("child input = %q, want echo + bare Enter", out)
	}
}

func TestAlreadyActiveModelSkipsSwitch(t *testing.T) {
	rt := &fakeRouter{alias: "haiku"}
	ad := &fakeSwitchAdapter{models: map[string]string{"haiku": "model-haiku"}}
	h := newRoutingHarness(t, rt, ad)
	h.it.currentModel = "model-haiku"

	h.submit(t, "small question")

	if len(ad.switched) != 0 {
		t.Fatalf("must not re-switch, switched %v", ad.switched)
	}
	if out := h.drain(t); !strings.HasSuffix(out, "small question\r") {
		t.Fatalf("child input = %q, want echo + bare Enter", out)
	}
}

func TestSwitchFailureStillSubmitsPrompt(t *testing.T) {
	rt := &fakeRouter{alias: "sonnet"}
	ad := &fakeSwitchAdapter{
		models:     map[string]string{"sonnet": "model-sonnet"},
		failSwitch: true,
	}
	h := newRoutingHarness(t, rt, ad)

	h.submit(t, "needs sonnet")

	if h.it.currentModel != "" {
		t.Fatalf("currentModel = %q, want unchanged after failed switch", h.it.currentModel)
	}
	out := h.drain(t)
	// Input cleared, then the prompt is resent and submitted on the current model.
	for _, want := range []string{"\x15", "needs sonnet\r"} {
		if !strings.Contains(out, want) {
			t.Fatalf("child input missing %q; got %q", want, out)
		}
	}
}

func TestMultiLinePromptForwardedAsBracketedPaste(t *testing.T) {
	rt := &fakeRouter{alias: "sonnet"}
	ad := &fakeSwitchAdapter{models: map[string]string{"sonnet": "model-sonnet"}}
	h := newRoutingHarness(t, rt, ad)

	// Paste a two-line prompt, then press Enter.
	if err := h.it.HandleInput([]byte("\x1b[200~line one\rline two\x1b[201~")); err != nil {
		t.Fatal(err)
	}
	h.submit(t, "")

	out := h.drain(t)
	// The resent prompt must be wrapped in paste markers so the embedded \r
	// does not submit early; \x7f\x15 clears the extra child input line.
	want := "\x1b[200~line one\rline two\x1b[201~\r"
	if !strings.Contains(out, want) {
		t.Fatalf("child input missing bracketed resend %q; got %q", want, out)
	}
	if !strings.Contains(out, "\x15\x7f\x15") {
		t.Fatalf("child input missing multi-line clear; got %q", out)
	}
}

func TestPumpInputReassemblesSplitSequence(t *testing.T) {
	rt := &fakeRouter{alias: "haiku"}
	ad := &fakeSwitchAdapter{models: map[string]string{"haiku": "model-haiku"}}
	h := newRoutingHarness(t, rt, ad)

	chunks := make(chan []byte)
	done := make(chan struct{})
	go func() { pumpInput(chunks, h.it); close(done) }()

	// Continuation arrives well within EscFlushTimeout: one reassembled write.
	chunks <- []byte("\x1b")
	chunks <- []byte("[38;1R")
	close(chunks)
	<-done

	if got := h.drain(t); got != "\x1b[38;1R" {
		t.Fatalf("forwarded %q, want reassembled sequence", got)
	}
	if len(h.it.buf) != 0 {
		t.Fatalf("buffer polluted: %q", h.it.buf)
	}
}

func TestPumpInputFlushesLoneEscapeAfterTimeout(t *testing.T) {
	rt := &fakeRouter{alias: "haiku"}
	ad := &fakeSwitchAdapter{models: map[string]string{"haiku": "model-haiku"}}
	h := newRoutingHarness(t, rt, ad)

	chunks := make(chan []byte)
	done := make(chan struct{})
	go func() { pumpInput(chunks, h.it); close(done) }()

	chunks <- []byte("abc\x1b")
	// No continuation: after EscFlushTimeout the ESC must be resolved as a
	// bare Escape keypress (forwarded alone, shadow buffer cleared).
	time.Sleep(EscFlushTimeout + 50*time.Millisecond)
	close(chunks)
	<-done

	if got := h.drain(t); got != "abc\x1b" {
		t.Fatalf("forwarded %q, want echo + flushed ESC", got)
	}
	if len(h.it.buf) != 0 {
		t.Fatalf("buffer = %q, want cleared by bare Escape", h.it.buf)
	}
}
