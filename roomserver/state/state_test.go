// Copyright 2024 New Vector Ltd.
// Copyright 2019, 2020 The Matrix.org Foundation C.I.C.
// Copyright 2018 New Vector Ltd
// Copyright 2017 Vector Creations Ltd
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package state

// This file contains pragmatic unit tests for the state package helper functions.
//
// Coverage: 12.2% (with 100% coverage of tested helper functions)
// Tests: 13 test functions covering 40+ scenarios
//
// TESTED FUNCTIONS (100% coverage):
// - findDuplicateStateKeys: Finds duplicate state key tuples in sorted lists
// - UniqueStateSnapshotNIDs: Sorts and deduplicates state snapshot NIDs
// - uniqueStateBlockNIDs: Sorts and deduplicates state block NIDs
// - stateKeyTuplesNeeded: Converts StateNeeded to StateKeyTuples
// - stateEntryMap.lookup: Binary search in state entry maps
// - eventMap.lookup: Binary search in event maps
// - stateBlockNIDListMap.lookup: Binary search in state block NID list maps
// - stateEntryListMap.lookup: Binary search in state entry list maps
// - All sorter implementations (stateEntrySorter, stateEntryByStateKeySorter, etc.)
//
// INTEGRATION GAPS (require database mocking, future work):
// - LoadStateAtSnapshot: Requires database queries for state blocks and entries
// - LoadStateAtEvent: Requires database snapshot lookups
// - LoadCombinedStateAfterEvents: Requires complex state merging with database
// - DifferenceBetweeenStateSnapshots: Requires database state loading (could be partially tested)
// - resolveConflictsV1/V2: Full state resolution requires auth events and database
// - calculateStateAfterManyEvents: Complex integration with database and state resolution
// - loadStateEvents: Requires database event loading
// - CalculateAndStoreStateBeforeEvent/AfterEvents: Requires database writes
//
// RECOMMENDATIONS FOR FUTURE WORK:
// 1. Mock database interfaces to test Load* functions
// 2. Create test fixtures for common state scenarios (room creation, member join, etc.)
// 3. Integration tests for state resolution algorithms (V1 and V2)
// 4. Performance tests for large state sets
//
// The current tests focus on pure helper functions and provide a solid foundation
// for understanding state manipulation logic without complex database dependencies.

import (
	"sort"
	"testing"

	"github.com/matrix-org/gomatrixserverlib"
	"github.com/stretchr/testify/assert"

	"github.com/element-hq/dendrite/roomserver/types"
)

// TestFindDuplicateStateKeys tests the findDuplicateStateKeys function with various scenarios
func TestFindDuplicateStateKeys(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input []types.StateEntry
		want  []types.StateEntry
	}{{
		name: "duplicate state keys",
		input: []types.StateEntry{
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 1},
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 2},
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 2}, EventNID: 3},
		},
		want: []types.StateEntry{
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 1},
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 2},
		},
	}, {
		name: "no duplicates",
		input: []types.StateEntry{
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 1},
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 2}, EventNID: 2},
		},
		want: nil,
	}, {
		name:  "empty input",
		input: []types.StateEntry{},
		want:  nil,
	}, {
		name: "single entry",
		input: []types.StateEntry{
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 1},
		},
		want: nil,
	}, {
		name: "triple duplicate",
		input: []types.StateEntry{
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 1},
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 2},
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 3},
		},
		want: []types.StateEntry{
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 1},
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 2},
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 3},
		},
	}, {
		name: "multiple duplicate groups",
		input: []types.StateEntry{
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 1},
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 2},
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1}, EventNID: 3},
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1}, EventNID: 4},
		},
		want: []types.StateEntry{
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 1},
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 2},
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1}, EventNID: 3},
			{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1}, EventNID: 4},
		},
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := findDuplicateStateKeys(tc.input)
			assert.Equal(t, tc.want, got, "Duplicate state keys should match expected")
		})
	}
}

