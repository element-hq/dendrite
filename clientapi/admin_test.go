package clientapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/element-hq/dendrite/federationapi"
	"github.com/element-hq/dendrite/internal/caching"
	"github.com/element-hq/dendrite/internal/httputil"
	"github.com/element-hq/dendrite/internal/sqlutil"
	mediaStorage "github.com/element-hq/dendrite/mediaapi/storage"
	mediaTypes "github.com/element-hq/dendrite/mediaapi/types"
	"github.com/element-hq/dendrite/roomserver"
	"github.com/element-hq/dendrite/roomserver/api"
	basepkg "github.com/element-hq/dendrite/setup/base"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/setup/jetstream"
	"github.com/element-hq/dendrite/syncapi"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/fclient"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
	"github.com/tidwall/gjson"

	capi "github.com/element-hq/dendrite/clientapi/api"
	"github.com/element-hq/dendrite/test"
	"github.com/element-hq/dendrite/test/testrig"
	"github.com/element-hq/dendrite/userapi"
	uapi "github.com/element-hq/dendrite/userapi/api"
)

// adminTestPath defines versioned and unversioned admin API paths for tests.
type adminTestPath struct {
	name string
	path string
}

// adminDualPaths returns both versioned and legacy admin API paths for an endpoint.
func adminDualPaths(endpoint string) []adminTestPath {
	return []adminTestPath{
		{name: "v1", path: "/_dendrite/admin/v1" + endpoint},
		{name: "legacy", path: "/_dendrite/admin" + endpoint},
	}
}

func TestAdminCreateToken(t *testing.T) {
	aliceAdmin := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))
	bob := test.NewUser(t, test.WithAccountType(uapi.AccountTypeUser))
	ctx := context.Background()
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		cfg.ClientAPI.RegistrationRequiresToken = true
		defer close()
		natsInstance := jetstream.NATSInstance{}
		routers := httputil.NewRouters()
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)
		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)
		rsAPI.SetFederationAPI(nil, nil)
		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)
		AddPublicRoutes(processCtx, routers, cfg, &natsInstance, cm, nil, rsAPI, nil, nil, nil, userAPI, nil, nil, caching.DisableMetrics)
		accessTokens := map[*test.User]userDevice{
			aliceAdmin: {},
			bob:        {},
		}
		createAccessTokens(t, accessTokens, userAPI, ctx, routers)
		testCases := []struct {
			name           string
			requestingUser *test.User
			requestOpt     test.HTTPRequestOpt
			wantOK         bool
			withHeader     bool
		}{
			{
				name:           "Missing auth",
				requestingUser: bob,
				wantOK:         false,
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"token": "token1",
				},
				),
			},
			{
				name:           "Bob is denied access",
				requestingUser: bob,
				wantOK:         false,
				withHeader:     true,
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"token": "token2",
				},
				),
			},
			{
				name:           "Alice can create a token without specifyiing any information",
				requestingUser: aliceAdmin,
				wantOK:         true,
				withHeader:     true,
				requestOpt:     test.WithJSONBody(t, map[string]interface{}{}),
			},
			{
				name:           "Alice can to create a token specifying a name",
				requestingUser: aliceAdmin,
				wantOK:         true,
				withHeader:     true,
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"token": "token3",
				},
				),
			},
			{
				name:           "Alice cannot to create a token that already exists",
				requestingUser: aliceAdmin,
				wantOK:         false,
				withHeader:     true,
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"token": "token3",
				},
				),
			},
			{
				name:           "Alice can create a token specifying valid params",
				requestingUser: aliceAdmin,
				wantOK:         true,
				withHeader:     true,
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"token":        "token4",
					"uses_allowed": 5,
					"expiry_time":  time.Now().Add(5*24*time.Hour).UnixNano() / int64(time.Millisecond),
				},
				),
			},
			{
				name:           "Alice cannot create a token specifying invalid name",
				requestingUser: aliceAdmin,
				wantOK:         false,
				withHeader:     true,
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"token": "token@",
				},
				),
			},
			{
				name:           "Alice cannot create a token specifying invalid uses_allowed",
				requestingUser: aliceAdmin,
				wantOK:         false,
				withHeader:     true,
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"token":        "token5",
					"uses_allowed": -1,
				},
				),
			},
			{
				name:           "Alice cannot create a token specifying invalid expiry_time",
				requestingUser: aliceAdmin,
				wantOK:         false,
				withHeader:     true,
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"token":       "token6",
					"expiry_time": time.Now().Add(-1*5*24*time.Hour).UnixNano() / int64(time.Millisecond),
				},
				),
			},
			{
				name:           "Alice cannot to create a token specifying invalid length",
				requestingUser: aliceAdmin,
				wantOK:         false,
				withHeader:     true,
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"length": 80,
				},
				),
			},
		}

		for _, tc := range testCases {
			tc := tc
			for _, path := range adminDualPaths("/registrationTokens/new") {
				path := path
				t.Run(tc.name+"_"+path.name, func(t *testing.T) {
					req := test.NewRequest(t, http.MethodPost, path.path)
					if tc.requestOpt != nil {
						req = test.NewRequest(t, http.MethodPost, path.path, tc.requestOpt)
					}
					if tc.withHeader {
						req.Header.Set("Authorization", "Bearer "+accessTokens[tc.requestingUser].accessToken)
					}
					rec := httptest.NewRecorder()
					routers.DendriteAdmin.ServeHTTP(rec, req)
					t.Logf("%s", rec.Body.String())
					if tc.wantOK && rec.Code != http.StatusOK {
						t.Fatalf("expected http status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
					}
				})
			}
		}
	})
}

