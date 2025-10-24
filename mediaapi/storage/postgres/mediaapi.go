// Copyright 2024 New Vector Ltd.
// Copyright 2019, 2020 The Matrix.org Foundation C.I.C.
// Copyright 2017, 2018 New Vector Ltd
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package postgres

import (
	"context"

	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/mediaapi/storage/postgres/deltas"
	"github.com/element-hq/dendrite/mediaapi/storage/shared"
	"github.com/element-hq/dendrite/setup/config"
	_ "github.com/lib/pq"
)

// NewDatabase opens a postgres database.
func NewDatabase(conMan *sqlutil.Connections, dbProperties *config.DatabaseOptions) (*shared.Database, error) {
	db, writer, err := conMan.Connection(dbProperties)
	if err != nil {
		return nil, err
	}
	mediaRepo, err := NewPostgresMediaRepositoryTable(db)
	if err != nil {
		return nil, err
	}
	if _, err = db.Exec(`CREATE INDEX IF NOT EXISTS mediaapi_media_repository_user_id_idx ON mediaapi_media_repository(user_id)`); err != nil {
		return nil, err
	}
	thumbnails, err := NewPostgresThumbnailsTable(db)
	if err != nil {
		return nil, err
	}
	m := sqlutil.NewMigrator(db)
	m.AddMigrations(sqlutil.Migration{
		Version: "mediaapi: normalize media origins",
		Up:      deltas.UpNormalizeMediaOrigins,
		Down:    deltas.DownNormalizeMediaOrigins,
	})
	if err := m.Up(context.Background()); err != nil {
		return nil, err
	}
	return &shared.Database{
		MediaRepository: mediaRepo,
		Thumbnails:      thumbnails,
		DB:              db,
		Writer:          writer,
	}, nil
}
