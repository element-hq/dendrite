// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package sqlite3

import (
	"context"
	"database/sql"

	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/syncapi/storage/tables"
)

const slidingSyncSchema = `
-- See syncapi/storage/sqlite3/sliding_sync_schema.sql for full schema
-- This should be empty as schema is applied separately
`

// SQL statements for connection management (SQLite3 uses INTEGER instead of BIGINT)
const insertConnectionSQL = `
	INSERT INTO syncapi_sliding_sync_connections (user_id, device_id, conn_id, created_ts)
	VALUES ($1, $2, $3, $4)
	RETURNING connection_key
`

const selectConnectionByKeySQL = `
	SELECT connection_key, user_id, device_id, conn_id, created_ts
	FROM syncapi_sliding_sync_connections
	WHERE connection_key = $1
`

const selectConnectionByIDsSQL = `
	SELECT connection_key, user_id, device_id, conn_id, created_ts
	FROM syncapi_sliding_sync_connections
	WHERE user_id = $1 AND device_id = $2 AND conn_id = $3
`

const deleteConnectionSQL = `
	DELETE FROM syncapi_sliding_sync_connections
	WHERE connection_key = $1
`

const deleteOldConnectionsSQL = `
	DELETE FROM syncapi_sliding_sync_connections
	WHERE created_ts < $1
`

// SQL statements for position management
const insertConnectionPositionSQL = `
	INSERT INTO syncapi_sliding_sync_connection_positions (connection_key, created_ts)
	VALUES ($1, $2)
	RETURNING connection_position
`

const selectConnectionPositionSQL = `
	SELECT connection_position, connection_key, created_ts
	FROM syncapi_sliding_sync_connection_positions
	WHERE connection_position = $1
`

const selectLatestConnectionPositionSQL = `
	SELECT connection_position, connection_key, created_ts
	FROM syncapi_sliding_sync_connection_positions
	WHERE connection_key = $1
	ORDER BY connection_position DESC
	LIMIT 1
`

// SQL statements for required state management
const insertRequiredStateSQL = `
	INSERT INTO syncapi_sliding_sync_connection_required_state (connection_key, required_state)
	VALUES ($1, $2)
	RETURNING required_state_id
`

const selectRequiredStateSQL = `
	SELECT required_state
	FROM syncapi_sliding_sync_connection_required_state
	WHERE required_state_id = $1
`

const selectRequiredStateByContentSQL = `
	SELECT required_state_id
	FROM syncapi_sliding_sync_connection_required_state
	WHERE connection_key = $1 AND required_state = $2
	LIMIT 1
`

// SQL statements for room config management
const upsertRoomConfigSQL = `
	INSERT INTO syncapi_sliding_sync_connection_room_configs
		(connection_position, room_id, timeline_limit, required_state_id)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (connection_position, room_id)
	DO UPDATE SET timeline_limit = $3, required_state_id = $4
`

const selectRoomConfigSQL = `
	SELECT connection_position, room_id, timeline_limit, required_state_id
	FROM syncapi_sliding_sync_connection_room_configs
	WHERE connection_position = $1 AND room_id = $2
`

const selectLatestRoomConfigSQL = `
	SELECT rc.connection_position, rc.room_id, rc.timeline_limit, rc.required_state_id
	FROM syncapi_sliding_sync_connection_room_configs rc
	INNER JOIN syncapi_sliding_sync_connection_positions cp USING (connection_position)
	WHERE cp.connection_key = $1 AND rc.room_id = $2
	ORDER BY rc.connection_position DESC
	LIMIT 1
`

// selectRoomConfigsByPositionSQL retrieves all room configs for a specific position
// Used to load previous room configs for copy-forward during sync
const selectRoomConfigsByPositionSQL = `
	SELECT connection_position, room_id, timeline_limit, required_state_id
	FROM syncapi_sliding_sync_connection_room_configs
	WHERE connection_position = $1
`

