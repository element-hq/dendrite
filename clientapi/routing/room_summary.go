// Copyright 2024 New Vector Ltd.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package routing

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/element-hq/dendrite/federationapi/api"
	"github.com/element-hq/dendrite/internal/caching"
	rsAPI "github.com/element-hq/dendrite/roomserver/api"
	userapi "github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/fclient"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
)

// RoomSummaryResponse represents the response for MSC3266 room summary API
type RoomSummaryResponse struct {
	RoomID          string   `json:"room_id"`
	RoomType        string   `json:"room_type,omitempty"`
	Name            string   `json:"name,omitempty"`
	Topic           string   `json:"topic,omitempty"`
	AvatarURL       string   `json:"avatar_url,omitempty"`
	CanonicalAlias  string   `json:"canonical_alias,omitempty"`
	NumJoinedMembers int     `json:"num_joined_members"`
	GuestCanJoin    bool     `json:"guest_can_join"`
	WorldReadable   bool     `json:"world_readable"`
	JoinRule        string   `json:"join_rule,omitempty"`
	AllowedRoomIDs  []string `json:"allowed_room_ids,omitempty"`
	Encryption      string   `json:"im.nheko.summary.encryption,omitempty"` // Unstable prefix
	Membership      string   `json:"membership,omitempty"`
	RoomVersion     string   `json:"im.nheko.summary.room_version,omitempty"` // Unstable prefix
}

// GetRoomSummary implements MSC3266 room summary API
// GET /_matrix/client/unstable/im.nheko.summary/summary/{roomIdOrAlias}
// Supports both authenticated and unauthenticated requests.
// Unauthenticated requests can only access public/world-readable rooms.
func GetRoomSummary(
	req *http.Request,
	device *userapi.Device, // May be nil for unauthenticated requests
	roomIDOrAlias string,
	roomserverAPI rsAPI.ClientRoomserverAPI,
	fsAPI api.FederationInternalAPI,
	serverName spec.ServerName,
	cache caching.RoomSummaryCache,
) util.JSONResponse {
	ctx := req.Context()
	authenticated := device != nil

	// Parse via query parameters for federation
	vias := req.URL.Query()["via"]

	// Parse and validate room ID or alias
	roomID, jsonErr := parseRoomIDOrAlias(ctx, roomIDOrAlias, roomserverAPI)
	if jsonErr != nil {
		return *jsonErr
	}

	// Try to fetch room state locally first
	stateRes := &rsAPI.QueryBulkStateContentResponse{}
	err := roomserverAPI.QueryBulkStateContent(ctx, &rsAPI.QueryBulkStateContentRequest{
		RoomIDs:        []string{roomID},
		AllowWildcards: true,
		StateTuples: []gomatrixserverlib.StateKeyTuple{
			{EventType: spec.MRoomName, StateKey: ""},
			{EventType: spec.MRoomTopic, StateKey: ""},
			{EventType: spec.MRoomAvatar, StateKey: ""},
			{EventType: spec.MRoomCanonicalAlias, StateKey: ""},
			{EventType: spec.MRoomJoinRules, StateKey: ""},
			{EventType: spec.MRoomGuestAccess, StateKey: ""},
			{EventType: spec.MRoomHistoryVisibility, StateKey: ""},
			{EventType: spec.MRoomCreate, StateKey: ""},
			{EventType: spec.MRoomEncryption, StateKey: ""},
			{EventType: spec.MRoomMember, StateKey: "*"}, // Wildcard for member count
		},
	}, stateRes)
	if err != nil {
		util.GetLogger(ctx).WithError(err).Error("QueryBulkStateContent failed")
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}

	// Check if room exists locally
	roomState, roomExistsLocally := stateRes.Rooms[roomID]

	// If room doesn't exist locally, try federation
	if !roomExistsLocally {
		// Attempt to fetch via federation
		fedResponse := fetchRoomSummaryViaFederation(ctx, fsAPI, serverName, roomID, vias)
		if fedResponse != nil {
			return *fedResponse
		}

		// Federation failed, return 404
		return util.JSONResponse{
			Code: http.StatusNotFound,
			JSON: spec.NotFound("Room not found"),
		}
	}

	// Room exists locally - check access control
	var userID *spec.UserID
	var membership string

	if device != nil {
		// Authenticated request - get user ID and check full access
		parsedUserID, err := spec.NewUserID(device.UserID, true)
		if err != nil {
			util.GetLogger(ctx).WithError(err).Error("UserID is invalid")
			return util.JSONResponse{
				Code: http.StatusBadRequest,
				JSON: spec.InvalidParam("Device UserID is invalid"),
			}
		}
		userID = parsedUserID

		// Check access control (world-readable, public, or user membership)
		canAccess, userMembership := checkRoomAccess(ctx, roomserverAPI, roomID, *userID, roomState)
		if !canAccess {
			// Return 404 instead of 403 to not leak room existence
			return util.JSONResponse{
				Code: http.StatusNotFound,
				JSON: spec.NotFound("Room not found"),
			}
		}
		membership = userMembership
	} else {
		// Unauthenticated request - only allow public/world-readable rooms
		canAccess := checkUnauthenticatedAccess(roomState)
		if !canAccess {
			// Return 404 instead of 403 to not leak room existence
			return util.JSONResponse{
				Code: http.StatusNotFound,
				JSON: spec.NotFound("Room not found"),
			}
		}
		// Don't include membership for unauthenticated requests
		membership = ""
	}

	// Check cache for room summary (without user-specific membership)
	if cache != nil {
		if cachedResponse, ok := cache.GetRoomSummary(roomID, authenticated); ok {
			// Cache hit - convert to routing response type
			response := fromCacheResponse(cachedResponse)
			// Add user's membership if authenticated
			if authenticated && membership != "" {
				response.Membership = membership
			}
			return util.JSONResponse{
				Code: http.StatusOK,
				JSON: response,
			}
		}
	}

	// Cache miss - query room version and build response
	roomVersion := getRoomVersion(ctx, roomserverAPI, roomID)

	// Build response (without membership for caching)
	response := buildRoomSummaryResponse(roomID, roomState, "", roomVersion)

	// Store in cache (without user-specific membership)
	if cache != nil {
		cache.StoreRoomSummary(roomID, authenticated, toCacheResponse(response))
	}

	// Add user's membership to response if authenticated
	if authenticated && membership != "" {
		response.Membership = membership
	}

	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: response,
	}
}

