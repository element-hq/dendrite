// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package routing

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/element-hq/dendrite/mediaapi/types"
	userapi "github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpload_HTTPHandler tests the Upload HTTP handler function
func TestUpload_HTTPHandler(t *testing.T) {
	tests := []struct {
		name           string
		setupRequest   func() *http.Request
		device         *userapi.Device
		expectedStatus int
		checkResponse  func(t *testing.T, code int, jsonResp interface{})
	}{
		{
			name: "successful file upload with content type header",
			setupRequest: func() *http.Request {
				body := bytes.NewReader([]byte("test file content"))
				req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", body)
				req.Header.Set("Content-Type", "text/plain")
				req.ContentLength = int64(len("test file content"))
				return req
			},
			device: &userapi.Device{
				UserID: "@alice:test.local",
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, code int, jsonResp interface{}) {
				resp, ok := jsonResp.(uploadResponse)
				require.True(t, ok, "response should be uploadResponse type")
				assert.Contains(t, resp.ContentURI, "mxc://", "content URI should be an mxc:// URL")
			},
		},
		{
			name: "successful upload with filename parameter",
			setupRequest: func() *http.Request {
				body := bytes.NewReader([]byte("test content with filename"))
				req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload?filename=test.txt", body)
				req.Header.Set("Content-Type", "text/plain")
				req.ContentLength = int64(len("test content with filename"))
				return req
			},
			device: &userapi.Device{
				UserID: "@bob:test.local",
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, code int, jsonResp interface{}) {
				resp, ok := jsonResp.(uploadResponse)
				require.True(t, ok, "response should be uploadResponse type")
				assert.Contains(t, resp.ContentURI, "mxc://")
			},
		},
		{
			name: "successful upload with image data",
			setupRequest: func() *http.Request {
				imageData := createTestPNG(t, 50, 50)
				body := bytes.NewReader(imageData)
				req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload?filename=test.png", body)
				req.Header.Set("Content-Type", "image/png")
				req.ContentLength = int64(len(imageData))
				return req
			},
			device: &userapi.Device{
				UserID: "@charlie:test.local",
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, code int, jsonResp interface{}) {
				resp, ok := jsonResp.(uploadResponse)
				require.True(t, ok, "response should be uploadResponse type")
				assert.Contains(t, resp.ContentURI, "mxc://")
			},
		},
		{
			name: "file size exceeds limit",
			setupRequest: func() *http.Request {
				// Create data larger than the configured limit
				largeData := make([]byte, 20000)
				body := bytes.NewReader(largeData)
				req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", body)
				req.Header.Set("Content-Type", "application/octet-stream")
				req.ContentLength = int64(len(largeData))
				return req
			},
			device: &userapi.Device{
				UserID: "@dave:test.local",
			},
			expectedStatus: http.StatusRequestEntityTooLarge,
			checkResponse: func(t *testing.T, code int, jsonResp interface{}) {
				matrixErr, ok := jsonResp.(spec.MatrixError)
				require.True(t, ok, "error response should be MatrixError type")
				assert.Contains(t, matrixErr.Err, "greater than the maximum", "error should mention size limit")
			},
		},
		{
			name: "filename starting with tilde is rejected",
			setupRequest: func() *http.Request {
				body := bytes.NewReader([]byte("test content"))
				req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload?filename=~badfile.txt", body)
				req.Header.Set("Content-Type", "text/plain")
				req.ContentLength = int64(len("test content"))
				return req
			},
			device: &userapi.Device{
				UserID: "@eve:test.local",
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, code int, jsonResp interface{}) {
				matrixErr, ok := jsonResp.(spec.MatrixError)
				require.True(t, ok, "error response should be MatrixError type")
				assert.Contains(t, matrixErr.Err, "must not begin with", "error should mention tilde restriction")
			},
		},
		{
			name: "empty file upload is allowed",
			setupRequest: func() *http.Request {
				body := bytes.NewReader([]byte(""))
				req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload?filename=empty.txt", body)
				req.Header.Set("Content-Type", "text/plain")
				req.ContentLength = 0
				return req
			},
			device: &userapi.Device{
				UserID: "@frank:test.local",
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, code int, jsonResp interface{}) {
				resp, ok := jsonResp.(uploadResponse)
				require.True(t, ok, "response should be uploadResponse type")
				assert.Contains(t, resp.ContentURI, "mxc://")
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh database for each test
			cfg, _ := testMediaConfig(t, 10000)
			db := testDatabase(t)
			activeThumbnailGen := testActiveThumbnailGeneration()

			// Disable thumbnail generation for tests to prevent async background
			// processes from interfering with test cleanup
			cfg.ThumbnailSizes = nil

			req := tt.setupRequest()

			// Call the Upload handler
			jsonResp := Upload(req, cfg, tt.device, db, activeThumbnailGen)

			assert.Equal(t, tt.expectedStatus, jsonResp.Code, "unexpected status code")
			if tt.checkResponse != nil {
				tt.checkResponse(t, jsonResp.Code, jsonResp.JSON)
			}
		})
	}
}

