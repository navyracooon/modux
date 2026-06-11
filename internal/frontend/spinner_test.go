package frontend

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer guards a bytes.Buffer against concurrent spinner writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestSpinnerDrawsImmediatelyAndStopsWithResult(t *testing.T) {
	var buf syncBuffer
	s := startSpinner(&buf, 24, "Classifying…")

	if !strings.Contains(buf.String(), "Classifying…") {
		t.Fatal("first frame must be drawn immediately, before the first tick")
	}
	// Frames must save the cursor, position absolutely, and restore — never
	// scroll the screen.
	if !strings.Contains(buf.String(), "\x1b7\x1b[24;1H\x1b[2K") {
		t.Fatalf("frame is not absolutely positioned: %q", buf.String())
	}
	if strings.Contains(buf.String(), "\n") {
		t.Fatalf("spinner must not scroll the screen: %q", buf.String())
	}

	s.SetMessage("Switching to haiku…")
	time.Sleep(3 * spinnerInterval)
	if !strings.Contains(buf.String(), "Switching to haiku…") {
		t.Fatal("updated message was not drawn")
	}

	s.StopWith(10*time.Millisecond, "model: %s", "haiku")
	out := buf.String()
	if !strings.Contains(out, "[modux] model: haiku") {
		t.Fatalf("final status missing: %q", out)
	}
	// After the hold, the row must be cleared and the cursor restored.
	if !strings.HasSuffix(out, "\x1b7\x1b[24;1H\x1b[2K\x1b8") {
		t.Fatalf("spinner row was not cleared at the end: %q", out)
	}

	// No frames may be drawn after StopWith.
	n := len(buf.String())
	time.Sleep(3 * spinnerInterval)
	if len(buf.String()) != n {
		t.Fatal("spinner kept drawing after StopWith")
	}
}
