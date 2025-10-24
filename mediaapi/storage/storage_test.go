package storage_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/mediaapi/storage"
	"github.com/element-hq/dendrite/mediaapi/types"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/test"
)

func mustCreateDatabase(t *testing.T, dbType test.DBType) (storage.Database, func()) {
	connStr, close := test.PrepareDBConnectionString(t, dbType)
	cm := sqlutil.NewConnectionManager(nil, config.DatabaseOptions{})
	db, err := storage.NewMediaAPIDatasource(cm, &config.DatabaseOptions{
		ConnectionString: config.DataSource(connStr),
	})
	if err != nil {
		t.Fatalf("NewSyncServerDatasource returned %s", err)
	}
	return db, close
}
func TestMediaRepository(t *testing.T) {
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		db, close := mustCreateDatabase(t, dbType)
		defer close()
		ctx := context.Background()
		t.Run("can insert media & query media", func(t *testing.T) {
			metadata := &types.MediaMetadata{
				MediaID:           "testing",
				Origin:            "localhost",
				ContentType:       "image/png",
				FileSizeBytes:     10,
				UploadName:        "upload test",
				Base64Hash:        "dGVzdGluZw==",
				UserID:            "@alice:localhost",
				Quarantined:       false,
				QuarantinedAt:     0,
				QuarantinedByUser: "",
				QuarantineReason:  "",
			}
			if err := db.StoreMediaMetadata(ctx, metadata); err != nil {
				t.Fatalf("unable to store media metadata: %v", err)
			}
			// query by media id
			gotMetadata, err := db.GetMediaMetadata(ctx, metadata.MediaID, metadata.Origin)
			if err != nil {
				t.Fatalf("unable to query media metadata: %v", err)
			}
			if !reflect.DeepEqual(metadata, gotMetadata) {
				t.Fatalf("expected metadata %+v, got %v", metadata, gotMetadata)
			}
			// query by media hash
			gotMetadata, err = db.GetMediaMetadataByHash(ctx, metadata.Base64Hash, metadata.Origin)
			if err != nil {
				t.Fatalf("unable to query media metadata by hash: %v", err)
			}
			if !reflect.DeepEqual(metadata, gotMetadata) {
				t.Fatalf("expected metadata %+v, got %v", metadata, gotMetadata)
			}
		})

		t.Run("media origin lookup is case insensitive", func(t *testing.T) {
			metadata := &types.MediaMetadata{
				MediaID:           "case-media",
				Origin:            "MiXeD.Example",
				ContentType:       "text/plain",
				FileSizeBytes:     5,
				UploadName:        "case.txt",
				Base64Hash:        "Y2FzZWhhc2g=",
				UserID:            "@case:example.com",
				Quarantined:       false,
				QuarantinedAt:     0,
				QuarantinedByUser: "",
				QuarantineReason:  "",
			}
			if err := db.StoreMediaMetadata(ctx, metadata); err != nil {
				t.Fatalf("unable to store media metadata: %v", err)
			}

			got, err := db.GetMediaMetadata(ctx, metadata.MediaID, "mixed.example")
			if err != nil {
				t.Fatalf("unable to query media metadata with lowercase origin: %v", err)
			}
			if got == nil {
				t.Fatalf("expected media metadata, got nil")
			}
			if got.Origin != "mixed.example" {
				t.Fatalf("expected normalized origin 'mixed.example', got %s", got.Origin)
			}
		})
	})
}