func TestAdminListUsersEndpoint(t *testing.T) {
	aliceAdmin := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))
	bob := test.NewUser(t, test.WithAccountType(uapi.AccountTypeUser))
	carol := test.NewUser(t, test.WithAccountType(uapi.AccountTypeUser))
	ctx := context.Background()

	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		defer close()
		natsInstance := jetstream.NATSInstance{}
		routers := httputil.NewRouters()
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)
		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)
		rsAPI.SetFederationAPI(nil, nil)
		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)
		AddPublicRoutes(processCtx, routers, cfg, &natsInstance, cm, nil, rsAPI, nil, nil, nil, userAPI, nil, nil, caching.DisableMetrics)

		accessTokens := map[*test.User]userDevice{
			aliceAdmin: {},
			bob:        {},
			carol:      {},
		}
		createAccessTokens(t, accessTokens, userAPI, processCtx.Context(), routers)

		setDisplayName := func(u *test.User, name string) {
			localpart, serverName, err := gomatrixserverlib.SplitID('@', u.ID)
			if err != nil {
				t.Fatalf("SplitID failed: %v", err)
			}
			if _, _, err := userAPI.SetDisplayName(ctx, localpart, serverName, name); err != nil {
				t.Fatalf("failed to set display name: %v", err)
			}
		}
		setDisplayName(aliceAdmin, "Alice Admin")
		setDisplayName(bob, "Bob Brown")
		setDisplayName(carol, "Carol Cyan")

		updateLastSeen := func(u *test.User) {
			device := accessTokens[u]
			if device.deviceID == "" {
				t.Fatalf("missing device id for %s", u.ID)
			}
			req := &uapi.PerformLastSeenUpdateRequest{
				UserID:     u.ID,
				DeviceID:   device.deviceID,
				RemoteAddr: "127.0.0.1",
				UserAgent:  "tests",
			}
			if err := userAPI.PerformLastSeenUpdate(processCtx.Context(), req, &uapi.PerformLastSeenUpdateResponse{}); err != nil {
				t.Fatalf("failed to update last seen: %v", err)
			}
		}
		updateLastSeen(bob)
		time.Sleep(5 * time.Millisecond)
		updateLastSeen(carol)

		doRequest := func(token string, path adminTestPath, query string) *httptest.ResponseRecorder {
			url := path.path
			if query != "" {
				url = fmt.Sprintf("%s?%s", url, query)
			}
			req := test.NewRequest(t, http.MethodGet, url)
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			rec := httptest.NewRecorder()
			routers.DendriteAdmin.ServeHTTP(rec, req)
			return rec
		}

		for _, path := range adminDualPaths("/users") {
			path := path
			t.Run("missing_auth_"+path.name, func(t *testing.T) {
				rec := doRequest("", path, "")
				if rec.Code == http.StatusOK {
					t.Fatalf("expected failure without auth, got 200")
				}
			})

			t.Run("non_admin_forbidden_"+path.name, func(t *testing.T) {
				rec := doRequest(accessTokens[bob].accessToken, path, "")
				if rec.Code != http.StatusForbidden {
					t.Fatalf("expected 403 for non-admin, got %d", rec.Code)
				}
			})

			t.Run("admin_success_default_"+path.name, func(t *testing.T) {
				rec := doRequest(accessTokens[aliceAdmin].accessToken, path, "")
				if rec.Code != http.StatusOK {
					t.Fatalf("expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
				}
				total := gjson.GetBytes(rec.Body.Bytes(), "total").Int()
				if total != 3 {
					t.Fatalf("expected total 3, got %d", total)
				}
				users := gjson.GetBytes(rec.Body.Bytes(), "users").Array()
				if len(users) != 3 {
					t.Fatalf("expected 3 users, got %d", len(users))
				}
				if users[0].Get("user_id").Str != carol.ID {
					t.Fatalf("expected latest user first, got %s", users[0].Get("user_id").Str)
				}
			})
		}

		versionedPath := adminDualPaths("/users")[0]

		t.Run("pagination_and_next_from", func(t *testing.T) {
			rec := doRequest(accessTokens[aliceAdmin].accessToken, versionedPath, "limit=2")
			if rec.Code != http.StatusOK {
				t.Fatalf("expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
			}
			users := gjson.GetBytes(rec.Body.Bytes(), "users").Array()
			if len(users) != 2 {
				t.Fatalf("expected 2 users, got %d", len(users))
			}
			nextFrom := gjson.GetBytes(rec.Body.Bytes(), "next_from").Int()
			if nextFrom != 2 {
				t.Fatalf("expected next_from 2, got %d", nextFrom)
			}
		})

		t.Run("search_by_display_name", func(t *testing.T) {
			rec := doRequest(accessTokens[aliceAdmin].accessToken, versionedPath, "search=cyan")
			if rec.Code != http.StatusOK {
				t.Fatalf("expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
			}
			users := gjson.GetBytes(rec.Body.Bytes(), "users").Array()
			if len(users) != 1 {
				t.Fatalf("expected 1 user, got %d", len(users))
			}
			if users[0].Get("display_name").Str != "Carol Cyan" {
				t.Fatalf("unexpected display name %s", users[0].Get("display_name").Str)
			}
		})

		t.Run("sort_by_last_seen", func(t *testing.T) {
			rec := doRequest(accessTokens[aliceAdmin].accessToken, versionedPath, "sort=last_seen_ts")
			if rec.Code != http.StatusOK {
				t.Fatalf("expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
			}
			users := gjson.GetBytes(rec.Body.Bytes(), "users").Array()
			if len(users) == 0 {
				t.Fatalf("expected users in response")
			}
			if users[0].Get("user_id").Str != carol.ID {
				t.Fatalf("expected Carol to be most recently seen, got %s", users[0].Get("user_id").Str)
			}
		})

		// deactivate Carol for filter test
		carolLocalpart, carolServer, err := gomatrixserverlib.SplitID('@', carol.ID)
		if err != nil {
			t.Fatalf("SplitID failed: %v", err)
		}
		if err := userAPI.PerformAccountDeactivation(processCtx.Context(), &uapi.PerformAccountDeactivationRequest{
			Localpart:  carolLocalpart,
			ServerName: carolServer,
		}, &uapi.PerformAccountDeactivationResponse{}); err != nil {
			t.Fatalf("failed to deactivate user: %v", err)
		}

		t.Run("filter_deactivated", func(t *testing.T) {
			rec := doRequest(accessTokens[aliceAdmin].accessToken, versionedPath, "deactivated=true")
			if rec.Code != http.StatusOK {
				t.Fatalf("expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
			}
			users := gjson.GetBytes(rec.Body.Bytes(), "users").Array()
			if len(users) != 1 {
				t.Fatalf("expected 1 deactivated user, got %d", len(users))
			}
			if !users[0].Get("deactivated").Bool() {
				t.Fatalf("expected user to be marked deactivated")
			}
		})

		t.Run("invalid_params", func(t *testing.T) {
			rec := doRequest(accessTokens[aliceAdmin].accessToken, versionedPath, "limit=0")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for invalid limit, got %d", rec.Code)
			}
			rec = doRequest(accessTokens[aliceAdmin].accessToken, versionedPath, "sort=unknown")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for invalid sort, got %d", rec.Code)
			}
		})
	})
}

func TestAdminListRegistrationTokens(t *testing.T) {
	aliceAdmin := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))
	bob := test.NewUser(t, test.WithAccountType(uapi.AccountTypeUser))
	ctx := context.Background()
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		cfg.ClientAPI.RegistrationRequiresToken = true
		defer close()
		natsInstance := jetstream.NATSInstance{}
		routers := httputil.NewRouters()
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)
		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)
		rsAPI.SetFederationAPI(nil, nil)
		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)
		AddPublicRoutes(processCtx, routers, cfg, &natsInstance, cm, nil, rsAPI, nil, nil, nil, userAPI, nil, nil, caching.DisableMetrics)
		accessTokens := map[*test.User]userDevice{
			aliceAdmin: {},
			bob:        {},
		}
		tokens := []capi.RegistrationToken{
			{
				Token:       getPointer("valid"),
				UsesAllowed: getPointer(int32(10)),
				ExpiryTime:  getPointer(time.Now().Add(5*24*time.Hour).UnixNano() / int64(time.Millisecond)),
				Pending:     getPointer(int32(0)),
				Completed:   getPointer(int32(0)),
			},
			{
				Token:       getPointer("invalid"),
				UsesAllowed: getPointer(int32(10)),
				ExpiryTime:  getPointer(time.Now().Add(-1*5*24*time.Hour).UnixNano() / int64(time.Millisecond)),
				Pending:     getPointer(int32(0)),
				Completed:   getPointer(int32(0)),
			},
		}
		for _, tkn := range tokens {
			tkn := tkn
			userAPI.PerformAdminCreateRegistrationToken(ctx, &tkn)
		}
		createAccessTokens(t, accessTokens, userAPI, ctx, routers)
		testCases := []struct {
			name             string
			requestingUser   *test.User
			valid            string
			isValidSpecified bool
			wantOK           bool
			withHeader       bool
		}{
			{
				name:             "Missing auth",
				requestingUser:   bob,
				wantOK:           false,
				isValidSpecified: false,
			},
			{
				name:             "Bob is denied access",
				requestingUser:   bob,
				wantOK:           false,
				withHeader:       true,
				isValidSpecified: false,
			},
			{
				name:           "Alice can list all tokens",
				requestingUser: aliceAdmin,
				wantOK:         true,
				withHeader:     true,
			},
			{
				name:             "Alice can list all valid tokens",
				requestingUser:   aliceAdmin,
				wantOK:           true,
				withHeader:       true,
				valid:            "true",
				isValidSpecified: true,
			},
			{
				name:             "Alice can list all invalid tokens",
				requestingUser:   aliceAdmin,
				wantOK:           true,
				withHeader:       true,
				valid:            "false",
				isValidSpecified: true,
			},
			{
				name:             "No response when valid has a bad value",
				requestingUser:   aliceAdmin,
				wantOK:           false,
				withHeader:       true,
				valid:            "trueee",
				isValidSpecified: true,
			},
		}

		for _, path := range adminDualPaths("/registrationTokens") {
			path := path
			for _, tc := range testCases {
				tc := tc
				t.Run(tc.name+"_"+path.name, func(t *testing.T) {
					url := path.path
					if tc.isValidSpecified {
						url = fmt.Sprintf("%s?valid=%v", path.path, tc.valid)
					}
					req := test.NewRequest(t, http.MethodGet, url)
					if tc.withHeader {
						req.Header.Set("Authorization", "Bearer "+accessTokens[tc.requestingUser].accessToken)
					}
					rec := httptest.NewRecorder()
					routers.DendriteAdmin.ServeHTTP(rec, req)
					t.Logf("%s", rec.Body.String())
					if tc.wantOK && rec.Code != http.StatusOK {
						t.Fatalf("expected http status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
					}
				})
			}
		}
	})
}

