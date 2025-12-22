// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/element-hq/dendrite/syncapi/storage"
	"github.com/element-hq/dendrite/syncapi/types"
	userapi "github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/sirupsen/logrus"
)

// RoomWithBumpStamp represents a room with its latest activity timestamp
type RoomWithBumpStamp struct {
	RoomID     string
	BumpStamp  int64 // Stream position of latest event
	Membership string
}

// GetRoomsForUser retrieves all rooms for a user with their bump stamps
// This will be used for building room lists and applying filters
func (rp *RequestPool) GetRoomsForUser(ctx context.Context, userID string, membership string) ([]RoomWithBumpStamp, error) {
	snapshot, err := rp.db.NewDatabaseSnapshot(ctx)
	if err != nil {
		logrus.WithError(err).Error("Failed to acquire database snapshot")
		return nil, err
	}
	var succeeded bool
	defer func() {
		if succeeded {
			snapshot.Commit() // Best effort
		}
		snapshot.Rollback() // No-op if already committed
	}()

	var roomIDs []string

	// IMPORTANT: Invites are stored in a separate table (syncapi_invite_events)
	// RoomIDsWithMembership only queries syncapi_current_room_state
	// We need to query both tables for invites (v3 sync uses InviteStreamProvider for this)
	if membership == "invite" || membership == spec.Invite {
		// Query the invites table using InviteEventsInRange
		// Use range from 0 to max to get all current invites
		maxID, err := snapshot.MaxStreamPositionForInvites(ctx)
		if err != nil {
			logrus.WithError(err).Warn("Failed to get max invite ID")
		} else if maxID > 0 {
			// Get all invite events for this user
			inviteRange := types.Range{
				From:      0,
				To:        maxID,
				Backwards: false,
			}
			invites, retired, _, err := snapshot.InviteEventsInRange(ctx, userID, inviteRange)
			if err != nil {
				logrus.WithError(err).Warn("Failed to query invite events")
			} else {
				// Extract room IDs from active invites (not retired)
				for roomID := range invites {
					// Only include if not in retired map
					if _, isRetired := retired[roomID]; !isRetired {
						roomIDs = append(roomIDs, roomID)
					}
				}
			}
		}
	} else {
		// For non-invite memberships, use the standard query
		roomIDs, err = snapshot.RoomIDsWithMembership(ctx, userID, membership)
		if err != nil {
			return nil, err
		}
	}

	// Get bump stamps (latest event positions) for all rooms
	rooms := make([]RoomWithBumpStamp, 0, len(roomIDs))

	// Query the maximum stream position (latest event) for each room
	bumpStamps, err := snapshot.MaxStreamPositionsForRooms(ctx, roomIDs)
	if err != nil {
		logrus.WithError(err).Warn("[V4_SYNC] Failed to get bump stamps for rooms")
		// Continue with zero bump stamps as fallback
		bumpStamps = make(map[string]types.StreamPosition)
	}

	for _, roomID := range roomIDs {
		rooms = append(rooms, RoomWithBumpStamp{
			RoomID:     roomID,
			BumpStamp:  int64(bumpStamps[roomID]),
			Membership: membership,
		})
	}

	logrus.WithFields(logrus.Fields{
		"user_id":    userID,
		"membership": membership,
		"room_count": len(rooms),
	}).Debug("[V4_SYNC] GetRoomsForUser completed")

	succeeded = true
	return rooms, nil
}

