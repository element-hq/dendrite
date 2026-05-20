// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package sync

import (
	"testing"

	"github.com/element-hq/dendrite/syncapi/synctypes"
	"github.com/element-hq/dendrite/syncapi/types"
	"github.com/stretchr/testify/assert"
)

// TestMatchesPattern tests the event type pattern matching logic
func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		pattern  string
		expected bool
	}{
		{
			name:     "exact match",
			value:    "m.room.message",
			pattern:  "m.room.message",
			expected: true,
		},
		{
			name:     "exact mismatch",
			value:    "m.room.message",
			pattern:  "m.room.name",
			expected: false,
		},
		{
			name:     "wildcard matches anything",
			value:    "m.room.message",
			pattern:  "*",
			expected: true,
		},
		{
			name:     "wildcard matches empty string",
			value:    "",
			pattern:  "*",
			expected: true,
		},
		{
			name:     "wildcard matches m.room.create",
			value:    "m.room.create",
			pattern:  "*",
			expected: true,
		},
		{
			name:     "wildcard matches custom event type",
			value:    "com.example.custom",
			pattern:  "*",
			expected: true,
		},
		{
			name:     "empty pattern only matches empty value",
			value:    "",
			pattern:  "",
			expected: true,
		},
		{
			name:     "empty pattern does not match non-empty value",
			value:    "m.room.message",
			pattern:  "",
			expected: false,
		},
		{
			name:     "case sensitive exact match",
			value:    "M.Room.Message",
			pattern:  "m.room.message",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesPattern(tt.value, tt.pattern)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMatchesStateKeyPattern tests the state key pattern matching logic
// Includes $ME, $LAZY, and wildcard patterns
func TestMatchesStateKeyPattern(t *testing.T) {
	userID := "@alice:example.com"
	lazySenders := map[string]bool{
		"@bob:example.com":   true,
		"@carol:example.com": true,
	}

	tests := []struct {
		name        string
		stateKey    string
		pattern     string
		userID      string
		lazySenders map[string]bool
		expected    bool
	}{
		// Exact matches
		{
			name:        "exact match - room creator",
			stateKey:    "",
			pattern:     "",
			userID:      userID,
			lazySenders: nil,
			expected:    true,
		},
		{
			name:        "exact match - specific user",
			stateKey:    "@bob:example.com",
			pattern:     "@bob:example.com",
			userID:      userID,
			lazySenders: nil,
			expected:    true,
		},
		{
			name:        "exact mismatch",
			stateKey:    "@bob:example.com",
			pattern:     "@carol:example.com",
			userID:      userID,
			lazySenders: nil,
			expected:    false,
		},

		// Wildcard
		{
			name:        "wildcard matches any state key",
			stateKey:    "@anyone:example.com",
			pattern:     "*",
			userID:      userID,
			lazySenders: nil,
			expected:    true,
		},
		{
			name:        "wildcard matches empty state key",
			stateKey:    "",
			pattern:     "*",
			userID:      userID,
			lazySenders: nil,
			expected:    true,
		},

		// $ME pattern
		{
			name:        "$ME matches current user",
			stateKey:    "@alice:example.com",
			pattern:     "$ME",
			userID:      userID,
			lazySenders: nil,
			expected:    true,
		},
		{
			name:        "$ME does not match other user",
			stateKey:    "@bob:example.com",
			pattern:     "$ME",
			userID:      userID,
			lazySenders: nil,
			expected:    false,
		},
		{
			name:        "$ME does not match empty state key",
			stateKey:    "",
			pattern:     "$ME",
			userID:      userID,
			lazySenders: nil,
			expected:    false,
		},

		// $LAZY pattern
		{
			name:        "$LAZY matches sender in timeline",
			stateKey:    "@bob:example.com",
			pattern:     "$LAZY",
			userID:      userID,
			lazySenders: lazySenders,
			expected:    true,
		},
		{
			name:        "$LAZY matches another sender in timeline",
			stateKey:    "@carol:example.com",
			pattern:     "$LAZY",
			userID:      userID,
			lazySenders: lazySenders,
			expected:    true,
		},
		{
			name:        "$LAZY does not match non-sender",
			stateKey:    "@dave:example.com",
			pattern:     "$LAZY",
			userID:      userID,
			lazySenders: lazySenders,
			expected:    false,
		},
		{
			name:        "$LAZY with nil lazySenders returns false",
			stateKey:    "@bob:example.com",
			pattern:     "$LAZY",
			userID:      userID,
			lazySenders: nil,
			expected:    false,
		},
		{
			name:        "$LAZY with empty lazySenders returns false",
			stateKey:    "@bob:example.com",
			pattern:     "$LAZY",
			userID:      userID,
			lazySenders: map[string]bool{},
			expected:    false,
		},

		// Edge cases
		{
			name:        "literal $ME string does not match",
			stateKey:    "$ME",
			pattern:     "$ME",
			userID:      userID,
			lazySenders: nil,
			expected:    false, // $ME is interpreted as current user pattern
		},
		{
			name:        "literal $LAZY string does not match",
			stateKey:    "$LAZY",
			pattern:     "$LAZY",
			userID:      userID,
			lazySenders: nil,
			expected:    false, // $LAZY with nil lazySenders returns false
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesStateKeyPattern(tt.stateKey, tt.pattern, tt.userID, tt.lazySenders)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractLazySenders tests extraction of sender IDs from timeline events
func TestExtractLazySenders(t *testing.T) {
	tests := []struct {
		name        string
		config      *types.RequiredStateConfig
		timeline    []synctypes.ClientEvent
		wantSenders map[string]bool
	}{
		{
			name: "no $LAZY pattern - returns nil",
			config: &types.RequiredStateConfig{
				Include: [][]string{
					{"m.room.name", ""},
					{"m.room.member", "$ME"},
				},
			},
			timeline: []synctypes.ClientEvent{
				{Sender: "@alice:test"},
				{Sender: "@bob:test"},
			},
			wantSenders: nil,
		},
		{
			name: "$LAZY pattern extracts senders",
			config: &types.RequiredStateConfig{
				Include: [][]string{
					{"m.room.member", "$LAZY"},
				},
			},
			timeline: []synctypes.ClientEvent{
				{Sender: "@alice:test"},
				{Sender: "@bob:test"},
				{Sender: "@carol:test"},
			},
			wantSenders: map[string]bool{
				"@alice:test": true,
				"@bob:test":   true,
				"@carol:test": true,
			},
		},
		{
			name: "$LAZY with duplicate senders - deduplicated",
			config: &types.RequiredStateConfig{
				Include: [][]string{
					{"m.room.member", "$LAZY"},
				},
			},
			timeline: []synctypes.ClientEvent{
				{Sender: "@alice:test"},
				{Sender: "@bob:test"},
				{Sender: "@alice:test"}, // Duplicate
				{Sender: "@bob:test"},   // Duplicate
			},
			wantSenders: map[string]bool{
				"@alice:test": true,
				"@bob:test":   true,
			},
		},
		{
			name: "$LAZY with empty timeline - returns empty map",
			config: &types.RequiredStateConfig{
				Include: [][]string{
					{"m.room.member", "$LAZY"},
				},
			},
			timeline:    []synctypes.ClientEvent{},
			wantSenders: map[string]bool{},
		},
		{
			name: "$LAZY skips empty sender",
			config: &types.RequiredStateConfig{
				Include: [][]string{
					{"m.room.member", "$LAZY"},
				},
			},
			timeline: []synctypes.ClientEvent{
				{Sender: "@alice:test"},
				{Sender: ""}, // Empty sender
				{Sender: "@bob:test"},
			},
			wantSenders: map[string]bool{
				"@alice:test": true,
				"@bob:test":   true,
			},
		},
		{
			name: "$LAZY with other patterns - still extracts",
			config: &types.RequiredStateConfig{
				Include: [][]string{
					{"m.room.name", ""},
					{"m.room.member", "$ME"},
					{"m.room.member", "$LAZY"}, // $LAZY is here
					{"m.room.create", ""},
				},
			},
			timeline: []synctypes.ClientEvent{
				{Sender: "@alice:test"},
			},
			wantSenders: map[string]bool{
				"@alice:test": true,
			},
		},
		{
			name: "nil config - returns nil",
			config: &types.RequiredStateConfig{
				Include: nil,
			},
			timeline: []synctypes.ClientEvent{
				{Sender: "@alice:test"},
			},
			wantSenders: nil,
		},
		{
			name: "malformed pattern (single element) - ignored",
			config: &types.RequiredStateConfig{
				Include: [][]string{
					{"m.room.member"}, // Missing state key pattern
					{"m.room.member", "$LAZY"},
				},
			},
			timeline: []synctypes.ClientEvent{
				{Sender: "@alice:test"},
			},
			wantSenders: map[string]bool{
				"@alice:test": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Need to create a dummy RequestPool to call extractLazySenders
			// Since it's a method on RequestPool but doesn't use any fields,
			// we can just use a nil-safe approach or test the logic directly
			rp := &RequestPool{}
			result := rp.extractLazySenders(tt.config, tt.timeline)
			assert.Equal(t, tt.wantSenders, result)
		})
	}
}

// TestBumpEventTypes tests that the BumpEventTypes map is correct
func TestBumpEventTypes(t *testing.T) {
	// Verify expected bump event types are included
	expectedBumpTypes := []string{
		"m.room.create",
		"m.room.message",
		"m.room.encrypted",
		"m.sticker",
		"m.call.invite",
		"m.poll.start",
		"m.beacon_info",
	}

	for _, eventType := range expectedBumpTypes {
		assert.True(t, BumpEventTypes[eventType], "Expected %s to be a bump event type", eventType)
	}

	// Verify non-bump types are NOT included
	nonBumpTypes := []string{
		"m.room.name",
		"m.room.topic",
		"m.room.member",
		"m.room.power_levels",
		"m.room.join_rules",
		"m.room.avatar",
		"m.reaction",
		"m.room.redaction",
	}

	for _, eventType := range nonBumpTypes {
		assert.False(t, BumpEventTypes[eventType], "Expected %s to NOT be a bump event type", eventType)
	}
}
