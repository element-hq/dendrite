// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSortRoomsByActivity tests room sorting by bump stamp
func TestSortRoomsByActivity(t *testing.T) {
	tests := []struct {
		name     string
		input    []RoomWithBumpStamp
		expected []string // Expected room ID order
	}{
		{
			name: "already sorted descending",
			input: []RoomWithBumpStamp{
				{RoomID: "!room1:test", BumpStamp: 100},
				{RoomID: "!room2:test", BumpStamp: 50},
				{RoomID: "!room3:test", BumpStamp: 25},
			},
			expected: []string{"!room1:test", "!room2:test", "!room3:test"},
		},
		{
			name: "reverse sorted",
			input: []RoomWithBumpStamp{
				{RoomID: "!room1:test", BumpStamp: 25},
				{RoomID: "!room2:test", BumpStamp: 50},
				{RoomID: "!room3:test", BumpStamp: 100},
			},
			expected: []string{"!room3:test", "!room2:test", "!room1:test"},
		},
		{
			name: "unsorted",
			input: []RoomWithBumpStamp{
				{RoomID: "!room1:test", BumpStamp: 50},
				{RoomID: "!room2:test", BumpStamp: 100},
				{RoomID: "!room3:test", BumpStamp: 25},
				{RoomID: "!room4:test", BumpStamp: 75},
			},
			expected: []string{"!room2:test", "!room4:test", "!room1:test", "!room3:test"},
		},
		{
			name: "equal timestamps - stable sort",
			input: []RoomWithBumpStamp{
				{RoomID: "!room1:test", BumpStamp: 50},
				{RoomID: "!room2:test", BumpStamp: 50},
				{RoomID: "!room3:test", BumpStamp: 50},
			},
			// With equal timestamps, Go's sort.Slice is NOT stable by default
			// but all should still be present
			expected: nil, // Will check length instead
		},
		{
			name:     "empty list",
			input:    []RoomWithBumpStamp{},
			expected: []string{},
		},
		{
			name: "single room",
			input: []RoomWithBumpStamp{
				{RoomID: "!room1:test", BumpStamp: 100},
			},
			expected: []string{"!room1:test"},
		},
		{
			name: "zero bump stamps",
			input: []RoomWithBumpStamp{
				{RoomID: "!room1:test", BumpStamp: 0},
				{RoomID: "!room2:test", BumpStamp: 100},
				{RoomID: "!room3:test", BumpStamp: 0},
			},
			expected: []string{"!room2:test", "!room1:test", "!room3:test"},
		},
		{
			name: "negative bump stamps",
			input: []RoomWithBumpStamp{
				{RoomID: "!room1:test", BumpStamp: -100},
				{RoomID: "!room2:test", BumpStamp: 50},
				{RoomID: "!room3:test", BumpStamp: -50},
			},
			expected: []string{"!room2:test", "!room3:test", "!room1:test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy to avoid modifying test data
			rooms := make([]RoomWithBumpStamp, len(tt.input))
			copy(rooms, tt.input)

			SortRoomsByActivity(rooms)

			if tt.expected == nil {
				// Just check all rooms are present
				assert.Len(t, rooms, len(tt.input))
			} else {
				// Extract room IDs for comparison
				resultIDs := make([]string, len(rooms))
				for i, room := range rooms {
					resultIDs[i] = room.RoomID
				}
				assert.Equal(t, tt.expected, resultIDs)
			}
		})
	}
}

