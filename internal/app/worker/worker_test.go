package worker

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/jobs"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

type mockJobRepo struct {
	mu             sync.Mutex
	jobs           map[uuid.UUID]*jobs.Job
	findPendingFn  func(ctx context.Context, limit int) ([]jobs.Job, error)
	createFn       func(ctx context.Context, job *jobs.Job) error
	updateStatusFn func(ctx context.Context, id uuid.UUID, status jobs.JobStatus, result json.RawMessage, errMsg *string) error
}

func newMockJobRepo() *mockJobRepo {
	return &mockJobRepo{
		jobs: make(map[uuid.UUID]*jobs.Job),
	}
}

func (m *mockJobRepo) FindByID(_ context.Context, id uuid.UUID) (*jobs.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return nil, nil
	}
	cp := *job
	return &cp, nil
}

func (m *mockJobRepo) FindPending(ctx context.Context, limit int) ([]jobs.Job, error) {
	if m.findPendingFn != nil {
		return m.findPendingFn(ctx, limit)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var pending []jobs.Job
	for _, job := range m.jobs {
		if job.Status == jobs.JobPending {
			cp := *job
			pending = append(pending, cp)
			if len(pending) >= limit {
				break
			}
		}
	}
	return pending, nil
}

func (m *mockJobRepo) Create(_ context.Context, job *jobs.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *job
	m.jobs[job.ID] = &cp
	return nil
}

func (m *mockJobRepo) UpdateStatus(_ context.Context, id uuid.UUID, status jobs.JobStatus, result json.RawMessage, errMsg *string) error {
	if m.updateStatusFn != nil {
		return m.updateStatusFn(context.Background(), id, status, result, errMsg)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return errors.New("job not found")
	}
	job.Status = status
	if result != nil {
		job.Result = result
	}
	if errMsg != nil {
		job.ErrorMessage = errMsg
	}
	return nil
}

type mockPipeline struct {
	executeFn func(ctx context.Context, req *engine.Request) (*engine.Response, error)
}

func (m *mockPipeline) ExecuteRequest(ctx context.Context, req *engine.Request) (*engine.Response, error) {
	return m.executeFn(ctx, req)
}

type mockStatusUpdater struct {
	mu      sync.Mutex
	updates []statusUpdate
}

type statusUpdate struct {
	JobID  uuid.UUID
	Status jobs.JobStatus
	Result json.RawMessage
	ErrMsg *string
}

func newMockStatusUpdater() *mockStatusUpdater {
	return &mockStatusUpdater{}
}

func (u *mockStatusUpdater) UpdateJobStatus(_ context.Context, id uuid.UUID, status jobs.JobStatus, result json.RawMessage, errMsg *string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.updates = append(u.updates, statusUpdate{
		JobID:  id,
		Status: status,
		Result: result,
		ErrMsg: errMsg,
	})
	return nil
}

func (u *mockStatusUpdater) updatesLen() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.updates)
}

func (u *mockStatusUpdater) hasStatus(s jobs.JobStatus) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, upd := range u.updates {
		if upd.Status == s {
			return true
		}
	}
	return false
}

func workerWithUpdater(
	cfg Config,
	repo jobs.JobRepository,
	pipeline engine.ExecutePipeline,
	updater JobStatusUpdater,
) *Worker {
	cfg.validate()
	return &Worker{
		config:   cfg,
		jobRepo:  repo,
		pipeline: pipeline,
		updater:  updater,
		logger:   noopLogger(),
		signals:  make(chan uuid.UUID, cfg.QueueSize),
		sem:      make(chan struct{}, cfg.MaxConcurrent),
	}
}

func noopLogger() zerolog.Logger {
	return zerolog.New(nil).Level(zerolog.Disabled)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("expected PollInterval=5s, got %v", cfg.PollInterval)
	}
	if cfg.MaxConcurrent != 5 {
		t.Errorf("expected MaxConcurrent=5, got %d", cfg.MaxConcurrent)
	}
	if cfg.RequestTimeout != 300*time.Second {
		t.Errorf("expected RequestTimeout=300s, got %v", cfg.RequestTimeout)
	}
	if cfg.QueueSize != 100 {
		t.Errorf("expected QueueSize=100, got %d", cfg.QueueSize)
	}
}

func TestConfig_Validation(t *testing.T) {
	cfg := Config{}
	cfg.validate()
	if cfg.PollInterval <= 0 {
		t.Error("expected PollInterval to be defaulted")
	}
	if cfg.MaxConcurrent <= 0 {
		t.Error("expected MaxConcurrent to be defaulted")
	}
	if cfg.RequestTimeout <= 0 {
		t.Error("expected RequestTimeout to be defaulted")
	}
	if cfg.QueueSize <= 0 {
		t.Error("expected QueueSize to be defaulted")
	}
}