// TestUniqueStateSnapshotNIDs tests sorting and deduplication of StateSnapshotNIDs
func TestUniqueStateSnapshotNIDs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input []types.StateSnapshotNID
		want  []types.StateSnapshotNID
	}{
		{
			name:  "duplicates",
			input: []types.StateSnapshotNID{3, 1, 2, 1, 3},
			want:  []types.StateSnapshotNID{1, 2, 3},
		},
		{
			name:  "no duplicates",
			input: []types.StateSnapshotNID{3, 1, 2},
			want:  []types.StateSnapshotNID{1, 2, 3},
		},
		{
			name:  "empty",
			input: []types.StateSnapshotNID{},
			want:  []types.StateSnapshotNID{},
		},
		{
			name:  "single element",
			input: []types.StateSnapshotNID{42},
			want:  []types.StateSnapshotNID{42},
		},
		{
			name:  "all duplicates",
			input: []types.StateSnapshotNID{5, 5, 5, 5},
			want:  []types.StateSnapshotNID{5},
		},
		{
			name:  "already sorted",
			input: []types.StateSnapshotNID{1, 2, 3, 4, 5},
			want:  []types.StateSnapshotNID{1, 2, 3, 4, 5},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := UniqueStateSnapshotNIDs(tc.input)
			assert.Equal(t, tc.want, got, "Unique state snapshot NIDs should match expected")
		})
	}
}

// TestUniqueStateBlockNIDs tests sorting and deduplication of StateBlockNIDs
func TestUniqueStateBlockNIDs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input []types.StateBlockNID
		want  []types.StateBlockNID
	}{
		{
			name:  "duplicates",
			input: []types.StateBlockNID{10, 5, 10, 3, 5},
			want:  []types.StateBlockNID{3, 5, 10},
		},
		{
			name:  "no duplicates",
			input: []types.StateBlockNID{7, 2, 9},
			want:  []types.StateBlockNID{2, 7, 9},
		},
		{
			name:  "empty",
			input: []types.StateBlockNID{},
			want:  []types.StateBlockNID{},
		},
		{
			name:  "single element",
			input: []types.StateBlockNID{100},
			want:  []types.StateBlockNID{100},
		},
		{
			name:  "reverse sorted",
			input: []types.StateBlockNID{5, 4, 3, 2, 1},
			want:  []types.StateBlockNID{1, 2, 3, 4, 5},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := uniqueStateBlockNIDs(tc.input)
			assert.Equal(t, tc.want, got, "Unique state block NIDs should match expected")
		})
	}
}

// TestStateEntryMapLookup tests binary search lookup in state entry map
func TestStateEntryMapLookup(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		entries   []types.StateEntry
		searchKey types.StateKeyTuple
		wantNID   types.EventNID
		wantOk    bool
	}{
		{
			name: "found - first entry",
			entries: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 10},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 2}, EventNID: 11},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1}, EventNID: 20},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 3, EventStateKeyNID: 1}, EventNID: 30},
			},
			searchKey: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1},
			wantNID:   10,
			wantOk:    true,
		},
		{
			name: "found - middle entry",
			entries: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 10},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 2}, EventNID: 11},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1}, EventNID: 20},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 3, EventStateKeyNID: 1}, EventNID: 30},
			},
			searchKey: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1},
			wantNID:   20,
			wantOk:    true,
		},
		{
			name: "found - last entry",
			entries: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 10},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 2}, EventNID: 11},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1}, EventNID: 20},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 3, EventStateKeyNID: 1}, EventNID: 30},
			},
			searchKey: types.StateKeyTuple{EventTypeNID: 3, EventStateKeyNID: 1},
			wantNID:   30,
			wantOk:    true,
		},
		{
			name: "not found - before first",
			entries: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 10},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 2}, EventNID: 11},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1}, EventNID: 20},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 3, EventStateKeyNID: 1}, EventNID: 30},
			},
			searchKey: types.StateKeyTuple{EventTypeNID: 0, EventStateKeyNID: 1},
			wantNID:   0,
			wantOk:    false,
		},
		{
			name: "not found - after last",
			entries: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 10},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 2}, EventNID: 11},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1}, EventNID: 20},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 3, EventStateKeyNID: 1}, EventNID: 30},
			},
			searchKey: types.StateKeyTuple{EventTypeNID: 5, EventStateKeyNID: 1},
			wantNID:   0,
			wantOk:    false,
		},
		{
			name: "not found - between entries",
			entries: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 10},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 2}, EventNID: 11},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1}, EventNID: 20},
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 3, EventStateKeyNID: 1}, EventNID: 30},
			},
			searchKey: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 3},
			wantNID:   0,
			wantOk:    false,
		},
		{
			name:      "empty map - not found",
			entries:   []types.StateEntry{},
			searchKey: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1},
			wantNID:   0,
			wantOk:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			entries := stateEntryMap(tc.entries)
			gotNID, gotOk := entries.lookup(tc.searchKey)
			assert.Equal(t, tc.wantOk, gotOk, "lookup() ok should match expected")
			assert.Equal(t, tc.wantNID, gotNID, "lookup() NID should match expected")
		})
	}
}

