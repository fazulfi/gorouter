package realtime

import (
	"context"
	"sync"

	"gorouter/internal/domain/console"
)

// fakeUsageSource is a test-injected usage stream source. The state fields
// are guarded because the handler goroutine reads them while the test
// mutates them between updates.
type fakeUsageSource struct {
	mu        sync.Mutex
	updates   chan struct{}
	stats     StatsPayload
	active    int
	recent    []RecentRequestPayload
	errs      []string
	cancelled bool
}

func newFakeUsageSource() *fakeUsageSource {
	return &fakeUsageSource{updates: make(chan struct{}, 16)}
}

func (f *fakeUsageSource) Updates() <-chan struct{} { return f.updates }
func (f *fakeUsageSource) Stats(context.Context) (StatsPayload, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stats, nil
}
func (f *fakeUsageSource) ActiveRequests(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active, nil
}
func (f *fakeUsageSource) RecentRequests(context.Context) ([]RecentRequestPayload, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recent, nil
}
func (f *fakeUsageSource) ErrorProviders(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.errs, nil
}
func (f *fakeUsageSource) Cancel() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = true
}
func (f *fakeUsageSource) notify() {
	select {
	case f.updates <- struct{}{}:
	default:
	}
}

// fakeConsoleSource is a test-injected console stream source.
type fakeConsoleSource struct {
	mu        sync.Mutex
	updates   chan struct{}
	rows      []console.ConsoleLog
	cancelled bool
}

func newFakeConsoleSource() *fakeConsoleSource {
	return &fakeConsoleSource{updates: make(chan struct{}, 16)}
}

func (f *fakeConsoleSource) Updates() <-chan struct{} { return f.updates }
func (f *fakeConsoleSource) ListAfter(_ context.Context, seq int64, _ int) ([]console.ConsoleLog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []console.ConsoleLog
	for _, row := range f.rows {
		if row.Seq > seq {
			out = append(out, row)
		}
	}
	return out, nil
}
func (f *fakeConsoleSource) Cancel() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = true
}
func (f *fakeConsoleSource) notify() {
	select {
	case f.updates <- struct{}{}:
	default:
	}
}

// fakeProvidersSource is a test-injected providers stream source.
type fakeProvidersSource struct {
	mu        sync.Mutex
	updates   chan struct{}
	statuses  []ProviderStatusPayload
	cancelled bool
}

func newFakeProvidersSource() *fakeProvidersSource {
	return &fakeProvidersSource{updates: make(chan struct{}, 16)}
}

func (f *fakeProvidersSource) Updates() <-chan struct{} { return f.updates }
func (f *fakeProvidersSource) Status(context.Context) ([]ProviderStatusPayload, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statuses, nil
}
func (f *fakeProvidersSource) Cancel() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = true
}
func (f *fakeProvidersSource) notify() {
	select {
	case f.updates <- struct{}{}:
	default:
	}
}

// fakeJobsSource is a test-injected jobs stream source.
type fakeJobsSource struct {
	mu        sync.Mutex
	updates   chan struct{}
	snap      JobsSnapshot
	cancelled bool
}

func newFakeJobsSource() *fakeJobsSource {
	return &fakeJobsSource{updates: make(chan struct{}, 16)}
}

func (f *fakeJobsSource) Updates() <-chan struct{} { return f.updates }
func (f *fakeJobsSource) Snapshot(context.Context) (JobsSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap, nil
}
func (f *fakeJobsSource) Cancel() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = true
}
func (f *fakeJobsSource) notify() {
	select {
	case f.updates <- struct{}{}:
	default:
	}
}
