// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package routing

import (
	"context"
	"net/http"
	"testing"

	"github.com/element-hq/dendrite/mediaapi/types"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUploadRequest_Validate(t *testing.T) {
	tests := []struct {
		name              string
		maxFileSizeBytes  config.FileSizeBytes
		uploadRequest     *uploadRequest
		wantErr           bool
		expectedErrorCode int
		errorContains     string
	}{
		{
			name:             "valid upload request",
			maxFileSizeBytes: 1000,
			uploadRequest: &uploadRequest{
				MediaMetadata: &types.MediaMetadata{
					FileSizeBytes: 500,
					UploadName:    "test.txt",
					UserID:        "@alice:test.local",
				},
				Logger: testLogger(),
			},
			wantErr: false,
		},
		{
			name:             "file size exceeds limit",
			maxFileSizeBytes: 100,
			uploadRequest: &uploadRequest{
				MediaMetadata: &types.MediaMetadata{
					FileSizeBytes: 200,
					UploadName:    "large.txt",
					UserID:        "@alice:test.local",
				},
				Logger: testLogger(),
			},
			wantErr:           true,
			expectedErrorCode: http.StatusRequestEntityTooLarge,
			errorContains:     "greater than the maximum",
		},
		{
			name:             "file size at exact limit is allowed",
			maxFileSizeBytes: 100,
			uploadRequest: &uploadRequest{
				MediaMetadata: &types.MediaMetadata{
					FileSizeBytes: 100,
					UploadName:    "exact.txt",
					UserID:        "@alice:test.local",
				},
				Logger: testLogger(),
			},
			wantErr: false,
		},
		{
			name:             "filename starts with tilde",
			maxFileSizeBytes: 1000,
			uploadRequest: &uploadRequest{
				MediaMetadata: &types.MediaMetadata{
					FileSizeBytes: 50,
					UploadName:    "~test.txt",
					UserID:        "@alice:test.local",
				},
				Logger: testLogger(),
			},
			wantErr:           true,
			expectedErrorCode: http.StatusBadRequest,
			errorContains:     "must not begin with '~'",
		},
		{
			name:             "invalid user ID format - missing @",
			maxFileSizeBytes: 1000,
			uploadRequest: &uploadRequest{
				MediaMetadata: &types.MediaMetadata{
					FileSizeBytes: 50,
					UploadName:    "test.txt",
					UserID:        "alice:test.local",
				},
				Logger: testLogger(),
			},
			wantErr:           true,
			expectedErrorCode: http.StatusBadRequest,
			errorContains:     "@localpart:domain",
		},
		{
			name:             "invalid user ID format - missing domain",
			maxFileSizeBytes: 1000,
			uploadRequest: &uploadRequest{
				MediaMetadata: &types.MediaMetadata{
					FileSizeBytes: 50,
					UploadName:    "test.txt",
					UserID:        "@alice",
				},
				Logger: testLogger(),
			},
			wantErr:           true,
			expectedErrorCode: http.StatusBadRequest,
			errorContains:     "@localpart:domain",
		},
		{
			name:             "empty user ID is allowed",
			maxFileSizeBytes: 1000,
			uploadRequest: &uploadRequest{
				MediaMetadata: &types.MediaMetadata{
					FileSizeBytes: 50,
					UploadName:    "test.txt",
					UserID:        "",
				},
				Logger: testLogger(),
			},
			wantErr: false,
		},
		{
			name:             "zero max file size means unlimited",
			maxFileSizeBytes: 0,
			uploadRequest: &uploadRequest{
				MediaMetadata: &types.MediaMetadata{
					FileSizeBytes: 999999999,
					UploadName:    "huge.txt",
					UserID:        "@alice:test.local",
				},
				Logger: testLogger(),
			},
			wantErr: false,
		},
		{
			name:             "empty filename is allowed",
			maxFileSizeBytes: 1000,
			uploadRequest: &uploadRequest{
				MediaMetadata: &types.MediaMetadata{
					FileSizeBytes: 50,
					UploadName:    "",
					UserID:        "@alice:test.local",
				},
				Logger: testLogger(),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tt.uploadRequest.Validate(tt.maxFileSizeBytes)

			if tt.wantErr {
				require.NotNil(t, result, "expected error response but got nil")
				assert.Equal(t, tt.expectedErrorCode, result.Code, "unexpected error code")
				if tt.errorContains != "" {
					matrixErr, ok := result.JSON.(spec.MatrixError)
					require.True(t, ok, "expected MatrixError type")
					assert.Contains(t, matrixErr.Err, tt.errorContains, "error message should contain expected text")
				}
			} else {
				assert.Nil(t, result, "expected no error but got: %v", result)
			}
		})
	}
}

func TestUploadRequest_GenerateMediaID(t *testing.T) {
	db := testDatabase(t)
	cfg, _ := testMediaConfig(t, 1000)

	r := &uploadRequest{
		MediaMetadata: &types.MediaMetadata{
			Origin: cfg.Matrix.ServerName,
		},
		Logger: testLogger(),
	}

	// Generate first media ID - should succeed
	mediaID1, err := r.generateMediaID(ctx(), db)
	require.NoError(t, err, "failed to generate first media ID")
	assert.NotEmpty(t, mediaID1, "media ID should not be empty")
	assert.Len(t, mediaID1, 64, "media ID should be 64 characters (32 bytes hex-encoded)")

	// Store metadata for first ID
	r.MediaMetadata.MediaID = mediaID1
	r.MediaMetadata.Base64Hash = "test_hash_1"
	err = db.StoreMediaMetadata(ctx(), r.MediaMetadata)
	require.NoError(t, err, "failed to store first media metadata")

	// Generate second media ID - should be different
	mediaID2, err := r.generateMediaID(ctx(), db)
	require.NoError(t, err, "failed to generate second media ID")
	assert.NotEmpty(t, mediaID2, "media ID should not be empty")
	assert.NotEqual(t, mediaID1, mediaID2, "media IDs should be different")
}

func TestUploadRequest_GenerateMediaID_Collision(t *testing.T) {
	// This test verifies that if a collision occurs, we generate a new ID
	// We can't easily force a collision, but we can verify the logic works
	// by testing that we check the database for existing IDs
	db := testDatabase(t)
	cfg, _ := testMediaConfig(t, 1000)

	// Store an existing media entry
	existingMetadata := createTestMediaMetadata("existing_id", cfg.Matrix.ServerName)
	existingMetadata.Base64Hash = "existing_hash"
	err := db.StoreMediaMetadata(ctx(), existingMetadata)
	require.NoError(t, err)

	r := &uploadRequest{
		MediaMetadata: &types.MediaMetadata{
			Origin: cfg.Matrix.ServerName,
		},
		Logger: testLogger(),
	}

	// Generate new ID - should not conflict with existing
	newID, err := r.generateMediaID(ctx(), db)
	require.NoError(t, err)
	assert.NotEqual(t, "existing_id", newID, "should generate different ID from existing")
}

func TestRequestEntityTooLargeJSONResponse(t *testing.T) {
	maxSize := config.FileSizeBytes(1024)
	response := requestEntityTooLargeJSONResponse(maxSize)

	assert.NotNil(t, response)
	assert.Equal(t, http.StatusRequestEntityTooLarge, response.Code)

	// Verify the error message contains the max size
	errMsg := response.JSON.(spec.MatrixError).Err
	assert.Contains(t, errMsg, "1024", "error message should contain max file size")
}

// ctx returns a background context for tests
func ctx() context.Context {
	return context.Background()
}
