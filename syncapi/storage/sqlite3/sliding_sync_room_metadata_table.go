// Copyright 2025 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package sqlite3

import (
	"context"
	"database/sql"
	"strings"

	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/syncapi/storage/tables"
)

// SQL statements for rooms to recalculate
const insertRoomToRecalculateSQLite = `
	INSERT OR IGNORE INTO syncapi_sliding_sync_rooms_to_recalculate (room_id)
	VALUES ($1)
`

const selectRoomsToRecalculateSQLite = `
	SELECT room_id FROM syncapi_sliding_sync_rooms_to_recalculate
	LIMIT $1
`

const deleteRoomToRecalculateSQLite = `
	DELETE FROM syncapi_sliding_sync_rooms_to_recalculate
	WHERE room_id = $1
`

// SQL statements for joined rooms
const upsertJoinedRoomSQLite = `
	INSERT INTO syncapi_sliding_sync_joined_rooms
		(room_id, event_stream_ordering, bump_stamp, room_type, room_name, is_encrypted, tombstone_successor_room_id)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	ON CONFLICT (room_id)
	DO UPDATE SET
		event_stream_ordering = $2,
		bump_stamp = $3,
		room_type = $4,
		room_name = $5,
		is_encrypted = $6,
		tombstone_successor_room_id = $7
`

const selectJoinedRoomSQLite = `
	SELECT room_id, event_stream_ordering, bump_stamp, room_type, room_name, is_encrypted, tombstone_successor_room_id
	FROM syncapi_sliding_sync_joined_rooms
	WHERE room_id = $1
`

const deleteJoinedRoomSQLite = `
	DELETE FROM syncapi_sliding_sync_joined_rooms
	WHERE room_id = $1
`

// SQL statements for membership snapshots
const upsertMembershipSnapshotSQLite = `
	INSERT INTO syncapi_sliding_sync_membership_snapshots
		(room_id, user_id, sender, membership_event_id, membership, forgotten, event_stream_ordering,
		 has_known_state, room_type, room_name, is_encrypted, tombstone_successor_room_id)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	ON CONFLICT (room_id, user_id)
	DO UPDATE SET
		sender = $3,
		membership_event_id = $4,
		membership = $5,
		forgotten = $6,
		event_stream_ordering = $7,
		has_known_state = $8,
		room_type = $9,
		room_name = $10,
		is_encrypted = $11,
		tombstone_successor_room_id = $12
`

const selectMembershipSnapshotSQLite = `
	SELECT room_id, user_id, sender, membership_event_id, membership, forgotten, event_stream_ordering,
		   has_known_state, room_type, room_name, is_encrypted, tombstone_successor_room_id
	FROM syncapi_sliding_sync_membership_snapshots
	WHERE room_id = $1 AND user_id = $2
`

const selectMembershipSnapshotsForUserSQLite = `
	SELECT room_id, user_id, sender, membership_event_id, membership, forgotten, event_stream_ordering,
		   has_known_state, room_type, room_name, is_encrypted, tombstone_successor_room_id
	FROM syncapi_sliding_sync_membership_snapshots
	WHERE user_id = $1 AND forgotten = 0
`

const updateMembershipForgottenSQLite = `
	UPDATE syncapi_sliding_sync_membership_snapshots
	SET forgotten = $3
	WHERE room_id = $1 AND user_id = $2
`

const deleteMembershipSnapshotSQLite = `
	DELETE FROM syncapi_sliding_sync_membership_snapshots
	WHERE room_id = $1 AND user_id = $2
`

type slidingSyncRoomMetadataStatementsSQLite struct {
	insertRoomToRecalculateStmt      *sql.Stmt
	selectRoomsToRecalculateStmt     *sql.Stmt
	deleteRoomToRecalculateStmt      *sql.Stmt
	upsertJoinedRoomStmt             *sql.Stmt
	selectJoinedRoomStmt             *sql.Stmt
	deleteJoinedRoomStmt             *sql.Stmt
	upsertMembershipSnapshotStmt     *sql.Stmt
	selectMembershipSnapshotStmt     *sql.Stmt
	selectMembershipSnapshotsForUserStmt *sql.Stmt
	updateMembershipForgottenStmt    *sql.Stmt
	deleteMembershipSnapshotStmt     *sql.Stmt
	db                               *sql.DB
}

