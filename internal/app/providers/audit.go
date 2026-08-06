package providers

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"

	"github.com/google/uuid"
)

var errActorRequired = errors.New("providers: actor is required for mutations")

func requireActor(actor *auth.Actor) error {
	if actor == nil {
		return errActorRequired
	}
	return nil
}

// auditEntry builds an actor-aware audit log entry. Only caller-provided
// non-credential details are included; secrets and raw credentials are never
// written to the audit trail (decision #127).
func auditEntry(actor *auth.Actor, action, resourceType string, resourceID *uuid.UUID, details map[string]any) *tx.AuditLogEntry {
	var raw json.RawMessage
	if len(details) > 0 {
		if b, err := json.Marshal(details); err == nil {
			raw = b
		}
	}
	entry := &tx.AuditLogEntry{
		ID:           uuid.New(),
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      raw,
		OccurredAt:   time.Now().UTC(),
	}
	if actor != nil {
		id := actor.UserID
		entry.ActorID = &id
	}
	return entry
}

func writeAudit(ctx context.Context, log tx.AuditLogRepository, actor *auth.Actor, action, resourceType string, resourceID *uuid.UUID, details map[string]any) error {
	return log.Create(ctx, auditEntry(actor, action, resourceType, resourceID, details))
}

// uuidOrNil parses a node ID as a UUID for audit resource targeting. Node IDs
// may be arbitrary strings (the PG store derives deterministic UUIDs), so a
// non-UUID ID yields a nil resource ID carried in details instead.
func uuidOrNil(id string) *uuid.UUID {
	u, err := uuid.Parse(id)
	if err != nil {
		return nil
	}
	return &u
}