func TestWorker_StartStop(t *testing.T) {
	repo := newMockJobRepo()
	pipeline := &mockPipeline{
		executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
			return &engine.Response{Body: []byte(`ok`)}, nil
		},
	}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(
		Config{PollInterval: 100 * time.Millisecond, MaxConcurrent: 2},
		repo, pipeline, updater,
	)

	w.Start(context.Background())
	defer w.Stop()

	w.mu.Lock()
	started := w.ctx != nil
	w.mu.Unlock()
	if !started {
		t.Fatal("expected worker to be started")
	}
}

func TestWorker_StartTwice(t *testing.T) {
	repo := newMockJobRepo()
	pipeline := &mockPipeline{}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(DefaultConfig(), repo, pipeline, updater)

	w.Start(context.Background())
	w.Start(context.Background())

	w.Stop()
}

func TestWorker_EnqueueAfterStop(t *testing.T) {
	repo := newMockJobRepo()
	pipeline := &mockPipeline{}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(DefaultConfig(), repo, pipeline, updater)

	w.Start(context.Background())
	w.Stop()

	err := w.Enqueue(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error when enqueuing after stop")
	}
}

func TestWorker_EnqueueSuccess(t *testing.T) {
	repo := newMockJobRepo()
	pipeline := &mockPipeline{}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(DefaultConfig(), repo, pipeline, updater)

	w.Start(context.Background())
	defer w.Stop()

	err := w.Enqueue(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWorker_JobLifecycle_Success(t *testing.T) {
	repo := newMockJobRepo()
	updater := newMockStatusUpdater()

	var executedRequestID uuid.UUID
	pipeline := &mockPipeline{
		executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
			executedRequestID = req.ID
			return &engine.Response{
				RequestID:  req.ID,
				Body:       []byte(`{"choices":[{"message":{"content":"hello"}}]}`),
				Model:      "gpt-4",
				StatusCode: 200,
			}, nil
		},
	}

	jobID := uuid.New()
	payload, _ := json.Marshal(map[string]interface{}{
		"body":  json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`),
		"model": "gpt-4",
	})
	_ = repo.Create(context.Background(), &jobs.Job{
		ID:      jobID,
		Type:    JobTypeChatCompletion,
		Status:  jobs.JobPending,
		Payload: payload,
	})

	w := workerWithUpdater(
		Config{PollInterval: 50 * time.Millisecond, MaxConcurrent: 2, RequestTimeout: 5 * time.Second, QueueSize: 10},
		repo, pipeline, updater,
	)

	w.Start(context.Background())
	defer w.Stop()

	time.Sleep(500 * time.Millisecond)

	if updater.updatesLen() < 2 {
		t.Fatalf("expected at least 2 status updates (running + completed), got %d", updater.updatesLen())
	}
	if !updater.hasStatus(jobs.JobCompleted) {
		t.Fatal("expected JobCompleted status")
	}
	if executedRequestID != jobID {
		t.Fatalf("expected executed request ID %s, got %s", jobID.String(), executedRequestID.String())
	}
}

func TestWorker_JobLifecycle_Failure(t *testing.T) {
	repo := newMockJobRepo()
	updater := newMockStatusUpdater()

	pipeline := &mockPipeline{
		executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
			return nil, errors.New("provider unavailable")
		},
	}

	jobID := uuid.New()
	payload, _ := json.Marshal(map[string]interface{}{
		"body":  json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`),
		"model": "gpt-4",
	})
	_ = repo.Create(context.Background(), &jobs.Job{
		ID:      jobID,
		Type:    JobTypeChatCompletion,
		Status:  jobs.JobPending,
		Payload: payload,
	})

	w := workerWithUpdater(
		Config{PollInterval: 50 * time.Millisecond, MaxConcurrent: 2, RequestTimeout: 5 * time.Second, QueueSize: 10},
		repo, pipeline, updater,
	)

	w.Start(context.Background())
	defer w.Stop()

	time.Sleep(500 * time.Millisecond)

	if updater.updatesLen() < 2 {
		t.Fatalf("expected at least 2 status updates (running + failed), got %d", updater.updatesLen())
	}
	if !updater.hasStatus(jobs.JobFailed) {
		t.Fatal("expected JobFailed status")
	}
}

