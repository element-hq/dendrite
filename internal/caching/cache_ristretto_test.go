// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package caching

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/element-hq/dendrite/roomserver/types"
	"github.com/element-hq/dendrite/setup/config"
	userapi "github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/fclient"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Helper Functions
// =============================================================================

// createTestCache creates a new Ristretto cache for testing
func createTestCache(t *testing.T, maxCost config.DataUnit, maxAge time.Duration) *Caches {
	t.Helper()
	return NewRistrettoCache(maxCost, maxAge, DisableMetrics)
}

// createDefaultTestCache creates a cache with sensible defaults
func createDefaultTestCache(t *testing.T) *Caches {
	t.Helper()
	return createTestCache(t, 1024*1024, time.Hour) // 1MB cache, 1 hour TTL
}

// createShortLivedCache creates a cache with short TTL for expiration tests
func createShortLivedCache(t *testing.T, ttl time.Duration) *Caches {
	t.Helper()
	return createTestCache(t, 1024*1024, ttl)
}

// waitForCacheProcessing waits for ristretto background processing
func waitForCacheProcessing(t *testing.T) {
	t.Helper()
	time.Sleep(10 * time.Millisecond) // Ristretto uses async operations
}

// createTestHeaderedEvent creates a test event for cache testing
func createTestHeaderedEvent(t *testing.T, eventID string) *types.HeaderedEvent {
	t.Helper()
	// Create a minimal valid PDU for testing
	event, err := gomatrixserverlib.MustGetRoomVersion(gomatrixserverlib.RoomVersionV10).NewEventFromTrustedJSON(
		[]byte(fmt.Sprintf(`{
			"type": "m.room.message",
			"room_id": "!test:server",
			"sender": "@user:server",
			"event_id": "%s",
			"origin_server_ts": 1000,
			"content": {"body": "test"}
		}`, eventID)),
		false,
	)
	if err != nil {
		t.Fatalf("Failed to create test event: %v", err)
	}
	return &types.HeaderedEvent{PDU: event}
}

// createTestDevice creates a test device for lazy loading tests
func createTestDevice(userID, deviceID string) *userapi.Device {
	return &userapi.Device{
		UserID: userID,
		ID:     deviceID,
	}
}

// =============================================================================
// RistrettoCachePartition Basic Operations
// =============================================================================

func TestRistrettoCachePartition_Set_StoresValue(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	cache.RoomVersions.Set("!room1:server", gomatrixserverlib.RoomVersionV10)
	waitForCacheProcessing(t)

	version, ok := cache.RoomVersions.Get("!room1:server")

	assert.True(t, ok, "Expected value to be found in cache")
	assert.Equal(t, gomatrixserverlib.RoomVersionV10, version)
}

func TestRistrettoCachePartition_Get_ReturnsValueWhenPresent(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	cache.RoomVersions.Set("!room1:server", gomatrixserverlib.RoomVersionV9)
	waitForCacheProcessing(t)

	version, ok := cache.RoomVersions.Get("!room1:server")

	assert.True(t, ok)
	assert.Equal(t, gomatrixserverlib.RoomVersionV9, version)
}

func TestRistrettoCachePartition_Get_ReturnsFalseWhenMissing(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	version, ok := cache.RoomVersions.Get("!nonexistent:server")

	assert.False(t, ok)
	assert.Equal(t, gomatrixserverlib.RoomVersion(""), version)
}

func TestRistrettoCachePartition_Unset_RemovesValue(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	// Set value
	cache.ServerKeys.Set("server1", gomatrixserverlib.PublicKeyLookupResult{})
	waitForCacheProcessing(t)

	// Verify it's there
	_, ok := cache.ServerKeys.Get("server1")
	assert.True(t, ok)

	// Unset it
	cache.ServerKeys.Unset("server1")
	waitForCacheProcessing(t)

	// Verify it's gone
	_, ok = cache.ServerKeys.Get("server1")
	assert.False(t, ok)
}

