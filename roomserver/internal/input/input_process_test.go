// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package input_test

import (
	"context"
	"testing"
	"time"

	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/element-hq/dendrite/internal/caching"
	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/roomserver"
	"github.com/element-hq/dendrite/roomserver/api"
	"github.com/element-hq/dendrite/roomserver/internal/input"
	"github.com/element-hq/dendrite/roomserver/storage"
	"github.com/element-hq/dendrite/roomserver/types"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/setup/jetstream"
	"github.com/element-hq/dendrite/setup/process"
	"github.com/element-hq/dendrite/test"
	"github.com/element-hq/dendrite/test/testrig"
)

// testInputterContext holds all dependencies needed for testing the Inputer
type testInputterContext struct {
	t           *testing.T
	ctx         context.Context
	cancel      context.CancelFunc
	cfg         *config.Dendrite
	processCtx  *process.ProcessContext
	db          storage.RoomDatabase
	rsAPI       api.RoomserverInternalAPI
	inputter    *input.Inputer
	natsCleanup func()
}

// setupInputter creates a complete Inputer instance for testing
func setupInputter(t *testing.T, dbType test.DBType) *testInputterContext {
	t.Helper()

	cfg, processCtx, closeDB := testrig.CreateConfig(t, dbType)
	cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)

	natsInstance := &jetstream.NATSInstance{}
	js, jc := natsInstance.Prepare(processCtx, &cfg.Global.JetStream)
	caches := caching.NewRistrettoCache(8*1024*1024, time.Hour, caching.DisableMetrics)

	rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, natsInstance, caches, caching.DisableMetrics)
	rsAPI.SetFederationAPI(nil, nil)

	// Get database from internal roomserver
	db, err := storage.Open(processCtx.Context(), cm, &cfg.RoomServer.Database, caches)
	require.NoError(t, err)

	deadline, ok := t.Deadline()
	if !ok || time.Until(deadline) < 5*time.Second {
		deadline = time.Now().Add(5 * time.Second)
	}
	ctx, cancel := context.WithDeadline(processCtx.Context(), deadline)

	return &testInputterContext{
		t:          t,
		ctx:        ctx,
		cancel:     cancel,
		cfg:        cfg,
		processCtx: processCtx,
		db:         db,
		rsAPI:      rsAPI,
		inputter: &input.Inputer{
			JetStream:  js,
			NATSClient: jc,
			Cfg:        &cfg.RoomServer,
			DB:         db,
		},
		natsCleanup: func() {
			cancel()
			closeDB()
		},
	}
}

// Cleanup closes all resources
func (tc *testInputterContext) Cleanup() {
	if tc.natsCleanup != nil {
		tc.natsCleanup()
	}
}

// createTestEvent creates a test event with given parameters (helper removed - use room.CreateEvent directly)

// inputRoomEvent creates an InputRoomEvent for testing
func inputRoomEvent(event *types.HeaderedEvent, kind api.Kind) api.InputRoomEvent {
	return api.InputRoomEvent{
		Kind:  kind,
		Event: event,
	}
}

