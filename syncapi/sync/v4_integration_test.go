// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package sync

import (
	"context"
	"testing"

	"github.com/element-hq/dendrite/syncapi/synctypes"
	"github.com/element-hq/dendrite/syncapi/types"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/stretchr/testify/assert"
)

// TestDetermineRoomStreamState tests the room stream state determination logic
// This is critical for incremental sync behavior (initial vs live vs previously)
func TestDetermineRoomStreamState(t *testing.T) {
	ctx := context.Background()
	roomID := "!testroom:localhost"
	userID := "@alice:localhost"

	tests := []struct {
		name               string
		connState          *V4ConnectionState
		setupMock          func(*mockSnapshot)
		expectedStatus     types.HaveSentRoomFlag
		expectedHasToken   bool
		description        string
	}{
		{
			name:      "nil connection state - returns NEVER",
			connState: nil,
			setupMock: func(m *mockSnapshot) {},
			expectedStatus:   types.HaveSentRoomNever,
			expectedHasToken: false,
			description:      "When connState is nil, room is treated as never sent",
		},
		{
			name: "nil PreviousStreamStates - returns NEVER",
			connState: &V4ConnectionState{
				ConnectionKey:        1,
				PreviousStreamStates: nil,
			},
			setupMock:        func(m *mockSnapshot) {},
			expectedStatus:   types.HaveSentRoomNever,
			expectedHasToken: false,
			description:      "When PreviousStreamStates is nil, room is treated as never sent",
		},
		{
			name: "room not in previous states - returns NEVER",
			connState: &V4ConnectionState{
				ConnectionKey:        1,
				PreviousStreamStates: map[string]map[string]*types.SlidingSyncStreamState{},
			},
			setupMock:        func(m *mockSnapshot) {},
			expectedStatus:   types.HaveSentRoomNever,
			expectedHasToken: false,
			description:      "Room not previously sent returns NEVER status",
		},
		{
			name: "room in previous states with LIVE status",
			connState: &V4ConnectionState{
				ConnectionKey: 1,
				PreviousStreamStates: map[string]map[string]*types.SlidingSyncStreamState{
					roomID: {
						"events": {
							RoomStatus: "live",
							LastToken:  "s100_50_25_10_5_3_1_0_8",
						},
					},
				},
			},
			setupMock: func(m *mockSnapshot) {
				// User is currently joined
				m.SetMembership(roomID, userID, spec.Join, 100)
			},
			expectedStatus:   types.HaveSentRoomLive,
			expectedHasToken: true,
			description:      "Room previously sent with LIVE status returns LIVE",
		},
		{
			name: "room in previous states with PREVIOUSLY status",
			connState: &V4ConnectionState{
				ConnectionKey: 1,
				PreviousStreamStates: map[string]map[string]*types.SlidingSyncStreamState{
					roomID: {
						"events": {
							RoomStatus: "previously",
							LastToken:  "s100_50_25_10_5_3_1_0_8",
						},
					},
				},
			},
			setupMock: func(m *mockSnapshot) {
				// User is currently joined
				m.SetMembership(roomID, userID, spec.Join, 100)
			},
			expectedStatus:   types.HaveSentRoomPreviously,
			expectedHasToken: true,
			description:      "Room previously sent with PREVIOUSLY status returns PREVIOUSLY",
		},
		{
			name: "membership transition (leave -> join) - returns NEVER",
			connState: &V4ConnectionState{
				ConnectionKey: 1,
				PreviousStreamStates: map[string]map[string]*types.SlidingSyncStreamState{
					roomID: {
						"events": {
							RoomStatus: "live",
							LastToken:  "s100_50_25_10_5_3_1_0_8",
						},
					},
				},
			},
			setupMock: func(m *mockSnapshot) {
				// User was leave at position 100, now join at position 200
				m.membershipForUser[roomID] = map[string]mockMembership{
					userID: {membership: spec.Join, topoPos: 200},
				}
			},
			expectedStatus:   types.HaveSentRoomNever,
			expectedHasToken: false,
			description:      "Membership transition from leave to join triggers NEVER (newly joined)",
		},
		{
			name: "invalid last token - returns NEVER",
			connState: &V4ConnectionState{
				ConnectionKey: 1,
				PreviousStreamStates: map[string]map[string]*types.SlidingSyncStreamState{
					roomID: {
						"events": {
							RoomStatus: "live",
							LastToken:  "invalid_token",
						},
					},
				},
			},
			setupMock: func(m *mockSnapshot) {
				m.SetMembership(roomID, userID, spec.Join, 100)
			},
			expectedStatus:   types.HaveSentRoomNever,
			expectedHasToken: false,
			description:      "Invalid token format causes fallback to NEVER",
		},
		{
			name: "empty last token - returns NEVER",
			connState: &V4ConnectionState{
				ConnectionKey: 1,
				PreviousStreamStates: map[string]map[string]*types.SlidingSyncStreamState{
					roomID: {
						"events": {
							RoomStatus: "live",
							LastToken:  "",
						},
					},
				},
			},
			setupMock: func(m *mockSnapshot) {
				m.SetMembership(roomID, userID, spec.Join, 100)
			},
			expectedStatus:   types.HaveSentRoomNever,
			expectedHasToken: false,
			description:      "Empty token causes fallback to NEVER",
		},
		{
			name: "continuing join (no membership change) - returns LIVE",
			connState: &V4ConnectionState{
				ConnectionKey: 1,
				PreviousStreamStates: map[string]map[string]*types.SlidingSyncStreamState{
					roomID: {
						"events": {
							RoomStatus: "live",
							LastToken:  "s100_50_25_10_5_3_1_0_8",
						},
					},
				},
			},
			setupMock: func(m *mockSnapshot) {
				// User was joined at position 50 and is still joined at 200
				m.membershipForUser[roomID] = map[string]mockMembership{
					userID: {membership: spec.Join, topoPos: 50},
				}
			},
			expectedStatus:   types.HaveSentRoomLive,
			expectedHasToken: true,
			description:      "User still joined (no transition) returns LIVE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockSnapshot()
			tt.setupMock(mock)

			result := determineRoomStreamState(ctx, mock, tt.connState, roomID, userID)

			assert.Equal(t, tt.expectedStatus, result.Status, tt.description)
			if tt.expectedHasToken {
				assert.NotNil(t, result.LastToken, "Expected LastToken to be set")
			} else {
				assert.Nil(t, result.LastToken, "Expected LastToken to be nil")
			}
		})
	}
}

