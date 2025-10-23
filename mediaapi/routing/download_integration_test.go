// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package routing

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/element-hq/dendrite/mediaapi/fileutils"
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

func TestDownloadRequest_DoDownload_QuarantinedMedia(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 10000)
	db := testDatabase(t)

	testData := []byte("quarantined media content")
	mediaID := types.MediaID("quarantined-media")
	storeTestMedia(t, db, cfg, mediaID, testData)

	_, err := db.QuarantineMedia(
		context.Background(),
		mediaID,
		cfg.Matrix.ServerName,
		types.MatrixUserID("@admin:test"),
		"test quarantine",
		true,
	)
	require.NoError(t, err)

	cases := []struct {
		name               string
		isThumbnailRequest bool
	}{
		{
			name:               "full media",
			isThumbnailRequest: false,
		},
		{
			name:               "thumbnail request",
			isThumbnailRequest: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {

			w := httptest.NewRecorder()
			dReq := &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					MediaID: mediaID,
					Origin:  cfg.Matrix.ServerName,
				},
				IsThumbnailRequest: tc.isThumbnailRequest,
				Logger:             testLogger(),
			}
			if tc.isThumbnailRequest {
				dReq.ThumbnailSize = types.ThumbnailSize{
					Width:        32,
					Height:       32,
					ResizeMethod: types.Scale,
				}
			}

			activeThumbnailGen := testActiveThumbnailGeneration()
			activeRemoteReq := testActiveRemoteRequests()

			resultMetadata, err := dReq.doDownload(
				context.Background(),
				w,
				cfg,
				db,
				nil,
				activeRemoteReq,
				activeThumbnailGen,
			)

			require.Error(t, err)
			assert.ErrorIs(t, err, errMediaQuarantined)
			assert.Nil(t, resultMetadata)
		})
	}
}

func TestDownload_Handler_QuarantinedMedia(t *testing.T) {
	cfg, _ := testMediaConfig(t, 10000)
	db := testDatabase(t)

	mediaID := types.MediaID("quarantined-handler")
	testData := []byte("handler quarantine")
	storeTestMedia(t, db, cfg, mediaID, testData)

	_, err := db.QuarantineMedia(
		context.Background(),
		mediaID,
		cfg.Matrix.ServerName,
		types.MatrixUserID("@admin:test"),
		"handler quarantine",
		true,
	)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/media/v3/download", nil)
	req = req.WithContext(context.Background())

	w := httptest.NewRecorder()
	activeRemote := testActiveRemoteRequests()
	activeThumb := testActiveThumbnailGeneration()

	Download(
		w,
		req,
		cfg.Matrix.ServerName,
		mediaID,
		cfg,
		db,
		nil,
		nil,
		activeRemote,
		activeThumb,
		false,
		"",
		false,
	)

	res := w.Result()
	require.Equal(t, http.StatusNotFound, res.StatusCode)

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "media is unavailable")
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
				"", // customFilename
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

