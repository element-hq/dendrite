// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package routing

import (
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/mediaapi/fileutils"
	"github.com/element-hq/dendrite/mediaapi/storage"
	"github.com/element-hq/dendrite/mediaapi/types"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/matrix-org/gomatrixserverlib/spec"
	log "github.com/sirupsen/logrus"
)

// testMediaConfig creates a test media configuration
func testMediaConfig(t *testing.T, maxFileSize config.FileSizeBytes) (*config.MediaAPI, string) {
	t.Helper()

	testdataPath := filepath.Join(t.TempDir(), "mediatest")
	err := os.MkdirAll(testdataPath, os.ModePerm)
	if err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	globalConfig := &config.Global{}
	globalConfig.ServerName = "test.local"

	cfg := &config.MediaAPI{
		MaxFileSizeBytes:  maxFileSize,
		BasePath:          config.Path(testdataPath),
		AbsBasePath:       config.Path(testdataPath),
		DynamicThumbnails: false,
		ThumbnailSizes: []config.ThumbnailSize{
			{Width: 32, Height: 32, ResizeMethod: types.Scale},
			{Width: 96, Height: 96, ResizeMethod: types.Scale},
			{Width: 320, Height: 240, ResizeMethod: types.Scale},
			{Width: 640, Height: 480, ResizeMethod: types.Scale},
			{Width: 800, Height: 600, ResizeMethod: types.Scale},
		},
		MaxThumbnailGenerators: 10,
		Matrix:                 globalConfig,
	}

	return cfg, testdataPath
}

// testDatabase creates an in-memory test database
func testDatabase(t *testing.T) storage.Database {
	t.Helper()

	cm := sqlutil.NewConnectionManager(nil, config.DatabaseOptions{})
	db, err := storage.NewMediaAPIDatasource(cm, &config.DatabaseOptions{
		ConnectionString:       "file::memory:?cache=shared",
		MaxOpenConnections:     100,
		MaxIdleConnections:     2,
		ConnMaxLifetimeSeconds: -1,
	})
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}

	return db
}

// testLogger creates a test logger
func testLogger() *log.Entry {
	logger := log.New()
	logger.SetOutput(io.Discard) // Don't spam test output
	return logger.WithField("test", "mediaapi")
}

// createTestPNG creates a PNG image with the specified dimensions
func createTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// Fill with a gradient pattern
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := color.RGBA{
				R: uint8((x * 255) / width),
				G: uint8((y * 255) / height),
				B: 128,
				A: 255,
			}
			img.Set(x, y, c)
		}
	}

	var buf io.ReadWriter
	buf = &testBuffer{}
	err := png.Encode(buf, img)
	if err != nil {
		t.Fatalf("failed to encode PNG: %v", err)
	}

	data, _ := io.ReadAll(buf)
	return data
}

// createTestJPEG creates a JPEG image with the specified dimensions
func createTestJPEG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// Fill with a different pattern
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := color.RGBA{
				R: uint8((y * 255) / height),
				G: 128,
				B: uint8((x * 255) / width),
				A: 255,
			}
			img.Set(x, y, c)
		}
	}

	var buf io.ReadWriter
	buf = &testBuffer{}
	err := jpeg.Encode(buf, img, nil)
	if err != nil {
		t.Fatalf("failed to encode JPEG: %v", err)
	}

	data, _ := io.ReadAll(buf)
	return data
}

// testBuffer is a simple buffer for testing
type testBuffer struct {
	data []byte
	pos  int
}

func (b *testBuffer) Write(p []byte) (n int, err error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *testBuffer) Read(p []byte) (n int, err error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	n = copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}

// storeTestMedia stores test media in the database and returns the metadata
func storeTestMedia(t *testing.T, db storage.Database, cfg *config.MediaAPI, mediaID types.MediaID, content []byte) *types.MediaMetadata {
	t.Helper()

	metadata := &types.MediaMetadata{
		MediaID:       mediaID,
		Origin:        cfg.Matrix.ServerName,
		ContentType:   "image/png",
		FileSizeBytes: types.FileSizeBytes(len(content)),
		UploadName:    "test.png",
		UserID:        "@test:test.local",
	}

	// Write the file
	hash, bytesWritten, tmpDir, err := fileutils.WriteTempFile(context.Background(), &testBuffer{data: content}, cfg.AbsBasePath)
	if err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	metadata.Base64Hash = hash
	metadata.FileSizeBytes = bytesWritten

	// Move to final location
	finalPath, _, err := fileutils.MoveFileWithHashCheck(tmpDir, metadata, cfg.AbsBasePath, testLogger())
	if err != nil {
		t.Fatalf("failed to move file: %v", err)
	}

	// Store metadata
	err = db.StoreMediaMetadata(context.Background(), metadata)
	if err != nil {
		t.Fatalf("failed to store metadata: %v", err)
	}

	t.Cleanup(func() {
		fileutils.RemoveDir(types.Path(filepath.Dir(string(finalPath))), nil)
	})

	return metadata
}

// createTestMediaMetadata creates test media metadata without storing files
func createTestMediaMetadata(mediaID types.MediaID, origin spec.ServerName) *types.MediaMetadata {
	return &types.MediaMetadata{
		MediaID:       mediaID,
		Origin:        origin,
		ContentType:   "text/plain",
		FileSizeBytes: 100,
		UploadName:    "test.txt",
		UserID:        "@test:test.local",
		Base64Hash:    "dGVzdGhhc2g=", // base64 encoded "testhash"
	}
}

// testActiveThumbnailGeneration creates a test active thumbnail generation tracker
func testActiveThumbnailGeneration() *types.ActiveThumbnailGeneration {
	return &types.ActiveThumbnailGeneration{
		PathToResult: map[string]*types.ThumbnailGenerationResult{},
	}
}

// testActiveRemoteRequests creates a test active remote requests tracker
func testActiveRemoteRequests() *types.ActiveRemoteRequests {
	return &types.ActiveRemoteRequests{
		MXCToResult: map[string]*types.RemoteRequestResult{},
	}
}
