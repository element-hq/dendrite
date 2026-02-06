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

// UpResyncStateNID adds a resync_state_nid column to roomserver_rooms.
// This column records the state snapshot NID after a partial state resync completes,
// allowing us to detect and prevent state regressions from out-of-order events.
func UpResyncStateNID(ctx context.Context, tx *sql.Tx) error {
	// SQLite doesn't support IF NOT EXISTS for ADD COLUMN, so we need to check first
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('roomserver_rooms') WHERE name = 'resync_state_nid'`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check column existence: %w", err)
	}
	if count == 0 {
		_, err = tx.ExecContext(ctx, `ALTER TABLE roomserver_rooms ADD COLUMN resync_state_nid INTEGER NOT NULL DEFAULT 0;`)
		if err != nil {
			return fmt.Errorf("failed to execute upgrade: %w", err)
		}
	}
	return nil
}

func DownResyncStateNID(ctx context.Context, tx *sql.Tx) error {
	// SQLite doesn't support DROP COLUMN in older versions, so we just leave the column
	return nil
}
