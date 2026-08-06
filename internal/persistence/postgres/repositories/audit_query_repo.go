package repositories

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"gorouter/internal/app/tx"

	"github.com/jackc/pgx/v5"
)

// auditQueryRepo implements tx.AuditLogQueryRepository: the SELECT-only read
// surface of the append-only gorouter_audit_log. It never mutates rows; the
// runtime role holds INSERT and SELECT only (migration 000009).
type auditQueryRepo struct {
	tx pgx.Tx
}

// NewAuditQueryRepo creates the SELECT-only audit query repository backed by
// the given transaction.
func NewAuditQueryRepo(tx pgx.Tx) tx.AuditLogQueryRepository {
	return &auditQueryRepo{tx: tx}
}

const auditQueryColumns = `id, actor_id, action, resource_type, resource_id, details, host(ip_address) AS ip_text, occurred_at, job_id, actor_kind`

func (r *auditQueryRepo) List(ctx context.Context, filters tx.AuditFilters, page tx.AuditPage) ([]tx.AuditEntry, error) {
	offset, limit := page.Normalized()
	where, args := buildAuditWhere(filters)
	sql := `SELECT ` + auditQueryColumns + ` FROM gorouter_audit_log` + where +
		` ORDER BY occurred_at DESC, id DESC LIMIT $` + strconv.Itoa(len(args)+1) +
		` OFFSET $` + strconv.Itoa(len(args)+2)
	args = append(args, limit, offset)
	return queryAuditEntries(ctx, r.tx, sql, args...)
}

func (r *auditQueryRepo) Export(ctx context.Context, filters tx.AuditFilters) ([]tx.AuditEntry, error) {
	where, args := buildAuditWhere(filters)
	sql := `SELECT ` + auditQueryColumns + ` FROM gorouter_audit_log` + where +
		` ORDER BY occurred_at DESC, id DESC`
	return queryAuditEntries(ctx, r.tx, sql, args...)
}

// buildAuditWhere assembles the parameterized filter clause. All values are
// bound parameters; no input is interpolated into SQL.
func buildAuditWhere(f tx.AuditFilters) (string, []any) {
	var conds []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if f.ActorID != nil {
		add("actor_id = $%d", *f.ActorID)
	}
	if f.ResourceType != nil {
		add("resource_type = $%d", *f.ResourceType)
	}
	if f.ResourceID != nil {
		add("resource_id = $%d", *f.ResourceID)
	}
	if f.Action != nil {
		add("action = $%d", *f.Action)
	}
	if f.ActorKind != nil {
		add("actor_kind = $%d", *f.ActorKind)
	}
	if f.JobID != nil {
		add("job_id = $%d", *f.JobID)
	}
	if f.Since != nil {
		add("occurred_at >= $%d", *f.Since)
	}
	if f.Until != nil {
		add("occurred_at <= $%d", *f.Until)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func queryAuditEntries(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, sql string, args ...any) ([]tx.AuditEntry, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]tx.AuditEntry, 0)
	for rows.Next() {
		var e tx.AuditEntry
		var detailsBytes []byte
		var ipText *string
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.ResourceType, &e.ResourceID,
			&detailsBytes, &ipText, &e.OccurredAt, &e.JobID, &e.ActorKind); err != nil {
			return nil, err
		}
		e.Details = append([]byte(nil), detailsBytes...)
		if ipText != nil && *ipText != "" {
			e.IPAddress = net.ParseIP(*ipText)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Compile-time interface check.
var _ tx.AuditLogQueryRepository = (*auditQueryRepo)(nil)
