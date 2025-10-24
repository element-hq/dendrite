package sqlite3

import (
	"context"
	"database/sql"
	"time"

	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/userapi/storage/tables"
)

const emailVerificationLimitsSchema = `
CREATE TABLE IF NOT EXISTS userapi_email_verification_limits (
    limit_key TEXT PRIMARY KEY,
    counter INTEGER NOT NULL,
    window_start BIGINT NOT NULL
);
`

const selectEmailVerificationLimitSQL = `
SELECT counter, window_start FROM userapi_email_verification_limits WHERE limit_key = $1
`

const upsertEmailVerificationLimitSQL = `
INSERT INTO userapi_email_verification_limits (limit_key, counter, window_start)
VALUES ($1, $2, $3)
ON CONFLICT(limit_key) DO UPDATE SET counter = $2, window_start = $3
`

const deleteEmailVerificationLimitOlderThanSQL = `
DELETE FROM userapi_email_verification_limits WHERE window_start < $1
`

type emailVerificationLimitStatements struct {
	selectStmt *sql.Stmt
	upsertStmt *sql.Stmt
	deleteStmt *sql.Stmt
}

func NewSQLiteEmailVerificationLimitTable(db *sql.DB) (tables.EmailVerificationRateLimitTable, error) {
	if _, err := db.Exec(emailVerificationLimitsSchema); err != nil {
		return nil, err
	}
	stmts := &emailVerificationLimitStatements{}
	return stmts, sqlutil.StatementList{
		{&stmts.selectStmt, selectEmailVerificationLimitSQL},
		{&stmts.upsertStmt, upsertEmailVerificationLimitSQL},
		{&stmts.deleteStmt, deleteEmailVerificationLimitOlderThanSQL},
	}.Prepare(db)
}

func (s *emailVerificationLimitStatements) SelectEmailVerificationLimit(ctx context.Context, txn *sql.Tx, key string) (int, time.Time, error) {
	stmt := sqlutil.TxStmt(txn, s.selectStmt)
	var count int
	var startMs int64
	err := stmt.QueryRowContext(ctx, key).Scan(&count, &startMs)
	if err != nil {
		return 0, time.Time{}, err
	}
	return count, time.UnixMilli(startMs).UTC(), nil
}

func (s *emailVerificationLimitStatements) SelectEmailVerificationLimitForUpdate(ctx context.Context, txn *sql.Tx, key string) (int, time.Time, error) {
	// SQLite locks on write, so a normal select suffices.
	return s.SelectEmailVerificationLimit(ctx, txn, key)
}

func (s *emailVerificationLimitStatements) UpsertEmailVerificationLimit(ctx context.Context, txn *sql.Tx, key string, counter int, windowStart time.Time) error {
	stmt := sqlutil.TxStmt(txn, s.upsertStmt)
	_, err := stmt.ExecContext(ctx, key, counter, windowStart.UTC().UnixMilli())
	return err
}

func (s *emailVerificationLimitStatements) DeleteEmailVerificationLimitBefore(ctx context.Context, txn *sql.Tx, threshold time.Time) error {
	stmt := sqlutil.TxStmt(txn, s.deleteStmt)
	_, err := stmt.ExecContext(ctx, threshold.UTC().UnixMilli())
	return err
}

var _ tables.EmailVerificationRateLimitTable = (*emailVerificationLimitStatements)(nil)