func TestThumbnailsStorage(t *testing.T) {
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		db, close := mustCreateDatabase(t, dbType)
		defer close()
		ctx := context.Background()
		t.Run("can insert thumbnails & query media", func(t *testing.T) {
			thumbnails := []*types.ThumbnailMetadata{
				{
					MediaMetadata: &types.MediaMetadata{
						MediaID:       "testing",
						Origin:        "localhost",
						ContentType:   "image/png",
						FileSizeBytes: 6,
					},
					ThumbnailSize: types.ThumbnailSize{
						Width:        5,
						Height:       5,
						ResizeMethod: types.Crop,
					},
				},
				{
					MediaMetadata: &types.MediaMetadata{
						MediaID:       "testing",
						Origin:        "localhost",
						ContentType:   "image/png",
						FileSizeBytes: 7,
					},
					ThumbnailSize: types.ThumbnailSize{
						Width:        1,
						Height:       1,
						ResizeMethod: types.Scale,
					},
				},
			}
			for i := range thumbnails {
				if err := db.StoreThumbnail(ctx, thumbnails[i]); err != nil {
					t.Fatalf("unable to store thumbnail metadata: %v", err)
				}
			}
			// query by single thumbnail
			gotMetadata, err := db.GetThumbnail(ctx,
				thumbnails[0].MediaMetadata.MediaID,
				thumbnails[0].MediaMetadata.Origin,
				thumbnails[0].ThumbnailSize.Width, thumbnails[0].ThumbnailSize.Height,
				thumbnails[0].ThumbnailSize.ResizeMethod,
			)
			if err != nil {
				t.Fatalf("unable to query thumbnail metadata: %v", err)
			}
			if !reflect.DeepEqual(thumbnails[0].MediaMetadata, gotMetadata.MediaMetadata) {
				t.Fatalf("expected metadata %+v, got %+v", thumbnails[0].MediaMetadata, gotMetadata.MediaMetadata)
			}
			if !reflect.DeepEqual(thumbnails[0].ThumbnailSize, gotMetadata.ThumbnailSize) {
				t.Fatalf("expected metadata %+v, got %+v", thumbnails[0].MediaMetadata, gotMetadata.MediaMetadata)
			}
			// query by all thumbnails
			gotMediadatas, err := db.GetThumbnails(ctx, thumbnails[0].MediaMetadata.MediaID, thumbnails[0].MediaMetadata.Origin)
			if err != nil {
				t.Fatalf("unable to query media metadata by hash: %v", err)
			}
			if len(gotMediadatas) != len(thumbnails) {
				t.Fatalf("expected %d stored thumbnail metadata, got %d", len(thumbnails), len(gotMediadatas))
			}
			for i := range gotMediadatas {
				// metadata may be returned in a different order than it was stored, perform a search
				metaDataMatches := func() bool {
					for _, t := range thumbnails {
						if reflect.DeepEqual(t.MediaMetadata, gotMediadatas[i].MediaMetadata) && reflect.DeepEqual(t.ThumbnailSize, gotMediadatas[i].ThumbnailSize) {
							return true
						}
					}
					return false
				}

				if !metaDataMatches() {
					t.Fatalf("expected metadata %+v, got %+v", thumbnails[i].MediaMetadata, gotMediadatas[i].MediaMetadata)

				}
			}
		})

		t.Run("thumbnail origin lookup is case insensitive", func(t *testing.T) {
			thumb := &types.ThumbnailMetadata{
				MediaMetadata: &types.MediaMetadata{
					MediaID:       "case-thumb",
					Origin:        "Files.SERVER",
					ContentType:   "image/png",
					FileSizeBytes: 12,
				},
				ThumbnailSize: types.ThumbnailSize{Width: 32, Height: 32, ResizeMethod: types.Crop},
			}
			if err := db.StoreThumbnail(ctx, thumb); err != nil {
				t.Fatalf("unable to store thumbnail: %v", err)
			}

			thumbMeta, err := db.GetThumbnail(ctx, thumb.MediaMetadata.MediaID, "files.server", thumb.ThumbnailSize.Width, thumb.ThumbnailSize.Height, thumb.ThumbnailSize.ResizeMethod)
			if err != nil {
				t.Fatalf("unable to query thumbnail with lowercase origin: %v", err)
			}
			if thumbMeta == nil {
				t.Fatalf("expected thumbnail metadata, got nil")
			}
			if thumbMeta.MediaMetadata.Origin != "files.server" {
				t.Fatalf("expected normalized origin 'files.server', got %s", thumbMeta.MediaMetadata.Origin)
			}
		})
	})
}

