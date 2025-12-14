// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
	"github.com/sirupsen/logrus"

	"github.com/element-hq/dendrite/internal"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/syncapi/storage"
	"github.com/element-hq/dendrite/syncapi/types"
	userapi "github.com/element-hq/dendrite/userapi/api"
)

// mustAwaitFullState checks if a required_state configuration requires full room state.
// Returns true if the subscription should be skipped for partial state rooms.
// Per MSC3706/Synapse: partial state rooms can only be subscribed to if the requested
// state can be satisfied from the local server's perspective.
func mustAwaitFullState(requiredState types.RequiredStateConfig, cfg *config.Global) bool {
	for _, tuple := range requiredState.Include {
		if len(tuple) < 2 {
			continue
		}
		stateType, stateKey := tuple[0], tuple[1]

		// Wildcard state type requests require full state
		if stateType == "*" {
			return true
		}

		// For m.room.member events, check if we need remote user memberships
		if stateType == "m.room.member" {
			// Wildcard member requests require full state
			if stateKey == "*" {
				return true
			}
			// $LAZY and $ME are special - can be satisfied locally
			if stateKey == "$LAZY" || stateKey == "$ME" {
				continue
			}
			// Check if this is a remote user
			if strings.HasPrefix(stateKey, "@") && strings.Contains(stateKey, ":") {
				// Extract server name from user ID
				parts := strings.SplitN(stateKey, ":", 2)
				if len(parts) == 2 {
					serverName := spec.ServerName(parts[1])
					if !cfg.IsLocalServerName(serverName) {
						// Remote user membership request requires full state
						return true
					}
				}
			}
		}
	}
	return false
}

// V4ConnectionState tracks per-connection state for sliding sync
// Phase 10: Stream-based delta tracking
type V4ConnectionState struct {
	// Database connection key (stable identifier)
	ConnectionKey int64
	// Connection position for THIS response (created at start of request)
	ConnectionPosition int64
	// Stream states from previous syncs (for delta computation)
	// map[roomID]map[stream]*StreamState
	PreviousStreamStates map[string]map[string]*types.SlidingSyncStreamState
}

// determineRoomStreamState determines the RoomStreamState for a room based on connection state
// This is used to drive incremental sync behavior (initial vs live vs previously)
// CRITICAL: Detects membership transitions (like v3 sync's NewlyJoined) to properly handle
// rejoin scenarios where a user left/was kicked and then rejoined
func determineRoomStreamState(
	ctx context.Context,
	snapshot storage.DatabaseTransaction,
	connState *V4ConnectionState,
	roomID string,
	userID string,
) types.RoomStreamState {
	if connState == nil || connState.PreviousStreamStates == nil {
		logrus.WithField("room_id", roomID).Debug("[V4_STATE_DEBUG] connState is nil or no PreviousStreamStates")
		return types.RoomStreamState{
			Status:    types.HaveSentRoomNever,
			LastToken: nil,
		}
	}

	var previousState *types.SlidingSyncStreamState
	if connState.PreviousStreamStates[roomID] != nil {
		previousState = connState.PreviousStreamStates[roomID]["events"]
	}

	if previousState == nil {
		// Room has never been sent on this connection
		logrus.WithField("room_id", roomID).Debug("[V4_STATE_DEBUG] No previous state found for room - status=NEVER")
		return types.RoomStreamState{
			Status:    types.HaveSentRoomNever,
			LastToken: nil,
		}
	}

	// Room was sent before - parse the last token
	var lastToken *types.StreamingToken
	if previousState.LastToken != "" {
		parsedToken, err := types.NewStreamTokenFromString(previousState.LastToken)
		if err != nil {
			logrus.WithError(err).WithField("room_id", roomID).Warn("[V4_STATE_DEBUG] Failed to parse LastToken, treating as initial")
			return types.RoomStreamState{
				Status:    types.HaveSentRoomNever,
				LastToken: nil,
			}
		}
		lastToken = &parsedToken
	}

	if lastToken == nil {
		logrus.WithFields(logrus.Fields{
			"room_id":     roomID,
			"room_status": previousState.RoomStatus,
			"last_token":  previousState.LastToken,
		}).Debug("[V4_STATE_DEBUG] LastToken is nil after parsing - status=NEVER")
		return types.RoomStreamState{
			Status:    types.HaveSentRoomNever,
			LastToken: nil,
		}
	}

	// CRITICAL FIX: Check for membership transitions (like v3 sync's NewlyJoined detection)
	// If the user has transitioned TO join from a non-join state, treat as newly joined
	// This handles kick→rejoin, leave→rejoin, ban→unban+join, invite→join scenarios

	// First query the current membership (at the latest position)
	currentMembership, _, err := snapshot.SelectMembershipForUser(
		ctx, roomID, userID, math.MaxInt64,
	)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"room_id": roomID,
			"user_id": userID,
		}).Warn("[V4_STATE_DEBUG] Failed to query current membership, treating as incremental")
		// On error, fall through to normal LIVE/PREVIOUSLY logic
	} else if currentMembership == spec.Join {
		// User is currently joined - check if this is a transition from non-join
		// Query their membership at the last sync position to detect transitions
		// Use lastToken.PDUPosition as the topological position cutoff
		// SelectMembershipForUser returns the membership at or before that position
		prevMembership, _, err := snapshot.SelectMembershipForUser(
			ctx, roomID, userID, int64(lastToken.PDUPosition),
		)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"room_id":  roomID,
				"user_id":  userID,
				"position": lastToken.PDUPosition,
			}).Warn("[V4_STATE_DEBUG] Failed to query previous membership, treating as incremental")
			// On error, fall through to normal LIVE/PREVIOUSLY logic
		} else if prevMembership != spec.Join {
			// Membership transition detected: non-join → join
			// This is a "newly joined" room - treat as initial regardless of previous connection state
			logrus.WithFields(logrus.Fields{
				"room_id":            roomID,
				"prev_membership":    prevMembership,
				"current_membership": currentMembership,
				"last_position":      lastToken.PDUPosition,
			}).Info("[V4_STATE_DEBUG] Membership transition detected (rejoin) - status=NEVER")
			return types.RoomStreamState{
				Status:    types.HaveSentRoomNever,
				LastToken: nil, // Nil token to trigger full state/timeline fetch
			}
		}
		// else: prevMembership == join, so this is a continuing join (not a transition)
	}

	// No membership transition detected - determine if LIVE or PREVIOUSLY based on room_status
	if previousState.RoomStatus == types.HaveSentRoomLive.String() {
		logrus.WithFields(logrus.Fields{
			"room_id":     roomID,
			"room_status": previousState.RoomStatus,
			"last_token":  previousState.LastToken,
		}).Debug("[V4_STATE_DEBUG] Room status=LIVE from database")
		return types.RoomStreamState{
			Status:    types.HaveSentRoomLive,
			LastToken: lastToken,
		}
	}

	logrus.WithFields(logrus.Fields{
		"room_id":     roomID,
		"room_status": previousState.RoomStatus,
		"last_token":  previousState.LastToken,
	}).Debug("[V4_STATE_DEBUG] Room status=PREVIOUSLY from database")
	return types.RoomStreamState{
		Status:    types.HaveSentRoomPreviously,
		LastToken: lastToken,
	}
}

// logV4Response logs full response body at trace level (sensitive data)
// Enable with logging.*.level: trace in config
func logV4Response(response interface{}, userID, deviceID string, statusCode int) {
	responseBodyJSON, err := json.Marshal(response)
	if err == nil {
		logrus.WithFields(logrus.Fields{
			"timestamp":     time.Now().Format(time.RFC3339),
			"user_id":       userID,
			"device_id":     deviceID,
			"status":        statusCode,
			"response_body": string(responseBodyJSON),
		}).Trace("[V4_SYNC_DEBUG] Full response")
	}
}