// fetchRoomSummaryViaFederation attempts to fetch room summary via federation
// Returns nil if federation fails or room is not accessible
func fetchRoomSummaryViaFederation(
	ctx context.Context,
	fsAPI api.FederationInternalAPI,
	serverName spec.ServerName,
	roomID string,
	vias []string,
) *util.JSONResponse {
	// Extract server name from room ID if no via parameters provided
	if len(vias) == 0 {
		_, domain, err := gomatrixserverlib.SplitID('!', roomID)
		if err == nil {
			vias = []string{string(domain)}
		} else {
			return nil
		}
	}

	// Try each via server in sequence
	for _, via := range vias {
		if via == string(serverName) {
			// Skip our own server
			continue
		}

		// Call federation hierarchy endpoint
		res, err := fsAPI.RoomHierarchies(
			ctx,
			serverName,
			spec.ServerName(via),
			roomID,
			false, // suggestedOnly = false to get all room info
		)
		if err != nil {
			util.GetLogger(ctx).WithError(err).Warnf("Failed to fetch room hierarchy from %s", via)
			continue
		}

		// Convert federation hierarchy response to room summary
		summary := convertHierarchyToSummary(res.Room)

		return &util.JSONResponse{
			Code: http.StatusOK,
			JSON: summary,
		}
	}

	// All federation attempts failed
	return nil
}

// convertHierarchyToSummary converts a federation hierarchy room to a room summary response
func convertHierarchyToSummary(room fclient.RoomHierarchyRoom) RoomSummaryResponse {
	var roomType string
	if room.RoomType != nil {
		roomType = *room.RoomType
	}
	summary := RoomSummaryResponse{
		RoomID:           room.PublicRoom.RoomID,
		Name:             room.PublicRoom.Name,
		Topic:            room.PublicRoom.Topic,
		AvatarURL:        room.PublicRoom.AvatarURL,
		CanonicalAlias:   room.PublicRoom.CanonicalAlias,
		NumJoinedMembers: int(room.PublicRoom.JoinedMembersCount),
		GuestCanJoin:     room.PublicRoom.GuestCanJoin,
		WorldReadable:    room.PublicRoom.WorldReadable,
		JoinRule:         room.PublicRoom.JoinRule,
		RoomType:         roomType,
	}

	// Add allowed room IDs for restricted rooms
	if len(room.AllowedRoomIDs) > 0 {
		summary.AllowedRoomIDs = room.AllowedRoomIDs
	}

	// Note: Federation doesn't return membership, encryption, or room_version yet
	// These will be added in Phase 3 of the implementation

	return summary
}

