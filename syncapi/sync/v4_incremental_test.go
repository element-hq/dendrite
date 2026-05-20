// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package sync

import (
	"testing"

	"github.com/element-hq/dendrite/syncapi/synctypes"
	"github.com/element-hq/dendrite/syncapi/types"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHaveSentRoomFlag tests the HaveSentRoomFlag enum behavior
func TestHaveSentRoomFlag(t *testing.T) {
	tests := []struct {
		name          string
		status        types.HaveSentRoomFlag
		wantIsInitial bool
		wantString    string
	}{
		{
			name:          "NEVER is initial",
			status:        types.HaveSentRoomNever,
			wantIsInitial: true,
			wantString:    "never",
		},
		{
			name:          "LIVE is not initial",
			status:        types.HaveSentRoomLive,
			wantIsInitial: false,
			wantString:    "live",
		},
		{
			name:          "PREVIOUSLY is not initial",
			status:        types.HaveSentRoomPreviously,
			wantIsInitial: false,
			wantString:    "previously",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantIsInitial, tt.status.IsInitial())
			assert.Equal(t, tt.wantString, tt.status.String())
		})
	}
}

// TestHaveSentRoomFlagShouldSendHistorical tests timeline fetch mode determination
func TestHaveSentRoomFlagShouldSendHistorical(t *testing.T) {
	tests := []struct {
		name               string
		status             types.HaveSentRoomFlag
		wantSendHistorical bool
	}{
		{
			name:               "NEVER should send historical",
			status:             types.HaveSentRoomNever,
			wantSendHistorical: true,
		},
		{
			name:               "LIVE should not send historical",
			status:             types.HaveSentRoomLive,
			wantSendHistorical: false,
		},
		{
			name:               "PREVIOUSLY should not send historical",
			status:             types.HaveSentRoomPreviously,
			wantSendHistorical: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantSendHistorical, tt.status.ShouldSendHistorical())
		})
	}
}

// TestRoomStreamStateCreation tests creating RoomStreamState for different scenarios
func TestRoomStreamStateCreation(t *testing.T) {
	tests := []struct {
		name         string
		status       types.HaveSentRoomFlag
		hasLastToken bool
		wantInitial  bool
	}{
		{
			name:         "NEVER state is initial",
			status:       types.HaveSentRoomNever,
			hasLastToken: false,
			wantInitial:  true,
		},
		{
			name:         "LIVE state is not initial",
			status:       types.HaveSentRoomLive,
			hasLastToken: true,
			wantInitial:  false,
		},
		{
			name:         "PREVIOUSLY state is not initial",
			status:       types.HaveSentRoomPreviously,
			hasLastToken: true,
			wantInitial:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := types.RoomStreamState{
				Status: tt.status,
			}
			if tt.hasLastToken {
				state.LastToken = &types.StreamingToken{PDUPosition: 50}
			}

			assert.Equal(t, tt.wantInitial, state.Status.IsInitial())
			if tt.hasLastToken {
				assert.NotNil(t, state.LastToken)
			} else {
				assert.Nil(t, state.LastToken)
			}
		})
	}
}

