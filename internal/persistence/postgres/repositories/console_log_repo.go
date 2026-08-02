package repositories

import (
	"context"
	"time"

	"gorouter/internal/domain/console"

	"github.com/jackc/pgx/v5"
)

// Compile-time interface assertion.
var _ console.ConsoleLogRepository = (*consoleLogRepo)(nil)

// NewConsoleLogRepo creates a console-log repository bound to the given
// transaction.
func NewConsoleLogRepo(tx pgx.Tx) console.ConsoleLogRepository {
	return &consoleLogRepo{tx: tx}
}

type consoleLogRepo struct {
	tx pgx.Tx
}

func (r *consoleLogRepo) Append(ctx context.Context, entry *console.ConsoleLog) error {
	_, err := r.tx.Exec(ctx,
		`INSERT INTO gorouter_console_logs (seq, level, message, redacted_message, occurred_at, retention_until)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		entry.Seq, entry.Level, entry.Message, entry.RedactedMessage,
		entry.OccurredAt, entry.RetentionUntil)
	return err
}

func (r *consoleLogRepo) ListAfter(ctx context.Context, seq int64, limit int) ([]console.ConsoleLog, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT id, seq, level, message, redacted_message, occurred_at, retention_until
		 FROM gorouter_console_logs
		 WHERE seq > $1
		 ORDER BY seq ASC, id ASC
		 LIMIT LEAST($2::int, 50)`, seq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []console.ConsoleLog
	for rows.Next() {
		var e console.ConsoleLog
		if err := rows.Scan(&e.ID, &e.Seq, &e.Level, &e.Message,
			&e.RedactedMessage, &e.OccurredAt, &e.RetentionUntil); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *consoleLogRepo) MaxSeq(ctx context.Context) (int64, error) {
	var max int64
	if err := r.tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(seq), 0) FROM gorouter_console_logs`).Scan(&max); err != nil {
		return 0, err
	}
	return max, nil
}

func (r *consoleLogRepo) DeleteBefore(ctx context.Context, seq int64) (int64, error) {
	tag, err := r.tx.Exec(ctx,
		`DELETE FROM gorouter_console_logs WHERE seq < $1`, seq)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *consoleLogRepo) PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.tx.Exec(ctx,
		`DELETE FROM gorouter_console_logs WHERE retention_until < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