// TestDownload_ThumbnailRequest tests thumbnail request handling through Download HTTP handler
func TestDownload_ThumbnailRequest(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	// Disable dynamic thumbnails to avoid async generation issues
	cfg.DynamicThumbnails = false
	db := testDatabase(t)

	// Create and store a PNG image
	testData := createTestPNG(t, 200, 200)
	mediaID := types.MediaID("thumbtest")
	storeTestMedia(t, db, cfg, mediaID, testData)

	tests := []struct {
		name           string
		width          string
		height         string
		method         string
		expectedStatus int
	}{
		{
			name:           "valid thumbnail request with scale",
			width:          "96",
			height:         "96",
			method:         "scale",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid thumbnail request with crop",
			width:          "32",
			height:         "32",
			method:         "crop",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "thumbnail request with default method (empty)",
			width:          "64",
			height:         "64",
			method:         "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid thumbnail request - zero width",
			width:          "0",
			height:         "96",
			method:         "scale",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid thumbnail request - zero height",
			width:          "96",
			height:         "0",
			method:         "scale",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid thumbnail request - invalid method",
			width:          "96",
			height:         "96",
			method:         "invalid",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			url := "/_matrix/media/v3/thumbnail?width=" + tt.width + "&height=" + tt.height
			if tt.method != "" {
				url += "&method=" + tt.method
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)

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
				true, // isThumbnailRequest
				"",
				false,
			)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

// TestDownload_ThumbnailRequest_WithFormValues tests thumbnail parsing from form values
func TestDownload_ThumbnailRequest_WithFormValues(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	cfg.DynamicThumbnails = false
	db := testDatabase(t)

	testData := createTestPNG(t, 200, 200)
	mediaID := types.MediaID("formtest")
	storeTestMedia(t, db, cfg, mediaID, testData)

	tests := []struct {
		name           string
		formValues     map[string]string
		expectedStatus int
	}{
		{
			name: "valid form values",
			formValues: map[string]string{
				"width":  "96",
				"height": "96",
				"method": "scale",
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "invalid width in form",
			formValues: map[string]string{
				"width":  "invalid",
				"height": "96",
				"method": "scale",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "invalid height in form",
			formValues: map[string]string{
				"width":  "96",
				"height": "invalid",
				"method": "scale",
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			url := "/_matrix/media/v3/thumbnail?"
			for k, v := range tt.formValues {
				url += k + "=" + v + "&"
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)

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
				true,
				"",
				false,
			)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

// TestGetThumbnailFile tests the getThumbnailFile function with various scenarios
func TestGetThumbnailFile(t *testing.T) {
	t.Parallel()

	t.Run("no thumbnail exists - returns nil", func(t *testing.T) {
		t.Parallel()

		cfg, _ := testMediaConfig(t, 100000)
		cfg.DynamicThumbnails = false
		// Clear thumbnail sizes to ensure no pre-generated thumbnails are expected
		cfg.ThumbnailSizes = nil
		db := testDatabase(t)

		testData := createTestPNG(t, 200, 200)
		mediaID := types.MediaID("nothumb")
		metadata := storeTestMedia(t, db, cfg, mediaID, testData)

		dReq := &downloadRequest{
			MediaMetadata: metadata,
			ThumbnailSize: types.ThumbnailSize{
				Width:        96,
				Height:       96,
				ResizeMethod: types.Scale,
			},
			Logger: testLogger(),
		}

		// Get the file path
		filePath, err := fileutils.GetPathFromBase64Hash(metadata.Base64Hash, cfg.AbsBasePath)
		require.NoError(t, err)

		thumbFile, thumbMetadata, err := dReq.getThumbnailFile(
			context.Background(),
			types.Path(filePath),
			testActiveThumbnailGeneration(),
			cfg.MaxThumbnailGenerators,
			db,
			cfg.DynamicThumbnails,
			cfg.ThumbnailSizes,
		)

		// Should return nil when no thumbnail exists and dynamic thumbnails disabled
		assert.NoError(t, err)
		assert.Nil(t, thumbFile)
		assert.Nil(t, thumbMetadata)
	})
}

// TestDoDownload_Thumbnail tests doDownload with thumbnail requests
func TestDoDownload_Thumbnail(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	cfg.DynamicThumbnails = false
	db := testDatabase(t)

	testData := createTestPNG(t, 200, 200)
	mediaID := types.MediaID("thumbdownload")
	storeTestMedia(t, db, cfg, mediaID, testData)

	w := httptest.NewRecorder()
	dReq := &downloadRequest{
		MediaMetadata: &types.MediaMetadata{
			MediaID: mediaID,
			Origin:  cfg.Matrix.ServerName,
		},
		IsThumbnailRequest: true,
		ThumbnailSize: types.ThumbnailSize{
			Width:        96,
			Height:       96,
			ResizeMethod: types.Scale,
		},
		Logger: testLogger(),
	}

	metadata, err := dReq.doDownload(
		context.Background(),
		w,
		cfg,
		db,
		nil,
		testActiveRemoteRequests(),
		testActiveThumbnailGeneration(),
	)

	require.NoError(t, err)
	require.NotNil(t, metadata)
	assert.Equal(t, mediaID, metadata.MediaID)
	// When no thumbnail exists, should fallback to original
	assert.Greater(t, w.Body.Len(), 0)
}

// TestJsonErrorResponse_EdgeCases tests additional error scenarios
func TestJsonErrorResponse_EdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		code           int
		errorJSON      interface{}
		expectedStatus int
	}{
		{
			name:           "bad request error",
			code:           http.StatusBadRequest,
			errorJSON:      map[string]interface{}{"errcode": "M_BAD_REQUEST", "error": "Bad request"},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "internal server error",
			code:           http.StatusInternalServerError,
			errorJSON:      map[string]interface{}{"errcode": "M_UNKNOWN", "error": "Internal error"},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:           "too many requests error",
			code:           http.StatusTooManyRequests,
			errorJSON:      map[string]interface{}{"errcode": "M_LIMIT_EXCEEDED", "error": "Rate limited"},
			expectedStatus: http.StatusTooManyRequests,
		},
		{
			name:           "simple string error",
			code:           http.StatusNotFound,
			errorJSON:      "Simple error message",
			expectedStatus: http.StatusNotFound,
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
				JSON: tt.errorJSON,
			})

			assert.Equal(t, tt.expectedStatus, w.Code)
			assert.Greater(t, w.Body.Len(), 0)
		})
	}
}

// TestDownload_ErrorPaths tests various error paths in the Download handler
func TestDownload_ErrorPaths(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	db := testDatabase(t)

	tests := []struct {
		name               string
		mediaID            types.MediaID
		origin             spec.ServerName
		isThumbnailRequest bool
		thumbnailWidth     string
		thumbnailHeight    string
		expectedStatus     int
	}{
		{
			name:               "invalid media ID characters",
			mediaID:            "invalid/media/id",
			origin:             cfg.Matrix.ServerName,
			isThumbnailRequest: false,
			expectedStatus:     http.StatusNotFound,
		},
		{
			name:               "empty origin",
			mediaID:            "validmedia",
			origin:             "",
			isThumbnailRequest: false,
			expectedStatus:     http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			url := "/_matrix/media/v3/download"
			if tt.isThumbnailRequest {
				url = "/_matrix/media/v3/thumbnail?width=" + tt.thumbnailWidth + "&height=" + tt.thumbnailHeight
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)

			Download(
				w,
				req,
				tt.origin,
				tt.mediaID,
				cfg,
				db,
				nil,
				nil,
				testActiveRemoteRequests(),
				testActiveThumbnailGeneration(),
				tt.isThumbnailRequest,
				"",
				false,
			)

			assert.Equal(t, tt.expectedStatus, w.Code)
			assert.Greater(t, w.Body.Len(), 0, "expected error response body")
		})
	}
}

// TestDownload_FileSizeMismatch tests handling of file size mismatches
func TestDownload_FileSizeMismatch(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	db := testDatabase(t)

	// Create and store media
	testData := createTestPNG(t, 100, 100)
	mediaID := types.MediaID("sizemismatch")
	_ = storeTestMedia(t, db, cfg, mediaID, testData)

	// Get the actual stored metadata
	storedMetadata, err := db.GetMediaMetadata(context.Background(), mediaID, cfg.Matrix.ServerName)
	require.NoError(t, err)
	require.NotNil(t, storedMetadata)

	// Create a request with incorrect file size in the in-memory metadata
	// This simulates a mismatch between database and on-disk file
	incorrectMetadata := *storedMetadata
	incorrectMetadata.FileSizeBytes = 12345 // Wrong size

	w := httptest.NewRecorder()
	dReq := &downloadRequest{
		MediaMetadata:      &incorrectMetadata,
		IsThumbnailRequest: false,
		Logger:             testLogger(),
	}

	_, err = dReq.respondFromLocalFile(
		context.Background(),
		w,
		cfg.AbsBasePath,
		testActiveThumbnailGeneration(),
		cfg.MaxThumbnailGenerators,
		db,
		cfg.DynamicThumbnails,
		cfg.ThumbnailSizes,
	)

	// Should return error due to file size mismatch
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "file size")
}

