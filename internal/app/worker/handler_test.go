package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/jobs"
)

// Tests for HandleJobStatus

func TestHandleJobStatus_Success(t *testing.T) {
	repo := newMockJobRepo()
	jobID := uuid.New()
	now := time.Now().UTC()

	result := json.RawMessage(`{"choices":[{"message":{"content":"hello"}}]}`)
	_ = repo.Create(context.Background(), &jobs.Job{
		ID:        jobID,
		Type:      JobTypeChatCompletion,
		Status:    jobs.JobCompleted,
		Payload:   json.RawMessage(`{"body":"test"}`),
		Result:    result,
		CreatedAt: now,
		UpdatedAt: now,
	})

	pipeline := &mockPipeline{
		executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
			return &engine.Response{}, nil
		},
	}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(DefaultConfig(), repo, pipeline, updater)
	h := NewHandler(w, repo, nil, noopLogger())

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/v1/jobs/"+jobID.String(), nil)
	wRec := httptest.NewRecorder()
	r.ServeHTTP(wRec, req)

	if wRec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", wRec.Code)
	}

	var resp jobStatusResponse
	if err := json.Unmarshal(wRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if resp.ID != jobID {
		t.Errorf("expected job ID %s, got %s", jobID.String(), resp.ID.String())
	}
	if resp.Status != jobs.JobCompleted {
		t.Errorf("expected status %s, got %s", jobs.JobCompleted, resp.Status)
	}
	if len(resp.Result) == 0 {
		t.Error("expected non-empty result")
	}
}

func TestHandleJobStatus_NotFound(t *testing.T) {
	repo := newMockJobRepo()
	pipeline := &mockPipeline{}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(DefaultConfig(), repo, pipeline, updater)
	h := NewHandler(w, repo, nil, noopLogger())

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	jobID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/v1/jobs/"+jobID.String(), nil)
	wRec := httptest.NewRecorder()
	r.ServeHTTP(wRec, req)

	if wRec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", wRec.Code)
	}
}

func TestHandleJobStatus_InvalidID(t *testing.T) {
	repo := newMockJobRepo()
	pipeline := &mockPipeline{}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(DefaultConfig(), repo, pipeline, updater)
	h := NewHandler(w, repo, nil, noopLogger())

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/v1/jobs/not-a-uuid", nil)
	wRec := httptest.NewRecorder()
	r.ServeHTTP(wRec, req)

	if wRec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", wRec.Code)
	}
}

func TestHandleJobStatus_FailedJob(t *testing.T) {
	repo := newMockJobRepo()
	jobID := uuid.New()
	now := time.Now().UTC()

	errMsg := "something went wrong"
	_ = repo.Create(context.Background(), &jobs.Job{
		ID:           jobID,
		Type:         JobTypeChatCompletion,
		Status:       jobs.JobFailed,
		Payload:      json.RawMessage(`{"body":"test"}`),
		ErrorMessage: &errMsg,
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	pipeline := &mockPipeline{}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(DefaultConfig(), repo, pipeline, updater)
	h := NewHandler(w, repo, nil, noopLogger())

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/v1/jobs/"+jobID.String(), nil)
	wRec := httptest.NewRecorder()
	r.ServeHTTP(wRec, req)

	if wRec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", wRec.Code)
	}

	var resp jobStatusResponse
	if err := json.Unmarshal(wRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if resp.ID != jobID {
		t.Errorf("expected job ID %s, got %s", jobID.String(), resp.ID.String())
	}
	if resp.Status != jobs.JobFailed {
		t.Errorf("expected status %s, got %s", jobs.JobFailed, resp.Status)
	}
	if resp.ErrorMessage == nil {
		t.Fatal("expected non-nil ErrorMessage")
	}
	if *resp.ErrorMessage != errMsg {
		t.Errorf("expected error message %q, got %q", errMsg, *resp.ErrorMessage)
	}
}

func TestHandleJobStatus_PendingJob(t *testing.T) {
	repo := newMockJobRepo()
	jobID := uuid.New()
	now := time.Now().UTC()

	_ = repo.Create(context.Background(), &jobs.Job{
		ID:        jobID,
		Type:      JobTypeChatCompletion,
		Status:    jobs.JobPending,
		Payload:   json.RawMessage(`{"body":"test"}`),
		CreatedAt: now,
		UpdatedAt: now,
	})

	pipeline := &mockPipeline{}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(DefaultConfig(), repo, pipeline, updater)
	h := NewHandler(w, repo, nil, noopLogger())

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/v1/jobs/"+jobID.String(), nil)
	wRec := httptest.NewRecorder()
	r.ServeHTTP(wRec, req)

	if wRec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", wRec.Code)
	}

	var resp jobStatusResponse
	if err := json.Unmarshal(wRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Type != JobTypeChatCompletion {
		t.Errorf("expected type %s, got %s", JobTypeChatCompletion, resp.Type)
	}
	if resp.Status != jobs.JobPending {
		t.Errorf("expected status %s, got %s", jobs.JobPending, resp.Status)
	}
}

// Tests for HandleAsyncChat early return paths (no tx dependency)

func TestHandleAsyncChat_EmptyBody(t *testing.T) {
	repo := newMockJobRepo()
	pipeline := &mockPipeline{}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(DefaultConfig(), repo, pipeline, updater)
	h := NewHandler(w, repo, nil, noopLogger())

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions/async", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	wRec := httptest.NewRecorder()
	r.ServeHTTP(wRec, req)

	if wRec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body, got %d", wRec.Code)
	}
}

func TestHandleAsyncChat_InvalidJSON(t *testing.T) {
	repo := newMockJobRepo()
	pipeline := &mockPipeline{}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(DefaultConfig(), repo, pipeline, updater)
	h := NewHandler(w, repo, nil, noopLogger())

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	body := `{invalid json`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions/async", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	wRec := httptest.NewRecorder()
	r.ServeHTTP(wRec, req)

	if wRec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", wRec.Code)
	}
}

