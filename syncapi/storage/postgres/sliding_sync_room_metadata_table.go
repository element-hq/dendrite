// Copyright 2025 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package postgres

import (
	"context"
	"database/sql"

	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/syncapi/storage/tables"
	"github.com/lib/pq"
)

// SQL statements for rooms to recalculate
const insertRoomToRecalculateSQL = `
	INSERT INTO syncapi_sliding_sync_rooms_to_recalculate (room_id)
	VALUES ($1)
	ON CONFLICT (room_id) DO NOTHING
`

const selectRoomsToRecalculateSQL = `
	SELECT room_id FROM syncapi_sliding_sync_rooms_to_recalculate
	LIMIT $1
`

const deleteRoomToRecalculateSQL = `
	DELETE FROM syncapi_sliding_sync_rooms_to_recalculate
	WHERE room_id = $1
`

// SQL statements for joined rooms
const upsertJoinedRoomSQL = `
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

const selectJoinedRoomSQL = `
	SELECT room_id, event_stream_ordering, bump_stamp, room_type, room_name, is_encrypted, tombstone_successor_room_id
	FROM syncapi_sliding_sync_joined_rooms
	WHERE room_id = $1
`

const selectJoinedRoomsSQL = `
	SELECT room_id, event_stream_ordering, bump_stamp, room_type, room_name, is_encrypted, tombstone_successor_room_id
	FROM syncapi_sliding_sync_joined_rooms
	WHERE room_id = ANY($1)
`

const deleteJoinedRoomSQL = `
	DELETE FROM syncapi_sliding_sync_joined_rooms
	WHERE room_id = $1
`

// SQL statements for membership snapshots
const upsertMembershipSnapshotSQL = `
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

const selectMembershipSnapshotSQL = `
	SELECT room_id, user_id, sender, membership_event_id, membership, forgotten, event_stream_ordering,
		   has_known_state, room_type, room_name, is_encrypted, tombstone_successor_room_id
	FROM syncapi_sliding_sync_membership_snapshots
	WHERE room_id = $1 AND user_id = $2
`

const selectMembershipSnapshotsForUserSQL = `
	SELECT room_id, user_id, sender, membership_event_id, membership, forgotten, event_stream_ordering,
		   has_known_state, room_type, room_name, is_encrypted, tombstone_successor_room_id
	FROM syncapi_sliding_sync_membership_snapshots
	WHERE user_id = $1 AND forgotten = 0
`

const selectMembershipSnapshotsForUserWithMembershipsSQL = `
	SELECT room_id, user_id, sender, membership_event_id, membership, forgotten, event_stream_ordering,
		   has_known_state, room_type, room_name, is_encrypted, tombstone_successor_room_id
	FROM syncapi_sliding_sync_membership_snapshots
	WHERE user_id = $1 AND forgotten = 0 AND membership = ANY($2)
`

const updateMembershipForgottenSQL = `
	UPDATE syncapi_sliding_sync_membership_snapshots
	SET forgotten = $3
	WHERE room_id = $1 AND user_id = $2
`

const deleteMembershipSnapshotSQL = `
	DELETE FROM syncapi_sliding_sync_membership_snapshots
	WHERE room_id = $1 AND user_id = $2
`

type slidingSyncRoomMetadataStatements struct {
	insertRoomToRecalculateStmt                      *sql.Stmt
	selectRoomsToRecalculateStmt                     *sql.Stmt
	deleteRoomToRecalculateStmt                      *sql.Stmt
	upsertJoinedRoomStmt                             *sql.Stmt
	selectJoinedRoomStmt                             *sql.Stmt
	selectJoinedRoomsStmt                            *sql.Stmt
	deleteJoinedRoomStmt                             *sql.Stmt
	upsertMembershipSnapshotStmt                     *sql.Stmt
	selectMembershipSnapshotStmt                     *sql.Stmt
	selectMembershipSnapshotsForUserStmt             *sql.Stmt
	selectMembershipSnapshotsForUserWithMembershipsStmt *sql.Stmt
	updateMembershipForgottenStmt                    *sql.Stmt
	deleteMembershipSnapshotStmt                     *sql.Stmt
	db                                               *sql.DB
}

func NewPostgresSlidingSyncRoomMetadataTable(db *sql.DB) (tables.SlidingSyncRoomMetadata, error) {
	s := &slidingSyncRoomMetadataStatements{db: db}
	return s, sqlutil.StatementList{
		{&s.insertRoomToRecalculateStmt, insertRoomToRecalculateSQL},
		{&s.selectRoomsToRecalculateStmt, selectRoomsToRecalculateSQL},
		{&s.deleteRoomToRecalculateStmt, deleteRoomToRecalculateSQL},
		{&s.upsertJoinedRoomStmt, upsertJoinedRoomSQL},
		{&s.selectJoinedRoomStmt, selectJoinedRoomSQL},
		{&s.selectJoinedRoomsStmt, selectJoinedRoomsSQL},
		{&s.deleteJoinedRoomStmt, deleteJoinedRoomSQL},
		{&s.upsertMembershipSnapshotStmt, upsertMembershipSnapshotSQL},
		{&s.selectMembershipSnapshotStmt, selectMembershipSnapshotSQL},
		{&s.selectMembershipSnapshotsForUserStmt, selectMembershipSnapshotsForUserSQL},
		{&s.selectMembershipSnapshotsForUserWithMembershipsStmt, selectMembershipSnapshotsForUserWithMembershipsSQL},
		{&s.updateMembershipForgottenStmt, updateMembershipForgottenSQL},
		{&s.deleteMembershipSnapshotStmt, deleteMembershipSnapshotSQL},
	}.Prepare(db)
}

