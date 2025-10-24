// Copyright 2025 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package deltas

import (
	"context"
	"database/sql"
	"fmt"
)

func UpPasswordResetAttemptIdempotency(ctx context.Context, tx *sql.Tx) error {
	stmts := []string{
		"ALTER TABLE userapi_password_reset_tokens ADD COLUMN IF NOT EXISTS session_id TEXT",
		"ALTER TABLE userapi_password_reset_tokens ADD COLUMN IF NOT EXISTS client_secret TEXT",
		"ALTER TABLE userapi_password_reset_tokens ADD COLUMN IF NOT EXISTS send_attempt INTEGER",
		"UPDATE userapi_password_reset_tokens SET session_id = token_lookup WHERE session_id IS NULL OR session_id = ''",
		"UPDATE userapi_password_reset_tokens SET client_secret = token_lookup WHERE client_secret IS NULL OR client_secret = ''",
		"UPDATE userapi_password_reset_tokens SET send_attempt = COALESCE(send_attempt, 0)",
		"ALTER TABLE userapi_password_reset_tokens ALTER COLUMN session_id SET NOT NULL",
		"ALTER TABLE userapi_password_reset_tokens ALTER COLUMN client_secret SET NOT NULL",
		"ALTER TABLE userapi_password_reset_tokens ALTER COLUMN send_attempt SET NOT NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS userapi_password_reset_tokens_attempt_idx ON userapi_password_reset_tokens(client_secret, email, send_attempt)",
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("password reset attempt idempotency migration failed executing %q: %w", stmt, err)
		}
	}
	return nil
}

func DownPasswordResetAttemptIdempotency(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, "DROP INDEX IF EXISTS userapi_password_reset_tokens_attempt_idx"); err != nil {
		return fmt.Errorf("password reset attempt idempotency down migration failed dropping index: %w", err)
	}
	stmts := []string{
		"ALTER TABLE userapi_password_reset_tokens DROP COLUMN IF EXISTS send_attempt",
		"ALTER TABLE userapi_password_reset_tokens DROP COLUMN IF EXISTS client_secret",
		"ALTER TABLE userapi_password_reset_tokens DROP COLUMN IF EXISTS session_id",
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("password reset attempt idempotency down migration failed executing %q: %w", stmt, err)
		}
	}
	return nil
}