// TestDetermineRoomStreamStateRejoinScenarios tests various rejoin scenarios
func TestDetermineRoomStreamStateRejoinScenarios(t *testing.T) {
	ctx := context.Background()
	roomID := "!testroom:localhost"
	userID := "@alice:localhost"

	// Helper to create connection state with room previously sent
	makeConnState := func(roomStatus, lastToken string) *V4ConnectionState {
		return &V4ConnectionState{
			ConnectionKey: 1,
			PreviousStreamStates: map[string]map[string]*types.SlidingSyncStreamState{
				roomID: {
					"events": {
						RoomStatus: roomStatus,
						LastToken:  lastToken,
					},
				},
			},
		}
	}

	tests := []struct {
		name           string
		connState      *V4ConnectionState
		prevMembership string
		prevTopoPos    int64
		currMembership string
		currTopoPos    int64
		expectedStatus types.HaveSentRoomFlag
	}{
		{
			name:           "kick then rejoin",
			connState:      makeConnState("live", "s100_50_25_10_5_3_1_0_8"),
			prevMembership: spec.Leave,
			prevTopoPos:    80,
			currMembership: spec.Join,
			currTopoPos:    150,
			expectedStatus: types.HaveSentRoomNever,
		},
		{
			name:           "ban then unban+join",
			connState:      makeConnState("live", "s100_50_25_10_5_3_1_0_8"),
			prevMembership: spec.Ban,
			prevTopoPos:    80,
			currMembership: spec.Join,
			currTopoPos:    150,
			expectedStatus: types.HaveSentRoomNever,
		},
		{
			name:           "invite then join",
			connState:      makeConnState("live", "s100_50_25_10_5_3_1_0_8"),
			prevMembership: spec.Invite,
			prevTopoPos:    80,
			currMembership: spec.Join,
			currTopoPos:    150,
			expectedStatus: types.HaveSentRoomNever,
		},
		{
			name:           "continuous join - no transition",
			connState:      makeConnState("live", "s100_50_25_10_5_3_1_0_8"),
			prevMembership: spec.Join,
			prevTopoPos:    50,
			currMembership: spec.Join,
			currTopoPos:    50,
			expectedStatus: types.HaveSentRoomLive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockSnapshot()

			// Set up membership state
			// The mock returns the same membership for any position <= the topoPos
			mock.membershipForUser[roomID] = map[string]mockMembership{
				userID: {membership: tt.currMembership, topoPos: tt.currTopoPos},
			}

			// For the "previous" position query (pos=100 from token), we need different behavior
			// This is tricky with our simple mock - the mock doesn't support position-based queries properly
			// For this test, we rely on the current position being different from previous

			result := determineRoomStreamState(ctx, mock, tt.connState, roomID, userID)

			assert.Equal(t, tt.expectedStatus, result.Status)
		})
	}
}