// TestApplySlidingWindow tests the sliding window extraction
func TestApplySlidingWindow(t *testing.T) {
	rooms := []RoomWithBumpStamp{
		{RoomID: "!room0:test", BumpStamp: 100},
		{RoomID: "!room1:test", BumpStamp: 90},
		{RoomID: "!room2:test", BumpStamp: 80},
		{RoomID: "!room3:test", BumpStamp: 70},
		{RoomID: "!room4:test", BumpStamp: 60},
		{RoomID: "!room5:test", BumpStamp: 50},
		{RoomID: "!room6:test", BumpStamp: 40},
		{RoomID: "!room7:test", BumpStamp: 30},
		{RoomID: "!room8:test", BumpStamp: 20},
		{RoomID: "!room9:test", BumpStamp: 10},
	}

	tests := []struct {
		name        string
		rangeSpec   []int
		expectedIDs []string
	}{
		{
			name:        "first 5 rooms [0,4]",
			rangeSpec:   []int{0, 4},
			expectedIDs: []string{"!room0:test", "!room1:test", "!room2:test", "!room3:test", "!room4:test"},
		},
		{
			name:        "middle range [3,6]",
			rangeSpec:   []int{3, 6},
			expectedIDs: []string{"!room3:test", "!room4:test", "!room5:test", "!room6:test"},
		},
		{
			name:        "last 3 rooms [7,9]",
			rangeSpec:   []int{7, 9},
			expectedIDs: []string{"!room7:test", "!room8:test", "!room9:test"},
		},
		{
			name:        "single room [0,0]",
			rangeSpec:   []int{0, 0},
			expectedIDs: []string{"!room0:test"},
		},
		{
			name:        "all rooms [0,9]",
			rangeSpec:   []int{0, 9},
			expectedIDs: []string{"!room0:test", "!room1:test", "!room2:test", "!room3:test", "!room4:test", "!room5:test", "!room6:test", "!room7:test", "!room8:test", "!room9:test"},
		},
		{
			name:        "end beyond list bounds [5,20] - clamped",
			rangeSpec:   []int{5, 20},
			expectedIDs: []string{"!room5:test", "!room6:test", "!room7:test", "!room8:test", "!room9:test"},
		},
		{
			name:        "start beyond list bounds [15,20] - empty",
			rangeSpec:   []int{15, 20},
			expectedIDs: []string{},
		},
		{
			name:        "negative start clamped [−5,4]",
			rangeSpec:   []int{-5, 4},
			expectedIDs: []string{"!room0:test", "!room1:test", "!room2:test", "!room3:test", "!room4:test"},
		},
		{
			name:        "invalid range end < start [5,3] - clamped to [5,5]",
			rangeSpec:   []int{5, 3},
			expectedIDs: []string{"!room5:test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ApplySlidingWindow(rooms, tt.rangeSpec)

			resultIDs := make([]string, len(result))
			for i, room := range result {
				resultIDs[i] = room.RoomID
			}

			assert.Equal(t, tt.expectedIDs, resultIDs)
		})
	}
}

// TestApplySlidingWindowEdgeCases tests edge cases for sliding window
func TestApplySlidingWindowEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		rooms       []RoomWithBumpStamp
		rangeSpec   []int
		expectedLen int
	}{
		{
			name:        "empty rooms list",
			rooms:       []RoomWithBumpStamp{},
			rangeSpec:   []int{0, 5},
			expectedLen: 0,
		},
		{
			name: "invalid range spec (single element)",
			rooms: []RoomWithBumpStamp{
				{RoomID: "!room0:test"},
				{RoomID: "!room1:test"},
			},
			rangeSpec:   []int{0},
			expectedLen: 2, // Returns all rooms
		},
		{
			name: "invalid range spec (three elements)",
			rooms: []RoomWithBumpStamp{
				{RoomID: "!room0:test"},
				{RoomID: "!room1:test"},
			},
			rangeSpec:   []int{0, 1, 2},
			expectedLen: 2, // Returns all rooms
		},
		{
			name: "nil range spec",
			rooms: []RoomWithBumpStamp{
				{RoomID: "!room0:test"},
				{RoomID: "!room1:test"},
			},
			rangeSpec:   nil,
			expectedLen: 2, // Returns all rooms
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ApplySlidingWindow(tt.rooms, tt.rangeSpec)
			assert.Len(t, result, tt.expectedLen)
		})
	}
}