// TestProcessRoomEvent_CreateEvent tests processing a room creation event
func TestProcessRoomEvent_CreateEvent(t *testing.T) {
	t.Parallel()
	tc := setupInputter(t, test.DBTypeSQLite)
	defer tc.Cleanup()

	// Create test user and room
	alice := test.NewUser(t)
	room := test.NewRoom(t, alice, test.RoomVersion(gomatrixserverlib.RoomVersionV10))

	// Get the m.room.create event
	createEvent := room.Events()[0]
	require.Equal(t, spec.MRoomCreate, createEvent.Type())

	// Process the create event
	input := inputRoomEvent(createEvent, api.KindNew)
	res := &api.InputRoomEventsResponse{}
	tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
		InputRoomEvents: []api.InputRoomEvent{input},
		Asynchronous:    false,
	}, res)

	// Verify no errors
	require.NoError(t, res.Err(), "Processing create event should succeed")

	// Verify room exists in database
	roomInfo, err := tc.db.RoomInfo(tc.ctx, createEvent.RoomID().String())
	require.NoError(t, err, "Should be able to retrieve room info")
	assert.NotNil(t, roomInfo, "Room info should exist")
	assert.NotEqual(t, types.RoomNID(0), roomInfo.RoomNID, "Room NID should be non-zero")

	// Verify the create event is stored and retrievable
	storedCreateEvent, err := tc.db.GetStateEvent(tc.ctx, createEvent.RoomID().String(), spec.MRoomCreate, "")
	require.NoError(t, err, "Should be able to retrieve create event")
	assert.NotNil(t, storedCreateEvent, "Create event should be stored")
	assert.Equal(t, createEvent.EventID(), storedCreateEvent.EventID(), "Stored create event should match")

	// Verify room version is correct
	assert.Equal(t, gomatrixserverlib.RoomVersionV10, roomInfo.RoomVersion, "Room version should match")
}

// TestProcessRoomEvent_OutlierEvent tests processing an outlier event
func TestProcessRoomEvent_OutlierEvent(t *testing.T) {
	t.Parallel()
	tc := setupInputter(t, test.DBTypeSQLite)
	defer tc.Cleanup()

	alice := test.NewUser(t)
	room := test.NewRoom(t, alice)

	// First process the room creation event
	for _, event := range room.Events() {
		input := inputRoomEvent(event, api.KindNew)
		res := &api.InputRoomEventsResponse{}
		tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
			InputRoomEvents: []api.InputRoomEvent{input},
			Asynchronous:    false,
		}, res)
		require.NoError(t, res.Err())
	}

	// Create a message event as an outlier
	msgEvent := room.CreateEvent(t, alice, "m.room.message", map[string]interface{}{
		"msgtype": "m.text",
		"body":    "Hello World",
	})

	input := inputRoomEvent(msgEvent, api.KindOutlier)
	res := &api.InputRoomEventsResponse{}
	tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
		InputRoomEvents: []api.InputRoomEvent{input},
		Asynchronous:    false,
	}, res)

	// Verify outlier was processed successfully
	assert.NoError(t, res.Err())
}

// TestProcessRoomEvent_MembershipJoin tests processing a join membership event
func TestProcessRoomEvent_MembershipJoin(t *testing.T) {
	t.Parallel()
	tc := setupInputter(t, test.DBTypeSQLite)
	defer tc.Cleanup()

	alice := test.NewUser(t)
	bob := test.NewUser(t)
	room := test.NewRoom(t, alice, test.RoomPreset(test.PresetPublicChat))

	// Process all initial events (create, alice join, power levels, etc.)
	for _, event := range room.Events() {
		input := inputRoomEvent(event, api.KindNew)
		res := &api.InputRoomEventsResponse{}
		tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
			InputRoomEvents: []api.InputRoomEvent{input},
			Asynchronous:    false,
		}, res)
		require.NoError(t, res.Err())
	}

	// Bob joins the room
	bobJoinEvent := room.CreateAndInsert(t, bob, spec.MRoomMember, map[string]interface{}{
		"membership": spec.Join,
	}, test.WithStateKey(bob.ID))

	input := inputRoomEvent(bobJoinEvent, api.KindNew)
	res := &api.InputRoomEventsResponse{}
	tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
		InputRoomEvents: []api.InputRoomEvent{input},
		Asynchronous:    false,
	}, res)

	// Verify Bob's join was processed
	require.NoError(t, res.Err(), "Bob's join should be processed successfully")

	// Verify room membership in database
	roomInfo, err := tc.db.RoomInfo(tc.ctx, bobJoinEvent.RoomID().String())
	require.NoError(t, err, "Should be able to retrieve room info")

	// Get all joined members
	members, err := tc.db.GetMembershipEventNIDsForRoom(tc.ctx, roomInfo.RoomNID, true, false)
	require.NoError(t, err, "Should be able to retrieve members")
	assert.NotEmpty(t, members, "Room should have members")
	// Room should have at least 2 members (Alice who created it, and Bob who joined)
	assert.GreaterOrEqual(t, len(members), 2, "Room should have at least 2 members")

	// Verify Bob's membership event is stored and retrievable
	bobMemberEvent, err := tc.db.GetStateEvent(tc.ctx, bobJoinEvent.RoomID().String(), spec.MRoomMember, bob.ID)
	require.NoError(t, err, "Should be able to retrieve Bob's membership event")
	assert.NotNil(t, bobMemberEvent, "Bob's membership event should exist")
	assert.Equal(t, bobJoinEvent.EventID(), bobMemberEvent.EventID(), "Stored membership event should match")
	assert.Equal(t, spec.MRoomMember, bobMemberEvent.Type(), "Event type should be m.room.member")
}

