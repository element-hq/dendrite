// Copyright 2024 New Vector Ltd.
// Copyright 2020 The Matrix.org Foundation C.I.C.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lib/pq"

	"github.com/element-hq/dendrite/internal"
	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/syncapi/storage/postgres/deltas"
	"github.com/element-hq/dendrite/syncapi/storage/tables"
	"github.com/element-hq/dendrite/syncapi/types"
	"github.com/matrix-org/gomatrixserverlib/spec"
)

const receiptsSchema = `
CREATE SEQUENCE IF NOT EXISTS syncapi_receipt_id;

-- Stores data about receipts
CREATE TABLE IF NOT EXISTS syncapi_receipts (
	-- The ID
	id BIGINT PRIMARY KEY DEFAULT nextval('syncapi_receipt_id'),
	room_id TEXT NOT NULL,
	receipt_type TEXT NOT NULL,
	user_id TEXT NOT NULL,
	event_id TEXT NOT NULL,
	receipt_ts BIGINT NOT NULL,
	CONSTRAINT syncapi_receipts_unique UNIQUE (room_id, receipt_type, user_id)
);
CREATE INDEX IF NOT EXISTS syncapi_receipts_room_id ON syncapi_receipts(room_id);
`

const upsertReceipt = "" +
	"INSERT INTO syncapi_receipts" +
	" (room_id, receipt_type, user_id, event_id, receipt_ts)" +
	" VALUES ($1, $2, $3, $4, $5)" +
	" ON CONFLICT (room_id, receipt_type, user_id)" +
	" DO UPDATE SET id = CASE" +
	"   WHEN syncapi_receipts.event_id != EXCLUDED.event_id THEN nextval('syncapi_receipt_id')" +
	"   ELSE syncapi_receipts.id" +
	" END, event_id = EXCLUDED.event_id, receipt_ts = EXCLUDED.receipt_ts" +
	" RETURNING id"

const selectRoomReceipts = "" +
	"SELECT id, room_id, receipt_type, user_id, event_id, receipt_ts" +
	" FROM syncapi_receipts" +
	" WHERE room_id = ANY($1) AND id > $2"

const selectMaxReceiptIDSQL = "" +
	"SELECT MAX(id) FROM syncapi_receipts"

const purgeReceiptsSQL = "" +
	"DELETE FROM syncapi_receipts WHERE room_id = $1"

// New queries for per-connection receipt tracking (MSC4186 sliding sync)
const selectLatestUserReceiptsSQL = "" +
	"SELECT DISTINCT ON (room_id, receipt_type, user_id) " +
	"id, room_id, receipt_type, user_id, event_id, receipt_ts " +
	"FROM syncapi_receipts " +
	"WHERE room_id = ANY($1) " +
	"ORDER BY room_id, receipt_type, user_id, id DESC"

const selectConnectionReceiptsSQL = "" +
	"SELECT room_id, receipt_type, user_id, last_delivered_event_id, last_delivered_ts " +
	"FROM syncapi_sliding_sync_connection_receipts " +
	"WHERE connection_key = $1"

const upsertConnectionReceiptSQL = "" +
	"INSERT INTO syncapi_sliding_sync_connection_receipts " +
	"(connection_key, room_id, receipt_type, user_id, last_delivered_event_id, last_delivered_ts) " +
	"VALUES ($1, $2, $3, $4, $5, $6) " +
	"ON CONFLICT (connection_key, room_id, receipt_type, user_id) " +
	"DO UPDATE SET last_delivered_event_id = $5, last_delivered_ts = $6"

const deleteConnectionReceiptsSQL = "" +
	"DELETE FROM syncapi_sliding_sync_connection_receipts WHERE connection_key = $1"

type receiptStatements struct {
	db                           *sql.DB
	upsertReceipt                *sql.Stmt
	selectRoomReceipts           *sql.Stmt
	selectMaxReceiptID           *sql.Stmt
	purgeReceiptsStmt            *sql.Stmt
	// New statements for per-connection tracking
	selectLatestUserReceipts     *sql.Stmt
	selectConnectionReceipts     *sql.Stmt
	upsertConnectionReceipt      *sql.Stmt
	deleteConnectionReceipts     *sql.Stmt
}

