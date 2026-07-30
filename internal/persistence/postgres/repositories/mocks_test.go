package repositories

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ---------------------------------------------------------------------------
// mockRow — implements pgx.Row
// ---------------------------------------------------------------------------

type mockRow struct {
	vals []interface{}
	err  error
}

func (r *mockRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		if i >= len(r.vals) {
			break
		}
		assignReflect(dest[i], r.vals[i])
	}
	return nil
}

// ---------------------------------------------------------------------------
// mockRows — implements pgx.Rows
// ---------------------------------------------------------------------------

type mockRows struct {
	rows [][]interface{}
	idx  int
	err  error
}

func (r *mockRows) Close()                                       {}
func (r *mockRows) Err() error                                   { return r.err }
func (r *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *mockRows) Conn() *pgx.Conn                              { return nil }
func (r *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *mockRows) RawValues() [][]byte                          { return nil }
func (r *mockRows) Values() ([]interface{}, error)               { return nil, nil }

func (r *mockRows) Next() bool {
	r.idx++
	return r.idx <= len(r.rows)
}

func (r *mockRows) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	if r.idx < 1 || r.idx > len(r.rows) {
		return nil
	}
	for i := range dest {
		if i >= len(r.rows[r.idx-1]) {
			break
		}
		assignReflect(dest[i], r.rows[r.idx-1][i])
	}
	return nil
}

// ---------------------------------------------------------------------------
// mockTx — implements pgx.Tx
// ---------------------------------------------------------------------------

type mockTx struct {
	execFn     func(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
	queryRowFn func(context.Context, string, ...interface{}) pgx.Row
	queryFn    func(context.Context, string, ...interface{}) (pgx.Rows, error)
}

func (m *mockTx) Begin(ctx context.Context) (pgx.Tx, error) {
	panic("mockTx.Begin not expected in tests")
}

func (m *mockTx) BeginFunc(ctx context.Context, f func(pgx.Tx) error) error {
	panic("mockTx.BeginFunc not expected in tests")
}

func (m *mockTx) Commit(ctx context.Context) error {
	panic("mockTx.Commit not expected in tests")
}

func (m *mockTx) Rollback(ctx context.Context) error {
	panic("mockTx.Rollback not expected in tests")
}

func (m *mockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	panic("mockTx.CopyFrom not expected in tests")
}

func (m *mockTx) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return m.execFn(ctx, sql, args...)
}

func (m *mockTx) LargeObjects() pgx.LargeObjects {
	panic("mockTx.LargeObjects not expected in tests")
}

func (m *mockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	panic("mockTx.Prepare not expected in tests")
}

func (m *mockTx) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return m.queryFn(ctx, sql, args...)
}

func (m *mockTx) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return m.queryRowFn(ctx, sql, args...)
}

func (m *mockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	panic("mockTx.SendBatch not expected in tests")
}

func (m *mockTx) Conn() *pgx.Conn {
	panic("mockTx.Conn not expected in tests")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// assignReflect copies src into the value pointed to by dest using reflection.
// It handles same-type assignments and pointer-to-pointer matching. Nil src
// pointers are left untouched (the destination stays at its zero value).
func assignReflect(dest, src interface{}) {
	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Ptr || dv.IsNil() {
		return
	}
	sv := reflect.ValueOf(src)
	if !sv.IsValid() {
		return
	}
	// nil typed pointers (e.g. (*time.Time)(nil)) → leave dest unchanged
	if sv.Kind() == reflect.Ptr && sv.IsNil() {
		return
	}

	dt := dv.Elem().Type()
	st := sv.Type()

	if st.AssignableTo(dt) {
		dv.Elem().Set(sv)
	} else if st.ConvertibleTo(dt) {
		dv.Elem().Set(sv.Convert(dt))
	}
}