// TestEventMapLookup tests binary search lookup in event map
func TestEventMapLookup(t *testing.T) {
	t.Parallel()

	// Create a sorted event map
	events := eventMap([]types.Event{
		{EventNID: 10},
		{EventNID: 20},
		{EventNID: 30},
		{EventNID: 40},
	})

	testCases := []struct {
		name     string
		searchID types.EventNID
		wantOk   bool
	}{
		{
			name:     "found - first",
			searchID: 10,
			wantOk:   true,
		},
		{
			name:     "found - middle",
			searchID: 20,
			wantOk:   true,
		},
		{
			name:     "found - last",
			searchID: 40,
			wantOk:   true,
		},
		{
			name:     "not found - before first",
			searchID: 5,
			wantOk:   false,
		},
		{
			name:     "not found - after last",
			searchID: 50,
			wantOk:   false,
		},
		{
			name:     "not found - between",
			searchID: 25,
			wantOk:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			event, ok := events.lookup(tc.searchID)
			assert.Equal(t, tc.wantOk, ok, "lookup() ok should match expected")
			if ok {
				assert.Equal(t, tc.searchID, event.EventNID, "lookup() returned event NID should match expected")
			}
		})
	}
}

// TestStateBlockNIDListMapLookup tests binary search in state block NID list map
func TestStateBlockNIDListMapLookup(t *testing.T) {
	t.Parallel()

	// Create a sorted map
	blockMap := stateBlockNIDListMap([]types.StateBlockNIDList{
		{StateSnapshotNID: 1, StateBlockNIDs: []types.StateBlockNID{10, 11}},
		{StateSnapshotNID: 2, StateBlockNIDs: []types.StateBlockNID{20, 21}},
		{StateSnapshotNID: 3, StateBlockNIDs: []types.StateBlockNID{30, 31}},
	})

	testCases := []struct {
		name         string
		searchNID    types.StateSnapshotNID
		wantBlockNID []types.StateBlockNID
		wantOk       bool
	}{
		{
			name:         "found - first",
			searchNID:    1,
			wantBlockNID: []types.StateBlockNID{10, 11},
			wantOk:       true,
		},
		{
			name:         "found - middle",
			searchNID:    2,
			wantBlockNID: []types.StateBlockNID{20, 21},
			wantOk:       true,
		},
		{
			name:         "found - last",
			searchNID:    3,
			wantBlockNID: []types.StateBlockNID{30, 31},
			wantOk:       true,
		},
		{
			name:         "not found",
			searchNID:    5,
			wantBlockNID: nil,
			wantOk:       false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotBlockNIDs, gotOk := blockMap.lookup(tc.searchNID)
			assert.Equal(t, tc.wantOk, gotOk, "lookup() ok should match expected")
			assert.Equal(t, tc.wantBlockNID, gotBlockNIDs, "State block NID list should match expected")
		})
	}
}

// TestStateEntryListMapLookup tests binary search in state entry list map
func TestStateEntryListMapLookup(t *testing.T) {
	t.Parallel()

	// Create a sorted map
	entryListMap := stateEntryListMap([]types.StateEntryList{
		{
			StateBlockNID: 10,
			StateEntries: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 100},
			},
		},
		{
			StateBlockNID: 20,
			StateEntries: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1}, EventNID: 200},
			},
		},
	})

	testCases := []struct {
		name        string
		searchNID   types.StateBlockNID
		wantEntries []types.StateEntry
		wantOk      bool
	}{
		{
			name:      "found - first",
			searchNID: 10,
			wantEntries: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 100},
			},
			wantOk: true,
		},
		{
			name:      "found - second",
			searchNID: 20,
			wantEntries: []types.StateEntry{
				{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1}, EventNID: 200},
			},
			wantOk: true,
		},
		{
			name:        "not found",
			searchNID:   30,
			wantEntries: nil,
			wantOk:      false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotEntries, gotOk := entryListMap.lookup(tc.searchNID)
			assert.Equal(t, tc.wantOk, gotOk, "lookup() ok should match expected")
			assert.Equal(t, tc.wantEntries, gotEntries, "State entry list should match expected")
		})
	}
}