// SQL statements for stream management
const upsertConnectionStreamSQL = `
	INSERT INTO syncapi_sliding_sync_connection_streams
		(connection_position, room_id, stream, room_status, last_token)
	VALUES ($1, $2, $3, $4, $5)
	ON CONFLICT (connection_position, room_id, stream)
	DO UPDATE SET room_status = $4, last_token = $5
`

const selectConnectionStreamSQL = `
	SELECT connection_position, room_id, stream, room_status, last_token
	FROM syncapi_sliding_sync_connection_streams
	WHERE connection_position = $1 AND room_id = $2 AND stream = $3
`

const selectLatestConnectionStreamSQL = `
	SELECT cs.connection_position, cs.room_id, cs.stream, cs.room_status, cs.last_token
	FROM syncapi_sliding_sync_connection_streams cs
	INNER JOIN syncapi_sliding_sync_connection_positions cp USING (connection_position)
	WHERE cp.connection_key = $1 AND cs.room_id = $2 AND cs.stream = $3
	ORDER BY cs.connection_position DESC
	LIMIT 1
`

const selectAllLatestConnectionStreamsSQL = `
	SELECT room_id, stream, room_status, last_token, connection_position
	FROM syncapi_sliding_sync_latest_room_state
	WHERE connection_key = $1
`

// selectConnectionStreamsByPositionSQL retrieves all streams for a specific position
// This is used for incremental syncs to get the state as it was at that position
const selectConnectionStreamsByPositionSQL = `
	SELECT room_id, stream, room_status, last_token, connection_position
	FROM syncapi_sliding_sync_connection_streams
	WHERE connection_position = $1
`

// deleteOtherConnectionPositionsSQL removes all positions except the specified one
// This is called when a client uses a position, to clean up old state (like Synapse)
const deleteOtherConnectionPositionsSQL = `
	DELETE FROM syncapi_sliding_sync_connection_positions
	WHERE connection_key = $1 AND connection_position != $2
`

// SQL statements for list management
const upsertConnectionListSQL = `
	INSERT INTO syncapi_sliding_sync_connection_lists (connection_key, list_name, room_ids)
	VALUES ($1, $2, $3)
	ON CONFLICT (connection_key, list_name)
	DO UPDATE SET room_ids = $3
`

const selectConnectionListSQL = `
	SELECT room_ids
	FROM syncapi_sliding_sync_connection_lists
	WHERE connection_key = $1 AND list_name = $2
`

type slidingSyncStatements struct {
	db                                   *sql.DB
	insertConnectionStmt                 *sql.Stmt
	selectConnectionByKeyStmt            *sql.Stmt
	selectConnectionByIDsStmt            *sql.Stmt
	deleteConnectionStmt                 *sql.Stmt
	deleteOldConnectionsStmt             *sql.Stmt
	insertConnectionPositionStmt         *sql.Stmt
	selectConnectionPositionStmt         *sql.Stmt
	selectLatestConnectionPositionStmt   *sql.Stmt
	insertRequiredStateStmt              *sql.Stmt
	selectRequiredStateStmt              *sql.Stmt
	selectRequiredStateByContentStmt     *sql.Stmt
	upsertRoomConfigStmt                  *sql.Stmt
	selectRoomConfigStmt                  *sql.Stmt
	selectLatestRoomConfigStmt            *sql.Stmt
	selectRoomConfigsByPositionStmt       *sql.Stmt
	upsertConnectionStreamStmt            *sql.Stmt
	selectConnectionStreamStmt            *sql.Stmt
	selectLatestConnectionStreamStmt      *sql.Stmt
	selectAllLatestConnectionStreamsStmt  *sql.Stmt
	selectConnectionStreamsByPositionStmt *sql.Stmt
	deleteOtherConnectionPositionsStmt    *sql.Stmt
	upsertConnectionListStmt              *sql.Stmt
	selectConnectionListStmt              *sql.Stmt
}

