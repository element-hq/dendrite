// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package deltas

import (
	"context"
	"database/sql"
	"fmt"
)

func UpPartialStateDeviceListStreamID(ctx context.Context, tx *sql.Tx) error {
	// SQLite doesn't support IF NOT EXISTS for ADD COLUMN, so we need to check first
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('roomserver_partial_state_rooms') WHERE name = 'device_lists_stream_id'`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check column existence: %w", err)
	}
	if count == 0 {
		_, err = tx.ExecContext(ctx, `ALTER TABLE roomserver_partial_state_rooms ADD COLUMN device_lists_stream_id INTEGER NOT NULL DEFAULT 0;`)
		if err != nil {
			return fmt.Errorf("failed to execute upgrade: %w", err)
		}
	}
	return nil
}

func DownPartialStateDeviceListStreamID(ctx context.Context, tx *sql.Tx) error {
	// SQLite doesn't support DROP COLUMN in older versions, so this is a no-op
	// The column will remain but be unused
	return nil
}