func TestAdminGetRegistrationToken(t *testing.T) {
	aliceAdmin := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))
	bob := test.NewUser(t, test.WithAccountType(uapi.AccountTypeUser))
	ctx := context.Background()
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		cfg.ClientAPI.RegistrationRequiresToken = true
		defer close()
		natsInstance := jetstream.NATSInstance{}
		routers := httputil.NewRouters()
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)
		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)
		rsAPI.SetFederationAPI(nil, nil)
		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)
		AddPublicRoutes(processCtx, routers, cfg, &natsInstance, cm, nil, rsAPI, nil, nil, nil, userAPI, nil, nil, caching.DisableMetrics)
		accessTokens := map[*test.User]userDevice{
			aliceAdmin: {},
			bob:        {},
		}
		tokens := []capi.RegistrationToken{
			{
				Token:       getPointer("alice_token1"),
				UsesAllowed: getPointer(int32(10)),
				ExpiryTime:  getPointer(time.Now().Add(5*24*time.Hour).UnixNano() / int64(time.Millisecond)),
				Pending:     getPointer(int32(0)),
				Completed:   getPointer(int32(0)),
			},
			{
				Token:       getPointer("alice_token2"),
				UsesAllowed: getPointer(int32(10)),
				ExpiryTime:  getPointer(time.Now().Add(-1*5*24*time.Hour).UnixNano() / int64(time.Millisecond)),
				Pending:     getPointer(int32(0)),
				Completed:   getPointer(int32(0)),
			},
		}
		for _, tkn := range tokens {
			tkn := tkn
			userAPI.PerformAdminCreateRegistrationToken(ctx, &tkn)
		}
		createAccessTokens(t, accessTokens, userAPI, ctx, routers)
		testCases := []struct {
			name           string
			requestingUser *test.User
			token          string
			wantOK         bool
			withHeader     bool
		}{
			{
				name:           "Missing auth",
				requestingUser: bob,
				wantOK:         false,
			},
			{
				name:           "Bob is denied access",
				requestingUser: bob,
				wantOK:         false,
				withHeader:     true,
			},
			{
				name:           "Alice can GET alice_token1",
				token:          "alice_token1",
				requestingUser: aliceAdmin,
				wantOK:         true,
				withHeader:     true,
			},
			{
				name:           "Alice can GET alice_token2",
				requestingUser: aliceAdmin,
				wantOK:         true,
				withHeader:     true,
				token:          "alice_token2",
			},
			{
				name:           "Alice cannot GET a token that does not exists",
				requestingUser: aliceAdmin,
				wantOK:         false,
				withHeader:     true,
				token:          "alice_token3",
			},
		}

		for _, tc := range testCases {
			tc := tc
			for _, path := range adminDualPaths(fmt.Sprintf("/registrationTokens/%s", tc.token)) {
				path := path
				t.Run(tc.name+"_"+path.name, func(t *testing.T) {
					req := test.NewRequest(t, http.MethodGet, path.path)
					if tc.withHeader {
						req.Header.Set("Authorization", "Bearer "+accessTokens[tc.requestingUser].accessToken)
					}
					rec := httptest.NewRecorder()
					routers.DendriteAdmin.ServeHTTP(rec, req)
					t.Logf("%s", rec.Body.String())
					if tc.wantOK && rec.Code != http.StatusOK {
						t.Fatalf("expected http status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
					}
				})
			}
		}
	})
}

func TestAdminDeleteRegistrationToken(t *testing.T) {
	aliceAdmin := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))
	bob := test.NewUser(t, test.WithAccountType(uapi.AccountTypeUser))
	ctx := context.Background()
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		cfg.ClientAPI.RegistrationRequiresToken = true
		defer close()
		natsInstance := jetstream.NATSInstance{}
		routers := httputil.NewRouters()
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)
		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)
		rsAPI.SetFederationAPI(nil, nil)
		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)
		AddPublicRoutes(processCtx, routers, cfg, &natsInstance, cm, nil, rsAPI, nil, nil, nil, userAPI, nil, nil, caching.DisableMetrics)
		accessTokens := map[*test.User]userDevice{
			aliceAdmin: {},
			bob:        {},
		}
		tokens := []capi.RegistrationToken{
			{
				Token:       getPointer("alice_token1"),
				UsesAllowed: getPointer(int32(10)),
				ExpiryTime:  getPointer(time.Now().Add(5*24*time.Hour).UnixNano() / int64(time.Millisecond)),
				Pending:     getPointer(int32(0)),
				Completed:   getPointer(int32(0)),
			},
			{
				Token:       getPointer("alice_token2"),
				UsesAllowed: getPointer(int32(10)),
				ExpiryTime:  getPointer(time.Now().Add(-1*5*24*time.Hour).UnixNano() / int64(time.Millisecond)),
				Pending:     getPointer(int32(0)),
				Completed:   getPointer(int32(0)),
			},
		}
		for _, tkn := range tokens {
			tkn := tkn
			userAPI.PerformAdminCreateRegistrationToken(ctx, &tkn)
		}
		createAccessTokens(t, accessTokens, userAPI, ctx, routers)
		testCases := []struct {
			name           string
			requestingUser *test.User
			token          string
			wantOK         bool
			withHeader     bool
		}{
			{
				name:           "Missing auth",
				requestingUser: bob,
				wantOK:         false,
			},
			{
				name:           "Bob is denied access",
				requestingUser: bob,
				wantOK:         false,
				withHeader:     true,
			},
			{
				name:           "Alice can DELETE alice_token1",
				token:          "alice_token1",
				requestingUser: aliceAdmin,
				wantOK:         true,
				withHeader:     true,
			},
			{
				name:           "Alice can DELETE alice_token2",
				requestingUser: aliceAdmin,
				wantOK:         true,
				withHeader:     true,
				token:          "alice_token2",
			},
		}

		for _, tc := range testCases {
			tc := tc
			for _, path := range adminDualPaths(fmt.Sprintf("/registrationTokens/%s", tc.token)) {
				path := path
				t.Run(tc.name+"_"+path.name, func(t *testing.T) {
					req := test.NewRequest(t, http.MethodDelete, path.path)
					if tc.withHeader {
						req.Header.Set("Authorization", "Bearer "+accessTokens[tc.requestingUser].accessToken)
					}
					rec := httptest.NewRecorder()
					routers.DendriteAdmin.ServeHTTP(rec, req)
					t.Logf("%s", rec.Body.String())
					if tc.wantOK && rec.Code != http.StatusOK {
						t.Fatalf("expected http status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
					}
				})
			}
		}
	})
}

