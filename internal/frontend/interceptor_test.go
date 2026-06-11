package frontend

import (
	"os"
	"testing"
	"time"

	"github.com/navyracooon/modux/internal/adapter"
	"github.com/navyracooon/modux/internal/router"
)

func newTestInterceptor(t *testing.T) (*Interceptor, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })

	ad, err := adapter.New("claude", map[string]string{"haiku": "claude-haiku-4-5-20251001"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New("claude-haiku-4-5-20251001", 100*time.Millisecond)
	it := NewInterceptor(w, "claude", ad, rt, NewMonitor())
	return it, r
}

func readAvailable(t *testing.T, r *os.File) string {
	t.Helper()
	buf := make([]byte, 256)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	return string(buf[:n])
}

func TestNormalCharsBufferedAndEchoed(t *testing.T) {
	it, r := newTestInterceptor(t)
	if err := it.HandleInput([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if got := readAvailable(t, r); got != "abc" {
		t.Fatalf("echoed %q, want %q", got, "abc")
	}
	if string(it.buf) != "abc" {
		t.Fatalf("buffer = %q, want %q", it.buf, "abc")
	}
}

func TestBackspaceDropsRuneAndForwards(t *testing.T) {
	it, r := newTestInterceptor(t)
	if err := it.HandleInput([]byte("aあ\x7f")); err != nil {
		t.Fatal(err)
	}
	if string(it.buf) != "a" {
		t.Fatalf("buffer = %q, want %q", it.buf, "a")
	}
	if got := readAvailable(t, r); got != "aあ\x7f" {
		t.Fatalf("forwarded %q", got)
	}
}

func TestEscapeSequenceBypassesBuffer(t *testing.T) {
	it, r := newTestInterceptor(t)
	// Up-arrow: ESC [ A — passed through, buffer untouched.
	if err := it.HandleInput([]byte("hi\x1b[A")); err != nil {
		t.Fatal(err)
	}
	if string(it.buf) != "hi" {
		t.Fatalf("buffer = %q, want %q", it.buf, "hi")
	}
	if got := readAvailable(t, r); got != "hi\x1b[A" {
		t.Fatalf("forwarded %q", got)
	}
}

func TestDeviceReplyBypassesBuffer(t *testing.T) {
	it, r := newTestInterceptor(t)
	// tmux XTVERSION reply (DCS … ST) followed by a cursor-position report
	// (CSI … R): both must pass through without polluting the buffer.
	in := "\x1bP>|tmux 3.6a\x1b\\\x1b[24;80Rhi"
	if err := it.HandleInput([]byte(in)); err != nil {
		t.Fatal(err)
	}
	if string(it.buf) != "hi" {
		t.Fatalf("buffer = %q, want %q", it.buf, "hi")
	}
	if got := readAvailable(t, r); got != in {
		t.Fatalf("forwarded %q, want %q", got, in)
	}
}

func TestBracketedPasteBuffersNewlinesWithoutRouting(t *testing.T) {
	it, r := newTestInterceptor(t)
	// A multi-line bracketed paste: \r inside must be buffered as content,
	// and the leading '/' must not start slash-command pass-through.
	in := "\x1b[200~/etc/hosts\rline2\x1b[201~"
	if err := it.HandleInput([]byte(in)); err != nil {
		t.Fatal(err)
	}
	if string(it.buf) != "/etc/hosts\rline2" {
		t.Fatalf("buffer = %q", it.buf)
	}
	if it.pasteMode {
		t.Fatal("paste mode should end at CSI 201~")
	}
	if it.passThruLine {
		t.Fatal("pasted '/' must not trigger slash pass-through")
	}
	if got := readAvailable(t, r); got != in {
		t.Fatalf("forwarded %q, want %q", got, in)
	}
}

func TestBareEscapeClearsBufferAndState(t *testing.T) {
	it, r := newTestInterceptor(t)
	// A lone ESC ending the chunk is a bare Escape keypress.
	if err := it.HandleInput([]byte("abc\x1b")); err != nil {
		t.Fatal(err)
	}
	if len(it.buf) != 0 {
		t.Fatalf("buffer = %q, want empty", it.buf)
	}
	if it.esc != escNone {
		t.Fatalf("esc state = %v, want escNone", it.esc)
	}
	// A following slash must start command pass-through, not be eaten as a
	// sequence byte.
	if err := it.HandleInput([]byte("/help")); err != nil {
		t.Fatal(err)
	}
	if !it.passThruLine {
		t.Fatal("slash after bare Escape must start pass-through")
	}
	if got := readAvailable(t, r); got != "abc\x1b/help" {
		t.Fatalf("forwarded %q", got)
	}
}

func TestDigitsOnlyEnterSkipsRouting(t *testing.T) {
	it, r := newTestInterceptor(t)
	// Bare digits are picker selections; Enter must pass through without
	// routing (routing would shell out to the classifier here).
	if err := it.HandleInput([]byte("5\r")); err != nil {
		t.Fatal(err)
	}
	if len(it.buf) != 0 {
		t.Fatalf("buffer = %q, want empty", it.buf)
	}
	if got := readAvailable(t, r); got != "5\r" {
		t.Fatalf("forwarded %q", got)
	}
}

func TestCtrlUClearsBuffer(t *testing.T) {
	it, r := newTestInterceptor(t)
	if err := it.HandleInput([]byte("abc\x15")); err != nil {
		t.Fatal(err)
	}
	if len(it.buf) != 0 {
		t.Fatalf("buffer = %q, want empty", it.buf)
	}
	if got := readAvailable(t, r); got != "abc\x15" {
		t.Fatalf("forwarded %q", got)
	}
}

func TestSlashCommandLinePassesThrough(t *testing.T) {
	it, r := newTestInterceptor(t)
	if err := it.HandleInput([]byte("/help\r")); err != nil {
		t.Fatal(err)
	}
	if len(it.buf) != 0 {
		t.Fatalf("buffer = %q, want empty", it.buf)
	}
	if it.passThruLine {
		t.Fatal("pass-through mode should end at Enter")
	}
	if got := readAvailable(t, r); got != "/help\r" {
		t.Fatalf("forwarded %q", got)
	}
}

func TestEmptyEnterPassesThrough(t *testing.T) {
	it, r := newTestInterceptor(t)
	if err := it.HandleInput([]byte("\r")); err != nil {
		t.Fatal(err)
	}
	if got := readAvailable(t, r); got != "\r" {
		t.Fatalf("forwarded %q", got)
	}
}

func TestCtrlCPassesThroughAndClearsBuffer(t *testing.T) {
	it, r := newTestInterceptor(t)
	if err := it.HandleInput([]byte("abc\x03")); err != nil {
		t.Fatal(err)
	}
	if len(it.buf) != 0 {
		t.Fatalf("buffer = %q, want empty", it.buf)
	}
	if got := readAvailable(t, r); got != "abc\x03" {
		t.Fatalf("forwarded %q", got)
	}
}
