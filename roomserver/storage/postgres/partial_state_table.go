// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package postgres

import (
	"context"
	"database/sql"

	"github.com/element-hq/dendrite/internal"
	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/roomserver/storage/postgres/deltas"
	"github.com/element-hq/dendrite/roomserver/storage/tables"
	"github.com/element-hq/dendrite/roomserver/types"
	"github.com/lib/pq"
)

// Schema for tracking rooms with partial state from MSC3706 faster joins.
// Two tables are used:
// - roomserver_partial_state_rooms: tracks which rooms have partial state
// - roomserver_partial_state_rooms_servers: tracks servers known to be in the room
const partialStateSchema = `
-- Track rooms where we've done a partial-state join (MSC3706)
CREATE TABLE IF NOT EXISTS roomserver_partial_state_rooms (
    room_nid BIGINT PRIMARY KEY,
    join_event_nid BIGINT NOT NULL,
    joined_via TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    -- Device list stream position at the time of the partial state join (MSC3706/MSC3902)
    -- Used to replay device list changes when the room becomes fully synced
    device_lists_stream_id BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_partial_state_rooms_created
    ON roomserver_partial_state_rooms(created_at);

-- Servers known to be in the room at join time
CREATE TABLE IF NOT EXISTS roomserver_partial_state_rooms_servers (
    room_nid BIGINT NOT NULL REFERENCES roomserver_partial_state_rooms(room_nid)
        ON DELETE CASCADE,
    server_name TEXT NOT NULL,
    PRIMARY KEY (room_nid, server_name)
);
`

const insertPartialStateRoomSQL = "" +
	"INSERT INTO roomserver_partial_state_rooms (room_nid, join_event_nid, joined_via, device_lists_stream_id) VALUES ($1, $2, $3, $4)" +
	" ON CONFLICT (room_nid) DO UPDATE SET join_event_nid = $2, joined_via = $3, created_at = NOW(), device_lists_stream_id = $4"

const insertPartialStateRoomServersSQL = "" +
	"INSERT INTO roomserver_partial_state_rooms_servers (room_nid, server_name) VALUES ($1, unnest($2::text[]))" +
	" ON CONFLICT (room_nid, server_name) DO NOTHING"

const selectPartialStateRoomSQL = "" +
	"SELECT 1 FROM roomserver_partial_state_rooms WHERE room_nid = $1"

const selectPartialStateServersSQL = "" +
	"SELECT server_name FROM roomserver_partial_state_rooms_servers WHERE room_nid = $1"

const selectAllPartialStateRoomsSQL = "" +
	"SELECT room_nid FROM roomserver_partial_state_rooms ORDER BY created_at ASC"

const selectDeviceListStreamIDSQL = "" +
	"SELECT device_lists_stream_id FROM roomserver_partial_state_rooms WHERE room_nid = $1"

const deletePartialStateRoomSQL = "" +
	"DELETE FROM roomserver_partial_state_rooms WHERE room_nid = $1 RETURNING device_lists_stream_id"

type partialStateStatements struct {
	insertPartialStateRoomStmt         *sql.Stmt
	insertPartialStateRoomServersStmt  *sql.Stmt
	selectPartialStateRoomStmt         *sql.Stmt
	selectPartialStateServersStmt      *sql.Stmt
	selectAllPartialStateRoomsStmt     *sql.Stmt
	selectDeviceListStreamIDStmt       *sql.Stmt
	deletePartialStateRoomStmt         *sql.Stmt
}

func CreatePartialStateTable(db *sql.DB) error {
	_, err := db.Exec(partialStateSchema)
	if err != nil {
		return err
	}
	m := sqlutil.NewMigrator(db)
	m.AddMigrations(sqlutil.Migration{
		Version: "roomserver: add device_lists_stream_id to partial state rooms",
		Up:      deltas.UpPartialStateDeviceListStreamID,
	})
	return m.Up(context.Background())
}