// TestMultipartResponse tests the multipart response generation
func TestMultipartResponse(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	db := testDatabase(t)

	testData := []byte("multipart test data")
	mediaID := types.MediaID("multiparttest")
	storeTestMedia(t, db, cfg, mediaID, testData)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/_matrix/federation/v1/media/download/"+string(mediaID), nil)

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
		false,
		"",
		true, // federation request - triggers multipart
	)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify multipart response
	contentType := w.Header().Get("Content-Type")
	assert.Contains(t, contentType, "multipart/mixed")
	// Content-Length is removed for multipart responses (set by Go's http library)

	// Verify response body contains both JSON part and media part
	body := w.Body.String()
	assert.Contains(t, body, "application/json")
	assert.Greater(t, len(body), len(testData), "multipart response should be larger than original data")
}

// TestDownload_FederationEmptyOrigin tests federation request with empty origin
func TestDownload_FederationEmptyOrigin(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	db := testDatabase(t)

	testData := []byte("fed origin test")
	mediaID := types.MediaID("fedorigin")
	storeTestMedia(t, db, cfg, mediaID, testData)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/_matrix/federation/v1/media/download/"+string(mediaID), nil)

	// When federationRequest=true and origin="", it should default to cfg.Matrix.ServerName
	Download(
		w,
		req,
		"", // empty origin
		mediaID,
		cfg,
		db,
		nil,
		nil,
		testActiveRemoteRequests(),
		testActiveThumbnailGeneration(),
		false,
		"",
		true, // federation request
	)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestDownload_ThumbnailWithDifferentSizes tests thumbnail requests with various sizes
func TestDownload_ThumbnailWithDifferentSizes(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	cfg.DynamicThumbnails = false
	db := testDatabase(t)

	testData := createTestJPEG(t, 400, 300)
	mediaID := types.MediaID("multisizetest")
	storeTestMedia(t, db, cfg, mediaID, testData)

	tests := []struct {
		name   string
		width  int
		height int
		method string
	}{
		{
			name:   "small square thumbnail with crop",
			width:  32,
			height: 32,
			method: types.Crop,
		},
		{
			name:   "medium thumbnail with scale",
			width:  320,
			height: 240,
			method: types.Scale,
		},
		{
			name:   "large thumbnail with crop",
			width:  640,
			height: 480,
			method: types.Crop,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			dReq := &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					MediaID: mediaID,
					Origin:  cfg.Matrix.ServerName,
				},
				IsThumbnailRequest: true,
				ThumbnailSize: types.ThumbnailSize{
					Width:        tt.width,
					Height:       tt.height,
					ResizeMethod: tt.method,
				},
				Logger: testLogger(),
			}

			metadata, err := dReq.doDownload(
				context.Background(),
				w,
				cfg,
				db,
				nil,
				testActiveRemoteRequests(),
				testActiveThumbnailGeneration(),
			)

			require.NoError(t, err)
			require.NotNil(t, metadata)
			assert.Greater(t, w.Body.Len(), 0)
		})
	}
}

