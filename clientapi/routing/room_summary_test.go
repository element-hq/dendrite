package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/element-hq/dendrite/internal/caching"
	rsapi "github.com/element-hq/dendrite/roomserver/api"
	"github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/spec"
)

// Mock roomserver API for room summary testing
type roomSummaryTestRoomserverAPI struct {
	rsapi.ClientRoomserverAPI
	t *testing.T

	// Room data
	rooms map[string]*mockRoomData
}

type mockRoomData struct {
	roomID           string
	roomVersion      gomatrixserverlib.RoomVersion
	name             string
	topic            string
	avatarURL        string
	canonicalAlias   string
	joinRule         string
	historyVis       string
	guestAccess      string
	roomType         string
	encryption       string
	memberCount      int
	userMemberships  map[string]string // userID -> membership
}

func newRoomSummaryTestRoomserverAPI(t *testing.T) *roomSummaryTestRoomserverAPI {
	return &roomSummaryTestRoomserverAPI{
		t:     t,
		rooms: make(map[string]*mockRoomData),
	}
}

func (r *roomSummaryTestRoomserverAPI) addRoom(room *mockRoomData) {
	r.rooms[room.roomID] = room
}

func (r *roomSummaryTestRoomserverAPI) QueryBulkStateContent(
	ctx context.Context,
	req *rsapi.QueryBulkStateContentRequest,
	res *rsapi.QueryBulkStateContentResponse,
) error {
	res.Rooms = make(map[string]map[gomatrixserverlib.StateKeyTuple]string)

	for _, roomID := range req.RoomIDs {
		room, ok := r.rooms[roomID]
		if !ok {
			continue
		}

		roomState := make(map[gomatrixserverlib.StateKeyTuple]string)

		for _, tuple := range req.StateTuples {
			switch tuple.EventType {
			case spec.MRoomName:
				if room.name != "" {
					roomState[tuple] = room.name
				}
			case spec.MRoomTopic:
				if room.topic != "" {
					roomState[tuple] = room.topic
				}
			case "m.room.avatar":
				if room.avatarURL != "" {
					roomState[tuple] = room.avatarURL
				}
			case spec.MRoomCanonicalAlias:
				if room.canonicalAlias != "" {
					roomState[tuple] = room.canonicalAlias
				}
			case spec.MRoomJoinRules:
				if room.joinRule != "" {
					roomState[tuple] = room.joinRule
				}
			case spec.MRoomHistoryVisibility:
				if room.historyVis != "" {
					roomState[tuple] = room.historyVis
				}
			case "m.room.guest_access":
				if room.guestAccess != "" {
					roomState[tuple] = room.guestAccess
				}
			case spec.MRoomCreate:
				if room.roomType != "" {
					content, _ := json.Marshal(map[string]string{"type": room.roomType})
					roomState[tuple] = string(content)
				} else {
					roomState[tuple] = "{}"
				}
			case spec.MRoomEncryption:
				if room.encryption != "" {
					content, _ := json.Marshal(map[string]string{"algorithm": room.encryption})
					roomState[tuple] = string(content)
				}
			case spec.MRoomMember:
				// Handle wildcard for member count
				if tuple.StateKey == "*" && req.AllowWildcards {
					for i := 0; i < room.memberCount; i++ {
						memberTuple := gomatrixserverlib.StateKeyTuple{
							EventType: spec.MRoomMember,
							StateKey:  fmt.Sprintf("@member%d:test", i), // Unique state key per member
						}
						roomState[memberTuple] = "join"
					}
				}
			}
		}

		res.Rooms[roomID] = roomState
	}

	return nil
}

func (r *roomSummaryTestRoomserverAPI) QueryMembershipForUser(
	ctx context.Context,
	req *rsapi.QueryMembershipForUserRequest,
	res *rsapi.QueryMembershipForUserResponse,
) error {
	room, ok := r.rooms[req.RoomID]
	if !ok {
		return nil
	}

	if membership, ok := room.userMemberships[req.UserID.String()]; ok {
		res.Membership = membership
		res.IsInRoom = membership == "join"
	}

	return nil
}

