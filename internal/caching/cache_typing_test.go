// Copyright 2024 New Vector Ltd.
// Copyright 2019, 2020 The Matrix.org Foundation C.I.C.
// Copyright 2017, 2018 New Vector Ltd
// Copyright 2017 Vector Creations Ltd
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package caching

import (
	"testing"
	"time"

	"github.com/element-hq/dendrite/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEDUCache(t *testing.T) {
	tCache := NewTypingCache()
	if tCache == nil {
		t.Fatal("NewTypingCache failed")
	}

	t.Run("AddTypingUser", func(t *testing.T) {
		testAddTypingUser(t, tCache)
	})

	t.Run("GetTypingUsers", func(t *testing.T) {
		testGetTypingUsers(t, tCache)
	})

	t.Run("RemoveUser", func(t *testing.T) {
		testRemoveUser(t, tCache)
	})
}

func testAddTypingUser(t *testing.T, tCache *EDUCache) { // nolint: unparam
	present := time.Now()
	tests := []struct {
		userID string
		roomID string
		expire *time.Time
	}{ // Set four users typing state to room1
		{"user1", "room1", nil},
		{"user2", "room1", nil},
		{"user3", "room1", nil},
		{"user4", "room1", nil},
		//typing state with past expireTime should not take effect or removed.
		{"user1", "room2", &present},
	}

	for _, tt := range tests {
		tCache.AddTypingUser(tt.userID, tt.roomID, tt.expire)
	}
}

func testGetTypingUsers(t *testing.T, tCache *EDUCache) {
	tests := []struct {
		roomID    string
		wantUsers []string
	}{
		{"room1", []string{"user1", "user2", "user3", "user4"}},
		{"room2", []string{}},
	}

	for _, tt := range tests {
		gotUsers := tCache.GetTypingUsers(tt.roomID)
		if !test.UnsortedStringSliceEqual(gotUsers, tt.wantUsers) {
			t.Errorf("TypingCache.GetTypingUsers(%s) = %v, want %v", tt.roomID, gotUsers, tt.wantUsers)
		}
	}
}

func testRemoveUser(t *testing.T, tCache *EDUCache) {
	tests := []struct {
		roomID  string
		userIDs []string
	}{
		{"room3", []string{"user1"}},
		{"room4", []string{"user1", "user2", "user3"}},
	}

	for _, tt := range tests {
		for _, userID := range tt.userIDs {
			tCache.AddTypingUser(userID, tt.roomID, nil)
		}

		length := len(tt.userIDs)
		tCache.RemoveUser(tt.userIDs[length-1], tt.roomID)
		expLeftUsers := tt.userIDs[:length-1]
		if leftUsers := tCache.GetTypingUsers(tt.roomID); !test.UnsortedStringSliceEqual(leftUsers, expLeftUsers) {
			t.Errorf("Response after removal is unexpected. Want = %s, got = %s", leftUsers, expLeftUsers)
		}
	}
}

// TestTypingCache_SetTimeoutCallback_TriggeredOnExpiry tests that the timeout callback
// is triggered when a typing user expires.
// Covers cache_typing.go lines 99-101 (callback invocation)
func TestTypingCache_SetTimeoutCallback_TriggeredOnExpiry(t *testing.T) {
	t.Parallel()
	cache := NewTypingCache()

	var callbackUserID, callbackRoomID string
	var callbackSyncPos int64
	callbackCalled := false

	// Set the callback BEFORE adding user
	cache.SetTimeoutCallback(func(userID, roomID string, latestSyncPosition int64) {
		callbackCalled = true
		callbackUserID = userID
		callbackRoomID = roomID
		callbackSyncPos = latestSyncPosition
	})

	// Add user with very short timeout (5ms from now) for fast, deterministic test
	shortExpiry := time.Now().Add(5 * time.Millisecond)
	cache.AddTypingUser("@alice:server", "!room:server", &shortExpiry)

	// Wait for timeout to trigger using require.Eventually (no sleep/flake)
	require.Eventually(t, func() bool {
		return callbackCalled
	}, 200*time.Millisecond, 10*time.Millisecond,
		"Callback should be triggered after timeout expires")

	// Verify callback received correct parameters
	assert.Equal(t, "@alice:server", callbackUserID)
	assert.Equal(t, "!room:server", callbackRoomID)
	assert.Greater(t, callbackSyncPos, int64(0))

	// Verify user was actually removed after timeout
	users := cache.GetTypingUsers("!room:server")
	assert.Empty(t, users, "User should be removed after timeout")
}