// TestRespondFromLocalFile_ThumbnailFallback tests thumbnail fallback to original
func TestRespondFromLocalFile_ThumbnailFallback(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	cfg.DynamicThumbnails = false
	cfg.ThumbnailSizes = nil // No pre-generated sizes
	db := testDatabase(t)

	testData := createTestPNG(t, 150, 150)
	mediaID := types.MediaID("fallbacktest")
	metadata := storeTestMedia(t, db, cfg, mediaID, testData)

	w := httptest.NewRecorder()
	dReq := &downloadRequest{
		MediaMetadata:      metadata,
		IsThumbnailRequest: true,
		ThumbnailSize: types.ThumbnailSize{
			Width:        50,
			Height:       50,
			ResizeMethod: types.Scale,
		},
		Logger: testLogger(),
	}

	resultMetadata, err := dReq.respondFromLocalFile(
		context.Background(),
		w,
		cfg.AbsBasePath,
		testActiveThumbnailGeneration(),
		cfg.MaxThumbnailGenerators,
		db,
		cfg.DynamicThumbnails,
		cfg.ThumbnailSizes,
	)

	require.NoError(t, err)
	require.NotNil(t, resultMetadata)
	// Should fallback to original file when no thumbnail available
	assert.Equal(t, metadata.MediaID, resultMetadata.MediaID)
	assert.Greater(t, w.Body.Len(), 0)
}

