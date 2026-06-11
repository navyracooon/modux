package frontend

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"

	"github.com/navyracooon/modux/internal/adapter"
	"github.com/navyracooon/modux/internal/config"
	"github.com/navyracooon/modux/internal/router"
)

// Run launches the target CLI under a PTY and proxies I/O through the
// interceptor until the child exits. It returns the child's exit code.
func Run(cfg *config.Config, target string, args []string) (int, error) {
	mon := NewMonitor()
	ad, err := adapter.New(target, cfg.Models[target], mon)
	if err != nil {
		return 1, err
	}
	// Each target classifies via its own vendor's CLI by default; the session
	// prewarms in the background while the user types their first prompt.
	rt := router.New(cfg.ClassifierModel(target), cfg.Classifier.TimeoutDuration())
	rt.Prewarm()
	defer rt.Close()

	c := exec.Command(target, args...)
	ptmx, err := pty.Start(c)
	if err != nil {
		return 1, fmt.Errorf("failed to start %s: %w", target, err)
	}
	defer ptmx.Close()

	// Propagate terminal size now and on every SIGWINCH.
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			_ = pty.InheritSize(os.Stdin, ptmx)
		}
	}()
	winch <- syscall.SIGWINCH

	// Raw mode for stdin; restored before returning.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return 1, fmt.Errorf("failed to set raw mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	go mon.Run(ptmx)

	it := NewInterceptor(ptmx, target, ad, rt, mon)

	// Stdin → interceptor. Bytes typed while a submission is in flight stay
	// queued (in the channel and the OS stdin buffer) until the interceptor
	// returns. The quiet-period timer resolves escape sequences split across
	// reads: a lone ESC with no continuation is a bare Escape keypress,
	// while a terminal reply's remainder simply continues the sequence.
	chunks := make(chan []byte)
	go func() {
		defer close(chunks)
		buf := make([]byte, 4096)
		for {
			n, rerr := os.Stdin.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				chunks <- chunk
			}
			if rerr != nil {
				if rerr != io.EOF {
					fmt.Fprintf(os.Stderr, "\r\n[modux] stdin error: %v\r\n", rerr)
				}
				return
			}
		}
	}()
	go func() {
		for {
			var quiet <-chan time.Time
			if it.PendingEscape() {
				quiet = time.After(EscFlushTimeout)
			}
			select {
			case chunk, ok := <-chunks:
				if !ok {
					return
				}
				if err := it.HandleInput(chunk); err != nil {
					return
				}
			case <-quiet:
				if err := it.FlushEscape(); err != nil {
					return
				}
			}
		}
	}()

	// Wait for child exit (PTY EOF), then collect its status.
	<-mon.EOF()
	err = c.Wait()
	if err == nil {
		return 0, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), nil
	}
	return 1, err
}
