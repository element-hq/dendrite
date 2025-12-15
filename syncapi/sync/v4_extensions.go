// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package sync

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/element-hq/dendrite/syncapi/internal"
	"github.com/element-hq/dendrite/syncapi/storage"
	"github.com/element-hq/dendrite/syncapi/streams"
	"github.com/element-hq/dendrite/syncapi/synctypes"
	"github.com/element-hq/dendrite/syncapi/types"
	userapi "github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/sirupsen/logrus"
)

// findRelevantRoomIDsForExtension handles the reserved `lists`/`rooms` keys for extensions.
// Extensions should only return results for rooms in the Sliding Sync response. This matches up
// the requested rooms/lists with the actual lists/rooms in the Sliding Sync response.
//
// Behavior (MSC3959, MSC3960, MSC3961):
//   nil (omitted)              // Default: Process all rooms (wildcard behavior)
//   {"lists": []}              // Explicitly process no lists
//   {"lists": ["rooms", "dms"]} // Process only specified lists
//   {"lists": ["*"]}           // Process all lists (explicit wildcard)
//   {"rooms": []}              // Explicitly process no room subscriptions
//   {"rooms": ["!a:b", "!c:d"]} // Process only specified room subscriptions
//   {"rooms": ["*"]}           // Process all room subscriptions (explicit wildcard)
//
// Args:
//   requestedLists: The `lists` from the extension request (nil = default wildcard)
//   requestedRooms: The `rooms` from the extension request (nil = default wildcard)
//   actualLists: The actual lists from the Sliding Sync response
//   actualRoomSubscriptions: The actual room subscriptions from the Sliding Sync request
//
// Returns: Set of room IDs to process for this extension
func findRelevantRoomIDsForExtension(
	requestedLists []string,
	requestedRooms []string,
	actualLists map[string]types.SlidingList,
	actualRoomSubscriptions map[string]bool,
) map[string]bool {
	relevantRoomIDs := make(map[string]bool)

	// Handle rooms parameter
	if requestedRooms != nil {
		// Explicitly provided (could be empty array)
		if len(requestedRooms) == 0 {
			// Empty array [] = explicitly process no rooms
			// Continue to check lists parameter
		} else {
			for _, roomID := range requestedRooms {
				// Wildcard means process all room subscriptions
				if roomID == "*" {
					for roomID := range actualRoomSubscriptions {
						relevantRoomIDs[roomID] = true
					}
					break
				}

				// Specific room - only include if in actual subscriptions
				if actualRoomSubscriptions[roomID] {
					relevantRoomIDs[roomID] = true
				}
			}
		}
	} else {
		// nil (omitted) = default to wildcard behavior (all room subscriptions)
		for roomID := range actualRoomSubscriptions {
			relevantRoomIDs[roomID] = true
		}
	}

	// Handle lists parameter
	if requestedLists != nil {
		// Explicitly provided (could be empty array)
		if len(requestedLists) == 0 {
			// Empty array [] = explicitly process no lists
			// relevantRoomIDs already populated from rooms parameter above
		} else {
			for _, listKey := range requestedLists {
				// Wildcard means process all lists
				if listKey == "*" {
					for _, list := range actualLists {
						// Extract room IDs from all operations (typically one SYNC op)
						for _, op := range list.Ops {
							for _, roomID := range op.RoomIDs {
								relevantRoomIDs[roomID] = true
							}
						}
					}
					break
				}

				// Specific list - only include if it exists
				if list, exists := actualLists[listKey]; exists {
					for _, op := range list.Ops {
						for _, roomID := range op.RoomIDs {
							relevantRoomIDs[roomID] = true
						}
					}
				}
			}
		}
	} else {
		// nil (omitted) = default to wildcard behavior (all lists)
		for _, list := range actualLists {
			for _, op := range list.Ops {
				for _, roomID := range op.RoomIDs {
					relevantRoomIDs[roomID] = true
				}
			}
		}
	}

	return relevantRoomIDs
}