// OnIncomingSyncRequestV4 handles POST /v4/sync requests (MSC4186 Simplified Sliding Sync)
func (rp *RequestPool) OnIncomingSyncRequestV4(req *http.Request, device *userapi.Device) util.JSONResponse {
	// Create a root span for tracing the entire sync request
	trace, ctx := internal.StartTask(req.Context(), "SlidingSync.V4")
	defer trace.EndTask()
	trace.SetTag("user_id", device.UserID)
	trace.SetTag("device_id", device.ID)

	// Replace request context with traced context
	req = req.WithContext(ctx)

	// Parse request body
	var v4Req types.SlidingSyncRequest
	if err := json.NewDecoder(req.Body).Decode(&v4Req); err != nil {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.BadJSON(fmt.Sprintf("Failed to parse request body: %s", err.Error())),
		}
	}


	// Read from query parameters if present (takes precedence over JSON body for compatibility)
	// Element Web and other clients using MSC3575 may send pos/timeout as URL params
	if posQuery := req.URL.Query().Get("pos"); posQuery != "" {
		v4Req.Pos = posQuery
	}
	if timeoutQuery := req.URL.Query().Get("timeout"); timeoutQuery != "" {
		logrus.WithField("timeout_query", timeoutQuery).Debug("[V4_SYNC] Got timeout from query param")
		if timeout, err := time.ParseDuration(timeoutQuery + "ms"); err == nil {
			v4Req.Timeout = int(timeout.Milliseconds())
			logrus.WithField("timeout_ms", v4Req.Timeout).Debug("[V4_SYNC] Parsed timeout successfully")
		} else {
			logrus.WithError(err).WithField("timeout_query", timeoutQuery).Error("[V4_SYNC] Failed to parse timeout from query param")
		}
	}

	// Default connection ID if not provided
	connID := v4Req.ConnID
	if connID == "" {
		connID = "default"
	}
	trace.SetTag("conn_id", connID)
	trace.SetTag("is_initial", v4Req.Pos == "")
	trace.SetTag("num_lists", len(v4Req.Lists))

	// DEBUG: Log incoming request details
	logrus.WithFields(logrus.Fields{
		"user_id":         device.UserID,
		"device_id":       device.ID,
		"conn_id":         connID,
		"pos":             v4Req.Pos,
		"timeout":         v4Req.Timeout,
		"num_lists":       len(v4Req.Lists),
		"num_room_subs":   len(v4Req.RoomSubscriptions),
		"has_extensions":  v4Req.Extensions != nil,
	}).Info("[V4_SYNC] Incoming sync request")

	// DEBUG: Full request body logging at trace level (sensitive data)
	// Enable with logging.*.level: trace in config
	requestBodyJSON, err := json.Marshal(v4Req)
	if err == nil {
		logrus.WithFields(logrus.Fields{
			"timestamp":    time.Now().Format(time.RFC3339),
			"user_id":      device.UserID,
			"device_id":    device.ID,
			"method":       req.Method,
			"path":         req.URL.Path,
			"query":        req.URL.RawQuery,
			"request_body": string(requestBodyJSON),
		}).Trace("[V4_SYNC_DEBUG] Full request")
	}

	// Parse position token if provided
	var since *types.SlidingSyncStreamToken
	if v4Req.Pos != "" {
		since, err = types.ParseSlidingSyncStreamToken(v4Req.Pos)
		if err != nil {
			return util.JSONResponse{
				Code: http.StatusBadRequest,
				JSON: spec.InvalidParam(fmt.Sprintf("Invalid position token: %s", err.Error())),
			}
		}
	}

	// Phase 10: Get or create connection (returns connection_key)
	connectionKey, err := rp.db.GetOrCreateConnection(req.Context(), device.UserID, device.ID, connID)
	if err != nil {
		logrus.WithError(err).Error("Failed to get or create sliding sync connection")
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}

	// Validate position token if provided
	if since != nil {
		// Validate that the position exists and belongs to this connection
		err = rp.db.ValidateConnectionPosition(req.Context(), since.ConnectionPosition, connectionKey)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"provided_position": since.ConnectionPosition,
				"connection_key":    connectionKey,
			}).Warn("Invalid position token - client should start fresh")
			// Return M_UNKNOWN_POS to signal the client to start a fresh connection
			// This matches Synapse behavior and tells the client the position is stale
			return util.JSONResponse{
				Code: http.StatusBadRequest,
				JSON: spec.MatrixError{
					ErrCode: spec.ErrorUnknownPos,
					Err:     "Connection position not found or expired. Please start a new sync connection.",
				},
			}
		}

		// Clean up old positions (like Synapse does)
		// Now that the client has used this position, we can delete all other positions
		// This prevents old state from accumulating and bleeding into new sessions
		if err := rp.db.DeleteOtherConnectionPositions(req.Context(), connectionKey, since.ConnectionPosition); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"connection_key":  connectionKey,
				"keep_position":   since.ConnectionPosition,
			}).Warn("Failed to clean up old connection positions")
			// Non-fatal - continue with the sync
		}
	}

	// Phase 10: Load previous stream states for delta computation
	// IMPORTANT: Only load previous states for incremental syncs (pos is non-empty)
	// For initial syncs (pos=""), start fresh with no previous state
	// Use position-specific query to avoid old state from previous sessions bleeding in
	var previousStreamStates map[string]map[string]*types.SlidingSyncStreamState
	if since != nil {
		// Load streams for the SPECIFIC position the client is syncing from
		// This is critical: we want the state AS IT WAS at that position, not "latest across all positions"
		previousStreamStates, err = rp.db.GetConnectionStreamsByPosition(req.Context(), since.ConnectionPosition)
		if err != nil {
			logrus.WithError(err).Error("Failed to load connection stream states")
			return util.JSONResponse{
				Code: http.StatusInternalServerError,
				JSON: spec.InternalServerError{},
			}
		}
		logrus.WithFields(logrus.Fields{
			"connection_key":      connectionKey,
			"connection_position": since.ConnectionPosition,
			"num_rooms_loaded":    len(previousStreamStates),
		}).Debug("[V4_STATE_DEBUG] Loaded previous stream states for specific position")
		// Log specific room states for debugging
		for roomID, streams := range previousStreamStates {
			if eventsStream, ok := streams["events"]; ok {
				logrus.WithFields(logrus.Fields{
					"room_id":             roomID,
					"room_status":         eventsStream.RoomStatus,
					"last_token":          eventsStream.LastToken,
					"connection_position": eventsStream.ConnectionPosition,
				}).Debug("[V4_STATE_DEBUG] Loaded room state from database")
			}
		}
	} else {
		// Initial sync - no previous states
		previousStreamStates = make(map[string]map[string]*types.SlidingSyncStreamState)
		logrus.Debug("[V4_STATE_DEBUG] Initial sync - no previous stream states")

		// Clear stale receipt delivery state for this connection
		// This ensures receipts are re-delivered on fresh sync (e.g., after logout/login, token expiry)
		if err := rp.db.DeleteConnectionReceipts(req.Context(), connectionKey); err != nil {
			logrus.WithError(err).WithField("connection_key", connectionKey).Warn("Failed to clear connection receipts on fresh sync")
			// Non-fatal - continue with the sync
		}
	}

	// Create connection state for this request
	connState := &V4ConnectionState{
		ConnectionKey:        connectionKey,
		ConnectionPosition:   0, // Will be set after creating position
		PreviousStreamStates: previousStreamStates,
	}

	// DEBUG: Log connection state
	numRoomsSent := len(previousStreamStates)
	logrus.WithFields(logrus.Fields{
		"conn_id":           connID,
		"connection_key":    connectionKey,
		"num_rooms_sent":    numRoomsSent,
	}).Debug("[V4_SYNC] Connection state loaded")

	// Update presence and last seen (reuse existing v3 logic)
	rp.updateLastSeen(req, device)
	rp.updatePresence(rp.db, v4Req.SetPresence, device.UserID)

	// Main sync loop - similar to v3 sync
	// Loop until we have updates to return or timeout expires
	for {
		startTime := time.Now()

		// Get current position from notifier
		currentPos := rp.Notifier.CurrentPosition()

	// For incremental syncs with timeout, wait for changes
	// This implements long-polling (per MSC4186 spec line 236)
	// Synapse behavior: always wait if timeout > 0 AND since != nil, regardless of global position
	logrus.WithFields(logrus.Fields{
		"since_nil":    since == nil,
		"timeout":      v4Req.Timeout,
		"will_wait":    since != nil && v4Req.Timeout > 0,
	}).Info("[V4_SYNC] Long polling check")

	if since != nil && v4Req.Timeout > 0 {
		logrus.WithField("timeout_ms", v4Req.Timeout).Info("[V4_SYNC] Entering long poll wait")
		// Incremental sync with timeout - wait for new events
		timeout := time.Duration(v4Req.Timeout) * time.Millisecond

		// Wait for changes using the notifier
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		// Create a minimal sync request for the notifier
		syncReq := &types.SyncRequest{
			Context:       req.Context(),
			Device:        device,
			Since:         since.StreamToken,
			Timeout:       timeout,
			WantFullState: false,
		}

		userStream := rp.Notifier.GetListener(*syncReq)
		defer userStream.Close()

		select {
		case <-userStream.GetNotifyChannel(since.StreamToken):
			// New events arrived, continue processing
			logrus.Info("[V4_SYNC] User stream notified - events may be available")
			currentPos = rp.Notifier.CurrentPosition()
			currentPos.ApplyUpdates(userStream.GetSyncPosition())
			logrus.WithFields(logrus.Fields{
				"old_pos": since.StreamToken.String(),
				"new_pos": currentPos.String(),
			}).Info("[V4_SYNC] Position updated after notification")
		case <-timer.C:
			// Timeout - return current position without changes
			// But we still need to process lists to return their current state
			logrus.Info("[V4_SYNC] Timeout expired with no changes")
			timeoutResp := types.SlidingSyncResponse{
				Pos:        since.String(), // Return same position
				Lists:      make(map[string]types.SlidingList),
				Rooms:      make(map[string]types.SlidingRoomData),
				Extensions: &types.ExtensionResponse{},
			}

			// Process requested lists to include their current state
			ctx := req.Context()
			roomsInLists := make(map[string]types.RoomSubscriptionConfig)
			for listName, listConfig := range v4Req.Lists {
				list, err := rp.processRoomList(ctx, device.UserID, listName, listConfig, connState, false)
				if err != nil {
					logrus.WithError(err).WithField("list_name", listName).Error("[V4_SYNC] Failed to process list on timeout")
					continue
				}
				timeoutResp.Lists[listName] = list

				// Track rooms that appear in list operations so we can populate room data
				for _, op := range list.Ops {
					if op.Op == "SYNC" && op.RoomIDs != nil {
						for _, roomID := range op.RoomIDs {
							// Use the max timeline_limit if room appears in multiple lists
							existing, exists := roomsInLists[roomID]
							if !exists || listConfig.TimelineLimit > existing.TimelineLimit {
								roomsInLists[roomID] = types.RoomSubscriptionConfig{
									TimelineLimit: listConfig.TimelineLimit,
									RequiredState: listConfig.RequiredState,
								}
							}
						}
					}
				}
			}

			// Populate room data for rooms that appear in list operations
			// This is critical - MSC4186 requires room data for rooms in list ops
			if len(roomsInLists) > 0 {
				snapshot, err := rp.db.NewDatabaseSnapshot(ctx)
				if err != nil {
					logrus.WithError(err).Error("[V4_SYNC] Failed to create snapshot for timeout room data")
				} else {
					var succeeded bool
					defer func() {
						if succeeded {
							snapshot.Commit()
						}
						snapshot.Rollback()
					}()

					logrus.WithField("num_rooms", len(roomsInLists)).Debug("[V4_SYNC] Populating room data for timeout response")

					for roomID, config := range roomsInLists {
						// For timeout responses, let BuildRoomData determine if there are actual changes
						var requiredStateConfig *types.RequiredStateConfig
						if len(config.RequiredState.Include) > 0 || len(config.RequiredState.Exclude) > 0 {
							requiredStateConfig = &config.RequiredState
						}

						// Determine room state from connection for proper incremental sync
						roomState := determineRoomStreamState(ctx, snapshot, connState, roomID, device.UserID)

						// Prepare fromToken for num_live calculation
						var fromPosPtr *types.StreamingToken
						if since != nil {
							fromPosPtr = &since.StreamToken
						}

						roomData, err := rp.BuildRoomData(ctx, snapshot, roomID, device.UserID, config.TimelineLimit, roomState, since.StreamToken, fromPosPtr, requiredStateConfig, false)
						if err != nil {
							logrus.WithError(err).WithField("room_id", roomID).Error("[V4_SYNC] Failed to build room data for timeout")
							continue
						}

						timeoutResp.Rooms[roomID] = *roomData
					}
					succeeded = true
				}
			}

			// Process extensions for timeout response
			// Extensions should be included even on timeout to provide e2ee data (OTK counts, etc.)
			snapshot, err := rp.db.NewDatabaseSnapshot(ctx)
			if err != nil {
				logrus.WithError(err).Error("[V4_SYNC] Failed to create snapshot for timeout extensions")
			} else {
				var succeeded bool
				defer func() {
					if succeeded {
						snapshot.Commit()
					}
					snapshot.Rollback()
				}()

				var fromPosPtr *types.StreamingToken
				if since != nil {
					fromPosPtr = &since.StreamToken
				}
				roomSubscriptions := make(map[string]bool, len(v4Req.RoomSubscriptions))
				for roomID := range v4Req.RoomSubscriptions {
					roomSubscriptions[roomID] = true
				}
				extensionResp, _, _, err := rp.ProcessExtensions(ctx, snapshot, &v4Req, device.UserID, device.ID, connectionKey, fromPosPtr, currentPos, timeoutResp.Lists, roomSubscriptions)
				if err != nil {
					logrus.WithError(err).Error("[V4_SYNC] Failed to process extensions for timeout")
					// Keep empty extension response
				} else {
					timeoutResp.Extensions = extensionResp
				}
				succeeded = true
			}

			logV4Response(timeoutResp, device.UserID, device.ID, http.StatusOK)
			return util.JSONResponse{
				Code: http.StatusOK,
				JSON: timeoutResp,
			}
		case <-req.Context().Done():
			// Client disconnected
			logrus.Info("[V4_SYNC] Client disconnected during wait")
			disconnectResp := types.SlidingSyncResponse{
				Pos:        since.String(),
				Lists:      make(map[string]types.SlidingList),
				Rooms:      make(map[string]types.SlidingRoomData),
				Extensions: &types.ExtensionResponse{},
			}

			// Process requested lists to include their current state
			ctx := req.Context()
			roomsInLists := make(map[string]types.RoomSubscriptionConfig)
			for listName, listConfig := range v4Req.Lists {
				list, err := rp.processRoomList(ctx, device.UserID, listName, listConfig, connState, false)
				if err != nil {
					logrus.WithError(err).WithField("list_name", listName).Error("[V4_SYNC] Failed to process list on disconnect")
					continue
				}
				disconnectResp.Lists[listName] = list

				// Track rooms that appear in list operations so we can populate room data
				for _, op := range list.Ops {
					if op.Op == "SYNC" && op.RoomIDs != nil {
						for _, roomID := range op.RoomIDs {
							// Use the max timeline_limit if room appears in multiple lists
							existing, exists := roomsInLists[roomID]
							if !exists || listConfig.TimelineLimit > existing.TimelineLimit {
								roomsInLists[roomID] = types.RoomSubscriptionConfig{
									TimelineLimit: listConfig.TimelineLimit,
									RequiredState: listConfig.RequiredState,
								}
							}
						}
					}
				}
			}

			// Populate room data for rooms that appear in list operations
			// This is critical - MSC4186 requires room data for rooms in list ops
			if len(roomsInLists) > 0 {
				snapshot, err := rp.db.NewDatabaseSnapshot(ctx)
				if err != nil {
					logrus.WithError(err).Error("[V4_SYNC] Failed to create snapshot for disconnect room data")
				} else {
					var succeeded bool
					defer func() {
						if succeeded {
							snapshot.Commit()
						}
						snapshot.Rollback()
					}()

					logrus.WithField("num_rooms", len(roomsInLists)).Debug("[V4_SYNC] Populating room data for disconnect response")

					for roomID, config := range roomsInLists {
						// For disconnect responses, let BuildRoomData determine if there are actual changes
						var requiredStateConfig *types.RequiredStateConfig
						if len(config.RequiredState.Include) > 0 || len(config.RequiredState.Exclude) > 0 {
							requiredStateConfig = &config.RequiredState
						}

						// Determine room state from connection for proper incremental sync
						roomState := determineRoomStreamState(ctx, snapshot, connState, roomID, device.UserID)

						// Prepare fromToken for num_live calculation
						var fromPosPtr *types.StreamingToken
						if since != nil {
							fromPosPtr = &since.StreamToken
						}

						roomData, err := rp.BuildRoomData(ctx, snapshot, roomID, device.UserID, config.TimelineLimit, roomState, since.StreamToken, fromPosPtr, requiredStateConfig, false)
						if err != nil {
							logrus.WithError(err).WithField("room_id", roomID).Error("[V4_SYNC] Failed to build room data for disconnect")
							continue
						}

						disconnectResp.Rooms[roomID] = *roomData
					}
					succeeded = true
				}
			}

			logV4Response(disconnectResp, device.UserID, device.ID, http.StatusOK)
			return util.JSONResponse{
				Code: http.StatusOK,
				JSON: disconnectResp,
			}
		}
	}

	// Phase 10: Create new connection position for this sync response
	// This is what goes into the pos token
	connState.ConnectionPosition, err = rp.db.CreateConnectionPosition(req.Context(), connState.ConnectionKey)
	if err != nil {
		logrus.WithError(err).Error("Failed to create connection position")
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}

	// Create new position token for response
	newToken := types.NewSlidingSyncStreamToken(connState.ConnectionPosition, currentPos)

	// Build response
	response := types.SlidingSyncResponse{
		Pos:        newToken.String(),
		Lists:      make(map[string]types.SlidingList),
		Rooms:      make(map[string]types.SlidingRoomData),
		Extensions: &types.ExtensionResponse{},
	}

	// Phase 2: Process room lists
	ctx := req.Context()
	// Track which rooms appear in lists and their timeline_limit
	roomsInLists := make(map[string]types.RoomSubscriptionConfig) // map[roomID]config with timeline_limit and required_state
	// When pos is empty, force initial sync for all lists
	forceInitialSync := (since == nil)
	for listName, listConfig := range v4Req.Lists {
		logrus.WithFields(logrus.Fields{
			"list_name":         listName,
			"range":             listConfig.Range,
			"timeline_limit":    listConfig.TimelineLimit,
			"force_initial":     forceInitialSync,
		}).Debug("[V4_SYNC] Processing list")

		list, err := rp.processRoomList(ctx, device.UserID, listName, listConfig, connState, forceInitialSync)
		if err != nil {
			// Log error but continue processing other lists
			logrus.WithError(err).WithField("list_name", listName).Error("[V4_SYNC] Failed to process list")
			continue
		}
		// Always include the list in response (Synapse returns lists even with no changes)
		response.Lists[listName] = list
		logrus.WithFields(logrus.Fields{
			"list_name": listName,
			"count":     list.Count,
			"num_ops":   len(list.Ops),
		}).Info("[V4_SYNC] List processed")

		for i, op := range list.Ops {
			logrus.WithFields(logrus.Fields{
				"list_name":     listName,
				"op_index":      i,
				"op_type":       op.Op,
				"range":         op.Range,
				"num_room_ids":  len(op.RoomIDs),
			}).Debug("[V4_SYNC] List operation")
		}

		// Track rooms that appeared in lists for Phase 3 room data population
		// Store the config from the list so we can use timeline_limit and required_state when building room data
		for _, op := range list.Ops {
			if op.Op == "SYNC" && op.RoomIDs != nil {
				for _, roomID := range op.RoomIDs {
					// Use the max timeline_limit if room appears in multiple lists
					// Merge required_state from multiple lists
					existing, exists := roomsInLists[roomID]
					if !exists || listConfig.TimelineLimit > existing.TimelineLimit {
						roomsInLists[roomID] = types.RoomSubscriptionConfig{
							TimelineLimit: listConfig.TimelineLimit,
							RequiredState: listConfig.RequiredState,
						}
					}
				}
			}
		}
	}

	// Phase 3: Process room subscriptions
	// Build set of all rooms we need to return data for (from lists + subscriptions)
	roomsToPopulate := make(map[string]types.RoomSubscriptionConfig)

	// Get partial state room IDs for filtering (MSC3706 faster joins)
	// This is used for both list rooms and explicit subscriptions
	partialStateRooms := make(map[string]bool)
	partialStateRoomIDs, err := rp.rsAPI.GetPartialStateRoomIDs(ctx)
	if err != nil {
		logrus.WithError(err).Warn("[V4_SYNC] Failed to get partial state rooms")
	} else {
		for _, roomID := range partialStateRoomIDs {
			partialStateRooms[roomID] = true
		}
		if len(partialStateRooms) > 0 {
			logrus.WithField("count", len(partialStateRooms)).Debug("[V4_SYNC] Found partial state rooms for filtering")
		}
	}

	// Add rooms from lists with their config (timeline_limit and required_state) from the list config
	// Also apply partial state filtering (MSC3706)
	for roomID, config := range roomsInLists {
		// Filter partial state rooms if required_state needs full state
		if partialStateRooms[roomID] && mustAwaitFullState(config.RequiredState, rp.cfg.Matrix) {
			logrus.WithFields(logrus.Fields{
				"room_id": roomID,
			}).Debug("[V4_SYNC] Filtering out partial state room from list - requires full state")
			continue
		}
		roomsToPopulate[roomID] = config
	}

	// Add/merge rooms from explicit subscriptions
	// Explicit subscriptions override list config for that room
	// Per MSC4186 and Synapse behavior, we must filter subscriptions to only include
	// rooms where the user has appropriate membership (not self-left)
	// Filters applied (matching Synapse):
	// 1. Membership filtering: exclude self-left rooms, allow kicked rooms
	// 2. Ignored user invite filtering: exclude invites from ignored users
	// 3. Partial state filtering (MSC3706): exclude partial state rooms if required_state needs full state
	if len(v4Req.RoomSubscriptions) > 0 {
		subSnapshot, err := rp.db.NewDatabaseSnapshot(ctx)
		if err != nil {
			logrus.WithError(err).Error("[V4_SYNC] Failed to create snapshot for subscription filtering")
		} else {
			defer subSnapshot.Rollback()

			// Get list of kicked rooms (leave where sender != user) to allow those subscriptions
			kickedRoomIDs, err := subSnapshot.KickedRoomIDs(ctx, device.UserID)
			kickedRooms := make(map[string]bool)
			if err != nil {
				logrus.WithError(err).Warn("[V4_SYNC] Failed to get kicked rooms, will filter all left rooms")
			} else {
				for _, roomID := range kickedRoomIDs {
					kickedRooms[roomID] = true
				}
			}

			// Get ignored users for invite filtering
			ignoredUsers := make(map[string]bool)
			ignoresData, err := subSnapshot.IgnoresForUser(ctx, device.UserID)
			if err != nil {
				logrus.WithError(err).Warn("[V4_SYNC] Failed to get ignored users")
			} else if ignoresData != nil && ignoresData.List != nil {
				for userID := range ignoresData.List {
					ignoredUsers[userID] = true
				}
			}

			// Get current invites to build room -> sender map for ignored user filtering
			inviteSenders := make(map[string]string)
			if len(ignoredUsers) > 0 {
				maxInviteID, err := subSnapshot.MaxStreamPositionForInvites(ctx)
				if err == nil && maxInviteID > 0 {
					inviteRange := types.Range{From: 0, To: maxInviteID, Backwards: false}
					invites, _, _, err := subSnapshot.InviteEventsInRange(ctx, device.UserID, inviteRange)
					if err != nil {
						logrus.WithError(err).Warn("[V4_SYNC] Failed to get invites for ignored user filtering")
					} else {
						for roomID, inviteEvent := range invites {
							inviteSenders[roomID] = string(inviteEvent.SenderID())
						}
					}
				}
			}

			for roomID, subConfig := range v4Req.RoomSubscriptions {
				// Check if user is allowed to subscribe to this room
				// Per Synapse's filter_membership_for_sync:
				// - Include joined, invited, banned rooms
				// - Include kicked rooms (leave where sender != user)
				// - Exclude self-left rooms (unless newly_left, which we don't track yet)
				membership, _, err := subSnapshot.SelectMembershipForUser(ctx, roomID, device.UserID, math.MaxInt64)
				if err != nil {
					logrus.WithError(err).WithFields(logrus.Fields{
						"room_id": roomID,
						"user_id": device.UserID,
					}).Debug("[V4_SYNC] Failed to get membership for subscription, skipping room")
					continue
				}

				// Filter based on membership
				if membership == spec.Leave {
					// Check if this is a kick (in kickedRooms) or self-leave
					if !kickedRooms[roomID] {
						// Self-leave - exclude from subscriptions
						logrus.WithFields(logrus.Fields{
							"room_id":    roomID,
							"user_id":    device.UserID,
							"membership": membership,
						}).Debug("[V4_SYNC] Filtering out self-left room from subscription")
						continue
					}
					// Kicked - allow subscription
					logrus.WithFields(logrus.Fields{
						"room_id":    roomID,
						"user_id":    device.UserID,
						"membership": membership,
					}).Debug("[V4_SYNC] Allowing kicked room in subscription")
				}

				// Filter invites from ignored users
				if membership == spec.Invite {
					if sender, ok := inviteSenders[roomID]; ok && ignoredUsers[sender] {
						logrus.WithFields(logrus.Fields{
							"room_id":       roomID,
							"user_id":       device.UserID,
							"invite_sender": sender,
						}).Debug("[V4_SYNC] Filtering out invite from ignored user")
						continue
					}
				}

				// Filter partial state rooms if required_state needs full state (MSC3706)
				if partialStateRooms[roomID] && mustAwaitFullState(subConfig.RequiredState, rp.cfg.Matrix) {
					logrus.WithFields(logrus.Fields{
						"room_id": roomID,
						"user_id": device.UserID,
					}).Debug("[V4_SYNC] Filtering out partial state room - subscription requires full state")
					continue
				}

				roomsToPopulate[roomID] = subConfig
			}
		}
	}

	// Phase 3.5: Filter to only changed rooms for incremental sync
	// For initial sync (since == nil), include all rooms
	// For incremental sync, only include rooms with changes since last sync
	if since != nil && len(roomsToPopulate) > 0 {
		// Create temporary snapshot for filtering query
		filterSnapshot, err := rp.db.NewDatabaseSnapshot(ctx)
		if err != nil {
			return util.JSONResponse{
				Code: http.StatusInternalServerError,
				JSON: spec.InternalServerError{},
			}
		}
		defer filterSnapshot.Rollback()

		// Get list of all candidate room IDs
		candidateRoomIDs := make([]string, 0, len(roomsToPopulate))
		for roomID := range roomsToPopulate {
			candidateRoomIDs = append(candidateRoomIDs, roomID)
		}

		// Query database for rooms that have PDU events since the last sync position
		roomsWithPDUChanges, err := filterSnapshot.RoomsWithEventsSince(ctx, candidateRoomIDs, since.StreamToken.PDUPosition)
		if err != nil {
			logrus.WithError(err).Error("[V4_SYNC] Failed to filter PDU changed rooms")
			// Continue without filtering on error - return all rooms
		} else {
			// Also query for rooms with invite changes
			// Invites are tracked separately in InvitePosition stream
			roomsWithInviteChanges, err := filterSnapshot.RoomsWithInvitesSince(ctx, device.UserID, candidateRoomIDs, since.StreamToken.InvitePosition)
			if err != nil {
				logrus.WithError(err).Error("[V4_SYNC] Failed to filter invite changed rooms")
				// Continue with just PDU filtering
				roomsWithInviteChanges = nil
			}

			// Build set of rooms to keep (rooms with PDU or invite changes + rooms never sent before)
			roomsToKeep := make(map[string]bool)
			roomKeepReasons := make(map[string]string) // Track why each room is kept for debugging
			for _, roomID := range roomsWithPDUChanges {
				roomsToKeep[roomID] = true
				roomKeepReasons[roomID] = "has_pdu_changes"
			}
			for _, roomID := range roomsWithInviteChanges {
				roomsToKeep[roomID] = true
				if roomKeepReasons[roomID] != "" {
					roomKeepReasons[roomID] += ",has_invite_changes"
				} else {
					roomKeepReasons[roomID] = "has_invite_changes"
				}
			}

			// Also include rooms that have never been sent on this connection
			// These should always be included as they're "new" to the client
			for roomID := range roomsToPopulate {
				roomState := determineRoomStreamState(ctx, filterSnapshot, connState, roomID, device.UserID)
				if roomState.Status == types.HaveSentRoomNever {
					roomsToKeep[roomID] = true
					if roomKeepReasons[roomID] != "" {
						roomKeepReasons[roomID] += ",status_never"
					} else {
						roomKeepReasons[roomID] = "status_never"
					}
				}
			}

			// CRITICAL FIX: Also include rooms with expanded subscriptions (timeline_limit increase)
			// This handles the case where Element X subscribes to a room with timeline_limit: 20
			// after receiving it from a list with timeline_limit: 1
			// Without this, the room is filtered out (no PDU changes) and client never gets expanded timeline
			for roomID, subConfig := range v4Req.RoomSubscriptions {
				// Check if this room was already sent with a lower timeline_limit
				prevConfig, err := rp.db.GetLatestRoomConfig(ctx, connState.ConnectionKey, roomID)
				if err != nil {
					logrus.WithError(err).WithField("room_id", roomID).Debug("[V4_SYNC] Failed to get previous room config")
					continue
				}

				if prevConfig != nil {
					// Room was sent before - check if timeline_limit expanded
					if subConfig.TimelineLimit > prevConfig.TimelineLimit {
						roomsToKeep[roomID] = true
						reason := fmt.Sprintf("timeline_expanded:%d->%d", prevConfig.TimelineLimit, subConfig.TimelineLimit)
						if roomKeepReasons[roomID] != "" {
							roomKeepReasons[roomID] += "," + reason
						} else {
							roomKeepReasons[roomID] = reason
						}
						logrus.WithFields(logrus.Fields{
							"room_id":          roomID,
							"prev_limit":       prevConfig.TimelineLimit,
							"new_limit":        subConfig.TimelineLimit,
						}).Info("[V4_SYNC] Timeline limit expanded - resending room data")
					}
				} else {
					// Room was never sent before via subscription - include it
					// (This handles new room subscriptions)
					if !roomsToKeep[roomID] {
						roomsToKeep[roomID] = true
						if roomKeepReasons[roomID] != "" {
							roomKeepReasons[roomID] += ",new_subscription"
						} else {
							roomKeepReasons[roomID] = "new_subscription"
						}
						logrus.WithFields(logrus.Fields{
							"room_id":        roomID,
							"timeline_limit": subConfig.TimelineLimit,
						}).Info("[V4_SYNC] New room subscription - including room data")
					}
				}
			}

			// Log filtering decisions for debugging
			logrus.WithFields(logrus.Fields{
				"total_pdu_changes":    len(roomsWithPDUChanges),
				"total_invite_changes": len(roomsWithInviteChanges),
				"rooms_to_keep":        len(roomsToKeep),
			}).Debug("[V4_STATE_DEBUG] Room filtering results")
			for roomID, reason := range roomKeepReasons {
				logrus.WithFields(logrus.Fields{
					"room_id": roomID,
					"reason":  reason,
				}).Debug("[V4_STATE_DEBUG] Room kept in sync")
			}

			// Filter roomsToPopulate to only rooms we want to keep
			filteredRooms := make(map[string]types.RoomSubscriptionConfig)
			for roomID, config := range roomsToPopulate {
				if roomsToKeep[roomID] {
					filteredRooms[roomID] = config
				}
			}

			logrus.WithFields(logrus.Fields{
				"before_filter": len(roomsToPopulate),
				"after_filter":  len(filteredRooms),
				"filtered_out":  len(roomsToPopulate) - len(filteredRooms),
			}).Debug("[V4_SYNC] Filtered rooms to only changed rooms")

			roomsToPopulate = filteredRooms
		}
	}

	// Phase 3: Populate room data for all rooms
	// Create a single database snapshot for all room queries
	snapshot, err := rp.db.NewDatabaseSnapshot(ctx)
	if err != nil {
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}
	var succeeded bool
	defer func() {
		if succeeded {
			snapshot.Commit() // Best effort
		}
		snapshot.Rollback() // No-op if already committed
	}()

	logrus.WithFields(logrus.Fields{
		"num_rooms_to_populate": len(roomsToPopulate),
		"from_lists":            len(roomsInLists),
		"from_subscriptions":    len(v4Req.RoomSubscriptions),
	}).Debug("[V4_SYNC] Populating room data")

	for roomID, config := range roomsToPopulate {
		// Phase 10: Determine room state from previous stream states
		// This drives incremental sync behavior (initial vs live vs previously)
		roomState := determineRoomStreamState(ctx, snapshot, connState, roomID, device.UserID)

		// Check for timeline expansion per MSC4186
		// Per MSC4186: "if the timeline_limit has increased (to say N) the server SHOULD
		// ignore this and send down the latest N events, even if some of those events
		// have previously been sent. [...] The server should return rooms that have
		// expanded timelines immediately, rather than waiting for the next update"
		//
		// This check must happen early because we need to know if the timeline is
		// expanding before we decide to skip the room due to "extension only" updates.
		// Two cases to handle:
		// 1. Timeline limit increased from previous value (subscription or list)
		// 2. NEW subscription added for a room that was previously only in lists
		timelineExpanded := false
		if since != nil {
			prevConfig, err := rp.db.GetLatestRoomConfig(ctx, connState.ConnectionKey, roomID)
			if err == nil && prevConfig != nil {
				// Room was sent before - check if timeline_limit expanded
				if config.TimelineLimit > prevConfig.TimelineLimit {
					timelineExpanded = true
					logrus.WithFields(logrus.Fields{
						"room_id":    roomID,
						"prev_limit": prevConfig.TimelineLimit,
						"new_limit":  config.TimelineLimit,
					}).Info("[V4_SYNC] Timeline expanded - fetching historical events")
				}
			} else if err == nil && prevConfig == nil {
				// No previous config found but room might have been sent via lists
				// Check if this is a subscription for a room that was already sent
				if _, isSubscription := v4Req.RoomSubscriptions[roomID]; isSubscription {
					if roomState.Status == types.HaveSentRoomLive || roomState.Status == types.HaveSentRoomPreviously {
						// Room was sent before (tracked in stream state) but no room config stored
						// This can happen for rooms sent via lists before we started tracking configs
						// Treat as expansion to send historical events
						timelineExpanded = true
						logrus.WithFields(logrus.Fields{
							"room_id":     roomID,
							"new_limit":   config.TimelineLimit,
							"room_status": roomState.Status,
							"reason":      "new_subscription_for_previously_sent_room",
						}).Info("[V4_SYNC] New subscription for previously sent room - fetching historical events")
					}
				}
			}
		}

		logrus.WithFields(logrus.Fields{
			"room_id":           roomID,
			"room_status":       roomState.Status,
			"is_initial":        roomState.Status.IsInitial(),
			"timeline_limit":    config.TimelineLimit,
			"timeline_expanded": timelineExpanded,
		}).Debug("[V4_SYNC] Building room data")

		// Pass required_state config (Phase 4)
		var requiredStateConfig *types.RequiredStateConfig
		if len(config.RequiredState.Include) > 0 || len(config.RequiredState.Exclude) > 0 {
			requiredStateConfig = &config.RequiredState
		}

		// Prepare fromToken for num_live calculation and initial flag
		var fromPosPtr *types.StreamingToken
		if since != nil {
			fromPosPtr = &since.StreamToken
		}

		roomData, err := rp.BuildRoomData(ctx, snapshot, roomID, device.UserID, config.TimelineLimit, roomState, currentPos, fromPosPtr, requiredStateConfig, timelineExpanded)
		if err != nil {
			// Log error but continue with other rooms
			logrus.WithError(err).WithField("room_id", roomID).Error("[V4_SYNC] Failed to build room data")
			continue
		}

		// Set expanded_timeline flag if timeline_limit increased
		// This signals to clients that we're sending historical events due to expansion
		if timelineExpanded {
			roomData.ExpandedTimeline = true
		}

		response.Rooms[roomID] = *roomData

		// Phase 10: Track stream state for "events" stream
		// Store current position so we can compute deltas next time
		// Room is sent in this response, so mark as "live" for next sync
		roomStatus := types.HaveSentRoomLive.String()
		lastToken := currentPos.String()
		logrus.WithFields(logrus.Fields{
			"room_id":             roomID,
			"connection_position": connState.ConnectionPosition,
			"room_status":         roomStatus,
			"last_token":          lastToken,
		}).Debug("[V4_STATE_DEBUG] Persisting stream state to database")
		if err := rp.db.UpdateConnectionStream(ctx, connState.ConnectionPosition, roomID, "events", roomStatus, lastToken); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"room_id":             roomID,
				"connection_position": connState.ConnectionPosition,
			}).Error("[V4_STATE_DEBUG] Failed to persist stream state")
			// Continue anyway - this is not fatal
		} else {
			logrus.WithFields(logrus.Fields{
				"room_id":             roomID,
				"connection_position": connState.ConnectionPosition,
			}).Debug("[V4_STATE_DEBUG] Successfully persisted stream state")
		}

		// Track room config (timeline_limit) for detecting expanded subscriptions
		// This allows us to detect when a client increases timeline_limit and needs more events
		// We need a valid required_state_id due to foreign key constraint
		var requiredStateID int64
		if requiredStateConfig != nil {
			// Serialize required_state to JSON for deduplication
			requiredStateJSON, err := json.Marshal(requiredStateConfig)
			if err != nil {
				logrus.WithError(err).WithField("room_id", roomID).Error("[V4_STATE_DEBUG] Failed to serialize required_state")
			} else {
				requiredStateID, err = rp.db.GetOrCreateRequiredStateID(ctx, connState.ConnectionKey, string(requiredStateJSON))
				if err != nil {
					logrus.WithError(err).WithField("room_id", roomID).Error("[V4_STATE_DEBUG] Failed to get/create required_state_id")
				}
			}
		}
		// If no required_state or error, use a default empty config
		if requiredStateID == 0 {
			emptyJSON := "[]"
			var err error
			requiredStateID, err = rp.db.GetOrCreateRequiredStateID(ctx, connState.ConnectionKey, emptyJSON)
			if err != nil {
				logrus.WithError(err).WithField("room_id", roomID).Error("[V4_STATE_DEBUG] Failed to get/create default required_state_id")
				// Continue without storing room config - this will cause timeline expansion to repeat
				// but at least it won't cause the sync to fail
			}
		}
		if requiredStateID != 0 {
			if err := rp.db.UpdateRoomConfig(ctx, connState.ConnectionPosition, roomID, config.TimelineLimit, requiredStateID); err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"room_id":          roomID,
					"timeline_limit":   config.TimelineLimit,
					"required_state_id": requiredStateID,
				}).Error("[V4_STATE_DEBUG] Failed to persist room config")
				// Continue anyway - this is not fatal
			} else {
				logrus.WithFields(logrus.Fields{
					"room_id":          roomID,
					"timeline_limit":   config.TimelineLimit,
					"required_state_id": requiredStateID,
				}).Debug("[V4_STATE_DEBUG] Persisted room config for timeline expansion tracking")
			}
		}
	}

	// CRITICAL FIX: Copy forward stream states for rooms that were previously sent
	// but not processed in this response. Without this, when we delete old positions
	// (via cascade delete), we lose the stream state for rooms that had no changes.
	// This causes those rooms to incorrectly appear as "never sent" on the next request,
	// even though they were sent before.
	// See: https://github.com/element-hq/dendrite/issues/XXXX
	if since != nil && connState.PreviousStreamStates != nil {
		copiedCount := 0
		for roomID, streams := range connState.PreviousStreamStates {
			// Skip rooms that were processed in this response (they already have updated state)
			if _, processed := roomsToPopulate[roomID]; processed {
				continue
			}
			// Copy forward the "events" stream state to the new position
			if eventsStream, ok := streams["events"]; ok {
				if err := rp.db.UpdateConnectionStream(ctx, connState.ConnectionPosition, roomID, "events", eventsStream.RoomStatus, eventsStream.LastToken); err != nil {
					logrus.WithError(err).WithFields(logrus.Fields{
						"room_id":             roomID,
						"connection_position": connState.ConnectionPosition,
					}).Error("[V4_STATE_DEBUG] Failed to copy forward stream state")
				} else {
					copiedCount++
				}
			}
		}
		if copiedCount > 0 {
			logrus.WithFields(logrus.Fields{
				"copied_count":        copiedCount,
				"connection_position": connState.ConnectionPosition,
			}).Debug("[V4_STATE_DEBUG] Copied forward stream states for unchanged rooms")
		}
	}

	succeeded = true

	// Phase 9: Process extensions
	var fromPosPtr *types.StreamingToken
	if since != nil {
		fromPosPtr = &since.StreamToken
	}
	// Build room subscriptions map from request
	roomSubscriptions := make(map[string]bool, len(v4Req.RoomSubscriptions))
	for roomID := range v4Req.RoomSubscriptions {
		roomSubscriptions[roomID] = true
	}
	extensionResp, updatedPos, deliveredReceipts, err := rp.ProcessExtensions(ctx, snapshot, &v4Req, device.UserID, device.ID, connectionKey, fromPosPtr, currentPos, response.Lists, roomSubscriptions)
	if err != nil {
		logrus.WithError(err).Error("Failed to process extensions")
		// Continue anyway - extensions are optional, return empty extension response
		response.Extensions = &types.ExtensionResponse{}
	} else {
		response.Extensions = extensionResp
		// Use the updated position from extensions (fixes receipt position tracking)
		oldPos := currentPos
		currentPos = updatedPos
		// CRITICAL: Update response.Pos with the new position that includes updated extension positions
		// This fixes the receipt repetition bug where receipt position wasn't advancing
		newToken = types.NewSlidingSyncStreamToken(connState.ConnectionPosition, currentPos)
		response.Pos = newToken.String()

		// DEBUG: Log position update if it changed
		if oldPos.ReceiptPosition != currentPos.ReceiptPosition {
			logrus.WithFields(logrus.Fields{
				"old_receipt_pos": oldPos.ReceiptPosition,
				"new_receipt_pos": currentPos.ReceiptPosition,
				"updated_token":   response.Pos,
			}).Info("[V4_SYNC] Receipt position advanced in token")
		}

		// Update connection state for delivered receipts in a write transaction
		// IMPORTANT: This must be done in a separate write transaction, NOT in the read-only snapshot
		if len(deliveredReceipts) > 0 {
			logrus.WithField("count", len(deliveredReceipts)).Debug("[RECEIPTS] Updating connection state for delivered receipts")
			txn, err := rp.db.NewDatabaseTransaction(ctx)
			if err != nil {
				logrus.WithError(err).Error("[RECEIPTS] Failed to create write transaction")
			} else {
				defer txn.Rollback()
				for _, receipt := range deliveredReceipts {
					err := txn.UpsertConnectionReceipt(
						ctx, connectionKey,
						receipt.RoomID, receipt.Type, receipt.UserID,
						receipt.EventID, receipt.Timestamp,
					)
					if err != nil {
						logrus.WithError(err).WithFields(logrus.Fields{
							"room_id": receipt.RoomID,
							"type":    receipt.Type,
						}).Error("[RECEIPTS] Failed to update connection receipt")
						break
					}
				}
				if err := txn.Commit(); err != nil {
					logrus.WithError(err).Error("[RECEIPTS] Failed to commit connection receipt updates")
				}
			}
		}
	}

	// DEBUG: Log final response summary
	logrus.WithFields(logrus.Fields{
		"new_pos":      response.Pos,
		"num_lists":    len(response.Lists),
		"num_rooms":    len(response.Rooms),
		"has_ext":      response.Extensions != nil,
	}).Info("[V4_SYNC] Returning response")

	// Log room IDs in response
	if len(response.Rooms) > 0 {
		roomIDs := make([]string, 0, len(response.Rooms))
		for roomID := range response.Rooms {
			roomIDs = append(roomIDs, roomID)
		}
		logrus.WithField("room_ids", roomIDs).Debug("[V4_SYNC] Response rooms")
	}

		// DEBUG: Log response structure to verify JSON format
		listOpsCount := make(map[string]int)
		for listName, list := range response.Lists {
			listOpsCount[listName] = len(list.Ops)
		}
		logrus.WithFields(logrus.Fields{
			"pos":            response.Pos,
			"list_ops":       listOpsCount,
			"has_extensions": response.Extensions != nil,
		}).Debug("[V4_SYNC] Response structure")

		// Check if response has meaningful updates
		// Similar to v3 sync's HasUpdates() check
		hasUpdates := v4ResponseHasUpdates(response)
		logrus.WithField("has_updates", hasUpdates).Info("[V4_SYNC] Checked for updates")

		// If no updates and timeout remaining, loop again with bumped position
		// This handles the case where global position advanced but there are no user-specific changes
		if !hasUpdates && v4Req.Timeout > 0 && since != nil {
			// Bump since to current position
			since = types.NewSlidingSyncStreamToken(connState.ConnectionPosition, currentPos)
			// Reduce timeout by elapsed time
			elapsed := time.Since(startTime)
			v4Req.Timeout = int(time.Duration(v4Req.Timeout)*time.Millisecond - elapsed)/int(time.Millisecond)
			if v4Req.Timeout < 0 {
				v4Req.Timeout = 0
			}
			logrus.WithFields(logrus.Fields{
				"elapsed_ms":  elapsed.Milliseconds(),
				"new_timeout": v4Req.Timeout,
			}).Info("[V4_SYNC] No updates, looping again with reduced timeout")
			continue
		}

		// Return response (either has updates or timeout expired or first sync)
		logV4Response(response, device.UserID, device.ID, http.StatusOK)
		return util.JSONResponse{
			Code: http.StatusOK,
			JSON: response,
		}
	}
}