// TestParseAndValidateRequest_WithHTTPRequest tests parseAndValidateRequest with real HTTP requests
func TestParseAndValidateRequest_WithHTTPRequest(t *testing.T) {
	cfg, _ := testMediaConfig(t, 1000)

	tests := []struct {
		name           string
		setupRequest   func() *http.Request
		device         *userapi.Device
		expectError    bool
		errorContains  string
		expectedStatus int
	}{
		{
			name: "valid request with all fields",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload?filename=test.txt", strings.NewReader("content"))
				req.Header.Set("Content-Type", "text/plain")
				req.ContentLength = 7
				return req
			},
			device: &userapi.Device{
				UserID: "@alice:test.local",
			},
			expectError: false,
		},
		{
			name: "valid request without filename",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", strings.NewReader("content"))
				req.Header.Set("Content-Type", "application/octet-stream")
				req.ContentLength = 7
				return req
			},
			device: &userapi.Device{
				UserID: "@bob:test.local",
			},
			expectError: false,
		},
		{
			name: "request with filename needing URL encoding",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload?filename=test%20file.txt", strings.NewReader("content"))
				req.Header.Set("Content-Type", "text/plain")
				req.ContentLength = 7
				return req
			},
			device: &userapi.Device{
				UserID: "@charlie:test.local",
			},
			expectError: false,
		},
		{
			name: "file size in header exceeds limit",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", strings.NewReader("content"))
				req.Header.Set("Content-Type", "text/plain")
				req.ContentLength = 2000 // Exceeds 1000 byte limit
				return req
			},
			device: &userapi.Device{
				UserID: "@dave:test.local",
			},
			expectError:    true,
			errorContains:  "greater than the maximum",
			expectedStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name: "filename with tilde",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload?filename=~secret.txt", strings.NewReader("content"))
				req.Header.Set("Content-Type", "text/plain")
				req.ContentLength = 7
				return req
			},
			device: &userapi.Device{
				UserID: "@eve:test.local",
			},
			expectError:    true,
			errorContains:  "must not begin with",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "special characters in filename",
			setupRequest: func() *http.Request {
				filename := url.QueryEscape("file (1).txt")
				req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload?filename="+filename, strings.NewReader("content"))
				req.Header.Set("Content-Type", "text/plain")
				req.ContentLength = 7
				return req
			},
			device: &userapi.Device{
				UserID: "@frank:test.local",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupRequest()

			// Call parseAndValidateRequest
			uploadReq, errResp := parseAndValidateRequest(req, cfg, tt.device)

			if tt.expectError {
				require.NotNil(t, errResp, "expected error response")
				assert.Equal(t, tt.expectedStatus, errResp.Code, "unexpected error status code")
				if tt.errorContains != "" {
					matrixErr, ok := errResp.JSON.(spec.MatrixError)
					require.True(t, ok, "expected MatrixError type")
					assert.Contains(t, matrixErr.Err, tt.errorContains, "error should contain expected text")
				}
				assert.Nil(t, uploadReq, "upload request should be nil on error")
			} else {
				require.Nil(t, errResp, "expected no error response")
				require.NotNil(t, uploadReq, "upload request should not be nil")
				assert.NotNil(t, uploadReq.MediaMetadata, "metadata should be set")
				assert.Equal(t, cfg.Matrix.ServerName, uploadReq.MediaMetadata.Origin, "origin should match config")
				assert.Equal(t, types.MatrixUserID(tt.device.UserID), uploadReq.MediaMetadata.UserID, "user ID should match device")
			}
		})
	}
}

