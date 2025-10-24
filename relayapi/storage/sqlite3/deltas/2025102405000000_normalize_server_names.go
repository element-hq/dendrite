package deltas

import (
	"context"
	"database/sql"
	"fmt"
)

func UpNormalizeServerNames(ctx context.Context, tx *sql.Tx) error {
	const duplicateCheck = `
SELECT LOWER(server_name) AS canonical, COUNT(*)
FROM relayapi_queue
GROUP BY LOWER(server_name)
HAVING COUNT(*) > 1
LIMIT 1;
`
	var canonical string
	var count int
	switch err := tx.QueryRowContext(ctx, duplicateCheck).Scan(&canonical, &count); err {
	case sql.ErrNoRows:
	case nil:
		return fmt.Errorf("relayapi_queue contains duplicate server names (canonical=%s) differing only by case; deduplicate before upgrading", canonical)
	default:
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE relayapi_queue SET server_name = LOWER(server_name) WHERE server_name <> LOWER(server_name)`)
	return err
}

func DownNormalizeServerNames(ctx context.Context, tx *sql.Tx) error {
	return nil
}