// v4ResponseHasUpdates checks if a sliding sync response has meaningful updates
// Similar to v3 sync's HasUpdates() method
func v4ResponseHasUpdates(response types.SlidingSyncResponse) bool {
	// Check if any list has operations
	for _, list := range response.Lists {
		if len(list.Ops) > 0 {
			return true
		}
	}

	// Check if there are room updates
	if len(response.Rooms) > 0 {
		return true
	}

	// Check if extensions have meaningful data
	if response.Extensions != nil {
		// to_device events
		if response.Extensions.ToDevice != nil && len(response.Extensions.ToDevice.Events) > 0 {
			return true
		}

		// E2EE updates (device lists changed)
		if response.Extensions.E2EE != nil && response.Extensions.E2EE.DeviceLists != nil {
			if len(response.Extensions.E2EE.DeviceLists.Changed) > 0 || len(response.Extensions.E2EE.DeviceLists.Left) > 0 {
				return true
			}
		}

		// Account data updates
		if response.Extensions.AccountData != nil {
			if len(response.Extensions.AccountData.Global) > 0 || len(response.Extensions.AccountData.Rooms) > 0 {
				return true
			}
		}

		// Receipt updates
		if response.Extensions.Receipts != nil && len(response.Extensions.Receipts.Rooms) > 0 {
			return true
		}

		// Typing updates
		if response.Extensions.Typing != nil && len(response.Extensions.Typing.Rooms) > 0 {
			return true
		}
	}

	return false
}