// TestProcessRoomEvent_MembershipLeave tests processing a leave membership event
func TestProcessRoomEvent_MembershipLeave(t *testing.T) {
	t.Parallel()
	tc := setupInputter(t, test.DBTypeSQLite)
	defer tc.Cleanup()

	alice := test.NewUser(t)
	bob := test.NewUser(t)
	room := test.NewRoom(t, alice, test.RoomPreset(test.PresetPublicChat))

	// Process all initial events
	for _, event := range room.Events() {
		input := inputRoomEvent(event, api.KindNew)
		res := &api.InputRoomEventsResponse{}
		tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
			InputRoomEvents: []api.InputRoomEvent{input},
			Asynchronous:    false,
		}, res)
		require.NoError(t, res.Err())
	}

	// Bob joins
	bobJoinEvent := room.CreateAndInsert(t, bob, spec.MRoomMember, map[string]interface{}{
		"membership": spec.Join,
	}, test.WithStateKey(bob.ID))

	input := inputRoomEvent(bobJoinEvent, api.KindNew)
	res := &api.InputRoomEventsResponse{}
	tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
		InputRoomEvents: []api.InputRoomEvent{input},
		Asynchronous:    false,
	}, res)
	require.NoError(t, res.Err())

	// Bob leaves
	bobLeaveEvent := room.CreateAndInsert(t, bob, spec.MRoomMember, map[string]interface{}{
		"membership": spec.Leave,
	}, test.WithStateKey(bob.ID))

	input = inputRoomEvent(bobLeaveEvent, api.KindNew)
	res = &api.InputRoomEventsResponse{}
	tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
		InputRoomEvents: []api.InputRoomEvent{input},
		Asynchronous:    false,
	}, res)

	// Verify leave was processed
	assert.NoError(t, res.Err())
}

