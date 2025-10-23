// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package sqlite3

import (
	"database/sql"
)

func ensureSQLiteColumns(db *sql.DB, table string, columns map[string]string) error {
	for column, definition := range columns {
		exists, err := sqliteColumnExists(db, table, column)
		if err != nil {
			return err
		}
		if !exists {
			if _, err = db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + definition); err != nil {
				return err
			}
		}
	}
	return nil
}

func sqliteColumnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func ensureSQLiteIndex(db *sql.DB, createStmt string) error {
	_, err := db.Exec(createStmt)
	return err
}
