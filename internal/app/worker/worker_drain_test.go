package worker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/jobs"
)

// realisticStatusUpdater persists through the repository when the context is
// usable and rejects cancelled contexts, matching the production
// TransactionalJobUpdater (proven with the real manager and pool in
// worker_updater_test.go).
type realisticStatusUpdater struct {
	repo *mockJobRepo
}

func (u *realisticStatusUpdater) UpdateJobStatus(ctx context.Context, id uuid.UUID, status jobs.JobStatus, result json.RawMessage, errMsg *string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return u.repo.UpdateStatus(ctx, id, status, result, errMsg)
}

// blockingPipeline returns a pipeline whose work finishes only when the
// execution context is cancelled.
func blockingPipeline() *mockPipeline {
	return &mockPipeline{
		executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
}

func createPendingJob(t *testing.T, repo *mockJobRepo, id uuid.UUID) {
	t.Helper()
	payload, err := json.Marshal(map[string]interface{}{
		"body":  json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`),
		"model": "gpt-4",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := repo.Create(context.Background(), &jobs.Job{
		ID:      id,
		Type:    JobTypeChatCompletion,
		Status:  jobs.JobPending,
		Payload: payload,
	}); err != nil {
		t.Fatalf("create job: %v", err)
	}
}

// waitForJobStatus polls the repository until the job reaches the given
// status or the deadline elapses, failing the test otherwise.
func waitForJobStatus(t *testing.T, repo *mockJobRepo, id uuid.UUID, status jobs.JobStatus, timeout time.Duration) *jobs.Job {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := repo.FindByID(context.Background(), id)
		if err != nil {
			t.Fatalf("FindByID: %v", err)
		}
		if job != nil && job.Status == status {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, _ := repo.FindByID(context.Background(), id)
	if job == nil {
		t.Fatalf("job %s not found within %v", id, timeout)
	}
	t.Fatalf("job did not reach status %q within %v; current status %q", status, timeout, job.Status)
	return nil
}

// TestWorker_ShutdownDrainDoesNotStrandRunningJob: stopping the worker while
// a job is in flight must leave a recoverable persisted state (cancelled),
// never running.
func TestWorker_ShutdownDrainDoesNotStrandRunningJob(t *testing.T) {
	repo := newMockJobRepo()
	updater := &realisticStatusUpdater{repo: repo}

	jobID := uuid.New()
	createPendingJob(t, repo, jobID)

	w := workerWithUpdater(
		Config{PollInterval: 50 * time.Millisecond, MaxConcurrent: 1, RequestTimeout: 30 * time.Second, QueueSize: 10},
		repo, blockingPipeline(), updater,
	)
	w.Start(context.Background())

	waitForJobStatus(t, repo, jobID, jobs.JobRunning, 5*time.Second)
	w.Stop()

	waitForJobStatus(t, repo, jobID, jobs.JobCancelled, 5*time.Second)
}

// TestWorker_RequestTimeoutDoesNotStrandRunningJob: an expired execution
// context deadline must not prevent the terminal status transition, so the
// job ends in a recoverable persisted state rather than running.
func TestWorker_RequestTimeoutDoesNotStrandRunningJob(t *testing.T) {
	repo := newMockJobRepo()
	updater := &realisticStatusUpdater{repo: repo}

	jobID := uuid.New()
	createPendingJob(t, repo, jobID)

	w := workerWithUpdater(
		Config{PollInterval: 50 * time.Millisecond, MaxConcurrent: 1, RequestTimeout: 200 * time.Millisecond, QueueSize: 10},
		repo, blockingPipeline(), updater,
	)
	w.Start(context.Background())
	defer w.Stop()

	waitForJobStatus(t, repo, jobID, jobs.JobRunning, 5*time.Second)
	waitForJobStatus(t, repo, jobID, jobs.JobCancelled, 5*time.Second)
}