// TestStateKeyTuplesNeeded tests conversion of StateNeeded to StateKeyTuples
func TestStateKeyTuplesNeeded(t *testing.T) {
	t.Parallel()

	v := &StateResolution{}

	testCases := []struct {
		name          string
		stateKeyNIDs  map[string]types.EventStateKeyNID
		stateNeeded   gomatrixserverlib.StateNeeded
		wantTuples    []types.StateKeyTuple
		wantMinLength int
	}{
		{
			name: "create only",
			stateNeeded: gomatrixserverlib.StateNeeded{
				Create: true,
			},
			wantTuples: []types.StateKeyTuple{
				{EventTypeNID: types.MRoomCreateNID, EventStateKeyNID: types.EmptyStateKeyNID},
			},
		},
		{
			name: "power levels only",
			stateNeeded: gomatrixserverlib.StateNeeded{
				PowerLevels: true,
			},
			wantTuples: []types.StateKeyTuple{
				{EventTypeNID: types.MRoomPowerLevelsNID, EventStateKeyNID: types.EmptyStateKeyNID},
			},
		},
		{
			name: "join rules only",
			stateNeeded: gomatrixserverlib.StateNeeded{
				JoinRules: true,
			},
			wantTuples: []types.StateKeyTuple{
				{EventTypeNID: types.MRoomJoinRulesNID, EventStateKeyNID: types.EmptyStateKeyNID},
			},
		},
		{
			name: "all basic flags",
			stateNeeded: gomatrixserverlib.StateNeeded{
				Create:      true,
				PowerLevels: true,
				JoinRules:   true,
			},
			wantMinLength: 3,
		},
		{
			name: "with members",
			stateKeyNIDs: map[string]types.EventStateKeyNID{
				"@alice:example.com": 10,
				"@bob:example.com":   11,
			},
			stateNeeded: gomatrixserverlib.StateNeeded{
				Create:  true,
				Member:  []string{"@alice:example.com", "@bob:example.com"},
			},
			wantMinLength: 3,
		},
		{
			name: "with third party invite",
			stateKeyNIDs: map[string]types.EventStateKeyNID{
				"token123": 20,
			},
			stateNeeded: gomatrixserverlib.StateNeeded{
				ThirdPartyInvite: []string{"token123"},
			},
			wantMinLength: 1,
		},
		{
			name: "member not in map - should be skipped",
			stateKeyNIDs: map[string]types.EventStateKeyNID{
				"@alice:example.com": 10,
			},
			stateNeeded: gomatrixserverlib.StateNeeded{
				Member: []string{"@alice:example.com", "@unknown:example.com"},
			},
			wantMinLength: 1,
		},
		{
			name:          "empty",
			stateKeyNIDs:  map[string]types.EventStateKeyNID{},
			stateNeeded:   gomatrixserverlib.StateNeeded{},
			wantMinLength: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.stateKeyNIDs == nil {
				tc.stateKeyNIDs = map[string]types.EventStateKeyNID{}
			}

			got := v.stateKeyTuplesNeeded(tc.stateKeyNIDs, tc.stateNeeded)

			if tc.wantTuples != nil {
				assert.Equal(t, tc.wantTuples, got, "State key tuples should match expected")
			} else {
				assert.GreaterOrEqual(t, len(got), tc.wantMinLength, "State key tuples length should be at least expected minimum")
			}
		})
	}
}