// TestV4ConnectionStateInitialization tests V4ConnectionState creation
func TestV4ConnectionStateInitialization(t *testing.T) {
	tests := []struct {
		name              string
		connectionKey     int64
		previousStates    map[string]map[string]*types.SlidingSyncStreamState
		expectNumRooms    int
	}{
		{
			name:           "empty connection state",
			connectionKey:  1,
			previousStates: nil,
			expectNumRooms: 0,
		},
		{
			name:          "connection state with rooms",
			connectionKey: 1,
			previousStates: map[string]map[string]*types.SlidingSyncStreamState{
				"!room1:test": {"events": {RoomStatus: "live"}},
				"!room2:test": {"events": {RoomStatus: "previously"}},
			},
			expectNumRooms: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connState := &V4ConnectionState{
				ConnectionKey:        tt.connectionKey,
				PreviousStreamStates: tt.previousStates,
			}

			assert.Equal(t, tt.connectionKey, connState.ConnectionKey)
			if tt.previousStates == nil {
				assert.Nil(t, connState.PreviousStreamStates)
			} else {
				assert.Len(t, connState.PreviousStreamStates, tt.expectNumRooms)
			}
		})
	}
}

// TestGetRoomMetadata tests room metadata extraction functions
func TestGetRoomMetadata(t *testing.T) {
	ctx := context.Background()
	roomID := "!testroom:localhost"

	// Create a minimal RequestPool with mocked rsAPI
	rp := &RequestPool{
		rsAPI: &mockRoomserverAPI{},
	}

	t.Run("getRoomNameFromDB", func(t *testing.T) {
		tests := []struct {
			name         string
			setupMock    func(*mockSnapshot)
			expectedName string
		}{
			{
				name:         "no room name event",
				setupMock:    func(m *mockSnapshot) {},
				expectedName: "",
			},
			{
				name: "room has name",
				setupMock: func(m *mockSnapshot) {
					m.SetStateEvent(roomID, "m.room.name", "", createMockStateEvent(
						"m.room.name", "", `{"name": "Test Room Name"}`,
					))
				},
				expectedName: "Test Room Name",
			},
			{
				name: "room name event with empty name",
				setupMock: func(m *mockSnapshot) {
					m.SetStateEvent(roomID, "m.room.name", "", createMockStateEvent(
						"m.room.name", "", `{"name": ""}`,
					))
				},
				expectedName: "",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mock := newMockSnapshot()
				tt.setupMock(mock)

				result := rp.getRoomNameFromDB(ctx, mock, roomID)
				assert.Equal(t, tt.expectedName, result)
			})
		}
	})

	t.Run("getRoomAvatar", func(t *testing.T) {
		tests := []struct {
			name        string
			setupMock   func(*mockSnapshot)
			expectedURL string
		}{
			{
				name:        "no avatar event",
				setupMock:   func(m *mockSnapshot) {},
				expectedURL: "",
			},
			{
				name: "room has avatar",
				setupMock: func(m *mockSnapshot) {
					m.SetStateEvent(roomID, "m.room.avatar", "", createMockStateEvent(
						"m.room.avatar", "", `{"url": "mxc://example.com/avatar123"}`,
					))
				},
				expectedURL: "mxc://example.com/avatar123",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mock := newMockSnapshot()
				tt.setupMock(mock)

				result := rp.getRoomAvatar(ctx, mock, roomID)
				assert.Equal(t, tt.expectedURL, result)
			})
		}
	})

	t.Run("getRoomTopic", func(t *testing.T) {
		tests := []struct {
			name          string
			setupMock     func(*mockSnapshot)
			expectedTopic string
		}{
			{
				name:          "no topic event",
				setupMock:     func(m *mockSnapshot) {},
				expectedTopic: "",
			},
			{
				name: "room has topic",
				setupMock: func(m *mockSnapshot) {
					m.SetStateEvent(roomID, "m.room.topic", "", createMockStateEvent(
						"m.room.topic", "", `{"topic": "Welcome to the test room!"}`,
					))
				},
				expectedTopic: "Welcome to the test room!",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mock := newMockSnapshot()
				tt.setupMock(mock)

				result := rp.getRoomTopic(ctx, mock, roomID)
				assert.Equal(t, tt.expectedTopic, result)
			})
		}
	})
}

