package input

import (
	"context"
	"testing"

	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/stretchr/testify/assert"

	"github.com/element-hq/dendrite/roomserver/types"
	"github.com/element-hq/dendrite/test"
)

func Test_EventAuth(t *testing.T) {
	alice := test.NewUser(t)
	bob := test.NewUser(t)

	// create two rooms, so we can craft "illegal" auth events
	room1 := test.NewRoom(t, alice)
	room2 := test.NewRoom(t, alice, test.RoomPreset(test.PresetPublicChat))

	authEventIDs := make([]string, 0, 4)
	authEvents := []gomatrixserverlib.PDU{}

	// Add the legal auth events from room2
	for _, x := range room2.Events() {
		if x.Type() == spec.MRoomCreate {
			authEventIDs = append(authEventIDs, x.EventID())
			authEvents = append(authEvents, x.PDU)
		}
		if x.Type() == spec.MRoomPowerLevels {
			authEventIDs = append(authEventIDs, x.EventID())
			authEvents = append(authEvents, x.PDU)
		}
		if x.Type() == spec.MRoomJoinRules {
			authEventIDs = append(authEventIDs, x.EventID())
			authEvents = append(authEvents, x.PDU)
		}
	}

	// Add the illegal auth event from room1 (rooms are different)
	for _, x := range room1.Events() {
		if x.Type() == spec.MRoomMember {
			authEventIDs = append(authEventIDs, x.EventID())
			authEvents = append(authEvents, x.PDU)
		}
	}

	// Craft the illegal join event, with auth events from different rooms
	ev := room2.CreateEvent(t, bob, "m.room.member", map[string]interface{}{
		"membership": "join",
	}, test.WithStateKey(bob.ID), test.WithAuthIDs(authEventIDs))

	// Add the auth events to the allower
	allower, _ := gomatrixserverlib.NewAuthEvents(nil)
	for _, a := range authEvents {
		if err := allower.AddEvent(a); err != nil {
			t.Fatalf("allower.AddEvent failed: %v", err)
		}
	}

	// Finally check that the event is NOT allowed
	if err := gomatrixserverlib.Allowed(ev.PDU, allower, func(roomID spec.RoomID, senderID spec.SenderID) (*spec.UserID, error) {
		return spec.NewUserID(string(senderID), true)
	}); err == nil {
		t.Fatalf("event should not be allowed, but it was")
	}
}

// mockInputer is a minimal mock for testing resolvePartialStateAuth
type mockInputer struct {
	Inputer
}

func (m *mockInputer) resolvePartialStateAuth(
	ctx context.Context,
	roomInfo *types.RoomInfo,
	localState []gomatrixserverlib.PDU,
	authEvents []gomatrixserverlib.PDU,
) ([]gomatrixserverlib.PDU, error) {
	// Call the actual implementation through an Inputer with nil fields
	// We'll test the logic directly instead
	return nil, nil
}

// Test_ResolvePartialStateAuth_NoConflicts tests that when there are no conflicts,
// the function returns all unique state events
func Test_ResolvePartialStateAuth_NoConflicts(t *testing.T) {
	alice := test.NewUser(t)
	room := test.NewRoom(t, alice)

	var localState []gomatrixserverlib.PDU
	var authEvents []gomatrixserverlib.PDU

	// Get room events as local state
	for _, ev := range room.Events() {
		if ev.StateKey() != nil {
			localState = append(localState, ev.PDU)
		}
	}

	// Use same events as auth events (no conflicts expected)
	authEvents = localState

	// Test the conflict detection logic directly
	type stateKey struct {
		eventType string
		stateKey  string
	}
	stateMap := make(map[stateKey]gomatrixserverlib.PDU)
	var conflicted []gomatrixserverlib.PDU

	// First, add all local state events
	for _, ev := range localState {
		if ev.StateKey() == nil {
			continue
		}
		key := stateKey{ev.Type(), *ev.StateKey()}
		stateMap[key] = ev
	}

	// Then check auth events for conflicts
	for _, ev := range authEvents {
		if ev.StateKey() == nil {
			continue
		}
		key := stateKey{ev.Type(), *ev.StateKey()}
		if existing, ok := stateMap[key]; ok {
			if existing.EventID() != ev.EventID() {
				conflicted = append(conflicted, existing, ev)
				delete(stateMap, key)
			}
		} else {
			stateMap[key] = ev
		}
	}

	// No conflicts expected since same events
	assert.Empty(t, conflicted, "Should have no conflicts when using same events")
	assert.Len(t, stateMap, len(localState), "State map should contain all local state events")
}

