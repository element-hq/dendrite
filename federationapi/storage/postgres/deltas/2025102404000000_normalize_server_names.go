package deltas

import (
	"context"
	"database/sql"
	"fmt"
)

func UpNormalizeServerNames(ctx context.Context, tx *sql.Tx) error {
	var canonical string
	var count int
	checks := []string{
		"SELECT LOWER(server_name) AS canonical, COUNT(*) FROM federationsender_assumed_offline GROUP BY LOWER(server_name) HAVING COUNT(*) > 1 LIMIT 1",
		"SELECT LOWER(server_name) AS canonical, COUNT(*) FROM federationsender_blacklist GROUP BY LOWER(server_name) HAVING COUNT(*) > 1 LIMIT 1",
	}
	for _, table := range checks {
		switch err := tx.QueryRowContext(ctx, table).Scan(&canonical, &count); err {
		case sql.ErrNoRows:
		case nil:
			return fmt.Errorf("federation table contains server names that differ only by case (canonical=%s) - deduplicate before rerunning", canonical)
		default:
			return err
		}
	}

	var relayCanonicalServer, relayCanonicalTarget string
	var relayCount int
	relayQuery := `
SELECT LOWER(server_name) AS canonical_server, LOWER(relay_server_name) AS canonical_target, COUNT(*)
FROM federationsender_relay_servers
GROUP BY LOWER(server_name), LOWER(relay_server_name)
HAVING COUNT(*) > 1
LIMIT 1;
`
	switch err := tx.QueryRowContext(ctx, relayQuery).Scan(&relayCanonicalServer, &relayCanonicalTarget, &relayCount); err {
	case sql.ErrNoRows:
	case nil:
		return fmt.Errorf("federationsender_relay_servers contains duplicate entries for server=%s relay=%s differing only by case - deduplicate before rerunning", relayCanonicalServer, relayCanonicalTarget)
	default:
		return err
	}

	statements := []string{
		`UPDATE federationsender_assumed_offline SET server_name = LOWER(server_name) WHERE server_name <> LOWER(server_name)`,
		`UPDATE federationsender_blacklist SET server_name = LOWER(server_name) WHERE server_name <> LOWER(server_name)`,
		`UPDATE federationsender_inbound_peeks SET server_name = LOWER(server_name) WHERE server_name <> LOWER(server_name)`,
		`UPDATE federationsender_outbound_peeks SET server_name = LOWER(server_name) WHERE server_name <> LOWER(server_name)`,
		`UPDATE federationsender_queue_edus SET server_name = LOWER(server_name) WHERE server_name <> LOWER(server_name)`,
		`UPDATE federationsender_queue_pdus SET server_name = LOWER(server_name) WHERE server_name <> LOWER(server_name)`,
		`UPDATE federationsender_relay_servers SET server_name = LOWER(server_name), relay_server_name = LOWER(relay_server_name) WHERE server_name <> LOWER(server_name) OR relay_server_name <> LOWER(relay_server_name)`,
		`UPDATE federationsender_joined_hosts SET server_name = LOWER(server_name) WHERE server_name <> LOWER(server_name)`,
		`UPDATE federationsender_notary_server_keys_json SET server_name = LOWER(server_name) WHERE server_name <> LOWER(server_name)`,
		`UPDATE federationsender_notary_server_keys_metadata SET server_name = LOWER(server_name) WHERE server_name <> LOWER(server_name)`,
		`UPDATE keydb_server_keys SET server_name = LOWER(server_name), server_name_and_key_id = LOWER(server_name) || E'\x1F' || server_key_id WHERE server_name <> LOWER(server_name)`,
	}

	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	return nil
}

func DownNormalizeServerNames(ctx context.Context, tx *sql.Tx) error {
	return nil
}