func TestHandleAsyncChat_MissingBodyField(t *testing.T) {
	repo := newMockJobRepo()
	pipeline := &mockPipeline{}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(DefaultConfig(), repo, pipeline, updater)
	h := NewHandler(w, repo, nil, noopLogger())

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	body := `{"model":"gpt-4"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions/async", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	wRec := httptest.NewRecorder()
	r.ServeHTTP(wRec, req)

	if wRec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing body field, got %d", wRec.Code)
	}
}

func TestHandleAsyncChat_ContentLengthZero(t *testing.T) {
	repo := newMockJobRepo()
	pipeline := &mockPipeline{}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(DefaultConfig(), repo, pipeline, updater)
	h := NewHandler(w, repo, nil, noopLogger())

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions/async", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = 0
	wRec := httptest.NewRecorder()
	r.ServeHTTP(wRec, req)

	if wRec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for zero-length content, got %d", wRec.Code)
	}
}

// Tests for handler construction and route registration

func TestHandler_NewHandler(t *testing.T) {
	repo := newMockJobRepo()
	pipeline := &mockPipeline{}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(DefaultConfig(), repo, pipeline, updater)

	h := NewHandler(w, repo, nil, noopLogger())
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}
	if h.config.DefaultModel != "gpt-4" {
		t.Errorf("expected DefaultModel=gpt-4, got %s", h.config.DefaultModel)
	}
}

func TestHandler_RegisterRoutes(t *testing.T) {
	repo := newMockJobRepo()
	pipeline := &mockPipeline{}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(DefaultConfig(), repo, pipeline, updater)
	h := NewHandler(w, repo, nil, noopLogger())

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	// Create a job so GET /v1/jobs/{id} returns 200 instead of 404.
	existingJobID := uuid.New()
	_ = repo.Create(context.Background(), &jobs.Job{
		ID:     existingJobID,
		Type:   JobTypeChatCompletion,
		Status: jobs.JobPending,
		Payload: json.RawMessage(`{"body":"test"}`),
	})

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/v1/chat/completions/async", `{}`},
		{http.MethodGet, "/v1/jobs/" + existingJobID.String(), ""},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			var req *http.Request
			if tt.body != "" {
				req = httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			} else {
				req = httptest.NewRequest(tt.method, tt.path, nil)
			}
			req.Header.Set("Content-Type", "application/json")
			wRec := httptest.NewRecorder()
			r.ServeHTTP(wRec, req)

			if wRec.Code == http.StatusNotFound {
				t.Errorf("expected route %s %s to be registered, got 404", tt.method, tt.path)
			}
		})
	}
}

func TestHandler_AsyncResponseFormat(t *testing.T) {
	resp := asyncResponse{
		ID:      uuid.New(),
		Status:  string(jobs.JobPending),
		Message: "job enqueued",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal asyncResponse: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if parsed["status"] != "pending" {
		t.Errorf("expected status pending, got %v", parsed["status"])
	}
	if parsed["message"] != "job enqueued" {
		t.Errorf("expected message 'job enqueued', got %v", parsed["message"])
	}
}

func TestHandler_WriteJSONError(t *testing.T) {
	repo := newMockJobRepo()
	pipeline := &mockPipeline{}
	updater := newMockStatusUpdater()
	w := workerWithUpdater(DefaultConfig(), repo, pipeline, updater)
	h := NewHandler(w, repo, nil, noopLogger())

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	// Trigger writeJSONError via empty body on async endpoint.
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions/async", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	wRec := httptest.NewRecorder()
	r.ServeHTTP(wRec, req)

	if wRec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", wRec.Code)
	}
	if wRec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", wRec.Header().Get("Content-Type"))
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(wRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response should be valid JSON: %v", err)
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("expected error object in response")
	}
	if errObj["type"] != "invalid_request_error" {
		t.Errorf("expected type invalid_request_error, got %v", errObj["type"])
	}
}