func TestQuarantineMedia(t *testing.T) {
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		db, close := mustCreateDatabase(t, dbType)
		defer close()
		ctx := context.Background()

		meta := &types.MediaMetadata{
			MediaID:       "local-media",
			Origin:        "localhost",
			ContentType:   "image/jpeg",
			FileSizeBytes: 42,
			UploadName:    "photo.jpg",
			Base64Hash:    "bG9jYWw=",
			UserID:        "@bob:example.com",
		}
		if err := db.StoreMediaMetadata(ctx, meta); err != nil {
			t.Fatalf("StoreMediaMetadata returned error: %v", err)
		}

		isQuarantined, err := db.IsMediaQuarantined(ctx, meta.MediaID, meta.Origin)
		if err != nil {
			t.Fatalf("IsMediaQuarantined returned error: %v", err)
		}
		if isQuarantined {
			t.Fatalf("expected media to not be quarantined by default")
		}

		admin := types.MatrixUserID("@admin:test")
		count, err := db.QuarantineMedia(ctx, meta.MediaID, meta.Origin, admin, "test case", true)
		if err != nil {
			t.Fatalf("QuarantineMedia returned error: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected to quarantine 1 record, updated %d", count)
		}

		updated, err := db.GetMediaMetadata(ctx, meta.MediaID, meta.Origin)
		if err != nil {
			t.Fatalf("GetMediaMetadata returned error: %v", err)
		}
		if updated == nil {
			t.Fatalf("expected metadata after quarantine")
		}
		if !updated.Quarantined {
			t.Fatalf("expected media to be quarantined after update")
		}
		if updated.QuarantinedByUser != admin {
			t.Fatalf("expected quarantined_by to be %s, got %s", admin, updated.QuarantinedByUser)
		}
		if updated.QuarantineReason != "test case" {
			t.Fatalf("unexpected quarantine reason: %s", updated.QuarantineReason)
		}
		if updated.QuarantinedAt == 0 {
			t.Fatalf("expected quarantined timestamp to be populated")
		}

		isQuarantined, err = db.IsMediaQuarantined(ctx, meta.MediaID, meta.Origin)
		if err != nil {
			t.Fatalf("IsMediaQuarantined returned error: %v", err)
		}
		if !isQuarantined {
			t.Fatalf("expected media to be quarantined according to status check")
		}
	})
}

func TestQuarantineMediaByUser(t *testing.T) {
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		db, close := mustCreateDatabase(t, dbType)
		defer close()
		ctx := context.Background()

		userID := types.MatrixUserID("@charlie:example.com")
		metas := []*types.MediaMetadata{
			{
				MediaID:       "user-media-1",
				Origin:        "localhost",
				ContentType:   "image/png",
				FileSizeBytes: 11,
				UploadName:    "one.png",
				Base64Hash:    "dXNlcjE=",
				UserID:        userID,
			},
			{
				MediaID:       "user-media-2",
				Origin:        "localhost",
				ContentType:   "image/png",
				FileSizeBytes: 22,
				UploadName:    "two.png",
				Base64Hash:    "dXNlcjI=",
				UserID:        userID,
			},
			{
				MediaID:       "other-user-media",
				Origin:        "localhost",
				ContentType:   "image/png",
				FileSizeBytes: 33,
				UploadName:    "three.png",
				Base64Hash:    "b3RoZXI=",
				UserID:        "@someone:else",
			},
		}
		for _, m := range metas {
			if err := db.StoreMediaMetadata(ctx, m); err != nil {
				t.Fatalf("StoreMediaMetadata returned error: %v", err)
			}
		}

		admin := types.MatrixUserID("@admin:test")
		count, err := db.QuarantineMediaByUser(ctx, userID, admin, "bulk user quarantine", false)
		if err != nil {
			t.Fatalf("QuarantineMediaByUser returned error: %v", err)
		}
		if count != 2 {
			t.Fatalf("expected to quarantine 2 media records, updated %d", count)
		}

		for i, id := range []types.MediaID{"user-media-1", "user-media-2"} {
			meta, err := db.GetMediaMetadata(ctx, id, "localhost")
			if err != nil {
				t.Fatalf("GetMediaMetadata returned error: %v", err)
			}
			if meta == nil || !meta.Quarantined {
				t.Fatalf("expected media %d to be quarantined", i)
			}
			if meta.QuarantinedByUser != admin {
				t.Fatalf("expected quarantined_by for media %d to be %s, got %s", i, admin, meta.QuarantinedByUser)
			}
		}

		other, err := db.GetMediaMetadata(ctx, "other-user-media", "localhost")
		if err != nil {
			t.Fatalf("GetMediaMetadata returned error: %v", err)
		}
		if other == nil || other.Quarantined {
			t.Fatalf("expected media from other users to remain unquarantined")
		}
	})
}
