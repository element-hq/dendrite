// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sync"
	"testing"

	"github.com/element-hq/dendrite/appservice/api"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/stretchr/testify/assert"
)

// createTestQueryAPI is a helper function that creates a test AppServiceQueryAPI
// with standard configuration. The namespaceMap parameter allows customization
// of namespace matching rules.
func createTestQueryAPI(srv *httptest.Server, namespaceMap map[string][]config.ApplicationServiceNamespace) *AppServiceQueryAPI {
	as := &config.ApplicationService{
		ID:              "test-as",
		URL:             srv.URL,
		ASToken:         "as-token",
		HSToken:         "hs-token",
		SenderLocalpart: "bot",
		NamespaceMap:    namespaceMap,
	}
	as.CreateHTTPClient(false)

	cfg := &config.AppServiceAPI{
		Derived: &config.Derived{},
	}
	cfg.Derived.ApplicationServices = []config.ApplicationService{*as}

	return &AppServiceQueryAPI{
		Cfg:           cfg,
		ProtocolCache: make(map[string]api.ASProtocolResponse),
		CacheMu:       sync.Mutex{},
	}
}

// createTestQueryAPIWithConfig is a helper function that creates a test AppServiceQueryAPI
// with custom configuration options (e.g., LegacyPaths, LegacyAuth).
func createTestQueryAPIWithConfig(srv *httptest.Server, namespaceMap map[string][]config.ApplicationServiceNamespace, configFn func(*config.AppServiceAPI)) *AppServiceQueryAPI {
	as := &config.ApplicationService{
		ID:              "test-as",
		URL:             srv.URL,
		ASToken:         "as-token",
		HSToken:         "hs-token",
		SenderLocalpart: "bot",
		NamespaceMap:    namespaceMap,
	}
	as.CreateHTTPClient(false)

	cfg := &config.AppServiceAPI{
		Derived: &config.Derived{},
	}
	cfg.Derived.ApplicationServices = []config.ApplicationService{*as}

	if configFn != nil {
		configFn(cfg)
	}

	return &AppServiceQueryAPI{
		Cfg:           cfg,
		ProtocolCache: make(map[string]api.ASProtocolResponse),
		CacheMu:       sync.Mutex{},
	}
}

// createTestQueryAPIWithProtocols is a helper function that creates a test AppServiceQueryAPI
// for protocol-related tests.
func createTestQueryAPIWithProtocols(srv *httptest.Server, protocols []string) *AppServiceQueryAPI {
	as := &config.ApplicationService{
		ID:              "test-as",
		URL:             srv.URL,
		ASToken:         "as-token",
		HSToken:         "hs-token",
		SenderLocalpart: "bot",
		Protocols:       protocols,
	}
	as.CreateHTTPClient(false)

	cfg := &config.AppServiceAPI{
		Derived: &config.Derived{},
	}
	cfg.Derived.ApplicationServices = []config.ApplicationService{*as}

	return &AppServiceQueryAPI{
		Cfg:           cfg,
		ProtocolCache: make(map[string]api.ASProtocolResponse),
		CacheMu:       sync.Mutex{},
	}
}

// TestRoomAliasExists_AliasFound_ReturnsTrue tests that when an AS acknowledges
// ownership of a room alias, RoomAliasExists returns true
func TestRoomAliasExists_AliasFound_ReturnsTrue(t *testing.T) {
	t.Parallel()

	// Create a test HTTP server that simulates an AS accepting the alias
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "#test:example.com")
		assert.Contains(t, r.Header.Get("Authorization"), "Bearer")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	queryAPI := createTestQueryAPI(srv, map[string][]config.ApplicationServiceNamespace{
		"aliases": {{RegexpObject: regexp.MustCompile("#test:.*")}},
	})

	req := &api.RoomAliasExistsRequest{Alias: "#test:example.com"}
	resp := &api.RoomAliasExistsResponse{}

	err := queryAPI.RoomAliasExists(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.True(t, resp.AliasExists, "Expected alias to exist")
}

// TestRoomAliasExists_AliasNotFound_ReturnsFalse tests that when an AS returns
// 404, RoomAliasExists returns false
func TestRoomAliasExists_AliasNotFound_ReturnsFalse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	queryAPI := createTestQueryAPI(srv, map[string][]config.ApplicationServiceNamespace{
		"aliases": {{RegexpObject: regexp.MustCompile("#test:.*")}},
	})

	req := &api.RoomAliasExistsRequest{Alias: "#test:example.com"}
	resp := &api.RoomAliasExistsResponse{}

	err := queryAPI.RoomAliasExists(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.False(t, resp.AliasExists, "Expected alias to not exist")
}

