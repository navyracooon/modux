package router

import (
	"context"
	"strings"
	"testing"
	"time"
)

// fakeCls scripts the classifier backend.
type fakeCls struct {
	out   string
	err   error
	delay time.Duration

	asked []string
}

func (f *fakeCls) ask(ctx context.Context, prompt string) (string, error) {
	f.asked = append(f.asked, prompt)
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return f.out, f.err
}
func (f *fakeCls) ready() bool                  { return true }
func (f *fakeCls) awaitReady(_ context.Context) {}
func (f *fakeCls) close()                       {}

func newTestRouter(timeout time.Duration, f *fakeCls) *Router {
	r := New("test-model", timeout)
	r.mkCls = func() classifier { return f }
	return r
}

var testAliases = map[string]string{
	"haiku":  "model-haiku",
	"sonnet": "model-sonnet",
	"opus":   "model-opus",
}

func TestRouteReturnsClassifiedAlias(t *testing.T) {
	f := &fakeCls{out: "sonnet"}
	r := newTestRouter(time.Second, f)

	alias, err := r.Route(context.Background(), "claude", testAliases, "fix this bug")
	if err != nil || alias != "sonnet" {
		t.Fatalf("Route = (%q, %v)", alias, err)
	}
}

func TestRoutePromptCarriesInstructionsAndHistory(t *testing.T) {
	f := &fakeCls{out: "haiku"}
	r := newTestRouter(time.Second, f)
	r.Remember("earlier question about sorting")

	if _, err := r.Route(context.Background(), "codex", testAliases, "what is a mutex"); err != nil {
		t.Fatal(err)
	}
	if len(f.asked) != 1 {
		t.Fatalf("asked %d times", len(f.asked))
	}
	sent := f.asked[0]
	for _, want := range []string{
		"codex CLI",                       // target identifies the wrapped tool
		"haiku, sonnet, opus",             // tiers listed cheapest-first
		"earlier question about sorting",  // rolling history included
		"what is a mutex",                 // the new prompt itself
		"exactly one word",                // single-word answer contract
		"Ignore any project or system in", // shields against CLAUDE.md/AGENTS.md language rules
	} {
		if !strings.Contains(sent, want) {
			t.Fatalf("classifier prompt missing %q:\n%s", want, sent)
		}
	}
}

func TestRouteTimeout(t *testing.T) {
	f := &fakeCls{out: "haiku", delay: 200 * time.Millisecond}
	r := newTestRouter(20*time.Millisecond, f)

	_, err := r.Route(context.Background(), "claude", testAliases, "p")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want timeout", err)
	}
}

func TestRouteClassifierError(t *testing.T) {
	f := &fakeCls{err: context.Canceled}
	r := newTestRouter(time.Second, f)

	if _, err := r.Route(context.Background(), "claude", testAliases, "p"); err == nil {
		t.Fatal("classifier error must propagate")
	}
}

func TestRouteUnknownAnswer(t *testing.T) {
	f := &fakeCls{out: "gpt-7-ultra"}
	r := newTestRouter(time.Second, f)

	_, err := r.Route(context.Background(), "claude", testAliases, "p")
	if err == nil || !strings.Contains(err.Error(), "no known alias") {
		t.Fatalf("err = %v, want no-known-alias", err)
	}
}
