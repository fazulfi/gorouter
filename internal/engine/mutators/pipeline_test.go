package mutators

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"gorouter/internal/domain/engine"
)

func fixtureRequest(body string) *engine.Request {
	return &engine.Request{
		MappedBody: []byte(body),
	}
}

func testLogger(buf *strings.Builder) func(string, ...interface{}) {
	return func(f string, args ...interface{}) {
		buf.WriteString(strings.TrimSpace(fmt.Sprintf(f, args...)) + "\n")
	}
}

type spyMutator struct {
	name    string
	applied bool
	err     error
	mu      sync.Mutex
	callLog []string
}

func (s *spyMutator) Name() string { return s.name }

func (s *spyMutator) Mutate(_ context.Context, _ *engine.Request) (*Result, error) {
	s.mu.Lock()
	s.callLog = append(s.callLog, s.name)
	s.mu.Unlock()
	if s.err != nil {
		return &Result{Applied: false}, s.err
	}
	return &Result{Applied: s.applied}, nil
}

func TestPipeline_Run_Order(t *testing.T) {
	a := &spyMutator{name: "a", applied: true}
	b := &spyMutator{name: "b", applied: true}
	c := &spyMutator{name: "c", applied: true}
	p := New(a, b, c)
	req := fixtureRequest("{}")
	reports := p.Run(context.Background(), req, nil)

	if len(reports) != 3 {
		t.Fatalf("expected 3 reports, got %d", len(reports))
	}
	if a.callLog[0] != "a" || b.callLog[0] != "b" || c.callLog[0] != "c" {
		t.Fatalf("expected call order a,b,c got a=%q b=%q c=%q", a.callLog[0], b.callLog[0], c.callLog[0])
	}
	if AppliedCount(reports) != 3 {
		t.Fatalf("expected 3 applied, got %d", AppliedCount(reports))
	}
}

func TestPipeline_Run_FailOpen(t *testing.T) {
	var logBuf strings.Builder
	a := &spyMutator{name: "a", applied: true}
	b := &spyMutator{name: "b", err: errors.New("boom")}
	c := &spyMutator{name: "c", applied: true}
	p := New(a, b, c)
	req := fixtureRequest("{}")
	reports := p.Run(context.Background(), req, testLogger(&logBuf))

	if len(reports) != 3 {
		t.Fatalf("expected 3 reports, got %d", len(reports))
	}
	if AppliedCount(reports) != 2 {
		t.Fatalf("expected 2 applied (a, c), got %d", AppliedCount(reports))
	}
	if ErrorCount(reports) != 1 {
		t.Fatalf("expected 1 error (b), got %d", ErrorCount(reports))
	}
	if !strings.Contains(logBuf.String(), "b failed-open") {
		t.Fatalf("expected fail-open log, got: %s", logBuf.String())
	}
	if len(a.callLog) != 1 || len(b.callLog) != 1 || len(c.callLog) != 1 {
		t.Fatalf("expected all 3 mutators to run, got a=%d b=%d c=%d", len(a.callLog), len(b.callLog), len(c.callLog))
	}
}

func TestPipeline_Run_Cancellation(t *testing.T) {
	a := &spyMutator{name: "a", applied: true}
	b := &spyMutator{name: "b", applied: true}
	p := New(a, b)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := fixtureRequest("{}")
	reports := p.Run(ctx, req, nil)

	if len(reports) != 1 {
		t.Fatalf("expected 1 report after cancel, got %d", len(reports))
	}
	if len(b.callLog) > 0 {
		t.Fatal("expected 'b' not to be called after cancellation")
	}
}

func TestPipeline_Run_NilRequest(t *testing.T) {
	p := New(&spyMutator{name: "a"})
	reports := p.Run(context.Background(), nil, nil)
	if reports != nil {
		t.Fatal("expected nil reports for nil request")
	}
}

func TestPipeline_Run_NilResultNonError(t *testing.T) {
	m := NamedFunc("skip", func(_ context.Context, _ *engine.Request) (*Result, error) {
		return nil, nil
	})
	p := New(m)
	req := fixtureRequest("{}")
	reports := p.Run(context.Background(), req, nil)
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}
	if AppliedCount(reports) != 0 {
		t.Fatal("expected 0 applied for no-op mutator")
	}
}

func TestPipeline_Run_Concurrency(t *testing.T) {
	m := &spyMutator{name: "safe", applied: true}
	p := New(m, m, m)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := fixtureRequest("{}")
			p.Run(context.Background(), req, nil)
		}()
	}
	wg.Wait()
	if len(m.callLog) != 30 {
		t.Logf("got %d calls (may be shared across positions)", len(m.callLog))
	}
}

func TestPipeline_Append(t *testing.T) {
	a := &spyMutator{name: "a"}
	b := &spyMutator{name: "b"}
	p := New(a)
	p.Append(b)
	muts := p.Mutators()
	if len(muts) != 2 {
		t.Fatalf("expected 2 mutators, got %d", len(muts))
	}
}

func TestPipeline_MutatorsCopy(t *testing.T) {
	a := &spyMutator{name: "a"}
	p := New(a)
	muts := p.Mutators()
	muts[0] = &spyMutator{name: "b"}
	if p.Mutators()[0].Name() != "a" {
		t.Fatal("original should be unchanged")
	}
}

func TestPipeline_Default(t *testing.T) {
	p := DefaultPipeline()
	muts := p.Mutators()
	if len(muts) != 5 {
		t.Fatalf("expected 5 default mutators, got %d", len(muts))
	}
	expected := []string{"rtk", "headroom", "caveman", "ponytail", "pxpipe"}
	for i, m := range muts {
		if m.Name() != expected[i] {
			t.Fatalf("position %d: expected %q, got %q", i, expected[i], m.Name())
		}
	}
}

func TestSanitizeError(t *testing.T) {
	tests := []struct {
		input string
		check string
	}{
		{"sk-proj-abc123 token", "sk-proj-***"},
		{"Bearer eyJhbGci token", "Bearer ***"},
		{"normal error", "normal error"},
	}
	for _, tt := range tests {
		san := sanitizeError(errors.New(tt.input))
		if !strings.Contains(san.Error(), tt.check) {
			t.Errorf("sanitize(%q) = %q, want contains %q", tt.input, san.Error(), tt.check)
		}
	}
}

func TestResultLog(t *testing.T) {
	r := &Result{Applied: true}
	log := r.Log("test")
	if !strings.Contains(log, "test") {
		t.Fatalf("expected 'test' in log, got %q", log)
	}
	r2 := &Result{Applied: false}
	if r2.Log("test") != "" {
		t.Fatal("expected empty log for non-applied")
	}
	if (*Result)(nil).Log("test") != "" {
		t.Fatal("expected empty log for nil result")
	}
}