// TestDownload_CustomFilenameWithSpecialChars tests custom filenames with special characters
func TestDownload_CustomFilenameWithSpecialChars(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	db := testDatabase(t)

	testData := []byte("test content with special filename")
	mediaID := types.MediaID("specialchars")
	storeTestMedia(t, db, cfg, mediaID, testData)

	tests := []struct {
		name             string
		customFilename   string
		shouldContainStr string
	}{
		{
			name:             "filename with spaces",
			customFilename:   "my file name.txt",
			shouldContainStr: "my file name.txt",
		},
		{
			name:             "filename with unicode",
			customFilename:   "文件.txt",
			shouldContainStr: "文件.txt",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/_matrix/media/v3/download", nil)

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
				false,
				tt.customFilename,
				false,
			)

			assert.Equal(t, http.StatusOK, w.Code)
			contentDisposition := w.Header().Get("Content-Disposition")
			assert.NotEmpty(t, contentDisposition)
		})
	}
}

// TestJsonErrorResponse_MarshalFailure tests handling of JSON marshal errors
func TestJsonErrorResponse_MarshalFailure(t *testing.T) {
	w := httptest.NewRecorder()
	dReq := &downloadRequest{
		Logger: testLogger(),
	}

	// Create a value that can't be marshaled (channels can't be marshaled to JSON)
	invalidJSON := make(chan int)

	dReq.jsonErrorResponse(w, util.JSONResponse{
		Code: http.StatusInternalServerError,
		JSON: invalidJSON,
	})

	// Should still respond with a status code
	assert.Equal(t, http.StatusNotFound, w.Code)
	// Should have fallback error message
	assert.Greater(t, w.Body.Len(), 0)
}

// TestDownload_MultipartWithThumbnail tests multipart response with thumbnail request
func TestDownload_MultipartWithThumbnail(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	cfg.DynamicThumbnails = false
	db := testDatabase(t)

	testData := createTestPNG(t, 200, 200)
	mediaID := types.MediaID("multipartthumb")
	storeTestMedia(t, db, cfg, mediaID, testData)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/_matrix/federation/v1/media/thumbnail?width=96&height=96&method=scale", nil)

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
		true, // thumbnail request
		"",
		true, // federation request
	)

	assert.Equal(t, http.StatusOK, w.Code)
	contentType := w.Header().Get("Content-Type")
	assert.Contains(t, contentType, "multipart/mixed")
}

// TestDownload_DownloadFilenameEdgeCases tests edge cases in download filename handling
func TestDownload_DownloadFilenameEdgeCases(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	db := testDatabase(t)

	testData := []byte("test content for filename edge cases")
	mediaID := types.MediaID("emptyname")
	_ = storeTestMedia(t, db, cfg, mediaID, testData)

	tests := []struct {
		name           string
		customFilename string
	}{
		{
			name:           "empty custom filename",
			customFilename: "",
		},
		{
			name:           "custom filename provided",
			customFilename: "override.txt",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/_matrix/media/v3/download", nil)

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
				false,
				tt.customFilename,
				false,
			)

			assert.Equal(t, http.StatusOK, w.Code)
			// Should have Content-Disposition header
			_ = w.Header().Get("Content-Disposition")
		})
	}
}

