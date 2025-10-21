// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package routing

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/element-hq/dendrite/roomserver/api"
	"github.com/element-hq/dendrite/roomserver/types"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRoomserverAPI is a minimal mock for testing handleInviteResult
type mockRoomserverAPI struct {
	api.FederationRoomserverAPI
	handleInviteErr error
}

func (m *mockRoomserverAPI) HandleInvite(ctx context.Context, event *types.HeaderedEvent) error {
	return m.handleInviteErr
}

// mockPDU is a minimal mock PDU for testing
type mockPDU struct {
	gomatrixserverlib.PDU
}

// TestHandleInviteResult_NilError tests successful invite handling
func TestHandleInviteResult_NilError(t *testing.T) {
	t.Parallel()

	mockRSAPI := &mockRoomserverAPI{}
	mockEvent := &mockPDU{}

	event, resp := handleInviteResult(context.Background(), mockEvent, nil, mockRSAPI)

	assert.NotNil(t, event, "Should return event on success")
	assert.Nil(t, resp, "Should return nil JSONResponse on success")
}

// TestHandleInviteResult_InternalServerError tests InternalServerError handling
func TestHandleInviteResult_InternalServerError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		wantCode int
	}{
		{
			name:     "basic internal server error",
			err:      spec.InternalServerError{},
			wantCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockRSAPI := &mockRoomserverAPI{}

			// Call actual production function
			event, resp := handleInviteResult(context.Background(), nil, tt.err, mockRSAPI)

			assert.Nil(t, event, "Should return nil event on error")
			require.NotNil(t, resp, "Should return JSONResponse on error")
			assert.Equal(t, tt.wantCode, resp.Code, "HTTP status should match")

			// Verify response is InternalServerError
			_, ok := resp.JSON.(spec.InternalServerError)
			assert.True(t, ok, "Response should be spec.InternalServerError type")
		})
	}
}

// TestHandleInviteResult_MatrixError tests MatrixError handling and HTTP status code mapping
func TestHandleInviteResult_MatrixError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		matrixError     spec.MatrixError
		expectedCode    int
		expectedErrCode spec.MatrixErrorCode
	}{
		{
			name: "forbidden error maps to 403",
			matrixError: spec.MatrixError{
				ErrCode: spec.ErrorForbidden,
				Err:     "Access denied",
			},
			expectedCode:    http.StatusForbidden,
			expectedErrCode: spec.ErrorForbidden,
		},
		{
			name: "unsupported room version maps to 400",
			matrixError: spec.MatrixError{
				ErrCode: spec.ErrorUnsupportedRoomVersion,
				Err:     "Room version not supported",
			},
			expectedCode:    http.StatusBadRequest,
			expectedErrCode: spec.ErrorUnsupportedRoomVersion,
		},
		{
			name: "bad JSON error maps to 400",
			matrixError: spec.MatrixError{
				ErrCode: spec.ErrorBadJSON,
				Err:     "Invalid JSON",
			},
			expectedCode:    http.StatusBadRequest,
			expectedErrCode: spec.ErrorBadJSON,
		},
		{
			name: "unknown error code defaults to 500",
			matrixError: spec.MatrixError{
				ErrCode: "M_UNKNOWN",
				Err:     "Unknown error",
			},
			expectedCode:    http.StatusInternalServerError,
			expectedErrCode: "M_UNKNOWN",
		},
		{
			name: "custom error code defaults to 500",
			matrixError: spec.MatrixError{
				ErrCode: "M_CUSTOM_ERROR",
				Err:     "Custom error",
			},
			expectedCode:    http.StatusInternalServerError,
			expectedErrCode: "M_CUSTOM_ERROR",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockRSAPI := &mockRoomserverAPI{}

			// Call actual production function
			event, resp := handleInviteResult(context.Background(), nil, tt.matrixError, mockRSAPI)

			assert.Nil(t, event, "Should return nil event on error")
			require.NotNil(t, resp, "Should return JSONResponse on error")
			assert.Equal(t, tt.expectedCode, resp.Code, "HTTP status code should match expected")

			// Verify MatrixError is returned correctly
			matrixErr, ok := resp.JSON.(spec.MatrixError)
			require.True(t, ok, "Response should be spec.MatrixError type, got %T", resp.JSON)
			assert.Equal(t, tt.expectedErrCode, matrixErr.ErrCode, "Matrix error code should match")
		})
	}
}

