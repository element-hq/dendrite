// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package api

import (
	"context"
	"errors"
	"testing"

	"github.com/element-hq/dendrite/clientapi/auth/authtypes"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/stretchr/testify/assert"
)

// mockProfileAPI is a mock implementation of userapi.ProfileAPI for testing
type mockProfileAPI struct {
	profiles          map[string]*authtypes.Profile
	err               error
	queryProfileFunc  func(ctx context.Context, userID string) (*authtypes.Profile, error)
}

func (m *mockProfileAPI) QueryProfile(ctx context.Context, userID string) (*authtypes.Profile, error) {
	if m.queryProfileFunc != nil {
		return m.queryProfileFunc(ctx, userID)
	}
	if m.err != nil {
		return nil, m.err
	}
	if profile, ok := m.profiles[userID]; ok {
		return profile, nil
	}
	return nil, errors.New("profile not found")
}

func (m *mockProfileAPI) SetAvatarURL(ctx context.Context, localpart string, serverName spec.ServerName, avatarURL string) (*authtypes.Profile, bool, error) {
	return nil, false, nil
}

func (m *mockProfileAPI) SetDisplayName(ctx context.Context, localpart string, serverName spec.ServerName, displayName string) (*authtypes.Profile, bool, error) {
	return nil, false, nil
}

// mockAppServiceAPI is a mock implementation of AppServiceInternalAPI for testing
type mockAppServiceAPI struct {
	userExists         bool
	err                error
	userIDExistsCalled bool // Track if UserIDExists was called
}

func (m *mockAppServiceAPI) RoomAliasExists(ctx context.Context, req *RoomAliasExistsRequest, resp *RoomAliasExistsResponse) error {
	return nil
}

func (m *mockAppServiceAPI) UserIDExists(ctx context.Context, req *UserIDExistsRequest, resp *UserIDExistsResponse) error {
	m.userIDExistsCalled = true // Track calls
	if m.err != nil {
		return m.err
	}
	resp.UserIDExists = m.userExists
	return nil
}

func (m *mockAppServiceAPI) Locations(ctx context.Context, req *LocationRequest, resp *LocationResponse) error {
	return nil
}

func (m *mockAppServiceAPI) User(ctx context.Context, req *UserRequest, resp *UserResponse) error {
	return nil
}

func (m *mockAppServiceAPI) Protocols(ctx context.Context, req *ProtocolRequest, resp *ProtocolResponse) error {
	return nil
}

// TestRetrieveUserProfile_LocalProfileExists_ReturnsProfile tests that when a profile
// exists locally, it's returned immediately without querying the AS
func TestRetrieveUserProfile_LocalProfileExists_ReturnsProfile(t *testing.T) {
	t.Parallel()

	expectedProfile := &authtypes.Profile{
		Localpart:   "alice",
		ServerName:  "example.com",
		DisplayName: "Alice",
		AvatarURL:   "mxc://example.com/avatar",
	}

	profileAPI := &mockProfileAPI{
		profiles: map[string]*authtypes.Profile{
			"@alice:example.com": expectedProfile,
		},
	}

	asAPI := &mockAppServiceAPI{
		userExists: false, // Should not be queried
	}

	profile, err := RetrieveUserProfile(context.Background(), "@alice:example.com", asAPI, profileAPI)

	assert.NoError(t, err)
	assert.Equal(t, expectedProfile, profile)
	assert.Equal(t, "Alice", profile.DisplayName)
	// Fixed: Verify that UserIDExists was not called for cached local profiles
	assert.False(t, asAPI.userIDExistsCalled, "UserIDExists should not be called for cached local profiles")
}

// TestRetrieveUserProfile_ASUserExists_ReturnsProfile tests that when a profile
// doesn't exist locally but the AS claims the user, we query the profile again after AS lookup
func TestRetrieveUserProfile_ASUserExists_ReturnsProfile(t *testing.T) {
	t.Parallel()

	expectedProfile := &authtypes.Profile{
		Localpart:   "as_bot",
		ServerName:  "example.com",
		DisplayName: "AS Bot",
	}

	callCount := 0
	profileAPI := &mockProfileAPI{
		profiles: make(map[string]*authtypes.Profile),
		queryProfileFunc: func(ctx context.Context, userID string) (*authtypes.Profile, error) {
			callCount++
			if callCount == 1 {
				// First call: profile doesn't exist
				return nil, errors.New("profile not found")
			}
			// Second call: AS has created the profile
			return expectedProfile, nil
		},
	}

	asAPI := &mockAppServiceAPI{
		userExists: true, // AS claims this user
	}

	profile, err := RetrieveUserProfile(context.Background(), "@as_bot:example.com", asAPI, profileAPI)

	assert.NoError(t, err)
	assert.Equal(t, expectedProfile, profile)
	assert.Equal(t, 2, callCount, "Expected QueryProfile to be called twice")
}

