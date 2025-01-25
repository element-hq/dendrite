package deltas

import (
	"context"
	"database/sql"
	"fmt"
)

func UpDropPrimaryKeyConstraint(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
ALTER TABLE userapi_devices DROP CONSTRAINT userapi_devices_pkey;`)
	if err != nil {
		return fmt.Errorf("failed to execute upgrade: %w", err)
	}
	return nil
}

func DownDropPrimaryKeyConstraint(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
	ALTER TABLE userapi_devices ADD CONSTRAINT userapi_devices_pkey PRIMARY KEY (access_token);`)
	if err != nil {
		return fmt.Errorf("failed to execute downgrade: %w", err)
	}
	return nil
}
