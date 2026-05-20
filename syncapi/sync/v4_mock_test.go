// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package sync

import (
	"context"

	roomserverAPI "github.com/element-hq/dendrite/roomserver/api"
	rstypes "github.com/element-hq/dendrite/roomserver/types"
	"github.com/element-hq/dendrite/syncapi/storage"
	"github.com/element-hq/dendrite/syncapi/synctypes"
	"github.com/element-hq/dendrite/syncapi/types"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/spec"
)

// mockSnapshot implements storage.DatabaseTransaction for testing
// Uses interface embedding - only override methods needed for tests
type mockSnapshot struct {
	storage.DatabaseTransaction

	// Configurable return values
	membershipForUser       map[string]map[string]mockMembership // roomID -> userID -> membership
	stateEvents             map[string]map[string]*rstypes.HeaderedEvent // roomID -> "type|stateKey" -> event
	recentEvents            map[string]types.RecentEvents // roomID -> events
	roomSummaries           map[string]*types.Summary
	maxStreamPositions      map[string]types.StreamPosition // roomID -> position
	inviteEvents            map[string]*rstypes.HeaderedEvent // roomID -> invite event
	retiredInvites          map[string]*rstypes.HeaderedEvent // roomID -> retired invite
	maxInvitePos            types.StreamPosition
	membershipCounts        map[string]map[string]int // roomID -> membership -> count
}

type mockMembership struct {
	membership string
	topoPos    int64
}

// newMockSnapshot creates a new mock snapshot with default empty maps
func newMockSnapshot() *mockSnapshot {
	return &mockSnapshot{
		membershipForUser:  make(map[string]map[string]mockMembership),
		stateEvents:        make(map[string]map[string]*rstypes.HeaderedEvent),
		recentEvents:       make(map[string]types.RecentEvents),
		roomSummaries:      make(map[string]*types.Summary),
		maxStreamPositions: make(map[string]types.StreamPosition),
		inviteEvents:       make(map[string]*rstypes.HeaderedEvent),
		retiredInvites:     make(map[string]*rstypes.HeaderedEvent),
		membershipCounts:   make(map[string]map[string]int),
	}
}

// SetMembership sets the membership for a user in a room
func (m *mockSnapshot) SetMembership(roomID, userID, membership string, topoPos int64) {
	if m.membershipForUser[roomID] == nil {
		m.membershipForUser[roomID] = make(map[string]mockMembership)
	}
	m.membershipForUser[roomID][userID] = mockMembership{
		membership: membership,
		topoPos:    topoPos,
	}
}

// SetStateEvent sets a state event for a room
func (m *mockSnapshot) SetStateEvent(roomID, eventType, stateKey string, event *rstypes.HeaderedEvent) {
	if m.stateEvents[roomID] == nil {
		m.stateEvents[roomID] = make(map[string]*rstypes.HeaderedEvent)
	}
	key := eventType + "|" + stateKey
	m.stateEvents[roomID][key] = event
}

// SetRecentEvents sets recent events for a room
func (m *mockSnapshot) SetRecentEvents(roomID string, events types.RecentEvents) {
	m.recentEvents[roomID] = events
}

// SetRoomSummary sets the room summary for a room
func (m *mockSnapshot) SetRoomSummary(roomID string, summary *types.Summary) {
	m.roomSummaries[roomID] = summary
}

// SetMaxStreamPosition sets the max stream position for a room
func (m *mockSnapshot) SetMaxStreamPosition(roomID string, pos types.StreamPosition) {
	m.maxStreamPositions[roomID] = pos
}

// SetMembershipCount sets the membership count for a room
func (m *mockSnapshot) SetMembershipCount(roomID, membership string, count int) {
	if m.membershipCounts[roomID] == nil {
		m.membershipCounts[roomID] = make(map[string]int)
	}
	m.membershipCounts[roomID][membership] = count
}

// Interface implementations

func (m *mockSnapshot) SelectMembershipForUser(ctx context.Context, roomID, userID string, pos int64) (string, int64, error) {
	if roomUsers, ok := m.membershipForUser[roomID]; ok {
		if membership, ok := roomUsers[userID]; ok {
			// If pos is specified, only return membership if topoPos <= pos
			if membership.topoPos <= pos {
				return membership.membership, membership.topoPos, nil
			}
		}
	}
	return "leave", 0, nil
}

