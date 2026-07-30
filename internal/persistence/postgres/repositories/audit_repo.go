package repositories

import (
	"context"

	"gorouter/internal/app/tx"

	"github.com/jackc/pgx/v5"
)

// auditLogRepo implements tx.AuditLogRepository backed by pgx.
type auditLogRepo struct {
	tx pgx.Tx
}

func (r *auditLogRepo) Create(ctx context.Context, entry *tx.AuditLogEntry) error {
	var detailsBytes []byte
	if entry.Details != nil {
		detailsBytes = []byte(entry.Details)
	}

	var ipBytes []byte
	if entry.IPAddress != nil {
		ipBytes, _ = entry.IPAddress.MarshalText()
	}

	_, err := r.tx.Exec(ctx,
		`INSERT INTO gorouter_audit_log (id, actor_id, action, resource_type, resource_id, details, ip_address, occurred_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		entry.ID, entry.ActorID, entry.Action, entry.ResourceType,
		entry.ResourceID, detailsBytes, ipBytes, entry.OccurredAt)
	return err
}