// TestRetrieveUserProfile_ASUserNotFound_ReturnsError tests that when neither
// local nor AS has the user, ErrProfileNotExists is returned
func TestRetrieveUserProfile_ASUserNotFound_ReturnsError(t *testing.T) {
	t.Parallel()

	profileAPI := &mockProfileAPI{
		profiles: make(map[string]*authtypes.Profile),
		err:      errors.New("profile not found"),
	}

	asAPI := &mockAppServiceAPI{
		userExists: false, // AS doesn't have this user either
	}

	profile, err := RetrieveUserProfile(context.Background(), "@unknown:example.com", asAPI, profileAPI)

	assert.Error(t, err)
	assert.Equal(t, ErrProfileNotExists, err)
	assert.Nil(t, profile)
	// Fixed: Verify that UserIDExists was called to check AS
	assert.True(t, asAPI.userIDExistsCalled, "UserIDExists should be called to check AS when profile not found locally")
}

// TestRetrieveUserProfile_ASError_ReturnsError tests that when AS query fails,
// the error is propagated
func TestRetrieveUserProfile_ASError_ReturnsError(t *testing.T) {
	t.Parallel()

	profileAPI := &mockProfileAPI{
		profiles: make(map[string]*authtypes.Profile),
		err:      errors.New("profile not found"),
	}

	asAPI := &mockAppServiceAPI{
		err: errors.New("AS connection failed"),
	}

	profile, err := RetrieveUserProfile(context.Background(), "@user:example.com", asAPI, profileAPI)

	assert.Error(t, err)
	assert.Equal(t, "AS connection failed", err.Error())
	assert.Nil(t, profile)
}

// TestRetrieveUserProfile_ProfileCreationFails_ReturnsError tests that when AS
// claims the user but profile creation fails, the error is returned
func TestRetrieveUserProfile_ProfileCreationFails_ReturnsError(t *testing.T) {
	t.Parallel()

	profileAPI := &mockProfileAPI{
		profiles: make(map[string]*authtypes.Profile),
		err:      errors.New("database error"),
	}

	asAPI := &mockAppServiceAPI{
		userExists: true,
	}

	profile, err := RetrieveUserProfile(context.Background(), "@as_user:example.com", asAPI, profileAPI)

	assert.Error(t, err)
	assert.Equal(t, "database error", err.Error())
	assert.Nil(t, profile)
}

// TestRetrieveUserProfile_EmptyUserID_HandlesGracefully tests behavior with empty user ID
func TestRetrieveUserProfile_EmptyUserID_HandlesGracefully(t *testing.T) {
	t.Parallel()

	profileAPI := &mockProfileAPI{
		profiles: make(map[string]*authtypes.Profile),
		err:      errors.New("invalid user ID"),
	}

	asAPI := &mockAppServiceAPI{
		userExists: false,
	}

	profile, err := RetrieveUserProfile(context.Background(), "", asAPI, profileAPI)

	assert.Error(t, err)
	assert.Nil(t, profile)
}

// TestRetrieveUserProfile_ContextCancellation_HandlesGracefully tests that context
// cancellation is handled properly
func TestRetrieveUserProfile_ContextCancellation_HandlesGracefully(t *testing.T) {
	t.Parallel()

	profileAPI := &mockProfileAPI{
		profiles: make(map[string]*authtypes.Profile),
		err:      errors.New("profile not found"),
	}

	asAPI := &mockAppServiceAPI{
		userExists: false,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	profile, err := RetrieveUserProfile(ctx, "@user:example.com", asAPI, profileAPI)

	// Should still attempt the operation and return error
	assert.Error(t, err)
	assert.Nil(t, profile)
}

// TestRetrieveUserProfile_ASUserExistsThenProfileStillNotFound_ReturnsError tests
// that when AS says user exists but profile still doesn't exist after AS lookup, error is returned
func TestRetrieveUserProfile_ASUserExistsThenProfileStillNotFound_ReturnsError(t *testing.T) {
	t.Parallel()

	profileAPI := &mockProfileAPI{
		profiles: make(map[string]*authtypes.Profile),
		err:      errors.New("profile not found"),
	}

	asAPI := &mockAppServiceAPI{
		userExists: true, // AS claims user exists
	}

	profile, err := RetrieveUserProfile(context.Background(), "@as_user:example.com", asAPI, profileAPI)

	assert.Error(t, err)
	assert.Equal(t, "profile not found", err.Error())
	assert.Nil(t, profile)
}