func NewSqliteSlidingSyncTable(db *sql.DB) (tables.SlidingSync, error) {
	s := &slidingSyncStatements{db: db}
	return s, sqlutil.StatementList{
		{&s.insertConnectionStmt, insertConnectionSQL},
		{&s.selectConnectionByKeyStmt, selectConnectionByKeySQL},
		{&s.selectConnectionByIDsStmt, selectConnectionByIDsSQL},
		{&s.deleteConnectionStmt, deleteConnectionSQL},
		{&s.deleteOldConnectionsStmt, deleteOldConnectionsSQL},
		{&s.insertConnectionPositionStmt, insertConnectionPositionSQL},
		{&s.selectConnectionPositionStmt, selectConnectionPositionSQL},
		{&s.selectLatestConnectionPositionStmt, selectLatestConnectionPositionSQL},
		{&s.insertRequiredStateStmt, insertRequiredStateSQL},
		{&s.selectRequiredStateStmt, selectRequiredStateSQL},
		{&s.selectRequiredStateByContentStmt, selectRequiredStateByContentSQL},
		{&s.upsertRoomConfigStmt, upsertRoomConfigSQL},
		{&s.selectRoomConfigStmt, selectRoomConfigSQL},
		{&s.selectLatestRoomConfigStmt, selectLatestRoomConfigSQL},
		{&s.selectRoomConfigsByPositionStmt, selectRoomConfigsByPositionSQL},
		{&s.upsertConnectionStreamStmt, upsertConnectionStreamSQL},
		{&s.selectConnectionStreamStmt, selectConnectionStreamSQL},
		{&s.selectLatestConnectionStreamStmt, selectLatestConnectionStreamSQL},
		{&s.selectAllLatestConnectionStreamsStmt, selectAllLatestConnectionStreamsSQL},
		{&s.selectConnectionStreamsByPositionStmt, selectConnectionStreamsByPositionSQL},
		{&s.deleteOtherConnectionPositionsStmt, deleteOtherConnectionPositionsSQL},
		{&s.upsertConnectionListStmt, upsertConnectionListSQL},
		{&s.selectConnectionListStmt, selectConnectionListSQL},
	}.Prepare(db)
}

// ===== Connection Management =====

func (s *slidingSyncStatements) InsertConnection(
	ctx context.Context, txn *sql.Tx, userID, deviceID, connID string, createdTS int64,
) (int64, error) {
	stmt := sqlutil.TxStmt(txn, s.insertConnectionStmt)
	var connectionKey int64
	err := stmt.QueryRowContext(ctx, userID, deviceID, connID, createdTS).Scan(&connectionKey)
	return connectionKey, err
}