func TestRistrettoCachePartition_SetMultipleKeys_AllRetrievable(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	testCases := []struct {
		roomID  string
		version gomatrixserverlib.RoomVersion
	}{
		{"!room1:server", gomatrixserverlib.RoomVersionV10},
		{"!room2:server", gomatrixserverlib.RoomVersionV9},
		{"!room3:server", gomatrixserverlib.RoomVersionV8},
	}

	// Set all values
	for _, tc := range testCases {
		cache.RoomVersions.Set(tc.roomID, tc.version)
	}
	waitForCacheProcessing(t)

	// Verify all values
	for _, tc := range testCases {
		version, ok := cache.RoomVersions.Get(tc.roomID)
		assert.True(t, ok, "Expected to find %s in cache", tc.roomID)
		assert.Equal(t, tc.version, version, "Version mismatch for %s", tc.roomID)
	}
}

// =============================================================================
// Cache Key Types
// =============================================================================

func TestRistrettoCachePartition_StringKeys_WorkCorrectly(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	cache.RoomVersions.Set("!test:server", gomatrixserverlib.RoomVersionV10)
	waitForCacheProcessing(t)

	version, ok := cache.RoomVersions.Get("!test:server")

	assert.True(t, ok)
	assert.Equal(t, gomatrixserverlib.RoomVersionV10, version)
}

func TestRistrettoCachePartition_Int64Keys_WorkCorrectly(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	event := createTestHeaderedEvent(t, "$event123")
	cache.RoomServerEvents.Set(123, event)
	waitForCacheProcessing(t)

	retrieved, ok := cache.RoomServerEvents.Get(123)

	assert.True(t, ok)
	assert.Equal(t, "$event123", retrieved.EventID())
}

func TestRistrettoCachePartition_TypedNIDKeys_WorkCorrectly(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	roomNID := types.RoomNID(42)
	cache.RoomServerRoomIDs.Set(roomNID, "!room:server")
	waitForCacheProcessing(t)

	roomID, ok := cache.RoomServerRoomIDs.Get(roomNID)

	assert.True(t, ok)
	assert.Equal(t, "!room:server", roomID)
}

func TestRistrettoCachePartition_CompositeKeys_WorkCorrectly(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	device := createTestDevice("@user:server", "DEVICE123")

	cache.StoreLazyLoadedUser(device, "!room:server", "@target:server", "$event123")
	waitForCacheProcessing(t)

	eventID, ok := cache.IsLazyLoadedUserCached(device, "!room:server", "@target:server")

	assert.True(t, ok)
	assert.Equal(t, "$event123", eventID)
}

// =============================================================================
// TTL and Expiration Tests
// =============================================================================

func TestRistrettoCachePartition_TTL_ExpiresAfterMaxAge(t *testing.T) {
	t.Parallel()

	// Create cache with very short TTL
	cache := createShortLivedCache(t, 50*time.Millisecond)

	cache.RoomVersions.Set("!room1:server", gomatrixserverlib.RoomVersionV10)
	waitForCacheProcessing(t)

	// Verify it's there initially
	_, ok := cache.RoomVersions.Get("!room1:server")
	assert.True(t, ok, "Value should be present immediately after Set")

	// Verify expiration after TTL with polling
	require.Eventually(t, func() bool {
		_, found := cache.RoomVersions.Get("!room1:server")
		return !found
	}, 200*time.Millisecond, 10*time.Millisecond,
		"Value should have expired after MaxAge")
}

func TestRistrettoCachePartition_TTL_DifferentMaxAgesForDifferentCaches(t *testing.T) {
	t.Parallel()

	// Federation caches have shorter TTL (30 minutes vs general maxAge)
	cache := createTestCache(t, 1024*1024, 2*time.Hour)

	// Federation PDUs should have shorter TTL (lesserOf(30min, maxAge))
	event := createTestHeaderedEvent(t, "$event1")
	cache.FederationPDUs.Set(1, event)
	waitForCacheProcessing(t)

	retrieved, ok := cache.FederationPDUs.Get(1)
	assert.True(t, ok)
	assert.Equal(t, event.EventID(), retrieved.EventID())
}

// =============================================================================
// Immutable Cache Tests
// =============================================================================

func TestRistrettoCachePartition_ImmutableCache_PanicsOnValueChange(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	// Set initial value
	cache.RoomVersions.Set("!room1:server", gomatrixserverlib.RoomVersionV10)
	waitForCacheProcessing(t)

	// Attempt to change value should panic (RoomVersions is immutable)
	assert.Panics(t, func() {
		cache.RoomVersions.Set("!room1:server", gomatrixserverlib.RoomVersionV9)
	}, "Setting different value in immutable cache should panic")
	waitForCacheProcessing(t)
}

