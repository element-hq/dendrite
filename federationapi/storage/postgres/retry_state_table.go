// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package postgres

import (
	"context"
	"database/sql"

	"github.com/element-hq/dendrite/federationapi/types"
	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/matrix-org/gomatrixserverlib/spec"
)

const retryStateSchema = `
CREATE TABLE IF NOT EXISTS federationsender_retry_state (
    -- The server name being tracked
    server_name TEXT NOT NULL PRIMARY KEY,
    -- Number of consecutive failures
    failure_count INTEGER NOT NULL DEFAULT 0,
    -- Timestamp (ms since epoch) when the backoff expires
    retry_until BIGINT NOT NULL DEFAULT 0
);
`

const upsertRetryStateSQL = "" +
	"INSERT INTO federationsender_retry_state (server_name, failure_count, retry_until) VALUES ($1, $2, $3)" +
	" ON CONFLICT (server_name) DO UPDATE SET failure_count = $2, retry_until = $3"

const selectRetryStateSQL = "" +
	"SELECT failure_count, retry_until FROM federationsender_retry_state WHERE server_name = $1"

const selectAllRetryStatesSQL = "" +
	"SELECT server_name, failure_count, retry_until FROM federationsender_retry_state"

const deleteRetryStateSQL = "" +
	"DELETE FROM federationsender_retry_state WHERE server_name = $1"

type retryStateStatements struct {
	db                       *sql.DB
	upsertRetryStateStmt     *sql.Stmt
	selectRetryStateStmt     *sql.Stmt
	selectAllRetryStatesStmt *sql.Stmt
	deleteRetryStateStmt     *sql.Stmt
}

func NewPostgresRetryStateTable(db *sql.DB) (s *retryStateStatements, err error) {
	s = &retryStateStatements{
		db: db,
	}
	_, err = db.Exec(retryStateSchema)
	if err != nil {
		return
	}

	return s, sqlutil.StatementList{
		{&s.upsertRetryStateStmt, upsertRetryStateSQL},
		{&s.selectRetryStateStmt, selectRetryStateSQL},
		{&s.selectAllRetryStatesStmt, selectAllRetryStatesSQL},
		{&s.deleteRetryStateStmt, deleteRetryStateSQL},
	}.Prepare(db)
}

func (s *retryStateStatements) UpsertRetryState(
	ctx context.Context, txn *sql.Tx, serverName spec.ServerName, failureCount uint32, retryUntil spec.Timestamp,
) error {
	stmt := sqlutil.TxStmt(txn, s.upsertRetryStateStmt)
	_, err := stmt.ExecContext(ctx, serverName, failureCount, retryUntil)
	return err
}

func (s *retryStateStatements) SelectRetryState(
	ctx context.Context, txn *sql.Tx, serverName spec.ServerName,
) (failureCount uint32, retryUntil spec.Timestamp, exists bool, err error) {
	stmt := sqlutil.TxStmt(txn, s.selectRetryStateStmt)
	err = stmt.QueryRowContext(ctx, serverName).Scan(&failureCount, &retryUntil)
	if err == sql.ErrNoRows {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	return failureCount, retryUntil, true, nil
}

func (s *retryStateStatements) SelectAllRetryStates(
	ctx context.Context, txn *sql.Tx,
) (map[spec.ServerName]types.RetryState, error) {
	stmt := sqlutil.TxStmt(txn, s.selectAllRetryStatesStmt)
	rows, err := stmt.QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // nolint:errcheck

	result := make(map[spec.ServerName]types.RetryState)
	for rows.Next() {
		var serverName spec.ServerName
		var failureCount uint32
		var retryUntil spec.Timestamp
		if err = rows.Scan(&serverName, &failureCount, &retryUntil); err != nil {
			return nil, err
		}
		result[serverName] = types.RetryState{
			FailureCount: failureCount,
			RetryUntil:   retryUntil,
		}
	}
	return result, rows.Err()
}

func (s *retryStateStatements) DeleteRetryState(
	ctx context.Context, txn *sql.Tx, serverName spec.ServerName,
) error {
	stmt := sqlutil.TxStmt(txn, s.deleteRetryStateStmt)
	_, err := stmt.ExecContext(ctx, serverName)
	return err
}
