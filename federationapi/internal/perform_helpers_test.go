// Copyright 2024 New Vector Ltd.
// Copyright 2022 The Matrix.org Foundation C.I.C.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package internal

import (
	"testing"

	"github.com/matrix-org/gomatrixserverlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create a test create event with specified room version
func createTestCreateEvent(t *testing.T, roomVersion string) gomatrixserverlib.PDU {
	t.Helper()

	content := `{"creator":"@test:localhost"`
	if roomVersion != "" {
		content += `,"room_version":"` + roomVersion + `"`
	}
	content += `}`

	eventJSON := `{
		"type":"m.room.create",
		"state_key":"",
		"sender":"@test:localhost",
		"room_id":"!test:localhost",
		"content":` + content + `,
		"auth_events":[],
		"prev_events":[],
		"depth":1,
		"origin_server_ts":1000000
	}`

	event, err := gomatrixserverlib.MustGetRoomVersion(gomatrixserverlib.RoomVersionV1).NewEventFromTrustedJSON(
		[]byte(eventJSON),
		false,
	)
	require.NoError(t, err, "failed to create test event")
	return event
}

// Helper function to create a non-create event
func createTestEvent(t *testing.T, eventType string) gomatrixserverlib.PDU {
	t.Helper()

	eventJSON := `{
		"type":"` + eventType + `",
		"sender":"@test:localhost",
		"room_id":"!test:localhost",
		"content":{},
		"auth_events":[],
		"prev_events":[],
		"depth":1,
		"origin_server_ts":1000000
	}`

	event, err := gomatrixserverlib.MustGetRoomVersion(gomatrixserverlib.RoomVersionV1).NewEventFromTrustedJSON(
		[]byte(eventJSON),
		false,
	)
	require.NoError(t, err, "failed to create test event")
	return event
}

// insertEventAt inserts an event at the specified position in a slice
func insertEventAt(events []gomatrixserverlib.PDU, position int, event gomatrixserverlib.PDU) []gomatrixserverlib.PDU {
	result := make([]gomatrixserverlib.PDU, 0, len(events)+1)
	result = append(result, events[:position]...)
	result = append(result, event)
	result = append(result, events[position:]...)
	return result
}

// Test checkEventsContainCreateEvent with valid create event
func TestCheckEventsContainCreateEvent_ValidCreateEvent_ReturnsNil(t *testing.T) {
	t.Parallel()

	createEvent := createTestCreateEvent(t, "1")
	events := []gomatrixserverlib.PDU{createEvent}

	err := checkEventsContainCreateEvent(events)

	assert.NoError(t, err, "valid create event should not return error")
}

// Test checkEventsContainCreateEvent with empty events list
func TestCheckEventsContainCreateEvent_EmptyList_ReturnsError(t *testing.T) {
	t.Parallel()

	events := []gomatrixserverlib.PDU{}

	err := checkEventsContainCreateEvent(events)

	assert.Error(t, err, "empty events list should return error")
	assert.Contains(t, err.Error(), "missing m.room.create", "error should mention missing create event")
}

// Test checkEventsContainCreateEvent without create event
func TestCheckEventsContainCreateEvent_NoCreateEvent_ReturnsError(t *testing.T) {
	t.Parallel()

	events := []gomatrixserverlib.PDU{
		createTestEvent(t, "m.room.member"),
		createTestEvent(t, "m.room.power_levels"),
		createTestEvent(t, "m.room.join_rules"),
	}

	err := checkEventsContainCreateEvent(events)

	assert.Error(t, err, "events without create should return error")
	assert.Contains(t, err.Error(), "missing m.room.create", "error should mention missing create event")
}

// Test checkEventsContainCreateEvent with unknown room version
func TestCheckEventsContainCreateEvent_UnknownVersion_ReturnsError(t *testing.T) {
	t.Parallel()

	createEvent := createTestCreateEvent(t, "unknown_version_999")
	events := []gomatrixserverlib.PDU{createEvent}

	err := checkEventsContainCreateEvent(events)

	assert.Error(t, err, "unknown room version should return error")
	assert.Contains(t, err.Error(), "unknown room version", "error should mention unknown version")
}

// Test checkEventsContainCreateEvent with no version (should default to "1")
func TestCheckEventsContainCreateEvent_NoVersion_DefaultsToV1(t *testing.T) {
	t.Parallel()

	createEvent := createTestCreateEvent(t, "")
	events := []gomatrixserverlib.PDU{createEvent}

	err := checkEventsContainCreateEvent(events)

	assert.NoError(t, err, "create event without version should default to v1")
}

// Test checkEventsContainCreateEvent with multiple events including create
func TestCheckEventsContainCreateEvent_MultipleEventsWithCreate_ReturnsNil(t *testing.T) {
	t.Parallel()

	events := []gomatrixserverlib.PDU{
		createTestCreateEvent(t, "1"),
		createTestEvent(t, "m.room.member"),
		createTestEvent(t, "m.room.power_levels"),
		createTestEvent(t, "m.room.join_rules"),
	}

	err := checkEventsContainCreateEvent(events)

	assert.NoError(t, err, "events with create should not return error")
}

// Test checkEventsContainCreateEvent with create event at different positions
func TestCheckEventsContainCreateEvent_CreateAtDifferentPositions_ReturnsNil(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		position int // position of create event (0=first, 1=middle, 2=last)
	}{
		{"create first", 0},
		{"create middle", 1},
		{"create last", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			events := []gomatrixserverlib.PDU{
				createTestEvent(t, "m.room.member"),
				createTestEvent(t, "m.room.power_levels"),
				createTestEvent(t, "m.room.join_rules"),
			}

			// Insert create event at specified position
			createEvent := createTestCreateEvent(t, "1")
			events = insertEventAt(events, tt.position, createEvent)

			err := checkEventsContainCreateEvent(events)

			assert.NoError(t, err, "create event at any position should be valid")
		})
	}
}

// Test checkEventsContainCreateEvent with various known room versions
func TestCheckEventsContainCreateEvent_KnownVersions_ReturnsNil(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
	}{
		{"version 1", "1"},
		{"version 2", "2"},
		{"version 3", "3"},
		{"version 4", "4"},
		{"version 5", "5"},
		{"version 6", "6"},
		{"version 7", "7"},
		{"version 8", "8"},
		{"version 9", "9"},
		{"version 10", "10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			createEvent := createTestCreateEvent(t, tt.version)
			events := []gomatrixserverlib.PDU{createEvent}

			err := checkEventsContainCreateEvent(events)

			assert.NoError(t, err, "known room version should not return error")
		})
	}
}