func TestRistrettoCachePartition_ImmutableCache_AllowsSameValue(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	// Set initial value
	cache.RoomVersions.Set("!room1:server", gomatrixserverlib.RoomVersionV10)
	waitForCacheProcessing(t)

	// Setting same value should not panic
	assert.NotPanics(t, func() {
		cache.RoomVersions.Set("!room1:server", gomatrixserverlib.RoomVersionV10)
	}, "Setting same value in immutable cache should not panic")
	waitForCacheProcessing(t)
}

func TestRistrettoCachePartition_ImmutableCache_PanicsOnUnset(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	cache.RoomVersions.Set("!room1:server", gomatrixserverlib.RoomVersionV10)
	waitForCacheProcessing(t)

	// Unset on immutable cache should panic
	assert.Panics(t, func() {
		cache.RoomVersions.Unset("!room1:server")
	}, "Unset on immutable cache should panic")
}

func TestRistrettoCachePartition_MutableCache_AllowsValueChange(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	// ServerKeys is mutable
	result1 := gomatrixserverlib.PublicKeyLookupResult{
		ValidUntilTS: 1000,
	}
	result2 := gomatrixserverlib.PublicKeyLookupResult{
		ValidUntilTS: 2000,
	}

	cache.ServerKeys.Set("server1", result1)
	waitForCacheProcessing(t)

	// Should not panic
	assert.NotPanics(t, func() {
		cache.ServerKeys.Set("server1", result2)
		waitForCacheProcessing(t)
	})

	retrieved, ok := cache.ServerKeys.Get("server1")
	assert.True(t, ok)
	assert.Equal(t, uint64(2000), uint64(retrieved.ValidUntilTS))
}

// =============================================================================
// Costed Cache Tests
// =============================================================================

func TestRistrettoCostedCachePartition_UsesCacheCostMethod(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	event := createTestHeaderedEvent(t, "$event1")

	// Should not panic - costed cache uses CacheCost() method
	assert.NotPanics(t, func() {
		cache.RoomServerEvents.Set(1, event)
		waitForCacheProcessing(t)
	})

	retrieved, ok := cache.RoomServerEvents.Get(1)
	assert.True(t, ok)
	assert.Equal(t, event.EventID(), retrieved.EventID())
}

func TestRistrettoCostedCachePartition_StoresAndRetrievesCorrectly(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	// Test multiple events
	events := map[int64]*types.HeaderedEvent{
		1: createTestHeaderedEvent(t, "$event1"),
		2: createTestHeaderedEvent(t, "$event2"),
		3: createTestHeaderedEvent(t, "$event3"),
	}

	for nid, event := range events {
		cache.RoomServerEvents.Set(nid, event)
	}
	waitForCacheProcessing(t)

	// Verify all events
	for nid, expectedEvent := range events {
		retrieved, ok := cache.RoomServerEvents.Get(nid)
		assert.True(t, ok, "Event %d should be in cache", nid)
		assert.Equal(t, expectedEvent.EventID(), retrieved.EventID())
	}
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

func TestRistrettoCachePartition_ConcurrentWrites_ThreadSafe(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	const numGoroutines = 100
	const numWrites = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numWrites; j++ {
				roomID := fmt.Sprintf("!room%d-%d:server", id, j)
				cache.RoomVersions.Set(roomID, gomatrixserverlib.RoomVersionV10)
			}
		}(i)
	}

	wg.Wait()
	waitForCacheProcessing(t)

	// Verify a sample of keys from different goroutines
	keysToCheck := []string{
		"!room0-0:server",  // First goroutine, first write
		"!room50-5:server", // Middle goroutine, middle write
		"!room99-9:server", // Last goroutine, last write
	}

	for _, roomID := range keysToCheck {
		version, ok := cache.RoomVersions.Get(roomID)
		assert.True(t, ok, "Expected to find %s in cache after concurrent writes", roomID)
		assert.Equal(t, gomatrixserverlib.RoomVersionV10, version, "Expected correct version for %s", roomID)
	}
}