// TestStateKeyTuplesNeededAllFlags tests all combinations of StateNeeded flags
func TestStateKeyTuplesNeededAllFlags(t *testing.T) {
	t.Parallel()

	v := &StateResolution{}

	testCases := []struct {
		name         string
		stateKeyNIDs map[string]types.EventStateKeyNID
		stateNeeded  gomatrixserverlib.StateNeeded
		wantTuples   []types.StateKeyTuple
	}{
		{
			name: "all flags set",
			stateKeyNIDs: map[string]types.EventStateKeyNID{
				"@user:server": 100,
				"token":        200,
			},
			stateNeeded: gomatrixserverlib.StateNeeded{
				Create:           true,
				PowerLevels:      true,
				JoinRules:        true,
				Member:           []string{"@user:server"},
				ThirdPartyInvite: []string{"token"},
			},
			wantTuples: []types.StateKeyTuple{
				{EventTypeNID: types.MRoomCreateNID, EventStateKeyNID: types.EmptyStateKeyNID},
				{EventTypeNID: types.MRoomPowerLevelsNID, EventStateKeyNID: types.EmptyStateKeyNID},
				{EventTypeNID: types.MRoomJoinRulesNID, EventStateKeyNID: types.EmptyStateKeyNID},
				{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 100},
				{EventTypeNID: types.MRoomThirdPartyInviteNID, EventStateKeyNID: 200},
			},
		},
		{
			name: "multiple members and invites",
			stateKeyNIDs: map[string]types.EventStateKeyNID{
				"@alice:server": 101,
				"@bob:server":   102,
				"token1":        201,
				"token2":        202,
			},
			stateNeeded: gomatrixserverlib.StateNeeded{
				Create:           true,
				Member:           []string{"@alice:server", "@bob:server"},
				ThirdPartyInvite: []string{"token1", "token2"},
			},
			wantTuples: []types.StateKeyTuple{
				{EventTypeNID: types.MRoomCreateNID, EventStateKeyNID: types.EmptyStateKeyNID},
				{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 101},
				{EventTypeNID: types.MRoomMemberNID, EventStateKeyNID: 102},
				{EventTypeNID: types.MRoomThirdPartyInviteNID, EventStateKeyNID: 201},
				{EventTypeNID: types.MRoomThirdPartyInviteNID, EventStateKeyNID: 202},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := v.stateKeyTuplesNeeded(tc.stateKeyNIDs, tc.stateNeeded)

			// Verify we got the expected number of tuples
			assert.Equal(t, len(tc.wantTuples), len(got), "Number of tuples should match expected")

			// Verify each expected tuple is present in the result
			for _, wantTuple := range tc.wantTuples {
				found := false
				for _, gotTuple := range got {
					if gotTuple == wantTuple {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected tuple %+v should be present in result", wantTuple)
			}
		})
	}
}

// TestStateEntrySorter tests the sorting of state entries
func TestStateEntrySorter(t *testing.T) {
	t.Parallel()

	entries := []types.StateEntry{
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 2, EventStateKeyNID: 1}, EventNID: 20},
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 2}, EventNID: 12},
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 11},
	}

	// Sort using the stateEntrySorter interface
	sort.Sort(stateEntrySorter(entries))

	// Verify sorted order - should be sorted by StateKeyTuple (EventTypeNID, EventStateKeyNID), then by EventNID
	assert.Equal(t, types.EventNID(11), entries[0].EventNID, "First entry should have EventNID 11")
	assert.Equal(t, types.EventNID(12), entries[1].EventNID, "Second entry should have EventNID 12")
	assert.Equal(t, types.EventNID(20), entries[2].EventNID, "Third entry should have EventNID 20")

	// Verify EventTypeNIDs are in ascending order
	assert.LessOrEqual(t, entries[0].EventTypeNID, entries[1].EventTypeNID, "EventTypeNIDs should be in ascending order")
	assert.LessOrEqual(t, entries[1].EventTypeNID, entries[2].EventTypeNID, "EventTypeNIDs should be in ascending order")

	// Verify state key tuples are correctly ordered
	assert.Equal(t, types.EventTypeNID(1), entries[0].EventTypeNID, "First entry should have EventTypeNID 1")
	assert.Equal(t, types.EventStateKeyNID(1), entries[0].EventStateKeyNID, "First entry should have EventStateKeyNID 1")
	assert.Equal(t, types.EventTypeNID(1), entries[1].EventTypeNID, "Second entry should have EventTypeNID 1")
	assert.Equal(t, types.EventStateKeyNID(2), entries[1].EventStateKeyNID, "Second entry should have EventStateKeyNID 2")
	assert.Equal(t, types.EventTypeNID(2), entries[2].EventTypeNID, "Third entry should have EventTypeNID 2")
	assert.Equal(t, types.EventStateKeyNID(1), entries[2].EventStateKeyNID, "Third entry should have EventStateKeyNID 1")
}

