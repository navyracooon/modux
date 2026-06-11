package router

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// classifier answers one classification prompt with the raw model output.
// Implementations keep a persistent headless session of their vendor's CLI,
// so each classification pays only API latency, not CLI startup.
type classifier interface {
	ask(ctx context.Context, prompt string) (string, error)
	ready() bool                    // false while the session is still warming up
	awaitReady(ctx context.Context) // block until warm (or ctx expires)
	close()
}

// newClassifier picks the backend by the classifier model's vendor: Claude
// models run through `claude -p` stream-json, anything else through
// `codex mcp-server`. With the per-target config defaults this means each
// wrapped tool classifies via its own CLI and subscription.
func newClassifier(model string) classifier {
	if strings.HasPrefix(model, "claude") {
		return newPersistentClassifier(model)
	}
	return newCodexMCPClassifier(model)
}

// maxSessionTurns caps how many classifications run in one persistent session
// before it is recycled: stream-json input is a single conversation, so every
// turn grows the context the next turn must carry.
const maxSessionTurns = 20

// persistentClassifier keeps one headless `claude -p` stream-json session
// alive for the whole modux run. Each classification then pays only API
// latency; the CLI's multi-second startup is paid once, at modux launch.
type persistentClassifier struct {
	model string

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	results chan streamResult
	turns   int
}

// streamResult is the subset of a stream-json "result" line we consume.
type streamResult struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

func newPersistentClassifier(model string) *persistentClassifier {
	p := &persistentClassifier{model: model}
	p.mu.Lock()
	_ = p.startLocked() // prewarm; a failure here resurfaces on the first ask
	p.mu.Unlock()
	return p
}

func (p *persistentClassifier) startLocked() error {
	cmd := exec.Command("claude", "-p", "--model", p.model, "--strict-mcp-config",
		"--input-format", "stream-json", "--output-format", "stream-json", "--verbose")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	// Each session gets its own results channel, so an answer from a killed
	// session can never be paired with an ask on its successor.
	results := make(chan streamResult, 1)
	go func() {
		defer close(results)
		r := bufio.NewReaderSize(stdout, 256*1024)
		for {
			line, err := r.ReadBytes('\n')
			if res, ok := parseResultLine(line); ok {
				select {
				case results <- res:
				default: // unpaired answer (ask timed out); drop, never block
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { _ = cmd.Wait() }()

	p.cmd, p.stdin, p.results, p.turns = cmd, stdin, results, 0
	return nil
}

// parseResultLine decodes one stream-json output line, keeping only "result"
// records (init banners, assistant deltas, rate-limit events are skipped).
func parseResultLine(line []byte) (streamResult, bool) {
	var res streamResult
	if json.Unmarshal(line, &res) != nil || res.Type != "result" {
		return streamResult{}, false
	}
	return res, true
}

func (p *persistentClassifier) stopLocked() {
	if p.cmd == nil {
		return
	}
	_ = p.stdin.Close()
	_ = p.cmd.Process.Kill()
	p.cmd, p.stdin, p.results = nil, nil, nil
}

func (p *persistentClassifier) ask(ctx context.Context, prompt string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd == nil {
		if err := p.startLocked(); err != nil {
			return "", fmt.Errorf("classifier session start failed: %w", err)
		}
	}
	msg, err := encodeUserMessage(prompt)
	if err != nil {
		return "", err
	}
	if _, err := p.stdin.Write(msg); err != nil {
		// The session died since the last ask; restart once and retry.
		p.stopLocked()
		if err := p.startLocked(); err != nil {
			return "", fmt.Errorf("classifier session restart failed: %w", err)
		}
		if _, err := p.stdin.Write(msg); err != nil {
			p.stopLocked()
			return "", fmt.Errorf("classifier session write failed: %w", err)
		}
	}

	select {
	case res, ok := <-p.results:
		if !ok {
			p.stopLocked()
			return "", fmt.Errorf("classifier session exited")
		}
		p.turns++
		if p.turns >= maxSessionTurns {
			// Recycle to cap context growth. Spawning is cheap; the new
			// session warms up while the user reads the current answer.
			p.stopLocked()
			_ = p.startLocked()
		}
		if res.IsError {
			return "", fmt.Errorf("classifier error: %s", res.Subtype)
		}
		return res.Result, nil

	case <-ctx.Done():
		// The in-flight answer can no longer be paired with an ask; kill the
		// session so it cannot desync the next one, and warm a fresh one.
		p.stopLocked()
		_ = p.startLocked()
		return "", ctx.Err()
	}
}

// ready is always true: the session accepts writes from the moment the
// process starts (stdin is buffered while the CLI finishes initializing).
func (p *persistentClassifier) ready() bool { return true }

func (p *persistentClassifier) awaitReady(context.Context) {}

func (p *persistentClassifier) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
}

// encodeUserMessage builds one stream-json input line carrying the prompt.
func encodeUserMessage(text string) ([]byte, error) {
	type textContent struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	msg := struct {
		Type    string `json:"type"`
		Message struct {
			Role    string        `json:"role"`
			Content []textContent `json:"content"`
		} `json:"message"`
	}{Type: "user"}
	msg.Message.Role = "user"
	msg.Message.Content = []textContent{{Type: "text", Text: text}}

	b, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