func NewPostgresReceiptsTable(db *sql.DB) (tables.Receipts, error) {
	_, err := db.Exec(receiptsSchema)
	if err != nil {
		return nil, err
	}
	m := sqlutil.NewMigrator(db)
	m.AddMigrations(
		sqlutil.Migration{
			Version: "syncapi: fix sequences",
			Up:      deltas.UpFixSequences,
		},
		sqlutil.Migration{
			Version: "syncapi: create sliding sync tables",
			Up:      deltas.UpCreateSlidingSyncTables,
		},
		sqlutil.Migration{
			Version: "syncapi: add connection receipts table for sliding sync",
			Up:      deltas.UpAddConnectionReceipts,
		},
	)
	err = m.Up(context.Background())
	if err != nil {
		return nil, err
	}
	r := &receiptStatements{
		db: db,
	}
	return r, sqlutil.StatementList{
		{&r.upsertReceipt, upsertReceipt},
		{&r.selectRoomReceipts, selectRoomReceipts},
		{&r.selectMaxReceiptID, selectMaxReceiptIDSQL},
		{&r.purgeReceiptsStmt, purgeReceiptsSQL},
		{&r.selectLatestUserReceipts, selectLatestUserReceiptsSQL},
		{&r.selectConnectionReceipts, selectConnectionReceiptsSQL},
		{&r.upsertConnectionReceipt, upsertConnectionReceiptSQL},
		{&r.deleteConnectionReceipts, deleteConnectionReceiptsSQL},
	}.Prepare(db)
}

func (r *receiptStatements) UpsertReceipt(ctx context.Context, txn *sql.Tx, roomId, receiptType, userId, eventId string, timestamp spec.Timestamp) (pos types.StreamPosition, err error) {
	stmt := sqlutil.TxStmt(txn, r.upsertReceipt)
	err = stmt.QueryRowContext(ctx, roomId, receiptType, userId, eventId, timestamp).Scan(&pos)
	return
}

func (r *receiptStatements) SelectRoomReceiptsAfter(ctx context.Context, txn *sql.Tx, roomIDs []string, streamPos types.StreamPosition) (types.StreamPosition, []types.OutputReceiptEvent, error) {
	var lastPos types.StreamPosition
	rows, err := sqlutil.TxStmt(txn, r.selectRoomReceipts).QueryContext(ctx, pq.Array(roomIDs), streamPos)
	if err != nil {
		return 0, nil, fmt.Errorf("unable to query room receipts: %w", err)
	}
	defer internal.CloseAndLogIfError(ctx, rows, "SelectRoomReceiptsAfter: rows.close() failed")
	var res []types.OutputReceiptEvent
	for rows.Next() {
		r := types.OutputReceiptEvent{}
		var id types.StreamPosition
		err = rows.Scan(&id, &r.RoomID, &r.Type, &r.UserID, &r.EventID, &r.Timestamp)
		if err != nil {
			return 0, res, fmt.Errorf("unable to scan row to api.Receipts: %w", err)
		}
		res = append(res, r)
		if id > lastPos {
			lastPos = id
		}
	}
	return lastPos, res, rows.Err()
}

func (s *receiptStatements) SelectMaxReceiptID(
	ctx context.Context, txn *sql.Tx,
) (id int64, err error) {
	var nullableID sql.NullInt64
	stmt := sqlutil.TxStmt(txn, s.selectMaxReceiptID)
	err = stmt.QueryRowContext(ctx).Scan(&nullableID)
	if nullableID.Valid {
		id = nullableID.Int64
	}
	return
}

func (s *receiptStatements) PurgeReceipts(
	ctx context.Context, txn *sql.Tx, roomID string,
) error {
	_, err := sqlutil.TxStmt(txn, s.purgeReceiptsStmt).ExecContext(ctx, roomID)
	return err
}