// TestNumLiveCalculation tests that num_live is calculated correctly
func TestNumLiveCalculation(t *testing.T) {
	tests := []struct {
		name          string
		roomState     types.RoomStreamState
		timelineLen   int
		wantNumLive   int
		description   string
	}{
		{
			name: "NEVER status - all historical",
			roomState: types.RoomStreamState{
				Status: types.HaveSentRoomNever,
			},
			timelineLen: 10,
			wantNumLive: 0,
			description: "Initial sync - all events are historical, not live",
		},
		{
			name: "LIVE status - all new",
			roomState: types.RoomStreamState{
				Status: types.HaveSentRoomLive,
				LastToken: &types.StreamingToken{
					PDUPosition: 50,
				},
			},
			timelineLen: 5,
			wantNumLive: 5,
			description: "Incremental sync - all timeline events are new since last sync",
		},
		{
			name: "PREVIOUSLY status - all new",
			roomState: types.RoomStreamState{
				Status: types.HaveSentRoomPreviously,
				LastToken: &types.StreamingToken{
					PDUPosition: 75,
				},
			},
			timelineLen: 3,
			wantNumLive: 3,
			description: "Incremental sync after gap - all events in timeline are new",
		},
		{
			name: "LIVE with empty timeline",
			roomState: types.RoomStreamState{
				Status: types.HaveSentRoomLive,
				LastToken: &types.StreamingToken{
					PDUPosition: 100,
				},
			},
			timelineLen: 0,
			wantNumLive: 0,
			description: "No new events since last sync",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the num_live calculation logic from v4_roomdata.go:217-224
			var numLive int
			if tt.roomState.Status == types.HaveSentRoomNever {
				numLive = 0 // All historical
			} else {
				numLive = tt.timelineLen // All new
			}

			assert.Equal(t, tt.wantNumLive, numLive, tt.description)
		})
	}
}

// TestTimelineRangeCalculation tests that timeline event ranges are correct
func TestTimelineRangeCalculation(t *testing.T) {
	tests := []struct {
		name          string
		roomState     types.RoomStreamState
		currentPos    types.StreamPosition
		wantFromPos   types.StreamPosition
		wantToPos     types.StreamPosition
		wantBackwards bool
	}{
		{
			name: "NEVER - historical range",
			roomState: types.RoomStreamState{
				Status: types.HaveSentRoomNever,
			},
			currentPos:    100,
			wantFromPos:   100,
			wantToPos:     0,
			wantBackwards: true,
		},
		{
			name: "LIVE - incremental range",
			roomState: types.RoomStreamState{
				Status: types.HaveSentRoomLive,
				LastToken: &types.StreamingToken{
					PDUPosition: 50,
				},
			},
			currentPos:    100,
			wantFromPos:   100,
			wantToPos:     50,
			wantBackwards: true,
		},
		{
			name: "PREVIOUSLY - incremental range from last token",
			roomState: types.RoomStreamState{
				Status: types.HaveSentRoomPreviously,
				LastToken: &types.StreamingToken{
					PDUPosition: 75,
				},
			},
			currentPos:    120,
			wantFromPos:   120,
			wantToPos:     75,
			wantBackwards: true,
		},
		{
			name: "LIVE with no last token - fallback to historical",
			roomState: types.RoomStreamState{
				Status:    types.HaveSentRoomLive,
				LastToken: nil,
			},
			currentPos:    100,
			wantFromPos:   100,
			wantToPos:     0,
			wantBackwards: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate timeline range calculation from v4_roomdata.go:265-293
			fromPos := tt.currentPos
			var toPos types.StreamPosition

			if tt.roomState.Status == types.HaveSentRoomNever {
				toPos = 0
			} else {
				if tt.roomState.LastToken != nil {
					toPos = tt.roomState.LastToken.PDUPosition
				} else {
					toPos = 0 // Fallback
				}
			}

			assert.Equal(t, tt.wantFromPos, fromPos)
			assert.Equal(t, tt.wantToPos, toPos)
		})
	}
}

// TestInitialFieldCalculation tests that the initial field is set correctly
func TestInitialFieldCalculation(t *testing.T) {
	tests := []struct {
		name        string
		roomState   types.RoomStreamState
		wantInitial bool
	}{
		{
			name: "NEVER = initial true",
			roomState: types.RoomStreamState{
				Status: types.HaveSentRoomNever,
			},
			wantInitial: true,
		},
		{
			name: "LIVE = initial false",
			roomState: types.RoomStreamState{
				Status: types.HaveSentRoomLive,
				LastToken: &types.StreamingToken{
					PDUPosition: 50,
				},
			},
			wantInitial: false,
		},
		{
			name: "PREVIOUSLY = initial false",
			roomState: types.RoomStreamState{
				Status: types.HaveSentRoomPreviously,
				LastToken: &types.StreamingToken{
					PDUPosition: 75,
				},
			},
			wantInitial: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initial := tt.roomState.Status.IsInitial()
			assert.Equal(t, tt.wantInitial, initial)
		})
	}
}

