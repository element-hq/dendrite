// Copyright 2025 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package deltas

import (
	"context"
	"database/sql"
	"fmt"
)

const sqliteDuplicateThreePIDCheckSQL = `
SELECT LOWER(threepid), medium, COUNT(*)
FROM userapi_threepids
GROUP BY LOWER(threepid), medium
HAVING COUNT(*) > 1
LIMIT 1;
`

const sqliteNormalizeThreePIDSQL = `
UPDATE userapi_threepids
SET threepid = LOWER(threepid)
WHERE medium = 'email' AND threepid != LOWER(threepid);
`

const sqliteCreateLowerThreePIDIndexSQL = `
CREATE UNIQUE INDEX IF NOT EXISTS userapi_threepids_threepid_lower_idx
ON userapi_threepids(LOWER(threepid), medium);
`

func UpNormalizeThreePIDs(ctx context.Context, tx *sql.Tx) error {
	row := tx.QueryRowContext(ctx, sqliteDuplicateThreePIDCheckSQL)
	var addr string
	var medium string
	var count int
	switch err := row.Scan(&addr, &medium, &count); err {
	case sql.ErrNoRows:
		// No duplicates detected; continue.
	case nil:
		return fmt.Errorf("normalize threepids: duplicate %s threepid detected for medium %s; please deduplicate before upgrade", addr, medium)
	default:
		return fmt.Errorf("normalize threepids: duplicate scan failed: %w", err)
	}

	if _, err := tx.ExecContext(ctx, sqliteNormalizeThreePIDSQL); err != nil {
		return fmt.Errorf("normalize threepids: failed to canonicalise data: %w", err)
	}

	if _, err := tx.ExecContext(ctx, sqliteCreateLowerThreePIDIndexSQL); err != nil {
		return fmt.Errorf("normalize threepids: failed to create lower-case index: %w", err)
	}

	return nil
}

func DownNormalizeThreePIDs(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
DROP INDEX IF EXISTS userapi_threepids_threepid_lower_idx;
`); err != nil {
		return fmt.Errorf("normalize threepids: failed to drop lower-case index: %w", err)
	}
	return nil
}
