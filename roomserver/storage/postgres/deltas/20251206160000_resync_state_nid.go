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
	_, err := tx.ExecContext(ctx, `ALTER TABLE roomserver_rooms ADD COLUMN IF NOT EXISTS resync_state_nid BIGINT NOT NULL DEFAULT 0;`)
	if err != nil {
		return fmt.Errorf("failed to execute upgrade: %w", err)
	}
	return nil
}

func DownResyncStateNID(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `ALTER TABLE roomserver_rooms DROP COLUMN IF EXISTS resync_state_nid;`)
	if err != nil {
		return fmt.Errorf("failed to execute downgrade: %w", err)
	}
	return nil
}