// GetKickedRooms retrieves rooms where the user was kicked (leave membership where sender != user).
// Per MSC4186/Synapse behavior, kicked rooms should be included in the sliding sync room list.
func (rp *RequestPool) GetKickedRooms(ctx context.Context, userID string) ([]RoomWithBumpStamp, error) {
	snapshot, err := rp.db.NewDatabaseSnapshot(ctx)
	if err != nil {
		logrus.WithError(err).Error("Failed to acquire database snapshot")
		return nil, err
	}
	var succeeded bool
	defer func() {
		if succeeded {
			snapshot.Commit() // Best effort
		}
		snapshot.Rollback() // No-op if already committed
	}()

	roomIDs, err := snapshot.KickedRoomIDs(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Query the maximum stream position (latest event) for each room
	bumpStamps, err := snapshot.MaxStreamPositionsForRooms(ctx, roomIDs)
	if err != nil {
		logrus.WithError(err).Warn("[V4_SYNC] Failed to get bump stamps for kicked rooms")
		bumpStamps = make(map[string]types.StreamPosition)
	}

	rooms := make([]RoomWithBumpStamp, 0, len(roomIDs))
	for _, roomID := range roomIDs {
		rooms = append(rooms, RoomWithBumpStamp{
			RoomID:     roomID,
			BumpStamp:  int64(bumpStamps[roomID]),
			Membership: spec.Leave,
		})
	}

	logrus.WithFields(logrus.Fields{
		"user_id":    userID,
		"room_count": len(rooms),
	}).Debug("[V4_SYNC] GetKickedRooms completed")

	succeeded = true
	return rooms, nil
}

// ApplyRoomFilters applies SlidingRoomFilter criteria to a list of rooms
func (rp *RequestPool) ApplyRoomFilters(
	ctx context.Context,
	rooms []RoomWithBumpStamp,
	filter *types.SlidingRoomFilter,
	userID string,
) ([]RoomWithBumpStamp, error) {
	if filter == nil {
		return rooms, nil
	}

	// Spaces filtering is not yet implemented (MSC4186)
	// Return error early if client tries to use it
	if len(filter.Spaces) > 0 {
		return nil, fmt.Errorf("spaces filtering is not yet implemented")
	}

	// PERFORMANCE: Create a single snapshot for all filter operations
	// This avoids N+1 snapshots when filtering many rooms
	snapshot, err := rp.db.NewDatabaseSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot for room filtering: %w", err)
	}
	defer snapshot.Rollback()

	filtered := make([]RoomWithBumpStamp, 0, len(rooms))

	for _, room := range rooms {
		// Apply all filter criteria using the shared snapshot
		if !rp.roomMatchesFilter(ctx, snapshot, room, filter, userID) {
			continue
		}
		filtered = append(filtered, room)
	}

	return filtered, nil
}

// roomMatchesFilter checks if a room matches all filter criteria
// PERFORMANCE: Accepts a snapshot parameter to avoid creating multiple database connections
func (rp *RequestPool) roomMatchesFilter(
	ctx context.Context,
	snapshot storage.DatabaseTransaction,
	room RoomWithBumpStamp,
	filter *types.SlidingRoomFilter,
	userID string,
) bool {
	// Phase 2: Basic implementation
	// Phase 7: Add optimized queries using sliding_sync_joined_rooms table

	// Filter by DM status
	if filter.IsDM != nil {
		isDM := rp.isDirectMessage(ctx, room.RoomID, userID)
		if isDM != *filter.IsDM {
			return false
		}
	}

	// Filter by room name
	if filter.RoomNameLike != nil {
		roomName := rp.getRoomNameWithSnapshot(ctx, snapshot, room.RoomID)
		if !strings.Contains(strings.ToLower(roomName), strings.ToLower(*filter.RoomNameLike)) {
			return false
		}
	}

	// Filter by encrypted status
	if filter.IsEncrypted != nil {
		isEncrypted := rp.isRoomEncryptedWithSnapshot(ctx, snapshot, room.RoomID)
		if isEncrypted != *filter.IsEncrypted {
			return false
		}
	}

	// Filter by invite status
	if filter.IsInvite != nil {
		isInvite := room.Membership == spec.Invite
		if isInvite != *filter.IsInvite {
			return false
		}
	}

	// Filter by room types
	if len(filter.RoomTypes) > 0 {
		roomType := rp.getRoomTypeWithSnapshot(ctx, snapshot, room.RoomID)
		if !contains(filter.RoomTypes, roomType) {
			return false
		}
	}

	// Filter out excluded room types
	if len(filter.NotRoomTypes) > 0 {
		roomType := rp.getRoomTypeWithSnapshot(ctx, snapshot, room.RoomID)
		if contains(filter.NotRoomTypes, roomType) {
			return false
		}
	}

	// Filter by tags (for favourites/low-priority/etc)
	if len(filter.Tags) > 0 {
		roomTags := rp.getRoomTags(ctx, room.RoomID, userID)
		hasMatchingTag := false
		for _, reqTag := range filter.Tags {
			if _, exists := roomTags[reqTag]; exists {
				hasMatchingTag = true
				break
			}
		}
		if !hasMatchingTag {
			return false
		}
	}

	// Filter out excluded tags
	if len(filter.NotTags) > 0 {
		roomTags := rp.getRoomTags(ctx, room.RoomID, userID)
		for _, excludeTag := range filter.NotTags {
			if _, exists := roomTags[excludeTag]; exists {
				return false
			}
		}
	}

	// Note: Spaces filtering check is done in ApplyRoomFilters before this function is called

	return true
}

