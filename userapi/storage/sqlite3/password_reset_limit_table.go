package sqlite3

import (
	"context"
	"database/sql"
	"time"

	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/userapi/storage/tables"
)

const passwordResetLimitsSchema = `
CREATE TABLE IF NOT EXISTS userapi_password_reset_limits (
	limit_key TEXT PRIMARY KEY,
	counter INTEGER NOT NULL,
	window_start BIGINT NOT NULL
);
`

const selectPasswordResetLimitSQL = `
SELECT counter, window_start FROM userapi_password_reset_limits WHERE limit_key = $1
`

const selectPasswordResetLimitForUpdateSQL = `
SELECT counter, window_start FROM userapi_password_reset_limits WHERE limit_key = $1
`

const upsertPasswordResetLimitSQL = `
INSERT INTO userapi_password_reset_limits (limit_key, counter, window_start)
VALUES ($1, $2, $3)
ON CONFLICT(limit_key) DO UPDATE SET counter = $2, window_start = $3
`

const deletePasswordResetLimitOlderThanSQL = `
DELETE FROM userapi_password_reset_limits WHERE window_start < $1
`

type passwordResetLimitStatements struct {
	selectStmt          *sql.Stmt
	selectForUpdateStmt *sql.Stmt
	upsertStmt          *sql.Stmt
	deleteStmt          *sql.Stmt
}

func NewSQLitePasswordResetLimitTable(db *sql.DB) (tables.PasswordResetRateLimitTable, error) {
	if _, err := db.Exec(passwordResetLimitsSchema); err != nil {
		return nil, err
	}
	stmts := &passwordResetLimitStatements{}
	return stmts, sqlutil.StatementList{
		{&stmts.selectStmt, selectPasswordResetLimitSQL},
		{&stmts.selectForUpdateStmt, selectPasswordResetLimitForUpdateSQL},
		{&stmts.upsertStmt, upsertPasswordResetLimitSQL},
		{&stmts.deleteStmt, deletePasswordResetLimitOlderThanSQL},
	}.Prepare(db)
}

func (s *passwordResetLimitStatements) SelectPasswordResetLimit(ctx context.Context, txn *sql.Tx, key string) (int, time.Time, error) {
	stmt := sqlutil.TxStmt(txn, s.selectStmt)
	var counter int
	var startMs int64
	err := stmt.QueryRowContext(ctx, key).Scan(&counter, &startMs)
	if err != nil {
		return 0, time.Time{}, err
	}
	return counter, time.UnixMilli(startMs).UTC(), nil
}

func (s *passwordResetLimitStatements) SelectPasswordResetLimitForUpdate(ctx context.Context, txn *sql.Tx, key string) (int, time.Time, error) {
	// SQLite locks the database on write transactions, so a normal select suffices.
	return s.SelectPasswordResetLimit(ctx, txn, key)
}

func (s *passwordResetLimitStatements) UpsertPasswordResetLimit(ctx context.Context, txn *sql.Tx, key string, counter int, windowStart time.Time) error {
	stmt := sqlutil.TxStmt(txn, s.upsertStmt)
	_, err := stmt.ExecContext(ctx, key, counter, windowStart.UnixMilli())
	return err
}

func (s *passwordResetLimitStatements) DeletePasswordResetLimitBefore(ctx context.Context, txn *sql.Tx, threshold time.Time) error {
	stmt := sqlutil.TxStmt(txn, s.deleteStmt)
	_, err := stmt.ExecContext(ctx, threshold.UnixMilli())
	return err
}