// TestUpload_MultipartFormData tests upload with multipart/form-data encoding
func TestUpload_MultipartFormData(t *testing.T) {
	cfg, _ := testMediaConfig(t, 10000)
	db := testDatabase(t)
	activeThumbnailGen := testActiveThumbnailGeneration()

	// Create a multipart form data request
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add file part
	part, err := writer.CreateFormFile("file", "upload.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("multipart form data content"))
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload?filename=upload.txt", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.ContentLength = int64(body.Len())

	device := &userapi.Device{
		UserID: "@multipart:test.local",
	}

	// Call Upload
	jsonResp := Upload(req, cfg, device, db, activeThumbnailGen)

	// Multipart requests should still work (content is read from body)
	assert.Equal(t, http.StatusOK, jsonResp.Code, "multipart upload should succeed")
}

// TestUpload_DuplicateContent tests that uploading the same content twice creates different media IDs
func TestUpload_DuplicateContent(t *testing.T) {
	cfg, _ := testMediaConfig(t, 10000)
	db := testDatabase(t)
	activeThumbnailGen := testActiveThumbnailGeneration()

	testContent := []byte("duplicate test content")

	// First upload
	req1 := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload?filename=file1.txt", bytes.NewReader(testContent))
	req1.Header.Set("Content-Type", "text/plain")
	req1.ContentLength = int64(len(testContent))

	device1 := &userapi.Device{
		UserID: "@user1:test.local",
	}

	jsonResp1 := Upload(req1, cfg, device1, db, activeThumbnailGen)
	assert.Equal(t, http.StatusOK, jsonResp1.Code, "first upload should succeed")

	// Second upload with same content
	req2 := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload?filename=file2.txt", bytes.NewReader(testContent))
	req2.Header.Set("Content-Type", "text/plain")
	req2.ContentLength = int64(len(testContent))

	device2 := &userapi.Device{
		UserID: "@user2:test.local",
	}

	jsonResp2 := Upload(req2, cfg, device2, db, activeThumbnailGen)
	assert.Equal(t, http.StatusOK, jsonResp2.Code, "second upload should succeed")

	// Both uploads should succeed and return different media IDs
	resp1, ok1 := jsonResp1.JSON.(uploadResponse)
	resp2, ok2 := jsonResp2.JSON.(uploadResponse)

	require.True(t, ok1, "first response should be uploadResponse")
	require.True(t, ok2, "second response should be uploadResponse")

	// Extract media IDs (they should be different even though content is the same)
	// The deduplication happens at the file level, but each upload gets a unique media ID
	assert.NotEqual(t, resp1.ContentURI, resp2.ContentURI, "responses should contain different media IDs")
}

// TestUpload_ContentTypeVariations tests various content types
func TestUpload_ContentTypeVariations(t *testing.T) {
	cfg, _ := testMediaConfig(t, 50000)
	db := testDatabase(t)
	activeThumbnailGen := testActiveThumbnailGeneration()

	// Disable thumbnail generation for tests to prevent async background
	// processes from interfering with test cleanup
	cfg.ThumbnailSizes = nil

	tests := []struct {
		name        string
		contentType string
		data        []byte
		filename    string
	}{
		{
			name:        "plain text",
			contentType: "text/plain",
			data:        []byte("plain text content"),
			filename:    "text.txt",
		},
		{
			name:        "json",
			contentType: "application/json",
			data:        []byte(`{"key": "value"}`),
			filename:    "data.json",
		},
		{
			name:        "png image",
			contentType: "image/png",
			data:        createTestPNG(t, 32, 32),
			filename:    "image.png",
		},
		{
			name:        "jpeg image",
			contentType: "image/jpeg",
			data:        createTestJPEG(t, 32, 32),
			filename:    "image.jpg",
		},
		{
			name:        "binary data",
			contentType: "application/octet-stream",
			data:        []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE},
			filename:    "binary.dat",
		},
		{
			name:        "no content type",
			contentType: "",
			data:        []byte("content without type"),
			filename:    "unknown.bin",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload?filename="+tt.filename, bytes.NewReader(tt.data))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			req.ContentLength = int64(len(tt.data))

			device := &userapi.Device{
				UserID: fmt.Sprintf("@%s:test.local", tt.name),
			}

			jsonResp := Upload(req, cfg, device, db, activeThumbnailGen)

			assert.Equal(t, http.StatusOK, jsonResp.Code, "upload should succeed for %s", tt.contentType)
			resp, ok := jsonResp.JSON.(uploadResponse)
			require.True(t, ok, "response should be uploadResponse type")
			assert.Contains(t, resp.ContentURI, "mxc://", "response should contain mxc:// URI")
		})
	}
}

// TestUpload_ZeroContentLength tests upload with Content-Length: 0
func TestUpload_ZeroContentLength(t *testing.T) {
	cfg, _ := testMediaConfig(t, 10000)
	db := testDatabase(t)
	activeThumbnailGen := testActiveThumbnailGeneration()

	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload?filename=empty.txt", bytes.NewReader([]byte("")))
	req.Header.Set("Content-Type", "text/plain")
	req.ContentLength = 0

	device := &userapi.Device{
		UserID: "@emptyfile:test.local",
	}

	jsonResp := Upload(req, cfg, device, db, activeThumbnailGen)

	assert.Equal(t, http.StatusOK, jsonResp.Code, "empty file upload should succeed")
	resp, ok := jsonResp.JSON.(uploadResponse)
	require.True(t, ok, "response should be uploadResponse type")
	assert.Contains(t, resp.ContentURI, "mxc://")
}