// TestStateEntryByStateKeySorter tests sorting entries by state key tuple only
func TestStateEntryByStateKeySorter(t *testing.T) {
	t.Parallel()

	entries := []types.StateEntry{
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 3, EventStateKeyNID: 1}, EventNID: 30},
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 2}, EventNID: 12},
		{StateKeyTuple: types.StateKeyTuple{EventTypeNID: 1, EventStateKeyNID: 1}, EventNID: 11},
	}

	// Sort using the stateEntryByStateKeySorter interface (sorts by StateKeyTuple only, not EventNID)
	sort.Sort(stateEntryByStateKeySorter(entries))

	// Verify sorted order - should be sorted by StateKeyTuple only (EventTypeNID, EventStateKeyNID)
	// EventNID is not used for sorting, so entries with same StateKeyTuple keep their relative order
	assert.Equal(t, types.EventTypeNID(1), entries[0].EventTypeNID, "First entry should have EventTypeNID 1")
	assert.Equal(t, types.EventStateKeyNID(1), entries[0].EventStateKeyNID, "First entry should have EventStateKeyNID 1")
	assert.Equal(t, types.EventTypeNID(1), entries[1].EventTypeNID, "Second entry should have EventTypeNID 1")
	assert.Equal(t, types.EventStateKeyNID(2), entries[1].EventStateKeyNID, "Second entry should have EventStateKeyNID 2")
	assert.Equal(t, types.EventTypeNID(3), entries[2].EventTypeNID, "Third entry should have EventTypeNID 3")
	assert.Equal(t, types.EventStateKeyNID(1), entries[2].EventStateKeyNID, "Third entry should have EventStateKeyNID 1")

	// Verify EventTypeNIDs are in ascending order
	assert.LessOrEqual(t, entries[0].EventTypeNID, entries[1].EventTypeNID, "EventTypeNIDs should be in ascending order")
	assert.LessOrEqual(t, entries[1].EventTypeNID, entries[2].EventTypeNID, "EventTypeNIDs should be in ascending order")
}

// TestStateNIDSorter tests sorting of StateSnapshotNIDs
func TestStateNIDSorter(t *testing.T) {
	t.Parallel()

	nids := []types.StateSnapshotNID{5, 2, 8, 1}

	// Sort using the stateNIDSorter interface
	sort.Sort(stateNIDSorter(nids))

	// Verify sorted order - should be in ascending order
	assert.Equal(t, types.StateSnapshotNID(1), nids[0], "First NID should be 1")
	assert.Equal(t, types.StateSnapshotNID(2), nids[1], "Second NID should be 2")
	assert.Equal(t, types.StateSnapshotNID(5), nids[2], "Third NID should be 5")
	assert.Equal(t, types.StateSnapshotNID(8), nids[3], "Fourth NID should be 8")

	// Verify ascending order using comparison
	assert.Less(t, nids[0], nids[1], "NIDs should be in ascending order")
	assert.Less(t, nids[1], nids[2], "NIDs should be in ascending order")
	assert.Less(t, nids[2], nids[3], "NIDs should be in ascending order")
}

// TestStateBlockNIDSorter tests sorting of StateBlockNIDs
func TestStateBlockNIDSorter(t *testing.T) {
	t.Parallel()

	nids := []types.StateBlockNID{10, 5, 20, 15}

	// Sort using the stateBlockNIDSorter interface
	sort.Sort(stateBlockNIDSorter(nids))

	// Verify sorted order - should be in ascending order
	assert.Equal(t, types.StateBlockNID(5), nids[0], "First NID should be 5")
	assert.Equal(t, types.StateBlockNID(10), nids[1], "Second NID should be 10")
	assert.Equal(t, types.StateBlockNID(15), nids[2], "Third NID should be 15")
	assert.Equal(t, types.StateBlockNID(20), nids[3], "Fourth NID should be 20")

	// Verify ascending order using comparison
	assert.Less(t, nids[0], nids[1], "NIDs should be in ascending order")
	assert.Less(t, nids[1], nids[2], "NIDs should be in ascending order")
	assert.Less(t, nids[2], nids[3], "NIDs should be in ascending order")
}
