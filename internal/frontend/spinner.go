package frontend

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/creack/pty"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerInterval = 80 * time.Millisecond

// Spinner shows a braille animation on the terminal's bottom row, giving
// immediate feedback that the submitted prompt was accepted and is being
// classified. Each frame is one atomic write that saves the cursor, draws at
// an absolute position, and restores the cursor — it never scrolls the
// screen, so the child TUI's differential rendering stays aligned and simply
// repaints over the line later.
type Spinner struct {
	w    io.Writer
	row  int
	mu   sync.Mutex
	msg  string
	done chan struct{}
	wg   sync.WaitGroup
	once sync.Once
}

// startSpinner begins animating on the given row (1-based) immediately.
func startSpinner(w io.Writer, row int, msg string) *Spinner {
	s := &Spinner{w: w, row: row, msg: msg, done: make(chan struct{})}
	s.draw(0)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(spinnerInterval)
		defer ticker.Stop()
		for frame := 1; ; frame++ {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				s.draw(frame)
			}
		}
	}()
	return s
}

// SetMessage swaps the text next to the spinner.
func (s *Spinner) SetMessage(msg string) {
	s.mu.Lock()
	s.msg = msg
	s.mu.Unlock()
}

// Stop halts the animation and clears the spinner row. The row stays blank
// until the child TUI repaints it; leaving text behind is not an option
// because differential renderers overwrite rows without clearing to the end
// of line, shredding any leftover text into fragments.
func (s *Spinner) Stop() {
	s.stop(0, "")
}

// StopWith replaces the spinner with a final status message, holds it long
// enough to read, then clears the row. Synchronous: call it while the child
// is idle, before forwarding input that would trigger repaints.
func (s *Spinner) StopWith(hold time.Duration, format string, args ...any) {
	s.stop(hold, fmt.Sprintf("[modux] "+format, args...))
}

func (s *Spinner) stop(hold time.Duration, text string) {
	s.once.Do(func() {
		close(s.done)
		s.wg.Wait()
		if text != "" {
			fmt.Fprintf(s.w, "\x1b7\x1b[%d;1H\x1b[2K%s\x1b8", s.row, text)
			time.Sleep(hold)
		}
		fmt.Fprintf(s.w, "\x1b7\x1b[%d;1H\x1b[2K\x1b8", s.row)
	})
}

func (s *Spinner) draw(frame int) {
	s.mu.Lock()
	msg := s.msg
	s.mu.Unlock()
	fmt.Fprintf(s.w, "\x1b7\x1b[%d;1H\x1b[2K\x1b[36m%s\x1b[0m %s\x1b8",
		s.row, spinnerFrames[frame%len(spinnerFrames)], msg)
}

// transientStatus draws a one-off status line on the given row, holds it
// long enough to read, then clears it.
func transientStatus(w io.Writer, row int, hold time.Duration, format string, args ...any) {
	fmt.Fprintf(w, "\x1b7\x1b[%d;1H\x1b[2K[modux] %s\x1b8", row, fmt.Sprintf(format, args...))
	time.Sleep(hold)
	fmt.Fprintf(w, "\x1b7\x1b[%d;1H\x1b[2K\x1b8", row)
}

// terminalRows returns the current terminal height, defaulting to 24 when it
// cannot be determined (e.g. in tests without a TTY).
func terminalRows() int {
	if ws, err := pty.GetsizeFull(os.Stdin); err == nil && ws.Rows > 0 {
		return int(ws.Rows)
	}
	return 24
}
