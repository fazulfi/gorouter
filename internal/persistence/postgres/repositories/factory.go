// Package repositories provides concrete SQL implementations of domain repository
// interfaces backed by PostgreSQL via pgx.
package repositories

import (
	"context"
	"encoding/json"
	"errors"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/jobs"
	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// NewTxScope constructs a *tx.TxScope wired with PostgreSQL-backed repository
// implementations all sharing the same pgx.Tx.
func NewTxScope(pgTx pgx.Tx) *tx.TxScope {
	return tx.NewTxScope(
		pgTx,
		&userRepo{tx: pgTx},
		&sessionRepo{tx: pgTx},
		&apiKeyRepo{tx: pgTx},
		&patRepo{tx: pgTx},
		&providerRepo{tx: pgTx},
		&jobRepo{tx: pgTx},
		&auditLogRepo{tx: pgTx},
		&accountRepo{tx: pgTx},
		&proxyRepo{tx: pgTx},
		NewModelRepository(pgTx),
		NewAliasRepo(pgTx),
		NewComboRepo(pgTx),
		NewOAuthRepo(pgTx),
		&proxyPoolRepo{tx: pgTx},
		&nodeStore{tx: pgTx},
		NewUsageRepo(pgTx),
		NewConsoleLogRepo(pgTx),
		NewPasswordResetRepo(pgTx),
		NewBackupRepo(pgTx),
		NewPricingRepo(pgTx),
		NewSettingsRepo(pgTx),
		NewAuditQueryRepo(pgTx),
	)
}

// ---------------------------------------------------------------------------
// providerRepo
// ---------------------------------------------------------------------------

type providerRepo struct {
	tx pgx.Tx
}

func scanProvider(row pgx.Row) (*provider.Provider, error) {
	p := &provider.Provider{}
	var cfgBytes []byte
	err := row.Scan(&p.ID, &p.Name, &p.Type, &p.BaseURL, &p.APIKeyValue,
		&cfgBytes, &p.IsEnabled, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	p.Config = json.RawMessage(cfgBytes)
	return p, nil
}

func (r *providerRepo) FindByID(ctx context.Context, id uuid.UUID) (*provider.Provider, error) {
	row := r.tx.QueryRow(ctx,
		`SELECT id, name, type, base_url, api_key_value, config, is_enabled, created_at, updated_at
		 FROM gorouter_providers WHERE id = $1`, id)
	return scanProvider(row)
}

func (r *providerRepo) FindByType(ctx context.Context, ptype provider.ProviderType) ([]provider.Provider, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT id, name, type, base_url, api_key_value, config, is_enabled, created_at, updated_at
		 FROM gorouter_providers WHERE type = $1 ORDER BY name`, string(ptype))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []provider.Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		if p != nil {
			out = append(out, *p)
		}
	}
	return out, rows.Err()
}

func (r *providerRepo) List(ctx context.Context) ([]provider.Provider, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT id, name, type, base_url, api_key_value, config, is_enabled, created_at, updated_at
		 FROM gorouter_providers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []provider.Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		if p != nil {
			out = append(out, *p)
		}
	}
	return out, rows.Err()
}

func (r *providerRepo) Create(ctx context.Context, p *provider.Provider) error {
	cfgBytes := []byte(p.Config)
	if p.Config == nil {
		cfgBytes = []byte("{}")
	}
	_, err := r.tx.Exec(ctx,
		`INSERT INTO gorouter_providers (id, name, type, base_url, api_key_value, config, is_enabled, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		p.ID, p.Name, string(p.Type), p.BaseURL, p.APIKeyValue,
		cfgBytes, p.IsEnabled, p.CreatedAt, p.UpdatedAt)
	return err
}

func (r *providerRepo) Update(ctx context.Context, p *provider.Provider) error {
	cfgBytes := []byte(p.Config)
	if p.Config == nil {
		cfgBytes = []byte("{}")
	}
	_, err := r.tx.Exec(ctx,
		`UPDATE gorouter_providers SET name=$1, type=$2, base_url=$3, api_key_value=$4, config=$5,
		 is_enabled=$6, updated_at=$7 WHERE id=$8`,
		p.Name, string(p.Type), p.BaseURL, p.APIKeyValue,
		cfgBytes, p.IsEnabled, p.UpdatedAt, p.ID)
	return err
}

func (r *providerRepo) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.tx.Exec(ctx, `DELETE FROM gorouter_providers WHERE id = $1`, id)
	return err
}

// ---------------------------------------------------------------------------
// jobRepo
// ---------------------------------------------------------------------------

type jobRepo struct {
	tx pgx.Tx
}

func scanJob(row pgx.Row) (*jobs.Job, error) {
	j := &jobs.Job{}
	var payloadBytes, resultBytes []byte
	err := row.Scan(&j.ID, &j.Type, &j.Status, &payloadBytes, &resultBytes,
		&j.ErrorMessage, &j.Attempts, &j.MaxAttempts, &j.ScheduledAt,
		&j.StartedAt, &j.CompletedAt, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	j.Payload = json.RawMessage(payloadBytes)
	j.Result = json.RawMessage(resultBytes)
	return j, nil
}

func (r *jobRepo) FindByID(ctx context.Context, id uuid.UUID) (*jobs.Job, error) {
	row := r.tx.QueryRow(ctx,
		`SELECT id, type, status, payload, result, error_message, attempts, max_attempts,
		 scheduled_at, started_at, completed_at, created_at, updated_at
		 FROM gorouter_jobs WHERE id = $1`, id)
	return scanJob(row)
}

func (r *jobRepo) FindPending(ctx context.Context, limit int) ([]jobs.Job, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT id, type, status, payload, result, error_message, attempts, max_attempts,
		 scheduled_at, started_at, completed_at, created_at, updated_at
		 FROM gorouter_jobs WHERE status = 'pending' AND (scheduled_at IS NULL OR scheduled_at <= NOW())
		 ORDER BY created_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []jobs.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		if j != nil {
			out = append(out, *j)
		}
	}
	return out, rows.Err()
}

func (r *jobRepo) Create(ctx context.Context, job *jobs.Job) error {
	payloadBytes := []byte(job.Payload)
	if job.Payload == nil {
		payloadBytes = []byte("{}")
	}
	_, err := r.tx.Exec(ctx,
		`INSERT INTO gorouter_jobs (id, type, status, payload, attempts, max_attempts, scheduled_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		job.ID, job.Type, string(job.Status), payloadBytes,
		job.Attempts, job.MaxAttempts, job.ScheduledAt, job.CreatedAt, job.UpdatedAt)
	return err
}

func (r *jobRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status jobs.JobStatus, result json.RawMessage, errMsg *string) error {
	resultBytes := []byte(result)
	if result == nil {
		resultBytes = []byte("{}")
	}
	nowField := "updated_at = NOW()"
	if status == jobs.JobRunning {
		nowField = "started_at = COALESCE(started_at, NOW()), updated_at = NOW()"
	} else if status == jobs.JobCompleted || status == jobs.JobFailed {
		nowField = "completed_at = NOW(), updated_at = NOW()"
	}
	_, err := r.tx.Exec(ctx,
		`UPDATE gorouter_jobs SET status=$1, result=$2, error_message=$3, `+nowField+` WHERE id=$4`,
		string(status), resultBytes, errMsg, id)
	return err
}
