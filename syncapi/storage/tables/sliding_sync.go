// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package tables

import (
	"context"
	"database/sql"
)

// SlidingSyncConnection represents a sliding sync connection
// Each connection is uniquely identified by (user_id, device_id, conn_id)
type SlidingSyncConnection struct {
	ConnectionKey int64  // Primary key (auto-increment)
	UserID        string
	DeviceID      string
	ConnID        string
	CreatedTS     int64 // Unix timestamp in milliseconds
}

// SlidingSyncConnectionPosition represents a snapshot position in a connection's history
// Each sync response creates a new position
type SlidingSyncConnectionPosition struct {
	ConnectionPosition int64 // Primary key (auto-increment) - This is what goes in the pos token!
	ConnectionKey      int64 // FK to connections
	CreatedTS          int64 // Unix timestamp in milliseconds
}

// SlidingSyncRequiredState represents a deduplicated required_state configuration
// Stored as JSON array of [type, state_key] tuples: [["m.room.create",""],["m.room.member","$ME"]]
type SlidingSyncRequiredState struct {
	RequiredStateID int64  // Primary key (auto-increment)
	ConnectionKey   int64  // FK to connections
	RequiredState   string // JSON array of tuples
}

// SlidingSyncRoomConfig tracks what room config was used at a specific position
// This allows detecting config changes (timeline_limit increase, required_state expansion)
type SlidingSyncRoomConfig struct {
	ConnectionPosition int64 // FK to positions (composite key part 1)
	RoomID             string // Composite key part 2
	TimelineLimit      int
	RequiredStateID    int64 // FK to required_state
}

// SlidingSyncConnectionStream tracks what data has been sent for a room/stream combination
// This is the key to implementing deltas!
type SlidingSyncConnectionStream struct {
	ConnectionPosition int64  // FK to positions (composite key part 1)
	RoomID             string // Composite key part 2
	Stream             string // Composite key part 3 (e.g., "events", "state", "account_data")
	RoomStatus         string // "live" (currently in lists) or "previously" (sent before, not in current lists)
	LastToken          string // Stream token for what we've sent (for computing deltas)
}

// SlidingSyncConnectionList tracks list state for operation generation
// Stores the room IDs that were in a list at the last position
type SlidingSyncConnectionList struct {
	ConnectionKey int64  // FK to connections (composite key part 1)
	ListName      string // Composite key part 2
	RoomIDs       string // JSON array of room IDs
}