func TestWorker_ConcurrentLimit(t *testing.T) {
	var mu sync.Mutex
	runningCount := 0
	maxSeen := 0

	repo := newMockJobRepo()
	updater := newMockStatusUpdater()

	var started atomic.Int32
	pipeline := &mockPipeline{
		executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
			started.Add(1)
			mu.Lock()
			runningCount++
			if runningCount > maxSeen {
				maxSeen = runningCount
			}
			mu.Unlock()

			// Block until context cancels, simulating slow work.
			<-ctx.Done()

			mu.Lock()
			runningCount--
			mu.Unlock()
			return nil, ctx.Err()
		},
	}

	const maxConcurrent = 3
	const jobCount = 10
	for i := 0; i < jobCount; i++ {
		payload, _ := json.Marshal(map[string]interface{}{
			"body":  json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`),
			"model": "gpt-4",
		})
		_ = repo.Create(context.Background(), &jobs.Job{
			ID:      uuid.New(),
			Type:    JobTypeChatCompletion,
			Status:  jobs.JobPending,
			Payload: payload,
		})
	}

	w := workerWithUpdater(
		Config{PollInterval: 50 * time.Millisecond, MaxConcurrent: maxConcurrent, RequestTimeout: 2 * time.Second, QueueSize: 100},
		repo, pipeline, updater,
	)

	w.Start(context.Background())
	defer w.Stop()

	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	seen := maxSeen
	mu.Unlock()

	if seen > maxConcurrent {
		t.Fatalf("expected max concurrent executions <= %d, got %d", maxConcurrent, seen)
	}
}

func TestWorker_ContextCancellation(t *testing.T) {
	repo := newMockJobRepo()
	updater := newMockStatusUpdater()

	blocker := make(chan struct{})
	pipeline := &mockPipeline{
		executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
			select {
			case <-blocker:
			case <-ctx.Done():
			}
			return nil, ctx.Err()
		},
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"body":  json.RawMessage(`{}`),
		"model": "gpt-4",
	})
	_ = repo.Create(context.Background(), &jobs.Job{
		ID:      uuid.New(),
		Type:    JobTypeChatCompletion,
		Status:  jobs.JobPending,
		Payload: payload,
	})

	w := workerWithUpdater(
		Config{PollInterval: 50 * time.Millisecond, MaxConcurrent: 1, RequestTimeout: 5 * time.Second, QueueSize: 10},
		repo, pipeline, updater,
	)

	w.Start(context.Background())
	time.Sleep(200 * time.Millisecond)

	w.Stop()
	close(blocker)
}

func TestWorker_InvalidPayload(t *testing.T) {
	repo := newMockJobRepo()
	updater := newMockStatusUpdater()

	pipeline := &mockPipeline{
		executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
			return &engine.Response{}, nil
		},
	}

	_ = repo.Create(context.Background(), &jobs.Job{
		ID:      uuid.New(),
		Type:    JobTypeChatCompletion,
		Status:  jobs.JobPending,
		Payload: json.RawMessage(`not valid json`),
	})

	w := workerWithUpdater(
		Config{PollInterval: 50 * time.Millisecond, MaxConcurrent: 2, RequestTimeout: 5 * time.Second, QueueSize: 10},
		repo, pipeline, updater,
	)

	w.Start(context.Background())
	defer w.Stop()

	time.Sleep(500 * time.Millisecond)

	if updater.updatesLen() < 2 {
		t.Fatalf("expected at least 2 status updates, got %d", updater.updatesLen())
	}
	if !updater.hasStatus(jobs.JobFailed) {
		t.Fatal("expected JobFailed for invalid payload")
	}
}

func TestWorker_EnqueueTriggersProcessing(t *testing.T) {
	repo := newMockJobRepo()
	updater := newMockStatusUpdater()

	pipeline := &mockPipeline{
		executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
			return &engine.Response{
				RequestID:  req.ID,
				Body:       []byte(`ok`),
				Model:      "gpt-4",
				StatusCode: 200,
			}, nil
		},
	}

	w := workerWithUpdater(
		Config{PollInterval: 1 * time.Hour, MaxConcurrent: 2, RequestTimeout: 5 * time.Second, QueueSize: 10},
		repo, pipeline, updater,
	)

	w.Start(context.Background())
	defer w.Stop()

	jobID := uuid.New()
	payload, _ := json.Marshal(map[string]interface{}{
		"body":  json.RawMessage(`{"model":"gpt-4"}`),
		"model": "gpt-4",
	})
	_ = repo.Create(context.Background(), &jobs.Job{
		ID:      jobID,
		Type:    JobTypeChatCompletion,
		Status:  jobs.JobPending,
		Payload: payload,
	})

	if err := w.Enqueue(context.Background(), jobID); err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	if updater.updatesLen() < 2 {
		t.Fatalf("expected job processed via enqueue (>=2 updates), got %d", updater.updatesLen())
	}
}

func TestNewHandler_Config(t *testing.T) {
	repo := newMockJobRepo()
	pipeline := &mockPipeline{}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(DefaultConfig(), repo, pipeline, updater)

	h := NewHandler(w, repo, nil, noopLogger())
	if h.config.DefaultModel != "gpt-4" {
		t.Errorf("expected default model gpt-4, got %s", h.config.DefaultModel)
	}
}
