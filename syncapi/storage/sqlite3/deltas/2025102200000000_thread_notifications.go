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
	if _, err := tx.QueryContext(ctx, "SELECT thread_root_event_id FROM syncapi_notification_data LIMIT 1"); err == nil {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS syncapi_notification_data_tmp (
			id INTEGER PRIMARY KEY,
			user_id TEXT NOT NULL,
			room_id TEXT NOT NULL,
			thread_root_event_id TEXT NOT NULL DEFAULT '',
			notification_count BIGINT NOT NULL DEFAULT 0,
			highlight_count BIGINT NOT NULL DEFAULT 0,
			CONSTRAINT syncapi_notifications_unique UNIQUE (user_id, room_id, thread_root_event_id)
		);

		INSERT INTO syncapi_notification_data_tmp (id, user_id, room_id, thread_root_event_id, notification_count, highlight_count)
			SELECT id, user_id, room_id, '', notification_count, highlight_count FROM syncapi_notification_data;

		DROP TABLE syncapi_notification_data;
		ALTER TABLE syncapi_notification_data_tmp RENAME TO syncapi_notification_data;
		CREATE INDEX IF NOT EXISTS syncapi_notification_data_thread_idx ON syncapi_notification_data(user_id, room_id, thread_root_event_id);
	`)
	if err != nil {
		return fmt.Errorf("failed to execute thread notification data upgrade: %w", err)
	}
	return nil
}

func DownThreadNotificationData(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.QueryContext(ctx, "SELECT thread_root_event_id FROM syncapi_notification_data LIMIT 1"); err != nil {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS syncapi_notification_data_tmp (
			id INTEGER PRIMARY KEY,
			user_id TEXT NOT NULL,
			room_id TEXT NOT NULL,
			notification_count BIGINT NOT NULL DEFAULT 0,
			highlight_count BIGINT NOT NULL DEFAULT 0,
			CONSTRAINT syncapi_notifications_unique UNIQUE (user_id, room_id)
		);

		INSERT INTO syncapi_notification_data_tmp (id, user_id, room_id, notification_count, highlight_count)
			SELECT id, user_id, room_id, notification_count, highlight_count FROM syncapi_notification_data;

		DROP TABLE syncapi_notification_data;
		ALTER TABLE syncapi_notification_data_tmp RENAME TO syncapi_notification_data;
	`)
	if err != nil {
		return fmt.Errorf("failed to execute thread notification data downgrade: %w", err)
	}
	return nil
}