// ProcessExtensions handles all extension requests and populates the response
// Phase 9: Implements to_device, e2ee (MSC3884), account_data, receipts, typing extensions
//
// Reference: /tmp/phase9_plan.md, /tmp/msc3884_research.md, /tmp/matrix_js_sdk_findings.md
//
// Processing order (from matrix-js-sdk):
//   - PreProcess: to_device, e2ee (before room data processing)
//   - PostProcess: account_data, receipts, typing (after room data processing)
//
// For now, we process all extensions together. Future optimization: split by PreProcess/PostProcess.
//
// responseLists: The actual lists from the sliding sync response (for extension filtering)
// roomSubscriptions: The actual room subscriptions from the sliding sync request (for extension filtering)
func (rp *RequestPool) ProcessExtensions(
	ctx context.Context,
	snapshot storage.DatabaseTransaction,
	req *types.SlidingSyncRequest,
	userID string,
	deviceID string,
	connectionKey int64, // For per-connection extension state (e.g., receipts)
	fromPos *types.StreamingToken, // nil for initial sync
	toPos types.StreamingToken,
	responseLists map[string]types.SlidingList, // Actual lists in the response
	roomSubscriptions map[string]bool, // Actual room subscriptions from request
) (*types.ExtensionResponse, types.StreamingToken, []types.OutputReceiptEvent, error) {

	// Return empty response if no extensions requested
	if req.Extensions == nil {
		return &types.ExtensionResponse{}, toPos, nil, nil
	}

	resp := &types.ExtensionResponse{}
	isInitialSync := (fromPos == nil)

	// Track updated positions from extensions (like v3 sync stream providers)
	updatedPos := toPos

	// Process to_device extension (PreProcess)
	if req.Extensions.ToDevice != nil && req.Extensions.ToDevice.Enabled {
		toDeviceResp, err := rp.processToDeviceExtension(ctx, snapshot, userID, deviceID, req.Extensions.ToDevice, fromPos, toPos)
		if err != nil {
			logrus.WithError(err).Error("Failed to process to_device extension")
			// Continue anyway - extensions are optional
		} else {
			resp.ToDevice = toDeviceResp
		}
	}

	// Process e2ee extension (PreProcess, MSC3884)
	if req.Extensions.E2EE != nil && req.Extensions.E2EE.Enabled {
		e2eeResp, err := rp.processE2EEExtension(ctx, snapshot, userID, deviceID, isInitialSync, fromPos, toPos)
		if err != nil {
			logrus.WithError(err).Error("Failed to process e2ee extension")
			// Continue anyway - extensions are optional
		} else {
			resp.E2EE = e2eeResp
		}
	}

	// Process account_data extension (PostProcess)
	if req.Extensions.AccountData != nil && req.Extensions.AccountData.Enabled {
		accountDataResp, accountDataLastPos, err := rp.processAccountDataExtension(ctx, snapshot, userID, req.Extensions.AccountData.Lists, req.Extensions.AccountData.Rooms, fromPos, toPos, responseLists, roomSubscriptions)
		if err != nil {
			logrus.WithError(err).Error("Failed to process account_data extension")
			// Return empty response on error - clients expect the field to be present with proper structure
			resp.AccountData = &types.AccountDataResponse{
				Global: []synctypes.ClientEvent{},
				Rooms:  make(map[string][]synctypes.ClientEvent),
			}
		} else {
			resp.AccountData = accountDataResp
			// Update account data position based on what was actually returned (v4 sync fix)
			// This ensures the position token matches the account data delivered to the client
			logrus.WithFields(logrus.Fields{
				"accountDataLastPos":              accountDataLastPos,
				"updatedPos.AccountDataPosition": updatedPos.AccountDataPosition,
				"toPos.AccountDataPosition":       toPos.AccountDataPosition,
			}).Debug("[ACCOUNT_DATA] Checking position update")
			if accountDataLastPos > updatedPos.AccountDataPosition {
				updatedPos.AccountDataPosition = accountDataLastPos
				logrus.WithFields(logrus.Fields{
					"old_pos": toPos.AccountDataPosition,
					"new_pos": accountDataLastPos,
				}).Info("[ACCOUNT_DATA] Updated account data position from extension")
			}
		}
	}

	// Track delivered receipts for connection state update in write transaction
	var deliveredReceipts []types.OutputReceiptEvent

	// Process receipts extension (PostProcess)
	if req.Extensions.Receipts != nil && req.Extensions.Receipts.Enabled {
		receiptsResp, receiptsLastPos, receiptsDelivered, err := rp.processReceiptsExtension(ctx, snapshot, connectionKey, userID, req.Extensions.Receipts.Lists, req.Extensions.Receipts.Rooms, fromPos, toPos, responseLists, roomSubscriptions)
		if err != nil {
			logrus.WithError(err).Error("Failed to process receipts extension")
			// Return empty response on error - clients expect the field to be present
			resp.Receipts = &types.ReceiptsResponse{Rooms: make(map[string]synctypes.ClientEvent)}
		} else {
			resp.Receipts = receiptsResp
			deliveredReceipts = receiptsDelivered
			// Update receipt position based on what was actually returned
			logrus.WithFields(logrus.Fields{
				"receiptsLastPos":           receiptsLastPos,
				"updatedPos.ReceiptPosition": updatedPos.ReceiptPosition,
				"toPos.ReceiptPosition":      toPos.ReceiptPosition,
			}).Debug("[RECEIPTS] Checking position update")
			if receiptsLastPos > updatedPos.ReceiptPosition {
				updatedPos.ReceiptPosition = receiptsLastPos
				logrus.WithFields(logrus.Fields{
					"old_pos":      toPos.ReceiptPosition,
					"receipts_pos": receiptsLastPos,
					"new_pos":      receiptsLastPos,
				}).Info("[RECEIPTS] Updated receipt position from extension")
			}
		}
	}

	// Process typing extension (PostProcess)
	if req.Extensions.Typing != nil && req.Extensions.Typing.Enabled {
		typingResp, err := rp.processTypingExtension(ctx, snapshot, userID, req.Extensions.Typing.Lists, req.Extensions.Typing.Rooms, fromPos, toPos, responseLists, roomSubscriptions)
		if err != nil {
			logrus.WithError(err).Error("Failed to process typing extension")
			// Return empty response on error - clients expect the field to be present
			resp.Typing = &types.TypingResponse{Rooms: make(map[string]synctypes.ClientEvent)}
		} else {
			resp.Typing = typingResp
		}
	}

	return resp, updatedPos, deliveredReceipts, nil
}