func NewSqliteSlidingSyncRoomMetadataTable(db *sql.DB) (tables.SlidingSyncRoomMetadata, error) {
	s := &slidingSyncRoomMetadataStatementsSQLite{db: db}
	return s, sqlutil.StatementList{
		{&s.insertRoomToRecalculateStmt, insertRoomToRecalculateSQLite},
		{&s.selectRoomsToRecalculateStmt, selectRoomsToRecalculateSQLite},
		{&s.deleteRoomToRecalculateStmt, deleteRoomToRecalculateSQLite},
		{&s.upsertJoinedRoomStmt, upsertJoinedRoomSQLite},
		{&s.selectJoinedRoomStmt, selectJoinedRoomSQLite},
		{&s.deleteJoinedRoomStmt, deleteJoinedRoomSQLite},
		{&s.upsertMembershipSnapshotStmt, upsertMembershipSnapshotSQLite},
		{&s.selectMembershipSnapshotStmt, selectMembershipSnapshotSQLite},
		{&s.selectMembershipSnapshotsForUserStmt, selectMembershipSnapshotsForUserSQLite},
		{&s.updateMembershipForgottenStmt, updateMembershipForgottenSQLite},
		{&s.deleteMembershipSnapshotStmt, deleteMembershipSnapshotSQLite},
	}.Prepare(db)
}

// ===== Rooms To Recalculate =====

func (s *slidingSyncRoomMetadataStatementsSQLite) InsertRoomToRecalculate(
	ctx context.Context, txn *sql.Tx, roomID string,
) error {
	stmt := sqlutil.TxStmt(txn, s.insertRoomToRecalculateStmt)
	_, err := stmt.ExecContext(ctx, roomID)
	return err
}

