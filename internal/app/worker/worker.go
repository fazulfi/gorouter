package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/jobs"
)

// JobTypeChatCompletion is the job type for async chat completion requests.
const JobTypeChatCompletion = "chat_completion"

// Config controls the worker's polling and execution behaviour.
type Config struct {
	PollInterval   time.Duration
	MaxConcurrent  int
	RequestTimeout time.Duration
	QueueSize      int
}

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() Config {
	return Config{
		PollInterval:   5 * time.Second,
		MaxConcurrent:  5,
		RequestTimeout: 300 * time.Second,
		QueueSize:      100,
	}
}

func (c *Config) validate() {
	if c.PollInterval <= 0 {
		c.PollInterval = DefaultConfig().PollInterval
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = DefaultConfig().MaxConcurrent
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = DefaultConfig().RequestTimeout
	}
	if c.QueueSize <= 0 {
		c.QueueSize = DefaultConfig().QueueSize
	}
}

// JobStatusUpdater handles transactional job status updates. The production
// implementation wraps tx.TransactionManager to ensure status changes are
// atomic. Tests provide a mock.
type JobStatusUpdater interface {
	UpdateJobStatus(ctx context.Context, id uuid.UUID, status jobs.JobStatus, result json.RawMessage, errMsg *string) error
}

// ScopeBeginner begins a transaction scope. tx.TransactionManager satisfies
// this interface; it is an interface so tests can drive the updater with a
// recording scope.
type ScopeBeginner interface {
	Begin(ctx context.Context) (*tx.TxScope, error)
}

// TransactionalJobUpdater implements JobStatusUpdater by delegating to a
// tx.TransactionManager for atomic status changes.
type TransactionalJobUpdater struct {
	beginner ScopeBeginner
}

// NewTransactionalJobUpdater creates a JobStatusUpdater that persists status
// changes through the transaction manager, ensuring atomicity.
func NewTransactionalJobUpdater(beginner ScopeBeginner) *TransactionalJobUpdater {
	return &TransactionalJobUpdater{beginner: beginner}
}

// UpdateJobStatus begins a single transaction, delegates the status change to
// the scoped JobRepository, and commits. On any error the transaction is
// rolled back. No repository is called outside the scope, so a status
// transition uses exactly one pool connection and its atomicity is real.
func (u *TransactionalJobUpdater) UpdateJobStatus(
	ctx context.Context,
	id uuid.UUID,
	status jobs.JobStatus,
	result json.RawMessage,
	errMsg *string,
) error {
	scope, err := u.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		_ = scope.Rollback(ctx)
	}()

	if err := scope.Jobs().UpdateStatus(ctx, id, status, result, errMsg); err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	return scope.Commit(ctx)
}

// Worker polls the jobs table for pending jobs and processes them in the
// background using the engine pipeline. It is non-blocking on start and
// supports graceful shutdown via Stop().
type Worker struct {
	config    Config
	jobRepo   jobs.JobRepository
	pipeline  engine.ExecutePipeline
	txManager *tx.TransactionManager
	updater   JobStatusUpdater
	logger    zerolog.Logger

	signals chan uuid.UUID
	sem     chan struct{}

	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.Mutex
	stopped bool
}

// New creates a Worker with the given dependencies. If updater is nil, a
// default TransactionalJobUpdater is constructed from the txManager.
func New(
	cfg Config,
	jobRepo jobs.JobRepository,
	pipeline engine.ExecutePipeline,
	txManager *tx.TransactionManager,
	logger zerolog.Logger,
) *Worker {
	cfg.validate()

	w := &Worker{
		config:    cfg,
		jobRepo:   jobRepo,
		pipeline:  pipeline,
		txManager: txManager,
		logger:    logger.With().Str("component", "worker").Logger(),
		signals:   make(chan uuid.UUID, cfg.QueueSize),
		sem:       make(chan struct{}, cfg.MaxConcurrent),
	}
	w.updater = NewTransactionalJobUpdater(txManager)
	return w
}

// Start begins polling the jobs table for pending work. It returns
// immediately; the poll loop runs in a background goroutine.
func (w *Worker) Start(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ctx != nil {
		w.logger.Warn().Msg("worker already started")
		return
	}
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.stopped = false
	w.wg.Add(1)
	go w.pollLoop()
	w.logger.Info().
		Dur("poll_interval", w.config.PollInterval).
		Int("max_concurrent", w.config.MaxConcurrent).
		Msg("worker started")
}

// Stop gracefully shuts down the worker. It cancels the internal context,
// which terminates the poll loop, then waits for all in-flight job goroutines
// to complete.
func (w *Worker) Stop() {
	w.mu.Lock()
	cancelFn := w.cancel
	if cancelFn != nil {
		w.stopped = true
	}
	w.mu.Unlock()

	if cancelFn != nil {
		cancelFn()
	}
	w.wg.Wait()
	w.logger.Info().Msg("worker stopped")
}

// Enqueue signals the worker to check for a specific job immediately, rather
// than waiting for the next poll cycle. Returns ErrWorkerStopped if the worker
// has been shut down, or ErrSignalQueueFull if the internal signal channel is
// at capacity.
func (w *Worker) Enqueue(_ context.Context, jobID uuid.UUID) error {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return ErrWorkerStopped
	}
	w.mu.Unlock()

	select {
	case w.signals <- jobID:
		return nil
	default:
		return ErrSignalQueueFull
	}
}