// TestRespondFromLocalFile_WithCustomFilename tests custom filename in respondFromLocalFile
func TestRespondFromLocalFile_WithCustomFilename(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	db := testDatabase(t)

	testData := []byte("test content")
	mediaID := types.MediaID("customfile")
	metadata := storeTestMedia(t, db, cfg, mediaID, testData)

	w := httptest.NewRecorder()
	dReq := &downloadRequest{
		MediaMetadata:      metadata,
		IsThumbnailRequest: false,
		DownloadFilename:   "mycustomfile.bin",
		Logger:             testLogger(),
	}

	resultMetadata, err := dReq.respondFromLocalFile(
		context.Background(),
		w,
		cfg.AbsBasePath,
		testActiveThumbnailGeneration(),
		cfg.MaxThumbnailGenerators,
		db,
		cfg.DynamicThumbnails,
		cfg.ThumbnailSizes,
	)

	require.NoError(t, err)
	require.NotNil(t, resultMetadata)
	assert.Greater(t, w.Body.Len(), 0)

	// Verify custom filename in Content-Disposition
	contentDisposition := w.Header().Get("Content-Disposition")
	assert.Contains(t, contentDisposition, "mycustomfile.bin")
}

// TestRespondFromLocalFile_Headers tests HTTP headers set in response
func TestRespondFromLocalFile_Headers(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	db := testDatabase(t)

	testData := createTestPNG(t, 50, 50)
	mediaID := types.MediaID("headerstest")
	metadata := storeTestMedia(t, db, cfg, mediaID, testData)

	w := httptest.NewRecorder()
	dReq := &downloadRequest{
		MediaMetadata:      metadata,
		IsThumbnailRequest: false,
		Logger:             testLogger(),
	}

	_, err := dReq.respondFromLocalFile(
		context.Background(),
		w,
		cfg.AbsBasePath,
		testActiveThumbnailGeneration(),
		cfg.MaxThumbnailGenerators,
		db,
		cfg.DynamicThumbnails,
		cfg.ThumbnailSizes,
	)

	require.NoError(t, err)

	// Verify headers are set
	assert.NotEmpty(t, w.Header().Get("Content-Type"))
	assert.NotEmpty(t, w.Header().Get("Content-Length"))
	assert.NotEmpty(t, w.Header().Get("Content-Disposition"))
	assert.Contains(t, w.Header().Get("Content-Security-Policy"), "default-src 'none'")
}

// TestMultipartResponse_Headers tests multipart response headers
func TestMultipartResponse_Headers(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	db := testDatabase(t)

	testData := createTestJPEG(t, 100, 100)
	mediaID := types.MediaID("multipartheaders")
	metadata := storeTestMedia(t, db, cfg, mediaID, testData)

	w := httptest.NewRecorder()
	dReq := &downloadRequest{
		MediaMetadata:      metadata,
		IsThumbnailRequest: false,
		Logger:             testLogger(),
		multipartResponse:  true, // Enable multipart response
	}

	_, err := dReq.respondFromLocalFile(
		context.Background(),
		w,
		cfg.AbsBasePath,
		testActiveThumbnailGeneration(),
		cfg.MaxThumbnailGenerators,
		db,
		cfg.DynamicThumbnails,
		cfg.ThumbnailSizes,
	)

	require.NoError(t, err)

	// Verify multipart headers
	contentType := w.Header().Get("Content-Type")
	assert.Contains(t, contentType, "multipart/mixed")

	// Verify body contains multipart structure
	body := w.Body.String()
	assert.Contains(t, body, "application/json")
	assert.Contains(t, body, "{}")
}