// TestRoomAliasExists_NoMatchingNamespace_ReturnsFalse tests that when no AS
// matches the alias namespace, RoomAliasExists returns false
func TestRoomAliasExists_NoMatchingNamespace_ReturnsFalse(t *testing.T) {
	t.Parallel()

	// No need for server since namespace won't match
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Should not be called when namespace doesn't match")
	}))
	defer srv.Close()

	queryAPI := createTestQueryAPI(srv, map[string][]config.ApplicationServiceNamespace{
		"aliases": {{RegexpObject: regexp.MustCompile("#other:.*")}},
	})

	req := &api.RoomAliasExistsRequest{Alias: "#test:example.com"}
	resp := &api.RoomAliasExistsResponse{}

	err := queryAPI.RoomAliasExists(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.False(t, resp.AliasExists, "Expected alias to not exist when no namespace matches")
}

// TestRoomAliasExists_EmptyURL_ReturnsFalse tests that AS with empty URL is skipped
func TestRoomAliasExists_EmptyURL_ReturnsFalse(t *testing.T) {
	t.Parallel()

	// Use empty server URL to simulate empty AS URL
	emptyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Should not be called with empty URL")
	}))
	emptyServer.Close() // Close immediately so URL is invalid

	queryAPI := createTestQueryAPI(emptyServer, map[string][]config.ApplicationServiceNamespace{
		"aliases": {{RegexpObject: regexp.MustCompile("#test:.*")}},
	})
	// Override URL to be empty
	queryAPI.Cfg.Derived.ApplicationServices[0].URL = ""

	req := &api.RoomAliasExistsRequest{Alias: "#test:example.com"}
	resp := &api.RoomAliasExistsResponse{}

	err := queryAPI.RoomAliasExists(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.False(t, resp.AliasExists, "Expected alias to not exist when AS URL is empty")
}

// TestRoomAliasExists_LegacyPaths_UsesCorrectPath tests legacy path support
func TestRoomAliasExists_LegacyPaths_UsesCorrectPath(t *testing.T) {
	t.Parallel()

	requestedPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	queryAPI := createTestQueryAPIWithConfig(srv, map[string][]config.ApplicationServiceNamespace{
		"aliases": {{RegexpObject: regexp.MustCompile("#test:.*")}},
	}, func(cfg *config.AppServiceAPI) {
		cfg.LegacyPaths = true
	})

	req := &api.RoomAliasExistsRequest{Alias: "#test:example.com"}
	resp := &api.RoomAliasExistsResponse{}

	err := queryAPI.RoomAliasExists(context.Background(), req, resp)

	assert.NoError(t, err)
	// Fixed: Use exact path match instead of weak assertion
	assert.Equal(t, "/rooms/#test:example.com", requestedPath,
		"Expected legacy path format")
}

// TestRoomAliasExists_ASReturnsError_LogsAndReturnsFalse tests error handling
func TestRoomAliasExists_ASReturnsError_LogsAndReturnsFalse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	queryAPI := createTestQueryAPI(srv, map[string][]config.ApplicationServiceNamespace{
		"aliases": {{RegexpObject: regexp.MustCompile("#test:.*")}},
	})

	req := &api.RoomAliasExistsRequest{Alias: "#test:example.com"}
	resp := &api.RoomAliasExistsResponse{}

	err := queryAPI.RoomAliasExists(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.False(t, resp.AliasExists, "Expected alias to not exist on AS error")
}

// TestUserIDExists_UserFound_ReturnsTrue tests that when an AS acknowledges
// ownership of a user ID, UserIDExists returns true
func TestUserIDExists_UserFound_ReturnsTrue(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "@test:example.com")
		assert.Contains(t, r.Header.Get("Authorization"), "Bearer")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	queryAPI := createTestQueryAPI(srv, map[string][]config.ApplicationServiceNamespace{
		"users": {{RegexpObject: regexp.MustCompile("@test:.*")}},
	})

	req := &api.UserIDExistsRequest{UserID: "@test:example.com"}
	resp := &api.UserIDExistsResponse{}

	err := queryAPI.UserIDExists(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.True(t, resp.UserIDExists, "Expected user to exist")
}

