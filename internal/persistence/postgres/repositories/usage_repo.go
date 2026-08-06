package repositories

import (
	"context"
	"errors"
	"time"

	"gorouter/internal/domain/usage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Compile-time interface assertion.
var _ usage.UsageRepository = (*usageRepo)(nil)

// NewUsageRepo creates a usage repository bound to the given transaction.
func NewUsageRepo(tx pgx.Tx) usage.UsageRepository {
	return &usageRepo{tx: tx}
}

type usageRepo struct {
	tx pgx.Tx
}

func (r *usageRepo) AggregateDaily(ctx context.Context, agg *usage.DailyAggregate) error {
	_, err := r.tx.Exec(ctx,
		`INSERT INTO gorouter_usage_daily (day, provider_id, model_id, requests, prompt_tokens, completion_tokens, cost)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (day) DO UPDATE SET
		   provider_id       = EXCLUDED.provider_id,
		   model_id          = EXCLUDED.model_id,
		   requests          = EXCLUDED.requests,
		   prompt_tokens     = EXCLUDED.prompt_tokens,
		   completion_tokens = EXCLUDED.completion_tokens,
		   cost              = EXCLUDED.cost`,
		agg.Day, agg.ProviderID, agg.ModelID, agg.Requests,
		agg.PromptTokens, agg.CompletionTokens, agg.Cost)
	return err
}

func (r *usageRepo) Daily(ctx context.Context, day time.Time) ([]usage.DailyAggregate, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT day, provider_id, model_id, requests, prompt_tokens, completion_tokens, cost
		 FROM gorouter_usage_daily WHERE day = $1
		 ORDER BY provider_id NULLS LAST, model_id`, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []usage.DailyAggregate
	for rows.Next() {
		var a usage.DailyAggregate
		if err := rows.Scan(&a.Day, &a.ProviderID, &a.ModelID, &a.Requests,
			&a.PromptTokens, &a.CompletionTokens, &a.Cost); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *usageRepo) WriteRequestDetail(ctx context.Context, detail *usage.RequestDetail) error {
	_, err := r.tx.Exec(ctx,
		`INSERT INTO gorouter_request_details
		 (id, request_id, provider_id, model, prompt_tokens, completion_tokens, cost, status, error_kind, occurred_at, debug_opt_in)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		detail.ID, detail.RequestID, detail.ProviderID, detail.Model,
		detail.PromptTokens, detail.CompletionTokens, detail.Cost,
		detail.Status, detail.ErrorKind, detail.OccurredAt, detail.DebugOptIn)
	return err
}

func (r *usageRepo) RequestDetail(ctx context.Context, id uuid.UUID) (*usage.RequestDetail, error) {
	row := r.tx.QueryRow(ctx,
		`SELECT id, request_id, provider_id, model, prompt_tokens, completion_tokens, cost, status, error_kind, occurred_at, debug_opt_in
		 FROM gorouter_request_details WHERE id = $1`, id)
	var d usage.RequestDetail
	err := row.Scan(&d.ID, &d.RequestID, &d.ProviderID, &d.Model,
		&d.PromptTokens, &d.CompletionTokens, &d.Cost,
		&d.Status, &d.ErrorKind, &d.OccurredAt, &d.DebugOptIn)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func (r *usageRepo) AppendHistory(ctx context.Context, entry *usage.RequestHistoryEntry) error {
	_, err := r.tx.Exec(ctx,
		`INSERT INTO gorouter_request_history (id, model, provider, prompt_tokens, completion_tokens, status, occurred_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entry.ID, entry.Model, entry.Provider,
		entry.PromptTokens, entry.CompletionTokens, entry.Status, entry.OccurredAt)
	return err
}

func (r *usageRepo) RecentHistory(ctx context.Context, limit int) ([]usage.RequestHistoryEntry, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT id, model, provider, prompt_tokens, completion_tokens, status, occurred_at
		 FROM gorouter_request_history
		 ORDER BY occurred_at DESC NULLS LAST, id DESC
		 LIMIT LEAST($1::int, 50)`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []usage.RequestHistoryEntry
	for rows.Next() {
		var e usage.RequestHistoryEntry
		if err := rows.Scan(&e.ID, &e.Model, &e.Provider,
			&e.PromptTokens, &e.CompletionTokens, &e.Status, &e.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *usageRepo) DetailsBetween(ctx context.Context, since time.Time) ([]usage.RequestDetail, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT id, request_id, provider_id, model, prompt_tokens, completion_tokens, cost, status, error_kind, occurred_at, debug_opt_in
		 FROM gorouter_request_details WHERE occurred_at >= $1
		 ORDER BY occurred_at ASC NULLS LAST, id ASC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []usage.RequestDetail
	for rows.Next() {
		var d usage.RequestDetail
		if err := rows.Scan(&d.ID, &d.RequestID, &d.ProviderID, &d.Model,
			&d.PromptTokens, &d.CompletionTokens, &d.Cost,
			&d.Status, &d.ErrorKind, &d.OccurredAt, &d.DebugOptIn); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *usageRepo) PurgeBefore(ctx context.Context, cutoff time.Time) (usage.PurgeStats, error) {
	var stats usage.PurgeStats
	tag, err := r.tx.Exec(ctx,
		`DELETE FROM gorouter_request_details WHERE occurred_at < $1`, cutoff)
	if err != nil {
		return stats, err
	}
	stats.DetailsPurged = tag.RowsAffected()
	tag, err = r.tx.Exec(ctx,
		`DELETE FROM gorouter_request_history WHERE occurred_at < $1`, cutoff)
	if err != nil {
		return stats, err
	}
	stats.HistoryPurged = tag.RowsAffected()
	tag, err = r.tx.Exec(ctx,
		`DELETE FROM gorouter_usage_daily WHERE day < $1::date`, cutoff)
	if err != nil {
		return stats, err
	}
	stats.DailyPurged = tag.RowsAffected()
	return stats, nil
}