func TestAdminUpdateRegistrationToken(t *testing.T) {
	aliceAdmin := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))
	bob := test.NewUser(t, test.WithAccountType(uapi.AccountTypeUser))
	ctx := context.Background()
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		cfg.ClientAPI.RegistrationRequiresToken = true
		defer close()
		natsInstance := jetstream.NATSInstance{}
		routers := httputil.NewRouters()
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)
		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)
		rsAPI.SetFederationAPI(nil, nil)
		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)
		AddPublicRoutes(processCtx, routers, cfg, &natsInstance, cm, nil, rsAPI, nil, nil, nil, userAPI, nil, nil, caching.DisableMetrics)
		accessTokens := map[*test.User]userDevice{
			aliceAdmin: {},
			bob:        {},
		}
		createAccessTokens(t, accessTokens, userAPI, ctx, routers)
		tokens := []capi.RegistrationToken{
			{
				Token:       getPointer("alice_token1"),
				UsesAllowed: getPointer(int32(10)),
				ExpiryTime:  getPointer(time.Now().Add(5*24*time.Hour).UnixNano() / int64(time.Millisecond)),
				Pending:     getPointer(int32(0)),
				Completed:   getPointer(int32(0)),
			},
			{
				Token:       getPointer("alice_token2"),
				UsesAllowed: getPointer(int32(10)),
				ExpiryTime:  getPointer(time.Now().Add(-1*5*24*time.Hour).UnixNano() / int64(time.Millisecond)),
				Pending:     getPointer(int32(0)),
				Completed:   getPointer(int32(0)),
			},
		}
		for _, tkn := range tokens {
			tkn := tkn
			userAPI.PerformAdminCreateRegistrationToken(ctx, &tkn)
		}
		testCases := []struct {
			name           string
			requestingUser *test.User
			method         string
			token          string
			requestOpt     test.HTTPRequestOpt
			wantOK         bool
			withHeader     bool
		}{
			{
				name:           "Missing auth",
				requestingUser: bob,
				wantOK:         false,
				token:          "alice_token1",
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"uses_allowed": 10,
				},
				),
			},
			{
				name:           "Bob is denied access",
				requestingUser: bob,
				wantOK:         false,
				withHeader:     true,
				token:          "alice_token1",
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"uses_allowed": 10,
				},
				),
			},
			{
				name:           "Alice can UPDATE a token's uses_allowed property",
				requestingUser: aliceAdmin,
				wantOK:         true,
				withHeader:     true,
				token:          "alice_token1",
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"uses_allowed": 10,
				}),
			},
			{
				name:           "Alice can UPDATE a token's expiry_time property",
				requestingUser: aliceAdmin,
				wantOK:         true,
				withHeader:     true,
				token:          "alice_token2",
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"expiry_time": time.Now().Add(5*24*time.Hour).UnixNano() / int64(time.Millisecond),
				},
				),
			},
			{
				name:           "Alice can UPDATE a token's uses_allowed and expiry_time property",
				requestingUser: aliceAdmin,
				wantOK:         false,
				withHeader:     true,
				token:          "alice_token1",
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"uses_allowed": 20,
					"expiry_time":  time.Now().Add(10*24*time.Hour).UnixNano() / int64(time.Millisecond),
				},
				),
			},
			{
				name:           "Alice CANNOT update a token with invalid properties",
				requestingUser: aliceAdmin,
				wantOK:         false,
				withHeader:     true,
				token:          "alice_token2",
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"uses_allowed": -5,
					"expiry_time":  time.Now().Add(-1*5*24*time.Hour).UnixNano() / int64(time.Millisecond),
				},
				),
			},
			{
				name:           "Alice CANNOT UPDATE a token that does not exist",
				requestingUser: aliceAdmin,
				wantOK:         false,
				withHeader:     true,
				token:          "alice_token9",
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"uses_allowed": 100,
				},
				),
			},
			{
				name:           "Alice can UPDATE token specifying uses_allowed as null - Valid for infinite uses",
				requestingUser: aliceAdmin,
				wantOK:         false,
				withHeader:     true,
				token:          "alice_token1",
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"uses_allowed": nil,
				},
				),
			},
			{
				name:           "Alice can UPDATE token specifying expiry_time AS null - Valid for infinite time",
				requestingUser: aliceAdmin,
				wantOK:         false,
				withHeader:     true,
				token:          "alice_token1",
				requestOpt: test.WithJSONBody(t, map[string]interface{}{
					"expiry_time": nil,
				},
				),
			},
		}

		for _, tc := range testCases {
			tc := tc
			for _, path := range adminDualPaths(fmt.Sprintf("/registrationTokens/%s", tc.token)) {
				path := path
				t.Run(tc.name+"_"+path.name, func(t *testing.T) {
					req := test.NewRequest(t, http.MethodPut, path.path)
					if tc.requestOpt != nil {
						req = test.NewRequest(t, http.MethodPut, path.path, tc.requestOpt)
					}
					if tc.withHeader {
						req.Header.Set("Authorization", "Bearer "+accessTokens[tc.requestingUser].accessToken)
					}
					rec := httptest.NewRecorder()
					routers.DendriteAdmin.ServeHTTP(rec, req)
					t.Logf("%s", rec.Body.String())
					if tc.wantOK && rec.Code != http.StatusOK {
						t.Fatalf("expected http status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
					}
				})
			}
		}
	})
}

func getPointer[T any](s T) *T {
	return &s
}

func TestAdminResetPassword(t *testing.T) {
	aliceAdmin := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))
	bob := test.NewUser(t, test.WithAccountType(uapi.AccountTypeUser))
	vhUser := &test.User{ID: "@vhuser:vh1"}

	ctx := context.Background()
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		defer close()
		natsInstance := jetstream.NATSInstance{}
		// add a vhost
		cfg.Global.VirtualHosts = append(cfg.Global.VirtualHosts, &config.VirtualHost{
			SigningIdentity: fclient.SigningIdentity{ServerName: "vh1"},
		})

		routers := httputil.NewRouters()
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)
		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)
		rsAPI.SetFederationAPI(nil, nil)
		// Needed for changing the password/login
		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)
		// We mostly need the userAPI for this test, so nil for other APIs/caches etc.
		AddPublicRoutes(processCtx, routers, cfg, &natsInstance, cm, nil, rsAPI, nil, nil, nil, userAPI, nil, nil, caching.DisableMetrics)

		// Create the users in the userapi and login
		accessTokens := map[*test.User]userDevice{
			aliceAdmin: {},
			bob:        {},
			vhUser:     {},
		}
		createAccessTokens(t, accessTokens, userAPI, ctx, routers)

		testCases := []struct {
			name           string
			requestingUser *test.User
			userID         string
			requestOpt     test.HTTPRequestOpt
			wantOK         bool
			withHeader     bool
		}{
			{name: "Missing auth", requestingUser: bob, wantOK: false, userID: bob.ID},
			{name: "Bob is denied access", requestingUser: bob, wantOK: false, withHeader: true, userID: bob.ID},
			{name: "Alice is allowed access", requestingUser: aliceAdmin, wantOK: true, withHeader: true, userID: bob.ID, requestOpt: test.WithJSONBody(t, map[string]interface{}{
				"password": util.RandomString(8),
			})},
			{name: "missing userID does not call function", requestingUser: aliceAdmin, wantOK: false, withHeader: true, userID: ""}, // this 404s
			{name: "rejects empty password", requestingUser: aliceAdmin, wantOK: false, withHeader: true, userID: bob.ID, requestOpt: test.WithJSONBody(t, map[string]interface{}{
				"password": "",
			})},
			{name: "rejects unknown server name", requestingUser: aliceAdmin, wantOK: false, withHeader: true, userID: "@doesnotexist:localhost", requestOpt: test.WithJSONBody(t, map[string]interface{}{})},
			{name: "rejects unknown user", requestingUser: aliceAdmin, wantOK: false, withHeader: true, userID: "@doesnotexist:test", requestOpt: test.WithJSONBody(t, map[string]interface{}{})},
			{name: "allows changing password for different vhost", requestingUser: aliceAdmin, wantOK: true, withHeader: true, userID: vhUser.ID, requestOpt: test.WithJSONBody(t, map[string]interface{}{
				"password": util.RandomString(8),
			})},
			{name: "rejects existing user, missing body", requestingUser: aliceAdmin, wantOK: false, withHeader: true, userID: bob.ID},
			{name: "rejects invalid userID", requestingUser: aliceAdmin, wantOK: false, withHeader: true, userID: "!notauserid:test", requestOpt: test.WithJSONBody(t, map[string]interface{}{})},
			{name: "rejects invalid json", requestingUser: aliceAdmin, wantOK: false, withHeader: true, userID: bob.ID, requestOpt: test.WithJSONBody(t, `{invalidJSON}`)},
			{name: "rejects too weak password", requestingUser: aliceAdmin, wantOK: false, withHeader: true, userID: bob.ID, requestOpt: test.WithJSONBody(t, map[string]interface{}{
				"password": util.RandomString(6),
			})},
			{name: "rejects too long password", requestingUser: aliceAdmin, wantOK: false, withHeader: true, userID: bob.ID, requestOpt: test.WithJSONBody(t, map[string]interface{}{
				"password": util.RandomString(513),
			})},
		}

		for _, tc := range testCases {
			tc := tc // ensure we don't accidentally only test the last test case
			for _, path := range adminDualPaths("/resetPassword/" + tc.userID) {
				path := path
				t.Run(tc.name+"_"+path.name, func(t *testing.T) {
					req := test.NewRequest(t, http.MethodPost, path.path)
					if tc.requestOpt != nil {
						req = test.NewRequest(t, http.MethodPost, path.path, tc.requestOpt)
					}

					if tc.withHeader {
						req.Header.Set("Authorization", "Bearer "+accessTokens[tc.requestingUser].accessToken)
					}

					rec := httptest.NewRecorder()
					routers.DendriteAdmin.ServeHTTP(rec, req)
					t.Logf("%s", rec.Body.String())
					if tc.wantOK && rec.Code != http.StatusOK {
						t.Fatalf("expected http status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
					}
				})
			}
		}
	})
}