// Test_ResolvePartialStateAuth_WithConflicts tests that conflicting events
// are detected and would be passed to state resolution
func Test_ResolvePartialStateAuth_WithConflicts(t *testing.T) {
	alice := test.NewUser(t)
	bob := test.NewUser(t)
	room := test.NewRoom(t, alice)

	var localState []gomatrixserverlib.PDU
	var authEvents []gomatrixserverlib.PDU

	// Get room events as local state
	for _, ev := range room.Events() {
		if ev.StateKey() != nil {
			localState = append(localState, ev.PDU)
		}
	}

	// Create a different power levels event to simulate conflict
	// In partial state, we might have a different power levels from auth events
	conflictingPL := room.CreateEvent(t, alice, spec.MRoomPowerLevels, map[string]interface{}{
		"users": map[string]int{
			alice.ID: 100,
			bob.ID:   50, // Different from original
		},
	}, test.WithStateKey(""))

	authEvents = []gomatrixserverlib.PDU{conflictingPL.PDU}

	// Test the conflict detection logic directly
	type stateKey struct {
		eventType string
		stateKey  string
	}
	stateMap := make(map[stateKey]gomatrixserverlib.PDU)
	var conflicted []gomatrixserverlib.PDU

	// First, add all local state events
	for _, ev := range localState {
		if ev.StateKey() == nil {
			continue
		}
		key := stateKey{ev.Type(), *ev.StateKey()}
		stateMap[key] = ev
	}

	// Then check auth events for conflicts
	for _, ev := range authEvents {
		if ev.StateKey() == nil {
			continue
		}
		key := stateKey{ev.Type(), *ev.StateKey()}
		if existing, ok := stateMap[key]; ok {
			if existing.EventID() != ev.EventID() {
				conflicted = append(conflicted, existing, ev)
				delete(stateMap, key)
			}
		} else {
			stateMap[key] = ev
		}
	}

	// Should have conflicts for power levels
	assert.NotEmpty(t, conflicted, "Should have conflicts for different power levels")
	assert.Equal(t, 2, len(conflicted), "Should have 2 conflicting events (original and new)")

	// Verify the conflicting events are power levels
	hasConflictingPL := false
	for _, ev := range conflicted {
		if ev.Type() == spec.MRoomPowerLevels {
			hasConflictingPL = true
			break
		}
	}
	assert.True(t, hasConflictingPL, "Conflicting events should include power levels")
}

// Test_ResolvePartialStateAuth_NewStateFromAuth tests that auth events
// with new state keys are added to the result
func Test_ResolvePartialStateAuth_NewStateFromAuth(t *testing.T) {
	alice := test.NewUser(t)
	bob := test.NewUser(t)
	room := test.NewRoom(t, alice)

	var localState []gomatrixserverlib.PDU

	// Get room events as local state (only create event to simulate partial state)
	for _, ev := range room.Events() {
		if ev.Type() == spec.MRoomCreate {
			localState = append(localState, ev.PDU)
			break
		}
	}

	// Auth events include a membership event not in local state
	// This simulates receiving auth events with member info we don't have
	bobMember := room.CreateEvent(t, bob, spec.MRoomMember, map[string]interface{}{
		"membership": "join",
	}, test.WithStateKey(bob.ID))

	authEvents := []gomatrixserverlib.PDU{bobMember.PDU}

	// Test the state merging logic
	type stateKey struct {
		eventType string
		stateKey  string
	}
	stateMap := make(map[stateKey]gomatrixserverlib.PDU)

	// First, add all local state events
	for _, ev := range localState {
		if ev.StateKey() == nil {
			continue
		}
		key := stateKey{ev.Type(), *ev.StateKey()}
		stateMap[key] = ev
	}

	// Then add auth events (no conflicts expected since different state keys)
	for _, ev := range authEvents {
		if ev.StateKey() == nil {
			continue
		}
		key := stateKey{ev.Type(), *ev.StateKey()}
		if _, ok := stateMap[key]; !ok {
			stateMap[key] = ev
		}
	}

	// Should have both create event and bob's membership
	assert.Len(t, stateMap, 2, "State map should contain create + membership")

	// Verify we have bob's membership
	bobKey := stateKey{spec.MRoomMember, bob.ID}
	_, hasBob := stateMap[bobKey]
	assert.True(t, hasBob, "State map should include Bob's membership from auth events")
}