func (r *roomSummaryTestRoomserverAPI) QueryRoomVersionForRoom(
	ctx context.Context,
	roomID string,
) (gomatrixserverlib.RoomVersion, error) {
	room, ok := r.rooms[roomID]
	if !ok {
		return "", nil
	}
	return room.roomVersion, nil
}

func (r *roomSummaryTestRoomserverAPI) GetRoomIDForAlias(
	ctx context.Context,
	req *rsapi.GetRoomIDForAliasRequest,
	res *rsapi.GetRoomIDForAliasResponse,
) error {
	// Simple alias lookup - check if any room has this canonical alias
	for roomID, room := range r.rooms {
		if room.canonicalAlias == req.Alias {
			res.RoomID = roomID
			return nil
		}
	}
	return nil
}

func TestGetRoomSummary(t *testing.T) {
	testCases := []struct {
		name           string
		roomID         string
		room           *mockRoomData
		userID         string // empty for unauthenticated
		expectedCode   int
		expectedFields map[string]interface{}
	}{
		{
			name:   "public room - unauthenticated",
			roomID: "!public:test",
			room: &mockRoomData{
				roomID:      "!public:test",
				roomVersion: gomatrixserverlib.RoomVersionV10,
				name:        "Public Room",
				topic:       "A public room",
				joinRule:    "public",
				historyVis:  "shared",
				memberCount: 5,
			},
			userID:       "",
			expectedCode: http.StatusOK,
			expectedFields: map[string]interface{}{
				"room_id":            "!public:test",
				"name":               "Public Room",
				"topic":              "A public room",
				"join_rule":          "public",
				"num_joined_members": float64(5),
			},
		},
		{
			name:   "public room - authenticated member",
			roomID: "!public:test",
			room: &mockRoomData{
				roomID:          "!public:test",
				roomVersion:     gomatrixserverlib.RoomVersionV10,
				name:            "Public Room",
				joinRule:        "public",
				historyVis:      "shared",
				memberCount:     5,
				userMemberships: map[string]string{"@alice:test": "join"},
			},
			userID:       "@alice:test",
			expectedCode: http.StatusOK,
			expectedFields: map[string]interface{}{
				"room_id":    "!public:test",
				"membership": "join",
			},
		},
		{
			name:   "private room - unauthenticated should return 404",
			roomID: "!private:test",
			room: &mockRoomData{
				roomID:      "!private:test",
				roomVersion: gomatrixserverlib.RoomVersionV10,
				name:        "Private Room",
				joinRule:    "invite",
				historyVis:  "shared",
				memberCount: 2,
			},
			userID:       "",
			expectedCode: http.StatusNotFound,
		},
		{
			name:   "private room - authenticated member",
			roomID: "!private:test",
			room: &mockRoomData{
				roomID:          "!private:test",
				roomVersion:     gomatrixserverlib.RoomVersionV10,
				name:            "Private Room",
				joinRule:        "invite",
				historyVis:      "shared",
				memberCount:     2,
				userMemberships: map[string]string{"@bob:test": "join"},
			},
			userID:       "@bob:test",
			expectedCode: http.StatusOK,
			expectedFields: map[string]interface{}{
				"room_id":    "!private:test",
				"name":       "Private Room",
				"membership": "join",
			},
		},
		{
			name:   "private room - authenticated non-member should return 404",
			roomID: "!private:test",
			room: &mockRoomData{
				roomID:          "!private:test",
				roomVersion:     gomatrixserverlib.RoomVersionV10,
				name:            "Private Room",
				joinRule:        "invite",
				historyVis:      "shared",
				memberCount:     2,
				userMemberships: map[string]string{},
			},
			userID:       "@charlie:test",
			expectedCode: http.StatusNotFound,
		},
		{
			name:   "world-readable room - unauthenticated",
			roomID: "!worldreadable:test",
			room: &mockRoomData{
				roomID:      "!worldreadable:test",
				roomVersion: gomatrixserverlib.RoomVersionV10,
				name:        "World Readable Room",
				joinRule:    "invite",
				historyVis:  "world_readable",
				memberCount: 10,
			},
			userID:       "",
			expectedCode: http.StatusOK,
			expectedFields: map[string]interface{}{
				"room_id":        "!worldreadable:test",
				"world_readable": true,
			},
		},
		{
			name:   "encrypted room - returns encryption field",
			roomID: "!encrypted:test",
			room: &mockRoomData{
				roomID:          "!encrypted:test",
				roomVersion:     gomatrixserverlib.RoomVersionV10,
				name:            "Encrypted Room",
				joinRule:        "invite",
				historyVis:      "shared",
				encryption:      "m.megolm.v1.aes-sha2",
				memberCount:     3,
				userMemberships: map[string]string{"@alice:test": "join"},
			},
			userID:       "@alice:test",
			expectedCode: http.StatusOK,
			expectedFields: map[string]interface{}{
				"room_id":                       "!encrypted:test",
				"im.nheko.summary.encryption":   "m.megolm.v1.aes-sha2",
			},
		},
		{
			name:   "space - returns room_type",
			roomID: "!space:test",
			room: &mockRoomData{
				roomID:      "!space:test",
				roomVersion: gomatrixserverlib.RoomVersionV10,
				name:        "Test Space",
				joinRule:    "public",
				historyVis:  "shared",
				roomType:    "m.space",
				memberCount: 5,
			},
			userID:       "",
			expectedCode: http.StatusOK,
			expectedFields: map[string]interface{}{
				"room_id":   "!space:test",
				"room_type": "m.space",
			},
		},
		{
			name:   "room with room_version field",
			roomID: "!versioned:test",
			room: &mockRoomData{
				roomID:      "!versioned:test",
				roomVersion: gomatrixserverlib.RoomVersionV10,
				name:        "Versioned Room",
				joinRule:    "public",
				historyVis:  "shared",
				memberCount: 1,
			},
			userID:       "",
			expectedCode: http.StatusOK,
			expectedFields: map[string]interface{}{
				"room_id":                        "!versioned:test",
				"im.nheko.summary.room_version": "10",
			},
		},
		{
			name:         "nonexistent room - returns 404",
			roomID:       "!nonexistent:test",
			room:         nil,
			userID:       "",
			expectedCode: http.StatusNotFound,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rsAPI := newRoomSummaryTestRoomserverAPI(t)
			if tc.room != nil {
				rsAPI.addRoom(tc.room)
			}

			// Create request
			req := httptest.NewRequest(http.MethodGet, "/_matrix/client/unstable/im.nheko.summary/summary/"+tc.roomID, nil)

			// Create mock device for authenticated requests
			var device *api.Device
			if tc.userID != "" {
				device = &api.Device{
					UserID: tc.userID,
				}
			}

			// Call GetRoomSummary directly
			resp := GetRoomSummary(
				req,
				device,
				tc.roomID,
				rsAPI,
				nil, // fsAPI - not testing federation
				"test",
				nil, // cache - not testing caching
			)

			// Check status code
			if resp.Code != tc.expectedCode {
				t.Errorf("expected status %d, got %d", tc.expectedCode, resp.Code)
				if resp.JSON != nil {
					jsonBytes, _ := json.Marshal(resp.JSON)
					t.Errorf("response: %s", string(jsonBytes))
				}
				return
			}

			// Check expected fields for successful responses
			if tc.expectedCode == http.StatusOK && tc.expectedFields != nil {
				jsonBytes, err := json.Marshal(resp.JSON)
				if err != nil {
					t.Fatalf("failed to marshal response: %v", err)
				}

				var respMap map[string]interface{}
				if err := json.Unmarshal(jsonBytes, &respMap); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}

				for key, expectedValue := range tc.expectedFields {
					actualValue, ok := respMap[key]
					if !ok {
						t.Errorf("expected field %q not found in response", key)
						continue
					}
					if actualValue != expectedValue {
						t.Errorf("field %q: expected %v, got %v", key, expectedValue, actualValue)
					}
				}
			}
		})
	}
}

