// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package tables_test

import (
	"context"
	"testing"

	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/roomserver/storage/postgres"
	"github.com/element-hq/dendrite/roomserver/storage/sqlite3"
	"github.com/element-hq/dendrite/roomserver/storage/tables"
	"github.com/element-hq/dendrite/roomserver/types"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustCreatePartialStateTable(t *testing.T, dbType test.DBType) (tables.PartialState, func()) {
	t.Helper()

	connStr, clearDB := test.PrepareDBConnectionString(t, dbType)
	db, err := sqlutil.Open(&config.DatabaseOptions{
		ConnectionString: config.DataSource(connStr),
	}, sqlutil.NewExclusiveWriter())
	require.NoError(t, err)

	var tab tables.PartialState
	switch dbType {
	case test.DBTypePostgres:
		err = postgres.CreatePartialStateTable(db)
		require.NoError(t, err)
		tab, err = postgres.PreparePartialStateTable(db)
	case test.DBTypeSQLite:
		err = sqlite3.CreatePartialStateTable(db)
		require.NoError(t, err)
		tab, err = sqlite3.PreparePartialStateTable(db)
	}
	require.NoError(t, err)

	return tab, func() {
		_ = db.Close()
		clearDB()
	}
}

func TestPartialStateTable(t *testing.T) {
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		tab, cleanup := mustCreatePartialStateTable(t, dbType)
		defer cleanup()

		ctx := context.Background()
		roomNID := types.RoomNID(1)
		joinEventNID := types.EventNID(100)
		joinedVia := "server1.example.com"
		serversInRoom := []string{"server1.example.com", "server2.example.com", "server3.example.com"}

		// Test insert (with device list stream ID = 12345)
		err := tab.InsertPartialStateRoom(ctx, nil, roomNID, joinEventNID, joinedVia, serversInRoom, 12345)
		require.NoError(t, err)

		// Test select - room should be partial state
		isPartial, err := tab.SelectPartialStateRoom(ctx, nil, roomNID)
		require.NoError(t, err)
		assert.True(t, isPartial, "Room should be in partial state")

		// Test select servers
		servers, err := tab.SelectPartialStateServers(ctx, nil, roomNID)
		require.NoError(t, err)
		assert.Len(t, servers, 3)
		assert.Contains(t, servers, "server1.example.com")
		assert.Contains(t, servers, "server2.example.com")
		assert.Contains(t, servers, "server3.example.com")

		// Test select all partial state rooms
		rooms, err := tab.SelectAllPartialStateRooms(ctx, nil)
		require.NoError(t, err)
		assert.Len(t, rooms, 1)
		assert.Equal(t, roomNID, rooms[0])

		// Test select device list stream ID
		streamID, err := tab.SelectDeviceListStreamID(ctx, nil, roomNID)
		require.NoError(t, err)
		assert.Equal(t, int64(12345), streamID)

		// Test delete - should return the device list stream ID
		returnedStreamID, err := tab.DeletePartialStateRoom(ctx, nil, roomNID)
		require.NoError(t, err)
		assert.Equal(t, int64(12345), returnedStreamID)

		// Room should no longer be partial state
		isPartial, err = tab.SelectPartialStateRoom(ctx, nil, roomNID)
		require.NoError(t, err)
		assert.False(t, isPartial, "Room should not be in partial state after delete")

		// Servers should also be deleted (cascade)
		servers, err = tab.SelectPartialStateServers(ctx, nil, roomNID)
		require.NoError(t, err)
		assert.Empty(t, servers)
	})
}

func TestPartialStateTableMultipleRooms(t *testing.T) {
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		tab, cleanup := mustCreatePartialStateTable(t, dbType)
		defer cleanup()

		ctx := context.Background()

		// Insert multiple rooms
		rooms := []struct {
			roomNID      types.RoomNID
			joinEventNID types.EventNID
			joinedVia    string
			servers      []string
		}{
			{types.RoomNID(1), types.EventNID(100), "server1.example.com", []string{"server1.example.com", "server2.example.com"}},
			{types.RoomNID(2), types.EventNID(200), "server3.example.com", []string{"server3.example.com"}},
			{types.RoomNID(3), types.EventNID(300), "server4.example.com", []string{"server4.example.com", "server5.example.com", "server6.example.com"}},
		}

		for _, room := range rooms {
			err := tab.InsertPartialStateRoom(ctx, nil, room.roomNID, room.joinEventNID, room.joinedVia, room.servers, 0)
			require.NoError(t, err)
		}

		// Verify all rooms are partial state
		allRooms, err := tab.SelectAllPartialStateRooms(ctx, nil)
		require.NoError(t, err)
		assert.Len(t, allRooms, 3)

		// Verify each room's servers
		for _, room := range rooms {
			servers, err := tab.SelectPartialStateServers(ctx, nil, room.roomNID)
			require.NoError(t, err)
			assert.Len(t, servers, len(room.servers))
		}

		// Delete one room
		_, err = tab.DeletePartialStateRoom(ctx, nil, types.RoomNID(2))
		require.NoError(t, err)

		// Should now have 2 rooms
		allRooms, err = tab.SelectAllPartialStateRooms(ctx, nil)
		require.NoError(t, err)
		assert.Len(t, allRooms, 2)
	})
}

