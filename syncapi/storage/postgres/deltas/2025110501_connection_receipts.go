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

// UpAddConnectionReceipts adds a table to track per-connection receipt delivery state
// This prevents receipt repetition across concurrent sliding sync connections (MSC4186)
func UpAddConnectionReceipts(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
-- Track which receipts have been delivered to each sliding sync connection
-- This enables event-ID based deduplication instead of position-based tracking
CREATE TABLE IF NOT EXISTS syncapi_sliding_sync_connection_receipts (
    connection_key BIGINT NOT NULL REFERENCES syncapi_sliding_sync_connections(connection_key) ON DELETE CASCADE,
    room_id TEXT NOT NULL,
    receipt_type TEXT NOT NULL,  -- 'm.read', 'm.read.private', etc.
    user_id TEXT NOT NULL,
    last_delivered_event_id TEXT NOT NULL,
    last_delivered_ts BIGINT NOT NULL,
    PRIMARY KEY (connection_key, room_id, receipt_type, user_id)
);

CREATE INDEX IF NOT EXISTS syncapi_sliding_sync_connection_receipts_conn_idx
    ON syncapi_sliding_sync_connection_receipts(connection_key);

CREATE INDEX IF NOT EXISTS syncapi_sliding_sync_connection_receipts_room_idx
    ON syncapi_sliding_sync_connection_receipts(room_id);
	`)
	if err != nil {
		return fmt.Errorf("failed to create connection receipts table: %w", err)
	}
	return nil
}

func DownAddConnectionReceipts(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		DROP TABLE IF EXISTS syncapi_sliding_sync_connection_receipts;
	`)
	if err != nil {
		return fmt.Errorf("failed to drop connection receipts table: %w", err)
	}
	return nil
}
