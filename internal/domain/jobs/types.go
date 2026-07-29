// Package jobs provides domain types for background job management.
package jobs

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// JobStatus represents the current state of a background job.
type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

// JobType defines known job type constants.
type JobType string

const (
	JobTypeSendEmail    JobType = "send_email"
	JobTypeRevokeKeys   JobType = "revoke_keys"
	JobTypeCleanupSess  JobType = "cleanup_sessions"
	JobTypeSyncProvider JobType = "sync_provider"
	JobTypeProxyRequest JobType = "proxy_request"
)

// Job represents a unit of background work.
type Job struct {
	ID           uuid.UUID
	Type         string
	Status       JobStatus
	Payload      json.RawMessage
	Result       json.RawMessage
	ErrorMessage *string
	Attempts     int
	MaxAttempts  int
	ScheduledAt  *time.Time
	StartedAt    *time.Time
	CompletedAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// JobPayloadSendEmail is the payload for send_email jobs.
type JobPayloadSendEmail struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// JobPayloadRevokeKeys is the payload for revoke_keys jobs.
type JobPayloadRevokeKeys struct {
	UserID uuid.UUID `json:"user_id"`
	Reason string    `json:"reason"`
}

// JobPayloadCleanupSessions is the payload for cleanup_sessions jobs.
type JobPayloadCleanupSessions struct {
	Before time.Time `json:"before"`
}

// JobRepository defines persistence operations for jobs.
type JobRepository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*Job, error)
	FindPending(ctx context.Context, limit int) ([]Job, error)
	Create(ctx context.Context, job *Job) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status JobStatus, result json.RawMessage, errMsg *string) error
}