// processRoomList handles a single room list configuration
func (rp *RequestPool) processRoomList(
	ctx context.Context,
	userID string,
	listName string,
	config types.SlidingListConfig,
	connState *V4ConnectionState,
	forceInitialSync bool,
) (types.SlidingList, error) {
	// Get all rooms for the user (joined + invited)
	// MSC4186: Lists include ALL rooms unless explicitly filtered by membership
	var rooms []RoomWithBumpStamp
	var err error

	// Check if filter explicitly requests only invites
	if config.Filters != nil && config.Filters.IsInvite != nil && *config.Filters.IsInvite {
		// Only invited rooms
		rooms, err = rp.GetRoomsForUser(ctx, userID, spec.Invite)
	} else if config.Filters != nil && config.Filters.IsInvite != nil && !*config.Filters.IsInvite {
		// Only non-invited rooms (joined, left, etc.)
		rooms, err = rp.GetRoomsForUser(ctx, userID, "join")
	} else {
		// No invite filter - get rooms with active memberships (joined + invited + banned + kicked)
		// Per MSC4186 and Synapse behavior:
		// - Joined rooms: always included
		// - Invited rooms: always included
		// - Banned rooms: included (users can /forget to remove)
		// - Kicked rooms (leave where sender != user): included (users can /forget to remove)
		// - Left rooms (self-leave): EXCLUDED from default lists
		// Left rooms should only appear as "newly_left" during incremental sync
		joinedRooms, err1 := rp.GetRoomsForUser(ctx, userID, "join")
		invitedRooms, err2 := rp.GetRoomsForUser(ctx, userID, spec.Invite)
		bannedRooms, err3 := rp.GetRoomsForUser(ctx, userID, spec.Ban)
		kickedRooms, err4 := rp.GetKickedRooms(ctx, userID)
		if err1 != nil {
			return types.SlidingList{}, err1
		}
		if err2 != nil {
			return types.SlidingList{}, err2
		}
		if err3 != nil {
			return types.SlidingList{}, err3
		}
		if err4 != nil {
			return types.SlidingList{}, err4
		}
		// Combine lists (excluding self-left rooms, but including kicked rooms)
		rooms = append(joinedRooms, invitedRooms...)
		rooms = append(rooms, bannedRooms...)
		rooms = append(rooms, kickedRooms...)

		// Deduplicate rooms - a room might appear in multiple membership states
		// (e.g., both banned and invited during kick→reinvite sequence)
		// Keep the first occurrence (highest priority membership: join > invite > ban > kicked)
		seen := make(map[string]bool)
		deduped := make([]RoomWithBumpStamp, 0, len(rooms))
		for _, room := range rooms {
			if !seen[room.RoomID] {
				seen[room.RoomID] = true
				deduped = append(deduped, room)
			}
		}
		rooms = deduped
	}

	if err != nil {
		return types.SlidingList{}, err
	}

	// Apply filters if specified
	if config.Filters != nil {
		rooms, err = rp.ApplyRoomFilters(ctx, rooms, config.Filters, userID)
		if err != nil {
			return types.SlidingList{}, err
		}
	}

	// Sort by activity (most recent first)
	SortRoomsByActivity(rooms)

	// Total count before windowing
	totalCount := len(rooms)

	// Apply sliding window if range is specified
	var windowedRooms []RoomWithBumpStamp
	var rangeSpec []int
	if len(config.Range) == 2 {
		rangeSpec = config.Range
		windowedRooms = ApplySlidingWindow(rooms, rangeSpec)
	} else {
		// No range specified, return all rooms
		windowedRooms = rooms
		rangeSpec = []int{0, len(rooms) - 1}
	}

	// Generate SYNC operation (Phase 2 only supports SYNC)
	// Phase 3+ will implement INSERT/DELETE/INVALIDATE for incremental updates
	ops := []types.SlidingOperation{}
	if len(windowedRooms) > 0 {
		// Extract room IDs for this list
		roomIDs := make([]string, len(windowedRooms))
		for i, room := range windowedRooms {
			roomIDs[i] = room.RoomID
		}

		// Phase 10: Always send SYNC operations for non-empty lists
		// This ensures notification count changes (from read receipts) are always sent,
		// even when the room membership hasn't changed.
		// Following Synapse's approach: rooms should be included when they have ANY updates
		// (events, receipts, notification counts), not just membership changes.
		// TODO: Optimize by tracking which specific rooms have updates (like Synapse's get_rooms_that_might_have_updates)
		var previousRoomIDs []string
		listChanged := true // Always send updates

		if !forceInitialSync {
			// Still load previous state for logging/debugging
			previousRoomIDsJSON, exists, err := rp.db.GetConnectionList(ctx, connState.ConnectionKey, listName)
			if err != nil {
				logrus.WithError(err).WithField("list", listName).Error("Failed to load connection list")
			} else if exists {
				if err := json.Unmarshal([]byte(previousRoomIDsJSON), &previousRoomIDs); err != nil {
					logrus.WithError(err).WithField("list", listName).Error("Failed to decode connection list JSON")
				}
			}
		}

		logrus.WithFields(logrus.Fields{
			"list_name":       listName,
			"list_changed":    listChanged,
			"prev_room_count": len(previousRoomIDs),
			"curr_room_count": len(roomIDs),
			"is_first_send":   previousRoomIDs == nil,
			"force_initial":   forceInitialSync,
		}).Debug("[V4_SYNC] List change detection")

		if listChanged {
			op := GenerateSyncOperation(windowedRooms, rangeSpec)
			ops = append(ops, op)

			logrus.WithFields(logrus.Fields{
				"list_name":    listName,
				"op_type":      op.Op,
				"num_room_ids": len(op.RoomIDs),
			}).Info("[V4_SYNC] Generated list operation (list changed)")

			// Phase 10: Store the current room IDs for this list in database (JSON encoded)
			roomIDsJSON, err := json.Marshal(roomIDs)
			if err != nil {
				logrus.WithError(err).WithField("list", listName).Error("Failed to encode room IDs to JSON")
			} else {
				if err := rp.db.UpdateConnectionList(ctx, connState.ConnectionKey, listName, string(roomIDsJSON)); err != nil {
					logrus.WithError(err).WithField("list", listName).Error("Failed to persist connection list")
					// Continue anyway - this is not fatal
				}
			}
		} else {
			logrus.WithField("list_name", listName).Debug("[V4_SYNC] List unchanged, no operations needed")
		}
		// If list hasn't changed, return empty ops (no update needed)
	}

	return types.SlidingList{
		Count: totalCount,
		Ops:   ops,
	}, nil
}

// equalStringSlices checks if two string slices have the same elements in order
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