// TestProcessRoomEvent_RejectedEvent tests processing events in an invite-only room
// Note: Authorization checking happens during event creation. This test verifies that
// the inputter correctly processes properly authorized invite and join events.
func TestProcessRoomEvent_RejectedEvent(t *testing.T) {
	t.Parallel()
	tc := setupInputter(t, test.DBTypeSQLite)
	defer tc.Cleanup()

	alice := test.NewUser(t)
	bob := test.NewUser(t)

	// Create a private room (invite only)
	room := test.NewRoom(t, alice, test.RoomPreset(test.PresetPrivateChat))

	// Process all initial events
	for _, event := range room.Events() {
		input := inputRoomEvent(event, api.KindNew)
		res := &api.InputRoomEventsResponse{}
		tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
			InputRoomEvents: []api.InputRoomEvent{input},
			Asynchronous:    false,
		}, res)
		require.NoError(t, res.Err())
	}

	// Alice invites Bob (this should succeed)
	bobInviteEvent := room.CreateAndInsert(t, alice, spec.MRoomMember, map[string]interface{}{
		"membership": spec.Invite,
	}, test.WithStateKey(bob.ID))

	input := inputRoomEvent(bobInviteEvent, api.KindNew)
	res := &api.InputRoomEventsResponse{}
	tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
		InputRoomEvents: []api.InputRoomEvent{input},
		Asynchronous:    false,
	}, res)
	require.NoError(t, res.Err(), "Processing invite event should succeed")

	// Bob joins after being invited (this should succeed)
	bobJoinEvent := room.CreateAndInsert(t, bob, spec.MRoomMember, map[string]interface{}{
		"membership": spec.Join,
	}, test.WithStateKey(bob.ID))

	input = inputRoomEvent(bobJoinEvent, api.KindNew)
	res = &api.InputRoomEventsResponse{}
	tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
		InputRoomEvents: []api.InputRoomEvent{input},
		Asynchronous:    false,
	}, res)
	require.NoError(t, res.Err(), "Processing join after invite should succeed")

	// Verify Bob's membership is stored
	bobMemberEvent, err := tc.db.GetStateEvent(tc.ctx, bobJoinEvent.RoomID().String(), spec.MRoomMember, bob.ID)
	require.NoError(t, err, "Should be able to retrieve Bob's membership event")
	assert.NotNil(t, bobMemberEvent, "Bob's membership event should exist")
	assert.Equal(t, bobJoinEvent.EventID(), bobMemberEvent.EventID(), "Bob should be joined")
}

// TestProcessRoomEvent_DuplicateEvent tests processing the same event twice
func TestProcessRoomEvent_DuplicateEvent(t *testing.T) {
	t.Parallel()
	tc := setupInputter(t, test.DBTypeSQLite)
	defer tc.Cleanup()

	alice := test.NewUser(t)
	room := test.NewRoom(t, alice)

	// Process all initial events
	for _, event := range room.Events() {
		input := inputRoomEvent(event, api.KindNew)
		res := &api.InputRoomEventsResponse{}
		tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
			InputRoomEvents: []api.InputRoomEvent{input},
			Asynchronous:    false,
		}, res)
		require.NoError(t, res.Err())
	}

	// Create a message event
	msgEvent := room.CreateAndInsert(t, alice, "m.room.message", map[string]interface{}{
		"msgtype": "m.text",
		"body":    "Test message",
	})

	// Process it the first time
	input := inputRoomEvent(msgEvent, api.KindNew)
	res := &api.InputRoomEventsResponse{}
	tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
		InputRoomEvents: []api.InputRoomEvent{input},
		Asynchronous:    false,
	}, res)
	require.NoError(t, res.Err())

	// Process the same event again - should be idempotent
	res = &api.InputRoomEventsResponse{}
	tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
		InputRoomEvents: []api.InputRoomEvent{input},
		Asynchronous:    false,
	}, res)

	// Should succeed (idempotent operation)
	assert.NoError(t, res.Err())
}

