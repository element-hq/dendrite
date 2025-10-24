package deltas

import (
	"context"
	"database/sql"
	"fmt"
)

func UpNormalizeLocalparts(ctx context.Context, tx *sql.Tx) error {
	const duplicateCheck = `
SELECT LOWER(localpart), server_name, COUNT(*)
FROM userapi_accounts
GROUP BY LOWER(localpart), server_name
HAVING COUNT(*) > 1
LIMIT 1;
`
	var canonical, serverName string
	var count int
	switch err := tx.QueryRowContext(ctx, duplicateCheck).Scan(&canonical, &serverName, &count); err {
	case sql.ErrNoRows:
	case nil:
		return fmt.Errorf("userapi_accounts contains localparts that differ only by case (localpart=%s server=%s) - deduplicate before rerunning", canonical, serverName)
	default:
		return err
	}

	statements := []string{
		`UPDATE userapi_accounts SET localpart = LOWER(localpart) WHERE localpart <> LOWER(localpart)`,
		`UPDATE userapi_devices SET localpart = LOWER(localpart) WHERE localpart <> LOWER(localpart)`,
		`UPDATE userapi_pushers SET localpart = LOWER(localpart) WHERE localpart <> LOWER(localpart)`,
		`UPDATE userapi_threepids SET localpart = LOWER(localpart) WHERE localpart <> LOWER(localpart)`,
		`UPDATE userapi_profiles SET localpart = LOWER(localpart) WHERE localpart <> LOWER(localpart)`,
		`UPDATE userapi_notifications SET localpart = LOWER(localpart) WHERE localpart <> LOWER(localpart)`,
		`UPDATE userapi_account_datas SET localpart = LOWER(localpart) WHERE localpart <> LOWER(localpart)`,
	}

	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	return nil
}

func DownNormalizeLocalparts(ctx context.Context, tx *sql.Tx) error {
	return nil
}
