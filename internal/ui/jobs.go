package ui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// jobManager runs one long operation at a time (they share nginx/MySQL state).
type jobManager struct {
	mu   sync.Mutex
	jobs map[string]*job
	cur  *job
	ids  []string
}

type job struct {
	ID      string
	Title   string
	mu      sync.Mutex
	cancel  context.CancelFunc
	status  string // running | done | failed
	err     string
	lines   []string
	cur     []byte
	started time.Time
}

type jobSnapshot struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	Log     string `json:"log"`
	Seconds int    `json:"seconds"`
}

func newJobManager() *jobManager { return &jobManager{jobs: map[string]*job{}} }

func (m *jobManager) start(title string, run func(context.Context, io.Writer) error) (*job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cur != nil {
		m.cur.mu.Lock()
		running := m.cur.status == "running"
		t := m.cur.Title
		m.cur.mu.Unlock()
		if running {
			return nil, fmt.Errorf("another operation is running (%s). Wait for it to finish", t)
		}
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	j := &job{ID: hex.EncodeToString(b), Title: title, status: "running", started: time.Now()}
	m.jobs[j.ID] = j
	m.cur = j
	m.ids = append(m.ids, j.ID)
	if len(m.ids) > 20 {
		delete(m.jobs, m.ids[0])
		m.ids = m.ids[1:]
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.cancel = cancel
	go func() {
		defer cancel()
		err := run(ctx, j)
		j.mu.Lock()
		defer j.mu.Unlock()
		if err != nil {
			j.status = "failed"
			j.err = err.Error()
		} else {
			j.status = "done"
		}
	}()
	return j, nil
}

// cancelCurrent asks the running operation to stop. It reports whether one was running.
func (m *jobManager) cancelCurrent() bool { //nolint:unused // used by the native window (Windows, macOS with cgo)
	m.mu.Lock()
	j := m.cur
	m.mu.Unlock()
	if j == nil {
		return false
	}
	j.mu.Lock()
	running, cancel := j.status == "running", j.cancel
	j.mu.Unlock()
	if running && cancel != nil {
		cancel()
		return true
	}
	return false
}

func (m *jobManager) get(id string) (*job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	return j, ok
}

func (m *jobManager) current() *job {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cur
}

// Write collects output. A carriage return overwrites the current line, so
// progress such as "  42%" shows once instead of hundreds of times.
func (j *job) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, b := range p {
		switch b {
		case '\n':
			j.lines = append(j.lines, string(j.cur))
			j.cur = j.cur[:0]
		case '\r':
			j.cur = j.cur[:0]
		default:
			j.cur = append(j.cur, b)
		}
	}
	if len(j.lines) > 2000 {
		j.lines = j.lines[len(j.lines)-2000:]
	}
	return len(p), nil
}

func (j *job) snapshot() jobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	all := j.lines
	if len(j.cur) > 0 {
		all = append(append([]string{}, j.lines...), string(j.cur))
	}
	return jobSnapshot{ID: j.ID, Title: j.Title, Status: j.status, Error: j.err,
		Log: strings.Join(all, "\n"), Seconds: int(time.Since(j.started).Seconds())}
}