func TestPurgeRoom(t *testing.T) {
	aliceAdmin := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))
	bob := test.NewUser(t)
	room := test.NewRoom(t, aliceAdmin, test.RoomPreset(test.PresetTrustedPrivateChat))

	// Invite Bob
	room.CreateAndInsert(t, aliceAdmin, spec.MRoomMember, map[string]interface{}{
		"membership": "invite",
	}, test.WithStateKey(bob.ID))

	ctx := context.Background()

	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)
		natsInstance := jetstream.NATSInstance{}
		defer func() {
			// give components the time to process purge requests
			time.Sleep(time.Millisecond * 50)
			close()
		}()

		routers := httputil.NewRouters()
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)

		// this starts the JetStream consumers
		fsAPI := federationapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, nil, rsAPI, caches, nil, true)
		rsAPI.SetFederationAPI(fsAPI, nil)

		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)
		syncapi.AddPublicRoutes(processCtx, routers, cfg, cm, &natsInstance, userAPI, rsAPI, caches, caching.DisableMetrics)

		// Create the room
		if err := api.SendEvents(ctx, rsAPI, api.KindNew, room.Events(), "test", "test", "test", nil, false); err != nil {
			t.Fatalf("failed to send events: %v", err)
		}

		// We mostly need the rsAPI for this test, so nil for other APIs/caches etc.
		AddPublicRoutes(processCtx, routers, cfg, &natsInstance, cm, nil, rsAPI, nil, nil, nil, userAPI, nil, nil, caching.DisableMetrics)

		// Create the users in the userapi and login
		accessTokens := map[*test.User]userDevice{
			aliceAdmin: {},
		}
		createAccessTokens(t, accessTokens, userAPI, ctx, routers)

		testCases := []struct {
			name   string
			roomID string
			wantOK bool
		}{
			{name: "Can purge existing room", wantOK: true, roomID: room.ID},
			{name: "Can not purge non-existent room", wantOK: false, roomID: "!doesnotexist:localhost"},
			{name: "rejects invalid room ID", wantOK: false, roomID: "@doesnotexist:localhost"},
		}

		for _, tc := range testCases {
			tc := tc // ensure we don't accidentally only test the last test case
			for _, path := range adminDualPaths("/purgeRoom/" + tc.roomID) {
				path := path
				t.Run(tc.name+"_"+path.name, func(t *testing.T) {
					req := test.NewRequest(t, http.MethodPost, path.path)
					req.Header.Set("Authorization", "Bearer "+accessTokens[aliceAdmin].accessToken)

					rec := httptest.NewRecorder()
					routers.DendriteAdmin.ServeHTTP(rec, req)
					t.Logf("%s", rec.Body.String())
					if tc.wantOK && rec.Code != http.StatusOK {
						t.Fatalf("expected http status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
					}
				})
			}
		}

	})
}

func TestAdminEvacuateRoom(t *testing.T) {
	aliceAdmin := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))
	bob := test.NewUser(t)
	room := test.NewRoom(t, aliceAdmin)

	// Join Bob
	room.CreateAndInsert(t, bob, spec.MRoomMember, map[string]interface{}{
		"membership": "join",
	}, test.WithStateKey(bob.ID))

	ctx := context.Background()

	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)
		natsInstance := jetstream.NATSInstance{}
		defer close()

		routers := httputil.NewRouters()
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)

		// this starts the JetStream consumers
		fsAPI := federationapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, nil, rsAPI, caches, nil, true)
		rsAPI.SetFederationAPI(fsAPI, nil)

		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)

		// Create the room
		if err := api.SendEvents(ctx, rsAPI, api.KindNew, room.Events(), "test", "test", api.DoNotSendToOtherServers, nil, false); err != nil {
			t.Fatalf("failed to send events: %v", err)
		}

		// We mostly need the rsAPI for this test, so nil for other APIs/caches etc.
		AddPublicRoutes(processCtx, routers, cfg, &natsInstance, cm, nil, rsAPI, nil, nil, nil, userAPI, nil, nil, caching.DisableMetrics)

		// Create the users in the userapi and login
		accessTokens := map[*test.User]userDevice{
			aliceAdmin: {},
		}
		createAccessTokens(t, accessTokens, userAPI, ctx, routers)

		testCases := []struct {
			name         string
			roomID       string
			wantOK       bool
			wantAffected []string
		}{
			{name: "Can evacuate existing room", wantOK: true, roomID: room.ID, wantAffected: []string{aliceAdmin.ID, bob.ID}},
			{name: "Can not evacuate non-existent room", wantOK: false, roomID: "!doesnotexist:localhost", wantAffected: []string{}},
		}

		for _, tc := range testCases {
			tc := tc
			for _, path := range adminDualPaths("/evacuateRoom/" + tc.roomID) {
				path := path
				t.Run(tc.name+"_"+path.name, func(t *testing.T) {
					req := test.NewRequest(t, http.MethodPost, path.path)

					req.Header.Set("Authorization", "Bearer "+accessTokens[aliceAdmin].accessToken)

					rec := httptest.NewRecorder()
					routers.DendriteAdmin.ServeHTTP(rec, req)
					t.Logf("%s", rec.Body.String())
					if tc.wantOK && rec.Code != http.StatusOK {
						t.Fatalf("expected http status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
					}

					affectedArr := gjson.GetBytes(rec.Body.Bytes(), "affected").Array()
					affected := make([]string, 0, len(affectedArr))
					for _, x := range affectedArr {
						affected = append(affected, x.Str)
					}
					if !reflect.DeepEqual(affected, tc.wantAffected) {
						t.Fatalf("expected affected %#v, but got %#v", tc.wantAffected, affected)
					}
				})
			}
		}

		// Wait for the FS API to have consumed every message
		js, _ := natsInstance.Prepare(processCtx, &cfg.Global.JetStream)
		timeout := time.After(time.Second)
		for {
			select {
			case <-timeout:
				t.Fatalf("FS API didn't process all events in time")
			default:
			}
			info, err := js.ConsumerInfo(cfg.Global.JetStream.Prefixed(jetstream.OutputRoomEvent), cfg.Global.JetStream.Durable("FederationAPIRoomServerConsumer")+"Pull")
			if err != nil {
				time.Sleep(time.Millisecond * 10)
				continue
			}
			if info.NumPending == 0 && info.NumAckPending == 0 {
				break
			}
		}
	})
}

