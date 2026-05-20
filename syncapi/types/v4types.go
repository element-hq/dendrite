// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package types

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/element-hq/dendrite/syncapi/synctypes"
	"github.com/matrix-org/gomatrixserverlib"
)

// SlidingSyncStreamToken represents a position in the sliding sync stream.
// It combines a per-connection position with Dendrite's existing stream token.
// Format: "{connection_position}/{stream_token}"
// Example: "5/s478_0_100_50_0_13_0_0_0"
type SlidingSyncStreamToken struct {
	// Per-connection incremental position counter
	ConnectionPosition int64
	// Dendrite's existing stream token for global position tracking
	StreamToken StreamingToken
}

// String serializes the token to the format: "{connection_position}/{stream_token}"
func (t *SlidingSyncStreamToken) String() string {
	return fmt.Sprintf("%d/%s", t.ConnectionPosition, t.StreamToken.String())
}

// NewSlidingSyncStreamToken creates a new sliding sync token from components
func NewSlidingSyncStreamToken(connPos int64, streamToken StreamingToken) *SlidingSyncStreamToken {
	return &SlidingSyncStreamToken{
		ConnectionPosition: connPos,
		StreamToken:        streamToken,
	}
}

// ParseSlidingSyncStreamToken parses a sliding sync token from string format
func ParseSlidingSyncStreamToken(s string) (*SlidingSyncStreamToken, error) {
	if s == "" {
		// Empty token is valid for initial sync
		return nil, nil
	}

	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid sliding sync token format: expected 'connPos/streamToken', got '%s'", s)
	}

	connPos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid connection position in token: %w", err)
	}

	streamToken, err := NewStreamTokenFromString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid stream token in sliding sync token: %w", err)
	}

	return &SlidingSyncStreamToken{
		ConnectionPosition: connPos,
		StreamToken:        streamToken,
	}, nil
}

// SlidingSyncRequest represents the request body for POST /v4/sync
type SlidingSyncRequest struct {
	// Connection ID - identifies this connection for per-connection state tracking
	ConnID string `json:"conn_id,omitempty"`

	// Position token from previous response (omitted on initial sync)
	Pos string `json:"pos,omitempty"`

	// Milliseconds to wait for new events (for long-polling)
	Timeout int `json:"timeout,omitempty"`

	// Controls online status marking
	SetPresence string `json:"set_presence,omitempty"`

	// Named list configurations with sliding windows
	Lists map[string]SlidingListConfig `json:"lists,omitempty"`

	// Explicit room subscriptions by room ID
	RoomSubscriptions map[string]RoomSubscriptionConfig `json:"room_subscriptions,omitempty"`

	// Extension data requests (Phase 9: to_device, e2ee, account_data, receipts, typing)
	Extensions *ExtensionRequest `json:"extensions,omitempty"`
}

// SlidingListConfig defines a filtered, windowed view of rooms
type SlidingListConfig struct {
	// Maximum number of timeline events to return per room
	TimelineLimit int `json:"timeline_limit"`

	// State event filtering configuration
	RequiredState RequiredStateConfig `json:"required_state"`

	// Sliding window range [start, end] (inclusive). Omitted = no windowing
	// MSC4186 uses "range" (singular), MSC3575 used "ranges" (plural, nested array)
	// We support both for backwards compatibility
	Range []int `json:"range,omitempty"`

	// Room filtering criteria
	Filters *SlidingRoomFilter `json:"filters,omitempty"`
}

// UnmarshalJSON implements custom unmarshaling to support both "range" (MSC4186)
// and "ranges" (MSC3575) field names for backwards compatibility with older clients
func (c *SlidingListConfig) UnmarshalJSON(data []byte) error {
	// Define a type alias to avoid infinite recursion
	type Alias SlidingListConfig
	aux := &struct {
		*Alias
		// MSC3575 used "ranges" as an array of ranges: [[0,5], [10,15]]
		// MSC4186 simplified to "range" as a single range: [0,5]
		Ranges [][]int `json:"ranges,omitempty"`
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	// If "range" was not set but "ranges" was provided (MSC3575 compatibility)
	// Use the first range from the ranges array
	if len(c.Range) == 0 && len(aux.Ranges) > 0 && len(aux.Ranges[0]) == 2 {
		c.Range = aux.Ranges[0]
	}

	return nil
}

// RequiredStateConfig controls which state events to return
type RequiredStateConfig struct {
	// State event patterns to include (type, state_key pairs)
	// Supports wildcards: ["*", "*"], ["m.room.member", "$ME"], ["m.room.member", "$LAZY"]
	Include [][]string `json:"include,omitempty"`

	// State event patterns to exclude
	Exclude [][]string `json:"exclude,omitempty"`

	// Enable lazy-loaded memberships (only senders/targets from timeline)
	LazyMembers bool `json:"lazy_members,omitempty"`
}

// UnmarshalJSON implements custom unmarshaling to support shorthand array syntax
// Supports both:
//   - Object format: {"include": [...], "exclude": [...]}
//   - Shorthand array format: [["type", "key"], ...] (interpreted as include)
func (r *RequiredStateConfig) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as array first (shorthand syntax)
	var arr [][]string
	if err := json.Unmarshal(data, &arr); err == nil {
		// It's an array - interpret as "include"
		r.Include = arr
		r.Exclude = nil
		r.LazyMembers = false
		return nil
	}

	// Not an array, try as object with explicit fields
	type Alias RequiredStateConfig
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(r),
	}
	return json.Unmarshal(data, aux)
}

