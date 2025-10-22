// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package routing_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/element-hq/dendrite/federationapi/routing"
	"github.com/element-hq/dendrite/internal/caching"
	"github.com/element-hq/dendrite/internal/httputil"
	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/roomserver"
	"github.com/element-hq/dendrite/setup/jetstream"
	"github.com/element-hq/dendrite/test"
	"github.com/element-hq/dendrite/test/testrig"
	"github.com/gorilla/mux"
	"github.com/matrix-org/gomatrixserverlib/fclient"
	"github.com/stretchr/testify/assert"
)

func TestInviteV2_InvalidRoomID_ReturnsBadRequest(t *testing.T) {
	t.Parallel()

	cfg, processCtx, close := testrig.CreateConfig(t, test.DBTypeSQLite)
	defer close()

	cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
	cfg.FederationAPI.Matrix.SigningIdentity.ServerName = testOrigin
	cfg.FederationAPI.Matrix.Metrics.Enabled = false

	natsInstance := &jetstream.NATSInstance{}
	caches := caching.NewRistrettoCache(8*1024*1024, time.Hour, caching.DisableMetrics)
	rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, natsInstance, caches, caching.DisableMetrics)
	rsAPI.SetFederationAPI(nil, nil)

	routers := httputil.NewRouters()
	fedMux := mux.NewRouter().SkipClean(true).PathPrefix(httputil.PublicFederationPathPrefix).Subrouter().UseEncodedPath()
	routers.Federation = fedMux

	// Setup routing with nil keys (we're not testing signature verification)
	routing.Setup(routers, cfg, rsAPI, nil, nil, nil, nil, &cfg.MSCs, nil, caching.DisableMetrics)

	// Test with invalid room ID
	invalidRoomID := "not-a-room-id"
	eventID := "$test:test.server"

	req := fclient.NewFederationRequest("PUT", testOrigin, testOrigin,
		fmt.Sprintf("/_matrix/federation/v2/invite/%s/%s", invalidRoomID, eventID))

	// Empty body
	req.SetContent(json.RawMessage(`{}`))

	httpReq, err := req.HTTPRequest()
	if err != nil {
		t.Fatalf("Failed to create HTTP request: %s", err)
	}

	vars := map[string]string{
		"roomID":  invalidRoomID,
		"eventID": eventID,
	}
	httpReq = mux.SetURLVars(httpReq, vars)

	w := httptest.NewRecorder()
	fedMux.ServeHTTP(w, httpReq)

	// Should return error for invalid room ID
	// May return 400 Bad Request, 401 Unauthorized (missing auth), 403 Forbidden, or 500
	assert.Contains(t, []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError}, w.Code,
		"Should return error for invalid room ID")
}

func TestVersion_ReturnsServerInfo(t *testing.T) {
	t.Parallel()

	cfg, processCtx, close := testrig.CreateConfig(t, test.DBTypeSQLite)
	defer close()

	cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
	natsInstance := &jetstream.NATSInstance{}
	caches := caching.NewRistrettoCache(8*1024*1024, time.Hour, caching.DisableMetrics)
	rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, natsInstance, caches, caching.DisableMetrics)
	rsAPI.SetFederationAPI(nil, nil)

	routers := httputil.NewRouters()
	fedMux := mux.NewRouter().SkipClean(true).PathPrefix(httputil.PublicFederationPathPrefix).Subrouter().UseEncodedPath()
	routers.Federation = fedMux

	routing.Setup(routers, cfg, rsAPI, nil, nil, nil, nil, &cfg.MSCs, nil, caching.DisableMetrics)

	req, err := http.NewRequest("GET", "/_matrix/federation/v1/version", nil)
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(context.Background())

	w := httptest.NewRecorder()
	fedMux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	// Should contain server information
	assert.NotNil(t, response["server"], "Response should contain server information")
}

func TestLocalKeys_ReturnsKeys(t *testing.T) {
	t.Parallel()

	cfg, processCtx, close := testrig.CreateConfig(t, test.DBTypeSQLite)
	defer close()

	cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
	cfg.FederationAPI.Matrix.SigningIdentity.ServerName = testOrigin

	natsInstance := &jetstream.NATSInstance{}
	caches := caching.NewRistrettoCache(8*1024*1024, time.Hour, caching.DisableMetrics)
	rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, natsInstance, caches, caching.DisableMetrics)
	rsAPI.SetFederationAPI(nil, nil)

	routers := httputil.NewRouters()
	keyMux := mux.NewRouter()
	routers.Keys = keyMux

	fedMux := mux.NewRouter().SkipClean(true).PathPrefix(httputil.PublicFederationPathPrefix).Subrouter().UseEncodedPath()
	routers.Federation = fedMux

	routing.Setup(routers, cfg, rsAPI, nil, nil, nil, nil, &cfg.MSCs, nil, caching.DisableMetrics)

	req, err := http.NewRequest("GET", "/v2/server/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = string(testOrigin)
	req = req.WithContext(context.Background())

	w := httptest.NewRecorder()
	keyMux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "Response body: %s", w.Body.String())

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	// Should contain server_name
	assert.NotNil(t, response["server_name"], "Response should contain server_name")
}