// TestHandleInviteResult_UnknownError tests handling of generic errors
func TestHandleInviteResult_UnknownError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		wantCode int
	}{
		{
			name:     "standard error",
			err:      errors.New("something went wrong"),
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "wrapped error",
			err:      errors.New("root cause error"),
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockRSAPI := &mockRoomserverAPI{}

			// Call actual production function
			event, resp := handleInviteResult(context.Background(), nil, tt.err, mockRSAPI)

			assert.Nil(t, event, "Should return nil event on error")
			require.NotNil(t, resp, "Should return JSONResponse on error")
			assert.Equal(t, tt.wantCode, resp.Code, "HTTP status should be BadRequest for unknown errors")

			// Verify it returns spec.Unknown error
			matrixErr, ok := resp.JSON.(spec.MatrixError)
			require.True(t, ok, "Response should be spec.MatrixError type")
			assert.Equal(t, "unknown error", matrixErr.Err, "Should return 'unknown error' message")
		})
	}
}

// TestHandleInviteResult_ErrorCodeMapping tests the specific HTTP status code mappings
// This ensures the switch statement in handleInviteResult correctly maps error codes to HTTP statuses
func TestHandleInviteResult_ErrorCodeMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		matrixErrCode spec.MatrixErrorCode
		expectedHTTP  int
	}{
		{spec.ErrorForbidden, http.StatusForbidden},
		{spec.ErrorUnsupportedRoomVersion, http.StatusBadRequest},
		{spec.ErrorBadJSON, http.StatusBadRequest},
		{"M_UNKNOWN_CODE", http.StatusInternalServerError}, // Default case
		{"M_CUSTOM_ERROR", http.StatusInternalServerError}, // Default case
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.matrixErrCode), func(t *testing.T) {
			t.Parallel()

			mockRSAPI := &mockRoomserverAPI{}
			err := spec.MatrixError{
				ErrCode: tt.matrixErrCode,
				Err:     "test error",
			}

			// Call actual production function
			_, resp := handleInviteResult(context.Background(), nil, err, mockRSAPI)

			require.NotNil(t, resp, "Should return JSONResponse on error")
			assert.Equal(t, tt.expectedHTTP, resp.Code,
				"Error code %s should map to HTTP %d", tt.matrixErrCode, tt.expectedHTTP)
		})
	}
}

// TestHandleInviteResult_RoomserverAPIError tests error handling when roomserver.HandleInvite fails
func TestHandleInviteResult_RoomserverAPIError(t *testing.T) {
	t.Parallel()

	mockRSAPI := &mockRoomserverAPI{
		handleInviteErr: errors.New("roomserver error"),
	}
	mockEvent := &mockPDU{}

	// Call with nil error (success from handleInvite), but roomserver should fail
	event, resp := handleInviteResult(context.Background(), mockEvent, nil, mockRSAPI)

	assert.Nil(t, event, "Should return nil event when roomserver fails")
	require.NotNil(t, resp, "Should return JSONResponse when roomserver fails")
	assert.Equal(t, http.StatusInternalServerError, resp.Code, "Should return 500 on roomserver error")

	// Verify it returns InternalServerError
	_, ok := resp.JSON.(spec.InternalServerError)
	assert.True(t, ok, "Response should be spec.InternalServerError type")
}

// TODO: Add integration test for full /invite endpoint flow
// This unit test covers only error mapping logic; full handler testing requires:
// - HTTP server setup with federation request signing
// - Signature verification with key server
// - Roomserver API mocking for state queries
// - Database transaction handling
// - Complex invite state validation