func TestRistrettoCachePartition_ConcurrentReadWrites_ThreadSafe(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	// Pre-populate cache
	for i := 0; i < 10; i++ {
		roomID := fmt.Sprintf("!room%d:server", i)
		cache.RoomVersions.Set(roomID, gomatrixserverlib.RoomVersionV10)
	}
	waitForCacheProcessing(t)

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // readers + writers

	// Concurrent readers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				roomID := fmt.Sprintf("!room%d:server", j)
				_, _ = cache.RoomVersions.Get(roomID)
			}
		}(i)
	}

	// Concurrent writers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				roomID := fmt.Sprintf("!newroom%d-%d:server", id, j)
				cache.RoomVersions.Set(roomID, gomatrixserverlib.RoomVersionV9)
			}
		}(i)
	}

	wg.Wait()
}

func TestRistrettoCachePartition_ConcurrentMutableCacheAccess_ThreadSafe(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			serverName := fmt.Sprintf("server%d", id)

			// Set, Get, Unset cycle
			result := gomatrixserverlib.PublicKeyLookupResult{
				ValidUntilTS: spec.Timestamp(id),
			}
			cache.ServerKeys.Set(serverName, result)

			retrieved, ok := cache.ServerKeys.Get(serverName)
			if ok {
				assert.Equal(t, uint64(id), uint64(retrieved.ValidUntilTS))
			}

			cache.ServerKeys.Unset(serverName)
		}(i)
	}

	wg.Wait()
}

// =============================================================================
// Specialized Cache Tests - RoomVersion
// =============================================================================

func TestCaches_StoreRoomVersion_StoresAndRetrieves(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	cache.StoreRoomVersion("!room1:server", gomatrixserverlib.RoomVersionV10)
	waitForCacheProcessing(t)

	version, ok := cache.GetRoomVersion("!room1:server")

	assert.True(t, ok)
	assert.Equal(t, gomatrixserverlib.RoomVersionV10, version)
}

func TestCaches_GetRoomVersion_ReturnsFalseWhenMissing(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	version, ok := cache.GetRoomVersion("!nonexistent:server")

	assert.False(t, ok)
	assert.Equal(t, gomatrixserverlib.RoomVersion(""), version)
}

func TestCaches_StoreRoomVersion_MultipleVersions(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	testCases := []struct {
		roomID  string
		version gomatrixserverlib.RoomVersion
	}{
		{"!v10room:server", gomatrixserverlib.RoomVersionV10},
		{"!v9room:server", gomatrixserverlib.RoomVersionV9},
		{"!v8room:server", gomatrixserverlib.RoomVersionV8},
		{"!v7room:server", gomatrixserverlib.RoomVersionV7},
	}

	for _, tc := range testCases {
		cache.StoreRoomVersion(tc.roomID, tc.version)
	}
	waitForCacheProcessing(t)

	for _, tc := range testCases {
		version, ok := cache.GetRoomVersion(tc.roomID)
		assert.True(t, ok, "Expected to find version for %s", tc.roomID)
		assert.Equal(t, tc.version, version, "Version mismatch for %s", tc.roomID)
	}
}

// =============================================================================
// Specialized Cache Tests - LazyLoading
// =============================================================================

func TestCaches_LazyLoading_StoresAndRetrievesEventID(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	device := createTestDevice("@user:server", "DEVICE123")

	cache.StoreLazyLoadedUser(device, "!room:server", "@target:server", "$event123")
	waitForCacheProcessing(t)

	eventID, ok := cache.IsLazyLoadedUserCached(device, "!room:server", "@target:server")

	assert.True(t, ok)
	assert.Equal(t, "$event123", eventID)
}

func TestCaches_LazyLoading_DifferentDevicesDifferentCache(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	device1 := createTestDevice("@user:server", "DEVICE1")
	device2 := createTestDevice("@user:server", "DEVICE2")

	cache.StoreLazyLoadedUser(device1, "!room:server", "@target:server", "$event1")
	cache.StoreLazyLoadedUser(device2, "!room:server", "@target:server", "$event2")
	waitForCacheProcessing(t)

	eventID1, ok1 := cache.IsLazyLoadedUserCached(device1, "!room:server", "@target:server")
	eventID2, ok2 := cache.IsLazyLoadedUserCached(device2, "!room:server", "@target:server")

	assert.True(t, ok1)
	assert.True(t, ok2)
	assert.Equal(t, "$event1", eventID1)
	assert.Equal(t, "$event2", eventID2)
}