// TestUserIDExists_UserNotFound_ReturnsFalse tests that when an AS returns non-OK
// status, UserIDExists returns false
func TestUserIDExists_UserNotFound_ReturnsFalse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	queryAPI := createTestQueryAPI(srv, map[string][]config.ApplicationServiceNamespace{
		"users": {{RegexpObject: regexp.MustCompile("@test:.*")}},
	})

	req := &api.UserIDExistsRequest{UserID: "@test:example.com"}
	resp := &api.UserIDExistsResponse{}

	err := queryAPI.UserIDExists(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.False(t, resp.UserIDExists, "Expected user to not exist")
}

// TestUserIDExists_NoMatchingNamespace_ReturnsFalse tests that when no AS
// matches the user namespace, UserIDExists returns false
func TestUserIDExists_NoMatchingNamespace_ReturnsFalse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Should not be called when namespace doesn't match")
	}))
	defer srv.Close()

	queryAPI := createTestQueryAPI(srv, map[string][]config.ApplicationServiceNamespace{
		"users": {{RegexpObject: regexp.MustCompile("@other:.*")}},
	})

	req := &api.UserIDExistsRequest{UserID: "@test:example.com"}
	resp := &api.UserIDExistsResponse{}

	err := queryAPI.UserIDExists(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.False(t, resp.UserIDExists, "Expected user to not exist when no namespace matches")
}

// TestUserIDExists_EmptyURL_ReturnsFalse tests that AS with empty URL is skipped
func TestUserIDExists_EmptyURL_ReturnsFalse(t *testing.T) {
	t.Parallel()

	emptyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Should not be called with empty URL")
	}))
	emptyServer.Close()

	queryAPI := createTestQueryAPI(emptyServer, map[string][]config.ApplicationServiceNamespace{
		"users": {{RegexpObject: regexp.MustCompile("@test:.*")}},
	})
	queryAPI.Cfg.Derived.ApplicationServices[0].URL = ""

	req := &api.UserIDExistsRequest{UserID: "@test:example.com"}
	resp := &api.UserIDExistsResponse{}

	err := queryAPI.UserIDExists(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.False(t, resp.UserIDExists, "Expected user to not exist when AS URL is empty")
}

// TestLocations_ValidProtocol_ReturnsLocations tests successful location query
func TestLocations_ValidProtocol_ReturnsLocations(t *testing.T) {
	t.Parallel()

	expectedLocations := []api.ASLocationResponse{
		{
			Alias:    "#irc_#test:example.com",
			Protocol: "irc",
			Fields:   json.RawMessage(`{"network":"freenode","channel":"#test"}`),
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "irc")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedLocations)
	}))
	defer srv.Close()

	queryAPI := createTestQueryAPIWithProtocols(srv, []string{"irc"})

	req := &api.LocationRequest{Protocol: "irc", Params: ""}
	resp := &api.LocationResponse{}

	err := queryAPI.Locations(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.True(t, resp.Exists)
	assert.Len(t, resp.Locations, 1)
	assert.Equal(t, "irc", resp.Locations[0].Protocol)
}

// TestLocations_NoLocations_ReturnsEmpty tests when no locations are found
func TestLocations_NoLocations_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]api.ASLocationResponse{})
	}))
	defer srv.Close()

	queryAPI := createTestQueryAPIWithProtocols(srv, []string{"irc"})

	req := &api.LocationRequest{Protocol: "irc", Params: ""}
	resp := &api.LocationResponse{}

	err := queryAPI.Locations(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.False(t, resp.Exists)
	assert.Empty(t, resp.Locations)
}

// TestUser_ValidProtocol_ReturnsUsers tests successful user query
func TestUser_ValidProtocol_ReturnsUsers(t *testing.T) {
	t.Parallel()

	expectedUsers := []api.ASUserResponse{
		{
			Protocol: "irc",
			UserID:   "@irc_nick:example.com",
			Fields:   json.RawMessage(`{"network":"freenode","nick":"testnick"}`),
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "irc")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedUsers)
	}))
	defer srv.Close()

	queryAPI := createTestQueryAPIWithProtocols(srv, []string{"irc"})

	req := &api.UserRequest{Protocol: "irc", Params: ""}
	resp := &api.UserResponse{}

	err := queryAPI.User(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.True(t, resp.Exists)
	assert.Len(t, resp.Users, 1)
	assert.Equal(t, "irc", resp.Users[0].Protocol)
}

