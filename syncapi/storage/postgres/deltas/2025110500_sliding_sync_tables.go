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

// UpCreateSlidingSyncTables creates the tables required for sliding sync (MSC3575/MSC4186)
// This migration MUST run before 2025110501_connection_receipts which depends on these tables
func UpCreateSlidingSyncTables(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
-- Sliding Sync Connection State Tables (MSC3575/MSC4186)
-- These tables track per-connection state for efficient delta sync

-- Main connections table - one row per (user, device, conn_id) tuple
CREATE TABLE IF NOT EXISTS syncapi_sliding_sync_connections (
    connection_key BIGSERIAL PRIMARY KEY,
    user_id TEXT NOT NULL,
    device_id TEXT NOT NULL,
    conn_id TEXT NOT NULL,
    created_ts BIGINT NOT NULL,
    UNIQUE (user_id, device_id, conn_id)
);

CREATE INDEX IF NOT EXISTS syncapi_sliding_sync_connections_user_idx
    ON syncapi_sliding_sync_connections(user_id);

-- Position snapshots - each sync response creates a new position
CREATE TABLE IF NOT EXISTS syncapi_sliding_sync_connection_positions (
    connection_position BIGSERIAL PRIMARY KEY,
    connection_key BIGINT NOT NULL REFERENCES syncapi_sliding_sync_connections(connection_key) ON DELETE CASCADE,
    created_ts BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS syncapi_sliding_sync_connection_positions_conn_idx
    ON syncapi_sliding_sync_connection_positions(connection_key);

-- Required state configurations (deduplicated)
CREATE TABLE IF NOT EXISTS syncapi_sliding_sync_connection_required_state (
    required_state_id BIGSERIAL PRIMARY KEY,
    connection_key BIGINT NOT NULL REFERENCES syncapi_sliding_sync_connections(connection_key) ON DELETE CASCADE,
    required_state TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS syncapi_sliding_sync_connection_required_state_conn_idx
    ON syncapi_sliding_sync_connection_required_state(connection_key);

-- Room config at each position
CREATE TABLE IF NOT EXISTS syncapi_sliding_sync_connection_room_configs (
    connection_position BIGINT NOT NULL REFERENCES syncapi_sliding_sync_connection_positions(connection_position) ON DELETE CASCADE,
    room_id TEXT NOT NULL,
    timeline_limit INT NOT NULL,
    required_state_id BIGINT NOT NULL REFERENCES syncapi_sliding_sync_connection_required_state(required_state_id) ON DELETE CASCADE,
    PRIMARY KEY (connection_position, room_id)
);

-- Stream state tracking for delta computation
CREATE TABLE IF NOT EXISTS syncapi_sliding_sync_connection_streams (
    connection_position BIGINT NOT NULL REFERENCES syncapi_sliding_sync_connection_positions(connection_position) ON DELETE CASCADE,
    room_id TEXT NOT NULL,
    stream TEXT NOT NULL,
    room_status TEXT NOT NULL,
    last_token TEXT NOT NULL,
    PRIMARY KEY (connection_position, room_id, stream)
);

-- List state (room ordering per list)
CREATE TABLE IF NOT EXISTS syncapi_sliding_sync_connection_lists (
    connection_key BIGINT NOT NULL REFERENCES syncapi_sliding_sync_connections(connection_key) ON DELETE CASCADE,
    list_name TEXT NOT NULL,
    room_ids TEXT NOT NULL,
    PRIMARY KEY (connection_key, list_name)
);

-- View for efficient latest room state lookup
CREATE OR REPLACE VIEW syncapi_sliding_sync_latest_room_state AS
SELECT DISTINCT ON (cp.connection_key, cs.room_id, cs.stream)
    cp.connection_key,
    cs.room_id,
    cs.stream,
    cs.room_status,
    cs.last_token,
    cs.connection_position
FROM syncapi_sliding_sync_connection_streams cs
INNER JOIN syncapi_sliding_sync_connection_positions cp USING (connection_position)
ORDER BY cp.connection_key, cs.room_id, cs.stream, cs.connection_position DESC;
	`)
	if err != nil {
		return fmt.Errorf("failed to create sliding sync tables: %w", err)
	}
	return nil
}

func DownCreateSlidingSyncTables(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		DROP VIEW IF EXISTS syncapi_sliding_sync_latest_room_state;
		DROP TABLE IF EXISTS syncapi_sliding_sync_connection_lists CASCADE;
		DROP TABLE IF EXISTS syncapi_sliding_sync_connection_streams CASCADE;
		DROP TABLE IF EXISTS syncapi_sliding_sync_connection_room_configs CASCADE;
		DROP TABLE IF EXISTS syncapi_sliding_sync_connection_required_state CASCADE;
		DROP TABLE IF EXISTS syncapi_sliding_sync_connection_positions CASCADE;
		DROP TABLE IF EXISTS syncapi_sliding_sync_connections CASCADE;
	`)
	if err != nil {
		return fmt.Errorf("failed to drop sliding sync tables: %w", err)
	}
	return nil
}
