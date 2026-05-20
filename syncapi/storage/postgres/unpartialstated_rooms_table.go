// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/element-hq/dendrite/internal"
	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/syncapi/storage/tables"
	"github.com/element-hq/dendrite/syncapi/types"
)

const unPartialStatedRoomsSchema = `
CREATE SEQUENCE IF NOT EXISTS syncapi_unpartialstated_rooms_id;

-- Tracks rooms that have completed their partial state resync (MSC3706).
-- When a room completes its partial state resync, we insert a row for each
-- user in the room so that sync can treat the room as "newly joined".
CREATE TABLE IF NOT EXISTS syncapi_unpartialstated_rooms (
	-- The stream position ID
	id BIGINT PRIMARY KEY DEFAULT nextval('syncapi_unpartialstated_rooms_id'),
	-- The room ID that completed partial state
	room_id TEXT NOT NULL,
	-- The user ID who should see this room as "newly joined"
	user_id TEXT NOT NULL,
	-- Timestamp when the room completed partial state
	created_at BIGINT NOT NULL DEFAULT (extract(epoch from now()) * 1000)
);
CREATE INDEX IF NOT EXISTS syncapi_unpartialstated_rooms_user_id ON syncapi_unpartialstated_rooms(user_id);
CREATE INDEX IF NOT EXISTS syncapi_unpartialstated_rooms_room_id ON syncapi_unpartialstated_rooms(room_id);
`

const insertUnPartialStatedRoomSQL = "" +
	"INSERT INTO syncapi_unpartialstated_rooms (room_id, user_id)" +
	" VALUES ($1, $2)" +
	" RETURNING id"

const selectUnPartialStatedRoomsInRangeSQL = "" +
	"SELECT id, room_id FROM syncapi_unpartialstated_rooms" +
	" WHERE user_id = $1 AND id > $2 AND id <= $3"

const selectMaxUnPartialStatedRoomIDSQL = "" +
	"SELECT MAX(id) FROM syncapi_unpartialstated_rooms"

const purgeUnPartialStatedRoomsSQL = "" +
	"DELETE FROM syncapi_unpartialstated_rooms WHERE room_id = $1"

type unPartialStatedRoomsStatements struct {
	db                                 *sql.DB
	insertUnPartialStatedRoomStmt      *sql.Stmt
	selectUnPartialStatedRoomsInRange  *sql.Stmt
	selectMaxUnPartialStatedRoomIDStmt *sql.Stmt
	purgeUnPartialStatedRoomsStmt      *sql.Stmt
}

func NewPostgresUnPartialStatedRoomsTable(db *sql.DB) (tables.UnPartialStatedRooms, error) {
	_, err := db.Exec(unPartialStatedRoomsSchema)
	if err != nil {
		return nil, err
	}
	s := &unPartialStatedRoomsStatements{
		db: db,
	}
	return s, sqlutil.StatementList{
		{&s.insertUnPartialStatedRoomStmt, insertUnPartialStatedRoomSQL},
		{&s.selectUnPartialStatedRoomsInRange, selectUnPartialStatedRoomsInRangeSQL},
		{&s.selectMaxUnPartialStatedRoomIDStmt, selectMaxUnPartialStatedRoomIDSQL},
		{&s.purgeUnPartialStatedRoomsStmt, purgeUnPartialStatedRoomsSQL},
	}.Prepare(db)
}

func (s *unPartialStatedRoomsStatements) InsertUnPartialStatedRoom(
	ctx context.Context, txn *sql.Tx, roomID, userID string,
) (pos types.StreamPosition, err error) {
	stmt := sqlutil.TxStmt(txn, s.insertUnPartialStatedRoomStmt)
	err = stmt.QueryRowContext(ctx, roomID, userID).Scan(&pos)
	return
}

func (s *unPartialStatedRoomsStatements) SelectUnPartialStatedRoomsInRange(
	ctx context.Context, txn *sql.Tx, userID string, r types.Range,
) ([]string, types.StreamPosition, error) {
	var lastPos types.StreamPosition
	rows, err := sqlutil.TxStmt(txn, s.selectUnPartialStatedRoomsInRange).QueryContext(ctx, userID, r.Low(), r.High())
	if err != nil {
		return nil, 0, fmt.Errorf("unable to query un-partial-stated rooms: %w", err)
	}
	defer internal.CloseAndLogIfError(ctx, rows, "SelectUnPartialStatedRoomsInRange: rows.close() failed")

	var roomIDs []string
	for rows.Next() {
		var id types.StreamPosition
		var roomID string
		if err = rows.Scan(&id, &roomID); err != nil {
			return nil, 0, fmt.Errorf("unable to scan row: %w", err)
		}
		roomIDs = append(roomIDs, roomID)
		if id > lastPos {
			lastPos = id
		}
	}
	return roomIDs, lastPos, rows.Err()
}

func (s *unPartialStatedRoomsStatements) SelectMaxUnPartialStatedRoomID(
	ctx context.Context, txn *sql.Tx,
) (id int64, err error) {
	var nullableID sql.NullInt64
	stmt := sqlutil.TxStmt(txn, s.selectMaxUnPartialStatedRoomIDStmt)
	err = stmt.QueryRowContext(ctx).Scan(&nullableID)
	if nullableID.Valid {
		id = nullableID.Int64
	}
	return
}

func (s *unPartialStatedRoomsStatements) PurgeUnPartialStatedRooms(
	ctx context.Context, txn *sql.Tx, roomID string,
) error {
	_, err := sqlutil.TxStmt(txn, s.purgeUnPartialStatedRoomsStmt).ExecContext(ctx, roomID)
	return err
}