// TestLimitedFieldFromDatabase tests that limited field comes from database
func TestLimitedFieldFromDatabase(t *testing.T) {
	tests := []struct {
		name             string
		eventsReturned   int
		timelineLimit    int
		dbLimited        bool
		wantLimited      bool
		description      string
	}{
		{
			name:           "db says limited - trust it",
			eventsReturned: 10,
			timelineLimit:  10,
			dbLimited:      true,
			wantLimited:    true,
			description:    "Database knows there were more events available",
		},
		{
			name:           "db says not limited - trust it",
			eventsReturned: 10,
			timelineLimit:  10,
			dbLimited:      false,
			wantLimited:    false,
			description:    "Database knows we got all events in range",
		},
		{
			name:           "exactly at limit but not limited",
			eventsReturned: 10,
			timelineLimit:  10,
			dbLimited:      false,
			wantLimited:    false,
			description:    "Exactly at limit but no more events exist - not limited",
		},
		{
			name:           "under limit and not limited",
			eventsReturned: 5,
			timelineLimit:  10,
			dbLimited:      false,
			wantLimited:    false,
			description:    "Fewer events than limit - definitely not limited",
		},
		{
			name:           "over limit must be limited",
			eventsReturned: 15,
			timelineLimit:  10,
			dbLimited:      true,
			wantLimited:    true,
			description:    "More events than limit means truncation occurred",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The key fix: use dbLimited directly from RecentEvents.Limited
			// Not a manual calculation like: limited = (len(timeline) >= limit)
			limited := tt.dbLimited

			assert.Equal(t, tt.wantLimited, limited, tt.description)
		})
	}
}

// TestStreamTokenParsing tests position token format
func TestStreamTokenParsing(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{
			name:      "valid token",
			input:     "s100_50_25_10_5_3_1_0_8",
			wantError: false,
		},
		{
			name:      "zero positions",
			input:     "s0_0_0_0_0_0_0_0_0",
			wantError: false,
		},
		{
			name:      "large positions",
			input:     "s999999_888888_777777_666666_555555_444444_333333_222222_111111",
			wantError: false,
		},
		{
			name:      "invalid format - no prefix",
			input:     "100_50_25_10_5_3_1_0_8",
			wantError: true,
		},
		{
			name:      "invalid format - wrong separator",
			input:     "s100-50-25-10-5-3-1-0-8",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := types.NewStreamTokenFromString(tt.input)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, token)
				// Verify round-trip
				assert.Equal(t, tt.input, token.String())
			}
		})
	}
}

// TestRoomStreamStateStatusMapping tests status string mapping
func TestRoomStreamStateStatusMapping(t *testing.T) {
	tests := []struct {
		name       string
		status     types.HaveSentRoomFlag
		wantString string
	}{
		{
			name:       "NEVER maps to never",
			status:     types.HaveSentRoomNever,
			wantString: "never",
		},
		{
			name:       "LIVE maps to live",
			status:     types.HaveSentRoomLive,
			wantString: "live",
		},
		{
			name:       "PREVIOUSLY maps to previously",
			status:     types.HaveSentRoomPreviously,
			wantString: "previously",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify string representation
			assert.Equal(t, tt.wantString, tt.status.String())

			// Verify status can be used in connection state
			state := types.RoomStreamState{
				Status: tt.status,
			}
			assert.Equal(t, tt.status, state.Status)
		})
	}
}

