// Copyright 2024 New Vector Ltd.
// Copyright 2022 The Matrix.org Foundation C.I.C.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package caching

import (
	"github.com/element-hq/dendrite/roomserver/types"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/fclient"
)

// Caches contains a set of references to caches. They may be
// different implementations as long as they satisfy the Cache
// interface.
type Caches struct {
	RoomVersions            Cache[string, gomatrixserverlib.RoomVersion]           // room ID -> room version
	ServerKeys              Cache[string, gomatrixserverlib.PublicKeyLookupResult] // server name -> server keys
	RoomServerRoomNIDs      Cache[string, types.RoomNID]                           // room ID -> room NID
	RoomServerRoomIDs       Cache[types.RoomNID, string]                           // room NID -> room ID
	RoomServerEvents        Cache[int64, *types.HeaderedEvent]                     // event NID -> event
	RoomServerStateKeys     Cache[types.EventStateKeyNID, string]                  // eventStateKey NID -> event state key
	RoomServerStateKeyNIDs  Cache[string, types.EventStateKeyNID]                  // event state key -> eventStateKey NID
	RoomServerEventTypeNIDs Cache[string, types.EventTypeNID]                      // eventType -> eventType NID
	RoomServerEventTypes    Cache[types.EventTypeNID, string]                      // eventType NID -> eventType
	FederationPDUs          Cache[int64, *types.HeaderedEvent]                     // queue NID -> PDU
	FederationEDUs          Cache[int64, *gomatrixserverlib.EDU]                   // queue NID -> EDU
	RoomHierarchies         Cache[string, fclient.RoomHierarchyResponse]           // room ID -> space response
	RoomHierarchyFailures   Cache[string, struct{}]                                // room ID -> failed federation marker
	LazyLoading             Cache[lazyLoadingCacheKey, string]                     // composite key -> event ID
	RoomSummaries           Cache[string, RoomSummaryResponse]                     // "roomID:auth" -> summary response
}

// RoomSummaryResponse is cached separately to avoid circular imports with clientapi.
// This mirrors the structure in clientapi/routing/room_summary.go
type RoomSummaryResponse struct {
	RoomID           string   `json:"room_id"`
	RoomType         string   `json:"room_type,omitempty"`
	Name             string   `json:"name,omitempty"`
	Topic            string   `json:"topic,omitempty"`
	AvatarURL        string   `json:"avatar_url,omitempty"`
	CanonicalAlias   string   `json:"canonical_alias,omitempty"`
	NumJoinedMembers int      `json:"num_joined_members"`
	GuestCanJoin     bool     `json:"guest_can_join"`
	WorldReadable    bool     `json:"world_readable"`
	JoinRule         string   `json:"join_rule,omitempty"`
	AllowedRoomIDs   []string `json:"allowed_room_ids,omitempty"`
	Encryption       string   `json:"im.nheko.summary.encryption,omitempty"`
	Membership       string   `json:"membership,omitempty"`
	RoomVersion      string   `json:"im.nheko.summary.room_version,omitempty"`
}

// Cache is the interface that an implementation must satisfy.
type Cache[K keyable, T any] interface {
	Get(key K) (value T, ok bool)
	Set(key K, value T)
	Unset(key K)
}

type keyable interface {
	// from https://github.com/dgraph-io/ristretto/blob/8e850b710d6df0383c375ec6a7beae4ce48fc8d5/z/z.go#L34
	~uint64 | ~string | []byte | byte | ~int | ~int32 | ~uint32 | ~int64 | lazyLoadingCacheKey
}

type costable interface {
	CacheCost() int
}