// processToDeviceExtension handles to-device message extension
// Implements stateful tracking with since/next_batch tokens
//
// IMPORTANT: to_device uses its own stateful token (req.Since) separate from
// the main sliding sync position. The client tracks this token independently.
func (rp *RequestPool) processToDeviceExtension(
	ctx context.Context,
	snapshot storage.DatabaseTransaction,
	userID string,
	deviceID string,
	req *types.ToDeviceRequest,
	fromPos *types.StreamingToken,
	toPos types.StreamingToken,
) (*types.V4ToDeviceResponse, error) {
	// Parse the to_device-specific "since" token from request
	// This is separate from the main sliding sync position
	var from types.StreamPosition
	if req.Since != "" {
		// Parse the since token as a stream position
		sincePos, err := types.NewStreamPositionFromString(req.Since)
		if err != nil {
			// Invalid token - start from 0
			logrus.WithError(err).Warn("Invalid to_device since token, starting from 0")
			from = 0
		} else {
			from = sincePos
		}
	} else {
		// No since token provided - use the main sliding sync position
		// For initial sync, start from 0; for incremental, use fromPos
		if fromPos != nil {
			from = fromPos.SendToDevicePosition
		} else {
			from = 0
		}
	}

	// Get to-device messages from database
	lastPos, events, err := snapshot.SendToDeviceUpdatesForSync(
		ctx, userID, deviceID, from, toPos.SendToDevicePosition,
	)
	if err != nil {
		return nil, fmt.Errorf("SendToDeviceUpdatesForSync failed: %w", err)
	}

	// Apply limit (default 100 as per spec)
	limit := req.Limit
	if limit == 0 {
		limit = 100
	}

	// Truncate events if over limit
	clientEvents := make([]gomatrixserverlib.SendToDeviceEvent, 0, len(events))
	for i, event := range events {
		if i >= limit {
			break
		}
		clientEvents = append(clientEvents, event.SendToDeviceEvent)
	}

	// Return next_batch token
	// If we hit the limit, the client should use this token to paginate
	// Otherwise, the client has caught up
	return &types.V4ToDeviceResponse{
		NextBatch: fmt.Sprintf("%d", lastPos),
		Events:    clientEvents,
	}, nil
}