// Helper functions for room properties

func (rp *RequestPool) isDirectMessage(ctx context.Context, roomID string, userID string) bool {
	// Query m.direct account data from userAPI
	var res userapi.QueryAccountDataResponse
	err := rp.userAPI.QueryAccountData(ctx, &userapi.QueryAccountDataRequest{
		UserID:   userID,
		RoomID:   "",        // Global account data
		DataType: "m.direct",
	}, &res)
	if err != nil || res.GlobalAccountData == nil {
		return false
	}

	// Get m.direct data from the map
	directData, ok := res.GlobalAccountData["m.direct"]
	if !ok {
		return false
	}

	// m.direct format: { "@user:domain": ["!roomid1", "!roomid2"] }
	var directRooms map[string][]string
	if err := json.Unmarshal(directData, &directRooms); err != nil {
		return false
	}

	// Check if this room is in any user's DM list
	for _, rooms := range directRooms {
		for _, dmRoomID := range rooms {
			if dmRoomID == roomID {
				return true
			}
		}
	}
	return false
}

// getRoomName creates its own snapshot - use getRoomNameWithSnapshot for batch operations
func (rp *RequestPool) getRoomName(ctx context.Context, roomID string) string {
	// Get a database snapshot
	snapshot, err := rp.db.NewDatabaseSnapshot(ctx)
	if err != nil {
		return ""
	}
	defer snapshot.Rollback()

	return rp.getRoomNameWithSnapshot(ctx, snapshot, roomID)
}

// getRoomNameWithSnapshot uses an existing snapshot for efficient batch operations
func (rp *RequestPool) getRoomNameWithSnapshot(ctx context.Context, snapshot storage.DatabaseTransaction, roomID string) string {
	// Query m.room.name state event
	event, err := snapshot.GetStateEvent(ctx, roomID, "m.room.name", "")
	if err != nil || event == nil {
		return ""
	}

	// Parse the name field from content
	var content struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(event.Content(), &content); err != nil {
		return ""
	}

	return content.Name
}

// isRoomEncrypted creates its own snapshot - use isRoomEncryptedWithSnapshot for batch operations
func (rp *RequestPool) isRoomEncrypted(ctx context.Context, roomID string) bool {
	// Get a database snapshot
	snapshot, err := rp.db.NewDatabaseSnapshot(ctx)
	if err != nil {
		return false
	}
	defer snapshot.Rollback()

	return rp.isRoomEncryptedWithSnapshot(ctx, snapshot, roomID)
}

// isRoomEncryptedWithSnapshot uses an existing snapshot for efficient batch operations
func (rp *RequestPool) isRoomEncryptedWithSnapshot(ctx context.Context, snapshot storage.DatabaseTransaction, roomID string) bool {
	// Check for m.room.encryption state event
	event, err := snapshot.GetStateEvent(ctx, roomID, "m.room.encryption", "")
	// If the event exists, the room is encrypted
	return err == nil && event != nil
}