// SlidingRoomFilter contains criteria for filtering rooms in a list
type SlidingRoomFilter struct {
	// Filter to DM rooms only
	IsDM *bool `json:"is_dm,omitempty"`

	// Include rooms that are in these spaces (MSC4186)
	// NOTE: Not yet implemented - will return error if used
	Spaces []string `json:"spaces,omitempty"`

	// Filter by room name substring (case-insensitive)
	RoomNameLike *string `json:"room_name_like,omitempty"`

	// Filter to encrypted rooms only
	IsEncrypted *bool `json:"is_encrypted,omitempty"`

	// Filter to invites only
	IsInvite *bool `json:"is_invite,omitempty"`

	// Include rooms of these types (e.g., "m.space")
	RoomTypes []string `json:"room_types,omitempty"`

	// Exclude rooms of these types
	NotRoomTypes []string `json:"not_room_types,omitempty"`

	// Include rooms with these tags
	Tags []string `json:"tags,omitempty"`

	// Exclude rooms with these tags
	NotTags []string `json:"not_tags,omitempty"`
}

// RoomSubscriptionConfig for direct room subscriptions
type RoomSubscriptionConfig struct {
	// Maximum number of timeline events to return
	TimelineLimit int `json:"timeline_limit"`

	// State event filtering configuration
	RequiredState RequiredStateConfig `json:"required_state"`
}

// SlidingSyncResponse represents the response body for POST /v4/sync
type SlidingSyncResponse struct {
	// Position token for next request (required)
	Pos string `json:"pos"`

	// Updated list results
	// Always include lists key (even if empty) to match Synapse behavior
	Lists map[string]SlidingList `json:"lists"`

	// Changed rooms with their data
	// Always include rooms key (even if empty) to match Synapse behavior
	Rooms map[string]SlidingRoomData `json:"rooms"`

	// Extension responses (Phase 9: to_device, e2ee, account_data, receipts, typing)
	Extensions *ExtensionResponse `json:"extensions,omitempty"`
}

// SlidingList represents a list result with operations
type SlidingList struct {
	// Total count of rooms matching filters
	Count int `json:"count"`

	// Operations describing how to update the list
	Ops []SlidingOperation `json:"ops,omitempty"`
}

// SlidingOperation describes a change to a room list
type SlidingOperation struct {
	// Operation type: "SYNC", "INSERT", "DELETE", "INVALIDATE"
	Op string `json:"op"`

	// Range [start, end] for SYNC/INVALIDATE operations
	Range []int `json:"range,omitempty"`

	// Index for INSERT/DELETE operations
	Index *int `json:"index,omitempty"`

	// Room IDs for SYNC/INSERT operations
	RoomIDs []string `json:"room_ids,omitempty"`
}

