package deltas

import (
	"context"
	"database/sql"
	"fmt"
)

const passwordResetLimitsSchema = `
CREATE TABLE IF NOT EXISTS userapi_password_reset_limits (
	limit_key TEXT PRIMARY KEY,
	counter INTEGER NOT NULL,
	window_start BIGINT NOT NULL
);
`

func UpPasswordResetLimits(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, passwordResetLimitsSchema); err != nil {
		return fmt.Errorf("failed to create password reset limits table: %w", err)
	}
	return nil
}

func DownPasswordResetLimits(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS userapi_password_reset_limits;`); err != nil {
		return fmt.Errorf("failed to drop password reset limits table: %w", err)
	}
	return nil
}