// parseRoomIDOrAlias resolves a room alias to room ID, or validates a room ID
func parseRoomIDOrAlias(ctx context.Context, roomIDOrAlias string, roomserverAPI rsAPI.ClientRoomserverAPI) (string, *util.JSONResponse) {
	// Try parsing as room ID first
	if roomID, err := spec.NewRoomID(roomIDOrAlias); err == nil {
		return roomID.String(), nil
	}

	// Try parsing as room alias - validate it has correct format
	_, _, err := gomatrixserverlib.SplitID('#', roomIDOrAlias)
	if err != nil {
		return "", &util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.InvalidParam("Invalid room ID or alias"),
		}
	}

	// Resolve alias to room ID
	queryReq := &rsAPI.GetRoomIDForAliasRequest{
		Alias:              roomIDOrAlias,
		IncludeAppservices: true,
	}
	queryRes := &rsAPI.GetRoomIDForAliasResponse{}
	if err := roomserverAPI.GetRoomIDForAlias(ctx, queryReq, queryRes); err != nil {
		util.GetLogger(ctx).WithError(err).Error("GetRoomIDForAlias failed")
		return "", &util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}

	if queryRes.RoomID == "" {
		return "", &util.JSONResponse{
			Code: http.StatusNotFound,
			JSON: spec.NotFound("Room alias not found"),
		}
	}

	return queryRes.RoomID, nil
}

// checkRoomAccess determines if the user can access the room summary
// Returns (canAccess, membership)
func checkRoomAccess(
	ctx context.Context,
	roomserverAPI rsAPI.ClientRoomserverAPI,
	roomID string,
	userID spec.UserID,
	roomState map[gomatrixserverlib.StateKeyTuple]string,
) (bool, string) {
	// Get user's membership state (we'll need this regardless)
	membership := getUserMembership(ctx, roomserverAPI, roomID, userID)

	// Check if room is world-readable
	histVisKey := gomatrixserverlib.StateKeyTuple{
		EventType: spec.MRoomHistoryVisibility,
		StateKey:  "",
	}
	if visibility, ok := roomState[histVisKey]; ok && visibility == "world_readable" {
		// World-readable rooms can be accessed by anyone
		return true, membership
	}

	// Check if room is public (join_rule: "public")
	// QueryBulkStateContent returns the extracted join_rule value directly (e.g., "public")
	joinRuleKey := gomatrixserverlib.StateKeyTuple{
		EventType: spec.MRoomJoinRules,
		StateKey:  "",
	}
	if joinRuleContent, ok := roomState[joinRuleKey]; ok {
		if joinRuleContent == "public" {
			// Public rooms can be previewed by anyone
			return true, membership
		}
	}

	// Allow access if user is/was a member (join, invite, leave, ban)
	// This matches Synapse behavior - you can see summary of rooms you've been in
	if membership == "join" || membership == "invite" || membership == "leave" || membership == "ban" {
		return true, membership
	}

	// No access - not world-readable, not public, and user never joined
	return false, ""
}

// checkUnauthenticatedAccess determines if an unauthenticated user can access the room summary
// Only allows access to public or world-readable rooms
func checkUnauthenticatedAccess(
	roomState map[gomatrixserverlib.StateKeyTuple]string,
) bool {
	// Check if room is world-readable
	histVisKey := gomatrixserverlib.StateKeyTuple{
		EventType: spec.MRoomHistoryVisibility,
		StateKey:  "",
	}
	if visibility, ok := roomState[histVisKey]; ok && visibility == "world_readable" {
		// World-readable rooms can be accessed by anyone
		return true
	}

	// Check if room is public (join_rule: "public")
	// QueryBulkStateContent returns the extracted join_rule value directly (e.g., "public")
	joinRuleKey := gomatrixserverlib.StateKeyTuple{
		EventType: spec.MRoomJoinRules,
		StateKey:  "",
	}
	if joinRuleContent, ok := roomState[joinRuleKey]; ok {
		if joinRuleContent == "public" {
			// Public rooms can be previewed by anyone
			return true
		}
	}

	// Unauthenticated users cannot access private rooms
	return false
}

// getUserMembership gets the current membership state for a user in a room
func getUserMembership(ctx context.Context, roomserverAPI rsAPI.ClientRoomserverAPI, roomID string, userID spec.UserID) string {
	var membershipRes rsAPI.QueryMembershipForUserResponse
	err := roomserverAPI.QueryMembershipForUser(ctx, &rsAPI.QueryMembershipForUserRequest{
		RoomID: roomID,
		UserID: userID,
	}, &membershipRes)

	if err != nil {
		util.GetLogger(ctx).WithError(err).Error("QueryMembershipForUser failed")
		return ""
	}

	return membershipRes.Membership
}