func TestAdminEvacuateUser(t *testing.T) {
	aliceAdmin := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))
	bob := test.NewUser(t)
	room := test.NewRoom(t, aliceAdmin)
	room2 := test.NewRoom(t, aliceAdmin)

	// Join Bob
	room.CreateAndInsert(t, bob, spec.MRoomMember, map[string]interface{}{
		"membership": "join",
	}, test.WithStateKey(bob.ID))
	room2.CreateAndInsert(t, bob, spec.MRoomMember, map[string]interface{}{
		"membership": "join",
	}, test.WithStateKey(bob.ID))

	ctx := context.Background()

	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)
		natsInstance := jetstream.NATSInstance{}
		defer close()

		routers := httputil.NewRouters()
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)

		// this starts the JetStream consumers
		fsAPI := federationapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, basepkg.CreateFederationClient(cfg, nil), rsAPI, caches, nil, true)
		rsAPI.SetFederationAPI(fsAPI, nil)

		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)

		// Create the room
		if err := api.SendEvents(ctx, rsAPI, api.KindNew, room.Events(), "test", "test", api.DoNotSendToOtherServers, nil, false); err != nil {
			t.Fatalf("failed to send events: %v", err)
		}
		if err := api.SendEvents(ctx, rsAPI, api.KindNew, room2.Events(), "test", "test", api.DoNotSendToOtherServers, nil, false); err != nil {
			t.Fatalf("failed to send events: %v", err)
		}

		// We mostly need the rsAPI for this test, so nil for other APIs/caches etc.
		AddPublicRoutes(processCtx, routers, cfg, &natsInstance, cm, nil, rsAPI, nil, nil, nil, userAPI, nil, nil, caching.DisableMetrics)

		// Create the users in the userapi and login
		accessTokens := map[*test.User]userDevice{
			aliceAdmin: {},
		}
		createAccessTokens(t, accessTokens, userAPI, ctx, routers)

		testCases := []struct {
			name              string
			userID            string
			wantOK            bool
			wantAffectedRooms []string
		}{
			{name: "Can evacuate existing user", wantOK: true, userID: bob.ID, wantAffectedRooms: []string{room.ID, room2.ID}},
			{name: "invalid userID is rejected", wantOK: false, userID: "!notauserid:test", wantAffectedRooms: []string{}},
			{name: "Can not evacuate user from different server", wantOK: false, userID: "@doesnotexist:localhost", wantAffectedRooms: []string{}},
			{name: "Can not evacuate non-existent user", wantOK: false, userID: "@doesnotexist:test", wantAffectedRooms: []string{}},
		}

		for _, tc := range testCases {
			tc := tc
			for _, path := range adminDualPaths("/evacuateUser/" + tc.userID) {
				path := path
				t.Run(tc.name+"_"+path.name, func(t *testing.T) {
					req := test.NewRequest(t, http.MethodPost, path.path)

					req.Header.Set("Authorization", "Bearer "+accessTokens[aliceAdmin].accessToken)

					rec := httptest.NewRecorder()
					routers.DendriteAdmin.ServeHTTP(rec, req)
					t.Logf("%s", rec.Body.String())
					if tc.wantOK && rec.Code != http.StatusOK {
						t.Fatalf("expected http status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
					}

					affectedArr := gjson.GetBytes(rec.Body.Bytes(), "affected").Array()
					affected := make([]string, 0, len(affectedArr))
					for _, x := range affectedArr {
						affected = append(affected, x.Str)
					}
					if !reflect.DeepEqual(affected, tc.wantAffectedRooms) {
						t.Fatalf("expected affected %#v, but got %#v", tc.wantAffectedRooms, affected)
					}

				})
			}
		}
		// Wait for the FS API to have consumed every message
		js, _ := natsInstance.Prepare(processCtx, &cfg.Global.JetStream)
		timeout := time.After(time.Second)
		for {
			select {
			case <-timeout:
				t.Fatalf("FS API didn't process all events in time")
			default:
			}
			info, err := js.ConsumerInfo(cfg.Global.JetStream.Prefixed(jetstream.OutputRoomEvent), cfg.Global.JetStream.Durable("FederationAPIRoomServerConsumer")+"Pull")
			if err != nil {
				time.Sleep(time.Millisecond * 10)
				continue
			}
			if info.NumPending == 0 && info.NumAckPending == 0 {
				break
			}
		}
	})
}

func TestAdminMarkAsStale(t *testing.T) {
	aliceAdmin := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))

	ctx := context.Background()

	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)
		natsInstance := jetstream.NATSInstance{}
		defer close()

		routers := httputil.NewRouters()
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)
		rsAPI.SetFederationAPI(nil, nil)
		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)

		// We mostly need the rsAPI for this test, so nil for other APIs/caches etc.
		AddPublicRoutes(processCtx, routers, cfg, &natsInstance, cm, nil, rsAPI, nil, nil, nil, userAPI, nil, nil, caching.DisableMetrics)

		// Create the users in the userapi and login
		accessTokens := map[*test.User]userDevice{
			aliceAdmin: {},
		}
		createAccessTokens(t, accessTokens, userAPI, ctx, routers)

		testCases := []struct {
			name   string
			userID string
			wantOK bool
		}{
			{name: "local user is not allowed", userID: aliceAdmin.ID},
			{name: "invalid userID", userID: "!notvalid:test"},
			{name: "remote user is allowed", userID: "@alice:localhost", wantOK: true},
		}

		for _, tc := range testCases {
			tc := tc
			for _, path := range adminDualPaths("/refreshDevices/" + tc.userID) {
				path := path
				t.Run(tc.name+"_"+path.name, func(t *testing.T) {
					req := test.NewRequest(t, http.MethodPost, path.path)

					req.Header.Set("Authorization", "Bearer "+accessTokens[aliceAdmin].accessToken)

					rec := httptest.NewRecorder()
					routers.DendriteAdmin.ServeHTTP(rec, req)
					t.Logf("%s", rec.Body.String())
					if tc.wantOK && rec.Code != http.StatusOK {
						t.Fatalf("expected http status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
					}
				})
			}
		}
	})
}

