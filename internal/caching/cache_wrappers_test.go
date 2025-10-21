// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package caching

import (
	"testing"
	"time"

	"github.com/element-hq/dendrite/roomserver/types"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/fclient"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/stretchr/testify/assert"
)

// =============================================================================
// Federation Cache Tests (PDU/EDU wrappers)
// =============================================================================

func TestCaches_FederationQueuedPDU_StoreAndRetrieve(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	event := createTestHeaderedEvent(t, "$fed_event123")

	cache.StoreFederationQueuedPDU(123, event)
	waitForCacheProcessing(t)

	retrieved, ok := cache.GetFederationQueuedPDU(123)

	assert.True(t, ok)
	assert.Equal(t, event.EventID(), retrieved.EventID())
}

func TestCaches_FederationQueuedPDU_EvictRemovesEvent(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	event := createTestHeaderedEvent(t, "$fed_event123")

	cache.StoreFederationQueuedPDU(123, event)
	waitForCacheProcessing(t)

	// Verify it's there
	_, ok := cache.GetFederationQueuedPDU(123)
	assert.True(t, ok)

	// Evict it
	cache.EvictFederationQueuedPDU(123)
	waitForCacheProcessing(t)

	// Verify it's gone
	_, ok = cache.GetFederationQueuedPDU(123)
	assert.False(t, ok)
}

func TestCaches_FederationQueuedEDU_StoreAndRetrieve(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	edu := &gomatrixserverlib.EDU{
		Type: "m.typing",
	}

	cache.StoreFederationQueuedEDU(456, edu)
	waitForCacheProcessing(t)

	retrieved, ok := cache.GetFederationQueuedEDU(456)

	assert.True(t, ok)
	assert.Equal(t, "m.typing", retrieved.Type)
}

func TestCaches_FederationQueuedEDU_EvictRemovesEDU(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	edu := &gomatrixserverlib.EDU{
		Type: "m.presence",
	}

	cache.StoreFederationQueuedEDU(456, edu)
	waitForCacheProcessing(t)

	// Verify it's there
	_, ok := cache.GetFederationQueuedEDU(456)
	assert.True(t, ok)

	// Evict it
	cache.EvictFederationQueuedEDU(456)
	waitForCacheProcessing(t)

	// Verify it's gone
	_, ok = cache.GetFederationQueuedEDU(456)
	assert.False(t, ok)
}

// =============================================================================
// RoomServer Events Cache Tests (EventNID wrappers)
// =============================================================================

func TestCaches_RoomServerEvent_StoreAndRetrieve(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	event := createTestHeaderedEvent(t, "$room_event123")
	eventNID := types.EventNID(789)

	cache.StoreRoomServerEvent(eventNID, event)
	waitForCacheProcessing(t)

	retrieved, ok := cache.GetRoomServerEvent(eventNID)

	assert.True(t, ok)
	assert.Equal(t, event.EventID(), retrieved.EventID())
}

func TestCaches_RoomServerEvent_InvalidateRemovesEvent(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	event := createTestHeaderedEvent(t, "$room_event123")
	eventNID := types.EventNID(789)

	cache.StoreRoomServerEvent(eventNID, event)
	waitForCacheProcessing(t)

	// Verify it's there
	_, ok := cache.GetRoomServerEvent(eventNID)
	assert.True(t, ok)

	// Invalidate it
	cache.InvalidateRoomServerEvent(eventNID)
	waitForCacheProcessing(t)

	// Verify it's gone
	_, ok = cache.GetRoomServerEvent(eventNID)
	assert.False(t, ok)
}

func TestCaches_RoomServerEvent_MultipleEvents(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	events := map[types.EventNID]*types.HeaderedEvent{
		100: createTestHeaderedEvent(t, "$event100"),
		200: createTestHeaderedEvent(t, "$event200"),
		300: createTestHeaderedEvent(t, "$event300"),
	}

	for nid, event := range events {
		cache.StoreRoomServerEvent(nid, event)
	}
	waitForCacheProcessing(t)

	for nid, expectedEvent := range events {
		retrieved, ok := cache.GetRoomServerEvent(nid)
		assert.True(t, ok, "Expected to find event %d", nid)
		assert.Equal(t, expectedEvent.EventID(), retrieved.EventID())
	}
}

// =============================================================================
// RoomServer NID Caches Tests (RoomID/RoomNID wrappers)
// =============================================================================

func TestCaches_RoomServerRoomNID_StoreAndRetrieve(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	roomID := "!test:server"
	roomNID := types.RoomNID(42)

	cache.RoomServerRoomNIDs.Set(roomID, roomNID)
	waitForCacheProcessing(t)

	retrieved, ok := cache.GetRoomServerRoomNID(roomID)

	assert.True(t, ok)
	assert.Equal(t, roomNID, retrieved)
}

func TestCaches_RoomServerRoomID_StoreAndRetrieve(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	roomID := "!test:server"
	roomNID := types.RoomNID(42)

	cache.StoreRoomServerRoomID(roomNID, roomID)
	waitForCacheProcessing(t)

	retrieved, ok := cache.GetRoomServerRoomID(roomNID)

	assert.True(t, ok)
	assert.Equal(t, roomID, retrieved)
}

// =============================================================================
// EventStateKey Cache Tests (StateKey/NID wrappers)
// =============================================================================

func TestCaches_EventStateKey_StoreAndRetrieve(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	stateKey := "@user:server"
	stateKeyNID := types.EventStateKeyNID(123)

	cache.StoreEventStateKey(stateKeyNID, stateKey)
	waitForCacheProcessing(t)

	retrieved, ok := cache.GetEventStateKey(stateKeyNID)

	assert.True(t, ok)
	assert.Equal(t, stateKey, retrieved)
}