// SlidingRoomData represents room data in the response
type SlidingRoomData struct {
	// Computed room name (from m.room.name or heroes)
	Name string `json:"name,omitempty"`

	// Room avatar URL
	AvatarURL string `json:"avatar_url,omitempty"`

	// Room topic
	Topic string `json:"topic,omitempty"`

	// Hero memberships for invites/knocks (up to 5)
	HeroMemberships []synctypes.ClientEvent `json:"hero_memberships,omitempty"`

	// True if this is the first time the room is sent on this connection
	Initial bool `json:"initial,omitempty"`

	// Required state events (filtered by required_state config)
	RequiredState []synctypes.ClientEvent `json:"required_state,omitempty"`

	// Timeline events (up to timeline_limit)
	// MSC4186: Field is named "timeline" (not "timeline_events" as previously incorrectly stated)
	Timeline []synctypes.ClientEvent `json:"timeline,omitempty"`

	// Unread highlight count (mentions, keywords)
	HighlightCount int `json:"highlight_count,omitempty"`

	// Unread notification count (all unread messages)
	NotificationCount int `json:"notification_count,omitempty"`

	// Number of joined members
	JoinedCount int `json:"joined_count,omitempty"`

	// Number of invited members
	InvitedCount int `json:"invited_count,omitempty"`

	// Bump stamp for client-side sorting (stream position of last bump event)
	BumpStamp int64 `json:"bump_stamp,omitempty"`

	// Stripped state for invite/knock/rejection rooms
	// MSC4186 spec uses "stripped_state" but Synapse/Element X use "invite_state"
	// We output both for forward/backward compatibility
	InviteState   []synctypes.ClientEvent `json:"invite_state,omitempty"`
	StrippedState []synctypes.ClientEvent `json:"stripped_state,omitempty"`

	// Phase 9: Additional room fields
	// Timeline was truncated (hit the limit)
	Limited bool `json:"limited,omitempty"`

	// Flag indicating we're returning more historic events due to timeline_limit increase
	// See MSC4186 "Changing room configs" section
	ExpandedTimeline bool `json:"expanded_timeline,omitempty"`

	// Pagination token for /messages endpoint (backwards pagination)
	PrevBatch string `json:"prev_batch,omitempty"`

	// Number of live events in timeline (vs historical/backfill)
	// Note: No omitempty - field should always be present per MSC4186 (matches Synapse behavior)
	NumLive int `json:"num_live"`

	// Direct message flag (from m.direct account data)
	IsDM bool `json:"is_dm,omitempty"`

	// Heroes for rooms without explicit name (MSC4186 format with displayname/avatar)
	Heroes []MSC4186Hero `json:"heroes,omitempty"`
}