// TestProtocols_SingleProtocol_ReturnsCachedResult tests protocol caching
func TestProtocols_SingleProtocol_ReturnsCachedResult(t *testing.T) {
	t.Parallel()

	callCount := 0
	expectedProtocol := api.ASProtocolResponse{
		Instances: []api.ProtocolInstance{
			{
				Description: "Freenode",
				NetworkID:   "freenode",
				Fields:      json.RawMessage(`{"network":"freenode"}`),
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedProtocol)
	}))
	defer srv.Close()

	queryAPI := createTestQueryAPIWithProtocols(srv, []string{"irc"})

	// First call
	req1 := &api.ProtocolRequest{Protocol: "irc"}
	resp1 := &api.ProtocolResponse{}
	err := queryAPI.Protocols(context.Background(), req1, resp1)
	assert.NoError(t, err)
	assert.True(t, resp1.Exists)
	assert.Equal(t, 1, callCount, "Expected one HTTP call")

	// Second call (should use cache)
	req2 := &api.ProtocolRequest{Protocol: "irc"}
	resp2 := &api.ProtocolResponse{}
	err = queryAPI.Protocols(context.Background(), req2, resp2)
	assert.NoError(t, err)
	assert.True(t, resp2.Exists)
	assert.Equal(t, 1, callCount, "Expected no additional HTTP calls (cached)")
}

// TestProtocols_AllProtocols_ReturnsAllAndCaches tests fetching all protocols
func TestProtocols_AllProtocols_ReturnsAllAndCaches(t *testing.T) {
	t.Parallel()

	expectedProtocol := api.ASProtocolResponse{
		Instances: []api.ProtocolInstance{
			{
				Description: "Freenode",
				NetworkID:   "freenode",
				Fields:      json.RawMessage(`{"network":"freenode"}`),
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedProtocol)
	}))
	defer srv.Close()

	queryAPI := createTestQueryAPIWithProtocols(srv, []string{"irc", "slack"})

	// Query all protocols (empty protocol string)
	req := &api.ProtocolRequest{Protocol: ""}
	resp := &api.ProtocolResponse{}
	err := queryAPI.Protocols(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.True(t, resp.Exists)
	assert.Len(t, resp.Protocols, 2, "Expected both protocols")
	assert.Contains(t, resp.Protocols, "irc")
	assert.Contains(t, resp.Protocols, "slack")
}

// TestProtocols_NoProtocols_ReturnsNotExists tests when AS has no protocols
func TestProtocols_NoProtocols_ReturnsNotExists(t *testing.T) {
	t.Parallel()

	cfg := &config.AppServiceAPI{
		Derived: &config.Derived{},
	}
	cfg.Derived.ApplicationServices = []config.ApplicationService{}

	queryAPI := &AppServiceQueryAPI{
		Cfg:           cfg,
		ProtocolCache: make(map[string]api.ASProtocolResponse),
		CacheMu:       sync.Mutex{},
	}

	req := &api.ProtocolRequest{Protocol: "irc"}
	resp := &api.ProtocolResponse{}
	err := queryAPI.Protocols(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.False(t, resp.Exists)
	assert.Nil(t, resp.Protocols)
}

// TestProtocols_MultipleASInstances_MergesInstances tests that protocol instances
// from multiple ASes are merged correctly
func TestProtocols_MultipleASInstances_MergesInstances(t *testing.T) {
	t.Parallel()

	// First AS server
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protocol := api.ASProtocolResponse{
			Instances: []api.ProtocolInstance{
				{Description: "Instance 1", NetworkID: "net1", Fields: json.RawMessage(`{"id":"1"}`)},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(protocol)
	}))
	defer srv1.Close()

	// Second AS server
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protocol := api.ASProtocolResponse{
			Instances: []api.ProtocolInstance{
				{Description: "Instance 2", NetworkID: "net2", Fields: json.RawMessage(`{"id":"2"}`)},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(protocol)
	}))
	defer srv2.Close()

	as1 := config.ApplicationService{
		ID:              "test-as-1",
		URL:             srv1.URL,
		ASToken:         "as-token-1",
		HSToken:         "hs-token-1",
		SenderLocalpart: "bot1",
		Protocols:       []string{"irc"},
	}
	as1.CreateHTTPClient(false)

	as2 := config.ApplicationService{
		ID:              "test-as-2",
		URL:             srv2.URL,
		ASToken:         "as-token-2",
		HSToken:         "hs-token-2",
		SenderLocalpart: "bot2",
		Protocols:       []string{"irc"},
	}
	as2.CreateHTTPClient(false)

	cfg := &config.AppServiceAPI{
		Derived: &config.Derived{},
	}
	cfg.Derived.ApplicationServices = []config.ApplicationService{as1, as2}

	queryAPI := &AppServiceQueryAPI{
		Cfg:           cfg,
		ProtocolCache: make(map[string]api.ASProtocolResponse),
		CacheMu:       sync.Mutex{},
	}

	req := &api.ProtocolRequest{Protocol: ""}
	resp := &api.ProtocolResponse{}
	err := queryAPI.Protocols(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.True(t, resp.Exists)
	assert.Len(t, resp.Protocols["irc"].Instances, 2, "Expected instances from both ASes to be merged")
}