// getRoomVersion queries the room version
func getRoomVersion(ctx context.Context, roomserverAPI rsAPI.ClientRoomserverAPI, roomID string) string {
	roomVersion, err := roomserverAPI.QueryRoomVersionForRoom(ctx, roomID)
	if err != nil {
		util.GetLogger(ctx).WithError(err).Error("QueryRoomVersionForRoom failed")
		return ""
	}

	return string(roomVersion)
}

// buildRoomSummaryResponse constructs the response from room state
func buildRoomSummaryResponse(
	roomID string,
	roomState map[gomatrixserverlib.StateKeyTuple]string,
	membership string,
	roomVersion string,
) RoomSummaryResponse {
	response := RoomSummaryResponse{
		RoomID:        roomID,
		Membership:    membership,
		RoomVersion:   roomVersion,
		GuestCanJoin:  false,
		WorldReadable: false,
	}

	// Extract state content values
	for tuple, content := range roomState {
		switch tuple.EventType {
		case spec.MRoomName:
			response.Name = content

		case spec.MRoomTopic:
			response.Topic = content

		case spec.MRoomAvatar:
			response.AvatarURL = content

		case spec.MRoomCanonicalAlias:
			response.CanonicalAlias = content

		case spec.MRoomJoinRules:
			// Parse join rules content
			var joinRules struct {
				JoinRule string   `json:"join_rule"`
				Allow    []struct {
					RoomID string `json:"room_id"`
					Type   string `json:"type"`
				} `json:"allow"`
			}
			if err := json.Unmarshal([]byte(content), &joinRules); err == nil {
				response.JoinRule = joinRules.JoinRule

				// Extract allowed room IDs for restricted rooms
				if joinRules.JoinRule == "restricted" && len(joinRules.Allow) > 0 {
					allowedRooms := make([]string, 0, len(joinRules.Allow))
					for _, allow := range joinRules.Allow {
						if allow.Type == "m.room_membership" && allow.RoomID != "" {
							allowedRooms = append(allowedRooms, allow.RoomID)
						}
					}
					if len(allowedRooms) > 0 {
						response.AllowedRoomIDs = allowedRooms
					}
				}
			}

		case spec.MRoomGuestAccess:
			response.GuestCanJoin = content == "can_join"

		case spec.MRoomHistoryVisibility:
			response.WorldReadable = content == "world_readable"

		case spec.MRoomCreate:
			// Parse create event for room type
			var createContent struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(content), &createContent); err == nil {
				response.RoomType = createContent.Type
			}

		case spec.MRoomEncryption:
			// Parse encryption event for algorithm
			var encryptionContent struct {
				Algorithm string `json:"algorithm"`
			}
			if err := json.Unmarshal([]byte(content), &encryptionContent); err == nil {
				response.Encryption = encryptionContent.Algorithm
			}

		case spec.MRoomMember:
			// Count joined members
			if content == "join" {
				response.NumJoinedMembers++
			}
		}
	}

	return response
}

// toCacheResponse converts routing.RoomSummaryResponse to caching.RoomSummaryResponse
func toCacheResponse(r RoomSummaryResponse) caching.RoomSummaryResponse {
	return caching.RoomSummaryResponse{
		RoomID:           r.RoomID,
		RoomType:         r.RoomType,
		Name:             r.Name,
		Topic:            r.Topic,
		AvatarURL:        r.AvatarURL,
		CanonicalAlias:   r.CanonicalAlias,
		NumJoinedMembers: r.NumJoinedMembers,
		GuestCanJoin:     r.GuestCanJoin,
		WorldReadable:    r.WorldReadable,
		JoinRule:         r.JoinRule,
		AllowedRoomIDs:   r.AllowedRoomIDs,
		Encryption:       r.Encryption,
		Membership:       r.Membership,
		RoomVersion:      r.RoomVersion,
	}
}

// fromCacheResponse converts caching.RoomSummaryResponse to routing.RoomSummaryResponse
func fromCacheResponse(r caching.RoomSummaryResponse) RoomSummaryResponse {
	return RoomSummaryResponse{
		RoomID:           r.RoomID,
		RoomType:         r.RoomType,
		Name:             r.Name,
		Topic:            r.Topic,
		AvatarURL:        r.AvatarURL,
		CanonicalAlias:   r.CanonicalAlias,
		NumJoinedMembers: r.NumJoinedMembers,
		GuestCanJoin:     r.GuestCanJoin,
		WorldReadable:    r.WorldReadable,
		JoinRule:         r.JoinRule,
		AllowedRoomIDs:   r.AllowedRoomIDs,
		Encryption:       r.Encryption,
		Membership:       r.Membership,
		RoomVersion:      r.RoomVersion,
	}
}