// TestTypingCache_SetTimeoutCallback_NilCallback tests that nil callback is safe
func TestTypingCache_SetTimeoutCallback_NilCallback(t *testing.T) {
	t.Parallel()
	cache := NewTypingCache()

	// Don't set a callback (leave it nil)
	// Add user with short expiry
	shortExpiry := time.Now().Add(5 * time.Millisecond)
	cache.AddTypingUser("@alice:server", "!room:server", &shortExpiry)

	// Wait for timeout - should not panic even with nil callback
	time.Sleep(20 * time.Millisecond)

	// User should still be removed
	users := cache.GetTypingUsers("!room:server")
	assert.Empty(t, users, "User should be removed even without callback")
}

// TestTypingCache_AddTypingUser_MultipleUsers tests multiple users typing simultaneously
func TestTypingCache_AddTypingUser_MultipleUsers(t *testing.T) {
	t.Parallel()
	cache := NewTypingCache()

	future := time.Now().Add(10 * time.Second)

	// Add multiple users to same room
	cache.AddTypingUser("@alice:server", "!room:server", &future)
	cache.AddTypingUser("@bob:server", "!room:server", &future)
	cache.AddTypingUser("@charlie:server", "!room:server", &future)

	users := cache.GetTypingUsers("!room:server")
	assert.Len(t, users, 3, "Should have 3 users typing")
	assert.Contains(t, users, "@alice:server")
	assert.Contains(t, users, "@bob:server")
	assert.Contains(t, users, "@charlie:server")
}

// TestTypingCache_AddTypingUser_UpdateExisting tests updating an already typing user
func TestTypingCache_AddTypingUser_UpdateExisting(t *testing.T) {
	t.Parallel()
	cache := NewTypingCache()

	future := time.Now().Add(10 * time.Second)

	// Add user
	syncPos1 := cache.AddTypingUser("@alice:server", "!room:server", &future)

	// Add same user again (update)
	syncPos2 := cache.AddTypingUser("@alice:server", "!room:server", &future)

	// Sync position should increment
	assert.Greater(t, syncPos2, syncPos1, "Sync position should increment on update")

	// Should still be only one user
	users := cache.GetTypingUsers("!room:server")
	assert.Len(t, users, 1, "Should have only 1 user after update")
	assert.Contains(t, users, "@alice:server")
}

// TestTypingCache_AddTypingUser_ExpiredTime tests adding user with past expiry time
// Covers cache_typing.go lines 96 and 105 (expired time branch)
func TestTypingCache_AddTypingUser_ExpiredTime(t *testing.T) {
	t.Parallel()
	cache := NewTypingCache()

	// First add a valid user to increment sync position
	future := time.Now().Add(10 * time.Second)
	cache.AddTypingUser("@bob:server", "!room:server", &future)

	// Add user with expiry in the past
	pastTime := time.Now().Add(-10 * time.Second)
	syncPos := cache.AddTypingUser("@alice:server", "!room:server", &pastTime)

	// Should return current sync position (which is now > 0)
	assert.Greater(t, syncPos, int64(0), "Should return current sync position")

	// User with past expiry should not be in typing list
	users := cache.GetTypingUsers("!room:server")
	assert.Len(t, users, 1, "Should only have the valid user")
	assert.Contains(t, users, "@bob:server", "Should only contain the valid user")
	assert.NotContains(t, users, "@alice:server", "Should not contain expired user")
}

// TestTypingCache_RemoveUser_NonExistentRoom tests removing user from non-existent room
// Covers cache_typing.go lines 147-148 (room doesn't exist early return)
func TestTypingCache_RemoveUser_NonExistentRoom(t *testing.T) {
	t.Parallel()
	cache := NewTypingCache()

	// Try to remove user from room that doesn't exist
	syncPos := cache.RemoveUser("@alice:server", "!nonexistent:server")

	// Should return latestSyncPosition (which is 0 for empty cache)
	assert.Equal(t, int64(0), syncPos, "Should return latestSyncPosition without error")

	// Verify no data was created for the non-existent room
	users := cache.GetTypingUsers("!nonexistent:server")
	assert.Empty(t, users, "Room should not exist in cache")
}

// TestTypingCache_RemoveUser_UserNotInRoom tests removing user that's not in room
// Covers cache_typing.go lines 151-153 (user not in room's userSet early return)
func TestTypingCache_RemoveUser_UserNotInRoom(t *testing.T) {
	t.Parallel()
	cache := NewTypingCache()

	// Add alice to room
	future := time.Now().Add(10 * time.Second)
	cache.AddTypingUser("@alice:server", "!room:server", &future)

	// Try to remove bob (who was never added)
	syncPos := cache.RemoveUser("@bob:server", "!room:server")

	// Should return current latestSyncPosition (which is 1 from alice's addition)
	assert.Equal(t, int64(1), syncPos, "Should return current latestSyncPosition")

	// Alice should still be in the room
	users := cache.GetTypingUsers("!room:server")
	assert.Len(t, users, 1, "Alice should still be typing")
	assert.Contains(t, users, "@alice:server", "Alice should still be in room")
	assert.NotContains(t, users, "@bob:server", "Bob should not be in room")
}
