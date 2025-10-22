// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package input

import (
	"testing"

	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/stretchr/testify/assert"

	"github.com/element-hq/dendrite/roomserver/types"
	"github.com/element-hq/dendrite/test"
)

// Test_EventAuth verifies that cross-room auth chains are correctly rejected.
// This is critical validation logic to prevent auth events from one room being
// used to authorize events in another room.
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

// TestRejectedError tests the RejectedError type
func TestRejectedError(t *testing.T) {
	t.Parallel()

	// Create a rejected error
	err := types.RejectedError("test rejection reason")
	assert.Error(t, err, "RejectedError should be an error")
	assert.Contains(t, err.Error(), "test rejection reason", "Error message should contain reason")
}

// TestMissingStateError tests MissingStateError type
func TestMissingStateError(t *testing.T) {
	t.Parallel()

	// Create a missing state error
	err := types.MissingStateError("missing state for event")
	assert.Error(t, err, "MissingStateError should be an error")
	assert.Contains(t, err.Error(), "missing state", "Error message should indicate missing state")
}

// TestErrorInvalidRoomInfo tests ErrorInvalidRoomInfo
func TestErrorInvalidRoomInfo(t *testing.T) {
	t.Parallel()

	err := types.ErrorInvalidRoomInfo
	assert.Error(t, err, "ErrorInvalidRoomInfo should be an error")
}
