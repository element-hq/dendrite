// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package input

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/element-hq/dendrite/roomserver/types"
)

// TestMembershipChanges tests the membershipChanges helper function
func TestMembershipChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		removed  []types.StateEntry
		added    []types.StateEntry
		expected int // expected number of membership changes
	}{
		{
			name: "single membership change",
			removed: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 1}, EventNID: 10},
			},
			added: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 1}, EventNID: 20},
			},
			expected: 1,
		},
		{
			name: "multiple membership changes",
			removed: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 1}, EventNID: 10},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 2}, EventNID: 11},
			},
			added: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 1}, EventNID: 20},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 2}, EventNID: 21},
			},
			expected: 2,
		},
		{
			name: "non-membership changes filtered out",
			removed: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomPowerLevelsNID, EventStateKeyNID: 1}, EventNID: 10},
			},
			added: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomPowerLevelsNID, EventStateKeyNID: 1}, EventNID: 20},
			},
			expected: 0, // power level changes don't count as membership changes
		},
		{
			name: "mixed membership and non-membership changes",
			removed: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 1}, EventNID: 10},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomJoinRulesNID, EventStateKeyNID: 1}, EventNID: 11},
			},
			added: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 1}, EventNID: 20},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomJoinRulesNID, EventStateKeyNID: 1}, EventNID: 21},
			},
			expected: 1, // only the membership change
		},
		{
			name:     "empty state changes",
			removed:  []types.StateEntry{},
			added:    []types.StateEntry{},
			expected: 0,
		},
		{
			name: "membership added only",
			removed: []types.StateEntry{},
			added: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 1}, EventNID: 20},
			},
			expected: 1,
		},
		{
			name: "membership removed only",
			removed: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 1}, EventNID: 10},
			},
			added:    []types.StateEntry{},
			expected: 1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			changes := membershipChanges(tt.removed, tt.added)
			assert.Equal(t, tt.expected, len(changes), "unexpected number of membership changes")

			// Verify all returned changes are membership events
			for _, change := range changes {
				assert.Equal(t, types.EventTypeNID(types.MRoomMemberNID), change.EventTypeNID, "non-membership event in changes")
			}
		})
	}
}

// TestPairUpChanges tests the pairUpChanges helper function
func TestPairUpChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		removed          []types.StateEntry
		added            []types.StateEntry
		expectedPairings int
	}{
		{
			name: "matching pairs",
			removed: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 10},
			},
			added: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 20},
			},
			expectedPairings: 1,
		},
		{
			name: "different state keys",
			removed: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 10},
			},
			added: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 2}, EventNID: 20},
			},
			expectedPairings: 2, // separate entries for each state key
		},
		{
			name: "only additions",
			removed: []types.StateEntry{},
			added: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 20},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1}, EventNID: 21},
			},
			expectedPairings: 2,
		},
		{
			name: "only removals",
			removed: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 10},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1}, EventNID: 11},
			},
			added:            []types.StateEntry{},
			expectedPairings: 2,
		},
		{
			name:             "empty state changes",
			removed:          []types.StateEntry{},
			added:            []types.StateEntry{},
			expectedPairings: 0,
		},
		{
			name: "complex pairing scenario",
			removed: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 10},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 2}, EventNID: 11},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1}, EventNID: 12},
			},
			added: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 20},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 3, EventStateKeyNID: 1}, EventNID: 21},
			},
			expectedPairings: 4, // (1,1) paired, (1,2) removed only, (2,1) removed only, (3,1) added only
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			changes := pairUpChanges(tt.removed, tt.added)
			assert.Equal(t, tt.expectedPairings, len(changes), "unexpected number of pairings")

			// Verify the pairing logic
			for _, change := range changes {
				// Find corresponding entries in removed and added
				var foundRemoved, foundAdded bool
				for _, r := range tt.removed {
					if r.StateKeyTuple == change.StateKeyTuple {
						foundRemoved = true
						assert.Equal(t, r.EventNID, change.removedEventNID, "removed event NID mismatch")
					}
				}
				for _, a := range tt.added {
					if a.StateKeyTuple == change.StateKeyTuple {
						foundAdded = true
						assert.Equal(t, a.EventNID, change.addedEventNID, "added event NID mismatch")
					}
				}

				// At least one of removed or added should be found
				assert.True(t, foundRemoved || foundAdded, "change not found in removed or added lists")
			}
		})
	}
}

