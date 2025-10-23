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

func UpThreadNotificationData(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		ALTER TABLE syncapi_notification_data
		ADD COLUMN IF NOT EXISTS thread_root_event_id TEXT NOT NULL DEFAULT '';

		ALTER TABLE syncapi_notification_data
		DROP CONSTRAINT IF EXISTS syncapi_notification_data_unique;

		ALTER TABLE syncapi_notification_data
		ADD CONSTRAINT syncapi_notification_data_unique UNIQUE (user_id, room_id, thread_root_event_id);

		CREATE INDEX IF NOT EXISTS syncapi_notification_data_thread_idx
			ON syncapi_notification_data (user_id, room_id, thread_root_event_id);
	`)
	if err != nil {
		return fmt.Errorf("failed to execute thread notification data upgrade: %w", err)
	}
	return nil
}

func DownThreadNotificationData(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		DROP INDEX IF EXISTS syncapi_notification_data_thread_idx;

		ALTER TABLE syncapi_notification_data
		DROP CONSTRAINT IF EXISTS syncapi_notification_data_unique;

		ALTER TABLE syncapi_notification_data
		ADD CONSTRAINT syncapi_notification_data_unique UNIQUE (user_id, room_id);

		ALTER TABLE syncapi_notification_data
		DROP COLUMN IF EXISTS thread_root_event_id;
	`)
	if err != nil {
		return fmt.Errorf("failed to execute thread notification data downgrade: %w", err)
	}
	return nil
}
