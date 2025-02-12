// Copyright 2025 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package sqlite3

import (
	"context"
	"database/sql"
	"time"

	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/userapi/api"
	"github.com/element-hq/dendrite/userapi/storage/tables"
	log "github.com/sirupsen/logrus"
)

const localpartExternalIDsSchema = `
-- Stores data about connections between accounts and third-party auth providers
CREATE TABLE IF NOT EXISTS userapi_localpart_external_ids (
    -- The Matrix user ID for this account
	localpart TEXT NOT NULL,
    -- The external ID
	external_id TEXT NOT NULL,
    -- Auth provider ID (see OIDCProvider.IDPID)
	auth_provider TEXT NOT NULL,
    -- When this connection was created, as a unix timestamp.
    created_ts BIGINT NOT NULL,

    UNIQUE(external_id, auth_provider),
    UNIQUE(localpart, external_id, auth_provider)
);

-- This index allows efficient lookup of the local user by the external ID
CREATE INDEX IF NOT EXISTS userapi_external_id_auth_provider_idx ON userapi_localpart_external_ids(external_id, auth_provider);
`

const insertLocalpartExternalIDSQL = "" +
	"INSERT INTO userapi_localpart_external_ids(localpart, external_id, auth_provider, created_ts) VALUES ($1, $2, $3, $4)"

const selectLocalpartExternalIDSQL = "" +
	"SELECT localpart, created_ts FROM userapi_localpart_external_ids WHERE external_id = $1 AND auth_provider = $2"

const deleteLocalpartExternalIDSQL = "" +
	"DELETE FROM userapi_localpart_external_ids WHERE external_id = $1 AND auth_provider = $2"

type localpartExternalIDStatements struct {
	db                       *sql.DB
	insertUserExternalIDStmt *sql.Stmt
	selectUserExternalIDStmt *sql.Stmt
	deleteUserExternalIDStmt *sql.Stmt
}

func NewSQLiteLocalpartExternalIDsTable(db *sql.DB) (tables.LocalpartExternalIDsTable, error) {
	s := &localpartExternalIDStatements{
		db: db,
	}
	_, err := db.Exec(localpartExternalIDsSchema)
	if err != nil {
		return nil, err
	}
	return s, sqlutil.StatementList{
		{&s.insertUserExternalIDStmt, insertLocalpartExternalIDSQL},
		{&s.selectUserExternalIDStmt, selectLocalpartExternalIDSQL},
		{&s.deleteUserExternalIDStmt, deleteLocalpartExternalIDSQL},
	}.Prepare(db)
}

// SelectLocalExternalPartID selects an existing OpenID Connect connection from the database
func (u *localpartExternalIDStatements) SelectLocalExternalPartID(ctx context.Context, txn *sql.Tx, externalID, authProvider string) (*api.LocalpartExternalID, error) {
	ret := api.LocalpartExternalID{
		ExternalID:   externalID,
		AuthProvider: authProvider,
	}
	err := u.selectUserExternalIDStmt.QueryRowContext(ctx, externalID, authProvider).Scan(
		&ret.Localpart, &ret.CreatedTS,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		log.WithError(err).Error("Unable to retrieve localpart from the db")
		return nil, err
	}

	return &ret, nil
}

// InsertLocalExternalPartID creates a new record representing an OpenID Connect connection between Matrix and external accounts.
func (u *localpartExternalIDStatements) InsertLocalExternalPartID(ctx context.Context, txn *sql.Tx, localpart, externalID, authProvider string) error {
	stmt := sqlutil.TxStmt(txn, u.insertUserExternalIDStmt)
	_, err := stmt.ExecContext(ctx, localpart, externalID, authProvider, time.Now().Unix())
	return err
}

// DeleteLocalExternalPartID deletes the existing OpenID Connect connection. After this method is called, the Matrix account will no longer be associated with the external account.
func (u *localpartExternalIDStatements) DeleteLocalExternalPartID(ctx context.Context, txn *sql.Tx, externalID, authProvider string) error {
	stmt := sqlutil.TxStmt(txn, u.deleteUserExternalIDStmt)
	_, err := stmt.ExecContext(ctx, externalID, authProvider)
	return err
}
