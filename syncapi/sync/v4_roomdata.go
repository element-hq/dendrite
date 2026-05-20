// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package sync

import (
	"context"
	"math"

	"github.com/element-hq/dendrite/internal"
	rstypes "github.com/element-hq/dendrite/roomserver/types"
	"github.com/element-hq/dendrite/syncapi/storage"
	"github.com/element-hq/dendrite/syncapi/synctypes"
	"github.com/element-hq/dendrite/syncapi/types"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// BuildRoomData constructs SlidingRoomData for a single room
// Phase 3: Basic implementation with timeline and metadata
// Phase 4: Required state filtering
// Phase 11: Invite state support for Element X
// roomState determines if this is initial/live/previously for proper incremental sync
// fromToken is the position from the sync request (nil for initial sync) - used for num_live calculation
// ignoreTimelineBound: if true, fetch timeline from scratch (for timeline_limit expansion)
//
//	This is separate from initial sync - we still set initial=false but fetch historical events
func (rp *RequestPool) BuildRoomData(
	ctx context.Context,
	snapshot storage.DatabaseTransaction,
	roomID string,
	userID string,
	timelineLimit int,
	roomState types.RoomStreamState,
	currentPos types.StreamingToken,
	fromToken *types.StreamingToken,
	requiredStateConfig *types.RequiredStateConfig,
	ignoreTimelineBound bool,
) (*types.SlidingRoomData, error) {
	// CRITICAL: initial indicates if this is the FIRST TIME this room is being sent on this CONNECTION
	// Per MSC4186: "Indicates whether this is the first time this room has been sent in this connection"
	// This is different from the sync-level fromToken:
	// - fromToken == nil: Client's first sync ever (all rooms are initial)
	// - roomState.Status == HaveSentRoomNever: Room never sent on this connection (this room is initial)
	// Both cases require initial=true for the room
	isInitialForRoom := fromToken == nil || roomState.Status == types.HaveSentRoomNever
	roomData := &types.SlidingRoomData{
		Initial: isInitialForRoom,
	}

	// Phase 11: Check if this is an invited room
	// Query user's membership in the room to determine how to build room data
	// IMPORTANT: Membership can come from two sources:
	// 1. syncapi_memberships table: Tracks join/leave/ban from PDU stream (PDUPosition)
	// 2. syncapi_invite_events table: Tracks invites from Invite stream (InvitePosition)
	// We need to check BOTH and use the most recent state

	// First check PDU-based membership (join/leave/ban)
	// Use math.MaxInt64 to get the most recent membership state regardless of topological position
	pduMembership, _, err := snapshot.SelectMembershipForUser(ctx, roomID, userID, math.MaxInt64)
	if err != nil {
		logrus.WithError(err).WithField("room_id", roomID).Warn("Failed to get PDU membership")
		pduMembership = "leave" // Default if we can't determine
	}

	// Then check if there's an active invite in the invites table
	membership := pduMembership
	var inviteEvent *rstypes.HeaderedEvent // Store invite event for buildInviteRoomData
	if currentPos.InvitePosition > 0 {
		inviteRange := types.Range{
			From:      0,
			To:        currentPos.InvitePosition,
			Backwards: false,
		}
		invites, retired, _, err := snapshot.InviteEventsInRange(ctx, userID, inviteRange)
		if err != nil {
			logrus.WithError(err).WithField("room_id", roomID).Warn("Failed to check for invites")
		} else {
			// Check if this specific room has an active invite (not retired)
			if invite, hasInvite := invites[roomID]; hasInvite {
				if _, isRetired := retired[roomID]; !isRetired {
					// Active invite found! This overrides the PDU membership
					membership = "invite"
					inviteEvent = invite // Keep the event for extracting invite_room_state
				}
			}
		}
	}

	logrus.WithFields(logrus.Fields{
		"room_id":        roomID,
		"user_id":        userID,
		"membership":     membership,
		"pdu_membership": pduMembership,
		"pdu_pos":        currentPos.PDUPosition,
		"invite_pos":     currentPos.InvitePosition,
	}).Debug("[V4_SYNC] Room membership detected")

	// If user is invited, return stripped state instead of timeline/required_state
	if membership == "invite" {
		logrus.WithFields(logrus.Fields{
			"room_id": roomID,
		}).Info("[V4_SYNC] Building invite room data (stripped state)")
		return rp.buildInviteRoomData(ctx, snapshot, roomID, userID, isInitialForRoom, inviteEvent)
	}

	// Phase 3: Get timeline events (up to timelineLimit)
	// Also calculates num_live (how many events are "live" vs historical)
	var timelineLimited bool
	var numLive int
	if timelineLimit > 0 {
		// Determine if we need to fetch from scratch (historical events):
		// 1. ignoreTimelineBound: timeline expansion requested
		// 2. isInitialForRoom: room never sent before on this connection
		// In both cases, we need to fetch the latest N events regardless of sync token
		timelineFromToken := fromToken
		if ignoreTimelineBound || isInitialForRoom {
			timelineFromToken = nil
		}
		timeline, limited, numLiveCount, err := rp.getTimelineEvents(ctx, snapshot, roomID, userID, timelineLimit, roomState, currentPos, timelineFromToken)
		if err != nil {
			logrus.WithError(err).WithField("room_id", roomID).Error("Failed to get timeline events")
			// Continue anyway, return room with empty timeline
		} else {
			roomData.Timeline = timeline
			timelineLimited = limited
			numLive = numLiveCount
		}
	}

	// Phase 3: Get room name from state
	roomData.Name = rp.getRoomNameFromDB(ctx, snapshot, roomID)

	// Phase 3: Get room avatar from state
	roomData.AvatarURL = rp.getRoomAvatar(ctx, snapshot, roomID)

	// Phase 3: Get room topic from state
	roomData.Topic = rp.getRoomTopic(ctx, snapshot, roomID)

	// Phase 4: Get required state events
	// Phase 5: Pass timeline for lazy member loading
	// Phase 12/13: Implement correct $LAZY member loading per MSC4186
	// - Initial sync: Send full required_state
	// - Incremental sync with timeline events: Send $LAZY members (senders) per MSC4186 section 279-296
	//   This allows the SDK to look up member info needed for push rule evaluation
	if requiredStateConfig != nil && len(requiredStateConfig.Include) > 0 {
		// Determine if we need to fetch required_state
		shouldFetchState := false
		reason := ""

		if isInitialForRoom {
			// Initial sync or first time room is sent on this connection: always include full required_state
			// This handles both fromToken == nil AND rooms with HaveSentRoomNever status
			shouldFetchState = true
			reason = "initial sync for room"
		} else if len(roomData.Timeline) > 0 {
			// Incremental sync with timeline events: check if $LAZY is requested
			// Per MSC4186: "the server will return the membership events for all the senders
			// of events in timeline_events, excluding membership events previously returned"
			hasLazy := false
			for _, pattern := range requiredStateConfig.Include {
				if len(pattern) == 2 && pattern[1] == "$LAZY" {
					hasLazy = true
					break
				}
			}
			if hasLazy {
				shouldFetchState = true
				reason = "incremental sync with timeline events and $LAZY"
			}
		}

		if shouldFetchState {
			logrus.WithFields(logrus.Fields{
				"room_id": roomID,
				"user_id": userID,
				"reason":  reason,
				"timeline_events": len(roomData.Timeline),
			}).Debug("[REQUIRED_STATE] Getting required state")

			requiredState, err := rp.getRequiredState(ctx, snapshot, roomID, userID, requiredStateConfig, roomData.Timeline)
			if err != nil {
				logrus.WithError(err).WithField("room_id", roomID).Error("Failed to get required state")
				// Continue anyway, return room without required state
			} else {
				roomData.RequiredState = requiredState

				logrus.WithFields(logrus.Fields{
					"room_id":      roomID,
					"user_id":      userID,
					"state_count":  len(requiredState),
					"reason":       reason,
				}).Debug("[REQUIRED_STATE] Returned required state events")
			}
		} else {
			logrus.WithFields(logrus.Fields{
				"room_id": roomID,
				"user_id": userID,
				"has_timeline": len(roomData.Timeline) > 0,
			}).Debug("[REQUIRED_STATE] Skipping required_state (no timeline events or $LAZY not requested)")
		}
	}

	// Phase 6: Notification counts
	// Hardcode notification_count and highlight_count to 0 to match Synapse behavior.
	// Rationale: Server-side notification counts cannot be calculated correctly for
	// encrypted rooms (the most common case) since push rules require inspecting
	// message content. Clients MUST calculate these counts themselves from decrypted
	// content and push rules. Returning server-calculated values confuses clients
	// (like Element X) which expect to do client-side calculation.
	// See: MSC4186 spec note "Synapse always returns 0 for notification_count and highlight_count"
	// See: Synapse code comment at synapse/handlers/sliding_sync/__init__.py:1365-1367
	roomData.NotificationCount = 0
	roomData.HighlightCount = 0

	// Phase 8: Add member counts
	// Use max(PDUPosition, InvitePosition) to ensure we see latest membership state
	// This is critical for invite counts which arrive via Invite stream
	countPos := currentPos.PDUPosition
	if currentPos.InvitePosition > countPos {
		countPos = currentPos.InvitePosition
	}

	joinedCount, err := snapshot.MembershipCount(ctx, roomID, "join", countPos)
	if err != nil {
		logrus.WithError(err).WithField("room_id", roomID).Error("Failed to get joined member count")
		// Continue anyway, return room without joined count
	} else {
		roomData.JoinedCount = joinedCount
	}

	invitedCount, err := snapshot.MembershipCount(ctx, roomID, "invite", countPos)
	if err != nil {
		logrus.WithError(err).WithField("room_id", roomID).Error("Failed to get invited member count")
		// Continue anyway, return room without invited count
	} else {
		roomData.InvitedCount = invitedCount
	}

	// Phase 9: Add missing room fields
	// Use the Limited value from database layer (more accurate than manually checking len >= limit)
	roomData.Limited = timelineLimited

	// If limited, generate prev_batch token for pagination
	if timelineLimited && len(roomData.Timeline) > 0 {
		// Use earliest event's position for prev_batch
		// The earliest event is at index 0 (chronological order, oldest first)
		earliestEvent := roomData.Timeline[0]
		if earliestEvent.EventID != "" {
			// Get the topological position of the earliest event
			topologyToken, err := snapshot.EventPositionInTopology(ctx, earliestEvent.EventID)
			if err != nil {
				logrus.WithError(err).WithField("event_id", earliestEvent.EventID).Error("Failed to get topology position for prev_batch")
				// Continue without prev_batch - client can still use the room
			} else {
				// Decrement the token to point to the position BEFORE this event
				// This matches the behavior in /messages handler (messages.go:433)
				topologyToken.Decrement()
				roomData.PrevBatch = topologyToken.String()
			}
		}
	}

	// Set num_live from getTimelineEvents (calculated using Synapse's algorithm)
	// num_live indicates how many timeline events are "live" (arrived after sync request's since token)
	// vs historical. This is critical for clients like Element X to determine if events should
	// trigger notifications. See getTimelineEvents for the algorithm implementation.
	roomData.NumLive = numLive

	// Phase 11: Add is_dm (direct message flag from m.direct account data)
	roomData.IsDM = rp.isDirectMessage(ctx, roomID, userID)

	// Phase 11: Calculate bump_stamp (timestamp of most recent event)
	// BumpStamp is used for client-side room sorting by recency
	roomData.BumpStamp = rp.calculateBumpStamp(ctx, snapshot, roomID, roomData.Timeline)

	// Phase 12: Add heroes (MSC4186Hero with displayname/avatar)
	// Heroes are used for rooms without explicit names to show "User A, User B" style names
	roomData.Heroes = rp.getHeroes(ctx, snapshot, roomID, userID)

	return roomData, nil
}

// getTimelineEvents retrieves recent timeline events for a room
// For initial syncs (NEVER), gets historical events
// For incremental syncs (LIVE/PREVIOUSLY), gets only new events since last sync
// Returns the events, whether the timeline was limited (truncated due to hitting the limit), and num_live count
// fromToken is used to calculate num_live (how many events arrived after the sync request's since token)
func (rp *RequestPool) getTimelineEvents(
	ctx context.Context,
	snapshot storage.DatabaseTransaction,
	roomID string,
	userID string,
	limit int,
	roomState types.RoomStreamState,
	currentPos types.StreamingToken,
	fromToken *types.StreamingToken,
) (timeline []synctypes.ClientEvent, limited bool, numLive int, err error) {
	// Create a trace region for timeline event retrieval
	timelineRegion, _ := internal.StartRegion(ctx, "SlidingSync.getTimelineEvents")
	defer timelineRegion.EndRegion()
	timelineRegion.SetTag("room_id", roomID)
	timelineRegion.SetTag("limit", limit)
	timelineRegion.SetTag("current_pdu_pos", currentPos.PDUPosition)
	if fromToken != nil {
		timelineRegion.SetTag("from_pdu_pos", fromToken.PDUPosition)
		timelineRegion.SetTag("is_incremental", true)
	} else {
		timelineRegion.SetTag("is_incremental", false)
	}

	// Create a filter with the limit
	filter := synctypes.RoomEventFilter{
		Limit: limit,
	}

	// CRITICAL: Determine range based on SYNC-LEVEL token, not per-room state
	// Match the logic used for Initial flag - if sync has since token, it's incremental for ALL rooms
	// This fixes Element X badge issues where room_subscriptions got full history on incremental syncs
	var fromPos, toPos types.StreamPosition
	fromPos = currentPos.PDUPosition // Always go backwards from current

	if fromToken == nil {
		// Initial sync (no since token) - get recent historical events
		toPos = 0
		logrus.WithFields(logrus.Fields{
			"room_id": roomID,
			"from":    fromPos,
			"to":      toPos,
			"mode":    "historical (no since token)",
		}).Debug("[TIMELINE] Fetching historical events for initial sync")
	} else {
		// Incremental sync (has since token) - only get NEW events since the sync's since token
		// Use sync-level token, NOT per-room state (fixes room_subscriptions returning full history)
		toPos = fromToken.PDUPosition
		logrus.WithFields(logrus.Fields{
			"room_id": roomID,
			"from":    fromPos,
			"to":      toPos,
			"mode":    "incremental (since token)",
		}).Debug("[TIMELINE] Fetching incremental events since sync token")
	}

	// Get events in the determined range
	recentEvents, err := snapshot.RecentEvents(
		ctx,
		[]string{roomID},
		types.Range{
			From:      fromPos, // High value (current position)
			To:        toPos,   // Low value (0 for initial, lastSentPos for incremental)
			Backwards: true,    // Get most recent first, limit will apply
		},
		&filter,
		true, // Chronological order (oldest first)
		true, // Only sync events
	)
	if err != nil {
		return nil, false, 0, err
	}

	events, ok := recentEvents[roomID]
	if !ok || len(events.Events) == 0 {
		return []synctypes.ClientEvent{}, false, 0, nil
	}

	// Calculate num_live BEFORE converting to ClientEvents (while we have StreamPosition)
	// Uses Synapse's algorithm: Count how many events arrived after the request's from_token
	// This is connection-level logic (based on request token), not room-level logic
	numLive = 0
	if fromToken != nil {
		// Iterate in reverse chronological order and break early when hitting historical events
		for i := len(events.Events) - 1; i >= 0; i-- {
			eventPos := events.Events[i].StreamPosition
			// Compare event position to the sync request's from_token PDU position
			if eventPos > fromToken.PDUPosition {
				numLive++
			} else {
				// Optimization from Synapse: break once we hit an event that's not live
				break
			}
		}
	}

	// Convert to ClientEvents
	clientEvents := make([]synctypes.ClientEvent, 0, len(events.Events))
	for _, event := range events.Events {
		clientEvent, err := synctypes.ToClientEvent(event, synctypes.FormatSync, func(roomID spec.RoomID, senderID spec.SenderID) (*spec.UserID, error) {
			return rp.rsAPI.QueryUserIDForSender(ctx, roomID, senderID)
		})
		if err != nil {
			logrus.WithError(err).WithField("event_id", event.EventID()).Warn("Failed to convert event to client format")
			continue
		}

		clientEvents = append(clientEvents, *clientEvent)
	}

	logrus.WithFields(logrus.Fields{
		"room_id":       roomID,
		"num_live":      numLive,
		"total":         len(clientEvents),
		"has_from_token": fromToken != nil,
		"limited":       events.Limited,
	}).Debug("[NUM_LIVE] Calculated num_live in getTimelineEvents")

	// Add output tags to trace
	timelineRegion.SetTag("events_returned", len(clientEvents))
	timelineRegion.SetTag("num_live", numLive)
	timelineRegion.SetTag("limited", events.Limited)

	// Return the events, limited flag, and num_live count
	return clientEvents, events.Limited, numLive, nil
}

// getRoomNameFromDB retrieves m.room.name state event from database
func (rp *RequestPool) getRoomNameFromDB(ctx context.Context, snapshot storage.DatabaseTransaction, roomID string) string {
	event, err := snapshot.GetStateEvent(ctx, roomID, "m.room.name", "")
	if err != nil || event == nil {
		return ""
	}

	return gjson.GetBytes(event.Content(), "name").Str
}

// getRoomAvatar retrieves m.room.avatar state event
func (rp *RequestPool) getRoomAvatar(ctx context.Context, snapshot storage.DatabaseTransaction, roomID string) string {
	event, err := snapshot.GetStateEvent(ctx, roomID, "m.room.avatar", "")
	if err != nil || event == nil {
		return ""
	}

	return gjson.GetBytes(event.Content(), "url").Str
}

// getRoomTopic retrieves m.room.topic state event
func (rp *RequestPool) getRoomTopic(ctx context.Context, snapshot storage.DatabaseTransaction, roomID string) string {
	event, err := snapshot.GetStateEvent(ctx, roomID, "m.room.topic", "")
	if err != nil || event == nil {
		return ""
	}

	return gjson.GetBytes(event.Content(), "topic").Str
}

// getRequiredState retrieves and filters state events based on required_state configuration
// Phase 4: Supports include/exclude patterns with wildcard matching
// Phase 5: Supports $LAZY pattern for lazy member loading
func (rp *RequestPool) getRequiredState(
	ctx context.Context,
	snapshot storage.DatabaseTransaction,
	roomID string,
	userID string,
	config *types.RequiredStateConfig,
	timeline []synctypes.ClientEvent,
) ([]synctypes.ClientEvent, error) {
	// Create a trace region for the getRequiredState operation
	reqStateRegion, _ := internal.StartRegion(ctx, "SlidingSync.getRequiredState")
	defer reqStateRegion.EndRegion()
	reqStateRegion.SetTag("room_id", roomID)

	// Phase 5: Extract lazy member senders if $LAZY is specified
	lazySenders := rp.extractLazySenders(config, timeline)

	// Get all current state events for the room
	// Pass empty filter to get all state
	emptyFilter := synctypes.StateFilter{}
	allState, err := snapshot.GetStateEventsForRoom(ctx, roomID, &emptyFilter)
	if err != nil {
		return nil, err
	}

	// Count member events in allState for debugging
	memberEventCount := 0
	for _, ev := range allState {
		if ev.Type() == "m.room.member" {
			memberEventCount++
		}
	}
	reqStateRegion.SetTag("total_state_events", len(allState))
	reqStateRegion.SetTag("member_events_in_db", memberEventCount)

	// Filter based on include/exclude patterns
	var filtered []*rstypes.HeaderedEvent
	for _, event := range allState {
		if rp.matchesRequiredState(event, userID, config, lazySenders) {
			filtered = append(filtered, event)
		}
	}

	// Count member events that passed filtering
	filteredMemberCount := 0
	for _, event := range filtered {
		if event.Type() == "m.room.member" {
			filteredMemberCount++
		}
	}
	reqStateRegion.SetTag("filtered_state_events", len(filtered))
	reqStateRegion.SetTag("member_events_returned", filteredMemberCount)

	// Convert to ClientEvents
	clientEvents := make([]synctypes.ClientEvent, 0, len(filtered))
	for _, event := range filtered {
		clientEvent, err := synctypes.ToClientEvent(event, synctypes.FormatAll, func(roomID spec.RoomID, senderID spec.SenderID) (*spec.UserID, error) {
			return rp.rsAPI.QueryUserIDForSender(ctx, roomID, senderID)
		})
		if err != nil {
			logrus.WithError(err).WithField("event_id", event.EventID()).Warn("Failed to convert state event to client format")
			continue
		}
		clientEvents = append(clientEvents, *clientEvent)
	}

	return clientEvents, nil
}

// matchesRequiredState checks if an event matches the required_state configuration
// Phase 5: Added lazySenders parameter for $LAZY pattern matching
func (rp *RequestPool) matchesRequiredState(
	event *rstypes.HeaderedEvent,
	userID string,
	config *types.RequiredStateConfig,
	lazySenders map[string]bool,
) bool {
	eventType := event.Type()
	stateKey := ""
	if event.StateKey() != nil {
		stateKey = *event.StateKey()
	}

	// Check if excluded
	for _, pattern := range config.Exclude {
		if len(pattern) == 2 {
			if matchesPattern(eventType, pattern[0]) && matchesStateKeyPattern(stateKey, pattern[1], userID, lazySenders) {
				return false // Explicitly excluded
			}
		}
	}

	// Check if included
	for _, pattern := range config.Include {
		if len(pattern) == 2 {
			if matchesPattern(eventType, pattern[0]) && matchesStateKeyPattern(stateKey, pattern[1], userID, lazySenders) {
				// Debug: Log $ME membership matches
				if pattern[0] == "m.room.member" && pattern[1] == "$ME" {
					logrus.WithFields(logrus.Fields{
						"event_type": eventType,
						"state_key":  stateKey,
						"user_id":    userID,
						"event_id":   event.EventID(),
						"matched":    true,
					}).Debug("[REQUIRED_STATE] $ME membership pattern matched")
				}
				return true // Matches include pattern
			}
		}
	}

	return false // Not included
}

// matchesPattern checks if a value matches a pattern (supports "*" wildcard)
func matchesPattern(value, pattern string) bool {
	if pattern == "*" {
		return true
	}
	// TODO: Support prefix wildcards like "m.room.*"
	return value == pattern
}

// matchesStateKeyPattern checks if a state key matches a pattern
// Supports "*" wildcard, "$ME" (current user), and "$LAZY" (timeline senders)
func matchesStateKeyPattern(stateKey, pattern, userID string, lazySenders map[string]bool) bool {
	if pattern == "*" {
		return true
	}
	if pattern == "$ME" {
		return stateKey == userID
	}
	// Phase 5: $LAZY pattern - only include if in timeline senders
	if pattern == "$LAZY" {
		if lazySenders == nil {
			return false // No timeline, no lazy members
		}
		return lazySenders[stateKey]
	}
	return stateKey == pattern
}

// extractLazySenders extracts sender IDs from timeline events if $LAZY is specified
// Phase 5: Returns a map of sender IDs for lazy member loading
func (rp *RequestPool) extractLazySenders(config *types.RequiredStateConfig, timeline []synctypes.ClientEvent) map[string]bool {
	// Check if $LAZY pattern is present
	hasLazy := false
	for _, pattern := range config.Include {
		if len(pattern) == 2 && pattern[1] == "$LAZY" {
			hasLazy = true
			break
		}
	}

	if !hasLazy {
		return nil
	}

	// Extract unique sender IDs from timeline
	senders := make(map[string]bool)
	for _, event := range timeline {
		if event.Sender != "" {
			senders[event.Sender] = true
		}
	}

	return senders
}

// BumpEventTypes defines the event types that count as "activity" for bump_stamp calculation
// Per MSC4186/Synapse, only these events should bump a room to the top of the list
var BumpEventTypes = map[string]bool{
	"m.room.create":    true,
	"m.room.message":   true,
	"m.room.encrypted": true,
	"m.sticker":        true,
	"m.call.invite":    true,
	"m.poll.start":     true,
	"m.beacon_info":    true,
}

// calculateBumpStamp calculates the stream position of the most recent "bumping" event
// Returns an opaque integer (stream position) for use in client-side room sorting
// Per MSC4186: Only specific event types count as "bump" events
func (rp *RequestPool) calculateBumpStamp(
	ctx context.Context,
	snapshot storage.DatabaseTransaction,
	roomID string,
	timeline []synctypes.ClientEvent,
) int64 {
	// Strategy 1: Check timeline for the most recent bump event
	// Timeline is in chronological order (oldest first), so iterate backwards
	for i := len(timeline) - 1; i >= 0; i-- {
		event := timeline[i]
		if BumpEventTypes[event.Type] {
			// Found a bump event - use its stream position if available
			// Note: ClientEvent doesn't have stream position, so we'll use timestamp as fallback
			// The timestamp is still useful for sorting since newer events have higher timestamps
			return int64(event.OriginServerTS)
		}
	}

	// Strategy 2: No bump events in timeline - query database
	// Use the efficient MaxStreamPositionsForRooms query which filters by bump event types
	bumpStamps, err := snapshot.MaxStreamPositionsForRooms(ctx, []string{roomID})
	if err != nil {
		logrus.WithError(err).WithField("room_id", roomID).Warn("Failed to get bump_stamp from database")
		return 0
	}

	if pos, ok := bumpStamps[roomID]; ok {
		return int64(pos)
	}

	// No bump events found - use 0 (room will sort to bottom)
	return 0
}

// buildInviteRoomData constructs room data for an invited room
// Returns stripped state events for room preview (MSC4186 Section 4.3)
// Phase 11: Element X compatibility
// inviteEvent contains the invite_room_state in its unsigned field for federated invites
func (rp *RequestPool) buildInviteRoomData(
	ctx context.Context,
	snapshot storage.DatabaseTransaction,
	roomID string,
	userID string,
	isInitial bool,
	inviteEvent *rstypes.HeaderedEvent,
) (*types.SlidingRoomData, error) {
	roomData := &types.SlidingRoomData{
		Initial: isInitial,
	}

	var strippedEvents []synctypes.ClientEvent

	// For federated invites, the room state is embedded in the invite event's unsigned field
	// as "invite_room_state". This is the same approach V3 sync uses (see NewInviteResponse).
	if inviteEvent != nil {
		if inviteRoomState := gjson.GetBytes(inviteEvent.Unsigned(), "invite_room_state"); inviteRoomState.Exists() {
			// Parse the invite_room_state array
			for _, stateEvent := range inviteRoomState.Array() {
				// Convert raw JSON to ClientEvent
				clientEvent := synctypes.ClientEvent{
					Type:     stateEvent.Get("type").String(),
					StateKey: func() *string { s := stateEvent.Get("state_key").String(); return &s }(),
					Sender:   stateEvent.Get("sender").String(),
					Content:  spec.RawJSON(stateEvent.Get("content").Raw),
				}
				strippedEvents = append(strippedEvents, clientEvent)

				// Extract room metadata from stripped state
				switch clientEvent.Type {
				case "m.room.name":
					roomData.Name = gjson.GetBytes(clientEvent.Content, "name").String()
				case "m.room.avatar":
					roomData.AvatarURL = gjson.GetBytes(clientEvent.Content, "url").String()
				case "m.room.topic":
					roomData.Topic = gjson.GetBytes(clientEvent.Content, "topic").String()
				}
			}

			// Add the invite event itself (the m.room.member event for this user)
			inviteClientEvent, err := synctypes.ToClientEvent(inviteEvent, synctypes.FormatAll, func(roomID spec.RoomID, senderID spec.SenderID) (*spec.UserID, error) {
				return rp.rsAPI.QueryUserIDForSender(ctx, roomID, senderID)
			})
			if err == nil {
				// Clear unsigned to not expose internal data
				inviteClientEvent.Unsigned = nil
				strippedEvents = append(strippedEvents, *inviteClientEvent)
			}
		}
	}

	// Fallback: If no invite_room_state (local invites), try to get state from local DB
	if len(strippedEvents) == 0 {
		strippedStateTypes := []struct {
			eventType string
			stateKey  string
		}{
			{"m.room.create", ""},
			{"m.room.name", ""},
			{"m.room.avatar", ""},
			{"m.room.topic", ""},
			{"m.room.join_rules", ""},
			{"m.room.encryption", ""},
			{"m.room.member", userID},
		}

		for _, stateType := range strippedStateTypes {
			event, err := snapshot.GetStateEvent(ctx, roomID, stateType.eventType, stateType.stateKey)
			if err != nil || event == nil {
				continue
			}

			clientEvent, err := synctypes.ToClientEvent(event, synctypes.FormatAll, func(roomID spec.RoomID, senderID spec.SenderID) (*spec.UserID, error) {
				return rp.rsAPI.QueryUserIDForSender(ctx, roomID, senderID)
			})
			if err != nil {
				continue
			}

			strippedEvents = append(strippedEvents, *clientEvent)
		}

		// Get metadata from local DB for local invites
		if roomData.Name == "" {
			roomData.Name = rp.getRoomNameFromDB(ctx, snapshot, roomID)
		}
		if roomData.AvatarURL == "" {
			roomData.AvatarURL = rp.getRoomAvatar(ctx, snapshot, roomID)
		}
		if roomData.Topic == "" {
			roomData.Topic = rp.getRoomTopic(ctx, snapshot, roomID)
		}
	}

	// Populate both fields for forward/backward compatibility
	// MSC4186 spec uses "stripped_state", Synapse/Element X use "invite_state"
	roomData.InviteState = strippedEvents
	roomData.StrippedState = strippedEvents

	logrus.WithFields(logrus.Fields{
		"room_id":       roomID,
		"stripped_count": len(strippedEvents),
		"name":          roomData.Name,
		"has_invite_event": inviteEvent != nil,
	}).Debug("[V4_SYNC] Built invite room data")

	return roomData, nil
}

// getHeroes fetches room heroes with displayname and avatar_url in MSC4186 format.
// Heroes are used for rooms without explicit names to show "User A, User B" style names.
// Per MSC4186: heroes should include up to 5 members (excluding the current user).
func (rp *RequestPool) getHeroes(
	ctx context.Context,
	snapshot storage.DatabaseTransaction,
	roomID string,
	userID string,
) []types.MSC4186Hero {
	// Get room summary which includes hero user IDs
	summary, err := snapshot.GetRoomSummary(ctx, roomID, userID)
	if err != nil {
		logrus.WithError(err).WithField("room_id", roomID).Warn("[V4_SYNC] Failed to get room summary for heroes")
		return nil
	}

	if len(summary.Heroes) == 0 {
		return nil
	}

	heroes := make([]types.MSC4186Hero, 0, len(summary.Heroes))

	// For each hero user ID, fetch their member event to get displayname and avatar_url
	for _, heroUserID := range summary.Heroes {
		hero := types.MSC4186Hero{
			UserID: heroUserID,
		}

		// Get the member event for this user
		memberEvent, err := snapshot.GetStateEvent(ctx, roomID, spec.MRoomMember, heroUserID)
		if err != nil || memberEvent == nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"room_id": roomID,
				"user_id": heroUserID,
			}).Debug("[V4_SYNC] Could not get member event for hero")
			// Still include the hero with just the user ID
			heroes = append(heroes, hero)
			continue
		}

		// Parse displayname and avatar_url from member event content
		content := memberEvent.Content()
		if displayname := gjson.GetBytes(content, "displayname").String(); displayname != "" {
			hero.Displayname = displayname
		}
		if avatarURL := gjson.GetBytes(content, "avatar_url").String(); avatarURL != "" {
			hero.AvatarURL = avatarURL
		}

		heroes = append(heroes, hero)
	}

	return heroes
}
