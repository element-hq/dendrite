// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package sync

import (
	"testing"

	"github.com/element-hq/dendrite/syncapi/types"
	"github.com/stretchr/testify/assert"
)

// TestFindRelevantRoomIDsForExtension tests the extension room filtering logic
// This implements MSC3959/MSC3960 behavior for lists/rooms parameters
func TestFindRelevantRoomIDsForExtension(t *testing.T) {
	// Setup common test data
	actualLists := map[string]types.SlidingList{
		"rooms": {
			Count: 3,
			Ops: []types.SlidingOperation{
				{Op: "SYNC", RoomIDs: []string{"!room1:test", "!room2:test", "!room3:test"}},
			},
		},
		"dms": {
			Count: 2,
			Ops: []types.SlidingOperation{
				{Op: "SYNC", RoomIDs: []string{"!dm1:test", "!dm2:test"}},
			},
		},
	}

	actualRoomSubscriptions := map[string]bool{
		"!sub1:test": true,
		"!sub2:test": true,
	}

	tests := []struct {
		name                    string
		requestedLists          []string
		requestedRooms          []string
		actualLists             map[string]types.SlidingList
		actualRoomSubscriptions map[string]bool
		wantRoomIDs             map[string]bool
		description             string
	}{
		{
			name:                    "nil lists and nil rooms - default wildcard behavior",
			requestedLists:          nil,
			requestedRooms:          nil,
			actualLists:             actualLists,
			actualRoomSubscriptions: actualRoomSubscriptions,
			wantRoomIDs: map[string]bool{
				"!room1:test": true, "!room2:test": true, "!room3:test": true,
				"!dm1:test": true, "!dm2:test": true,
				"!sub1:test": true, "!sub2:test": true,
			},
			description: "When both lists and rooms are nil (omitted), process all lists and all subscriptions",
		},
		{
			name:                    "empty lists array - process no lists",
			requestedLists:          []string{},
			requestedRooms:          nil,
			actualLists:             actualLists,
			actualRoomSubscriptions: actualRoomSubscriptions,
			wantRoomIDs: map[string]bool{
				"!sub1:test": true, "!sub2:test": true,
			},
			description: "Empty lists array [] means explicitly process no lists, but rooms defaults to wildcard",
		},
		{
			name:                    "empty rooms array - process no room subscriptions",
			requestedLists:          nil,
			requestedRooms:          []string{},
			actualLists:             actualLists,
			actualRoomSubscriptions: actualRoomSubscriptions,
			wantRoomIDs: map[string]bool{
				"!room1:test": true, "!room2:test": true, "!room3:test": true,
				"!dm1:test": true, "!dm2:test": true,
			},
			description: "Empty rooms array [] means explicitly process no subscriptions, but lists defaults to wildcard",
		},
		{
			name:                    "both empty arrays - process nothing",
			requestedLists:          []string{},
			requestedRooms:          []string{},
			actualLists:             actualLists,
			actualRoomSubscriptions: actualRoomSubscriptions,
			wantRoomIDs:             map[string]bool{},
			description:             "Both empty arrays means process nothing",
		},
		{
			name:                    "specific list only",
			requestedLists:          []string{"rooms"},
			requestedRooms:          []string{},
			actualLists:             actualLists,
			actualRoomSubscriptions: actualRoomSubscriptions,
			wantRoomIDs: map[string]bool{
				"!room1:test": true, "!room2:test": true, "!room3:test": true,
			},
			description: "Process only the 'rooms' list",
		},
		{
			name:                    "multiple specific lists",
			requestedLists:          []string{"rooms", "dms"},
			requestedRooms:          []string{},
			actualLists:             actualLists,
			actualRoomSubscriptions: actualRoomSubscriptions,
			wantRoomIDs: map[string]bool{
				"!room1:test": true, "!room2:test": true, "!room3:test": true,
				"!dm1:test": true, "!dm2:test": true,
			},
			description: "Process both 'rooms' and 'dms' lists",
		},
		{
			name:                    "wildcard list",
			requestedLists:          []string{"*"},
			requestedRooms:          []string{},
			actualLists:             actualLists,
			actualRoomSubscriptions: actualRoomSubscriptions,
			wantRoomIDs: map[string]bool{
				"!room1:test": true, "!room2:test": true, "!room3:test": true,
				"!dm1:test": true, "!dm2:test": true,
			},
			description: "Wildcard '*' in lists means process all lists",
		},
		{
			name:                    "specific rooms only",
			requestedLists:          []string{},
			requestedRooms:          []string{"!sub1:test"},
			actualLists:             actualLists,
			actualRoomSubscriptions: actualRoomSubscriptions,
			wantRoomIDs: map[string]bool{
				"!sub1:test": true,
			},
			description: "Process only specific room subscription",
		},
		{
			name:                    "wildcard rooms",
			requestedLists:          []string{},
			requestedRooms:          []string{"*"},
			actualLists:             actualLists,
			actualRoomSubscriptions: actualRoomSubscriptions,
			wantRoomIDs: map[string]bool{
				"!sub1:test": true, "!sub2:test": true,
			},
			description: "Wildcard '*' in rooms means process all subscriptions",
		},
		{
			name:                    "specific rooms not in subscriptions - filtered out",
			requestedLists:          []string{},
			requestedRooms:          []string{"!nonexistent:test"},
			actualLists:             actualLists,
			actualRoomSubscriptions: actualRoomSubscriptions,
			wantRoomIDs:             map[string]bool{},
			description:             "Rooms not in actual subscriptions are filtered out",
		},
		{
			name:                    "nonexistent list - ignored",
			requestedLists:          []string{"nonexistent"},
			requestedRooms:          []string{},
			actualLists:             actualLists,
			actualRoomSubscriptions: actualRoomSubscriptions,
			wantRoomIDs:             map[string]bool{},
			description:             "Lists that don't exist in actual response are ignored",
		},
		{
			name:                    "combination of list and rooms",
			requestedLists:          []string{"dms"},
			requestedRooms:          []string{"!sub1:test"},
			actualLists:             actualLists,
			actualRoomSubscriptions: actualRoomSubscriptions,
			wantRoomIDs: map[string]bool{
				"!dm1:test": true, "!dm2:test": true,
				"!sub1:test": true,
			},
			description: "Both list rooms and subscription rooms are included",
		},
		{
			name:                    "empty actual lists",
			requestedLists:          nil,
			requestedRooms:          nil,
			actualLists:             map[string]types.SlidingList{},
			actualRoomSubscriptions: actualRoomSubscriptions,
			wantRoomIDs: map[string]bool{
				"!sub1:test": true, "!sub2:test": true,
			},
			description: "When actual lists are empty, only subscriptions are returned",
		},
		{
			name:                    "empty actual subscriptions",
			requestedLists:          nil,
			requestedRooms:          nil,
			actualLists:             actualLists,
			actualRoomSubscriptions: map[string]bool{},
			wantRoomIDs: map[string]bool{
				"!room1:test": true, "!room2:test": true, "!room3:test": true,
				"!dm1:test": true, "!dm2:test": true,
			},
			description: "When actual subscriptions are empty, only list rooms are returned",
		},
		{
			name:                    "list with multiple operations",
			requestedLists:          []string{"multi"},
			requestedRooms:          []string{},
			actualLists: map[string]types.SlidingList{
				"multi": {
					Count: 4,
					Ops: []types.SlidingOperation{
						{Op: "SYNC", RoomIDs: []string{"!a:test", "!b:test"}},
						{Op: "SYNC", RoomIDs: []string{"!c:test", "!d:test"}},
					},
				},
			},
			actualRoomSubscriptions: map[string]bool{},
			wantRoomIDs: map[string]bool{
				"!a:test": true, "!b:test": true, "!c:test": true, "!d:test": true,
			},
			description: "Rooms from all operations in a list are included",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findRelevantRoomIDsForExtension(
				tt.requestedLists,
				tt.requestedRooms,
				tt.actualLists,
				tt.actualRoomSubscriptions,
			)

			assert.Equal(t, tt.wantRoomIDs, result, tt.description)
		})
	}
}

// TestFindRelevantRoomIDsForExtensionDeduplication tests that duplicate rooms are handled
func TestFindRelevantRoomIDsForExtensionDeduplication(t *testing.T) {
	// Room appears in both list and subscription
	actualLists := map[string]types.SlidingList{
		"rooms": {
			Ops: []types.SlidingOperation{
				{Op: "SYNC", RoomIDs: []string{"!shared:test", "!listonly:test"}},
			},
		},
	}
	actualSubscriptions := map[string]bool{
		"!shared:test": true,
		"!subonly:test": true,
	}

	result := findRelevantRoomIDsForExtension(nil, nil, actualLists, actualSubscriptions)

	// Should have 3 unique rooms (shared appears in both but only counted once)
	assert.Len(t, result, 3)
	assert.True(t, result["!shared:test"])
	assert.True(t, result["!listonly:test"])
	assert.True(t, result["!subonly:test"])
}
