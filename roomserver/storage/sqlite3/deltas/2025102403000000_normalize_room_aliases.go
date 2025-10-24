package deltas

import (
	"context"
	"database/sql"
	"fmt"
)

func UpNormalizeRoomAliases(ctx context.Context, tx *sql.Tx) error {
	const duplicateCheck = `
SELECT LOWER(alias), COUNT(*)
FROM roomserver_room_aliases
GROUP BY LOWER(alias)
HAVING COUNT(*) > 1
LIMIT 1;
`
	var canonical string
	var count int
	switch err := tx.QueryRowContext(ctx, duplicateCheck).Scan(&canonical, &count); err {
	case sql.ErrNoRows:
	case nil:
		return fmt.Errorf("roomserver_room_aliases contains aliases that differ only by case (canonical %s) - deduplicate before rerunning", canonical)
	default:
		return err
	}

	if _, err := tx.ExecContext(ctx, `
        UPDATE roomserver_room_aliases
        SET alias = LOWER(alias)
        WHERE alias <> LOWER(alias)
    `); err != nil {
		return err
	}

	return nil
}

func DownNormalizeRoomAliases(ctx context.Context, tx *sql.Tx) error {
	return nil
}