// MSC4186Hero represents a hero member with display name and avatar (MSC4186 format)
// Used for rooms without explicit names to show "User A, User B" style names
type MSC4186Hero struct {
	UserID      string `json:"user_id"`
	Displayname string `json:"displayname,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
}

// ============================================================================
// Phase 9: Extension Types (to_device, e2ee, account_data, receipts, typing)
// ============================================================================
// Reference: /tmp/phase9_plan.md, /tmp/matrix_js_sdk_findings.md

// ExtensionRequest contains all extension requests from the client
type ExtensionRequest struct {
	ToDevice    *ToDeviceRequest    `json:"to_device,omitempty"`
	E2EE        *E2EERequest        `json:"e2ee,omitempty"`
	AccountData *AccountDataRequest `json:"account_data,omitempty"`
	Receipts    *ReceiptsRequest    `json:"receipts,omitempty"`
	Typing      *TypingRequest      `json:"typing,omitempty"`
}

// ToDeviceRequest configures to-device message extension
type ToDeviceRequest struct {
	Enabled bool   `json:"enabled"`
	Since   string `json:"since,omitempty"` // Token from previous next_batch
	Limit   int    `json:"limit,omitempty"` // Max events to return (client default: 100)
}

// E2EERequest configures E2EE device extension (MSC3884)
type E2EERequest struct {
	Enabled bool `json:"enabled"` // Sticky parameter
}

// AccountDataRequest configures account data extension
type AccountDataRequest struct {
	Enabled bool     `json:"enabled"`
	Lists   []string `json:"lists,omitempty"` // Optional list filter (MSC3959)
	Rooms   []string `json:"rooms,omitempty"` // Optional room filter (MSC3960)
}

// ReceiptsRequest configures read receipts extension
type ReceiptsRequest struct {
	Enabled bool     `json:"enabled"`
	Lists   []string `json:"lists,omitempty"` // Optional list filter (MSC3959)
	Rooms   []string `json:"rooms,omitempty"` // Optional room filter (MSC3960)
}

// TypingRequest configures typing notifications extension
type TypingRequest struct {
	Enabled bool     `json:"enabled"`
	Lists   []string `json:"lists,omitempty"` // Optional list filter (MSC3959)
	Rooms   []string `json:"rooms,omitempty"` // Optional room filter (MSC3960)
}

// ExtensionResponse contains all extension responses from the server
type ExtensionResponse struct {
	ToDevice    *V4ToDeviceResponse  `json:"to_device,omitempty"`
	E2EE        *E2EEResponse        `json:"e2ee,omitempty"`
	AccountData *AccountDataResponse `json:"account_data,omitempty"`
	Receipts    *ReceiptsResponse    `json:"receipts,omitempty"`
	Typing      *TypingResponse      `json:"typing,omitempty"`
}

// V4ToDeviceResponse contains to-device messages for v4 sliding sync
// Note: Different from v3's ToDeviceResponse - includes next_batch for stateful tracking
type V4ToDeviceResponse struct {
	NextBatch string                                   `json:"next_batch"` // Client tracks this for next request
	Events    []gomatrixserverlib.SendToDeviceEvent `json:"events"`
}

// E2EEResponse contains E2EE device extension data (MSC3884)
type E2EEResponse struct {
	// One-time key counts by algorithm (ALWAYS include signed_curve25519: N for Android compat)
	DeviceOneTimeKeysCount map[string]int `json:"device_one_time_keys_count,omitempty"`

	// Unused fallback key types
	// NOTE: No omitempty - field must be present even when empty for spec compliance
	DeviceUnusedFallbackKeyTypes []string `json:"device_unused_fallback_key_types"`

	// LEGACY: Support old MSC2732 field name for backwards compatibility
	// Client (matrix-js-sdk) checks both fields
	// NOTE: No omitempty - field must be present even when empty for spec compliance
	DeviceUnusedFallbackKeyTypesLegacy []string `json:"org.matrix.msc2732.device_unused_fallback_key_types"`

	// Device list changes (ONLY for incremental syncs, omitted on initial sync)
	// Uses existing DeviceLists type from types.go
	DeviceLists *DeviceLists `json:"device_lists,omitempty"`
}

// AccountDataResponse contains account data updates
type AccountDataResponse struct {
	Global []synctypes.ClientEvent            `json:"global"` // Global account data events
	Rooms  map[string][]synctypes.ClientEvent `json:"rooms"`  // Per-room account data events
}

// ReceiptsResponse contains read receipt updates
// IMPORTANT: Contains a SINGLE event per room, not an array (matrix-js-sdk expects this)
type ReceiptsResponse struct {
	Rooms map[string]synctypes.ClientEvent `json:"rooms"` // Single receipt event per room (no omitempty - clients expect this field even when empty per MSC3575/MSC4186)
}

// TypingResponse contains typing notification updates
// IMPORTANT: Contains a SINGLE event per room, not an array (matrix-js-sdk expects this)
type TypingResponse struct {
	Rooms map[string]synctypes.ClientEvent `json:"rooms"` // Single typing event per room (no omitempty - clients expect this field even when empty per MSC3575)
}

// ===== Database Schema Types (Phase 10: Delta Tracking) =====

// SlidingSyncStreamState tracks what data has been sent for a room/stream combination
// This is used to compute deltas - only sending changed data
type SlidingSyncStreamState struct {
	ConnectionPosition int64  // Position when this was last sent
	RoomID             string // Room ID
	Stream             string // Stream type: "events", "state", "account_data", "receipts", "typing"
	RoomStatus         string // "live" (currently in lists) or "previously" (sent before, not in current lists)
	LastToken          string // Stream token for what we've sent (for delta computation)
}

// SlidingSyncRoomConfig tracks what room config was used at a specific position
// This enables detecting config changes (timeline_limit increase, required_state expansion)
type SlidingSyncRoomConfig struct {
	ConnectionPosition int64  // Position when this config was used
	RoomID             string // Room ID
	TimelineLimit      int    // Timeline limit used
	RequiredStateID    int64  // FK to required_state table (deduplicated config)
}

// HaveSentRoomFlag tracks whether a room has been sent on a connection
// Based on Synapse's implementation for proper incremental sync
type HaveSentRoomFlag string

const (
	// HaveSentRoomNever indicates the room has never been sent on this connection
	// Timeline should be historical (topological ordering)
	// initial field should be true
	HaveSentRoomNever HaveSentRoomFlag = "never"

	// HaveSentRoomLive indicates the room was sent in the last response
	// All updates have been sent up to from_token
	// Timeline should be incremental (stream ordering from from_token)
	// initial field should be false
	HaveSentRoomLive HaveSentRoomFlag = "live"

	// HaveSentRoomPreviously indicates the room was sent before but not in last response
	// There are updates we haven't sent (stored in last_token)
	// Timeline should be incremental (stream ordering from last_token)
	// initial field should be false
	HaveSentRoomPreviously HaveSentRoomFlag = "previously"
)

// String returns the string representation of the flag
func (f HaveSentRoomFlag) String() string {
	return string(f)
}

// IsInitial returns true if this is the first time sending the room
func (f HaveSentRoomFlag) IsInitial() bool {
	return f == HaveSentRoomNever
}

// ShouldSendHistorical returns true if we should use historical (topological) ordering
func (f HaveSentRoomFlag) ShouldSendHistorical() bool {
	return f == HaveSentRoomNever
}

// RoomStreamState combines the flag with the last sent token for incremental updates
type RoomStreamState struct {
	Status    HaveSentRoomFlag
	LastToken *StreamingToken // Only set for HaveSentRoomPreviously
}