func TestCaches_EventStateKeyNID_StoreAndRetrieve(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	stateKey := "@user:server"
	stateKeyNID := types.EventStateKeyNID(123)

	cache.RoomServerStateKeyNIDs.Set(stateKey, stateKeyNID)
	waitForCacheProcessing(t)

	retrieved, ok := cache.GetEventStateKeyNID(stateKey)

	assert.True(t, ok)
	assert.Equal(t, stateKeyNID, retrieved)
}

// =============================================================================
// EventType Cache Tests (EventType/NID wrappers)
// =============================================================================

func TestCaches_EventTypeKey_StoreAndRetrieve(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	eventType := "m.room.message"
	eventTypeNID := types.EventTypeNID(456)

	cache.StoreEventTypeKey(eventTypeNID, eventType)
	waitForCacheProcessing(t)

	retrieved, ok := cache.GetEventTypeKey(eventType)

	assert.True(t, ok)
	assert.Equal(t, eventTypeNID, retrieved)
}

// =============================================================================
// ServerKey Cache Tests (with timestamp validation)
// =============================================================================

func TestCaches_ServerKey_StoreAndRetrieve(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	request := gomatrixserverlib.PublicKeyLookupRequest{
		ServerName: "example.com",
		KeyID:      "ed25519:auto",
	}

	response := gomatrixserverlib.PublicKeyLookupResult{
		ValidUntilTS: spec.Timestamp(time.Now().Add(time.Hour).UnixMilli()),
		ExpiredTS:    gomatrixserverlib.PublicKeyNotExpired,
		VerifyKey: gomatrixserverlib.VerifyKey{
			Key: spec.Base64Bytes("test_key_data_here"),
		},
	}

	cache.StoreServerKey(request, response)
	waitForCacheProcessing(t)

	// Retrieve with current timestamp (should be valid)
	now := spec.AsTimestamp(time.Now())
	retrieved, ok := cache.GetServerKey(request, now)

	assert.True(t, ok)
	assert.Equal(t, response.ValidUntilTS, retrieved.ValidUntilTS)
}

func TestCaches_ServerKey_ExpiredKeyNotReturned(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	request := gomatrixserverlib.PublicKeyLookupRequest{
		ServerName: "example.com",
		KeyID:      "ed25519:old",
	}

	// Create a key that expired 1 hour ago
	pastTime := time.Now().Add(-2 * time.Hour)
	response := gomatrixserverlib.PublicKeyLookupResult{
		ValidUntilTS: spec.Timestamp(pastTime.UnixMilli()),
		ExpiredTS:    spec.Timestamp(time.Now().Add(-1 * time.Hour).UnixMilli()),
		VerifyKey: gomatrixserverlib.VerifyKey{
			Key: spec.Base64Bytes("old_key_data"),
		},
	}

	cache.StoreServerKey(request, response)
	waitForCacheProcessing(t)

	// Try to retrieve with current timestamp (should fail - key expired)
	now := spec.AsTimestamp(time.Now())
	_, ok := cache.GetServerKey(request, now)

	assert.False(t, ok, "Expired key should not be returned")
}

func TestCaches_ServerKey_MultipleServers(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	servers := []struct {
		serverName string
		keyID      string
	}{
		{"server1.com", "ed25519:key1"},
		{"server2.com", "ed25519:key2"},
		{"server3.com", "ed25519:key3"},
	}

	futureTime := spec.Timestamp(time.Now().Add(time.Hour).UnixMilli())

	for _, s := range servers {
		request := gomatrixserverlib.PublicKeyLookupRequest{
			ServerName: spec.ServerName(s.serverName),
			KeyID:      gomatrixserverlib.KeyID(s.keyID),
		}

		response := gomatrixserverlib.PublicKeyLookupResult{
			ValidUntilTS: futureTime,
			ExpiredTS:    gomatrixserverlib.PublicKeyNotExpired,
			VerifyKey: gomatrixserverlib.VerifyKey{
				Key: spec.Base64Bytes(s.keyID + "_data"),
			},
		}

		cache.StoreServerKey(request, response)
	}
	waitForCacheProcessing(t)

	// Verify all keys can be retrieved
	now := spec.AsTimestamp(time.Now())
	for _, s := range servers {
		request := gomatrixserverlib.PublicKeyLookupRequest{
			ServerName: spec.ServerName(s.serverName),
			KeyID:      gomatrixserverlib.KeyID(s.keyID),
		}

		_, ok := cache.GetServerKey(request, now)
		assert.True(t, ok, "Expected to find key for %s/%s", s.serverName, s.keyID)
	}
}

// =============================================================================
// RoomHierarchy Cache Tests (wrapper)
// =============================================================================

func TestCaches_RoomHierarchy_StoreAndRetrieve(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)
	roomID := "!space:server"

	response := fclient.RoomHierarchyResponse{
		Room: fclient.RoomHierarchyRoom{
			PublicRoom: fclient.PublicRoom{
				RoomID: roomID,
				Name:   "Test Space",
			},
		},
	}

	cache.StoreRoomHierarchy(roomID, response)
	waitForCacheProcessing(t)

	retrieved, ok := cache.GetRoomHierarchy(roomID)

	assert.True(t, ok)
	assert.Equal(t, roomID, retrieved.Room.RoomID)
	assert.Equal(t, "Test Space", retrieved.Room.Name)
}

func TestCaches_RoomHierarchy_MissingReturnsNotFound(t *testing.T) {
	t.Parallel()

	cache := createDefaultTestCache(t)

	_, ok := cache.GetRoomHierarchy("!nonexistent:server")

	assert.False(t, ok)
}