// TestUpload_UnlimitedFileSize tests upload with unlimited file size (MaxFileSizeBytes = 0)
func TestUpload_UnlimitedFileSize(t *testing.T) {
	// Create config with unlimited file size
	cfg, _ := testMediaConfig(t, 0) // 0 means unlimited
	db := testDatabase(t)
	activeThumbnailGen := testActiveThumbnailGeneration()

	// Create a large-ish file (10KB)
	largeData := make([]byte, 10240)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload?filename=large.bin", bytes.NewReader(largeData))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(largeData))

	device := &userapi.Device{
		UserID: "@unlimited:test.local",
	}

	jsonResp := Upload(req, cfg, device, db, activeThumbnailGen)

	assert.Equal(t, http.StatusOK, jsonResp.Code, "large file should upload when limit is unlimited")
}

// TestStoreFileAndMetadata_Errors tests error handling in storeFileAndMetadata
func TestStoreFileAndMetadata_Errors(t *testing.T) {
	cfg, _ := testMediaConfig(t, 10000)
	db := testDatabase(t)

	t.Run("invalid temp directory path", func(t *testing.T) {
		r := &uploadRequest{
			MediaMetadata: &types.MediaMetadata{
				MediaID:     "test123",
				Origin:      spec.ServerName("test.local"),
				ContentType: "text/plain",
				Base64Hash:  "invalidhash",
			},
			Logger: testLogger(),
		}

		activeThumbnailGen := testActiveThumbnailGeneration()

		// Try to store with invalid temp directory
		result := r.storeFileAndMetadata(
			context.Background(),
			"/nonexistent/temp/dir",
			cfg.AbsBasePath,
			db,
			cfg.ThumbnailSizes,
			activeThumbnailGen,
			cfg.MaxThumbnailGenerators,
		)

		require.NotNil(t, result, "should return error for invalid temp dir")
		assert.Equal(t, http.StatusBadRequest, result.Code)
	})
}

// TestDoUpload_DatabaseError tests doUpload behavior when database operations fail
func TestDoUpload_DatabaseError(t *testing.T) {
	// Note: This test is limited because we can't easily mock database failures
	// with the in-memory SQLite database. In a production test suite, you would
	// use a mock database interface.
	t.Skip("Skipping database error test - requires mock database interface")
}

// TestGenerateMediaID_ErrorHandling tests generateMediaID error scenarios
func TestGenerateMediaID_ErrorHandling(t *testing.T) {
	db := testDatabase(t)
	cfg, _ := testMediaConfig(t, 1000)

	r := &uploadRequest{
		MediaMetadata: &types.MediaMetadata{
			Origin: cfg.Matrix.ServerName,
		},
		Logger: testLogger(),
	}

	// Test successful generation multiple times to ensure uniqueness
	generatedIDs := make(map[types.MediaID]bool)
	for i := 0; i < 10; i++ {
		mediaID, err := r.generateMediaID(context.Background(), db)
		require.NoError(t, err, "should generate media ID successfully")
		assert.NotEmpty(t, mediaID)

		// Check for duplicates
		assert.False(t, generatedIDs[mediaID], "generated duplicate media ID")
		generatedIDs[mediaID] = true
	}
}

// TestUploadRequest_DoUpload_EdgeCases tests edge cases in doUpload
func TestUploadRequest_DoUpload_EdgeCases(t *testing.T) {
	t.Run("reader with error", func(t *testing.T) {
		cfg, _ := testMediaConfig(t, 1000)
		db := testDatabase(t)

		r := &uploadRequest{
			MediaMetadata: &types.MediaMetadata{
				Origin:     cfg.Matrix.ServerName,
				UploadName: "error.txt",
				UserID:     "@test:test.local",
			},
			Logger: testLogger(),
		}

		// Create a reader that returns an error
		errorReader := &errorReader{err: io.ErrUnexpectedEOF}
		activeThumbnailGen := testActiveThumbnailGeneration()

		result := r.doUpload(context.Background(), errorReader, cfg, db, activeThumbnailGen)

		require.NotNil(t, result, "should return error response")
		assert.Equal(t, http.StatusBadRequest, result.Code)
	})

	t.Run("max file size overflow protection", func(t *testing.T) {
		// Test the overflow protection when MaxFileSizeBytes + 1 overflows
		cfg, _ := testMediaConfig(t, 9223372036854775807) // Max int64
		db := testDatabase(t)

		r := &uploadRequest{
			MediaMetadata: &types.MediaMetadata{
				Origin:     cfg.Matrix.ServerName,
				UploadName: "test.txt",
				UserID:     "@test:test.local",
			},
			Logger: testLogger(),
		}

		reader := bytes.NewReader([]byte("test"))
		activeThumbnailGen := testActiveThumbnailGeneration()

		// This should trigger the overflow protection and use default max size
		result := r.doUpload(context.Background(), reader, cfg, db, activeThumbnailGen)

		// Should succeed with the default size being used
		assert.Nil(t, result, "should succeed with default max size")
	})
}

// errorReader is a test helper that returns an error when Read is called
type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}