// ===== Rooms To Recalculate =====

func (s *slidingSyncRoomMetadataStatements) InsertRoomToRecalculate(
	ctx context.Context, txn *sql.Tx, roomID string,
) error {
	stmt := sqlutil.TxStmt(txn, s.insertRoomToRecalculateStmt)
	_, err := stmt.ExecContext(ctx, roomID)
	return err
}

func (s *slidingSyncRoomMetadataStatements) SelectRoomsToRecalculate(
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

func (s *slidingSyncRoomMetadataStatements) DeleteRoomToRecalculate(
	ctx context.Context, txn *sql.Tx, roomID string,
) error {
	stmt := sqlutil.TxStmt(txn, s.deleteRoomToRecalculateStmt)
	_, err := stmt.ExecContext(ctx, roomID)
	return err
}

// ===== Joined Rooms =====

func (s *slidingSyncRoomMetadataStatements) UpsertJoinedRoom(
	ctx context.Context, txn *sql.Tx, room *tables.SlidingSyncJoinedRoom,
) error {
	stmt := sqlutil.TxStmt(txn, s.upsertJoinedRoomStmt)
	_, err := stmt.ExecContext(ctx,
		room.RoomID,
		room.EventStreamOrdering,
		room.BumpStamp,
		nullIfEmpty(room.RoomType),
		nullIfEmpty(room.RoomName),
		room.IsEncrypted,
		nullIfEmpty(room.TombstoneSuccessorRoomID),
	)
	return err
}

func (s *slidingSyncRoomMetadataStatements) SelectJoinedRoom(
	ctx context.Context, txn *sql.Tx, roomID string,
) (*tables.SlidingSyncJoinedRoom, error) {
	stmt := sqlutil.TxStmt(txn, s.selectJoinedRoomStmt)
	var room tables.SlidingSyncJoinedRoom
	var roomType, roomName, tombstone sql.NullString
	err := stmt.QueryRowContext(ctx, roomID).Scan(
		&room.RoomID,
		&room.EventStreamOrdering,
		&room.BumpStamp,
		&roomType,
		&roomName,
		&room.IsEncrypted,
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
	room.TombstoneSuccessorRoomID = tombstone.String
	return &room, nil
}

func (s *slidingSyncRoomMetadataStatements) SelectJoinedRooms(
	ctx context.Context, txn *sql.Tx, roomIDs []string,
) (map[string]*tables.SlidingSyncJoinedRoom, error) {
	if len(roomIDs) == 0 {
		return make(map[string]*tables.SlidingSyncJoinedRoom), nil
	}
	stmt := sqlutil.TxStmt(txn, s.selectJoinedRoomsStmt)
	rows, err := stmt.QueryContext(ctx, pq.Array(roomIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*tables.SlidingSyncJoinedRoom)
	for rows.Next() {
		var room tables.SlidingSyncJoinedRoom
		var roomType, roomName, tombstone sql.NullString
		if err := rows.Scan(
			&room.RoomID,
			&room.EventStreamOrdering,
			&room.BumpStamp,
			&roomType,
			&roomName,
			&room.IsEncrypted,
			&tombstone,
		); err != nil {
			return nil, err
		}
		room.RoomType = roomType.String
		room.RoomName = roomName.String
		room.TombstoneSuccessorRoomID = tombstone.String
		result[room.RoomID] = &room
	}
	return result, rows.Err()
}

func (s *slidingSyncRoomMetadataStatements) DeleteJoinedRoom(
	ctx context.Context, txn *sql.Tx, roomID string,
) error {
	stmt := sqlutil.TxStmt(txn, s.deleteJoinedRoomStmt)
	_, err := stmt.ExecContext(ctx, roomID)
	return err
}

func (s *slidingSyncRoomMetadataStatements) SelectJoinedRoomsByFilters(
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
	argNum := 1

	if isEncrypted != nil {
		query += ` AND is_encrypted = $` + string(rune('0'+argNum))
		args = append(args, *isEncrypted)
		argNum++
	}

	if roomType != nil {
		if *roomType == "" {
			query += ` AND room_type IS NULL`
		} else {
			query += ` AND room_type = $` + string(rune('0'+argNum))
			args = append(args, *roomType)
			argNum++
		}
	}

	if len(notRoomTypes) > 0 {
		query += ` AND (room_type IS NULL OR room_type != ALL($` + string(rune('0'+argNum)) + `))`
		args = append(args, pq.Array(notRoomTypes))
		argNum++
	}

	query += ` ORDER BY bump_stamp DESC NULLS LAST, event_stream_ordering DESC`

	if limit > 0 {
		query += ` LIMIT $` + string(rune('0'+argNum))
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
		if err := rows.Scan(
			&room.RoomID,
			&room.EventStreamOrdering,
			&room.BumpStamp,
			&roomTypeVal,
			&roomName,
			&room.IsEncrypted,
			&tombstone,
		); err != nil {
			return nil, err
		}
		room.RoomType = roomTypeVal.String
		room.RoomName = roomName.String
		room.TombstoneSuccessorRoomID = tombstone.String
		rooms = append(rooms, room)
	}
	return rooms, rows.Err()
}

// ===== Membership Snapshots =====

func (s *slidingSyncRoomMetadataStatements) UpsertMembershipSnapshot(
	ctx context.Context, txn *sql.Tx, snapshot *tables.SlidingSyncMembershipSnapshot,
) error {
	stmt := sqlutil.TxStmt(txn, s.upsertMembershipSnapshotStmt)
	forgotten := 0
	if snapshot.Forgotten {
		forgotten = 1
	}
	_, err := stmt.ExecContext(ctx,
		snapshot.RoomID,
		snapshot.UserID,
		snapshot.Sender,
		snapshot.MembershipEventID,
		snapshot.Membership,
		forgotten,
		snapshot.EventStreamOrdering,
		snapshot.HasKnownState,
		nullIfEmpty(snapshot.RoomType),
		nullIfEmpty(snapshot.RoomName),
		snapshot.IsEncrypted,
		nullIfEmpty(snapshot.TombstoneSuccessorRoomID),
	)
	return err
}

func (s *slidingSyncRoomMetadataStatements) SelectMembershipSnapshot(
	ctx context.Context, txn *sql.Tx, roomID, userID string,
) (*tables.SlidingSyncMembershipSnapshot, error) {
	stmt := sqlutil.TxStmt(txn, s.selectMembershipSnapshotStmt)
	var snapshot tables.SlidingSyncMembershipSnapshot
	var forgotten int
	var roomType, roomName, tombstone sql.NullString
	err := stmt.QueryRowContext(ctx, roomID, userID).Scan(
		&snapshot.RoomID,
		&snapshot.UserID,
		&snapshot.Sender,
		&snapshot.MembershipEventID,
		&snapshot.Membership,
		&forgotten,
		&snapshot.EventStreamOrdering,
		&snapshot.HasKnownState,
		&roomType,
		&roomName,
		&snapshot.IsEncrypted,
		&tombstone,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	snapshot.Forgotten = forgotten != 0
	snapshot.RoomType = roomType.String
	snapshot.RoomName = roomName.String
	snapshot.TombstoneSuccessorRoomID = tombstone.String
	return &snapshot, nil
}

func (s *slidingSyncRoomMetadataStatements) SelectMembershipSnapshotsForUser(
	ctx context.Context, txn *sql.Tx, userID string, memberships []string,
) ([]tables.SlidingSyncMembershipSnapshot, error) {
	var rows *sql.Rows
	var err error

	if len(memberships) == 0 {
		stmt := sqlutil.TxStmt(txn, s.selectMembershipSnapshotsForUserStmt)
		rows, err = stmt.QueryContext(ctx, userID)
	} else {
		stmt := sqlutil.TxStmt(txn, s.selectMembershipSnapshotsForUserWithMembershipsStmt)
		rows, err = stmt.QueryContext(ctx, userID, pq.Array(memberships))
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []tables.SlidingSyncMembershipSnapshot
	for rows.Next() {
		var snapshot tables.SlidingSyncMembershipSnapshot
		var forgotten int
		var roomType, roomName, tombstone sql.NullString
		if err := rows.Scan(
			&snapshot.RoomID,
			&snapshot.UserID,
			&snapshot.Sender,
			&snapshot.MembershipEventID,
			&snapshot.Membership,
			&forgotten,
			&snapshot.EventStreamOrdering,
			&snapshot.HasKnownState,
			&roomType,
			&roomName,
			&snapshot.IsEncrypted,
			&tombstone,
		); err != nil {
			return nil, err
		}
		snapshot.Forgotten = forgotten != 0
		snapshot.RoomType = roomType.String
		snapshot.RoomName = roomName.String
		snapshot.TombstoneSuccessorRoomID = tombstone.String
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, rows.Err()
}

func (s *slidingSyncRoomMetadataStatements) UpdateMembershipForgotten(
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

func (s *slidingSyncRoomMetadataStatements) DeleteMembershipSnapshot(
	ctx context.Context, txn *sql.Tx, roomID, userID string,
) error {
	stmt := sqlutil.TxStmt(txn, s.deleteMembershipSnapshotStmt)
	_, err := stmt.ExecContext(ctx, roomID, userID)
	return err
}

// Helper function to convert empty strings to NULL
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// Ensure we implement the interface
var _ tables.SlidingSyncRoomMetadata = &slidingSyncRoomMetadataStatements{}