func (s *slidingSyncStatements) SelectConnectionByKey(
	ctx context.Context, txn *sql.Tx, connectionKey int64,
) (*tables.SlidingSyncConnection, error) {
	stmt := sqlutil.TxStmt(txn, s.selectConnectionByKeyStmt)
	var conn tables.SlidingSyncConnection
	err := stmt.QueryRowContext(ctx, connectionKey).Scan(
		&conn.ConnectionKey, &conn.UserID, &conn.DeviceID, &conn.ConnID, &conn.CreatedTS,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &conn, err
}

func (s *slidingSyncStatements) SelectConnectionByIDs(
	ctx context.Context, txn *sql.Tx, userID, deviceID, connID string,
) (*tables.SlidingSyncConnection, error) {
	stmt := sqlutil.TxStmt(txn, s.selectConnectionByIDsStmt)
	var conn tables.SlidingSyncConnection
	err := stmt.QueryRowContext(ctx, userID, deviceID, connID).Scan(
		&conn.ConnectionKey, &conn.UserID, &conn.DeviceID, &conn.ConnID, &conn.CreatedTS,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &conn, err
}

func (s *slidingSyncStatements) DeleteConnection(
	ctx context.Context, txn *sql.Tx, connectionKey int64,
) error {
	stmt := sqlutil.TxStmt(txn, s.deleteConnectionStmt)
	_, err := stmt.ExecContext(ctx, connectionKey)
	return err
}

func (s *slidingSyncStatements) DeleteOldConnections(
	ctx context.Context, txn *sql.Tx, olderThanTS int64,
) error {
	stmt := sqlutil.TxStmt(txn, s.deleteOldConnectionsStmt)
	_, err := stmt.ExecContext(ctx, olderThanTS)
	return err
}

// ===== Position Management =====

func (s *slidingSyncStatements) InsertConnectionPosition(
	ctx context.Context, txn *sql.Tx, connectionKey int64, createdTS int64,
) (int64, error) {
	stmt := sqlutil.TxStmt(txn, s.insertConnectionPositionStmt)
	var connectionPosition int64
	err := stmt.QueryRowContext(ctx, connectionKey, createdTS).Scan(&connectionPosition)
	return connectionPosition, err
}

func (s *slidingSyncStatements) SelectConnectionPosition(
	ctx context.Context, txn *sql.Tx, connectionPosition int64,
) (*tables.SlidingSyncConnectionPosition, error) {
	stmt := sqlutil.TxStmt(txn, s.selectConnectionPositionStmt)
	var pos tables.SlidingSyncConnectionPosition
	err := stmt.QueryRowContext(ctx, connectionPosition).Scan(
		&pos.ConnectionPosition, &pos.ConnectionKey, &pos.CreatedTS,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &pos, err
}

func (s *slidingSyncStatements) SelectLatestConnectionPosition(
	ctx context.Context, txn *sql.Tx, connectionKey int64,
) (*tables.SlidingSyncConnectionPosition, error) {
	stmt := sqlutil.TxStmt(txn, s.selectLatestConnectionPositionStmt)
	var pos tables.SlidingSyncConnectionPosition
	err := stmt.QueryRowContext(ctx, connectionKey).Scan(
		&pos.ConnectionPosition, &pos.ConnectionKey, &pos.CreatedTS,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &pos, err
}

// ===== Required State Management =====

func (s *slidingSyncStatements) InsertRequiredState(
	ctx context.Context, txn *sql.Tx, connectionKey int64, requiredState string,
) (int64, error) {
	stmt := sqlutil.TxStmt(txn, s.insertRequiredStateStmt)
	var requiredStateID int64
	err := stmt.QueryRowContext(ctx, connectionKey, requiredState).Scan(&requiredStateID)
	return requiredStateID, err
}

func (s *slidingSyncStatements) SelectRequiredState(
	ctx context.Context, txn *sql.Tx, requiredStateID int64,
) (string, error) {
	stmt := sqlutil.TxStmt(txn, s.selectRequiredStateStmt)
	var requiredState string
	err := stmt.QueryRowContext(ctx, requiredStateID).Scan(&requiredState)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return requiredState, err
}

func (s *slidingSyncStatements) SelectRequiredStateByContent(
	ctx context.Context, txn *sql.Tx, connectionKey int64, requiredState string,
) (int64, bool, error) {
	stmt := sqlutil.TxStmt(txn, s.selectRequiredStateByContentStmt)
	var requiredStateID int64
	err := stmt.QueryRowContext(ctx, connectionKey, requiredState).Scan(&requiredStateID)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return requiredStateID, true, nil
}

// ===== Room Config Management =====

func (s *slidingSyncStatements) UpsertRoomConfig(
	ctx context.Context, txn *sql.Tx, connectionPosition int64, roomID string, timelineLimit int, requiredStateID int64,
) error {
	stmt := sqlutil.TxStmt(txn, s.upsertRoomConfigStmt)
	_, err := stmt.ExecContext(ctx, connectionPosition, roomID, timelineLimit, requiredStateID)
	return err
}

func (s *slidingSyncStatements) SelectRoomConfig(
	ctx context.Context, txn *sql.Tx, connectionPosition int64, roomID string,
) (*tables.SlidingSyncRoomConfig, error) {
	stmt := sqlutil.TxStmt(txn, s.selectRoomConfigStmt)
	var config tables.SlidingSyncRoomConfig
	err := stmt.QueryRowContext(ctx, connectionPosition, roomID).Scan(
		&config.ConnectionPosition, &config.RoomID, &config.TimelineLimit, &config.RequiredStateID,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &config, err
}

func (s *slidingSyncStatements) SelectLatestRoomConfig(
	ctx context.Context, txn *sql.Tx, connectionKey int64, roomID string,
) (*tables.SlidingSyncRoomConfig, error) {
	stmt := sqlutil.TxStmt(txn, s.selectLatestRoomConfigStmt)
	var config tables.SlidingSyncRoomConfig
	err := stmt.QueryRowContext(ctx, connectionKey, roomID).Scan(
		&config.ConnectionPosition, &config.RoomID, &config.TimelineLimit, &config.RequiredStateID,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &config, err
}

// SelectLatestRoomConfigsBatch retrieves the most recent room configs for multiple rooms
// For SQLite, we iterate through each room since SQLite doesn't support DISTINCT ON
// This is acceptable for SQLite deployments which are typically smaller scale
func (s *slidingSyncStatements) SelectLatestRoomConfigsBatch(
	ctx context.Context, txn *sql.Tx, connectionKey int64, roomIDs []string,
) (map[string]*tables.SlidingSyncRoomConfig, error) {
	if len(roomIDs) == 0 {
		return make(map[string]*tables.SlidingSyncRoomConfig), nil
	}

	result := make(map[string]*tables.SlidingSyncRoomConfig, len(roomIDs))
	for _, roomID := range roomIDs {
		config, err := s.SelectLatestRoomConfig(ctx, txn, connectionKey, roomID)
		if err != nil {
			return nil, err
		}
		if config != nil {
			result[roomID] = config
		}
	}
	return result, nil
}

// SelectRoomConfigsByPosition retrieves all room configs for a specific position
// Used to load previous room configs for copy-forward during sync
func (s *slidingSyncStatements) SelectRoomConfigsByPosition(
	ctx context.Context, txn *sql.Tx, connectionPosition int64,
) (map[string]*tables.SlidingSyncRoomConfig, error) {
	stmt := sqlutil.TxStmt(txn, s.selectRoomConfigsByPositionStmt)
	rows, err := stmt.QueryContext(ctx, connectionPosition)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*tables.SlidingSyncRoomConfig)
	for rows.Next() {
		var config tables.SlidingSyncRoomConfig
		if err := rows.Scan(
			&config.ConnectionPosition, &config.RoomID, &config.TimelineLimit, &config.RequiredStateID,
		); err != nil {
			return nil, err
		}
		result[config.RoomID] = &config
	}
	return result, rows.Err()
}

// ===== Stream Management =====

func (s *slidingSyncStatements) UpsertConnectionStream(
	ctx context.Context, txn *sql.Tx, connectionPosition int64, roomID, stream, roomStatus, lastToken string,
) error {
	stmt := sqlutil.TxStmt(txn, s.upsertConnectionStreamStmt)
	_, err := stmt.ExecContext(ctx, connectionPosition, roomID, stream, roomStatus, lastToken)
	return err
}

func (s *slidingSyncStatements) SelectConnectionStream(
	ctx context.Context, txn *sql.Tx, connectionPosition int64, roomID, stream string,
) (*tables.SlidingSyncConnectionStream, error) {
	stmt := sqlutil.TxStmt(txn, s.selectConnectionStreamStmt)
	var streamData tables.SlidingSyncConnectionStream
	err := stmt.QueryRowContext(ctx, connectionPosition, roomID, stream).Scan(
		&streamData.ConnectionPosition, &streamData.RoomID, &streamData.Stream,
		&streamData.RoomStatus, &streamData.LastToken,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &streamData, err
}

func (s *slidingSyncStatements) SelectLatestConnectionStream(
	ctx context.Context, txn *sql.Tx, connectionKey int64, roomID, stream string,
) (*tables.SlidingSyncConnectionStream, error) {
	stmt := sqlutil.TxStmt(txn, s.selectLatestConnectionStreamStmt)
	var streamData tables.SlidingSyncConnectionStream
	err := stmt.QueryRowContext(ctx, connectionKey, roomID, stream).Scan(
		&streamData.ConnectionPosition, &streamData.RoomID, &streamData.Stream,
		&streamData.RoomStatus, &streamData.LastToken,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &streamData, err
}

func (s *slidingSyncStatements) SelectAllLatestConnectionStreams(
	ctx context.Context, txn *sql.Tx, connectionKey int64,
) (map[string]map[string]*tables.SlidingSyncConnectionStream, error) {
	stmt := sqlutil.TxStmt(txn, s.selectAllLatestConnectionStreamsStmt)
	rows, err := stmt.QueryContext(ctx, connectionKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]map[string]*tables.SlidingSyncConnectionStream)
	for rows.Next() {
		var streamData tables.SlidingSyncConnectionStream
		if err := rows.Scan(
			&streamData.RoomID, &streamData.Stream, &streamData.RoomStatus,
			&streamData.LastToken, &streamData.ConnectionPosition,
		); err != nil {
			return nil, err
		}

		if result[streamData.RoomID] == nil {
			result[streamData.RoomID] = make(map[string]*tables.SlidingSyncConnectionStream)
		}
		result[streamData.RoomID][streamData.Stream] = &streamData
	}
	return result, rows.Err()
}

// SelectConnectionStreamsByPosition retrieves all streams for a specific position
// This is used for incremental syncs to get the state as it was at that exact position
func (s *slidingSyncStatements) SelectConnectionStreamsByPosition(
	ctx context.Context, txn *sql.Tx, connectionPosition int64,
) (map[string]map[string]*tables.SlidingSyncConnectionStream, error) {
	stmt := sqlutil.TxStmt(txn, s.selectConnectionStreamsByPositionStmt)
	rows, err := stmt.QueryContext(ctx, connectionPosition)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]map[string]*tables.SlidingSyncConnectionStream)
	for rows.Next() {
		var streamData tables.SlidingSyncConnectionStream
		if err := rows.Scan(
			&streamData.RoomID, &streamData.Stream, &streamData.RoomStatus,
			&streamData.LastToken, &streamData.ConnectionPosition,
		); err != nil {
			return nil, err
		}

		if result[streamData.RoomID] == nil {
			result[streamData.RoomID] = make(map[string]*tables.SlidingSyncConnectionStream)
		}
		result[streamData.RoomID][streamData.Stream] = &streamData
	}
	return result, rows.Err()
}

// DeleteOtherConnectionPositions removes all positions for a connection except the specified one
// This is called when a client uses a position token, to clean up old state (like Synapse does)
func (s *slidingSyncStatements) DeleteOtherConnectionPositions(
	ctx context.Context, txn *sql.Tx, connectionKey int64, keepPosition int64,
) error {
	stmt := sqlutil.TxStmt(txn, s.deleteOtherConnectionPositionsStmt)
	_, err := stmt.ExecContext(ctx, connectionKey, keepPosition)
	return err
}

// ===== List Management =====

func (s *slidingSyncStatements) UpsertConnectionList(
	ctx context.Context, txn *sql.Tx, connectionKey int64, listName string, roomIDsJSON string,
) error {
	stmt := sqlutil.TxStmt(txn, s.upsertConnectionListStmt)
	_, err := stmt.ExecContext(ctx, connectionKey, listName, roomIDsJSON)
	return err
}

func (s *slidingSyncStatements) SelectConnectionList(
	ctx context.Context, txn *sql.Tx, connectionKey int64, listName string,
) (string, bool, error) {
	stmt := sqlutil.TxStmt(txn, s.selectConnectionListStmt)
	var roomIDsJSON string
	err := stmt.QueryRowContext(ctx, connectionKey, listName).Scan(&roomIDsJSON)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return roomIDsJSON, true, nil
}

// Ensure we implement the interface
var _ tables.SlidingSync = &slidingSyncStatements{}
