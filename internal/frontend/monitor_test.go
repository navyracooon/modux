package frontend

import (
	"bytes"
	"testing"
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