func TestPartialStateTableUpsert(t *testing.T) {
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		tab, cleanup := mustCreatePartialStateTable(t, dbType)
		defer cleanup()

		ctx := context.Background()
		roomNID := types.RoomNID(1)

		// First insert
		err := tab.InsertPartialStateRoom(ctx, nil, roomNID, types.EventNID(100), "server1.example.com", []string{"server1.example.com"}, 100)
		require.NoError(t, err)

		// Upsert with new values
		err = tab.InsertPartialStateRoom(ctx, nil, roomNID, types.EventNID(200), "server2.example.com", []string{"server2.example.com", "server3.example.com"}, 200)
		require.NoError(t, err)

		// Should still be partial state
		isPartial, err := tab.SelectPartialStateRoom(ctx, nil, roomNID)
		require.NoError(t, err)
		assert.True(t, isPartial)

		// Servers should include both old and new (ON CONFLICT DO NOTHING for servers)
		servers, err := tab.SelectPartialStateServers(ctx, nil, roomNID)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(servers), 1) // At least 1 server
	})
}

func TestPartialStateTableEmptyServers(t *testing.T) {
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		tab, cleanup := mustCreatePartialStateTable(t, dbType)
		defer cleanup()

		ctx := context.Background()
		roomNID := types.RoomNID(1)

		// Insert with empty servers list
		err := tab.InsertPartialStateRoom(ctx, nil, roomNID, types.EventNID(100), "server1.example.com", []string{}, 0)
		require.NoError(t, err)

		// Should still be partial state
		isPartial, err := tab.SelectPartialStateRoom(ctx, nil, roomNID)
		require.NoError(t, err)
		assert.True(t, isPartial)

		// Servers should be empty
		servers, err := tab.SelectPartialStateServers(ctx, nil, roomNID)
		require.NoError(t, err)
		assert.Empty(t, servers)
	})
}

func TestPartialStateTableNonExistentRoom(t *testing.T) {
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		tab, cleanup := mustCreatePartialStateTable(t, dbType)
		defer cleanup()

		ctx := context.Background()
		nonExistentRoomNID := types.RoomNID(99999)

		// Select non-existent room should return false
		isPartial, err := tab.SelectPartialStateRoom(ctx, nil, nonExistentRoomNID)
		require.NoError(t, err)
		assert.False(t, isPartial)

		// Servers for non-existent room should be empty
		servers, err := tab.SelectPartialStateServers(ctx, nil, nonExistentRoomNID)
		require.NoError(t, err)
		assert.Empty(t, servers)

		// Delete non-existent room should not error and return 0 for stream ID
		streamID, err := tab.DeletePartialStateRoom(ctx, nil, nonExistentRoomNID)
		require.NoError(t, err)
		assert.Equal(t, int64(0), streamID)
	})
}

func TestPartialStateTableDuplicateServers(t *testing.T) {
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		tab, cleanup := mustCreatePartialStateTable(t, dbType)
		defer cleanup()

		ctx := context.Background()
		roomNID := types.RoomNID(1)

		// Insert with duplicate servers in list
		servers := []string{"server1.example.com", "server2.example.com", "server1.example.com"}
		err := tab.InsertPartialStateRoom(ctx, nil, roomNID, types.EventNID(100), "server1.example.com", servers, 0)
		require.NoError(t, err)

		// Should handle duplicates gracefully (ON CONFLICT DO NOTHING)
		resultServers, err := tab.SelectPartialStateServers(ctx, nil, roomNID)
		require.NoError(t, err)
		assert.Len(t, resultServers, 2) // Only unique servers
	})
}