// SelectLatestUserReceiptsForConnection returns receipts that have changed since last delivered
// to this connection. Uses event-ID based comparison instead of position tracking.
//
// IMPORTANT: This query is designed to solve the concurrent connection receipt repetition problem
// by tracking what was delivered to EACH connection separately.
//
// Returns receipts for ALL users in the room (not just the requesting user) to support:
// - Read receipts UI (showing where other users have read to)
// - Client-side notification badge logic
//
// Private receipts are filtered in v4_extensions.go, not here.
//
// Algorithm:
// 1. Get latest receipt for ALL users in each room (from syncapi_receipts)
// 2. Get last delivered receipts for this connection (from syncapi_sliding_sync_connection_receipts)
// 3. Compare event_ids - only return receipts where event_id has changed
// 4. Update connection state after delivery (caller's responsibility)
func (s *receiptStatements) SelectLatestUserReceiptsForConnection(
	ctx context.Context,
	txn *sql.Tx,
	connectionKey int64,
	roomIDs []string,
	userID string,
) ([]types.OutputReceiptEvent, error) {
	if len(roomIDs) == 0 {
		return []types.OutputReceiptEvent{}, nil
	}

	// Step 1: Get latest receipts for ALL users in these rooms
	// Note: Private receipt filtering happens in v4_extensions.go, not here
	latestRows, err := sqlutil.TxStmt(txn, s.selectLatestUserReceipts).QueryContext(ctx, pq.Array(roomIDs))
	if err != nil {
		return nil, fmt.Errorf("failed to query latest receipts: %w", err)
	}
	defer internal.CloseAndLogIfError(ctx, latestRows, "SelectLatestUserReceiptsForConnection: latestRows.close() failed")

	latestReceipts := make(map[string]types.OutputReceiptEvent) // key: room_id|receipt_type|user_id
	for latestRows.Next() {
		var r types.OutputReceiptEvent
		var id types.StreamPosition
		err = latestRows.Scan(&id, &r.RoomID, &r.Type, &r.UserID, &r.EventID, &r.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to scan latest receipt: %w", err)
		}
		key := fmt.Sprintf("%s|%s|%s", r.RoomID, r.Type, r.UserID)
		latestReceipts[key] = r
	}

	// Step 2: Get what we last delivered to this connection
	deliveredRows, err := sqlutil.TxStmt(txn, s.selectConnectionReceipts).QueryContext(ctx, connectionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to query connection receipts: %w", err)
	}
	defer internal.CloseAndLogIfError(ctx, deliveredRows, "SelectLatestUserReceiptsForConnection: deliveredRows.close() failed")

	lastDelivered := make(map[string]string) // key: room_id|receipt_type|user_id -> event_id
	for deliveredRows.Next() {
		var roomID, receiptType, userID, eventID string
		var ts spec.Timestamp
		err = deliveredRows.Scan(&roomID, &receiptType, &userID, &eventID, &ts)
		if err != nil {
			return nil, fmt.Errorf("failed to scan connection receipt: %w", err)
		}
		key := fmt.Sprintf("%s|%s|%s", roomID, receiptType, userID)
		lastDelivered[key] = eventID
	}

	// Step 3: Compare and return only changed receipts
	var result []types.OutputReceiptEvent
	for key, latest := range latestReceipts {
		lastEventID, exists := lastDelivered[key]
		// Return if: (1) never delivered before, OR (2) event_id has changed
		if !exists || lastEventID != latest.EventID {
			result = append(result, latest)
		}
	}

	return result, nil
}

// UpsertConnectionReceipt updates the last delivered receipt for a connection
func (s *receiptStatements) UpsertConnectionReceipt(
	ctx context.Context,
	txn *sql.Tx,
	connectionKey int64,
	roomID, receiptType, userID, eventID string,
	timestamp spec.Timestamp,
) error {
	_, err := sqlutil.TxStmt(txn, s.upsertConnectionReceipt).ExecContext(
		ctx, connectionKey, roomID, receiptType, userID, eventID, timestamp,
	)
	return err
}

// DeleteConnectionReceipts removes all delivered receipt state for a connection.
// This should be called on fresh sync (no pos token) to ensure receipts are re-delivered.
func (s *receiptStatements) DeleteConnectionReceipts(
	ctx context.Context,
	txn *sql.Tx,
	connectionKey int64,
) error {
	_, err := sqlutil.TxStmt(txn, s.deleteConnectionReceipts).ExecContext(ctx, connectionKey)
	return err
}
