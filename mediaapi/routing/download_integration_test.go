// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package routing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/element-hq/dendrite/mediaapi/types"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDownloadRequest_DoDownload_LocalFile tests downloading a local file
func TestDownloadRequest_DoDownload_LocalFile(t *testing.T) {
	t.Parallel()

	t.Run("download existing local file", func(t *testing.T) {
		t.Parallel()

		cfg, _ := testMediaConfig(t, 10000)
		db := testDatabase(t)

		// Store test media
		testData := []byte("test file content for download")
		mediaID := types.MediaID("test123")
		metadata := storeTestMedia(t, db, cfg, mediaID, testData)

		w := httptest.NewRecorder()
		dReq := &downloadRequest{
			MediaMetadata: &types.MediaMetadata{
				MediaID: mediaID,
				Origin:  cfg.Matrix.ServerName,
			},
			IsThumbnailRequest: false,
			Logger:             testLogger(),
		}

		activeThumbnailGen := testActiveThumbnailGeneration()
		activeRemoteReq := testActiveRemoteRequests()

		resultMetadata, err := dReq.doDownload(
			context.Background(),
			w,
			cfg,
			db,
			nil, // client
			activeRemoteReq,
			activeThumbnailGen,
		)

		require.NoError(t, err, "expected no error for existing file")
		require.NotNil(t, resultMetadata, "expected metadata to be returned")
		assert.Equal(t, metadata.MediaID, resultMetadata.MediaID)
		assert.Equal(t, metadata.Origin, resultMetadata.Origin)

		// Verify response
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, testData, w.Body.Bytes())
	})

	t.Run("download non-existent local file", func(t *testing.T) {
		t.Parallel()

		cfg, _ := testMediaConfig(t, 10000)
		db := testDatabase(t)

		w := httptest.NewRecorder()
		dReq := &downloadRequest{
			MediaMetadata: &types.MediaMetadata{
				MediaID: "nonexistent",
				Origin:  cfg.Matrix.ServerName,
			},
			IsThumbnailRequest: false,
			Logger:             testLogger(),
		}

		activeThumbnailGen := testActiveThumbnailGeneration()
		activeRemoteReq := testActiveRemoteRequests()

		resultMetadata, err := dReq.doDownload(
			context.Background(),
			w,
			cfg,
			db,
			nil, // client
			activeRemoteReq,
			activeThumbnailGen,
		)

		// Non-existent local file should return nil metadata, nil error
		assert.NoError(t, err, "expected no error for non-existent local file")
		assert.Nil(t, resultMetadata, "expected nil metadata for non-existent file")
	})
}

// TestDownloadRequest_RespondFromLocalFile tests the respondFromLocalFile function
func TestDownloadRequest_RespondFromLocalFile(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 10000)
	db := testDatabase(t)

	// Create test data
	testData := createTestPNG(t, 100, 100)
	mediaID := types.MediaID("testpng")
	metadata := storeTestMedia(t, db, cfg, mediaID, testData)

	tests := []struct {
		name               string
		isThumbnailRequest bool
		thumbnailWidth     int
		thumbnailHeight    int
		expectThumbnail    bool
	}{
		{
			name:               "download full-size image",
			isThumbnailRequest: false,
			expectThumbnail:    false,
		},
		{
			name:               "request thumbnail (should fallback to original)",
			isThumbnailRequest: true,
			thumbnailWidth:     32,
			thumbnailHeight:    32,
			expectThumbnail:    false, // No thumbnail pre-generated, fallback to original
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			dReq := &downloadRequest{
				MediaMetadata:      metadata,
				IsThumbnailRequest: tt.isThumbnailRequest,
				Logger:             testLogger(),
			}

			if tt.isThumbnailRequest {
				dReq.ThumbnailSize = types.ThumbnailSize{
					Width:        tt.thumbnailWidth,
					Height:       tt.thumbnailHeight,
					ResizeMethod: types.Scale,
				}
			}

			activeThumbnailGen := testActiveThumbnailGeneration()

			resultMetadata, err := dReq.respondFromLocalFile(
				context.Background(),
				w,
				cfg.AbsBasePath,
				activeThumbnailGen,
				cfg.MaxThumbnailGenerators,
				db,
				cfg.DynamicThumbnails,
				cfg.ThumbnailSizes,
			)

			require.NoError(t, err)
			require.NotNil(t, resultMetadata)
			assert.Equal(t, metadata.MediaID, resultMetadata.MediaID)

			// Verify response has data
			assert.Greater(t, w.Body.Len(), 0, "response should contain file data")
		})
	}
}

// TestDownloadRequest_RespondFromLocalFile_NonExistent tests error handling
func TestDownloadRequest_RespondFromLocalFile_NonExistent(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 10000)
	db := testDatabase(t)

	w := httptest.NewRecorder()
	dReq := &downloadRequest{
		MediaMetadata: &types.MediaMetadata{
			MediaID:     "fake",
			Origin:      cfg.Matrix.ServerName,
			Base64Hash:  "ZmFrZWhfZaXNoZGF0YQ==", // Invalid/non-existent hash
			ContentType: "text/plain",
		},
		IsThumbnailRequest: false,
		Logger:             testLogger(),
	}

	activeThumbnailGen := testActiveThumbnailGeneration()

	_, err := dReq.respondFromLocalFile(
		context.Background(),
		w,
		cfg.AbsBasePath,
		activeThumbnailGen,
		cfg.MaxThumbnailGenerators,
		db,
		cfg.DynamicThumbnails,
		cfg.ThumbnailSizes,
	)

	// Should error when trying to open non-existent file
	assert.Error(t, err)
	// Verify it's a file-related error
	var pathErr *os.PathError
	assert.True(t, errors.As(err, &pathErr), "Expected path error for non-existent file")
}