// pollLoop runs the main polling loop. It listens on three channels: the
// context cancellation (shutdown), the periodic ticker (poll), and the signals
// channel (direct job enqueue).
func (w *Worker) pollLoop() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Info().Msg("poll loop terminated")
			return

		case <-ticker.C:
			w.poll()

		case jobID := <-w.signals:
			w.logger.Debug().Str("job_id", jobID.String()).Msg("processing signaled job")
			w.processSignaledJob(jobID)
		}
	}
}

// poll queries the repository for pending jobs and spawns a goroutine for
// each one, subject to the MaxConcurrent semaphore.
func (w *Worker) poll() {
	pending, err := w.jobRepo.FindPending(w.ctx, w.config.MaxConcurrent)
	if err != nil {
		w.logger.Error().Err(err).Msg("failed to poll pending jobs")
		return
	}

	for i := range pending {
		job := pending[i]
		if !w.acquireSem() {
			return
		}

		jobCtx, cancel := context.WithTimeout(w.ctx, w.config.RequestTimeout)
		w.wg.Add(1)
		go func(j jobs.Job) {
			defer w.wg.Done()
			defer w.releaseSem()
			defer cancel()
			w.processJob(jobCtx, &j)
		}(job)
	}
}

// processSignaledJob looks up a job by ID and, if it is still pending, starts
// processing it outside the normal poll cycle.
func (w *Worker) processSignaledJob(jobID uuid.UUID) {
	job, err := w.jobRepo.FindByID(w.ctx, jobID)
	if err != nil {
		w.logger.Error().Err(err).Str("job_id", jobID.String()).Msg("failed to lookup signaled job")
		return
	}
	if job == nil {
		w.logger.Warn().Str("job_id", jobID.String()).Msg("signaled job not found")
		return
	}
	if job.Status != jobs.JobPending {
		w.logger.Debug().Str("job_id", jobID.String()).Str("status", string(job.Status)).Msg("signaled job not pending")
		return
	}

	if !w.acquireSem() {
		return
	}

	jobCtx, cancel := context.WithTimeout(w.ctx, w.config.RequestTimeout)
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer w.releaseSem()
		defer cancel()
		w.processJob(jobCtx, job)
	}()
}

func (w *Worker) acquireSem() bool {
	select {
	case w.sem <- struct{}{}:
		return true
	case <-w.ctx.Done():
		return false
	}
}

func (w *Worker) releaseSem() {
	<-w.sem
}

// processJob handles the full lifecycle of a single job:
//  1. Updates the job status to running
//  2. Reconstructs an engine.Request from the job payload
//  3. Executes the request through the pipeline
//  4. Stores the result and marks the job completed, or marks it failed on error
//
// All status updates are performed through the JobStatusUpdater, which in
// production wraps tx.TransactionManager for atomicity.
func (w *Worker) processJob(ctx context.Context, job *jobs.Job) {
	log := w.logger.With().
		Str("job_id", job.ID.String()).
		Str("job_type", job.Type).
		Str("status", string(job.Status)).
		Logger()

	log.Info().Msg("processing job")
	if err := w.updateJobStatus(ctx, job.ID, jobs.JobRunning, nil, nil); err != nil {
		log.Error().Err(err).Msg("failed to set job status to running")
		return
	}

	req, err := w.buildRequest(job)
	if err != nil {
		errMsg := err.Error()
		if updateErr := w.updateJobStatus(ctx, job.ID, jobs.JobFailed, nil, &errMsg); updateErr != nil {
			log.Error().Err(updateErr).Msg("failed to persist job failure")
		}
		log.Error().Err(err).Msg("failed to build engine request from job payload")
		return
	}

	resp, err := w.pipeline.ExecuteRequest(ctx, req)
	if err != nil {
		errMsg := fmt.Sprintf("execution failed: %v", err)
		if updateErr := w.updateJobStatus(ctx, job.ID, jobs.JobFailed, nil, &errMsg); updateErr != nil {
			log.Error().Err(updateErr).Msg("failed to persist job failure")
		}
		log.Error().Err(err).Msg("job execution failed")
		return
	}

	result, err := json.Marshal(resp)
	if err != nil {
		errMsg := fmt.Sprintf("failed to marshal response: %v", err)
		if updateErr := w.updateJobStatus(ctx, job.ID, jobs.JobFailed, nil, &errMsg); updateErr != nil {
			log.Error().Err(updateErr).Msg("failed to persist job failure")
		}
		log.Error().Err(err).Msg("failed to marshal response")
		return
	}

	if err := w.updateJobStatus(ctx, job.ID, jobs.JobCompleted, result, nil); err != nil {
		log.Error().Err(err).Msg("failed to persist job completion")
		return
	}
	log.Info().Int("result_bytes", len(result)).Msg("job completed successfully")
}

func (w *Worker) buildRequest(job *jobs.Job) (*engine.Request, error) {
	var payload struct {
		Body   json.RawMessage `json:"body"`
		Model  string          `json:"model,omitempty"`
		Stream bool            `json:"stream,omitempty"`
	}
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal job payload: %w", err)
	}
	if len(payload.Body) == 0 {
		return nil, fmt.Errorf("job payload missing 'body' field")
	}

	return &engine.Request{
		ID:      job.ID,
		RawBody: payload.Body,
		Stream:  payload.Stream,
		Model:   payload.Model,
	}, nil
}

func (w *Worker) updateJobStatus(
	ctx context.Context,
	jobID uuid.UUID,
	status jobs.JobStatus,
	result json.RawMessage,
	errMsg *string,
) error {
	return w.updater.UpdateJobStatus(ctx, jobID, status, result, errMsg)
}
