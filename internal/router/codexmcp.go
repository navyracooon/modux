package router

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// codexMCPClassifier classifies through a persistent `codex mcp-server`
// process. One-shot `codex exec` initializes a full agent session per run
// (~30s); the MCP server pays that once, after which a `codex` tool call is
// a few seconds. Each classification is its own tool call on a fresh thread,
// so no conversation context accumulates, and JSON-RPC ids pair every answer
// with its request — a timed-out call is simply abandoned, it cannot desync
// the next one.
type codexMCPClassifier struct {
	model string

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	nextID  int64
	pending map[int64]chan rpcResponse

	warmed chan struct{} // closed once the warmup tool call has finished
}

type rpcResponse struct {
	Result json.RawMessage
	Err    *rpcError
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// errSessionDown marks failures caused by the server process being gone, as
// opposed to an error answer from a live server; ask retries these once.
var errSessionDown = errors.New("codex classifier session down")

const (
	codexWarmupPrompt  = "Reply with exactly one word: ok"
	codexWarmupTimeout = 90 * time.Second
	mcpHandshakeWait   = 15 * time.Second
)

func newCodexMCPClassifier(model string) *codexMCPClassifier {
	p := &codexMCPClassifier{
		model:   model,
		pending: map[int64]chan rpcResponse{},
		warmed:  make(chan struct{}),
	}
	go p.warmup()
	return p
}

// warmup starts the server and runs one throwaway tool call: the first call
// on a fresh server carries one-time costs (auth, session machinery) that
// the user's first real classification should not pay.
func (p *codexMCPClassifier) warmup() {
	defer close(p.warmed)
	if err := p.start(); err != nil {
		return // resurfaces on the first ask
	}
	ctx, cancel := context.WithTimeout(context.Background(), codexWarmupTimeout)
	defer cancel()
	_, _ = p.toolCall(ctx, codexWarmupPrompt)
}

func (p *codexMCPClassifier) ready() bool {
	select {
	case <-p.warmed:
		return true
	default:
		return false
	}
}

func (p *codexMCPClassifier) awaitReady(ctx context.Context) {
	select {
	case <-p.warmed:
	case <-ctx.Done():
	}
}

func (p *codexMCPClassifier) start() error {
	cmd := exec.Command("codex", "mcp-server",
		"-c", "mcp_servers={}",
		"-c", `model_reasoning_effort="low"`,
		"-c", `history.persistence="none"`)
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
	p.mu.Lock()
	p.cmd, p.stdin = cmd, stdin
	p.mu.Unlock()
	go p.readLoop(stdout)
	go func() { _ = cmd.Wait() }()

	ctx, cancel := context.WithTimeout(context.Background(), mcpHandshakeWait)
	defer cancel()
	_, err = p.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "modux", "version": "0.1"},
	})
	if err != nil {
		p.stop()
		return fmt.Errorf("mcp handshake failed: %w", err)
	}
	return p.notify("notifications/initialized")
}

// readLoop routes JSON-RPC responses to their waiting callers. Event
// notifications (method "codex/event", no top-level id) are skipped.
func (p *codexMCPClassifier) readLoop(stdout io.Reader) {
	r := bufio.NewReaderSize(stdout, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var msg struct {
				ID     *int64          `json:"id"`
				Method string          `json:"method"`
				Result json.RawMessage `json:"result"`
				Error  *rpcError       `json:"error"`
			}
			if json.Unmarshal(line, &msg) == nil && msg.ID != nil && msg.Method == "" {
				p.mu.Lock()
				ch := p.pending[*msg.ID]
				delete(p.pending, *msg.ID)
				p.mu.Unlock()
				if ch != nil {
					ch <- rpcResponse{Result: msg.Result, Err: msg.Error}
				}
			}
		}
		if err != nil {
			// Server gone: fail everything still waiting.
			p.mu.Lock()
			for id, ch := range p.pending {
				delete(p.pending, id)
				close(ch)
			}
			p.mu.Unlock()
			return
		}
	}
}

func (p *codexMCPClassifier) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	p.mu.Lock()
	stdin := p.stdin
	if stdin == nil {
		p.mu.Unlock()
		return nil, errSessionDown
	}
	p.nextID++
	id := p.nextID
	ch := make(chan rpcResponse, 1)
	p.pending[id] = ch
	p.mu.Unlock()

	req, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	})
	if err != nil {
		return nil, err
	}
	if _, err := stdin.Write(append(req, '\n')); err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, fmt.Errorf("%w: write: %v", errSessionDown, err)
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("%w: server exited", errSessionDown)
		}
		if resp.Err != nil {
			return nil, fmt.Errorf("codex classifier: %s", resp.Err.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (p *codexMCPClassifier) notify(method string) error {
	p.mu.Lock()
	stdin := p.stdin
	p.mu.Unlock()
	if stdin == nil {
		return errSessionDown
	}
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method})
	if err != nil {
		return err
	}
	_, err = stdin.Write(append(req, '\n'))
	return err
}

// toolCall runs one `codex` tool call and returns the agent's final message.
func (p *codexMCPClassifier) toolCall(ctx context.Context, prompt string) (string, error) {
	raw, err := p.call(ctx, "tools/call", map[string]any{
		"name": "codex",
		"arguments": map[string]any{
			"prompt":            prompt,
			"model":             p.model,
			"sandbox":           "read-only",
			"approval-policy":   "never",
			"include-plan-tool": false,
			// A neutral cwd keeps project AGENTS.md files out of the
			// classifier's context.
			"cwd": os.TempDir(),
		},
	})
	if err != nil {
		return "", err
	}
	return parseToolResult(raw)
}

// parseToolResult extracts the agent's final message from an MCP tool result.
func parseToolResult(raw []byte) (string, error) {
	var res struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent struct {
			Content string `json:"content"`
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("codex classifier: bad tool result: %w", err)
	}
	text := res.StructuredContent.Content
	if text == "" && len(res.Content) > 0 {
		text = res.Content[0].Text
	}
	if res.IsError {
		return "", fmt.Errorf("codex classifier error: %s", firstLine(text))
	}
	return text, nil
}

func (p *codexMCPClassifier) ask(ctx context.Context, prompt string) (string, error) {
	// Let the warmup finish first; concurrent calls would both pay cold-path
	// costs. If it exceeds the routing timeout the user keeps their model.
	select {
	case <-p.warmed:
	case <-ctx.Done():
		return "", ctx.Err()
	}

	out, err := p.toolCall(ctx, prompt)
	if errors.Is(err, errSessionDown) && ctx.Err() == nil {
		// The server died since warmup; restart it and retry once.
		p.stop()
		if rerr := p.start(); rerr != nil {
			return "", fmt.Errorf("codex classifier restart failed: %w", rerr)
		}
		return p.toolCall(ctx, prompt)
	}
	return out, err
}

func (p *codexMCPClassifier) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil {
		return
	}
	_ = p.stdin.Close()
	_ = p.cmd.Process.Kill()
	p.cmd, p.stdin = nil, nil
}

func (p *codexMCPClassifier) close() {
	p.stop()
}
