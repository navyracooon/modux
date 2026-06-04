package tui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	boxW     = 34
	innerW   = boxW - 2 // 32
	perimLen = 2*boxW + 2 // 70
)

// ANSI gradient: head (bright) → tail (dim)
var grad = []string{
	"\x1b[97;1m",  // bright white — head
	"\x1b[96;1m",  // bright cyan bold
	"\x1b[96m",    // bright cyan
	"\x1b[36m",    // cyan
	"\x1b[36;2m",  // dim cyan
	"\x1b[37;2m",  // dim gray — tail fades into border
}

const (
	reset    = "\x1b[0m"
	dimGray  = "\x1b[37;2m"
	msgColor = "\x1b[97m"
)

// Loader draws a rectangular glow animation while waiting for the classifier.
type Loader struct {
	tty  *os.File
	done chan struct{}
	once sync.Once
}

// OpenTTY opens /dev/tty for direct terminal writes, falling back to stderr.
func OpenTTY() *os.File {
	if f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		return f
	}
	return os.Stderr
}

func NewLoader(tty *os.File) *Loader {
	return &Loader{tty: tty, done: make(chan struct{})}
}

// Start reserves 3 lines on the terminal and begins the animation.
func (l *Loader) Start(msg string) {
	// Hide cursor, reserve 3 lines below current position.
	fmt.Fprintf(l.tty, "\x1b[?25l\r\n\r\n\r\n")
	go func() {
		ticker := time.NewTicker(33 * time.Millisecond)
		defer ticker.Stop()
		for frame := 0; ; frame++ {
			select {
			case <-l.done:
				return
			case <-ticker.C:
				l.drawFrame(msg, frame%perimLen)
			}
		}
	}()
}

// StopWithResult stops animation, briefly shows the selected model, then clears.
func (l *Loader) StopWithResult(model string) {
	l.once.Do(func() {
		close(l.done)
		time.Sleep(50 * time.Millisecond)
		l.drawSuccess(model)
		time.Sleep(500 * time.Millisecond)
		l.clear()
	})
}

// Stop stops animation and clears without showing a result.
func (l *Loader) Stop() {
	l.once.Do(func() {
		close(l.done)
		time.Sleep(50 * time.Millisecond)
		l.clear()
	})
}

func (l *Loader) clear() {
	fmt.Fprintf(l.tty, "\x1b[3A\r\x1b[0J\x1b[?25h")
}

// drawFrame redraws all 3 lines with the glow at position head.
func (l *Loader) drawFrame(msg string, head int) {
	top := l.buildBorder(head, true)
	mid := l.buildMid(msg, head)
	bot := l.buildBorder(head, false)
	fmt.Fprintf(l.tty, "\x1b[3A\r%s\r\n%s\r\n%s", top, mid, bot)
}

// drawSuccess renders a static green "✓ model" box.
func (l *Loader) drawSuccess(model string) {
	green := "\x1b[92;1m"
	line := strings.Repeat("─", innerW)
	top := fmt.Sprintf("%s╭%s╮%s", green, line, reset)

	label := "✓ " + model
	padded := centerPad(label, innerW)
	mid := fmt.Sprintf("%s│%s\x1b[92;1m%s%s%s│%s", green, reset, padded, reset, green, reset)
	bot := fmt.Sprintf("%s╰%s╯%s", green, line, reset)

	fmt.Fprintf(l.tty, "\x1b[3A\r%s\r\n%s\r\n%s", top, mid, bot)
}

// colorAt returns the ANSI color for a perimeter position given the glow head.
func colorAt(perimPos, head int) string {
	dist := (head - perimPos + perimLen) % perimLen
	if dist < len(grad) {
		return grad[dist]
	}
	return dimGray
}

// buildBorder builds the top (isTop=true) or bottom border line.
func (l *Loader) buildBorder(head int, isTop bool) string {
	var sb strings.Builder
	for j := 0; j < boxW; j++ {
		var perimPos int
		if isTop {
			perimPos = j
		} else {
			// bottom goes right→left: BL(j=0)=68, BR(j=33)=35
			perimPos = 68 - j
		}
		var ch string
		switch {
		case isTop && j == 0:
			ch = "╭"
		case isTop && j == boxW-1:
			ch = "╮"
		case !isTop && j == 0:
			ch = "╰"
		case !isTop && j == boxW-1:
			ch = "╯"
		default:
			ch = "─"
		}
		sb.WriteString(colorAt(perimPos, head) + ch + reset)
	}
	return sb.String()
}

// buildMid builds the middle content line with left/right borders.
func (l *Loader) buildMid(msg string, head int) string {
	leftC  := colorAt(perimLen-1, head) // pos 69
	rightC := colorAt(boxW, head)        // pos 34
	padded := centerPad(msg, innerW)
	return fmt.Sprintf("%s│%s%s%s%s│%s",
		leftC, reset, msgColor+padded+reset, rightC, "", reset)
}

func centerPad(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	total := width - len(s)
	left := total / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", total-left)
}
