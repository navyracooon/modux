package frontend

import (
	"io"
	"os"
	"sync"
	"time"
)

const matchBufMax = 64 * 1024

// Monitor forwards PTY output to the user's terminal untouched, while
// optionally matching switch-completion patterns on the same stream.
type Monitor struct {
	out io.Writer // forwarding target; os.Stdout outside tests

	mu     sync.Mutex
	detect func([]byte) bool
	buf    []byte
	done   chan struct{}

	eofOnce sync.Once
	eof     chan struct{}
}

func NewMonitor() *Monitor {
	return &Monitor{out: os.Stdout, eof: make(chan struct{})}
}

// Run pumps PTY output to the terminal until the PTY closes (child exit).
func (m *Monitor) Run(ptmx io.Reader) {
	defer m.eofOnce.Do(func() { close(m.eof) })

	buf := make([]byte, 4096)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			_, _ = m.out.Write(chunk)
			m.feed(chunk)
		}
		if err != nil {
			return
		}
	}
}

// EOF is closed when the PTY stream ends.
func (m *Monitor) EOF() <-chan struct{} { return m.eof }

// Arm starts matching subsequent output with detect. The returned channel is
// closed once detect reports completion. Call Disarm when done waiting.
func (m *Monitor) Arm(detect func([]byte) bool) <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.detect = detect
	m.buf = m.buf[:0]
	m.done = make(chan struct{})
	return m.done
}

// Snapshot returns a copy of the bytes accumulated since Arm.
func (m *Monitor) Snapshot() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.buf...)
}

// WaitFor blocks until detect matches the output stream or the timeout
// expires, returning the accumulated output and whether it matched.
func (m *Monitor) WaitFor(detect func([]byte) bool, timeout time.Duration) ([]byte, bool) {
	done := m.Arm(detect)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var matched bool
	select {
	case <-done:
		matched = true
	case <-timer.C:
	}
	buf := m.Snapshot()
	m.Disarm()
	return buf, matched
}

// Disarm stops pattern matching.
func (m *Monitor) Disarm() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.detect = nil
	m.buf = nil
	m.done = nil
}

func (m *Monitor) feed(chunk []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.detect == nil {
		return
	}
	m.buf = append(m.buf, chunk...)
	if len(m.buf) > matchBufMax {
		m.buf = m.buf[len(m.buf)-matchBufMax:]
	}
	if m.detect(m.buf) {
		close(m.done)
		// Keep buf so Snapshot can return what was matched; Disarm clears it.
		m.detect = nil
		m.done = nil
	}
}