func TestAdminQueryEventReports(t *testing.T) {
	alice := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))
	bob := test.NewUser(t)
	room := test.NewRoom(t, alice)
	room2 := test.NewRoom(t, alice)

	// room2 has a name and canonical alias
	room2.CreateAndInsert(t, alice, spec.MRoomName, map[string]string{"name": "Testing"}, test.WithStateKey(""))
	room2.CreateAndInsert(t, alice, spec.MRoomCanonicalAlias, map[string]string{"alias": "#testing"}, test.WithStateKey(""))

	// Join the rooms with Bob
	room.CreateAndInsert(t, bob, spec.MRoomMember, map[string]interface{}{
		"membership": "join",
	}, test.WithStateKey(bob.ID))
	room2.CreateAndInsert(t, bob, spec.MRoomMember, map[string]interface{}{
		"membership": "join",
	}, test.WithStateKey(bob.ID))

	// Create a few events to report
	eventsToReportPerRoom := make(map[string][]string)
	for i := 0; i < 10; i++ {
		ev1 := room.CreateAndInsert(t, alice, "m.room.message", map[string]interface{}{"body": "hello world"})
		ev2 := room2.CreateAndInsert(t, alice, "m.room.message", map[string]interface{}{"body": "hello world"})
		eventsToReportPerRoom[room.ID] = append(eventsToReportPerRoom[room.ID], ev1.EventID())
		eventsToReportPerRoom[room2.ID] = append(eventsToReportPerRoom[room2.ID], ev2.EventID())
	}

	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		/*if dbType == test.DBTypeSQLite {
			t.Skip()
		}*/
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		routers := httputil.NewRouters()
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)
		defer close()
		natsInstance := jetstream.NATSInstance{}
		jsctx, _ := natsInstance.Prepare(processCtx, &cfg.Global.JetStream)
		defer jetstream.DeleteAllStreams(jsctx, &cfg.Global.JetStream)

		// Use an actual roomserver for this
		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)
		rsAPI.SetFederationAPI(nil, nil)
		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)

		if err := api.SendEvents(context.Background(), rsAPI, api.KindNew, room.Events(), "test", "test", "test", nil, false); err != nil {
			t.Fatalf("failed to send events: %v", err)
		}
		if err := api.SendEvents(context.Background(), rsAPI, api.KindNew, room2.Events(), "test", "test", "test", nil, false); err != nil {
			t.Fatalf("failed to send events: %v", err)
		}

		// We mostly need the rsAPI for this test, so nil for other APIs/caches etc.
		AddPublicRoutes(processCtx, routers, cfg, &natsInstance, cm, nil, rsAPI, nil, nil, nil, userAPI, nil, nil, caching.DisableMetrics)

		accessTokens := map[*test.User]userDevice{
			alice: {},
			bob:   {},
		}
		createAccessTokens(t, accessTokens, userAPI, processCtx.Context(), routers)

		reqBody := map[string]any{
			"reason": "baaad",
			"score":  -100,
		}
		body, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatal(err)
		}

		w := httptest.NewRecorder()

		var req *http.Request
		// Report all events
		for roomID, eventIDs := range eventsToReportPerRoom {
			for _, eventID := range eventIDs {
				req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/_matrix/client/v3/rooms/%s/report/%s", roomID, eventID), strings.NewReader(string(body)))
				req.Header.Set("Authorization", "Bearer "+accessTokens[bob].accessToken)

				routers.Client.ServeHTTP(w, req)

				if w.Code != http.StatusOK {
					t.Fatalf("expected report to succeed, got HTTP %d instead: %s", w.Code, w.Body.String())
				}
			}
		}

		type response struct {
			EventReports []api.QueryAdminEventReportsResponse `json:"event_reports"`
			Total        int64                                `json:"total"`
			NextToken    *int64                               `json:"next_token,omitempty"`
		}

		t.Run("Can query all reports", func(t *testing.T) {
			w = httptest.NewRecorder()
			req = httptest.NewRequest(http.MethodGet, "/_synapse/admin/v1/event_reports", strings.NewReader(string(body)))
			req.Header.Set("Authorization", "Bearer "+accessTokens[alice].accessToken)

			routers.SynapseAdmin.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected getting reports to succeed, got HTTP %d instead: %s", w.Code, w.Body.String())
			}
			var resp response
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			wantCount := 20
			// Only validating the count
			if len(resp.EventReports) != wantCount {
				t.Fatalf("expected %d events, got %d", wantCount, len(resp.EventReports))
			}
			if resp.Total != int64(wantCount) {
				t.Fatalf("expected total to be %d, got %d", wantCount, resp.Total)
			}
		})

		t.Run("Can filter on room", func(t *testing.T) {
			w = httptest.NewRecorder()
			req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/_synapse/admin/v1/event_reports?room_id=%s", room.ID), strings.NewReader(string(body)))
			req.Header.Set("Authorization", "Bearer "+accessTokens[alice].accessToken)

			routers.SynapseAdmin.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected getting reports to succeed, got HTTP %d instead: %s", w.Code, w.Body.String())
			}
			var resp response
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			wantCount := 10
			// Only validating the count
			if len(resp.EventReports) != wantCount {
				t.Fatalf("expected %d events, got %d", wantCount, len(resp.EventReports))
			}
			if resp.Total != int64(wantCount) {
				t.Fatalf("expected total to be %d, got %d", wantCount, resp.Total)
			}
		})

		t.Run("Can filter on user_id", func(t *testing.T) {
			w = httptest.NewRecorder()
			req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/_synapse/admin/v1/event_reports?user_id=%s", "@doesnotexist:test"), strings.NewReader(string(body)))
			req.Header.Set("Authorization", "Bearer "+accessTokens[alice].accessToken)

			routers.SynapseAdmin.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected getting reports to succeed, got HTTP %d instead: %s", w.Code, w.Body.String())
			}
			var resp response
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}

			// The user does not exist, so we expect no results
			wantCount := 0
			// Only validating the count
			if len(resp.EventReports) != wantCount {
				t.Fatalf("expected %d events, got %d", wantCount, len(resp.EventReports))
			}
			if resp.Total != int64(wantCount) {
				t.Fatalf("expected total to be %d, got %d", wantCount, resp.Total)
			}
		})

		t.Run("Can set direction=f", func(t *testing.T) {
			w = httptest.NewRecorder()
			req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/_synapse/admin/v1/event_reports?room_id=%s&dir=f", room.ID), strings.NewReader(string(body)))
			req.Header.Set("Authorization", "Bearer "+accessTokens[alice].accessToken)

			routers.SynapseAdmin.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected getting reports to succeed, got HTTP %d instead: %s", w.Code, w.Body.String())
			}
			var resp response
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			wantCount := 10
			// Only validating the count
			if len(resp.EventReports) != wantCount {
				t.Fatalf("expected %d events, got %d", wantCount, len(resp.EventReports))
			}
			if resp.Total != int64(wantCount) {
				t.Fatalf("expected total to be %d, got %d", wantCount, resp.Total)
			}
			// we now should have the first reported event
			wantEventID := eventsToReportPerRoom[room.ID][0]
			gotEventID := resp.EventReports[0].EventID
			if gotEventID != wantEventID {
				t.Fatalf("expected eventID to be %v, got %v", wantEventID, gotEventID)
			}
		})

		t.Run("Can limit and paginate", func(t *testing.T) {
			var from int64 = 0
			var limit int64 = 5
			var wantTotal int64 = 10 // We expect there to be 10 events in total
			var resp response
			for from+limit <= wantTotal {
				resp = response{}
				t.Logf("Getting reports starting from %d", from)
				w = httptest.NewRecorder()
				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/_synapse/admin/v1/event_reports?room_id=%s&limit=%d&from=%d", room2.ID, limit, from), strings.NewReader(string(body)))
				req.Header.Set("Authorization", "Bearer "+accessTokens[alice].accessToken)

				routers.SynapseAdmin.ServeHTTP(w, req)

				if w.Code != http.StatusOK {
					t.Fatalf("expected getting reports to succeed, got HTTP %d instead: %s", w.Code, w.Body.String())
				}

				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatal(err)
				}

				wantCount := 5 // we are limited to 5
				if len(resp.EventReports) != wantCount {
					t.Fatalf("expected %d events, got %d", wantCount, len(resp.EventReports))
				}
				if resp.Total != int64(wantTotal) {
					t.Fatalf("expected total to be %d, got %d", wantCount, resp.Total)
				}

				// We've reached the end
				if (from + int64(len(resp.EventReports))) == wantTotal {
					return
				}

				// The next_token should be set
				if resp.NextToken == nil {
					t.Fatal("expected nextToken to be set")
				}
				from = *resp.NextToken
			}
		})
	})
}

func TestEventReportsGetDelete(t *testing.T) {
	alice := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))
	bob := test.NewUser(t)
	room := test.NewRoom(t, alice)

	// Add a name and alias
	roomName := "Testing"
	alias := "#testing"
	room.CreateAndInsert(t, alice, spec.MRoomName, map[string]string{"name": roomName}, test.WithStateKey(""))
	room.CreateAndInsert(t, alice, spec.MRoomCanonicalAlias, map[string]string{"alias": alias}, test.WithStateKey(""))

	// Join the rooms with Bob
	room.CreateAndInsert(t, bob, spec.MRoomMember, map[string]interface{}{
		"membership": "join",
	}, test.WithStateKey(bob.ID))

	// Create a few events to report

	eventIDToReport := room.CreateAndInsert(t, alice, "m.room.message", map[string]interface{}{"body": "hello world"})

	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		routers := httputil.NewRouters()
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)
		defer close()
		natsInstance := jetstream.NATSInstance{}
		jsctx, _ := natsInstance.Prepare(processCtx, &cfg.Global.JetStream)
		defer jetstream.DeleteAllStreams(jsctx, &cfg.Global.JetStream)

		// Use an actual roomserver for this
		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)
		rsAPI.SetFederationAPI(nil, nil)
		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)

		if err := api.SendEvents(context.Background(), rsAPI, api.KindNew, room.Events(), "test", "test", "test", nil, false); err != nil {
			t.Fatalf("failed to send events: %v", err)
		}

		// We mostly need the rsAPI for this test, so nil for other APIs/caches etc.
		AddPublicRoutes(processCtx, routers, cfg, &natsInstance, cm, nil, rsAPI, nil, nil, nil, userAPI, nil, nil, caching.DisableMetrics)

		accessTokens := map[*test.User]userDevice{
			alice: {},
			bob:   {},
		}
		createAccessTokens(t, accessTokens, userAPI, processCtx.Context(), routers)

		reqBody := map[string]any{
			"reason": "baaad",
			"score":  -100,
		}
		body, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatal(err)
		}

		w := httptest.NewRecorder()

		var req *http.Request
		// Report the event
		req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/_matrix/client/v3/rooms/%s/report/%s", room.ID, eventIDToReport.EventID()), strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer "+accessTokens[bob].accessToken)

		routers.Client.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected report to succeed, got HTTP %d instead: %s", w.Code, w.Body.String())
		}

		t.Run("Can not query with invalid ID", func(t *testing.T) {
			w = httptest.NewRecorder()
			req = httptest.NewRequest(http.MethodGet, "/_synapse/admin/v1/event_reports/abc", strings.NewReader(string(body)))
			req.Header.Set("Authorization", "Bearer "+accessTokens[alice].accessToken)

			routers.SynapseAdmin.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected getting report to fail, got HTTP %d instead: %s", w.Code, w.Body.String())
			}
		})

		t.Run("Can query with valid ID", func(t *testing.T) {
			w = httptest.NewRecorder()
			req = httptest.NewRequest(http.MethodGet, "/_synapse/admin/v1/event_reports/1", strings.NewReader(string(body)))
			req.Header.Set("Authorization", "Bearer "+accessTokens[alice].accessToken)

			routers.SynapseAdmin.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected getting report to fail, got HTTP %d instead: %s", w.Code, w.Body.String())
			}
			resp := api.QueryAdminEventReportResponse{}
			if err = json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			// test a few things
			if resp.EventID != eventIDToReport.EventID() {
				t.Fatalf("expected eventID to be %s, got %s instead", eventIDToReport.EventID(), resp.EventID)
			}
			if resp.RoomName != roomName {
				t.Fatalf("expected roomName to be %s, got %s instead", roomName, resp.RoomName)
			}
			if resp.CanonicalAlias != alias {
				t.Fatalf("expected alias to be %s, got %s instead", alias, resp.CanonicalAlias)
			}
			if reflect.DeepEqual(resp.EventJSON, eventIDToReport.JSON()) {
				t.Fatal("mismatching eventJSON")
			}
		})

		t.Run("Can delete with a valid ID", func(t *testing.T) {
			w = httptest.NewRecorder()
			req = httptest.NewRequest(http.MethodDelete, "/_synapse/admin/v1/event_reports/1", strings.NewReader(string(body)))
			req.Header.Set("Authorization", "Bearer "+accessTokens[alice].accessToken)

			routers.SynapseAdmin.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected getting report to fail, got HTTP %d instead: %s", w.Code, w.Body.String())
			}
		})

		t.Run("Can not query deleted report", func(t *testing.T) {
			w = httptest.NewRecorder()
			req = httptest.NewRequest(http.MethodGet, "/_synapse/admin/v1/event_reports/1", strings.NewReader(string(body)))
			req.Header.Set("Authorization", "Bearer "+accessTokens[alice].accessToken)

			routers.SynapseAdmin.ServeHTTP(w, req)

			if w.Code == http.StatusOK {
				t.Fatalf("expected getting report to fail, got HTTP %d instead: %s", w.Code, w.Body.String())
			}
		})
	})
}

