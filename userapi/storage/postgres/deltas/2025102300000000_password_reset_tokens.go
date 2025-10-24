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

const passwordResetTokensTableSQL = `
CREATE TABLE IF NOT EXISTS userapi_password_reset_tokens (
	token_lookup TEXT PRIMARY KEY,
	token_hash TEXT NOT NULL,
	user_id TEXT NOT NULL,
	email TEXT NOT NULL,
	expires_at BIGINT NOT NULL,
	consumed_at BIGINT,
	created_at BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM clock_timestamp()) * 1000)
);

CREATE INDEX IF NOT EXISTS userapi_password_reset_tokens_user_idx
	ON userapi_password_reset_tokens(user_id);

CREATE INDEX IF NOT EXISTS userapi_password_reset_tokens_expires_idx
	ON userapi_password_reset_tokens(expires_at);

CREATE INDEX IF NOT EXISTS userapi_password_reset_tokens_consumed_idx
	ON userapi_password_reset_tokens(consumed_at);
`

func UpPasswordResetTokens(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, passwordResetTokensTableSQL); err != nil {
		return fmt.Errorf("failed to create password reset tokens table: %w", err)
	}
	return nil
}

func DownPasswordResetTokens(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
DROP INDEX IF EXISTS userapi_password_reset_tokens_consumed_idx;
DROP INDEX IF EXISTS userapi_password_reset_tokens_expires_idx;
DROP INDEX IF EXISTS userapi_password_reset_tokens_user_idx;
DROP TABLE IF EXISTS userapi_password_reset_tokens;
`); err != nil {
		return fmt.Errorf("failed to drop password reset tokens table: %w", err)
	}
	return nil
}
