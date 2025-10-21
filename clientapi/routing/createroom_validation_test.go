// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package routing

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper functions to reduce duplication and improve test assertions

// assertValidationError verifies that validation failed with the expected error details
func assertValidationError(t *testing.T, result *util.JSONResponse, expectedCode int, expectedMsgContains string) {
	t.Helper()
	require.NotNil(t, result, "Expected validation to fail")
	assert.Equal(t, expectedCode, result.Code, "HTTP status code should match")

	errorBody, ok := result.JSON.(spec.MatrixError)
	require.True(t, ok, "Expected MatrixError, got %T", result.JSON)
	assert.Contains(t, errorBody.Err, expectedMsgContains, "Error message should contain expected text")
}

// assertValidationSuccess verifies that validation passed
func assertValidationSuccess(t *testing.T, result *util.JSONResponse) {
	t.Helper()
	assert.Nil(t, result, "Expected validation to pass, got error: %v", result)
}

// TestCreateRoomRequest_Validate tests the validation logic for createRoomRequest
// focusing on room alias names, user IDs, presets, and creation content.
func TestCreateRoomRequest_Validate_ValidRequest(t *testing.T) {
	t.Parallel()

	req := createRoomRequest{
		Name:          "Test Room",
		Topic:         "A test topic",
		RoomAliasName: "testroom",
		Preset:        spec.PresetPrivateChat,
		Invite:        []string{"@bob:localhost", "@alice:localhost"},
	}

	result := req.Validate()
	assertValidationSuccess(t, result)
}

func TestCreateRoomRequest_Validate_RoomAliasName_Whitespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		roomAliasName string
		shouldFail    bool
		errorMsg      string
	}{
		{
			name:          "tab character",
			roomAliasName: "test\troom",
			shouldFail:    true,
			errorMsg:      "room_alias_name cannot contain whitespace or ':'",
		},
		{
			name:          "newline character",
			roomAliasName: "test\nroom",
			shouldFail:    true,
			errorMsg:      "room_alias_name cannot contain whitespace or ':'",
		},
		{
			name:          "space character",
			roomAliasName: "test room",
			shouldFail:    true,
			errorMsg:      "room_alias_name cannot contain whitespace or ':'",
		},
		{
			name:          "colon character",
			roomAliasName: "test:room",
			shouldFail:    true,
			errorMsg:      "room_alias_name cannot contain whitespace or ':'",
		},
		{
			name:          "valid alias with hyphen",
			roomAliasName: "test-room",
			shouldFail:    false,
		},
		{
			name:          "valid alias with underscore",
			roomAliasName: "test_room",
			shouldFail:    false,
		},
		{
			name:          "empty alias name",
			roomAliasName: "",
			shouldFail:    false, // Empty is valid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := createRoomRequest{
				RoomAliasName: tt.roomAliasName,
				Preset:        spec.PresetPrivateChat,
			}

			result := req.Validate()

			if tt.shouldFail {
				assertValidationError(t, result, http.StatusBadRequest, tt.errorMsg)
			} else {
				assertValidationSuccess(t, result)
			}
		})
	}
}

func TestCreateRoomRequest_Validate_InviteUserIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		invite     []string
		shouldFail bool
		errorMsg   string
	}{
		{
			name:       "valid user IDs",
			invite:     []string{"@alice:example.com", "@bob:test.org"},
			shouldFail: false,
		},
		{
			name:       "invalid user ID - missing @",
			invite:     []string{"alice:example.com"},
			shouldFail: true,
			errorMsg:   "user id must be in the form @localpart:domain",
		},
		{
			name:       "invalid user ID - missing domain",
			invite:     []string{"@alice"},
			shouldFail: true,
			errorMsg:   "user id must be in the form @localpart:domain",
		},
		{
			name:       "invalid user ID - empty string",
			invite:     []string{""},
			shouldFail: true,
			errorMsg:   "user id must be in the form @localpart:domain",
		},
		{
			name:       "mixed valid and invalid",
			invite:     []string{"@alice:example.com", "invalid"},
			shouldFail: true,
			errorMsg:   "user id must be in the form @localpart:domain",
		},
		{
			name:       "empty invite list",
			invite:     []string{},
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := createRoomRequest{
				Invite: tt.invite,
				Preset: spec.PresetPrivateChat,
			}

			result := req.Validate()

			if tt.shouldFail {
				assertValidationError(t, result, http.StatusBadRequest, tt.errorMsg)
			} else {
				assertValidationSuccess(t, result)
			}
		})
	}
}

func TestCreateRoomRequest_Validate_Preset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		preset     string
		shouldFail bool
	}{
		{
			name:       "valid preset - private_chat",
			preset:     spec.PresetPrivateChat,
			shouldFail: false,
		},
		{
			name:       "valid preset - trusted_private_chat",
			preset:     spec.PresetTrustedPrivateChat,
			shouldFail: false,
		},
		{
			name:       "valid preset - public_chat",
			preset:     spec.PresetPublicChat,
			shouldFail: false,
		},
		{
			name:       "valid preset - empty (default)",
			preset:     "",
			shouldFail: false,
		},
		{
			name:       "invalid preset",
			preset:     "invalid_preset",
			shouldFail: true,
		},
		{
			name:       "invalid preset - random string",
			preset:     "foobar",
			shouldFail: true,
		},
		{
			name:       "invalid preset - wrong case (uppercase)",
			preset:     "PRIVATE_CHAT",
			shouldFail: true,
		},
		{
			name:       "invalid preset - wrong case (mixed)",
			preset:     "Private_Chat",
			shouldFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := createRoomRequest{
				Preset: tt.preset,
			}

			result := req.Validate()

			if tt.shouldFail {
				assertValidationError(t, result, http.StatusBadRequest, "preset must be any of")
			} else {
				assertValidationSuccess(t, result)
			}
		})
	}
}