// processE2EEExtension handles E2EE device extension (MSC3884)
//
// CRITICAL requirements from research:
// - Initial sync: device_lists must be OMITTED (not nil, omitted entirely)
// - Initial sync: MUST include {"signed_curve25519": 0} for Android compatibility
// - Initial sync: device_unused_fallback_key_types returns empty array []
// - Incremental sync: device_lists includes changed/left users
//
// Reference: /tmp/msc3884_research.md lines 89-93, /tmp/matrix_js_sdk_findings.md lines 86-99
func (rp *RequestPool) processE2EEExtension(
	ctx context.Context,
	snapshot storage.DatabaseTransaction,
	userID string,
	deviceID string,
	isInitialSync bool,
	fromPos *types.StreamingToken,
	toPos types.StreamingToken,
) (*types.E2EEResponse, error) {
	resp := &types.E2EEResponse{
		// CRITICAL: Android compatibility - always include signed_curve25519: 0
		// This will be overwritten if we actually have keys, but ensures the field is present
		DeviceOneTimeKeysCount: map[string]int{"signed_curve25519": 0},
		DeviceUnusedFallbackKeyTypes:       []string{},
		DeviceUnusedFallbackKeyTypesLegacy: []string{},
		// DeviceLists intentionally nil for initial sync (will be omitted in JSON)
	}

	// Get OTK counts and fallback key types
	var queryRes userapi.QueryOneTimeKeysResponse
	err := rp.userAPI.QueryOneTimeKeys(ctx, &userapi.QueryOneTimeKeysRequest{
		UserID:   userID,
		DeviceID: deviceID,
	}, &queryRes)
	if err != nil || queryRes.Error != nil {
		logrus.WithError(err).Error("QueryOneTimeKeys failed")
		// Continue anyway - return with defaults
	} else {
		// Use the actual key counts
		if queryRes.Count.KeyCount != nil {
			resp.DeviceOneTimeKeysCount = queryRes.Count.KeyCount
			// Ensure signed_curve25519 is always present for Android compatibility
			if _, ok := resp.DeviceOneTimeKeysCount["signed_curve25519"]; !ok {
				resp.DeviceOneTimeKeysCount["signed_curve25519"] = 0
			}
		}
		// Set fallback key types (both new and legacy fields)
		// Ensure we never set nil - use empty slice if no fallback keys
		if queryRes.UnusedFallbackAlgorithms != nil {
			resp.DeviceUnusedFallbackKeyTypes = queryRes.UnusedFallbackAlgorithms
			resp.DeviceUnusedFallbackKeyTypesLegacy = queryRes.UnusedFallbackAlgorithms
		}
		// If nil, keep the empty slice we initialized above
	}

	// For incremental sync, get device list changes
	// For initial sync, device_lists MUST be omitted (left as nil)
	if !isInitialSync && fromPos != nil {
		// Only call DeviceListCatchup if the position has actually changed
		// If positions are the same, there are no device list changes
		if fromPos.DeviceListPosition != toPos.DeviceListPosition {
			// Create a minimal v3 Response to use with DeviceListCatchup
			tempResponse := &types.Response{
				DeviceLists: &types.DeviceLists{
					Changed: []string{},
					Left:    []string{},
				},
				// Need to populate Rooms.Join for DeviceListCatchup to detect newly joined rooms
				Rooms: &types.RoomsResponse{
					Join:   make(map[string]*types.JoinResponse),
					Invite: make(map[string]*types.InviteResponse),
					Leave:  make(map[string]*types.LeaveResponse),
				},
			}

			// Call DeviceListCatchup to get device list changes
			_, _, err := internal.DeviceListCatchup(
				ctx, snapshot, rp.userAPI, rp.rsAPI,
				userID, tempResponse,
				fromPos.DeviceListPosition, toPos.DeviceListPosition,
			)
			if err != nil {
				logrus.WithError(err).Error("DeviceListCatchup failed")
				// Continue anyway - device lists are optional
				// IMPORTANT: Still set DeviceLists even on error (Synapse always includes it)
				resp.DeviceLists = &types.DeviceLists{
					Changed: []string{},
					Left:    []string{},
				}
			} else {
				// CRITICAL: Always set DeviceLists for incremental sync, even if empty
				// Synapse always includes device_lists field with empty arrays when no changes
				// Clients expect this field to be present to distinguish "no changes" from "not tracking"
				resp.DeviceLists = tempResponse.DeviceLists
			}
		}
	}

	// Always return e2ee extension if requested
	// Synapse behavior: always returns OTK counts and device lists (even if empty arrays)
	return resp, nil
}

