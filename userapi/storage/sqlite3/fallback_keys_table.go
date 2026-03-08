// Copyright 2024 New Vector Ltd.
// Copyright 2017 Vector Creations Ltd
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package sqlite3

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/element-hq/dendrite/internal"
	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/userapi/api"
	"github.com/element-hq/dendrite/userapi/storage/tables"
)

var fallbackKeysSchema = `
-- Stores one-time public keys for users
CREATE TABLE IF NOT EXISTS keyserver_fallback_keys (
    user_id TEXT NOT NULL,
	device_id TEXT NOT NULL,
	key_id TEXT NOT NULL,
	algorithm TEXT NOT NULL,
	ts_added_secs BIGINT NOT NULL,
	key_json TEXT NOT NULL,
	used BOOLEAN NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS keyserver_fallback_keys_unique_idx ON keyserver_fallback_keys(user_id, device_id, algorithm);
CREATE INDEX IF NOT EXISTS keyserver_fallback_keys_idx ON keyserver_fallback_keys (user_id, device_id);
`

const upsertFallbackKeysSQL = "" +
	"INSERT INTO keyserver_fallback_keys (user_id, device_id, key_id, algorithm, ts_added_secs, key_json, used)" +
	" VALUES ($1, $2, $3, $4, $5, $6, false)" +
	" ON CONFLICT (user_id, device_id, algorithm)" +
	" DO UPDATE SET key_id = $3, key_json = $6, used = false"

const selectFallbackUnusedAlgorithmsSQL = "" +
	"SELECT algorithm FROM keyserver_fallback_keys WHERE user_id = $1 AND device_id = $2 AND used = false"

const selectFallbackKeysByAlgorithmSQL = "" +
	"SELECT key_id, key_json FROM keyserver_fallback_keys WHERE user_id = $1 AND device_id = $2 AND algorithm = $3 ORDER BY used ASC LIMIT 1"

const deleteFallbackKeysSQL = "" +
	"DELETE FROM keyserver_fallback_keys WHERE user_id = $1 AND device_id = $2"

const updateFallbackKeyUsedSQL = "" +
	"UPDATE keyserver_fallback_keys SET used=true WHERE user_id = $1 AND device_id = $2 AND key_id = $3 AND algorithm = $4"

type fallbackKeysStatements struct {
	db                         *sql.DB
	upsertKeysStmt             *sql.Stmt
	selectUnusedAlgorithmsStmt *sql.Stmt
	selectKeyByAlgorithmStmt   *sql.Stmt
	deleteFallbackKeysStmt     *sql.Stmt
	updateFallbackKeyUsedStmt  *sql.Stmt
}

func NewSqliteFallbackKeysTable(db *sql.DB) (tables.FallbackKeys, error) {
	s := &fallbackKeysStatements{
		db: db,
	}
	_, err := db.Exec(fallbackKeysSchema)
	if err != nil {
		return nil, err
	}
	return s, sqlutil.StatementList{
		{&s.upsertKeysStmt, upsertFallbackKeysSQL},
		{&s.selectUnusedAlgorithmsStmt, selectFallbackUnusedAlgorithmsSQL},
		{&s.selectKeyByAlgorithmStmt, selectFallbackKeysByAlgorithmSQL},
		{&s.deleteFallbackKeysStmt, deleteFallbackKeysSQL},
		{&s.updateFallbackKeyUsedStmt, updateFallbackKeyUsedSQL},
	}.Prepare(db)
}

func (s *fallbackKeysStatements) SelectUnusedFallbackKeyAlgorithms(ctx context.Context, userID, deviceID string) ([]string, error) {
	rows, err := s.selectUnusedAlgorithmsStmt.QueryContext(ctx, userID, deviceID)
	if err != nil {
		return nil, err
	}
	defer internal.CloseAndLogIfError(ctx, rows, "selectKeysCountStmt: rows.close() failed")
	algos := []string{}
	for rows.Next() {
		var algorithm string
		if err = rows.Scan(&algorithm); err != nil {
			return nil, err
		}
		algos = append(algos, algorithm)
	}
	return algos, rows.Err()
}

func (s *fallbackKeysStatements) InsertFallbackKeys(ctx context.Context, txn *sql.Tx, keys api.FallbackKeys) ([]string, error) {
	now := time.Now().Unix()
	for keyIDWithAlgo, keyJSON := range keys.KeyJSON {
		algo, keyID := keys.Split(keyIDWithAlgo)
		_, err := sqlutil.TxStmt(txn, s.upsertKeysStmt).ExecContext(
			ctx, keys.UserID, keys.DeviceID, keyID, algo, now, string(keyJSON),
		)
		if err != nil {
			return nil, err
		}
	}
	return s.SelectUnusedFallbackKeyAlgorithms(ctx, keys.UserID, keys.DeviceID)
}

func (s *fallbackKeysStatements) DeleteFallbackKeys(ctx context.Context, txn *sql.Tx, userID, deviceID string) error {
	_, err := sqlutil.TxStmt(txn, s.deleteFallbackKeysStmt).ExecContext(ctx, userID, deviceID)
	return err
}

func (s *fallbackKeysStatements) SelectAndUpdateFallbackKey(
	ctx context.Context, txn *sql.Tx, userID, deviceID, algorithm string,
) (map[string]json.RawMessage, error) {
	var keyID string
	var keyJSON string
	err := sqlutil.TxStmtContext(ctx, txn, s.selectKeyByAlgorithmStmt).QueryRowContext(ctx, userID, deviceID, algorithm).Scan(&keyID, &keyJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_, err = sqlutil.TxStmtContext(ctx, txn, s.updateFallbackKeyUsedStmt).ExecContext(ctx, userID, deviceID, algorithm, keyID)
	return map[string]json.RawMessage{
		algorithm + ":" + keyID: json.RawMessage(keyJSON),
	}, err
}
