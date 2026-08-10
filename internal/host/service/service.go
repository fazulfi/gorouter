package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type GraceMode int

const (
	Graceful GraceMode = iota
	Force
)

type LifecycleStatus struct {
	Running   bool   `json:"running"`
	Port      int    `json:"port"`
	Mode      string `json:"mode"`
	PID       int    `json:"pid"`
	Autostart bool   `json:"autostart"`
}
type LogOption func(*logOptions)
type logOptions struct {
	Tail   int
	Follow bool
}

func Tail(n int) LogOption { return func(o *logOptions) { o.Tail = n } }
func Follow() LogOption    { return func(o *logOptions) { o.Follow = true } }

type Lifecycle interface {
	RunFirstRun() error
	Status() (LifecycleStatus, error)
	Stop(GraceMode) error
	Logs(...LogOption) (<-chan string, error)
	AutostartEnabled() bool
}
type Manager struct {
	mu     sync.Mutex
	status LifecycleStatus
	logs   []string
}

func statePath() string {
	if p := os.Getenv("GOROUTER_STATE_FILE"); p != "" {
		return p
	}
	d, e := os.UserConfigDir()
	if e != nil {
		return filepath.Join(os.TempDir(), "gorouter-lifecycle.json")
	}
	return filepath.Join(d, "gorouter", "lifecycle.json")
}
func logPath() string               { return statePath() + ".log" }
func (m *Manager) lockPath() string { return statePath() + ".lock" }
func (m *Manager) withBoundary(fn func() error) error {
	p := m.lockPath()
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("lifecycle is busy")
	}
	_ = f.Close()
	defer os.Remove(p)
	return fn()
}
func NewManager(port int, mode string) *Manager {
	return &Manager{status: LifecycleStatus{Port: port, Mode: mode}}
}
func (m *Manager) RunFirstRun() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.withBoundary(func() error {
		if _, e := os.Stat(statePath()); e == nil {
			return errors.New("already running")
		}
		m.status.Running = true
		m.status.PID = os.Getpid()
		return writeState(m.status)
	})
}
func (m *Manager) Status() (LifecycleStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, e := readState(); e == nil {
		return s, nil
	}
	return m.status, nil
}
func (m *Manager) Stop(mode GraceMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.withBoundary(func() error {
		if _, e := os.Stat(statePath()); e != nil {
			return errors.New("not running")
		}
		return os.Remove(statePath())
	})
}
func (m *Manager) Logs(opts ...LogOption) (<-chan string, error) {
	o := logOptions{}
	for _, f := range opts {
		f(&o)
	}
	b, e := os.ReadFile(logPath())
	if e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	lines := splitLines(string(b))
	if o.Tail > 0 && o.Tail < len(lines) {
		lines = lines[len(lines)-o.Tail:]
	}
	ch := make(chan string, len(lines)+8)
	for _, l := range lines {
		ch <- l
	}
	if !o.Follow {
		close(ch)
		return ch, nil
	}
	go followLog(logPath(), ch, len(b))
	return ch, nil
}
func (m *Manager) AutostartEnabled() bool { s, e := readState(); return e == nil && s.Autostart }
func (m *Manager) SetAutostart(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.withBoundary(func() error {
		s, e := readState()
		if e != nil {
			s = m.status
		}
		s.Autostart = v
		m.status.Autostart = v
		return writeState(s)
	})
}
func (m *Manager) AddLog(line string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := fmt.Sprintln(line)
	m.logs = append(m.logs, entry)
	_ = os.MkdirAll(filepath.Dir(logPath()), 0700)
	f, e := os.OpenFile(logPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e == nil {
		_, _ = f.WriteString(entry)
		_ = f.Close()
	}
}
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	x := strings.SplitAfter(s, "\n")
	if x[len(x)-1] == "" {
		x = x[:len(x)-1]
	}
	return x
}
func followLog(p string, ch chan<- string, o int) {
	defer close(ch)
	for i := 0; i < 100; i++ {
		b, e := os.ReadFile(p)
		if e == nil && len(b) > o {
			for _, l := range splitLines(string(b[o:])) {
				ch <- l
			}
			o = len(b)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func writeState(s LifecycleStatus) error {
	if e := os.MkdirAll(filepath.Dir(statePath()), 0700); e != nil {
		return e
	}
	b, _ := json.Marshal(s)
	tmp := statePath() + ".tmp"
	if e := os.WriteFile(tmp, b, 0600); e != nil {
		return e
	}
	return os.Rename(tmp, statePath())
}
func readState() (LifecycleStatus, error) {
	b, e := os.ReadFile(statePath())
	var s LifecycleStatus
	if e != nil {
		return s, e
	}
	e = json.Unmarshal(b, &s)
	return s, e
}
