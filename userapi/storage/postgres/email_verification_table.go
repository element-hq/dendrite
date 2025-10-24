// Copyright 2025 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/userapi/api"
	"github.com/element-hq/dendrite/userapi/storage/tables"
)

const emailVerificationSchema = `
CREATE TABLE IF NOT EXISTS userapi_email_verification_sessions (
    session_id TEXT PRIMARY KEY,
    client_secret_hash TEXT NOT NULL,
    email TEXT NOT NULL,
    medium TEXT NOT NULL,
    token_lookup TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    send_attempt INTEGER NOT NULL,
    next_link TEXT,
    expires_at BIGINT NOT NULL,
    validated_at BIGINT,
    consumed_at BIGINT,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS userapi_email_verification_sessions_attempt_idx
    ON userapi_email_verification_sessions(client_secret_hash, email, medium, send_attempt);

CREATE INDEX IF NOT EXISTS userapi_email_verification_sessions_lookup_idx
    ON userapi_email_verification_sessions(token_lookup);

CREATE INDEX IF NOT EXISTS userapi_email_verification_sessions_expires_idx
    ON userapi_email_verification_sessions(expires_at);
`

const insertEmailVerificationSessionSQL = `
INSERT INTO userapi_email_verification_sessions (
    session_id, client_secret_hash, email, medium, token_lookup, token_hash,
    send_attempt, next_link, expires_at, validated_at, consumed_at, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULL, NULL, $10, $10)
`

const selectEmailVerificationSessionByAttemptSQL = `
SELECT session_id, client_secret_hash, email, medium, token_lookup, token_hash,
       send_attempt, next_link, expires_at, validated_at, consumed_at, created_at, updated_at
FROM userapi_email_verification_sessions
WHERE client_secret_hash = $1 AND email = $2 AND medium = $3 AND send_attempt = $4
`

const selectEmailVerificationSessionByIDSQL = `
SELECT session_id, client_secret_hash, email, medium, token_lookup, token_hash,
       send_attempt, next_link, expires_at, validated_at, consumed_at, created_at, updated_at
FROM userapi_email_verification_sessions WHERE session_id = $1
`

const updateEmailVerificationValidatedSQL = `
UPDATE userapi_email_verification_sessions
SET validated_at = $2, updated_at = $2
WHERE session_id = $1
`

const updateEmailVerificationConsumedSQL = `
UPDATE userapi_email_verification_sessions
SET consumed_at = $2, updated_at = $2
WHERE session_id = $1
`

const deleteExpiredEmailVerificationSessionsSQL = `
DELETE FROM userapi_email_verification_sessions
WHERE expires_at <= $1
`

const deleteEmailVerificationSessionSQL = `
DELETE FROM userapi_email_verification_sessions WHERE session_id = $1
`

type emailVerificationStatements struct {
	insertStmt      *sql.Stmt
	selectAttempt   *sql.Stmt
	selectByID      *sql.Stmt
	updateValidated *sql.Stmt
	updateConsumed  *sql.Stmt
	deleteExpired   *sql.Stmt
	deleteByID      *sql.Stmt
}

func NewPostgresEmailVerificationTable(db *sql.DB) (tables.EmailVerificationTokensTable, error) {
	if _, err := db.Exec(emailVerificationSchema); err != nil {
		return nil, fmt.Errorf("failed to ensure email verification schema: %w", err)
	}
	stmts := &emailVerificationStatements{}
	return stmts, sqlutil.StatementList{
		{&stmts.insertStmt, insertEmailVerificationSessionSQL},
		{&stmts.selectAttempt, selectEmailVerificationSessionByAttemptSQL},
		{&stmts.selectByID, selectEmailVerificationSessionByIDSQL},
		{&stmts.updateValidated, updateEmailVerificationValidatedSQL},
		{&stmts.updateConsumed, updateEmailVerificationConsumedSQL},
		{&stmts.deleteExpired, deleteExpiredEmailVerificationSessionsSQL},
		{&stmts.deleteByID, deleteEmailVerificationSessionSQL},
	}.Prepare(db)
}