// TestStateChange tests the stateChange structure
func TestStateChange(t *testing.T) {
	t.Parallel()

	sc := stateChange{
		StateKeyTuple: types.StateKeyTuple{
			EventTypeNID:     types.MRoomMemberNID,
			EventStateKeyNID: 42,
		},
		removedEventNID: 100,
		addedEventNID:   200,
	}

	assert.Equal(t, types.EventTypeNID(types.MRoomMemberNID), sc.EventTypeNID)
	assert.Equal(t, types.EventStateKeyNID(42), sc.EventStateKeyNID)
	assert.Equal(t, types.EventNID(100), sc.removedEventNID)
	assert.Equal(t, types.EventNID(200), sc.addedEventNID)
}

// TestMembershipChanges_Deduplication tests that duplicate state entries are handled correctly
func TestMembershipChanges_Deduplication(t *testing.T) {
	t.Parallel()

	// Same membership change listed twice in removed/added
	removed := []types.StateEntry{
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 1}, EventNID: 10},
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 1}, EventNID: 10},
	}
	added := []types.StateEntry{
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 1}, EventNID: 20},
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 1}, EventNID: 20},
	}

	changes := membershipChanges(removed, added)

	// Should still result in a single change (last one wins in the map)
	assert.Equal(t, 1, len(changes))
}

// TestPairUpChanges_VerifyBothSides tests that paired changes have correct NIDs
func TestPairUpChanges_VerifyBothSides(t *testing.T) {
	t.Parallel()

	tuple := types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 1}
	removed := []types.StateEntry{
		{StateKeyTuple: tuple, EventNID: 10},
	}
	added := []types.StateEntry{
		{StateKeyTuple: tuple, EventNID: 20},
	}

	changes := pairUpChanges(removed, added)
	require.Equal(t, 1, len(changes))

	change := changes[0]
	assert.Equal(t, tuple, change.StateKeyTuple)
	assert.Equal(t, types.EventNID(10), change.removedEventNID)
	assert.Equal(t, types.EventNID(20), change.addedEventNID)
}

// TestPairUpChanges_OnlyAdditions tests pairing when only additions are present
func TestPairUpChanges_OnlyAdditions(t *testing.T) {
	t.Parallel()

	added := []types.StateEntry{
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 20},
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 2}, EventNID: 21},
	}

	changes := pairUpChanges([]types.StateEntry{}, added)
	assert.Equal(t, 2, len(changes))

	for _, change := range changes {
		assert.Equal(t, types.EventNID(0), change.removedEventNID, "removed should be zero")
		assert.NotEqual(t, types.EventNID(0), change.addedEventNID, "added should be non-zero")
	}
}

// TestPairUpChanges_OnlyRemovals tests pairing when only removals are present
func TestPairUpChanges_OnlyRemovals(t *testing.T) {
	t.Parallel()

	removed := []types.StateEntry{
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 10},
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 2}, EventNID: 11},
	}

	changes := pairUpChanges(removed, []types.StateEntry{})
	assert.Equal(t, 2, len(changes))

	for _, change := range changes {
		assert.NotEqual(t, types.EventNID(0), change.removedEventNID, "removed should be non-zero")
		assert.Equal(t, types.EventNID(0), change.addedEventNID, "added should be zero")
	}
}

// TestMembershipChanges_MultipleUsersJoinLeave tests multiple membership changes
func TestMembershipChanges_MultipleUsersJoinLeave(t *testing.T) {
	t.Parallel()

	// Simulate: User 1 joins, User 2 leaves, User 3's membership unchanged (power levels change instead)
	removed := []types.StateEntry{
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 1}, EventNID: 10},       // User 1 old state
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 2}, EventNID: 11},       // User 2 old state
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomPowerLevelsNID, EventStateKeyNID: 0}, EventNID: 12}, // Power levels old state
	}
	added := []types.StateEntry{
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 1}, EventNID: 20},       // User 1 new state (joined)
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 2}, EventNID: 21},       // User 2 new state (left)
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomPowerLevelsNID, EventStateKeyNID: 0}, EventNID: 22}, // Power levels new state
	}

	changes := membershipChanges(removed, added)

	// Should have 2 membership changes (User 1 and User 2), power levels change filtered out
	assert.Equal(t, 2, len(changes))

	// All changes should be for membership events
	for _, change := range changes {
		assert.Equal(t, types.EventTypeNID(types.MRoomMemberNID), change.EventTypeNID)
	}
}

