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

// UpCreateSlidingSyncRoomMetadata creates optimized tables for room metadata
// in sliding sync (MSC4186 Phase 12). These tables cache room state to avoid
// expensive queries against current_state_events during sync.
//
// Based on Synapse's sliding_sync_joined_rooms and sliding_sync_membership_snapshots tables.
func UpCreateSlidingSyncRoomMetadata(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
-- Sliding Sync Room Metadata Optimization Tables (MSC4186 Phase 12)
-- These tables cache room state for efficient sliding sync queries

-- Table for tracking rooms that need their metadata recalculated
-- Used during background migration and when stale data is detected
CREATE TABLE IF NOT EXISTS syncapi_sliding_sync_rooms_to_recalculate (
    room_id TEXT NOT NULL PRIMARY KEY
);

-- Optimized room metadata for rooms with local members (joined rooms)
-- One row per room where local server is participating
-- Kept in sync with current_state_events
CREATE TABLE IF NOT EXISTS syncapi_sliding_sync_joined_rooms (
    room_id TEXT NOT NULL PRIMARY KEY,
    -- Stream ordering of the most recent event in the room
    event_stream_ordering INTEGER NOT NULL,
    -- Stream ordering of the last "bump" event (m.room.message, m.room.encrypted, etc.)
    -- Used for client-side room sorting by recency
    bump_stamp INTEGER,
    -- m.room.create content.type - for spaces/not_spaces filtering
    room_type TEXT,
    -- m.room.name content.name - for room_name_like filtering and display
    room_name TEXT,
    -- Whether room has m.room.encryption state event - for is_encrypted filtering
    is_encrypted INTEGER DEFAULT 0 NOT NULL,
    -- m.room.tombstone content.replacement_room - for include_old_rooms functionality
    tombstone_successor_room_id TEXT
);

-- Index for sorting by stream ordering (most recent rooms)
CREATE INDEX IF NOT EXISTS syncapi_sliding_sync_joined_rooms_stream_ordering_idx
    ON syncapi_sliding_sync_joined_rooms(event_stream_ordering DESC);

-- Index for filtering by room type (spaces)
CREATE INDEX IF NOT EXISTS syncapi_sliding_sync_joined_rooms_room_type_idx
    ON syncapi_sliding_sync_joined_rooms(room_type);

-- Index for filtering by encryption status
CREATE INDEX IF NOT EXISTS syncapi_sliding_sync_joined_rooms_encrypted_idx
    ON syncapi_sliding_sync_joined_rooms(is_encrypted);

-- Per-user membership snapshot with room state at time of membership
-- Tracks the latest membership event for each (room_id, user_id) pair
-- For remote invites/knocks, uses stripped state; for joins, uses current state
CREATE TABLE IF NOT EXISTS syncapi_sliding_sync_membership_snapshots (
    room_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    -- Sender of the membership event (to distinguish kicks from leaves)
    sender TEXT NOT NULL,
    -- The membership event ID
    membership_event_id TEXT NOT NULL,
    -- Current membership state (join, invite, leave, ban, knock)
    membership TEXT NOT NULL,
    -- Whether the user has forgotten this room (0 = not forgotten, 1 = forgotten)
    forgotten INTEGER DEFAULT 0 NOT NULL,
    -- Stream ordering of the membership event
    event_stream_ordering INTEGER NOT NULL,
    -- Whether we have known state (0 = false for remote invites with no stripped state, 1 = true)
    has_known_state INTEGER DEFAULT 0 NOT NULL,
    -- Room state snapshot at time of membership:
    -- m.room.create content.type
    room_type TEXT,
    -- m.room.name content.name
    room_name TEXT,
    -- Whether room has m.room.encryption (0 = false, 1 = true)
    is_encrypted INTEGER DEFAULT 0 NOT NULL,
    -- m.room.tombstone content.replacement_room
    tombstone_successor_room_id TEXT,
    PRIMARY KEY (room_id, user_id)
);

-- Index for fetching all rooms for a user (the main sliding sync query path)
CREATE INDEX IF NOT EXISTS syncapi_sliding_sync_membership_snapshots_user_idx
    ON syncapi_sliding_sync_membership_snapshots(user_id);

-- Index for sorting by stream ordering
CREATE INDEX IF NOT EXISTS syncapi_sliding_sync_membership_snapshots_stream_ordering_idx
    ON syncapi_sliding_sync_membership_snapshots(event_stream_ordering DESC);

-- Index for filtering by membership type
CREATE INDEX IF NOT EXISTS syncapi_sliding_sync_membership_snapshots_membership_idx
    ON syncapi_sliding_sync_membership_snapshots(user_id, membership);

-- Index for efficient forgotten room filtering
CREATE INDEX IF NOT EXISTS syncapi_sliding_sync_membership_snapshots_forgotten_idx
    ON syncapi_sliding_sync_membership_snapshots(user_id, forgotten);
	`)
	if err != nil {
		return fmt.Errorf("failed to create sliding sync room metadata tables: %w", err)
	}
	return nil
}

func DownCreateSlidingSyncRoomMetadata(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		DROP TABLE IF EXISTS syncapi_sliding_sync_membership_snapshots;
		DROP TABLE IF EXISTS syncapi_sliding_sync_joined_rooms;
		DROP TABLE IF EXISTS syncapi_sliding_sync_rooms_to_recalculate;
	`)
	if err != nil {
		return fmt.Errorf("failed to drop sliding sync room metadata tables: %w", err)
	}
	return nil
}
