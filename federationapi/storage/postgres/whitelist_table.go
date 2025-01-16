package postgres

import (
	"context"
	"database/sql"

	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/matrix-org/gomatrixserverlib/spec"
)

const whitelistSchema = `
CREATE TABLE IF NOT EXISTS federationsender_whitelist (
    -- The whitelisted server name
	server_name TEXT NOT NULL,
	UNIQUE (server_name)
`

const insertWhitelistSQL = "" +
	"INSERT INTO federationsender_whitelist (server_name) VALUES ($1)" +
	" ON CONFLICT DO NOTHING"

const selectWhitelistSQL = "" +
	"SELECT server_name FROM federationsender_whitelist WHERE server_name = $1"

const deleteWhitelistSQL = "" +
	"DELETE FROM federationsender_whitelist WHERE server_name = $1"

const deleteAllWhitelistSQL = "" +
	"TRUNCATE federationsender_whitelist"

type whitelistStatements struct {
	db                     *sql.DB
	insertWhitelistStmt    *sql.Stmt
	selectWhitelistStmt    *sql.Stmt
	deleteWhitelistStmt    *sql.Stmt
	deleteAllWhitelistStmt *sql.Stmt
}

func NewPostgresWhitelistTable(db *sql.DB) (s *whitelistStatements, err error) {
	s = &whitelistStatements{
		db: db,
	}
	_, err = db.Exec(whitelistSchema)
	if err != nil {
		return
	}

	return s, sqlutil.StatementList{
		{&s.insertWhitelistStmt, insertWhitelistSQL},
		{&s.selectWhitelistStmt, selectWhitelistSQL},
		{&s.deleteWhitelistStmt, deleteWhitelistSQL},
		{&s.deleteAllWhitelistStmt, deleteAllWhitelistSQL},
	}.Prepare(db)
}

func (s *whitelistStatements) InsertWhitelist(
	ctx context.Context, txn *sql.Tx, serverName spec.ServerName,
) error {
	stmt := sqlutil.TxStmt(txn, s.insertWhitelistStmt)
	_, err := stmt.ExecContext(ctx, serverName)
	return err
}

func (s *whitelistStatements) SelectWhitelist(
	ctx context.Context, txn *sql.Tx, serverName spec.ServerName,
) (bool, error) {
	stmt := sqlutil.TxStmt(txn, s.selectWhitelistStmt)
	res, err := stmt.QueryContext(ctx, serverName)
	if err != nil {
		return false, err
	}
	defer res.Close() // nolint:errcheck
	// The query will return the server name if the server is whitelisted, and
	// will return no rows if not. By calling Next, we find out if a row was
	// returned or not - we don't care about the value itself.
	return res.Next(), nil
}

func (s *whitelistStatements) DeleteWhitelist(
	ctx context.Context, txn *sql.Tx, serverName spec.ServerName,
) error {
	stmt := sqlutil.TxStmt(txn, s.deleteWhitelistStmt)
	_, err := stmt.ExecContext(ctx, serverName)
	return err
}

func (s *whitelistStatements) DeleteAllWhitelist(
	ctx context.Context, txn *sql.Tx,
) error {
	stmt := sqlutil.TxStmt(txn, s.deleteAllWhitelistStmt)
	_, err := stmt.ExecContext(ctx)
	return err
}
