// Copyright 2025 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package sqlite3

import (
	"context"
	"database/sql"
	"time"

	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/userapi/storage/tables"
)

const passwordResetTokensSchema = `
CREATE TABLE IF NOT EXISTS userapi_password_reset_tokens (
	token_lookup TEXT PRIMARY KEY,
	token_hash TEXT NOT NULL,
	user_id TEXT NOT NULL,
	email TEXT NOT NULL,
	session_id TEXT NOT NULL,
	client_secret TEXT NOT NULL,
	send_attempt INTEGER NOT NULL,
	expires_at BIGINT NOT NULL,
	consumed_at BIGINT,
	created_at BIGINT NOT NULL DEFAULT (STRFTIME('%s', 'now') * 1000)
);

CREATE INDEX IF NOT EXISTS userapi_password_reset_tokens_user_idx
	ON userapi_password_reset_tokens(user_id);

CREATE UNIQUE INDEX IF NOT EXISTS userapi_password_reset_tokens_attempt_idx
	ON userapi_password_reset_tokens(client_secret, email, send_attempt);

CREATE INDEX IF NOT EXISTS userapi_password_reset_tokens_expires_idx
	ON userapi_password_reset_tokens(expires_at);

CREATE INDEX IF NOT EXISTS userapi_password_reset_tokens_consumed_idx
	ON userapi_password_reset_tokens(consumed_at);
`

const insertPasswordResetTokenSQL = `
INSERT INTO userapi_password_reset_tokens (token_lookup, token_hash, user_id, email, session_id, client_secret, send_attempt, expires_at, consumed_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL, $9)
`

const selectPasswordResetTokenSQL = `
SELECT token_hash, user_id, email, expires_at FROM userapi_password_reset_tokens
WHERE token_lookup = $1 AND consumed_at IS NULL AND expires_at > $2
`

const selectPasswordResetTokenByAttemptSQL = `
SELECT token_lookup, session_id, expires_at FROM userapi_password_reset_tokens
WHERE client_secret = $1 AND email = $2 AND send_attempt = $3 AND consumed_at IS NULL AND expires_at > $4
ORDER BY created_at DESC
LIMIT 1
`

const consumePasswordResetTokenSQL = `
UPDATE userapi_password_reset_tokens
SET consumed_at = $1
WHERE token_lookup = $2 AND token_hash = $3 AND consumed_at IS NULL
`

const deleteExpiredPasswordResetTokensSQL = `
DELETE FROM userapi_password_reset_tokens
WHERE expires_at <= $1 OR (consumed_at IS NOT NULL AND consumed_at <= $1)
`

const deletePasswordResetTokenSQL = `
DELETE FROM userapi_password_reset_tokens WHERE token_lookup = $1
`

type passwordResetStatements struct {
	insertStmt        *sql.Stmt
	selectStmt        *sql.Stmt
	selectByAttempt   *sql.Stmt
	consumeStmt       *sql.Stmt
	deleteExpiredStmt *sql.Stmt
	deleteStmt        *sql.Stmt
}

func NewSQLitePasswordResetTokensTable(db *sql.DB) (tables.PasswordResetTokensTable, error) {
	s := &passwordResetStatements{}
	if _, err := db.Exec(passwordResetTokensSchema); err != nil {
		return nil, err
	}
	return s, sqlutil.StatementList{
		{&s.insertStmt, insertPasswordResetTokenSQL},
		{&s.selectStmt, selectPasswordResetTokenSQL},
		{&s.selectByAttempt, selectPasswordResetTokenByAttemptSQL},
		{&s.consumeStmt, consumePasswordResetTokenSQL},
		{&s.deleteExpiredStmt, deleteExpiredPasswordResetTokensSQL},
		{&s.deleteStmt, deletePasswordResetTokenSQL},
	}.Prepare(db)
}

func (s *passwordResetStatements) InsertPasswordResetToken(ctx context.Context, txn *sql.Tx, tokenHash, tokenLookup, userID, email, sessionID, clientSecret string, sendAttempt int, expiresAt time.Time) error {
	stmt := sqlutil.TxStmt(txn, s.insertStmt)
	_, err := stmt.ExecContext(ctx, tokenLookup, tokenHash, userID, email, sessionID, clientSecret, sendAttempt, expiresAt.UnixMilli(), time.Now().UTC().UnixMilli())
	return err
}

func (s *passwordResetStatements) SelectPasswordResetToken(ctx context.Context, txn *sql.Tx, tokenLookup string, now time.Time) (string, string, string, time.Time, error) {
	stmt := sqlutil.TxStmt(txn, s.selectStmt)
	var tokenHash, userID, email string
	var expiresAt int64
	err := stmt.QueryRowContext(ctx, tokenLookup, now.UnixMilli()).Scan(&tokenHash, &userID, &email, &expiresAt)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	return tokenHash, userID, email, time.UnixMilli(expiresAt).UTC(), nil
}

func (s *passwordResetStatements) SelectPasswordResetTokenByAttempt(ctx context.Context, txn *sql.Tx, clientSecret, email string, sendAttempt int, now time.Time) (string, string, time.Time, error) {
	stmt := sqlutil.TxStmt(txn, s.selectByAttempt)
	var tokenLookup, sessionID string
	var expiresAt int64
	err := stmt.QueryRowContext(ctx, clientSecret, email, sendAttempt, now.UnixMilli()).Scan(&tokenLookup, &sessionID, &expiresAt)
	if err != nil {
		return "", "", time.Time{}, err
	}
	return tokenLookup, sessionID, time.UnixMilli(expiresAt).UTC(), nil
}

func (s *passwordResetStatements) MarkPasswordResetTokenConsumed(ctx context.Context, txn *sql.Tx, tokenLookup, tokenHash string, consumedAt time.Time) error {
	stmt := sqlutil.TxStmt(txn, s.consumeStmt)
	res, err := stmt.ExecContext(ctx, consumedAt.UnixMilli(), tokenLookup, tokenHash)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *passwordResetStatements) DeleteExpiredPasswordResetTokens(ctx context.Context, txn *sql.Tx, now time.Time) error {
	stmt := sqlutil.TxStmt(txn, s.deleteExpiredStmt)
	_, err := stmt.ExecContext(ctx, now.UnixMilli())
	return err
}

func (s *passwordResetStatements) DeletePasswordResetToken(ctx context.Context, txn *sql.Tx, tokenLookup string) error {
	stmt := sqlutil.TxStmt(txn, s.deleteStmt)
	_, err := stmt.ExecContext(ctx, tokenLookup)
	return err
}