// SlidingSync table interface for managing sliding sync connection state
type SlidingSync interface {
	// ===== Connection Management =====

	// InsertConnection creates a new sliding sync connection
	// Returns the connection_key
	InsertConnection(ctx context.Context, txn *sql.Tx, userID, deviceID, connID string, createdTS int64) (connectionKey int64, err error)

	// SelectConnectionByKey retrieves a connection by its connection_key
	SelectConnectionByKey(ctx context.Context, txn *sql.Tx, connectionKey int64) (*SlidingSyncConnection, error)

	// SelectConnectionByIDs retrieves a connection by (user_id, device_id, conn_id)
	SelectConnectionByIDs(ctx context.Context, txn *sql.Tx, userID, deviceID, connID string) (*SlidingSyncConnection, error)

	// DeleteConnection removes a connection and all associated data (CASCADE)
	DeleteConnection(ctx context.Context, txn *sql.Tx, connectionKey int64) error

	// DeleteOldConnections removes connections older than the given timestamp
	DeleteOldConnections(ctx context.Context, txn *sql.Tx, olderThanTS int64) error

	// ===== Position Management =====

	// InsertConnectionPosition creates a new position for a connection
	// Returns the new connection_position (this goes in the pos token)
	InsertConnectionPosition(ctx context.Context, txn *sql.Tx, connectionKey int64, createdTS int64) (connectionPosition int64, err error)

	// SelectConnectionPosition retrieves a position by connection_position
	// Used to validate incoming pos tokens
	SelectConnectionPosition(ctx context.Context, txn *sql.Tx, connectionPosition int64) (*SlidingSyncConnectionPosition, error)

	// SelectLatestConnectionPosition retrieves the most recent position for a connection
	SelectLatestConnectionPosition(ctx context.Context, txn *sql.Tx, connectionKey int64) (*SlidingSyncConnectionPosition, error)

	// ===== Required State Management =====

	// InsertRequiredState stores a required_state config and returns its ID
	// The requiredState should be JSON-encoded array of [type, state_key] tuples
	InsertRequiredState(ctx context.Context, txn *sql.Tx, connectionKey int64, requiredState string) (requiredStateID int64, err error)

	// SelectRequiredState retrieves a required_state config by ID
	SelectRequiredState(ctx context.Context, txn *sql.Tx, requiredStateID int64) (string, error)

	// SelectRequiredStateByContent finds an existing required_state ID by content (for deduplication)
	SelectRequiredStateByContent(ctx context.Context, txn *sql.Tx, connectionKey int64, requiredState string) (requiredStateID int64, exists bool, err error)

	// ===== Room Config Management =====

	// UpsertRoomConfig stores the room config used at a specific position
	UpsertRoomConfig(ctx context.Context, txn *sql.Tx, connectionPosition int64, roomID string, timelineLimit int, requiredStateID int64) error

	// SelectRoomConfig retrieves the room config for a room at a specific position
	SelectRoomConfig(ctx context.Context, txn *sql.Tx, connectionPosition int64, roomID string) (*SlidingSyncRoomConfig, error)

	// SelectLatestRoomConfig retrieves the most recent room config for a room on a connection
	// Scans backwards through positions to find the last time this room was configured
	SelectLatestRoomConfig(ctx context.Context, txn *sql.Tx, connectionKey int64, roomID string) (*SlidingSyncRoomConfig, error)

	// ===== Stream Management (Delta Tracking) =====

	// UpsertConnectionStream stores stream state for a room at a position
	// This is the key to delta computation!
	UpsertConnectionStream(ctx context.Context, txn *sql.Tx, connectionPosition int64, roomID, stream, roomStatus, lastToken string) error

	// SelectConnectionStream retrieves stream state for a room at a position
	SelectConnectionStream(ctx context.Context, txn *sql.Tx, connectionPosition int64, roomID, stream string) (*SlidingSyncConnectionStream, error)

	// SelectLatestConnectionStream retrieves the most recent stream state for a room
	// Scans backwards through positions to find the last time we sent data for this stream
	SelectLatestConnectionStream(ctx context.Context, txn *sql.Tx, connectionKey int64, roomID, stream string) (*SlidingSyncConnectionStream, error)

	// SelectAllLatestConnectionStreams retrieves all stream states for a connection at the latest position
	// Returns map[roomID]map[stream]*SlidingSyncConnectionStream
	// DEPRECATED: Use SelectConnectionStreamsByPosition for incremental syncs to avoid old state bleeding in
	SelectAllLatestConnectionStreams(ctx context.Context, txn *sql.Tx, connectionKey int64) (map[string]map[string]*SlidingSyncConnectionStream, error)

	// SelectConnectionStreamsByPosition retrieves all streams for a specific position
	// This is used for incremental syncs to get the state as it was at that exact position
	// Returns map[roomID]map[stream]*SlidingSyncConnectionStream
	SelectConnectionStreamsByPosition(ctx context.Context, txn *sql.Tx, connectionPosition int64) (map[string]map[string]*SlidingSyncConnectionStream, error)

	// DeleteOtherConnectionPositions removes all positions for a connection except the specified one
	// This is called when a client uses a position token, to clean up old state (like Synapse does)
	DeleteOtherConnectionPositions(ctx context.Context, txn *sql.Tx, connectionKey int64, keepPosition int64) error

	// ===== List State Management =====

	// UpsertConnectionList stores the current state of a list (room IDs in order)
	UpsertConnectionList(ctx context.Context, txn *sql.Tx, connectionKey int64, listName string, roomIDsJSON string) error

	// SelectConnectionList retrieves the stored room IDs for a list (JSON array)
	SelectConnectionList(ctx context.Context, txn *sql.Tx, connectionKey int64, listName string) (roomIDsJSON string, exists bool, err error)
}

// SlidingSyncJoinedRoom represents cached room metadata for rooms with local members
// Based on Synapse's sliding_sync_joined_rooms table
type SlidingSyncJoinedRoom struct {
	RoomID                   string
	EventStreamOrdering      int64  // Stream position of the most recent event
	BumpStamp                *int64 // Stream position of last "bump" event (messages, etc.)
	RoomType                 string // m.room.create content.type (for spaces filtering)
	RoomName                 string // m.room.name content.name
	IsEncrypted              bool   // Whether room has m.room.encryption
	TombstoneSuccessorRoomID string // m.room.tombstone replacement_room
}