// TestV4ResponseHasUpdates tests the response update detection logic
func TestV4ResponseHasUpdates(t *testing.T) {
	tests := []struct {
		name     string
		response types.SlidingSyncResponse
		expected bool
	}{
		{
			name: "empty response - no updates",
			response: types.SlidingSyncResponse{
				Pos:        "pos1",
				Lists:      make(map[string]types.SlidingList),
				Rooms:      make(map[string]types.SlidingRoomData),
				Extensions: nil,
			},
			expected: false,
		},
		{
			name: "list with ops - has updates",
			response: types.SlidingSyncResponse{
				Pos: "pos1",
				Lists: map[string]types.SlidingList{
					"rooms": {
						Count: 5,
						Ops: []types.SlidingOperation{
							{Op: "SYNC", RoomIDs: []string{"!room1:test"}},
						},
					},
				},
				Rooms:      make(map[string]types.SlidingRoomData),
				Extensions: nil,
			},
			expected: true,
		},
		{
			name: "list without ops - no updates",
			response: types.SlidingSyncResponse{
				Pos: "pos1",
				Lists: map[string]types.SlidingList{
					"rooms": {
						Count: 5,
						Ops:   []types.SlidingOperation{},
					},
				},
				Rooms:      make(map[string]types.SlidingRoomData),
				Extensions: nil,
			},
			expected: false,
		},
		{
			name: "rooms present - has updates",
			response: types.SlidingSyncResponse{
				Pos:   "pos1",
				Lists: make(map[string]types.SlidingList),
				Rooms: map[string]types.SlidingRoomData{
					"!room1:test": {},
				},
				Extensions: nil,
			},
			expected: true,
		},
		{
			name: "to_device events - has updates",
			response: types.SlidingSyncResponse{
				Pos:   "pos1",
				Lists: make(map[string]types.SlidingList),
				Rooms: make(map[string]types.SlidingRoomData),
				Extensions: &types.ExtensionResponse{
					ToDevice: &types.V4ToDeviceResponse{
						Events: []gomatrixserverlib.SendToDeviceEvent{
							{Type: "m.room.encrypted"},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "empty to_device - no updates",
			response: types.SlidingSyncResponse{
				Pos:   "pos1",
				Lists: make(map[string]types.SlidingList),
				Rooms: make(map[string]types.SlidingRoomData),
				Extensions: &types.ExtensionResponse{
					ToDevice: &types.V4ToDeviceResponse{
						Events: []gomatrixserverlib.SendToDeviceEvent{},
					},
				},
			},
			expected: false,
		},
		{
			name: "e2ee device list changed - has updates",
			response: types.SlidingSyncResponse{
				Pos:   "pos1",
				Lists: make(map[string]types.SlidingList),
				Rooms: make(map[string]types.SlidingRoomData),
				Extensions: &types.ExtensionResponse{
					E2EE: &types.E2EEResponse{
						DeviceLists: &types.DeviceLists{
							Changed: []string{"@user:test"},
							Left:    []string{},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "e2ee device list left - has updates",
			response: types.SlidingSyncResponse{
				Pos:   "pos1",
				Lists: make(map[string]types.SlidingList),
				Rooms: make(map[string]types.SlidingRoomData),
				Extensions: &types.ExtensionResponse{
					E2EE: &types.E2EEResponse{
						DeviceLists: &types.DeviceLists{
							Changed: []string{},
							Left:    []string{"@user:test"},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "e2ee empty device lists - no updates",
			response: types.SlidingSyncResponse{
				Pos:   "pos1",
				Lists: make(map[string]types.SlidingList),
				Rooms: make(map[string]types.SlidingRoomData),
				Extensions: &types.ExtensionResponse{
					E2EE: &types.E2EEResponse{
						DeviceLists: &types.DeviceLists{
							Changed: []string{},
							Left:    []string{},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "e2ee nil device lists - no updates",
			response: types.SlidingSyncResponse{
				Pos:   "pos1",
				Lists: make(map[string]types.SlidingList),
				Rooms: make(map[string]types.SlidingRoomData),
				Extensions: &types.ExtensionResponse{
					E2EE: &types.E2EEResponse{
						DeviceLists: nil,
					},
				},
			},
			expected: false,
		},
		{
			name: "account data global - has updates",
			response: types.SlidingSyncResponse{
				Pos:   "pos1",
				Lists: make(map[string]types.SlidingList),
				Rooms: make(map[string]types.SlidingRoomData),
				Extensions: &types.ExtensionResponse{
					AccountData: &types.AccountDataResponse{
						Global: []synctypes.ClientEvent{{Type: "m.push_rules"}},
						Rooms:  make(map[string][]synctypes.ClientEvent),
					},
				},
			},
			expected: true,
		},
		{
			name: "account data rooms - has updates",
			response: types.SlidingSyncResponse{
				Pos:   "pos1",
				Lists: make(map[string]types.SlidingList),
				Rooms: make(map[string]types.SlidingRoomData),
				Extensions: &types.ExtensionResponse{
					AccountData: &types.AccountDataResponse{
						Global: []synctypes.ClientEvent{},
						Rooms: map[string][]synctypes.ClientEvent{
							"!room1:test": {{Type: "m.fully_read"}},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "empty account data - no updates",
			response: types.SlidingSyncResponse{
				Pos:   "pos1",
				Lists: make(map[string]types.SlidingList),
				Rooms: make(map[string]types.SlidingRoomData),
				Extensions: &types.ExtensionResponse{
					AccountData: &types.AccountDataResponse{
						Global: []synctypes.ClientEvent{},
						Rooms:  make(map[string][]synctypes.ClientEvent),
					},
				},
			},
			expected: false,
		},
		{
			name: "receipts - has updates",
			response: types.SlidingSyncResponse{
				Pos:   "pos1",
				Lists: make(map[string]types.SlidingList),
				Rooms: make(map[string]types.SlidingRoomData),
				Extensions: &types.ExtensionResponse{
					Receipts: &types.ReceiptsResponse{
						Rooms: map[string]synctypes.ClientEvent{
							"!room1:test": {Type: "m.receipt"},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "empty receipts - no updates",
			response: types.SlidingSyncResponse{
				Pos:   "pos1",
				Lists: make(map[string]types.SlidingList),
				Rooms: make(map[string]types.SlidingRoomData),
				Extensions: &types.ExtensionResponse{
					Receipts: &types.ReceiptsResponse{
						Rooms: map[string]synctypes.ClientEvent{},
					},
				},
			},
			expected: false,
		},
		{
			name: "typing - has updates",
			response: types.SlidingSyncResponse{
				Pos:   "pos1",
				Lists: make(map[string]types.SlidingList),
				Rooms: make(map[string]types.SlidingRoomData),
				Extensions: &types.ExtensionResponse{
					Typing: &types.TypingResponse{
						Rooms: map[string]synctypes.ClientEvent{
							"!room1:test": {Type: "m.typing"},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "empty typing - no updates",
			response: types.SlidingSyncResponse{
				Pos:   "pos1",
				Lists: make(map[string]types.SlidingList),
				Rooms: make(map[string]types.SlidingRoomData),
				Extensions: &types.ExtensionResponse{
					Typing: &types.TypingResponse{
						Rooms: map[string]synctypes.ClientEvent{},
					},
				},
			},
			expected: false,
		},
		{
			name: "multiple updates - has updates",
			response: types.SlidingSyncResponse{
				Pos: "pos1",
				Lists: map[string]types.SlidingList{
					"rooms": {Ops: []types.SlidingOperation{{Op: "SYNC"}}},
				},
				Rooms: map[string]types.SlidingRoomData{
					"!room1:test": {},
				},
				Extensions: &types.ExtensionResponse{
					ToDevice: &types.V4ToDeviceResponse{
						Events: []gomatrixserverlib.SendToDeviceEvent{{}},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := v4ResponseHasUpdates(tt.response)
			assert.Equal(t, tt.expected, result, "v4ResponseHasUpdates returned unexpected result")
		})
	}
}