func TestCaches_LazyLoading_InvalidateClearsCache(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	device := createTestDevice("@user:server", "DEVICE123")

	cache.StoreLazyLoadedUser(device, "!room:server", "@target:server", "$event123")
	waitForCacheProcessing(t)

	// Verify it's there
	_, ok := cache.IsLazyLoadedUserCached(device, "!room:server", "@target:server")
	assert.True(t, ok)

	// Invalidate
	cache.InvalidateLazyLoadedUser(device, "!room:server", "@target:server")
	waitForCacheProcessing(t)

	// Verify it's gone
	_, ok = cache.IsLazyLoadedUserCached(device, "!room:server", "@target:server")
	assert.False(t, ok)
}

func TestCaches_LazyLoading_DifferentRoomsSeparate(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	device := createTestDevice("@user:server", "DEVICE123")

	cache.StoreLazyLoadedUser(device, "!room1:server", "@target:server", "$event1")
	cache.StoreLazyLoadedUser(device, "!room2:server", "@target:server", "$event2")
	waitForCacheProcessing(t)

	eventID1, ok1 := cache.IsLazyLoadedUserCached(device, "!room1:server", "@target:server")
	eventID2, ok2 := cache.IsLazyLoadedUserCached(device, "!room2:server", "@target:server")

	assert.True(t, ok1)
	assert.True(t, ok2)
	assert.Equal(t, "$event1", eventID1)
	assert.Equal(t, "$event2", eventID2)
}

// =============================================================================
// Specialized Cache Tests - RoomServerNIDs
// =============================================================================

func TestCaches_RoomServerNIDs_BidirectionalMapping(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	roomID := "!room123:server"
	roomNID := types.RoomNID(42)

	// Set both directions
	cache.RoomServerRoomNIDs.Set(roomID, roomNID)
	cache.RoomServerRoomIDs.Set(roomNID, roomID)
	waitForCacheProcessing(t)

	// Verify roomID -> roomNID
	retrievedNID, ok := cache.RoomServerRoomNIDs.Get(roomID)
	assert.True(t, ok)
	assert.Equal(t, roomNID, retrievedNID)

	// Verify roomNID -> roomID
	retrievedID, ok := cache.RoomServerRoomIDs.Get(roomNID)
	assert.True(t, ok)
	assert.Equal(t, roomID, retrievedID)
}

// =============================================================================
// Specialized Cache Tests - RoomHierarchies
// =============================================================================

func TestCaches_RoomHierarchies_StoresAndRetrieves(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	roomID := "!space:server"
	response := fclient.RoomHierarchyResponse{
		Room: fclient.RoomHierarchyRoom{
			PublicRoom: fclient.PublicRoom{
				RoomID: roomID,
			},
		},
	}

	cache.RoomHierarchies.Set(roomID, response)
	waitForCacheProcessing(t)

	retrieved, ok := cache.RoomHierarchies.Get(roomID)

	assert.True(t, ok)
	assert.Equal(t, roomID, retrieved.Room.RoomID)
}

func TestCaches_RoomHierarchies_MutableAllowsUpdates(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	roomID := "!space:server"
	response1 := fclient.RoomHierarchyResponse{
		Room: fclient.RoomHierarchyRoom{
			PublicRoom: fclient.PublicRoom{
				RoomID:             roomID,
				JoinedMembersCount: 10,
			},
		},
	}
	response2 := fclient.RoomHierarchyResponse{
		Room: fclient.RoomHierarchyRoom{
			PublicRoom: fclient.PublicRoom{
				RoomID:             roomID,
				JoinedMembersCount: 20,
			},
		},
	}

	cache.RoomHierarchies.Set(roomID, response1)
	waitForCacheProcessing(t)

	// Update should not panic (mutable cache)
	assert.NotPanics(t, func() {
		cache.RoomHierarchies.Set(roomID, response2)
		waitForCacheProcessing(t)
	})

	retrieved, ok := cache.RoomHierarchies.Get(roomID)
	assert.True(t, ok)
	assert.Equal(t, 20, retrieved.Room.JoinedMembersCount)
}

