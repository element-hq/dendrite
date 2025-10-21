// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package routing

import (
	"net/http"
	"testing"

	"github.com/element-hq/dendrite/mediaapi/types"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadRequest_Validate(t *testing.T) {
	tests := []struct {
		name              string
		downloadRequest   *downloadRequest
		wantErr           bool
		expectedErrorCode int
		errorContains     string
	}{
		{
			name: "valid download request",
			downloadRequest: &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					MediaID: "validMediaID123",
					Origin:  "test.local",
				},
				IsThumbnailRequest: false,
				Logger:             testLogger(),
			},
			wantErr: false,
		},
		{
			name: "valid thumbnail request with scale method",
			downloadRequest: &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					MediaID: "validMediaID456",
					Origin:  "test.local",
				},
				IsThumbnailRequest: true,
				ThumbnailSize: types.ThumbnailSize{
					Width:        320,
					Height:       240,
					ResizeMethod: types.Scale,
				},
				Logger: testLogger(),
			},
			wantErr: false,
		},
		{
			name: "valid thumbnail request with crop method",
			downloadRequest: &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					MediaID: "validMediaID789",
					Origin:  "test.local",
				},
				IsThumbnailRequest: true,
				ThumbnailSize: types.ThumbnailSize{
					Width:        96,
					Height:       96,
					ResizeMethod: types.Crop,
				},
				Logger: testLogger(),
			},
			wantErr: false,
		},
		{
			name: "thumbnail request defaults to scale method when empty",
			downloadRequest: &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					MediaID: "validMediaID000",
					Origin:  "test.local",
				},
				IsThumbnailRequest: true,
				ThumbnailSize: types.ThumbnailSize{
					Width:        100,
					Height:       100,
					ResizeMethod: "", // Empty - should default to Scale
				},
				Logger: testLogger(),
			},
			wantErr: false,
			// Note: Validation should apply Scale as default for empty ResizeMethod
		},
		{
			name: "invalid media ID with spaces",
			downloadRequest: &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					MediaID: "invalid media id",
					Origin:  "test.local",
				},
				IsThumbnailRequest: false,
				Logger:             testLogger(),
			},
			wantErr:           true,
			expectedErrorCode: http.StatusNotFound,
			errorContains:     "mediaId must be",
		},
		{
			name: "invalid media ID with special characters",
			downloadRequest: &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					MediaID: "invalid@media#id",
					Origin:  "test.local",
				},
				IsThumbnailRequest: false,
				Logger:             testLogger(),
			},
			wantErr:           true,
			expectedErrorCode: http.StatusNotFound,
			errorContains:     "mediaId must be",
		},
		{
			name: "empty media ID",
			downloadRequest: &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					MediaID: "",
					Origin:  "test.local",
				},
				IsThumbnailRequest: false,
				Logger:             testLogger(),
			},
			wantErr:           true,
			expectedErrorCode: http.StatusNotFound,
			errorContains:     "non-empty string",
		},
		{
			name: "empty origin",
			downloadRequest: &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					MediaID: "validMediaID",
					Origin:  "",
				},
				IsThumbnailRequest: false,
				Logger:             testLogger(),
			},
			wantErr:           true,
			expectedErrorCode: http.StatusNotFound,
			errorContains:     "serverName must be",
		},
		{
			name: "thumbnail with zero width",
			downloadRequest: &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					MediaID: "validMediaID",
					Origin:  "test.local",
				},
				IsThumbnailRequest: true,
				ThumbnailSize: types.ThumbnailSize{
					Width:        0,
					Height:       100,
					ResizeMethod: types.Scale,
				},
				Logger: testLogger(),
			},
			wantErr:           true,
			expectedErrorCode: http.StatusBadRequest,
			errorContains:     "greater than 0",
		},
		{
			name: "thumbnail with negative height",
			downloadRequest: &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					MediaID: "validMediaID",
					Origin:  "test.local",
				},
				IsThumbnailRequest: true,
				ThumbnailSize: types.ThumbnailSize{
					Width:        100,
					Height:       -50,
					ResizeMethod: types.Scale,
				},
				Logger: testLogger(),
			},
			wantErr:           true,
			expectedErrorCode: http.StatusBadRequest,
			errorContains:     "greater than 0",
		},
		{
			name: "thumbnail with invalid resize method",
			downloadRequest: &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					MediaID: "validMediaID",
					Origin:  "test.local",
				},
				IsThumbnailRequest: true,
				ThumbnailSize: types.ThumbnailSize{
					Width:        100,
					Height:       100,
					ResizeMethod: "invalid",
				},
				Logger: testLogger(),
			},
			wantErr:           true,
			expectedErrorCode: http.StatusBadRequest,
			errorContains:     "crop or scale",
		},
		{
			name: "media ID with valid characters (letters, numbers, underscore, dash, equals)",
			downloadRequest: &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					MediaID: "Valid_Media-ID=123",
					Origin:  "test.local",
				},
				IsThumbnailRequest: false,
				Logger:             testLogger(),
			},
			wantErr: false,
		},
		{
			name: "large thumbnail dimensions are valid",
			downloadRequest: &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					MediaID: "validMediaID",
					Origin:  "test.local",
				},
				IsThumbnailRequest: true,
				ThumbnailSize: types.ThumbnailSize{
					Width:        3840,
					Height:       2160,
					ResizeMethod: types.Scale,
				},
				Logger: testLogger(),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tt.downloadRequest.Validate()

			if tt.wantErr {
				require.NotNil(t, result, "expected error response but got nil")
				assert.Equal(t, tt.expectedErrorCode, result.Code, "unexpected error code")
				if tt.errorContains != "" {
					// Check the error message
					matrixErr, ok := result.JSON.(spec.MatrixError)
					require.True(t, ok, "expected MatrixError type")
					assert.Contains(t, matrixErr.Err, tt.errorContains, "error message should contain expected text")
				}
			} else {
				assert.Nil(t, result, "expected no error but got: %v", result)
			}

			// Explicit verification: For the "defaults to scale" test case,
			// verify that validation applied the default ResizeMethod
			if tt.name == "thumbnail request defaults to scale method when empty" {
				assert.Equal(t, types.Scale, tt.downloadRequest.ThumbnailSize.ResizeMethod,
					"Validation should set ResizeMethod to Scale when empty")
			}
		})
	}
}

