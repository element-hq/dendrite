// Copyright 2024 New Vector Ltd.
// Copyright 2020 The Matrix.org Foundation C.I.C.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package storage

import (
	"context"

	"github.com/element-hq/dendrite/mediaapi/types"
	"github.com/matrix-org/gomatrixserverlib/spec"
)

type Database interface {
	MediaRepository
	Thumbnails
	QuarantineMedia(ctx context.Context, mediaID types.MediaID, mediaOrigin spec.ServerName, quarantinedBy types.MatrixUserID, reason string, quarantineThumbnails bool) (int64, error)
	QuarantineMediaByUser(ctx context.Context, userID types.MatrixUserID, quarantinedBy types.MatrixUserID, reason string, quarantineThumbnails bool) (int64, error)
	IsMediaQuarantined(ctx context.Context, mediaID types.MediaID, mediaOrigin spec.ServerName) (bool, error)
}

type MediaRepository interface {
	StoreMediaMetadata(ctx context.Context, mediaMetadata *types.MediaMetadata) error
	GetMediaMetadata(ctx context.Context, mediaID types.MediaID, mediaOrigin spec.ServerName) (*types.MediaMetadata, error)
	GetMediaMetadataByHash(ctx context.Context, mediaHash types.Base64Hash, mediaOrigin spec.ServerName) (*types.MediaMetadata, error)
	SetMediaQuarantined(ctx context.Context, mediaID types.MediaID, mediaOrigin spec.ServerName, quarantinedBy types.MatrixUserID, reason string) (int64, error)
	SetMediaQuarantinedByUser(ctx context.Context, userID types.MatrixUserID, quarantinedBy types.MatrixUserID, reason string) (int64, error)
	SelectMediaQuarantined(ctx context.Context, mediaID types.MediaID, mediaOrigin spec.ServerName) (bool, error)
}

type Thumbnails interface {
	StoreThumbnail(ctx context.Context, thumbnailMetadata *types.ThumbnailMetadata) error
	GetThumbnail(ctx context.Context, mediaID types.MediaID, mediaOrigin spec.ServerName, width, height int, resizeMethod string) (*types.ThumbnailMetadata, error)
	GetThumbnails(ctx context.Context, mediaID types.MediaID, mediaOrigin spec.ServerName) ([]*types.ThumbnailMetadata, error)
	SetThumbnailsQuarantined(ctx context.Context, mediaID types.MediaID, mediaOrigin spec.ServerName, quarantinedBy types.MatrixUserID, reason string) (int64, error)
}
