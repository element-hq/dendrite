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
    connection_key INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    device_id TEXT NOT NULL,
    conn_id TEXT NOT NULL,
    created_ts INTEGER NOT NULL,
    UNIQUE (user_id, device_id, conn_id)
);

CREATE INDEX IF NOT EXISTS syncapi_sliding_sync_connections_user_idx
    ON syncapi_sliding_sync_connections(user_id);

-- Position snapshots - each sync response creates a new position
CREATE TABLE IF NOT EXISTS syncapi_sliding_sync_connection_positions (
    connection_position INTEGER PRIMARY KEY AUTOINCREMENT,
    connection_key INTEGER NOT NULL REFERENCES syncapi_sliding_sync_connections(connection_key) ON DELETE CASCADE,
    created_ts INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS syncapi_sliding_sync_connection_positions_conn_idx
    ON syncapi_sliding_sync_connection_positions(connection_key);

-- Required state configurations (deduplicated)
CREATE TABLE IF NOT EXISTS syncapi_sliding_sync_connection_required_state (
    required_state_id INTEGER PRIMARY KEY AUTOINCREMENT,
    connection_key INTEGER NOT NULL REFERENCES syncapi_sliding_sync_connections(connection_key) ON DELETE CASCADE,
    required_state TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS syncapi_sliding_sync_connection_required_state_conn_idx
    ON syncapi_sliding_sync_connection_required_state(connection_key);

-- Room config at each position
CREATE TABLE IF NOT EXISTS syncapi_sliding_sync_connection_room_configs (
    connection_position INTEGER NOT NULL REFERENCES syncapi_sliding_sync_connection_positions(connection_position) ON DELETE CASCADE,
    room_id TEXT NOT NULL,
    timeline_limit INTEGER NOT NULL,
    required_state_id INTEGER NOT NULL REFERENCES syncapi_sliding_sync_connection_required_state(required_state_id) ON DELETE CASCADE,
    PRIMARY KEY (connection_position, room_id)
);

-- Stream state tracking for delta computation
CREATE TABLE IF NOT EXISTS syncapi_sliding_sync_connection_streams (
    connection_position INTEGER NOT NULL REFERENCES syncapi_sliding_sync_connection_positions(connection_position) ON DELETE CASCADE,
    room_id TEXT NOT NULL,
    stream TEXT NOT NULL,
    room_status TEXT NOT NULL,
    last_token TEXT NOT NULL,
    PRIMARY KEY (connection_position, room_id, stream)
);

-- List state (room ordering per list)
CREATE TABLE IF NOT EXISTS syncapi_sliding_sync_connection_lists (
    connection_key INTEGER NOT NULL REFERENCES syncapi_sliding_sync_connections(connection_key) ON DELETE CASCADE,
    list_name TEXT NOT NULL,
    room_ids TEXT NOT NULL,
    PRIMARY KEY (connection_key, list_name)
);
	`)
	if err != nil {
		return fmt.Errorf("failed to create sliding sync tables: %w", err)
	}

	// SQLite doesn't support CREATE OR REPLACE VIEW, so we need to drop first
	_, err = tx.ExecContext(ctx, `DROP VIEW IF EXISTS syncapi_sliding_sync_latest_room_state`)
	if err != nil {
		return fmt.Errorf("failed to drop existing view: %w", err)
	}

	// Create the view for efficient latest room state lookup
	// Note: SQLite doesn't support DISTINCT ON, so we use a subquery approach
	_, err = tx.ExecContext(ctx, `
CREATE VIEW syncapi_sliding_sync_latest_room_state AS
SELECT
    cp.connection_key,
    cs.room_id,
    cs.stream,
    cs.room_status,
    cs.last_token,
    cs.connection_position
FROM syncapi_sliding_sync_connection_streams cs
INNER JOIN syncapi_sliding_sync_connection_positions cp USING (connection_position)
WHERE cs.connection_position = (
    SELECT MAX(cs2.connection_position)
    FROM syncapi_sliding_sync_connection_streams cs2
    INNER JOIN syncapi_sliding_sync_connection_positions cp2 USING (connection_position)
    WHERE cp2.connection_key = cp.connection_key
      AND cs2.room_id = cs.room_id
      AND cs2.stream = cs.stream
)
	`)
	if err != nil {
		return fmt.Errorf("failed to create view: %w", err)
	}

	return nil
}

func DownCreateSlidingSyncTables(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		DROP VIEW IF EXISTS syncapi_sliding_sync_latest_room_state;
		DROP TABLE IF EXISTS syncapi_sliding_sync_connection_lists;
		DROP TABLE IF EXISTS syncapi_sliding_sync_connection_streams;
		DROP TABLE IF EXISTS syncapi_sliding_sync_connection_room_configs;
		DROP TABLE IF EXISTS syncapi_sliding_sync_connection_required_state;
		DROP TABLE IF EXISTS syncapi_sliding_sync_connection_positions;
		DROP TABLE IF EXISTS syncapi_sliding_sync_connections;
	`)
	if err != nil {
		return fmt.Errorf("failed to drop sliding sync tables: %w", err)
	}
	return nil
}