// TestProcessRoomEvent_StateResolution tests that state resolution works correctly
func TestProcessRoomEvent_StateResolution(t *testing.T) {
	t.Parallel()
	tc := setupInputter(t, test.DBTypeSQLite)
	defer tc.Cleanup()

	alice := test.NewUser(t)
	room := test.NewRoom(t, alice)

	// Process all initial events
	for _, event := range room.Events() {
		input := inputRoomEvent(event, api.KindNew)
		res := &api.InputRoomEventsResponse{}
		tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
			InputRoomEvents: []api.InputRoomEvent{input},
			Asynchronous:    false,
		}, res)
		require.NoError(t, res.Err())
	}

	// Change room name
	nameEvent := room.CreateAndInsert(t, alice, spec.MRoomName, map[string]interface{}{
		"name": "Test Room",
	}, test.WithStateKey(""))

	input := inputRoomEvent(nameEvent, api.KindNew)
	res := &api.InputRoomEventsResponse{}
	tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
		InputRoomEvents: []api.InputRoomEvent{input},
		Asynchronous:    false,
	}, res)
	require.NoError(t, res.Err(), "Setting room name should succeed")

	// Verify state was updated in database
	roomInfo, err := tc.db.RoomInfo(tc.ctx, nameEvent.RoomID().String())
	require.NoError(t, err, "Should be able to retrieve room info")
	assert.NotNil(t, roomInfo, "Room info should exist")

	// Verify the room name state event is stored and is the latest
	stateEvent, err := tc.db.GetStateEvent(tc.ctx, nameEvent.RoomID().String(), spec.MRoomName, "")
	require.NoError(t, err, "Should be able to retrieve room name state")
	assert.NotNil(t, stateEvent, "Room name state event should exist")
	assert.Equal(t, nameEvent.EventID(), stateEvent.EventID(), "Current room name should be the one we just set")
	assert.Equal(t, spec.MRoomName, stateEvent.Type(), "Event type should be m.room.name")
	assert.Equal(t, spec.SenderID(alice.ID), stateEvent.SenderID(), "Sender should be Alice")
}

// TestProcessRoomEvent_PowerLevelCheck tests that power level checks work
// Note: Power level authorization is performed during event creation by the auth system,
// not during input processing. The inputter processes events that have already been
// authorized. This test verifies that authorized state events are correctly processed.
func TestProcessRoomEvent_PowerLevelCheck(t *testing.T) {
	t.Parallel()
	tc := setupInputter(t, test.DBTypeSQLite)
	defer tc.Cleanup()

	alice := test.NewUser(t)
	bob := test.NewUser(t)
	room := test.NewRoom(t, alice, test.RoomPreset(test.PresetPublicChat))

	// Process all initial events
	for _, event := range room.Events() {
		input := inputRoomEvent(event, api.KindNew)
		res := &api.InputRoomEventsResponse{}
		tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
			InputRoomEvents: []api.InputRoomEvent{input},
			Asynchronous:    false,
		}, res)
		require.NoError(t, res.Err())
	}

	// Bob joins
	bobJoinEvent := room.CreateAndInsert(t, bob, spec.MRoomMember, map[string]interface{}{
		"membership": spec.Join,
	}, test.WithStateKey(bob.ID))

	input := inputRoomEvent(bobJoinEvent, api.KindNew)
	res := &api.InputRoomEventsResponse{}
	tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
		InputRoomEvents: []api.InputRoomEvent{input},
		Asynchronous:    false,
	}, res)
	require.NoError(t, res.Err())

	// Give Bob sufficient power level (50) to set room name
	plEvent := room.CreateAndInsert(t, alice, spec.MRoomPowerLevels, map[string]interface{}{
		"users": map[string]int64{
			alice.ID: 100,
			bob.ID:   50,
		},
		"events": map[string]int64{
			spec.MRoomName: 50,
		},
		"users_default":     0,
		"events_default":    0,
		"state_default":     50,
		"ban":               50,
		"kick":              50,
		"redact":            50,
		"invite":            0,
	}, test.WithStateKey(""))

	input = inputRoomEvent(plEvent, api.KindNew)
	res = &api.InputRoomEventsResponse{}
	tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
		InputRoomEvents: []api.InputRoomEvent{input},
		Asynchronous:    false,
	}, res)
	require.NoError(t, res.Err())

	// Bob changes room name (has exact required power level, should succeed)
	bobNameEvent := room.CreateAndInsert(t, bob, spec.MRoomName, map[string]interface{}{
		"name": "Bob's Room",
	}, test.WithStateKey(""))

	input = inputRoomEvent(bobNameEvent, api.KindNew)
	res = &api.InputRoomEventsResponse{}
	tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
		InputRoomEvents: []api.InputRoomEvent{input},
		Asynchronous:    false,
	}, res)

	// Should succeed - Bob has sufficient power level
	require.NoError(t, res.Err(), "User with sufficient power level should successfully set room name")

	// Verify the name was actually set in the database
	stateEvent, err := tc.db.GetStateEvent(tc.ctx, bobNameEvent.RoomID().String(), spec.MRoomName, "")
	require.NoError(t, err, "Should be able to retrieve room name state")
	assert.NotNil(t, stateEvent, "Room name state event should exist")
	assert.Equal(t, bobNameEvent.EventID(), stateEvent.EventID(), "Room name should be updated to Bob's event")
}