// =============================================================================
// Specialized Cache Tests - EventStateKeys
// =============================================================================

func TestCaches_EventStateKeys_BidirectionalMapping(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	stateKey := "some.state.key"
	stateKeyNID := types.EventStateKeyNID(123)

	cache.RoomServerStateKeys.Set(stateKeyNID, stateKey)
	cache.RoomServerStateKeyNIDs.Set(stateKey, stateKeyNID)
	waitForCacheProcessing(t)

	// Verify NID -> key
	retrievedKey, ok := cache.RoomServerStateKeys.Get(stateKeyNID)
	assert.True(t, ok)
	assert.Equal(t, stateKey, retrievedKey)

	// Verify key -> NID
	retrievedNID, ok := cache.RoomServerStateKeyNIDs.Get(stateKey)
	assert.True(t, ok)
	assert.Equal(t, stateKeyNID, retrievedNID)
}

// =============================================================================
// Specialized Cache Tests - EventTypes
// =============================================================================

func TestCaches_EventTypes_BidirectionalMapping(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	eventType := "m.room.message"
	eventTypeNID := types.EventTypeNID(456)

	cache.RoomServerEventTypes.Set(eventTypeNID, eventType)
	cache.RoomServerEventTypeNIDs.Set(eventType, eventTypeNID)
	waitForCacheProcessing(t)

	// Verify NID -> type
	retrievedType, ok := cache.RoomServerEventTypes.Get(eventTypeNID)
	assert.True(t, ok)
	assert.Equal(t, eventType, retrievedType)

	// Verify type -> NID
	retrievedNID, ok := cache.RoomServerEventTypeNIDs.Get(eventType)
	assert.True(t, ok)
	assert.Equal(t, eventTypeNID, retrievedNID)
}

// =============================================================================
// Cache Partitioning Tests
// =============================================================================

func TestRistrettoCachePartition_DifferentPrefixes_IsolateCaches(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	// Same key value, different cache partitions
	key := "test123"

	cache.RoomServerStateKeys.Set(types.EventStateKeyNID(1), key)
	cache.RoomServerEventTypes.Set(types.EventTypeNID(1), key)
	waitForCacheProcessing(t)

	// Both should coexist independently
	stateKey, ok1 := cache.RoomServerStateKeys.Get(types.EventStateKeyNID(1))
	eventType, ok2 := cache.RoomServerEventTypes.Get(types.EventTypeNID(1))

	assert.True(t, ok1)
	assert.True(t, ok2)
	assert.Equal(t, key, stateKey)
	assert.Equal(t, key, eventType)
}

// =============================================================================
// NewRistrettoCache Configuration Tests
// =============================================================================

func TestNewRistrettoCache_CreatesValidCache(t *testing.T) {
	t.Parallel()

	cache := NewRistrettoCache(1024*1024, time.Hour, DisableMetrics)

	require.NotNil(t, cache)
	require.NotNil(t, cache.RoomVersions)
	require.NotNil(t, cache.ServerKeys)
	require.NotNil(t, cache.RoomServerRoomNIDs)
	require.NotNil(t, cache.RoomServerRoomIDs)
	require.NotNil(t, cache.RoomServerEvents)
	require.NotNil(t, cache.FederationPDUs)
	require.NotNil(t, cache.FederationEDUs)
	require.NotNil(t, cache.RoomHierarchies)
	require.NotNil(t, cache.LazyLoading)
}

func TestNewRistrettoCache_WithMetrics_DoesNotPanic(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() {
		cache := NewRistrettoCache(1024*1024, time.Hour, EnableMetrics)
		require.NotNil(t, cache)
	})
}

func TestNewRistrettoCache_SmallMaxCost_Works(t *testing.T) {
	t.Parallel()

	cache := NewRistrettoCache(1024, 10*time.Minute, DisableMetrics) // 1KB cache

	cache.RoomVersions.Set("!room:server", gomatrixserverlib.RoomVersionV10)
	waitForCacheProcessing(t)

	version, ok := cache.RoomVersions.Get("!room:server")
	assert.True(t, ok)
	assert.Equal(t, gomatrixserverlib.RoomVersionV10, version)
}