// TestRoomSummaryCache tests the caching behavior
func TestRoomSummaryCache(t *testing.T) {
	rsAPI := newRoomSummaryTestRoomserverAPI(t)
	rsAPI.addRoom(&mockRoomData{
		roomID:      "!cached:test",
		roomVersion: gomatrixserverlib.RoomVersionV10,
		name:        "Cached Room",
		joinRule:    "public",
		historyVis:  "shared",
		memberCount: 5,
	})

	// Create a real cache
	cache := &testRoomSummaryCache{
		data: make(map[string]caching.RoomSummaryResponse),
	}

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/unstable/im.nheko.summary/summary/!cached:test", nil)

	// First request - should populate cache
	resp1 := GetRoomSummary(req, nil, "!cached:test", rsAPI, nil, "test", cache)
	if resp1.Code != http.StatusOK {
		t.Fatalf("first request failed: %d", resp1.Code)
	}

	// Verify cache was populated
	if len(cache.data) == 0 {
		t.Error("cache should have been populated")
	}

	// Second request - should hit cache
	resp2 := GetRoomSummary(req, nil, "!cached:test", rsAPI, nil, "test", cache)
	if resp2.Code != http.StatusOK {
		t.Fatalf("second request failed: %d", resp2.Code)
	}

	// Both responses should be equivalent
	json1, _ := json.Marshal(resp1.JSON)
	json2, _ := json.Marshal(resp2.JSON)
	if string(json1) != string(json2) {
		t.Errorf("cached response differs from original:\noriginal: %s\ncached: %s", json1, json2)
	}
}

