package worker

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/jobs"
)

// Job represents a scheduled or triggered background job.
type Job = jobs.Job

// JobType is an alias for jobs.JobType.
type JobType = jobs.JobType

// Handler provides HTTP endpoints for enqueuing async jobs and checking their
// status. Routes are mountable on a chi.Router.
type Handler struct {
	worker    *Worker
	jobRepo   jobs.JobRepository
	txManager *tx.TransactionManager
	config    struct {
		DefaultModel string
	}
	logger zerolog.Logger
}

// NewHandler creates a Handler with the given dependencies.
func NewHandler(worker *Worker, jobRepo jobs.JobRepository, txManager *tx.TransactionManager, logger zerolog.Logger) *Handler {
	return &Handler{
		worker:    worker,
		jobRepo:   jobRepo,
		txManager: txManager,
		config: struct {
			DefaultModel string
		}{
			DefaultModel: "gpt-4",
		},
		logger: logger.With().Str("component", "worker-handler").Logger(),
	}
}

// RegisterRoutes mounts the worker-related endpoints on the given router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/v1/chat/completions/async", h.HandleAsyncChat)
	r.Get("/v1/jobs/{id}", h.HandleJobStatus)
}

// asyncChatRequest is the JSON body accepted by the async chat endpoint.
type asyncChatRequest struct {
	Model   string          `json:"model"`
	Stream  bool            `json:"stream"`
	Body    json.RawMessage `json:"body"`
	MaxBody json.RawMessage `json:"max_body,omitempty"` // compatibility alias
}

// asyncResponse is returned when a job is successfully enqueued.
type asyncResponse struct {
	ID      uuid.UUID `json:"id"`
	Status  string    `json:"status"`
	Message string    `json:"message"`
}

// jobStatusResponse is returned by the job status endpoint.
type jobStatusResponse struct {
	ID           uuid.UUID       `json:"id"`
	Type         string          `json:"type"`
	Status       jobs.JobStatus  `json:"status"`
	Result       json.RawMessage `json:"result,omitempty"`
	ErrorMessage *string         `json:"error_message,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// HandleAsyncChat creates a background job from the incoming request body and
// returns 202 Accepted with the job ID. The actual request processing happens
// asynchronously in the worker.
func (h *Handler) HandleAsyncChat(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil || r.ContentLength == 0 {
		writeJSONError(w, http.StatusBadRequest, "empty request body")
		return
	}

	var req asyncChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Use the raw body as-is, or fall back to the compatibility alias.
	body := req.Body
	if len(body) == 0 && len(req.MaxBody) > 0 {
		body = req.MaxBody
	}
	if len(body) == 0 {
		writeJSONError(w, http.StatusBadRequest, "missing 'body' field in request")
		return
	}

	model := req.Model
	if model == "" {
		model = h.config.DefaultModel
	}

	// Build the job payload — a lightweight envelope carrying the raw HTTP body
	// and model hint. The worker's buildRequest method will reconstruct the
	// engine.Request from this.
	payload := map[string]interface{}{
		"body":   body,
		"model":  model,
		"stream": req.Stream,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to marshal job payload")
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	job := &jobs.Job{
		ID:      uuid.New(),
		Type:    JobTypeChatCompletion,
		Status:  jobs.JobPending,
		Payload: payloadJSON,
	}

	// Persist the job within a transaction.
	scope, err := h.txManager.Begin(r.Context())
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to begin transaction for job creation")
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() {
		_ = scope.Rollback(r.Context())
	}()

	if err := scope.Jobs().Create(r.Context(), job); err != nil {
		h.logger.Error().Err(err).Str("job_id", job.ID.String()).Msg("failed to create job")
		writeJSONError(w, http.StatusInternalServerError, "failed to create job")
		return
	}
	if err := scope.Commit(r.Context()); err != nil {
		h.logger.Error().Err(err).Str("job_id", job.ID.String()).Msg("failed to commit job creation")
		writeJSONError(w, http.StatusInternalServerError, "failed to persist job")
		return
	}

	// Enqueue the job for immediate processing. A full queue is not fatal —
	// the poll loop will pick it up on the next cycle.
	if err := h.worker.Enqueue(r.Context(), job.ID); err != nil && err != ErrSignalQueueFull {
		h.logger.Warn().Err(err).Str("job_id", job.ID.String()).Msg("failed to enqueue job")
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(asyncResponse{
		ID:      job.ID,
		Status:  string(jobs.JobPending),
		Message: "job enqueued",
	})
}

// HandleJobStatus returns the current status and result (if completed) for a
// job identified by its UUID.
func (h *Handler) HandleJobStatus(w http.ResponseWriter, r *http.Request) {
	jobIDStr := chi.URLParam(r, "id")
	jobID, err := uuid.Parse(jobIDStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid job ID: "+jobIDStr)
		return
	}

	job, err := h.jobRepo.FindByID(r.Context(), jobID)
	if err != nil {
		h.logger.Error().Err(err).Str("job_id", jobID.String()).Msg("failed to lookup job")
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if job == nil {
		writeJSONError(w, http.StatusNotFound, "job not found")
		return
	}

	resp := jobStatusResponse{
		ID:           job.ID,
		Type:         job.Type,
		Status:       job.Status,
		Result:       job.Result,
		ErrorMessage: job.ErrorMessage,
		CreatedAt:    job.CreatedAt,
		UpdatedAt:    job.UpdatedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}