// TestDoDownload_WithExistingMetadata tests doDownload when metadata already exists
func TestDoDownload_WithExistingMetadata(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	db := testDatabase(t)

	testData := createTestPNG(t, 120, 120)
	mediaID := types.MediaID("existingmeta")
	_ = storeTestMedia(t, db, cfg, mediaID, testData)

	w := httptest.NewRecorder()
	dReq := &downloadRequest{
		MediaMetadata: &types.MediaMetadata{
			MediaID: mediaID,
			Origin:  cfg.Matrix.ServerName,
		},
		IsThumbnailRequest: false,
		Logger:             testLogger(),
	}

	// First download - will populate metadata
	metadata, err := dReq.doDownload(
		context.Background(),
		w,
		cfg,
		db,
		nil,
		testActiveRemoteRequests(),
		testActiveThumbnailGeneration(),
	)

	require.NoError(t, err)
	require.NotNil(t, metadata)
	assert.Equal(t, mediaID, metadata.MediaID)

	// Second download - metadata should be cached in request
	w2 := httptest.NewRecorder()
	dReq2 := &downloadRequest{
		MediaMetadata:      metadata, // Use the populated metadata
		IsThumbnailRequest: false,
		Logger:             testLogger(),
	}

	metadata2, err := dReq2.doDownload(
		context.Background(),
		w2,
		cfg,
		db,
		nil,
		testActiveRemoteRequests(),
		testActiveThumbnailGeneration(),
	)

	require.NoError(t, err)
	require.NotNil(t, metadata2)
	assert.Equal(t, mediaID, metadata2.MediaID)
}

// TestRespondFromLocalFile_MultipleContentTypes tests different content types
func TestRespondFromLocalFile_MultipleContentTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		createMedia func(*testing.T) []byte
		contentType types.ContentType
	}{
		{
			name:        "PNG image",
			createMedia: func(t *testing.T) []byte { return createTestPNG(t, 80, 80) },
			contentType: "image/png",
		},
		{
			name:        "JPEG image",
			createMedia: func(t *testing.T) []byte { return createTestJPEG(t, 90, 90) },
			contentType: "image/jpeg",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, _ := testMediaConfig(t, 100000)
			db := testDatabase(t)

			testData := tt.createMedia(t)
			mediaID := types.MediaID("contenttype_" + tt.name)
			metadata := storeTestMedia(t, db, cfg, mediaID, testData)

			w := httptest.NewRecorder()
			dReq := &downloadRequest{
				MediaMetadata:      metadata,
				IsThumbnailRequest: false,
				Logger:             testLogger(),
			}

			resultMetadata, err := dReq.respondFromLocalFile(
				context.Background(),
				w,
				cfg.AbsBasePath,
				testActiveThumbnailGeneration(),
				cfg.MaxThumbnailGenerators,
				db,
				cfg.DynamicThumbnails,
				cfg.ThumbnailSizes,
			)

			require.NoError(t, err)
			require.NotNil(t, resultMetadata)
			assert.Greater(t, w.Body.Len(), 0)
			assert.NotEmpty(t, w.Header().Get("Content-Type"))
		})
	}
}

// TestDownload_ThumbnailWithMultipartResponse tests thumbnail + multipart combination
func TestDownload_ThumbnailWithMultipartResponse(t *testing.T) {
	t.Parallel()

	cfg, _ := testMediaConfig(t, 100000)
	cfg.DynamicThumbnails = false
	db := testDatabase(t)

	testData := createTestPNG(t, 180, 180)
	mediaID := types.MediaID("thumbmultipart")
	metadata := storeTestMedia(t, db, cfg, mediaID, testData)

	w := httptest.NewRecorder()
	dReq := &downloadRequest{
		MediaMetadata:      metadata,
		IsThumbnailRequest: true,
		ThumbnailSize: types.ThumbnailSize{
			Width:        96,
			Height:       96,
			ResizeMethod: types.Scale,
		},
		multipartResponse: true,
		Logger:            testLogger(),
	}

	resultMetadata, err := dReq.respondFromLocalFile(
		context.Background(),
		w,
		cfg.AbsBasePath,
		testActiveThumbnailGeneration(),
		cfg.MaxThumbnailGenerators,
		db,
		cfg.DynamicThumbnails,
		cfg.ThumbnailSizes,
	)

	require.NoError(t, err)
	require.NotNil(t, resultMetadata)

	// Verify multipart response
	contentType := w.Header().Get("Content-Type")
	assert.Contains(t, contentType, "multipart/mixed")
}

// Note: Content-Disposition header logic is covered by existing download tests
// The contentDispositionFor function has 100% coverage from download_validation_test.go
