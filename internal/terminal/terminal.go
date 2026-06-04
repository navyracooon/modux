package terminal

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"

	"github.com/navyracooon/modux/internal/classifier"
	"github.com/navyracooon/modux/internal/config"
	"github.com/navyracooon/modux/internal/tui"
)

// inputBuffer accumulates keystrokes until Enter.
type inputBuffer struct {
	buf []byte
	mu  sync.Mutex
}

func (b *inputBuffer) write(p []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
}

func (b *inputBuffer) reset() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, len(b.buf))
	copy(out, b.buf)
	b.buf = b.buf[:0]
	return out
}

// Run starts the target CLI and handles I/O with model-optimized injection.
func Run(cfg *config.Config) error {
	cl := classifier.New(cfg.AnthropicAPIKey, cfg.ClassifierModel)
	tty := tui.OpenTTY()

	c := exec.Command(cfg.Target, cfg.Args...)

	ptmx, err := pty.Start(c)
	if err != nil {
		return fmt.Errorf("failed to start %s: %w", cfg.Target, err)
	}
	defer ptmx.Close()

	if ws, err := pty.GetsizeFull(os.Stdin); err == nil {
		_ = pty.Setsize(ptmx, ws)
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("failed to set raw mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Forward PTY output → user stdout continuously.
	go func() {
		_, _ = io.Copy(os.Stdout, ptmx)
	}()

	stdinBuf := make([]byte, 1024)
	var ibuf inputBuffer

	for {
		n, err := os.Stdin.Read(stdinBuf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		chunk := stdinBuf[:n]

		if !containsEnter(chunk) {
			ibuf.write(chunk)
			if _, err := ptmx.Write(chunk); err != nil {
				return err
			}
			continue
		}

		// Enter detected: collect remaining non-enter bytes in chunk.
		ibuf.write(trimEnter(chunk))
		prompt := string(ibuf.reset())

		if strings.TrimSpace(prompt) != "" {
			classifyAndInject(cl, cfg, ptmx, tty, prompt)
		}

		// Forward the Enter key(s) to the PTY.
		if _, err := ptmx.Write(enterBytes(chunk)); err != nil {
			return err
		}
	}

	return c.Wait()
}

// classifyAndInject shows the loading animation, calls the classifier, injects
// the /model command, then clears the animation — all before Enter is forwarded.
func classifyAndInject(
	cl *classifier.Classifier,
	cfg *config.Config,
	ptmx *os.File,
	tty *os.File,
	prompt string,
) {
	loader := tui.NewLoader(tty)
	loader.Start("Classifying prompt…")

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	choice, err := cl.Classify(ctx, cfg.Target, prompt)
	if err != nil {
		loader.Stop()
		return
	}

	loader.StopWithResult(choice.Model)

	cmd := fmt.Sprintf("/model %s\r", choice.Model)
	_, _ = ptmx.Write([]byte(cmd))
}

func containsEnter(p []byte) bool {
	for _, b := range p {
		if b == '\r' || b == '\n' {
			return true
		}
	}
	return false
}

func trimEnter(p []byte) []byte {
	out := p[:0]
	for _, b := range p {
		if b != '\r' && b != '\n' {
			out = append(out, b)
		}
	}
	return out
}

func enterBytes(p []byte) []byte {
	var out []byte
	for _, b := range p {
		if b == '\r' || b == '\n' {
			out = append(out, b)
		}
	}
	return out
}