func (s *emailVerificationStatements) InsertEmailVerificationSession(ctx context.Context, txn *sql.Tx, session *api.EmailVerificationSession) error {
	now := time.Now().UTC().UnixMilli()
	stmt := sqlutil.TxStmt(txn, s.insertStmt)
	_, err := stmt.ExecContext(
		ctx,
		session.SessionID,
		session.ClientSecretHash,
		session.Email,
		session.Medium,
		session.TokenLookup,
		session.TokenHash,
		session.SendAttempt,
		nullIfEmpty(session.NextLink),
		session.ExpiresAt.UTC().UnixMilli(),
		now,
	)
	if err == nil {
		session.CreatedAt = time.UnixMilli(now).UTC()
		session.UpdatedAt = session.CreatedAt
	}
	return err
}

func (s *emailVerificationStatements) SelectEmailVerificationSessionByAttempt(ctx context.Context, txn *sql.Tx, clientSecretHash, email, medium string, sendAttempt int) (*api.EmailVerificationSession, error) {
	stmt := sqlutil.TxStmt(txn, s.selectAttempt)
	return scanEmailVerificationSession(stmt.QueryRowContext(ctx, clientSecretHash, email, medium, sendAttempt))
}

func (s *emailVerificationStatements) SelectEmailVerificationSessionByID(ctx context.Context, txn *sql.Tx, sessionID string) (*api.EmailVerificationSession, error) {
	stmt := sqlutil.TxStmt(txn, s.selectByID)
	return scanEmailVerificationSession(stmt.QueryRowContext(ctx, sessionID))
}

func (s *emailVerificationStatements) UpdateEmailVerificationValidated(ctx context.Context, txn *sql.Tx, sessionID string, validatedAt time.Time) error {
	stmt := sqlutil.TxStmt(txn, s.updateValidated)
	_, err := stmt.ExecContext(ctx, sessionID, validatedAt.UTC().UnixMilli())
	return err
}

func (s *emailVerificationStatements) UpdateEmailVerificationConsumed(ctx context.Context, txn *sql.Tx, sessionID string, consumedAt time.Time) error {
	stmt := sqlutil.TxStmt(txn, s.updateConsumed)
	_, err := stmt.ExecContext(ctx, sessionID, consumedAt.UTC().UnixMilli())
	return err
}

func (s *emailVerificationStatements) DeleteExpiredEmailVerificationSessions(ctx context.Context, txn *sql.Tx, now time.Time) error {
	stmt := sqlutil.TxStmt(txn, s.deleteExpired)
	_, err := stmt.ExecContext(ctx, now.UTC().UnixMilli())
	return err
}

func (s *emailVerificationStatements) DeleteEmailVerificationSession(ctx context.Context, txn *sql.Tx, sessionID string) error {
	stmt := sqlutil.TxStmt(txn, s.deleteByID)
	_, err := stmt.ExecContext(ctx, sessionID)
	return err
}

func scanEmailVerificationSession(row *sql.Row) (*api.EmailVerificationSession, error) {
	var (
		sessionID        string
		clientSecretHash string
		email            string
		medium           string
		tokenLookup      string
		tokenHash        string
		sendAttempt      int
		nextLink         sql.NullString
		expiresAt        int64
		validatedAt      sql.NullInt64
		consumedAt       sql.NullInt64
		createdAt        int64
		updatedAt        int64
	)

	if err := row.Scan(
		&sessionID,
		&clientSecretHash,
		&email,
		&medium,
		&tokenLookup,
		&tokenHash,
		&sendAttempt,
		&nextLink,
		&expiresAt,
		&validatedAt,
		&consumedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}

	session := &api.EmailVerificationSession{
		SessionID:        sessionID,
		ClientSecretHash: clientSecretHash,
		Email:            email,
		Medium:           medium,
		TokenLookup:      tokenLookup,
		TokenHash:        tokenHash,
		SendAttempt:      sendAttempt,
		NextLink:         nextLink.String,
		ExpiresAt:        time.UnixMilli(expiresAt).UTC(),
		CreatedAt:        time.UnixMilli(createdAt).UTC(),
		UpdatedAt:        time.UnixMilli(updatedAt).UTC(),
	}

	if validatedAt.Valid {
		ts := time.UnixMilli(validatedAt.Int64).UTC()
		session.ValidatedAt = &ts
	}
	if consumedAt.Valid {
		ts := time.UnixMilli(consumedAt.Int64).UTC()
		session.ConsumedAt = &ts
	}

	return session, nil
}

func nullIfEmpty(value string) interface{} {
	if value == "" {
		return sql.NullString{}
	}
	return value
}

var _ tables.EmailVerificationTokensTable = (*emailVerificationStatements)(nil)