// TestStateKeyTuple_Equality tests StateKeyTuple comparison
func TestStateKeyTuple_Equality(t *testing.T) {
	t.Parallel()

	tuple1 := types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 2}
	tuple2 := types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 2}
	tuple3 := types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 3}
	tuple4 := types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 2}

	assert.Equal(t, tuple1, tuple2, "identical tuples should be equal")
	assert.NotEqual(t, tuple1, tuple3, "tuples with different state keys should not be equal")
	assert.NotEqual(t, tuple1, tuple4, "tuples with different event types should not be equal")
}

// TestPairUpChanges_ComplexScenario tests a realistic state change scenario
func TestPairUpChanges_ComplexScenario(t *testing.T) {
	t.Parallel()

	// Simulate a room state change:
	// - Alice's membership updated (leave -> join)
	// - Bob joins (no previous membership)
	// - Charlie leaves (no new membership)
	// - Room power levels change
	// - Room join rules change

	alice := types.EventStateKeyNID(1)
	bob := types.EventStateKeyNID(2)
	charlie := types.EventStateKeyNID(3)

	removed := []types.StateEntry{
		// Alice was in the room
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: alice}, EventNID: 10},
		// Charlie was in the room
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: charlie}, EventNID: 11},
		// Old room power levels
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomPowerLevelsNID, EventStateKeyNID: 0}, EventNID: 12},
		// Old room join rules
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomJoinRulesNID, EventStateKeyNID: 0}, EventNID: 13},
	}

	added := []types.StateEntry{
		// Alice rejoined
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: alice}, EventNID: 20},
		// Bob joined (new)
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: bob}, EventNID: 21},
		// New room power levels
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomPowerLevelsNID, EventStateKeyNID: 0}, EventNID: 22},
		// New room join rules
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: types.MRoomJoinRulesNID, EventStateKeyNID: 0}, EventNID: 23},
	}

	changes := pairUpChanges(removed, added)

	// Should have 5 state changes total:
	// 1. Alice membership (removed + added)
	// 2. Bob membership (added only)
	// 3. Charlie membership (removed only)
	// 4. Room power levels (removed + added)
	// 5. Room join rules (removed + added)
	assert.Equal(t, 5, len(changes), "should have 5 distinct state key tuples")

	// Extract just membership changes
	membershipChanges := membershipChanges(removed, added)
	assert.Equal(t, 3, len(membershipChanges), "should have 3 membership changes")

	// Verify Alice's change has both removed and added
	var aliceChange *stateChange
	for i, change := range membershipChanges {
		if change.EventStateKeyNID == alice {
			aliceChange = &membershipChanges[i]
			break
		}
	}
	require.NotNil(t, aliceChange, "Alice's membership change should exist")
	assert.Equal(t, types.EventNID(10), aliceChange.removedEventNID)
	assert.Equal(t, types.EventNID(20), aliceChange.addedEventNID)

	// Verify Bob's change has only added
	var bobChange *stateChange
	for i, change := range membershipChanges {
		if change.EventStateKeyNID == bob {
			bobChange = &membershipChanges[i]
			break
		}
	}
	require.NotNil(t, bobChange, "Bob's membership change should exist")
	assert.Equal(t, types.EventNID(0), bobChange.removedEventNID, "Bob had no previous membership")
	assert.Equal(t, types.EventNID(21), bobChange.addedEventNID)

	// Verify Charlie's change has only removed
	var charlieChange *stateChange
	for i, change := range membershipChanges {
		if change.EventStateKeyNID == charlie {
			charlieChange = &membershipChanges[i]
			break
		}
	}
	require.NotNil(t, charlieChange, "Charlie's membership change should exist")
	assert.Equal(t, types.EventNID(11), charlieChange.removedEventNID)
	assert.Equal(t, types.EventNID(0), charlieChange.addedEventNID, "Charlie has no new membership")
}
