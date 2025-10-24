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

func UpNormalizeMediaOrigins(ctx context.Context, tx *sql.Tx) error {
	const mediaRepoDupes = `
SELECT media_id, LOWER(media_origin) AS canonical_origin, COUNT(*)
FROM mediaapi_media_repository
GROUP BY media_id, LOWER(media_origin)
HAVING COUNT(*) > 1
LIMIT 1;
`
	var mediaID, canonicalOrigin string
	var count int
	switch err := tx.QueryRowContext(ctx, mediaRepoDupes).Scan(&mediaID, &canonicalOrigin, &count); err {
	case sql.ErrNoRows:
	case nil:
		return fmt.Errorf("mediaapi_media_repository contains duplicate origins for media_id=%s (canonical=%s) differing only by case; deduplicate before upgrading", mediaID, canonicalOrigin)
	default:
		return err
	}

	const thumbnailDupes = `
SELECT media_id, LOWER(media_origin) AS canonical_origin, width, height, resize_method, COUNT(*)
FROM mediaapi_thumbnail
GROUP BY media_id, LOWER(media_origin), width, height, resize_method
HAVING COUNT(*) > 1
LIMIT 1;
`
	var width, height int
	var resizeMethod string
	switch err := tx.QueryRowContext(ctx, thumbnailDupes).Scan(&mediaID, &canonicalOrigin, &width, &height, &resizeMethod, &count); err {
	case sql.ErrNoRows:
	case nil:
		return fmt.Errorf("mediaapi_thumbnail contains duplicate origins for media_id=%s thumbnail=%dx%d %s (canonical=%s) differing only by case; deduplicate before upgrading", mediaID, width, height, resizeMethod, canonicalOrigin)
	default:
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE mediaapi_media_repository
		SET media_origin = LOWER(media_origin)
		WHERE media_origin <> LOWER(media_origin)
	`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE mediaapi_thumbnail
		SET media_origin = LOWER(media_origin)
		WHERE media_origin <> LOWER(media_origin)
	`); err != nil {
		return err
	}
	return nil
}

func DownNormalizeMediaOrigins(ctx context.Context, tx *sql.Tx) error {
	// Irreversible; original casing cannot be restored.
	return nil
}
