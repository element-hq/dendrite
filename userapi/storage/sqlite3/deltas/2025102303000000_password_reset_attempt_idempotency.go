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

func columnExists(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s)", table)
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			typeName   string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultVal, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func UpPasswordResetAttemptIdempotency(ctx context.Context, tx *sql.Tx) error {
	columns := []struct {
		name  string
		query string
	}{
		{"session_id", "ALTER TABLE userapi_password_reset_tokens ADD COLUMN session_id TEXT NOT NULL DEFAULT ''"},
		{"client_secret", "ALTER TABLE userapi_password_reset_tokens ADD COLUMN client_secret TEXT NOT NULL DEFAULT ''"},
		{"send_attempt", "ALTER TABLE userapi_password_reset_tokens ADD COLUMN send_attempt INTEGER NOT NULL DEFAULT 0"},
	}

	for _, col := range columns {
		exists, err := columnExists(ctx, tx, "userapi_password_reset_tokens", col.name)
		if err != nil {
			return fmt.Errorf("password reset attempt idempotency migration failed checking column %s: %w", col.name, err)
		}
		if !exists {
			if _, err := tx.ExecContext(ctx, col.query); err != nil {
				return fmt.Errorf("password reset attempt idempotency migration failed executing %q: %w", col.query, err)
			}
		}
	}

	stmts := []string{
		"UPDATE userapi_password_reset_tokens SET session_id = CASE WHEN session_id = '' THEN token_lookup ELSE session_id END",
		"UPDATE userapi_password_reset_tokens SET client_secret = CASE WHEN client_secret = '' THEN token_lookup ELSE client_secret END",
		"UPDATE userapi_password_reset_tokens SET send_attempt = COALESCE(send_attempt, 0)",
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
	return nil
}
