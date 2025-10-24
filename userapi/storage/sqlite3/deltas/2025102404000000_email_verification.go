package deltas

import (
	"context"
	"database/sql"
	"fmt"
)

const createEmailVerificationTablesSQL = `
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

CREATE TABLE IF NOT EXISTS userapi_email_verification_limits (
    limit_key TEXT PRIMARY KEY,
    counter INTEGER NOT NULL,
    window_start BIGINT NOT NULL
);
`

func UpEmailVerificationTables(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, createEmailVerificationTablesSQL); err != nil {
		return fmt.Errorf("failed to create email verification tables: %w", err)
	}
	return nil
}

func DownEmailVerificationTables(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
DROP TABLE IF EXISTS userapi_email_verification_limits;
DROP INDEX IF EXISTS userapi_email_verification_sessions_expires_idx;
DROP INDEX IF EXISTS userapi_email_verification_sessions_lookup_idx;
DROP INDEX IF EXISTS userapi_email_verification_sessions_attempt_idx;
DROP TABLE IF EXISTS userapi_email_verification_sessions;
`); err != nil {
		return fmt.Errorf("failed to drop email verification tables: %w", err)
	}
	return nil
}
