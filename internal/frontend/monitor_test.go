package frontend

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"
)

func TestMonitorArmDetectsAcrossChunks(t *testing.T) {
	m := NewMonitor()
	done := m.Arm(func(out []byte) bool {
		return bytes.Contains(out, []byte("Set model to sonnet"))
	})

	m.feed([]byte("…echo: /model claude-sonnet-4-6\r\n"))
	select {
	case <-done:
		t.Fatal("must not complete before the pattern appears")
	default:
	}

	// Pattern split across two chunks must still match.
	m.feed([]byte("⎿ Set model"))
	m.feed([]byte(" to sonnet (claude-sonnet-4-6)"))

	select {
	case <-done:
	default:
		t.Fatal("completion pattern was not detected")
	}
}

func TestMonitorDisarmStopsMatching(t *testing.T) {
	m := NewMonitor()
	done := m.Arm(func(out []byte) bool { return bytes.Contains(out, []byte("x")) })
	m.Disarm()
	m.feed([]byte("xxx"))
	select {
	case <-done:
		t.Fatal("disarmed monitor must not complete")
	default:
	}
}

func TestMonitorWaitForTimeout(t *testing.T) {
	m := NewMonitor()
	if _, ok := m.WaitFor(func([]byte) bool { return false }, 20*time.Millisecond); ok {
		t.Fatal("must report no match on timeout")
	}
}

func TestMonitorRunForwardsAndSignalsEOF(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	m := NewMonitor()
	var forwarded syncBuffer
	m.out = &forwarded

	go m.Run(r)
	if _, err := io.WriteString(w, "child output"); err != nil {
		t.Fatal(err)
	}
	w.Close()

	select {
	case <-m.EOF():
	case <-time.After(time.Second):
		t.Fatal("EOF not signalled after the PTY stream ended")
	}
	if forwarded.String() != "child output" {
		t.Fatalf("forwarded = %q", forwarded.String())
	}
}