func (m *mockSnapshot) GetStateEvent(ctx context.Context, roomID, evType, stateKey string) (*rstypes.HeaderedEvent, error) {
	if roomEvents, ok := m.stateEvents[roomID]; ok {
		key := evType + "|" + stateKey
		if event, ok := roomEvents[key]; ok {
			return event, nil
		}
	}
	return nil, nil
}

func (m *mockSnapshot) GetStateEventsForRoom(ctx context.Context, roomID string, stateFilterPart *synctypes.StateFilter) ([]*rstypes.HeaderedEvent, error) {
	var events []*rstypes.HeaderedEvent
	if roomEvents, ok := m.stateEvents[roomID]; ok {
		for _, event := range roomEvents {
			events = append(events, event)
		}
	}
	return events, nil
}

func (m *mockSnapshot) RecentEvents(ctx context.Context, roomIDs []string, r types.Range, eventFilter *synctypes.RoomEventFilter, chronologicalOrder bool, onlySyncEvents bool) (map[string]types.RecentEvents, error) {
	result := make(map[string]types.RecentEvents)
	for _, roomID := range roomIDs {
		if events, ok := m.recentEvents[roomID]; ok {
			result[roomID] = events
		}
	}
	return result, nil
}

func (m *mockSnapshot) GetRoomSummary(ctx context.Context, roomID, userID string) (*types.Summary, error) {
	if summary, ok := m.roomSummaries[roomID]; ok {
		return summary, nil
	}
	return &types.Summary{}, nil
}

func (m *mockSnapshot) MaxStreamPositionsForRooms(ctx context.Context, roomIDs []string) (map[string]types.StreamPosition, error) {
	result := make(map[string]types.StreamPosition)
	for _, roomID := range roomIDs {
		if pos, ok := m.maxStreamPositions[roomID]; ok {
			result[roomID] = pos
		}
	}
	return result, nil
}

func (m *mockSnapshot) InviteEventsInRange(ctx context.Context, targetUserID string, r types.Range) (map[string]*rstypes.HeaderedEvent, map[string]*rstypes.HeaderedEvent, types.StreamPosition, error) {
	return m.inviteEvents, m.retiredInvites, m.maxInvitePos, nil
}

func (m *mockSnapshot) MaxStreamPositionForInvites(ctx context.Context) (types.StreamPosition, error) {
	return m.maxInvitePos, nil
}

func (m *mockSnapshot) MembershipCount(ctx context.Context, roomID, membership string, pos types.StreamPosition) (int, error) {
	if roomCounts, ok := m.membershipCounts[roomID]; ok {
		if count, ok := roomCounts[membership]; ok {
			return count, nil
		}
	}
	return 0, nil
}

func (m *mockSnapshot) EventPositionInTopology(ctx context.Context, eventID string) (types.TopologyToken, error) {
	// Return a simple token for testing
	return types.TopologyToken{Depth: 10, PDUPosition: 100}, nil
}

// Transaction interface methods (no-ops for testing)
func (m *mockSnapshot) Commit() error   { return nil }
func (m *mockSnapshot) Rollback() error { return nil }

// mockRoomserverAPI implements api.SyncRoomserverAPI for testing
// Uses interface embedding to satisfy the interface without implementing all methods
type mockRoomserverAPI struct {
	roomserverAPI.SyncRoomserverAPI
}

func (m *mockRoomserverAPI) QueryUserIDForSender(ctx context.Context, roomID spec.RoomID, senderID spec.SenderID) (*spec.UserID, error) {
	return spec.NewUserID(string(senderID), true)
}

// createMockStateEvent creates a mock HeaderedEvent for testing
// This is a simple helper that creates events with minimal structure
func createMockStateEvent(eventType, stateKey, content string) *rstypes.HeaderedEvent {
	// Create a minimal valid event JSON with proper room version format
	eventJSON := []byte(`{
		"type":"` + eventType + `",
		"state_key":"` + stateKey + `",
		"room_id":"!test:localhost",
		"sender":"@test:localhost",
		"event_id":"$test:localhost",
		"origin_server_ts":1234567890,
		"depth":1,
		"prev_events":[],
		"auth_events":[],
		"content":` + content + `
	}`)

	// Use gomatrixserverlib to parse the event
	// Use room version 10 which is commonly used
	verImpl, _ := gomatrixserverlib.GetRoomVersion(gomatrixserverlib.RoomVersionV10)
	event, err := verImpl.NewEventFromTrustedJSON(eventJSON, false)
	if err != nil {
		// Return nil if parsing fails - test will catch this
		return nil
	}

	return &rstypes.HeaderedEvent{PDU: event}
}