// SlidingSyncMembershipSnapshot represents per-user membership with room state snapshot
// Based on Synapse's sliding_sync_membership_snapshots table
type SlidingSyncMembershipSnapshot struct {
	RoomID                   string
	UserID                   string
	Sender                   string // Sender of membership event (to detect kicks)
	MembershipEventID        string
	Membership               string // join, invite, leave, ban, knock
	Forgotten                bool   // Whether user has forgotten this room
	EventStreamOrdering      int64  // Stream ordering of membership event
	HasKnownState            bool   // False for remote invites with no stripped state
	RoomType                 string // m.room.create content.type
	RoomName                 string // m.room.name content.name
	IsEncrypted              bool   // Whether room has m.room.encryption
	TombstoneSuccessorRoomID string // m.room.tombstone replacement_room
}

// SlidingSyncRoomMetadata table interface for managing room metadata optimization (Phase 12)
// These tables cache room state for efficient sliding sync queries
type SlidingSyncRoomMetadata interface {
	// ===== Rooms To Recalculate (Background Job Queue) =====

	// InsertRoomToRecalculate adds a room to the recalculation queue
	InsertRoomToRecalculate(ctx context.Context, txn *sql.Tx, roomID string) error

	// SelectRoomsToRecalculate retrieves up to `limit` rooms that need recalculation
	SelectRoomsToRecalculate(ctx context.Context, txn *sql.Tx, limit int) ([]string, error)

	// DeleteRoomToRecalculate removes a room from the recalculation queue
	DeleteRoomToRecalculate(ctx context.Context, txn *sql.Tx, roomID string) error

	// ===== Joined Rooms (Room Metadata Cache) =====

	// UpsertJoinedRoom inserts or updates room metadata
	UpsertJoinedRoom(ctx context.Context, txn *sql.Tx, room *SlidingSyncJoinedRoom) error

	// SelectJoinedRoom retrieves room metadata by room ID
	SelectJoinedRoom(ctx context.Context, txn *sql.Tx, roomID string) (*SlidingSyncJoinedRoom, error)

	// SelectJoinedRooms retrieves room metadata for multiple rooms
	SelectJoinedRooms(ctx context.Context, txn *sql.Tx, roomIDs []string) (map[string]*SlidingSyncJoinedRoom, error)

	// DeleteJoinedRoom removes room metadata (when no local members remain)
	DeleteJoinedRoom(ctx context.Context, txn *sql.Tx, roomID string) error

	// SelectJoinedRoomsByFilters retrieves rooms matching the given filters
	// This is the main query path for sliding sync room lists
	SelectJoinedRoomsByFilters(ctx context.Context, txn *sql.Tx,
		isEncrypted *bool, roomType *string, notRoomTypes []string, limit int) ([]SlidingSyncJoinedRoom, error)

	// ===== Membership Snapshots (Per-User State) =====

	// UpsertMembershipSnapshot inserts or updates a membership snapshot
	UpsertMembershipSnapshot(ctx context.Context, txn *sql.Tx, snapshot *SlidingSyncMembershipSnapshot) error

	// SelectMembershipSnapshot retrieves a membership snapshot for a user in a room
	SelectMembershipSnapshot(ctx context.Context, txn *sql.Tx, roomID, userID string) (*SlidingSyncMembershipSnapshot, error)

	// SelectMembershipSnapshotsForUser retrieves all membership snapshots for a user
	// Optionally filtered by membership type
	SelectMembershipSnapshotsForUser(ctx context.Context, txn *sql.Tx, userID string, memberships []string) ([]SlidingSyncMembershipSnapshot, error)

	// UpdateMembershipForgotten marks a room as forgotten for a user
	UpdateMembershipForgotten(ctx context.Context, txn *sql.Tx, roomID, userID string, forgotten bool) error

	// DeleteMembershipSnapshot removes a membership snapshot
	DeleteMembershipSnapshot(ctx context.Context, txn *sql.Tx, roomID, userID string) error
}
