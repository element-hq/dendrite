// Copyright 2024 New Vector Ltd.
// Copyright 2022 The Matrix.org Foundation C.I.C.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package shared

import (
	"context"
	"database/sql"
	"errors"

	"github.com/element-hq/dendrite/internal/sqlutil"
	iutil "github.com/element-hq/dendrite/internal/util"
	"github.com/element-hq/dendrite/mediaapi/storage/tables"
	"github.com/element-hq/dendrite/mediaapi/types"
	"github.com/matrix-org/gomatrixserverlib/spec"
)

type Database struct {
	DB              *sql.DB
	Writer          sqlutil.Writer
	MediaRepository tables.MediaRepository
	Thumbnails      tables.Thumbnails
}

// StoreMediaMetadata inserts the metadata about the uploaded media into the database.
// Returns an error if the combination of MediaID and Origin are not unique in the table.
func (d *Database) StoreMediaMetadata(ctx context.Context, mediaMetadata *types.MediaMetadata) error {
	mediaMetadata.Origin = iutil.NormalizeServerName(mediaMetadata.Origin)
	return d.Writer.Do(d.DB, nil, func(txn *sql.Tx) error {
		return d.MediaRepository.InsertMedia(ctx, txn, mediaMetadata)
	})
}

// GetMediaMetadata returns metadata about media stored on this server.
// The media could have been uploaded to this server or fetched from another server and cached here.
// Returns nil metadata if there is no metadata associated with this media.
func (d *Database) GetMediaMetadata(ctx context.Context, mediaID types.MediaID, mediaOrigin spec.ServerName) (*types.MediaMetadata, error) {
	mediaOrigin = iutil.NormalizeServerName(mediaOrigin)
	mediaMetadata, err := d.MediaRepository.SelectMedia(ctx, nil, mediaID, mediaOrigin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return mediaMetadata, err
}

// GetMediaMetadataByHash returns metadata about media stored on this server.
// The media could have been uploaded to this server or fetched from another server and cached here.
// Returns nil metadata if there is no metadata associated with this media.
func (d *Database) GetMediaMetadataByHash(ctx context.Context, mediaHash types.Base64Hash, mediaOrigin spec.ServerName) (*types.MediaMetadata, error) {
	mediaOrigin = iutil.NormalizeServerName(mediaOrigin)
	mediaMetadata, err := d.MediaRepository.SelectMediaByHash(ctx, nil, mediaHash, mediaOrigin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return mediaMetadata, err
}

// StoreThumbnail inserts the metadata about the thumbnail into the database.
// Returns an error if the combination of MediaID and Origin are not unique in the table.
func (d *Database) StoreThumbnail(ctx context.Context, thumbnailMetadata *types.ThumbnailMetadata) error {
	thumbnailMetadata.MediaMetadata.Origin = iutil.NormalizeServerName(thumbnailMetadata.MediaMetadata.Origin)
	return d.Writer.Do(d.DB, nil, func(txn *sql.Tx) error {
		return d.Thumbnails.InsertThumbnail(ctx, txn, thumbnailMetadata)
	})
}

// GetThumbnail returns metadata about a specific thumbnail.
// The media could have been uploaded to this server or fetched from another server and cached here.
// Returns nil metadata if there is no metadata associated with this thumbnail.
func (d *Database) GetThumbnail(ctx context.Context, mediaID types.MediaID, mediaOrigin spec.ServerName, width, height int, resizeMethod string) (*types.ThumbnailMetadata, error) {
	mediaOrigin = iutil.NormalizeServerName(mediaOrigin)
	metadata, err := d.Thumbnails.SelectThumbnail(ctx, nil, mediaID, mediaOrigin, width, height, resizeMethod)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return metadata, err
}

// GetThumbnails returns metadata about all thumbnails for a specific media stored on this server.
// The media could have been uploaded to this server or fetched from another server and cached here.
// Returns nil metadata if there are no thumbnails associated with this media.
func (d *Database) GetThumbnails(ctx context.Context, mediaID types.MediaID, mediaOrigin spec.ServerName) ([]*types.ThumbnailMetadata, error) {
	mediaOrigin = iutil.NormalizeServerName(mediaOrigin)
	metadatas, err := d.Thumbnails.SelectThumbnails(ctx, nil, mediaID, mediaOrigin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return metadatas, err
}

func (d *Database) SetMediaQuarantined(
	ctx context.Context,
	mediaID types.MediaID,
	mediaOrigin spec.ServerName,
	quarantinedBy types.MatrixUserID,
	reason string,
) (int64, error) {
	return d.MediaRepository.SetMediaQuarantined(ctx, nil, mediaID, mediaOrigin, quarantinedBy, reason)
}

func (d *Database) SetMediaQuarantinedByUser(
	ctx context.Context,
	userID types.MatrixUserID,
	quarantinedBy types.MatrixUserID,
	reason string,
) (int64, error) {
	return d.MediaRepository.SetMediaQuarantinedByUser(ctx, nil, userID, quarantinedBy, reason)
}

func (d *Database) SelectMediaQuarantined(
	ctx context.Context,
	mediaID types.MediaID,
	mediaOrigin spec.ServerName,
) (bool, error) {
	mediaOrigin = iutil.NormalizeServerName(mediaOrigin)
	return d.MediaRepository.SelectMediaQuarantined(ctx, nil, mediaID, mediaOrigin)
}

func (d *Database) SetThumbnailsQuarantined(
	ctx context.Context,
	mediaID types.MediaID,
	mediaOrigin spec.ServerName,
	quarantinedBy types.MatrixUserID,
	reason string,
) (int64, error) {
	mediaOrigin = iutil.NormalizeServerName(mediaOrigin)
	return d.Thumbnails.SetThumbnailsQuarantined(ctx, nil, mediaID, mediaOrigin, quarantinedBy, reason)
}

// QuarantineMedia marks a single media item as quarantined.
func (d *Database) QuarantineMedia(
	ctx context.Context,
	mediaID types.MediaID,
	mediaOrigin spec.ServerName,
	quarantinedBy types.MatrixUserID,
	reason string,
	quarantineThumbnails bool,
) (int64, error) {
	var affected int64
	mediaOrigin = iutil.NormalizeServerName(mediaOrigin)
	err := d.Writer.Do(d.DB, nil, func(txn *sql.Tx) error {
		rows, err := d.MediaRepository.SetMediaQuarantined(ctx, txn, mediaID, mediaOrigin, quarantinedBy, reason)
		if err != nil {
			return err
		}
		affected = rows
		if rows == 0 || !quarantineThumbnails {
			return nil
		}
		_, err = d.Thumbnails.SetThumbnailsQuarantined(ctx, txn, mediaID, mediaOrigin, quarantinedBy, reason)
		return err
	})
	return affected, err
}

// QuarantineMediaByUser marks all media uploaded by the given user as quarantined.
func (d *Database) QuarantineMediaByUser(
	ctx context.Context,
	userID types.MatrixUserID,
	quarantinedBy types.MatrixUserID,
	reason string,
	quarantineThumbnails bool,
) (int64, error) {
	var affected int64
	err := d.Writer.Do(d.DB, nil, func(txn *sql.Tx) error {
		rows, err := d.MediaRepository.SetMediaQuarantinedByUser(ctx, txn, userID, quarantinedBy, reason)
		if err != nil {
			return err
		}
		affected = rows
		if rows == 0 || !quarantineThumbnails {
			return nil
		}
		_, err = d.Thumbnails.SetThumbnailsQuarantinedByUser(ctx, txn, userID, quarantinedBy, reason)
		return err
	})
	return affected, err
}

// IsMediaQuarantined returns true if the media has been quarantined.
func (d *Database) IsMediaQuarantined(ctx context.Context, mediaID types.MediaID, mediaOrigin spec.ServerName) (bool, error) {
	return d.MediaRepository.SelectMediaQuarantined(ctx, nil, mediaID, mediaOrigin)
}