func PreparePartialStateTable(db *sql.DB) (tables.PartialState, error) {
	s := &partialStateStatements{}

	return s, sqlutil.StatementList{
		{&s.insertPartialStateRoomStmt, insertPartialStateRoomSQL},
		{&s.insertPartialStateRoomServersStmt, insertPartialStateRoomServersSQL},
		{&s.selectPartialStateRoomStmt, selectPartialStateRoomSQL},
		{&s.selectPartialStateServersStmt, selectPartialStateServersSQL},
		{&s.selectAllPartialStateRoomsStmt, selectAllPartialStateRoomsSQL},
		{&s.selectDeviceListStreamIDStmt, selectDeviceListStreamIDSQL},
		{&s.deletePartialStateRoomStmt, deletePartialStateRoomSQL},
	}.Prepare(db)
}

func (s *partialStateStatements) InsertPartialStateRoom(
	ctx context.Context, txn *sql.Tx,
	roomNID types.RoomNID, joinEventNID types.EventNID, joinedVia string, serversInRoom []string,
	deviceListStreamID int64,
) error {
	// Insert the room entry
	stmt := sqlutil.TxStmt(txn, s.insertPartialStateRoomStmt)
	_, err := stmt.ExecContext(ctx, roomNID, joinEventNID, joinedVia, deviceListStreamID)
	if err != nil {
		return err
	}

	// Insert the servers
	if len(serversInRoom) > 0 {
		stmt = sqlutil.TxStmt(txn, s.insertPartialStateRoomServersStmt)
		_, err = stmt.ExecContext(ctx, roomNID, pq.Array(serversInRoom))
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *partialStateStatements) SelectPartialStateRoom(
	ctx context.Context, txn *sql.Tx, roomNID types.RoomNID,
) (bool, error) {
	var result int
	stmt := sqlutil.TxStmt(txn, s.selectPartialStateRoomStmt)
	err := stmt.QueryRowContext(ctx, roomNID).Scan(&result)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *partialStateStatements) SelectPartialStateServers(
	ctx context.Context, txn *sql.Tx, roomNID types.RoomNID,
) ([]string, error) {
	stmt := sqlutil.TxStmt(txn, s.selectPartialStateServersStmt)
	rows, err := stmt.QueryContext(ctx, roomNID)
	if err != nil {
		return nil, err
	}
	defer internal.CloseAndLogIfError(ctx, rows, "SelectPartialStateServers: rows.close() failed")

	var servers []string
	for rows.Next() {
		var server string
		if err = rows.Scan(&server); err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, rows.Err()
}

func (s *partialStateStatements) SelectAllPartialStateRooms(
	ctx context.Context, txn *sql.Tx,
) ([]types.RoomNID, error) {
	stmt := sqlutil.TxStmt(txn, s.selectAllPartialStateRoomsStmt)
	rows, err := stmt.QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer internal.CloseAndLogIfError(ctx, rows, "SelectAllPartialStateRooms: rows.close() failed")

	var roomNIDs []types.RoomNID
	for rows.Next() {
		var roomNID types.RoomNID
		if err = rows.Scan(&roomNID); err != nil {
			return nil, err
		}
		roomNIDs = append(roomNIDs, roomNID)
	}
	return roomNIDs, rows.Err()
}

func (s *partialStateStatements) SelectDeviceListStreamID(
	ctx context.Context, txn *sql.Tx, roomNID types.RoomNID,
) (int64, error) {
	var streamID int64
	stmt := sqlutil.TxStmt(txn, s.selectDeviceListStreamIDStmt)
	err := stmt.QueryRowContext(ctx, roomNID).Scan(&streamID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return streamID, nil
}

func (s *partialStateStatements) DeletePartialStateRoom(
	ctx context.Context, txn *sql.Tx, roomNID types.RoomNID,
) (int64, error) {
	var deviceListStreamID int64
	stmt := sqlutil.TxStmt(txn, s.deletePartialStateRoomStmt)
	err := stmt.QueryRowContext(ctx, roomNID).Scan(&deviceListStreamID)
	if err == sql.ErrNoRows {
		// Room wasn't in partial state, nothing to do
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return deviceListStreamID, nil
}