// getRoomType creates its own snapshot - use getRoomTypeWithSnapshot for batch operations
func (rp *RequestPool) getRoomType(ctx context.Context, roomID string) string {
	// Get a database snapshot
	snapshot, err := rp.db.NewDatabaseSnapshot(ctx)
	if err != nil {
		logrus.WithError(err).Error("Failed to acquire database snapshot for room type")
		return ""
	}
	defer snapshot.Rollback()

	return rp.getRoomTypeWithSnapshot(ctx, snapshot, roomID)
}

// getRoomTypeWithSnapshot uses an existing snapshot for efficient batch operations
func (rp *RequestPool) getRoomTypeWithSnapshot(ctx context.Context, snapshot storage.DatabaseTransaction, roomID string) string {
	// Query m.room.create state event
	event, err := snapshot.GetStateEvent(ctx, roomID, "m.room.create", "")
	if err != nil || event == nil {
		// No create event or error - return empty string (regular room)
		return ""
	}

	// Parse the type field from content
	var content struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(event.Content(), &content); err != nil {
		logrus.WithError(err).Warn("Failed to parse m.room.create content for room type")
		return ""
	}

	return content.Type
}

func (rp *RequestPool) getRoomTags(ctx context.Context, roomID string, userID string) map[string]interface{} {
	// Query m.tag room account data from userAPI
	var res userapi.QueryAccountDataResponse
	err := rp.userAPI.QueryAccountData(ctx, &userapi.QueryAccountDataRequest{
		UserID:   userID,
		RoomID:   roomID,
		DataType: "m.tag",
	}, &res)
	if err != nil || res.RoomAccountData == nil {
		return make(map[string]interface{})
	}

	// Get m.tag data for this room from the nested map
	roomData, ok := res.RoomAccountData[roomID]
	if !ok {
		return make(map[string]interface{})
	}

	tagData, ok := roomData["m.tag"]
	if !ok {
		return make(map[string]interface{})
	}

	// m.tag format: { "tags": { "m.favourite": {...}, "u.custom": {...} } }
	var parsed struct {
		Tags map[string]interface{} `json:"tags"`
	}
	if err := json.Unmarshal(tagData, &parsed); err != nil {
		return make(map[string]interface{})
	}

	return parsed.Tags
}

// SortRoomsByActivity sorts rooms by their bump stamp (most recent first)
func SortRoomsByActivity(rooms []RoomWithBumpStamp) {
	sort.Slice(rooms, func(i, j int) bool {
		// Sort in descending order (most recent first)
		return rooms[i].BumpStamp > rooms[j].BumpStamp
	})
}

// ApplySlidingWindow extracts the requested range from a sorted room list
func ApplySlidingWindow(rooms []RoomWithBumpStamp, rangeSpec []int) []RoomWithBumpStamp {
	if len(rangeSpec) != 2 {
		// Invalid range, return all rooms
		return rooms
	}

	start := rangeSpec[0]
	end := rangeSpec[1]

	// Clamp to valid bounds
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if end >= len(rooms) {
		end = len(rooms) - 1
	}

	// Return empty if out of bounds
	if start >= len(rooms) {
		return []RoomWithBumpStamp{}
	}

	// Extract slice (end is inclusive in MSC4186)
	return rooms[start : end+1]
}

// GenerateSyncOperation creates a SYNC operation for the initial response
// Phase 2 focuses on SYNC operations; phases 3+ will add INSERT/DELETE/INVALIDATE
func GenerateSyncOperation(rooms []RoomWithBumpStamp, rangeSpec []int) types.SlidingOperation {
	roomIDs := make([]string, len(rooms))
	for i, room := range rooms {
		roomIDs[i] = room.RoomID
	}

	return types.SlidingOperation{
		Op:      "SYNC",
		Range:   rangeSpec,
		RoomIDs: roomIDs,
	}
}

// Helper function
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