func TestMediaIDRegex(t *testing.T) {
	tests := []struct {
		name     string
		mediaID  string
		expected bool
	}{
		{"valid alphanumeric", "abc123XYZ", true},
		{"valid with underscore", "abc_123", true},
		{"valid with dash", "abc-123", true},
		{"valid with equals", "abc=123", true},
		{"valid all special chars", "aA0_-=", true},
		{"invalid with space", "abc 123", false},
		{"invalid with at sign", "abc@123", false},
		{"invalid with slash", "abc/123", false},
		{"invalid with dot", "abc.123", false},
		{"invalid with colon", "abc:123", false},
		{"invalid with hash", "abc#123", false},
		{"empty string", "", false},
		{"only numbers", "1234567890", true},
		{"only letters", "abcdefXYZ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := mediaIDRegex.MatchString(tt.mediaID)
			assert.Equal(t, tt.expected, result, "media ID regex match unexpected")
		})
	}
}

func TestContentDispositionFor(t *testing.T) {
	tests := []struct {
		name        string
		contentType types.ContentType
		expected    string
	}{
		{"empty content type", "", "attachment"},
		{"safe image/jpeg", "image/jpeg", "inline"},
		{"safe image/png", "image/png", "inline"},
		{"safe image/gif", "image/gif", "inline"},
		{"safe image/webp", "image/webp", "inline"},
		{"safe text/plain", "text/plain", "inline"},
		{"safe text/css", "text/css", "inline"},
		{"safe application/json", "application/json", "inline"},
		{"safe video/mp4", "video/mp4", "inline"},
		{"safe audio/mp4", "audio/mp4", "inline"},
		{"unsafe image/svg", "image/svg", "attachment"},
		{"unsafe application/pdf", "application/pdf", "attachment"},
		{"unsafe application/octet-stream", "application/octet-stream", "attachment"},
		{"unsafe text/html", "text/html", "attachment"},
		{"unsafe application/javascript", "application/javascript", "attachment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := contentDispositionFor(tt.contentType)
			assert.Equal(t, tt.expected, result, "content disposition unexpected")
		})
	}
}

func TestAllowInlineTypes(t *testing.T) {
	// Verify that all expected safe types are in the allow list
	safeTypes := []types.ContentType{
		"text/css",
		"text/plain",
		"text/csv",
		"application/json",
		"application/ld+json",
		"image/jpeg",
		"image/gif",
		"image/png",
		"image/apng",
		"image/webp",
		"image/avif",
		"video/mp4",
		"video/webm",
		"video/ogg",
		"video/quicktime",
		"audio/mp4",
		"audio/webm",
		"audio/aac",
		"audio/mpeg",
		"audio/ogg",
		"audio/wave",
		"audio/wav",
		"audio/x-wav",
		"audio/x-pn-wav",
		"audio/flac",
		"audio/x-flac",
	}

	for _, contentType := range safeTypes {
		t.Run(string(contentType), func(t *testing.T) {
			_, ok := allowInlineTypes[contentType]
			assert.True(t, ok, "expected %s to be in allowInlineTypes", contentType)
		})
	}

	// Verify that SVG is NOT in the allow list (security risk)
	_, ok := allowInlineTypes["image/svg+xml"]
	assert.False(t, ok, "SVG should not be in allowInlineTypes for security reasons")

	_, ok = allowInlineTypes["image/svg"]
	assert.False(t, ok, "SVG should not be in allowInlineTypes for security reasons")
}