// Test cache implementation
type testRoomSummaryCache struct {
	data map[string]caching.RoomSummaryResponse
}

func (c *testRoomSummaryCache) GetRoomSummary(roomID string, authenticated bool) (caching.RoomSummaryResponse, bool) {
	key := roomID
	if authenticated {
		key += ":true"
	} else {
		key += ":false"
	}
	resp, ok := c.data[key]
	return resp, ok
}

func (c *testRoomSummaryCache) StoreRoomSummary(roomID string, authenticated bool, r caching.RoomSummaryResponse) {
	key := roomID
	if authenticated {
		key += ":true"
	} else {
		key += ":false"
	}
	c.data[key] = r
}

func (c *testRoomSummaryCache) InvalidateRoomSummary(roomID string) {
	delete(c.data, roomID+":true")
	delete(c.data, roomID+":false")
}

func TestParseRoomIDOrAlias(t *testing.T) {
	rsAPI := newRoomSummaryTestRoomserverAPI(t)
	rsAPI.addRoom(&mockRoomData{
		roomID:         "!aliased:test",
		roomVersion:    gomatrixserverlib.RoomVersionV10,
		canonicalAlias: "#test-room:test",
		joinRule:       "public",
	})

	testCases := []struct {
		name         string
		input        string
		expectedID   string
		expectError  bool
	}{
		{
			name:        "valid room ID",
			input:       "!room:test",
			expectedID:  "!room:test",
			expectError: false,
		},
		{
			name:        "valid alias - resolves",
			input:       "#test-room:test",
			expectedID:  "!aliased:test",
			expectError: false,
		},
		{
			name:        "invalid alias - not found",
			input:       "#nonexistent:test",
			expectedID:  "",
			expectError: true,
		},
		{
			name:        "invalid format",
			input:       "invalid",
			expectedID:  "",
			expectError: true,
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			roomID, jsonErr := parseRoomIDOrAlias(ctx, tc.input, rsAPI)
			if tc.expectError {
				if jsonErr == nil {
					t.Errorf("expected error, got roomID: %s", roomID)
				}
			} else {
				if jsonErr != nil {
					t.Errorf("unexpected error: %v", jsonErr)
				}
				if roomID != tc.expectedID {
					t.Errorf("expected roomID %s, got %s", tc.expectedID, roomID)
				}
			}
		})
	}
}