// TestLocations_WithParams_PassesQueryString tests that query parameters are passed correctly
func TestLocations_WithParams_PassesQueryString(t *testing.T) {
	t.Parallel()

	receivedQuery := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]api.ASLocationResponse{})
	}))
	defer srv.Close()

	queryAPI := createTestQueryAPIWithProtocols(srv, []string{"irc"})

	req := &api.LocationRequest{
		Protocol: "irc",
		Params:   "searchFields%5Bnetwork%5D=freenode&searchFields%5Bchannel%5D=%23test",
	}
	resp := &api.LocationResponse{}
	err := queryAPI.Locations(context.Background(), req, resp)

	assert.NoError(t, err)
	// Fixed: Parse and verify query string parameters instead of weak assertion
	q, parseErr := url.ParseQuery(receivedQuery)
	assert.NoError(t, parseErr, "Query string should be valid")
	assert.Equal(t, "freenode", q.Get("searchFields[network]"), "Expected network parameter")
	assert.Equal(t, "#test", q.Get("searchFields[channel]"), "Expected channel parameter")
}

// TestUserIDExists_LegacyAuth_AddsTokenToQuery tests legacy auth mode
func TestUserIDExists_LegacyAuth_AddsTokenToQuery(t *testing.T) {
	t.Parallel()

	receivedQuery := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	queryAPI := createTestQueryAPIWithConfig(srv, map[string][]config.ApplicationServiceNamespace{
		"users": {{RegexpObject: regexp.MustCompile("@test:.*")}},
	}, func(cfg *config.AppServiceAPI) {
		cfg.LegacyAuth = true
		cfg.Derived.ApplicationServices[0].HSToken = "hs-token-secret"
	})

	req := &api.UserIDExistsRequest{UserID: "@test:example.com"}
	resp := &api.UserIDExistsResponse{}
	err := queryAPI.UserIDExists(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.Contains(t, receivedQuery, "access_token=hs-token-secret", "Expected access token in query string for legacy auth")
}

// TestRoomAliasExists_LegacyAuth_AddsTokenToQuery tests legacy auth mode for room aliases
func TestRoomAliasExists_LegacyAuth_AddsTokenToQuery(t *testing.T) {
	t.Parallel()

	receivedQuery := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	queryAPI := createTestQueryAPIWithConfig(srv, map[string][]config.ApplicationServiceNamespace{
		"aliases": {{RegexpObject: regexp.MustCompile("#test:.*")}},
	}, func(cfg *config.AppServiceAPI) {
		cfg.LegacyAuth = true
		cfg.Derived.ApplicationServices[0].HSToken = "hs-token-secret"
	})

	req := &api.RoomAliasExistsRequest{Alias: "#test:example.com"}
	resp := &api.RoomAliasExistsResponse{}
	err := queryAPI.RoomAliasExists(context.Background(), req, resp)

	assert.NoError(t, err)
	assert.Contains(t, receivedQuery, "access_token=hs-token-secret", "Expected access token in query string for legacy auth")
}

// TestRequestDo_HTTPError_ReturnsError tests HTTP request error handling
func TestRequestDo_HTTPError_ReturnsError(t *testing.T) {
	t.Parallel()

	as := &config.ApplicationService{
		ID:              "test-as",
		URL:             "http://localhost:99999", // Invalid port
		ASToken:         "as-token",
		HSToken:         "hs-token",
		SenderLocalpart: "bot",
	}
	as.CreateHTTPClient(false)

	var response api.ASProtocolResponse
	err := requestDo[api.ASProtocolResponse](as, as.URL+"/test", &response)

	assert.Error(t, err, "Expected error for invalid URL")
}

// TestRequestDo_InvalidJSON_ReturnsError tests JSON unmarshaling error
func TestRequestDo_InvalidJSON_ReturnsError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "invalid json{")
	}))
	defer srv.Close()

	as := &config.ApplicationService{
		ID:              "test-as",
		URL:             srv.URL,
		ASToken:         "as-token",
		HSToken:         "hs-token",
		SenderLocalpart: "bot",
	}
	as.CreateHTTPClient(false)

	var response api.ASProtocolResponse
	err := requestDo[api.ASProtocolResponse](as, srv.URL+"/test", &response)

	assert.Error(t, err, "Expected error for invalid JSON")
}