// processAccountDataExtension handles account data extension
func (rp *RequestPool) processAccountDataExtension(
	ctx context.Context,
	snapshot storage.DatabaseTransaction,
	userID string,
	requestedLists []string, // Optional list filter from request (MSC3959)
	requestedRooms []string, // Optional room filter from request (MSC3960)
	fromPos *types.StreamingToken,
	toPos types.StreamingToken,
	actualLists map[string]types.SlidingList, // Actual lists in response
	actualRoomSubscriptions map[string]bool, // Actual room subscriptions from request
) (*types.AccountDataResponse, types.StreamPosition, error) {
	// Get the "from" position for incremental sync
	var from types.StreamPosition
	if fromPos != nil {
		from = fromPos.AccountDataPosition
	}

	// Create range for account data query
	r := types.Range{
		From: from,
		To:   toPos.AccountDataPosition,
	}

	logrus.WithFields(logrus.Fields{
		"user_id": userID,
		"from":    from,
		"to":      toPos.AccountDataPosition,
		"is_initial": fromPos == nil,
	}).Debug("[ACCOUNT_DATA] Querying account data range")

	// Get account data changes in this range
	// Use filter with high limit to get all account data (typically < 100 events)
	filter := synctypes.EventFilter{
		Limit: 1000, // High limit to get all account data
	}
	dataTypes, lastPos, err := snapshot.GetAccountDataInRange(ctx, userID, r, &filter)
	if err != nil {
		return nil, 0, fmt.Errorf("GetAccountDataInRange failed: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"num_rooms": len(dataTypes),
		"data_types": dataTypes,
	}).Debug("[ACCOUNT_DATA] Got account data types")

	// Create response
	resp := &types.AccountDataResponse{
		Global: []synctypes.ClientEvent{},
		Rooms:  make(map[string][]synctypes.ClientEvent),
	}

	// Determine which rooms to process using unified helper (MSC3959/MSC3960)
	relevantRoomIDs := findRelevantRoomIDsForExtension(
		requestedLists,
		requestedRooms,
		actualLists,
		actualRoomSubscriptions,
	)

	logrus.WithFields(logrus.Fields{
		"requested_lists": requestedLists,
		"requested_rooms": requestedRooms,
		"relevant_rooms": len(relevantRoomIDs),
	}).Debug("[ACCOUNT_DATA] Filtered rooms for extension")

	// Iterate over rooms and data types
	for roomID, dataTypeList := range dataTypes {
		// Skip rooms not in the relevant set (unless it's global account data)
		if roomID != "" && !relevantRoomIDs[roomID] {
			continue
		}

		// Query each data type from userAPI
		for _, dataType := range dataTypeList {
			dataReq := userapi.QueryAccountDataRequest{
				UserID:   userID,
				RoomID:   roomID,
				DataType: dataType,
			}
			dataRes := userapi.QueryAccountDataResponse{}
			err = rp.userAPI.QueryAccountData(ctx, &dataReq, &dataRes)
			if err != nil {
				logrus.WithError(err).Error("QueryAccountData failed")
				continue
			}

			// Separate global vs per-room account data
			if roomID == "" {
				// Global account data
				if globalData, ok := dataRes.GlobalAccountData[dataType]; ok {
					resp.Global = append(resp.Global, synctypes.ClientEvent{
						Type:    dataType,
						Content: spec.RawJSON(globalData),
					})
				}
			} else {
				// Per-room account data
				if roomData, ok := dataRes.RoomAccountData[roomID][dataType]; ok {
					if resp.Rooms[roomID] == nil {
						resp.Rooms[roomID] = []synctypes.ClientEvent{}
					}
					resp.Rooms[roomID] = append(resp.Rooms[roomID], synctypes.ClientEvent{
						Type:    dataType,
						Content: spec.RawJSON(roomData),
					})
				}
			}
		}
	}

	// Always return account_data extension if requested
	// Synapse may return empty account_data objects
	logrus.WithFields(logrus.Fields{
		"num_global": len(resp.Global),
		"num_room_keys": len(resp.Rooms),
	}).Debug("[ACCOUNT_DATA] Returning account data response")

	// Return lastPos so v4 sync can update the account data position in the response token
	return resp, lastPos, nil
}