func (s *slidingSyncRoomMetadataStatementsSQLite) SelectRoomsToRecalculate(
	ctx context.Context, txn *sql.Tx, limit int,
) ([]string, error) {
	stmt := sqlutil.TxStmt(txn, s.selectRoomsToRecalculateStmt)
	rows, err := stmt.QueryContext(ctx, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roomIDs []string
	for rows.Next() {
		var roomID string
		if err := rows.Scan(&roomID); err != nil {
			return nil, err
		}
		roomIDs = append(roomIDs, roomID)
	}
	return roomIDs, rows.Err()
}

func (s *slidingSyncRoomMetadataStatementsSQLite) DeleteRoomToRecalculate(
	ctx context.Context, txn *sql.Tx, roomID string,
) error {
	stmt := sqlutil.TxStmt(txn, s.deleteRoomToRecalculateStmt)
	_, err := stmt.ExecContext(ctx, roomID)
	return err
}

// ===== Joined Rooms =====

func (s *slidingSyncRoomMetadataStatementsSQLite) UpsertJoinedRoom(
	ctx context.Context, txn *sql.Tx, room *tables.SlidingSyncJoinedRoom,
) error {
	stmt := sqlutil.TxStmt(txn, s.upsertJoinedRoomStmt)
	isEncrypted := 0
	if room.IsEncrypted {
		isEncrypted = 1
	}
	_, err := stmt.ExecContext(ctx,
		room.RoomID,
		room.EventStreamOrdering,
		room.BumpStamp,
		nullIfEmptySQLite(room.RoomType),
		nullIfEmptySQLite(room.RoomName),
		isEncrypted,
		nullIfEmptySQLite(room.TombstoneSuccessorRoomID),
	)
	return err
}

func (s *slidingSyncRoomMetadataStatementsSQLite) SelectJoinedRoom(
	ctx context.Context, txn *sql.Tx, roomID string,
) (*tables.SlidingSyncJoinedRoom, error) {
	stmt := sqlutil.TxStmt(txn, s.selectJoinedRoomStmt)
	var room tables.SlidingSyncJoinedRoom
	var roomType, roomName, tombstone sql.NullString
	var isEncrypted int
	err := stmt.QueryRowContext(ctx, roomID).Scan(
		&room.RoomID,
		&room.EventStreamOrdering,
		&room.BumpStamp,
		&roomType,
		&roomName,
		&isEncrypted,
		&tombstone,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	room.RoomType = roomType.String
	room.RoomName = roomName.String
	room.IsEncrypted = isEncrypted != 0
	room.TombstoneSuccessorRoomID = tombstone.String
	return &room, nil
}

func (s *slidingSyncRoomMetadataStatementsSQLite) SelectJoinedRooms(
	ctx context.Context, txn *sql.Tx, roomIDs []string,
) (map[string]*tables.SlidingSyncJoinedRoom, error) {
	if len(roomIDs) == 0 {
		return make(map[string]*tables.SlidingSyncJoinedRoom), nil
	}

	// SQLite doesn't support array parameters, so we build the query dynamically
	placeholders := make([]string, len(roomIDs))
	args := make([]interface{}, len(roomIDs))
	for i, roomID := range roomIDs {
		placeholders[i] = "?"
		args[i] = roomID
	}

	query := `
		SELECT room_id, event_stream_ordering, bump_stamp, room_type, room_name, is_encrypted, tombstone_successor_room_id
		FROM syncapi_sliding_sync_joined_rooms
		WHERE room_id IN (` + strings.Join(placeholders, ",") + `)`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*tables.SlidingSyncJoinedRoom)
	for rows.Next() {
		var room tables.SlidingSyncJoinedRoom
		var roomType, roomName, tombstone sql.NullString
		var isEncrypted int
		if err := rows.Scan(
			&room.RoomID,
			&room.EventStreamOrdering,
			&room.BumpStamp,
			&roomType,
			&roomName,
			&isEncrypted,
			&tombstone,
		); err != nil {
			return nil, err
		}
		room.RoomType = roomType.String
		room.RoomName = roomName.String
		room.IsEncrypted = isEncrypted != 0
		room.TombstoneSuccessorRoomID = tombstone.String
		result[room.RoomID] = &room
	}
	return result, rows.Err()
}

func (s *slidingSyncRoomMetadataStatementsSQLite) DeleteJoinedRoom(
	ctx context.Context, txn *sql.Tx, roomID string,
) error {
	stmt := sqlutil.TxStmt(txn, s.deleteJoinedRoomStmt)
	_, err := stmt.ExecContext(ctx, roomID)
	return err
}

func (s *slidingSyncRoomMetadataStatementsSQLite) SelectJoinedRoomsByFilters(
	ctx context.Context, txn *sql.Tx,
	isEncrypted *bool, roomType *string, notRoomTypes []string, limit int,
) ([]tables.SlidingSyncJoinedRoom, error) {
	// Build dynamic query based on filters
	query := `
		SELECT room_id, event_stream_ordering, bump_stamp, room_type, room_name, is_encrypted, tombstone_successor_room_id
		FROM syncapi_sliding_sync_joined_rooms
		WHERE 1=1
	`
	args := []interface{}{}

	if isEncrypted != nil {
		encVal := 0
		if *isEncrypted {
			encVal = 1
		}
		query += ` AND is_encrypted = ?`
		args = append(args, encVal)
	}

	if roomType != nil {
		if *roomType == "" {
			query += ` AND room_type IS NULL`
		} else {
			query += ` AND room_type = ?`
			args = append(args, *roomType)
		}
	}

	if len(notRoomTypes) > 0 {
		// SQLite doesn't have != ALL, so we use NOT IN
		placeholders := make([]string, len(notRoomTypes))
		for i, rt := range notRoomTypes {
			placeholders[i] = "?"
			args = append(args, rt)
		}
		query += ` AND (room_type IS NULL OR room_type NOT IN (` + strings.Join(placeholders, ",") + `))`
	}

	query += ` ORDER BY bump_stamp DESC, event_stream_ordering DESC`

	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rooms []tables.SlidingSyncJoinedRoom
	for rows.Next() {
		var room tables.SlidingSyncJoinedRoom
		var roomTypeVal, roomName, tombstone sql.NullString
		var isEncryptedVal int
		if err := rows.Scan(
			&room.RoomID,
			&room.EventStreamOrdering,
			&room.BumpStamp,
			&roomTypeVal,
			&roomName,
			&isEncryptedVal,
			&tombstone,
		); err != nil {
			return nil, err
		}
		room.RoomType = roomTypeVal.String
		room.RoomName = roomName.String
		room.IsEncrypted = isEncryptedVal != 0
		room.TombstoneSuccessorRoomID = tombstone.String
		rooms = append(rooms, room)
	}
	return rooms, rows.Err()
}

// ===== Membership Snapshots =====

func (s *slidingSyncRoomMetadataStatementsSQLite) UpsertMembershipSnapshot(
	ctx context.Context, txn *sql.Tx, snapshot *tables.SlidingSyncMembershipSnapshot,
) error {
	stmt := sqlutil.TxStmt(txn, s.upsertMembershipSnapshotStmt)
	forgotten := 0
	if snapshot.Forgotten {
		forgotten = 1
	}
	hasKnownState := 0
	if snapshot.HasKnownState {
		hasKnownState = 1
	}
	isEncrypted := 0
	if snapshot.IsEncrypted {
		isEncrypted = 1
	}
	_, err := stmt.ExecContext(ctx,
		snapshot.RoomID,
		snapshot.UserID,
		snapshot.Sender,
		snapshot.MembershipEventID,
		snapshot.Membership,
		forgotten,
		snapshot.EventStreamOrdering,
		hasKnownState,
		nullIfEmptySQLite(snapshot.RoomType),
		nullIfEmptySQLite(snapshot.RoomName),
		isEncrypted,
		nullIfEmptySQLite(snapshot.TombstoneSuccessorRoomID),
	)
	return err
}

func (s *slidingSyncRoomMetadataStatementsSQLite) SelectMembershipSnapshot(
	ctx context.Context, txn *sql.Tx, roomID, userID string,
) (*tables.SlidingSyncMembershipSnapshot, error) {
	stmt := sqlutil.TxStmt(txn, s.selectMembershipSnapshotStmt)
	var snapshot tables.SlidingSyncMembershipSnapshot
	var forgotten, hasKnownState, isEncrypted int
	var roomType, roomName, tombstone sql.NullString
	err := stmt.QueryRowContext(ctx, roomID, userID).Scan(
		&snapshot.RoomID,
		&snapshot.UserID,
		&snapshot.Sender,
		&snapshot.MembershipEventID,
		&snapshot.Membership,
		&forgotten,
		&snapshot.EventStreamOrdering,
		&hasKnownState,
		&roomType,
		&roomName,
		&isEncrypted,
		&tombstone,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	snapshot.Forgotten = forgotten != 0
	snapshot.HasKnownState = hasKnownState != 0
	snapshot.IsEncrypted = isEncrypted != 0
	snapshot.RoomType = roomType.String
	snapshot.RoomName = roomName.String
	snapshot.TombstoneSuccessorRoomID = tombstone.String
	return &snapshot, nil
}

func (s *slidingSyncRoomMetadataStatementsSQLite) SelectMembershipSnapshotsForUser(
	ctx context.Context, txn *sql.Tx, userID string, memberships []string,
) ([]tables.SlidingSyncMembershipSnapshot, error) {
	var rows *sql.Rows
	var err error

	if len(memberships) == 0 {
		stmt := sqlutil.TxStmt(txn, s.selectMembershipSnapshotsForUserStmt)
		rows, err = stmt.QueryContext(ctx, userID)
	} else {
		// Build dynamic query for memberships
		placeholders := make([]string, len(memberships))
		args := make([]interface{}, len(memberships)+1)
		args[0] = userID
		for i, m := range memberships {
			placeholders[i] = "?"
			args[i+1] = m
		}
		query := `
			SELECT room_id, user_id, sender, membership_event_id, membership, forgotten, event_stream_ordering,
				   has_known_state, room_type, room_name, is_encrypted, tombstone_successor_room_id
			FROM syncapi_sliding_sync_membership_snapshots
			WHERE user_id = ? AND forgotten = 0 AND membership IN (` + strings.Join(placeholders, ",") + `)`
		rows, err = s.db.QueryContext(ctx, query, args...)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []tables.SlidingSyncMembershipSnapshot
	for rows.Next() {
		var snapshot tables.SlidingSyncMembershipSnapshot
		var forgotten, hasKnownState, isEncrypted int
		var roomType, roomName, tombstone sql.NullString
		if err := rows.Scan(
			&snapshot.RoomID,
			&snapshot.UserID,
			&snapshot.Sender,
			&snapshot.MembershipEventID,
			&snapshot.Membership,
			&forgotten,
			&snapshot.EventStreamOrdering,
			&hasKnownState,
			&roomType,
			&roomName,
			&isEncrypted,
			&tombstone,
		); err != nil {
			return nil, err
		}
		snapshot.Forgotten = forgotten != 0
		snapshot.HasKnownState = hasKnownState != 0
		snapshot.IsEncrypted = isEncrypted != 0
		snapshot.RoomType = roomType.String
		snapshot.RoomName = roomName.String
		snapshot.TombstoneSuccessorRoomID = tombstone.String
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, rows.Err()
}

func (s *slidingSyncRoomMetadataStatementsSQLite) UpdateMembershipForgotten(
	ctx context.Context, txn *sql.Tx, roomID, userID string, forgotten bool,
) error {
	stmt := sqlutil.TxStmt(txn, s.updateMembershipForgottenStmt)
	forgottenInt := 0
	if forgotten {
		forgottenInt = 1
	}
	_, err := stmt.ExecContext(ctx, roomID, userID, forgottenInt)
	return err
}

func (s *slidingSyncRoomMetadataStatementsSQLite) DeleteMembershipSnapshot(
	ctx context.Context, txn *sql.Tx, roomID, userID string,
) error {
	stmt := sqlutil.TxStmt(txn, s.deleteMembershipSnapshotStmt)
	_, err := stmt.ExecContext(ctx, roomID, userID)
	return err
}

// Helper function to convert empty strings to NULL
func nullIfEmptySQLite(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// Ensure we implement the interface
var _ tables.SlidingSyncRoomMetadata = &slidingSyncRoomMetadataStatementsSQLite{}