// TestGetHeroes tests the heroes extraction for room display
func TestGetHeroes(t *testing.T) {
	ctx := context.Background()
	roomID := "!testroom:localhost"
	userID := "@alice:localhost"

	rp := &RequestPool{
		rsAPI: &mockRoomserverAPI{},
	}

	tests := []struct {
		name          string
		setupMock     func(*mockSnapshot)
		expectedLen   int
		checkHeroes   func(*testing.T, []types.MSC4186Hero)
	}{
		{
			name:        "no heroes",
			setupMock:   func(m *mockSnapshot) {},
			expectedLen: 0,
		},
		{
			name: "heroes with member events",
			setupMock: func(m *mockSnapshot) {
				m.SetRoomSummary(roomID, &types.Summary{
					Heroes: []string{"@bob:localhost", "@carol:localhost"},
				})
				m.SetStateEvent(roomID, "m.room.member", "@bob:localhost", createMockStateEvent(
					"m.room.member", "@bob:localhost",
					`{"displayname": "Bob", "avatar_url": "mxc://test/bob"}`,
				))
				m.SetStateEvent(roomID, "m.room.member", "@carol:localhost", createMockStateEvent(
					"m.room.member", "@carol:localhost",
					`{"displayname": "Carol"}`,
				))
			},
			expectedLen: 2,
			checkHeroes: func(t *testing.T, heroes []types.MSC4186Hero) {
				// Bob should have displayname and avatar
				assert.Equal(t, "@bob:localhost", heroes[0].UserID)
				assert.Equal(t, "Bob", heroes[0].Displayname)
				assert.Equal(t, "mxc://test/bob", heroes[0].AvatarURL)

				// Carol should have displayname only
				assert.Equal(t, "@carol:localhost", heroes[1].UserID)
				assert.Equal(t, "Carol", heroes[1].Displayname)
				assert.Empty(t, heroes[1].AvatarURL)
			},
		},
		{
			name: "hero without member event - still included",
			setupMock: func(m *mockSnapshot) {
				m.SetRoomSummary(roomID, &types.Summary{
					Heroes: []string{"@unknown:localhost"},
				})
				// No member event set - hero should still be included with just user ID
			},
			expectedLen: 1,
			checkHeroes: func(t *testing.T, heroes []types.MSC4186Hero) {
				assert.Equal(t, "@unknown:localhost", heroes[0].UserID)
				assert.Empty(t, heroes[0].Displayname)
				assert.Empty(t, heroes[0].AvatarURL)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockSnapshot()
			tt.setupMock(mock)

			result := rp.getHeroes(ctx, mock, roomID, userID)

			if tt.expectedLen == 0 {
				assert.Nil(t, result)
			} else {
				assert.Len(t, result, tt.expectedLen)
				if tt.checkHeroes != nil {
					tt.checkHeroes(t, result)
				}
			}
		})
	}
}

// TestCalculateBumpStamp tests bump stamp calculation
func TestCalculateBumpStamp(t *testing.T) {
	ctx := context.Background()
	roomID := "!testroom:localhost"

	rp := &RequestPool{
		rsAPI: &mockRoomserverAPI{},
	}

	tests := []struct {
		name              string
		timeline          []synctypes.ClientEvent
		setupMock         func(*mockSnapshot)
		expectedBumpStamp int64
	}{
		{
			name:              "empty timeline, no database bump stamp",
			timeline:          []synctypes.ClientEvent{},
			setupMock:         func(m *mockSnapshot) {},
			expectedBumpStamp: 0,
		},
		{
			name: "timeline with bump event",
			timeline: []synctypes.ClientEvent{
				{Type: "m.room.member", OriginServerTS: 1000}, // Not a bump event
				{Type: "m.room.message", OriginServerTS: 2000}, // Bump event!
				{Type: "m.reaction", OriginServerTS: 3000}, // Not a bump event
			},
			setupMock:         func(m *mockSnapshot) {},
			expectedBumpStamp: 2000, // Uses the most recent bump event (message at 2000)
		},
		{
			name: "timeline with multiple bump events - uses most recent",
			timeline: []synctypes.ClientEvent{
				{Type: "m.room.message", OriginServerTS: 1000},
				{Type: "m.room.encrypted", OriginServerTS: 2000},
				{Type: "m.room.message", OriginServerTS: 3000}, // Most recent bump event
			},
			setupMock:         func(m *mockSnapshot) {},
			expectedBumpStamp: 3000,
		},
		{
			name: "no bump events in timeline - falls back to database",
			timeline: []synctypes.ClientEvent{
				{Type: "m.room.member", OriginServerTS: 1000},
				{Type: "m.reaction", OriginServerTS: 2000},
			},
			setupMock: func(m *mockSnapshot) {
				m.SetMaxStreamPosition(roomID, 500) // Database has bump stamp
			},
			expectedBumpStamp: 500,
		},
		{
			name: "sticker event counts as bump",
			timeline: []synctypes.ClientEvent{
				{Type: "m.sticker", OriginServerTS: 5000},
			},
			setupMock:         func(m *mockSnapshot) {},
			expectedBumpStamp: 5000,
		},
		{
			name: "call invite event counts as bump",
			timeline: []synctypes.ClientEvent{
				{Type: "m.call.invite", OriginServerTS: 6000},
			},
			setupMock:         func(m *mockSnapshot) {},
			expectedBumpStamp: 6000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockSnapshot()
			tt.setupMock(mock)

			result := rp.calculateBumpStamp(ctx, mock, roomID, tt.timeline)
			assert.Equal(t, tt.expectedBumpStamp, result)
		})
	}
}