// processReceiptsExtension handles read receipts extension
// IMPORTANT: Response contains a SINGLE event per room, not an array (matrix-js-sdk expects this)
func (rp *RequestPool) processReceiptsExtension(
	ctx context.Context,
	snapshot storage.DatabaseTransaction,
	connectionKey int64, // For per-connection receipt tracking
	userID string,
	requestedLists []string, // Optional list filter from request (MSC3959)
	requestedRooms []string, // Optional room filter from request (MSC3960)
	fromPos *types.StreamingToken,
	toPos types.StreamingToken,
	actualLists map[string]types.SlidingList, // Actual lists in response
	actualRoomSubscriptions map[string]bool, // Actual room subscriptions from request
) (*types.ReceiptsResponse, types.StreamPosition, []types.OutputReceiptEvent, error) {
	// Determine which rooms to process using unified helper (MSC3959/MSC3960)
	relevantRoomIDs := findRelevantRoomIDsForExtension(
		requestedLists,
		requestedRooms,
		actualLists,
		actualRoomSubscriptions,
	)

	// Convert to slice for database query
	roomsToCheck := make([]string, 0, len(relevantRoomIDs))
	for roomID := range relevantRoomIDs {
		roomsToCheck = append(roomsToCheck, roomID)
	}

	logrus.WithFields(logrus.Fields{
		"connection_key":  connectionKey,
		"requested_lists": requestedLists,
		"requested_rooms": requestedRooms,
		"relevant_rooms":  len(roomsToCheck),
	}).Debug("[RECEIPTS] Filtered rooms for extension")

	logrus.WithFields(logrus.Fields{
		"user_id":        userID,
		"connection_key": connectionKey,
		"num_rooms":      len(roomsToCheck),
	}).Info("[RECEIPTS] Querying receipts for connection")

	// CRITICAL FIX: Query receipts using a fresh transaction instead of the snapshot
	// The snapshot uses REPEATABLE READ isolation which may not see recently committed receipts
	// This caused the stuck badge bug where long-polling connections received stale receipt data
	// Use a fresh READ COMMITTED transaction to ensure we see the latest receipts
	freshSnapshot, err := rp.db.NewDatabaseSnapshot(ctx)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("failed to create fresh snapshot for receipts: %w", err)
	}
	defer freshSnapshot.Rollback()

	// NEW APPROACH: Use per-connection event-ID based tracking instead of position-based
	// This prevents duplicate receipts across concurrent connections (room-list vs encryption)
	receipts, err := freshSnapshot.SelectLatestUserReceiptsForConnection(ctx, connectionKey, roomsToCheck, userID)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("SelectLatestUserReceiptsForConnection failed: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"user_id":        userID,
		"connection_key": connectionKey,
		"receipts_count": len(receipts),
	}).Info("[RECEIPTS] New receipts to deliver")

	// Keep track of all receipts we're delivering for connection state update later
	var deliveredReceipts []types.OutputReceiptEvent

	// Log each receipt for debugging
	for _, receipt := range receipts {
		logrus.WithFields(logrus.Fields{
			"room_id":  receipt.RoomID,
			"type":     receipt.Type,
			"user_id":  receipt.UserID,
			"event_id": receipt.EventID,
		}).Debug("[RECEIPTS] Delivering receipt")
	}

	// Group receipts by room (same logic as before)
	receiptsByRoom := make(map[string][]types.OutputReceiptEvent)
	for _, receipt := range receipts {
		// Don't send private read receipts to other users
		if receipt.Type == "m.read.private" && userID != receipt.UserID {
			continue
		}
		receiptsByRoom[receipt.RoomID] = append(receiptsByRoom[receipt.RoomID], receipt)
	}

	// Create response with single event per room
	resp := &types.ReceiptsResponse{
		Rooms: make(map[string]synctypes.ClientEvent),
	}

	for roomID, roomReceipts := range receiptsByRoom {
		ev := synctypes.ClientEvent{
			Type: "m.receipt",
		}
		// Structure: eventID -> receiptType -> userID -> {ts}
		// This allows m.read and m.read.private to be serialized separately
		content := make(map[string]map[string]map[string]ReceiptTS)
		for _, receipt := range roomReceipts {
			eventContent, ok := content[receipt.EventID]
			if !ok {
				eventContent = make(map[string]map[string]ReceiptTS)
				content[receipt.EventID] = eventContent
			}
			typeContent, ok := eventContent[receipt.Type]
			if !ok {
				typeContent = make(map[string]ReceiptTS)
				eventContent[receipt.Type] = typeContent
			}
			typeContent[receipt.UserID] = ReceiptTS{TS: receipt.Timestamp}

			// Collect this receipt for connection state update (will be done in write transaction later)
			deliveredReceipts = append(deliveredReceipts, receipt)
		}
		ev.Content, err = json.Marshal(content)
		if err != nil {
			logrus.WithError(err).Error("Failed to marshal receipt content")
			continue
		}

		resp.Rooms[roomID] = ev
	}

	logrus.WithFields(logrus.Fields{
		"connection_key":      connectionKey,
		"num_rooms":           len(resp.Rooms),
		"delivered_count":     len(deliveredReceipts),
		"user_id":             userID,
	}).Info("[RECEIPTS] Returning receipts response")

	// Return 0 for lastPos since we no longer track position for receipts
	// Receipts are tracked per-connection via event IDs in separate table
	// Return deliveredReceipts so caller can update connection state in write transaction
	return resp, 0, deliveredReceipts, nil
}