func TestAdminQuarantineMedia(t *testing.T) {
	admin := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))
	ctx := context.Background()

	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		defer close()
		natsInstance := jetstream.NATSInstance{}
		routers := httputil.NewRouters()
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)

		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)
		rsAPI.SetFederationAPI(nil, nil)
		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)

		AddPublicRoutes(processCtx, routers, cfg, &natsInstance, cm, nil, rsAPI, nil, nil, nil, userAPI, nil, nil, caching.DisableMetrics)

		mediaDB, err := mediaStorage.NewMediaAPIDatasource(cm, &cfg.MediaAPI.Database)
		if err != nil {
			t.Fatalf("failed to open media database: %v", err)
		}

		metadata := &mediaTypes.MediaMetadata{
			MediaID:       mediaTypes.MediaID("media-test"),
			Origin:        cfg.Matrix.ServerName,
			ContentType:   "image/png",
			FileSizeBytes: 1,
			UploadName:    "file.png",
			Base64Hash:    "hash",
			UserID:        mediaTypes.MatrixUserID(admin.ID),
		}
		if err = mediaDB.StoreMediaMetadata(ctx, metadata); err != nil {
			t.Fatalf("failed to store metadata: %v", err)
		}

		accessTokens := map[*test.User]userDevice{
			admin: {},
		}
		createAccessTokens(t, accessTokens, userAPI, ctx, routers)

		for _, path := range adminDualPaths(fmt.Sprintf("/quarantine_media/%s/%s", cfg.Matrix.ServerName, metadata.MediaID)) {
			req := test.NewRequest(t, http.MethodPost, path.path, test.WithJSONBody(t, map[string]any{
				"reason":                "test",
				"quarantine_thumbnails": true,
			}))
			req.Header.Set("Authorization", "Bearer "+accessTokens[admin].accessToken)
			rec := httptest.NewRecorder()
			routers.DendriteAdmin.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
			}
		}

		updated, err := mediaDB.GetMediaMetadata(ctx, metadata.MediaID, cfg.Matrix.ServerName)
		if err != nil {
			t.Fatalf("failed to load metadata: %v", err)
		}
		if updated == nil || !updated.Quarantined {
			t.Fatalf("expected media to be quarantined, got %+v", updated)
		}
		if updated.QuarantinedByUser != mediaTypes.MatrixUserID(admin.ID) {
			t.Fatalf("unexpected quarantined_by: %s", updated.QuarantinedByUser)
		}
	})
}

func TestAdminQuarantineMediaByUser(t *testing.T) {
	admin := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))
	target := test.NewUser(t, test.WithAccountType(uapi.AccountTypeUser))
	ctx := context.Background()

	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		defer close()
		natsInstance := jetstream.NATSInstance{}
		routers := httputil.NewRouters()
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)

		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)
		rsAPI.SetFederationAPI(nil, nil)
		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)

		AddPublicRoutes(processCtx, routers, cfg, &natsInstance, cm, nil, rsAPI, nil, nil, nil, userAPI, nil, nil, caching.DisableMetrics)

		mediaDB, err := mediaStorage.NewMediaAPIDatasource(cm, &cfg.MediaAPI.Database)
		if err != nil {
			t.Fatalf("failed to open media database: %v", err)
		}

		mediaIDs := []mediaTypes.MediaID{"media-alpha", "media-beta"}
		for _, mid := range mediaIDs {
			metadata := &mediaTypes.MediaMetadata{
				MediaID:       mid,
				Origin:        cfg.Matrix.ServerName,
				ContentType:   "image/png",
				FileSizeBytes: 1,
				UploadName:    "file.png",
				Base64Hash:    string(mid) + "-hash",
				UserID:        mediaTypes.MatrixUserID(target.ID),
			}
			if err = mediaDB.StoreMediaMetadata(ctx, metadata); err != nil {
				t.Fatalf("failed to store metadata: %v", err)
			}
		}

		accessTokens := map[*test.User]userDevice{
			admin: {},
		}
		createAccessTokens(t, accessTokens, userAPI, ctx, routers)

		for _, path := range adminDualPaths("/quarantine_media_by_user/" + target.ID) {
			req := test.NewRequest(t, http.MethodPost, path.path, test.WithJSONBody(t, map[string]any{"reason": "bulk"}))
			req.Header.Set("Authorization", "Bearer "+accessTokens[admin].accessToken)
			rec := httptest.NewRecorder()
			routers.DendriteAdmin.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
			}
		}

		for _, mid := range mediaIDs {
			updated, err := mediaDB.GetMediaMetadata(ctx, mid, cfg.Matrix.ServerName)
			if err != nil {
				t.Fatalf("failed to load metadata: %v", err)
			}
			if updated == nil || !updated.Quarantined {
				t.Fatalf("expected media %s to be quarantined", mid)
			}
		}
	})
}

func TestAdminQuarantineMediaInRoomNotImplemented(t *testing.T) {
	admin := test.NewUser(t, test.WithAccountType(uapi.AccountTypeAdmin))
	ctx := context.Background()

	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		defer close()
		natsInstance := jetstream.NATSInstance{}
		routers := httputil.NewRouters()
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)

		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)
		rsAPI.SetFederationAPI(nil, nil)
		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)

		AddPublicRoutes(processCtx, routers, cfg, &natsInstance, cm, nil, rsAPI, nil, nil, nil, userAPI, nil, nil, caching.DisableMetrics)

		accessTokens := map[*test.User]userDevice{
			admin: {},
		}
		createAccessTokens(t, accessTokens, userAPI, ctx, routers)

		for _, path := range adminDualPaths("/quarantine_media_in_room/!room:test") {
			req := test.NewRequest(t, http.MethodPost, path.path)
			req.Header.Set("Authorization", "Bearer "+accessTokens[admin].accessToken)
			rec := httptest.NewRecorder()
			routers.DendriteAdmin.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotImplemented {
				t.Fatalf("expected %d, got %d", http.StatusNotImplemented, rec.Code)
			}
		}
	})
}