func TestCreateRoomRequest_Validate_CreationContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		creationContent json.RawMessage
		shouldFail      bool
		errorMsg        string
	}{
		{
			name:            "valid creation content - empty",
			creationContent: json.RawMessage("{}"),
			shouldFail:      false,
		},
		{
			name:            "valid creation content with m.federate",
			creationContent: json.RawMessage(`{"m.federate": false}`),
			shouldFail:      false,
		},
		{
			name:            "malformed JSON - not closed",
			creationContent: json.RawMessage(`{"m.federate": false`),
			shouldFail:      true,
			errorMsg:        "malformed creation_content",
		},
		{
			name:            "malformed JSON - invalid syntax",
			creationContent: json.RawMessage(`{invalid json}`),
			shouldFail:      true,
			errorMsg:        "malformed creation_content",
		},
		{
			name:            "nil creation content",
			creationContent: nil,
			shouldFail:      false, // nil is treated as empty/valid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := createRoomRequest{
				CreationContent: tt.creationContent,
				Preset:          spec.PresetPrivateChat,
			}

			result := req.Validate()

			if tt.shouldFail {
				assertValidationError(t, result, http.StatusBadRequest, tt.errorMsg)
			} else {
				assertValidationSuccess(t, result)
			}
		})
	}
}

// TestCreateRoomRequest_Validate_Combined tests multiple validation failures together
func TestCreateRoomRequest_Validate_Combined(t *testing.T) {
	t.Parallel()

	// Test that validation fails on first error (room alias with whitespace)
	// even if other fields are also invalid
	req := createRoomRequest{
		RoomAliasName: "test room", // Invalid: contains space
		Preset:        "invalid",   // Invalid: not a valid preset
		Invite:        []string{"invalid"}, // Invalid: not a valid user ID
	}

	result := req.Validate()

	// Should fail on first validation check (room_alias_name)
	assertValidationError(t, result, http.StatusBadRequest, "room_alias_name")
}

// TestCreateRoomRequest_Validate_ComplexCreationContent tests more complex creation content scenarios
func TestCreateRoomRequest_Validate_ComplexCreationContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		creationContent json.RawMessage
		shouldFail      bool
	}{
		{
			name: "valid with room_version",
			creationContent: json.RawMessage(`{
				"m.federate": true,
				"room_version": "9"
			}`),
			shouldFail: false,
		},
		{
			name: "valid with predecessor",
			creationContent: json.RawMessage(`{
				"predecessor": {
					"room_id": "!oldroom:example.com",
					"event_id": "$event:example.com"
				}
			}`),
			shouldFail: false,
		},
		{
			name: "valid with type",
			creationContent: json.RawMessage(`{
				"type": "m.space"
			}`),
			shouldFail: false,
		},
		{
			name:            "invalid - array instead of object",
			creationContent: json.RawMessage(`["not", "an", "object"]`),
			shouldFail:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := createRoomRequest{
				CreationContent: tt.creationContent,
				Preset:          spec.PresetPrivateChat,
			}

			result := req.Validate()

			if tt.shouldFail {
				assertValidationError(t, result, http.StatusBadRequest, "malformed creation_content")
			} else {
				assertValidationSuccess(t, result)
			}
		})
	}
}

// TestCreateRoomRequest_Validate_EdgeCases tests edge case scenarios
func TestCreateRoomRequest_Validate_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		request    createRoomRequest
		shouldFail bool
	}{
		{
			name: "all fields valid",
			request: createRoomRequest{
				Name:          "My Room",
				Topic:         "Discussion about tests",
				RoomAliasName: "myroom",
				Preset:        spec.PresetPublicChat,
				Invite:        []string{"@user1:example.com"},
				RoomVersion:   gomatrixserverlib.RoomVersion("10"),
			},
			shouldFail: false,
		},
		{
			name: "minimal valid request - only preset",
			request: createRoomRequest{
				Preset: spec.PresetPrivateChat,
			},
			shouldFail: false,
		},
		{
			name: "minimal valid request - no preset (empty string)",
			request: createRoomRequest{
				Name: "Just a name",
			},
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tt.request.Validate()

			if tt.shouldFail {
				require.NotNil(t, result, "Expected validation to fail")
				assert.Equal(t, http.StatusBadRequest, result.Code)
			} else {
				assertValidationSuccess(t, result)
			}
		})
	}
}

// TODO: Add integration test for full /createRoom endpoint flow
// This unit test covers only request validation; full handler testing requires
// HTTP server setup, database mocking, and roomserver API integration.