// ReceiptMRead represents the m.read structure for receipts
type ReceiptMRead struct {
	User map[string]ReceiptTS `json:"m.read"`
}

// ReceiptTS represents a receipt timestamp
type ReceiptTS struct {
	TS spec.Timestamp `json:"ts"`
}

// processTypingExtension handles typing notifications extension
// IMPORTANT: Response contains a SINGLE event per room, not an array (matrix-js-sdk expects this)
func (rp *RequestPool) processTypingExtension(
	ctx context.Context,
	snapshot storage.DatabaseTransaction,
	userID string,
	requestedLists []string, // Optional list filter from request (MSC3959)
	requestedRooms []string, // Optional room filter from request (MSC3960)
	fromPos *types.StreamingToken,
	toPos types.StreamingToken,
	actualLists map[string]types.SlidingList, // Actual lists in response
	actualRoomSubscriptions map[string]bool, // Actual room subscriptions from request
) (*types.TypingResponse, error) {
	// Determine which rooms to process using unified helper (MSC3959/MSC3960)
	relevantRoomIDs := findRelevantRoomIDsForExtension(
		requestedLists,
		requestedRooms,
		actualLists,
		actualRoomSubscriptions,
	)

	// Convert to slice for typing stream query
	roomsToCheck := make([]string, 0, len(relevantRoomIDs))
	for roomID := range relevantRoomIDs {
		roomsToCheck = append(roomsToCheck, roomID)
	}

	logrus.WithFields(logrus.Fields{
		"requested_lists": requestedLists,
		"requested_rooms": requestedRooms,
		"relevant_rooms": len(roomsToCheck),
	}).Debug("[TYPING] Filtered rooms for extension")

	// Get the "from" position for incremental sync
	var from types.StreamPosition
	if fromPos != nil {
		from = fromPos.TypingPosition
	}

	// Access the typing stream provider's EDUCache
	// Cast to TypingStreamProvider to access the EDUCache
	typingProvider, ok := rp.streams.TypingStreamProvider.(*streams.TypingStreamProvider)
	if !ok {
		return nil, fmt.Errorf("failed to cast TypingStreamProvider")
	}

	// Create response
	resp := &types.TypingResponse{
		Rooms: make(map[string]synctypes.ClientEvent),
	}

	// Check each room for typing updates
	for _, roomID := range roomsToCheck {
		users, updated := typingProvider.EDUCache.GetTypingUsersIfUpdatedAfter(roomID, int64(from))
		if !updated {
			continue // No typing updates for this room
		}

		// Create typing event for this room
		ev := synctypes.ClientEvent{
			Type: "m.typing",
		}

		// Marshal typing user IDs into content
		var err error
		ev.Content, err = json.Marshal(map[string]interface{}{
			"user_ids": users,
		})
		if err != nil {
			logrus.WithError(err).Error("Failed to marshal typing content")
			continue
		}

		resp.Rooms[roomID] = ev
	}

	// Always return typing extension if requested
	// Synapse may return empty typing objects
	return resp, nil
}
