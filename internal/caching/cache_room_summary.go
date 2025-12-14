// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package caching

import "fmt"

// RoomSummaryCache caches responses to MSC3266 room summary requests.
// Cache key format: "roomID:authenticated" where authenticated is "true" or "false".
// Different keys are used because authenticated responses include membership field.
type RoomSummaryCache interface {
	GetRoomSummary(roomID string, authenticated bool) (r RoomSummaryResponse, ok bool)
	StoreRoomSummary(roomID string, authenticated bool, r RoomSummaryResponse)
	InvalidateRoomSummary(roomID string)
}

func roomSummaryCacheKey(roomID string, authenticated bool) string {
	return fmt.Sprintf("%s:%t", roomID, authenticated)
}

func (c Caches) GetRoomSummary(roomID string, authenticated bool) (r RoomSummaryResponse, ok bool) {
	return c.RoomSummaries.Get(roomSummaryCacheKey(roomID, authenticated))
}

func (c Caches) StoreRoomSummary(roomID string, authenticated bool, r RoomSummaryResponse) {
	c.RoomSummaries.Set(roomSummaryCacheKey(roomID, authenticated), r)
}

// InvalidateRoomSummary removes both authenticated and unauthenticated cache entries for a room.
// This should be called when room state changes (name, topic, join rules, etc.)
func (c Caches) InvalidateRoomSummary(roomID string) {
	c.RoomSummaries.Unset(roomSummaryCacheKey(roomID, true))
	c.RoomSummaries.Unset(roomSummaryCacheKey(roomID, false))
}