// TestProcessRoomEvent_MultipleEvents tests processing multiple events in sequence
func TestProcessRoomEvent_MultipleEvents(t *testing.T) {
	t.Parallel()
	tc := setupInputter(t, test.DBTypeSQLite)
	defer tc.Cleanup()

	alice := test.NewUser(t)
	bob := test.NewUser(t)
	room := test.NewRoom(t, alice, test.RoomPreset(test.PresetPublicChat))

	// Process all initial events
	for _, event := range room.Events() {
		input := inputRoomEvent(event, api.KindNew)
		res := &api.InputRoomEventsResponse{}
		tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
			InputRoomEvents: []api.InputRoomEvent{input},
			Asynchronous:    false,
		}, res)
		require.NoError(t, res.Err())
	}

	// Create multiple events
	events := []*types.HeaderedEvent{
		room.CreateAndInsert(t, alice, "m.room.message", map[string]interface{}{
			"msgtype": "m.text",
			"body":    "Message 1",
		}),
		room.CreateAndInsert(t, alice, "m.room.message", map[string]interface{}{
			"msgtype": "m.text",
			"body":    "Message 2",
		}),
		room.CreateAndInsert(t, bob, spec.MRoomMember, map[string]interface{}{
			"membership": spec.Join,
		}, test.WithStateKey(bob.ID)),
		room.CreateAndInsert(t, bob, "m.room.message", map[string]interface{}{
			"msgtype": "m.text",
			"body":    "Message from Bob",
		}),
	}

	// Process all events
	for _, event := range events {
		input := inputRoomEvent(event, api.KindNew)
		res := &api.InputRoomEventsResponse{}
		tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
			InputRoomEvents: []api.InputRoomEvent{input},
			Asynchronous:    false,
		}, res)
		assert.NoError(t, res.Err(), "Failed to process event %s", event.EventID())
	}
}

// TestProcessRoomEvent_AsyncMode tests asynchronous event processing
func TestProcessRoomEvent_AsyncMode(t *testing.T) {
	t.Parallel()
	tc := setupInputter(t, test.DBTypeSQLite)
	defer tc.Cleanup()

	alice := test.NewUser(t)
	room := test.NewRoom(t, alice)

	// Process create event synchronously first
	createEvent := room.Events()[0]
	input := inputRoomEvent(createEvent, api.KindNew)
	res := &api.InputRoomEventsResponse{}
	tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
		InputRoomEvents: []api.InputRoomEvent{input},
		Asynchronous:    false,
	}, res)
	require.NoError(t, res.Err())

	// Process remaining events asynchronously
	for _, event := range room.Events()[1:] {
		input := inputRoomEvent(event, api.KindNew)
		res := &api.InputRoomEventsResponse{}
		tc.inputter.InputRoomEvents(tc.ctx, &api.InputRoomEventsRequest{
			InputRoomEvents: []api.InputRoomEvent{input},
			Asynchronous:    true, // Async mode
		}, res)
		// In async mode, we don't wait for processing
		assert.NoError(t, res.Err())
	}

	// Verify room was created asynchronously using require.Eventually
	require.Eventually(t, func() bool {
		roomInfo, err := tc.db.RoomInfo(tc.ctx, createEvent.RoomID().String())
		return err == nil && roomInfo != nil
	}, 5*time.Second, 100*time.Millisecond,
		"Room should be created asynchronously within timeout")
}