// TestGenerateSyncOperation tests SYNC operation generation
func TestGenerateSyncOperation(t *testing.T) {
	tests := []struct {
		name           string
		rooms          []RoomWithBumpStamp
		rangeSpec      []int
		expectedOp     string
		expectedRange  []int
		expectedRoomIDs []string
	}{
		{
			name: "basic sync operation",
			rooms: []RoomWithBumpStamp{
				{RoomID: "!room1:test", BumpStamp: 100},
				{RoomID: "!room2:test", BumpStamp: 90},
				{RoomID: "!room3:test", BumpStamp: 80},
			},
			rangeSpec:      []int{0, 2},
			expectedOp:     "SYNC",
			expectedRange:  []int{0, 2},
			expectedRoomIDs: []string{"!room1:test", "!room2:test", "!room3:test"},
		},
		{
			name:           "empty rooms",
			rooms:          []RoomWithBumpStamp{},
			rangeSpec:      []int{0, 0},
			expectedOp:     "SYNC",
			expectedRange:  []int{0, 0},
			expectedRoomIDs: []string{},
		},
		{
			name: "single room",
			rooms: []RoomWithBumpStamp{
				{RoomID: "!only:test", BumpStamp: 50},
			},
			rangeSpec:      []int{0, 0},
			expectedOp:     "SYNC",
			expectedRange:  []int{0, 0},
			expectedRoomIDs: []string{"!only:test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := GenerateSyncOperation(tt.rooms, tt.rangeSpec)

			assert.Equal(t, tt.expectedOp, op.Op)
			assert.Equal(t, tt.expectedRange, op.Range)
			assert.Equal(t, tt.expectedRoomIDs, op.RoomIDs)
		})
	}
}

// TestContains tests the contains helper function
func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		item     string
		expected bool
	}{
		{
			name:     "item present",
			slice:    []string{"a", "b", "c"},
			item:     "b",
			expected: true,
		},
		{
			name:     "item not present",
			slice:    []string{"a", "b", "c"},
			item:     "d",
			expected: false,
		},
		{
			name:     "empty slice",
			slice:    []string{},
			item:     "a",
			expected: false,
		},
		{
			name:     "nil slice",
			slice:    nil,
			item:     "a",
			expected: false,
		},
		{
			name:     "item is first",
			slice:    []string{"a", "b", "c"},
			item:     "a",
			expected: true,
		},
		{
			name:     "item is last",
			slice:    []string{"a", "b", "c"},
			item:     "c",
			expected: true,
		},
		{
			name:     "empty string in slice",
			slice:    []string{"", "a", "b"},
			item:     "",
			expected: true,
		},
		{
			name:     "case sensitive",
			slice:    []string{"a", "B", "c"},
			item:     "b",
			expected: false,
		},
		{
			name:     "single element slice - match",
			slice:    []string{"only"},
			item:     "only",
			expected: true,
		},
		{
			name:     "single element slice - no match",
			slice:    []string{"only"},
			item:     "other",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contains(tt.slice, tt.item)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestEqualStringSlices tests the equalStringSlices helper function
func TestEqualStringSlices(t *testing.T) {
	tests := []struct {
		name     string
		a        []string
		b        []string
		expected bool
	}{
		{
			name:     "identical slices",
			a:        []string{"a", "b", "c"},
			b:        []string{"a", "b", "c"},
			expected: true,
		},
		{
			name:     "different elements",
			a:        []string{"a", "b", "c"},
			b:        []string{"a", "b", "d"},
			expected: false,
		},
		{
			name:     "different lengths",
			a:        []string{"a", "b"},
			b:        []string{"a", "b", "c"},
			expected: false,
		},
		{
			name:     "same elements different order",
			a:        []string{"a", "b", "c"},
			b:        []string{"c", "b", "a"},
			expected: false,
		},
		{
			name:     "both empty",
			a:        []string{},
			b:        []string{},
			expected: true,
		},
		{
			name:     "one empty",
			a:        []string{"a"},
			b:        []string{},
			expected: false,
		},
		{
			name:     "both nil",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "nil vs empty",
			a:        nil,
			b:        []string{},
			expected: true, // len(nil) == len([]) == 0
		},
		{
			name:     "single element match",
			a:        []string{"only"},
			b:        []string{"only"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := equalStringSlices(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}
