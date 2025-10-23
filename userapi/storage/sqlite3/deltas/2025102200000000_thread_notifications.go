// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package deltas

import (
	"context"
	"database/sql"
	"fmt"
)

func UpNotificationThreads(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.QueryContext(ctx, "SELECT thread_root_event_id FROM userapi_notifications LIMIT 1"); err == nil {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		ALTER TABLE userapi_notifications ADD COLUMN thread_root_event_id TEXT NOT NULL DEFAULT '';
		CREATE INDEX IF NOT EXISTS userapi_notification_thread_idx ON userapi_notifications(localpart, server_name, room_id, thread_root_event_id);
	`)
	if err != nil {
		return fmt.Errorf("failed to execute notification thread upgrade: %w", err)
	}
	return nil
}

func DownNotificationThreads(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		DROP INDEX IF EXISTS userapi_notification_thread_idx;
		ALTER TABLE userapi_notifications DROP COLUMN thread_root_event_id;
	`)
	if err != nil {
		return fmt.Errorf("failed to execute notification thread downgrade: %w", err)
	}
	return nil
}