// TestDownload_HTTPHandler tests the Download HTTP handler function
func TestDownload_HTTPHandler(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 10000)
	db := testDatabase(t)

	// Store test media
	testData := []byte("test content for http handler")
	mediaID := types.MediaID("httptest")
	storeTestMedia(t, db, cfg, mediaID, testData)

	tests := []struct {
		name               string
		mediaID            types.MediaID
		origin             spec.ServerName
		isThumbnailRequest bool
		federationRequest  bool
		expectedStatus     int
		expectBody         bool
	}{
		{
			name:               "download local file via HTTP",
			mediaID:            mediaID,
			origin:             cfg.Matrix.ServerName,
			isThumbnailRequest: false,
			federationRequest:  false,
			expectedStatus:     http.StatusOK,
			expectBody:         true,
		},
		{
			name:               "download non-existent local file",
			mediaID:            "missing",
			origin:             cfg.Matrix.ServerName,
			isThumbnailRequest: false,
			federationRequest:  false,
			expectedStatus:     http.StatusNotFound,
			expectBody:         true, // Error response
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/_matrix/media/v3/download", nil)

			activeThumbnailGen := testActiveThumbnailGeneration()
			activeRemoteReq := testActiveRemoteRequests()

			Download(
				w,
				req,
				tt.origin,
				tt.mediaID,
				cfg,
				db,
				nil, // client
				nil, // fedClient
				activeRemoteReq,
				activeThumbnailGen,
				tt.isThumbnailRequest,
				"",  // customFilename
				tt.federationRequest,
			)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.expectBody {
				assert.Greater(t, w.Body.Len(), 0, "expected response body")
			}
		})
	}
}

// TestDownload_CustomFilename tests the custom filename functionality
func TestDownload_CustomFilename(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 10000)
	db := testDatabase(t)

	// Store test media
	testData := []byte("test content")
	mediaID := types.MediaID("customname")
	storeTestMedia(t, db, cfg, mediaID, testData)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/_matrix/media/v3/download/test.local/customname/myfile.txt", nil)

	Download(
		w,
		req,
		cfg.Matrix.ServerName,
		mediaID,
		cfg,
		db,
		nil,
		nil,
		testActiveRemoteRequests(),
		testActiveThumbnailGeneration(),
		false, // not thumbnail
		"myfile.txt",
		false, // not federation
	)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify Content-Disposition header includes custom filename
	contentDisposition := w.Header().Get("Content-Disposition")
	assert.Contains(t, contentDisposition, "myfile.txt", "custom filename should appear in Content-Disposition")
}

// TestDownload_FederationRequest tests federation request handling
func TestDownload_FederationRequest(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 10000)
	db := testDatabase(t)

	// Store test media
	testData := []byte("federation test content")
	mediaID := types.MediaID("fedtest")
	storeTestMedia(t, db, cfg, mediaID, testData)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/_matrix/federation/v1/media/download/"+string(mediaID), nil)

	Download(
		w,
		req,
		"", // empty origin for federation request
		mediaID,
		cfg,
		db,
		nil,
		nil,
		testActiveRemoteRequests(),
		testActiveThumbnailGeneration(),
		false, // not thumbnail
		"",
		true, // federation request
	)

	assert.Equal(t, http.StatusOK, w.Code)

	// Federation requests should return multipart responses
	contentType := w.Header().Get("Content-Type")
	assert.Contains(t, contentType, "multipart/mixed", "federation requests should return multipart/mixed")
}

// TestJsonErrorResponse tests the jsonErrorResponse method
func TestJsonErrorResponse(t *testing.T) {
	tests := []struct {
		name           string
		code           int
		errorType      string
		message        string
		expectedStatus int
	}{
		{
			name:           "not found error",
			code:           http.StatusNotFound,
			errorType:      "M_NOT_FOUND",
			message:        "File not found",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "forbidden error",
			code:           http.StatusForbidden,
			errorType:      "M_FORBIDDEN",
			message:        "Access denied",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			dReq := &downloadRequest{
				Logger: testLogger(),
			}

			dReq.jsonErrorResponse(w, util.JSONResponse{
				Code: tt.code,
				JSON: map[string]interface{}{
					"errcode": tt.errorType,
					"error":   tt.message,
				},
			})

			assert.Equal(t, tt.expectedStatus, w.Code)

			// Verify response is valid JSON
			body := w.Body.Bytes()
			assert.Greater(t, len(body), 0)
			assert.Contains(t, string(body), tt.errorType)
			assert.Contains(t, string(body), tt.message)
		})
	}
}

// Note: Content-Disposition header logic is covered by existing download tests
// The contentDispositionFor function has 100% coverage from download_validation_test.go
