package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	clientapiTypes "github.com/element-hq/dendrite/clientapi/api"
	"github.com/element-hq/dendrite/clientapi/auth/authtypes"
	"github.com/element-hq/dendrite/internal/httputil"
	"github.com/element-hq/dendrite/internal/pushrules"
	"github.com/element-hq/dendrite/setup/config"
	userapi "github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
)

func TestAdminV1RouterCreated(t *testing.T) {
	routers := httputil.NewRouters()
	adminV1Router := createAdminV1Router(routers.DendriteAdmin)
	if adminV1Router == nil {
		t.Fatal("expected adminV1Router to be created, got nil")
	}

	adminV1Router.Handle("/probe", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).Methods(http.MethodGet)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/_dendrite/admin/v1/probe", nil)
	routers.DendriteAdmin.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
}

func TestV1EndpointRegistration(t *testing.T) {
	routers := httputil.NewRouters()
	adminV1Router := createAdminV1Router(routers.DendriteAdmin)

	adminV1Router.Handle("/echo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})).Methods(http.MethodGet)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/_dendrite/admin/v1/echo", nil)
	routers.DendriteAdmin.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected status %d, got %d", http.StatusTeapot, rec.Code)
	}
}

func TestDualRegistrationBothPathsWork(t *testing.T) {
	routers := httputil.NewRouters()
	adminV1Router := createAdminV1Router(routers.DendriteAdmin)
	userAPI := newStubClientUserAPI(&userapi.Device{
		UserID:      "@admin:test",
		AccountType: userapi.AccountTypeAdmin,
	})

	var hits int
	handler := AdminHandler(func(req *http.Request, device *userapi.Device) util.JSONResponse {
		hits++
		return util.JSONResponse{
			Code: http.StatusAccepted,
			JSON: map[string]string{"result": "ok"},
		}
	})

	registerAdminHandlerDual(
		routers.DendriteAdmin,
		adminV1Router,
		userAPI,
		"admin_dual",
		"/admin/dual",
		"/dual",
		handler,
		http.MethodGet,
	)

	for _, path := range []string{"/_dendrite/admin/dual", "/_dendrite/admin/v1/dual"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer test-token")
		routers.DendriteAdmin.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected status %d for %s, got %d", http.StatusAccepted, path, rec.Code)
		}
		if body := strings.TrimSpace(rec.Body.String()); !strings.Contains(body, `"result":"ok"`) {
			t.Fatalf("unexpected body for %s: %s", path, body)
		}
	}

	if hits != 2 {
		t.Fatalf("expected handler to be invoked twice, got %d", hits)
	}
}

func TestDualRegistrationSameResponse(t *testing.T) {
	routers := httputil.NewRouters()
	adminV1Router := createAdminV1Router(routers.DendriteAdmin)
	userAPI := newStubClientUserAPI(&userapi.Device{
		UserID:      "@admin:test",
		AccountType: userapi.AccountTypeAdmin,
	})

	handler := AdminHandler(func(req *http.Request, device *userapi.Device) util.JSONResponse {
		return util.JSONResponse{
			Code: http.StatusOK,
			JSON: map[string]string{"value": "identical"},
		}
	})

	registerAdminHandlerDual(
		routers.DendriteAdmin,
		adminV1Router,
		userAPI,
		"admin_match",
		"/admin/match",
		"/match",
		handler,
		http.MethodGet,
	)

	request := func(path string) string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer test-token")
		routers.DendriteAdmin.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status %d for %s, got %d", http.StatusOK, path, rec.Code)
		}
		return strings.TrimSpace(rec.Body.String())
	}

	unversionedBody := request("/_dendrite/admin/match")
	versionedBody := request("/_dendrite/admin/v1/match")

	if unversionedBody != versionedBody {
		t.Fatalf("expected identical responses, got %q and %q", unversionedBody, versionedBody)
	}
}

func TestUnversionedPathLogsWarning(t *testing.T) {
	hook := logrustest.NewGlobal()
	defer hook.Reset()

	routers := httputil.NewRouters()
	adminV1Router := createAdminV1Router(routers.DendriteAdmin)
	userAPI := newStubClientUserAPI(&userapi.Device{
		UserID:      "@admin:test",
		AccountType: userapi.AccountTypeAdmin,
	})

	handler := AdminHandler(func(req *http.Request, device *userapi.Device) util.JSONResponse {
		return util.JSONResponse{
			Code: http.StatusOK,
			JSON: map[string]string{"deprecated": "path"},
		}
	})

	registerAdminHandlerDual(
		routers.DendriteAdmin,
		adminV1Router,
		userAPI,
		"admin_warn",
		"/admin/warn",
		"/warn",
		handler,
		http.MethodGet,
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/_dendrite/admin/warn", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	routers.DendriteAdmin.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	entries := hook.AllEntries()
	if len(entries) == 0 {
		t.Fatal("expected warning log entry for deprecated path")
	}
	entry := entries[len(entries)-1]
	if entry.Level != logrus.WarnLevel {
		t.Fatalf("expected warn level log, got %s", entry.Level)
	}
	if got := entry.Data["deprecated_path"]; got != "/_dendrite/admin/warn" {
		t.Fatalf("expected deprecated_path to be %q, got %v", "/_dendrite/admin/warn", got)
	}
	if got := entry.Data["recommended_path"]; got != "/_dendrite/admin/v1/warn" {
		t.Fatalf("expected recommended_path to be %q, got %v", "/_dendrite/admin/v1/warn", got)
	}
	if !strings.Contains(entry.Message, "Deprecated unversioned admin API endpoint used") {
		t.Fatalf("expected warning message to mention deprecation, got %q", entry.Message)
	}
}

func TestV1PathNoWarning(t *testing.T) {
	hook := logrustest.NewGlobal()
	defer hook.Reset()

	routers := httputil.NewRouters()
	adminV1Router := createAdminV1Router(routers.DendriteAdmin)
	userAPI := newStubClientUserAPI(&userapi.Device{
		UserID:      "@admin:test",
		AccountType: userapi.AccountTypeAdmin,
	})

	handler := AdminHandler(func(req *http.Request, device *userapi.Device) util.JSONResponse {
		return util.JSONResponse{
			Code: http.StatusOK,
			JSON: map[string]string{"preferred": "path"},
		}
	})

	registerAdminHandlerDual(
		routers.DendriteAdmin,
		adminV1Router,
		userAPI,
		"admin_nowarn",
		"/admin/nowarn",
		"/nowarn",
		handler,
		http.MethodGet,
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/_dendrite/admin/v1/nowarn", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	routers.DendriteAdmin.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	for _, entry := range hook.AllEntries() {
		if entry.Level >= logrus.WarnLevel {
			t.Fatalf("expected no warning logs, got %s with message %q", entry.Level, entry.Message)
		}
	}
}

func TestAdminEndpointsDualPaths(t *testing.T) {
	cases := []struct {
		name          string
		method        string
		legacyPath    string
		versionedPath string
		body          []byte
		expectedCode  int
	}{
		{
			name:          "registration token create",
			method:        http.MethodPost,
			legacyPath:    "/_dendrite/admin/registrationTokens/new",
			versionedPath: "/_dendrite/admin/v1/registrationTokens/new",
			body:          []byte(`{"token":"sample","length":16}`),
			expectedCode:  http.StatusOK,
		},
		{
			name:          "registration token list",
			method:        http.MethodGet,
			legacyPath:    "/_dendrite/admin/registrationTokens",
			versionedPath: "/_dendrite/admin/v1/registrationTokens",
			expectedCode:  http.StatusOK,
		},
		{
			name:          "registration token get",
			method:        http.MethodGet,
			legacyPath:    "/_dendrite/admin/registrationTokens/sample",
			versionedPath: "/_dendrite/admin/v1/registrationTokens/sample",
			expectedCode:  http.StatusOK,
		},
		{
			name:          "registration token update",
			method:        http.MethodPut,
			legacyPath:    "/_dendrite/admin/registrationTokens/sample",
			versionedPath: "/_dendrite/admin/v1/registrationTokens/sample",
			body:          []byte(`{"uses_allowed":5}`),
			expectedCode:  http.StatusOK,
		},
		{
			name:          "registration token delete",
			method:        http.MethodDelete,
			legacyPath:    "/_dendrite/admin/registrationTokens/sample",
			versionedPath: "/_dendrite/admin/v1/registrationTokens/sample",
			expectedCode:  http.StatusOK,
		},
		{
			name:          "evacuate room",
			method:        http.MethodPost,
			legacyPath:    "/_dendrite/admin/evacuateRoom/!room:test",
			versionedPath: "/_dendrite/admin/v1/evacuateRoom/!room:test",
			expectedCode:  http.StatusOK,
		},
		{
			name:          "evacuate user",
			method:        http.MethodPost,
			legacyPath:    "/_dendrite/admin/evacuateUser/@user:test",
			versionedPath: "/_dendrite/admin/v1/evacuateUser/@user:test",
			expectedCode:  http.StatusOK,
		},
		{
			name:          "purge room",
			method:        http.MethodPost,
			legacyPath:    "/_dendrite/admin/purgeRoom/!room:test",
			versionedPath: "/_dendrite/admin/v1/purgeRoom/!room:test",
			expectedCode:  http.StatusOK,
		},
		{
			name:          "reset password",
			method:        http.MethodPost,
			legacyPath:    "/_dendrite/admin/resetPassword/@user:local",
			versionedPath: "/_dendrite/admin/v1/resetPassword/@user:local",
			body:          []byte(`{"password":"strongpass","logout_devices":false}`),
			expectedCode:  http.StatusOK,
		},
		{
			name:          "download state",
			method:        http.MethodGet,
			legacyPath:    "/_dendrite/admin/downloadState/example.com/!room:test",
			versionedPath: "/_dendrite/admin/v1/downloadState/example.com/!room:test",
			expectedCode:  http.StatusOK,
		},
		{
			name:          "reindex",
			method:        http.MethodGet,
			legacyPath:    "/_dendrite/admin/fulltext/reindex",
			versionedPath: "/_dendrite/admin/v1/fulltext/reindex",
			expectedCode:  http.StatusOK,
		},
		{
			name:          "refresh devices",
			method:        http.MethodPost,
			legacyPath:    "/_dendrite/admin/refreshDevices/@remote:example.com",
			versionedPath: "/_dendrite/admin/v1/refreshDevices/@remote:example.com",
			expectedCode:  http.StatusOK,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			routers := httputil.NewRouters()
			adminV1Router := createAdminV1Router(routers.DendriteAdmin)
			cfg := newTestClientAPIConfig()
			userAPI := newStubClientUserAPI(&userapi.Device{
				UserID:      "@admin:local",
				AccountType: userapi.AccountTypeAdmin,
			})
			roomAPI := &stubRoomserverAPI{
				evacuateRoomResponse: []string{"roomA"},
				evacuateUserResponse: []string{"roomA"},
			}
			natsClient := &stubNATSRequester{}
			registerAdminRoutes(routers.DendriteAdmin, adminV1Router, cfg, roomAPI, userAPI, natsClient)

			doRequest := func(path string) (int, any) {
				bodyBytes := tc.body
				if bodyBytes == nil {
					bodyBytes = []byte{}
				}
				req := httptest.NewRequest(tc.method, path, bytes.NewReader(bodyBytes))
				if len(bodyBytes) > 0 {
					req.Header.Set("Content-Type", "application/json")
				}
				req.Header.Set("Authorization", "Bearer test-token")
				rec := httptest.NewRecorder()
				routers.DendriteAdmin.ServeHTTP(rec, req)
				if rec.Code != tc.expectedCode {
					return rec.Code, rec.Body.String()
				}
				trimmed := strings.TrimSpace(rec.Body.String())
				if trimmed == "" {
					return rec.Code, nil
				}
				var data any
				if err := json.Unmarshal([]byte(trimmed), &data); err != nil {
					t.Fatalf("failed to decode JSON response for %s: %s", path, err)
				}
				return rec.Code, data
			}

			statusLegacy, bodyLegacy := doRequest(tc.legacyPath)
			if statusLegacy != tc.expectedCode {
				t.Fatalf("unexpected legacy status: got %d want %d, body: %v", statusLegacy, tc.expectedCode, bodyLegacy)
			}

			statusVersioned, bodyVersioned := doRequest(tc.versionedPath)
			if statusVersioned != tc.expectedCode {
				t.Fatalf("unexpected versioned status: got %d want %d, body: %v", statusVersioned, tc.expectedCode, bodyVersioned)
			}

			if !reflect.DeepEqual(bodyLegacy, bodyVersioned) {
				t.Fatalf("expected identical responses, got %v and %v", bodyLegacy, bodyVersioned)
			}
		})
	}
}

func TestAdminEndpointsRequireAuthentication(t *testing.T) {
	cases := []struct {
		name          string
		method        string
		legacyPath    string
		versionedPath string
		body          []byte
	}{
		{
			name:          "create token requires auth",
			method:        http.MethodPost,
			legacyPath:    "/_dendrite/admin/registrationTokens/new",
			versionedPath: "/_dendrite/admin/v1/registrationTokens/new",
			body:          []byte(`{"token":"sample","length":16}`),
		},
		{
			name:          "reset password requires auth",
			method:        http.MethodPost,
			legacyPath:    "/_dendrite/admin/resetPassword/@user:local",
			versionedPath: "/_dendrite/admin/v1/resetPassword/@user:local",
			body:          []byte(`{"password":"strongpass","logout_devices":false}`),
		},
		{
			name:          "reindex requires auth",
			method:        http.MethodGet,
			legacyPath:    "/_dendrite/admin/fulltext/reindex",
			versionedPath: "/_dendrite/admin/v1/fulltext/reindex",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			routers := httputil.NewRouters()
			adminV1Router := createAdminV1Router(routers.DendriteAdmin)
			cfg := newTestClientAPIConfig()
			userAPI := newStubClientUserAPI(&userapi.Device{
				UserID:      "@admin:local",
				AccountType: userapi.AccountTypeAdmin,
			})
			roomAPI := &stubRoomserverAPI{}
			natsClient := &stubNATSRequester{}
			registerAdminRoutes(routers.DendriteAdmin, adminV1Router, cfg, roomAPI, userAPI, natsClient)

			send := func(path string) int {
				bodyBytes := tc.body
				if bodyBytes == nil {
					bodyBytes = []byte{}
				}
				req := httptest.NewRequest(tc.method, path, bytes.NewReader(bodyBytes))
				if len(bodyBytes) > 0 {
					req.Header.Set("Content-Type", "application/json")
				}
				rec := httptest.NewRecorder()
				routers.DendriteAdmin.ServeHTTP(rec, req)
				return rec.Code
			}

			if status := send(tc.legacyPath); status != http.StatusUnauthorized {
				t.Fatalf("expected legacy path to require auth: got %d", status)
			}
			if status := send(tc.versionedPath); status != http.StatusUnauthorized {
				t.Fatalf("expected versioned path to require auth: got %d", status)
			}
		})
	}
}

func TestReindexRouteRequiresNATSConnection(t *testing.T) {
	routers := httputil.NewRouters()
	adminV1Router := createAdminV1Router(routers.DendriteAdmin)
	cfg := newTestClientAPIConfig()
	userAPI := newStubClientUserAPI(&userapi.Device{
		UserID:      "@admin:local",
		AccountType: userapi.AccountTypeAdmin,
	})
	roomAPI := &stubRoomserverAPI{}
	var nilConn *nats.Conn

	registerAdminRoutes(routers.DendriteAdmin, adminV1Router, cfg, roomAPI, userAPI, nilConn)

	req := httptest.NewRequest(http.MethodGet, "/_dendrite/admin/fulltext/reindex", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	routers.DendriteAdmin.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unregistered reindex route, got %d", rec.Code)
	}
}

func TestReindexRouteUsesRequester(t *testing.T) {
	routers := httputil.NewRouters()
	adminV1Router := createAdminV1Router(routers.DendriteAdmin)
	cfg := newTestClientAPIConfig()
	userAPI := newStubClientUserAPI(&userapi.Device{
		UserID:      "@admin:local",
		AccountType: userapi.AccountTypeAdmin,
	})
	roomAPI := &stubRoomserverAPI{}
	natsClient := &stubNATSRequester{}

	registerAdminRoutes(routers.DendriteAdmin, adminV1Router, cfg, roomAPI, userAPI, natsClient)

	req := httptest.NewRequest(http.MethodGet, "/_dendrite/admin/v1/fulltext/reindex", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	routers.DendriteAdmin.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from reindex with requester, got %d", rec.Code)
	}
	if natsClient.count != 1 {
		t.Fatalf("expected requester to be used once, got %d", natsClient.count)
	}
}

func TestAdminEndpointsRejectNonAdminUsers(t *testing.T) {
	cases := []struct {
		name          string
		method        string
		legacyPath    string
		versionedPath string
		body          []byte
	}{
		{
			name:          "create token forbidden",
			method:        http.MethodPost,
			legacyPath:    "/_dendrite/admin/registrationTokens/new",
			versionedPath: "/_dendrite/admin/v1/registrationTokens/new",
			body:          []byte(`{"token":"sample","length":16}`),
		},
		{
			name:          "reset password forbidden",
			method:        http.MethodPost,
			legacyPath:    "/_dendrite/admin/resetPassword/@user:local",
			versionedPath: "/_dendrite/admin/v1/resetPassword/@user:local",
			body:          []byte(`{"password":"strongpass","logout_devices":false}`),
		},
		{
			name:          "reindex forbidden",
			method:        http.MethodGet,
			legacyPath:    "/_dendrite/admin/fulltext/reindex",
			versionedPath: "/_dendrite/admin/v1/fulltext/reindex",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			routers := httputil.NewRouters()
			adminV1Router := createAdminV1Router(routers.DendriteAdmin)
			cfg := newTestClientAPIConfig()
			userAPI := newStubClientUserAPI(&userapi.Device{
				UserID:      "@regular:local",
				AccountType: userapi.AccountTypeUser,
			})
			roomAPI := &stubRoomserverAPI{}
			natsClient := &stubNATSRequester{}
			registerAdminRoutes(routers.DendriteAdmin, adminV1Router, cfg, roomAPI, userAPI, natsClient)

			send := func(path string) int {
				bodyBytes := tc.body
				if bodyBytes == nil {
					bodyBytes = []byte{}
				}
				req := httptest.NewRequest(tc.method, path, bytes.NewReader(bodyBytes))
				if len(bodyBytes) > 0 {
					req.Header.Set("Content-Type", "application/json")
				}
				req.Header.Set("Authorization", "Bearer test-token")
				rec := httptest.NewRecorder()
				routers.DendriteAdmin.ServeHTTP(rec, req)
				return rec.Code
			}

			if status := send(tc.legacyPath); status != http.StatusForbidden {
				t.Fatalf("expected legacy path to forbid non-admin: got %d", status)
			}
			if status := send(tc.versionedPath); status != http.StatusForbidden {
				t.Fatalf("expected versioned path to forbid non-admin: got %d", status)
			}
		})
	}
}

func newTestClientAPIConfig() *config.ClientAPI {
	global := &config.Global{}
	global.ServerName = spec.ServerName("local")
	global.JetStream.TopicPrefix = "test."

	return &config.ClientAPI{
		Matrix:                    global,
		RegistrationRequiresToken: true,
	}
}

func strPtr(s string) *string {
	return &s
}

type stubRoomserverAPI struct {
	evacuateRoomResponse []string
	evacuateUserResponse []string
	purgeRoomErr         error
	downloadStateErr     error
}

func (s *stubRoomserverAPI) PerformAdminEvacuateRoom(ctx context.Context, roomID string) ([]string, error) {
	return s.evacuateRoomResponse, s.purgeRoomErr
}

func (s *stubRoomserverAPI) PerformAdminEvacuateUser(ctx context.Context, userID string) ([]string, error) {
	return s.evacuateUserResponse, s.purgeRoomErr
}

func (s *stubRoomserverAPI) PerformAdminPurgeRoom(ctx context.Context, roomID string) error {
	return s.purgeRoomErr
}

func (s *stubRoomserverAPI) PerformAdminDownloadState(ctx context.Context, roomID, userID string, serverName spec.ServerName) error {
	return s.downloadStateErr
}

type stubNATSRequester struct {
	count int
	err   error
}

func (s *stubNATSRequester) RequestMsg(msg *nats.Msg, timeout time.Duration) (*nats.Msg, error) {
	s.count++
	if s.err != nil {
		return nil, s.err
	}
	return nats.NewMsg("test"), nil
}

type stubClientUserAPI struct {
	device                     *userapi.Device
	accountAvailable           bool
	passwordUpdated            bool
	registrationTokens         []clientapiTypes.RegistrationToken
	registrationTokenDetail    *clientapiTypes.RegistrationToken
	markAsStaleInvocationCount int
}

func newStubClientUserAPI(device *userapi.Device) *stubClientUserAPI {
	tokenValue := "sample"
	return &stubClientUserAPI{
		device:                  device,
		accountAvailable:        false,
		passwordUpdated:         true,
		registrationTokens:      []clientapiTypes.RegistrationToken{{Token: strPtr(tokenValue)}},
		registrationTokenDetail: &clientapiTypes.RegistrationToken{Token: strPtr(tokenValue)},
	}
}

var _ userapi.ClientUserAPI = (*stubClientUserAPI)(nil)

func (s *stubClientUserAPI) QueryAccessToken(ctx context.Context, req *userapi.QueryAccessTokenRequest, res *userapi.QueryAccessTokenResponse) error {
	res.Device = s.device
	return nil
}

func (s *stubClientUserAPI) PerformLoginTokenCreation(ctx context.Context, req *userapi.PerformLoginTokenCreationRequest, res *userapi.PerformLoginTokenCreationResponse) error {
	return nil
}

func (s *stubClientUserAPI) PerformLoginTokenDeletion(ctx context.Context, req *userapi.PerformLoginTokenDeletionRequest, res *userapi.PerformLoginTokenDeletionResponse) error {
	return nil
}

func (s *stubClientUserAPI) QueryLoginToken(ctx context.Context, req *userapi.QueryLoginTokenRequest, res *userapi.QueryLoginTokenResponse) error {
	return nil
}

func (s *stubClientUserAPI) QueryAccountByPassword(ctx context.Context, req *userapi.QueryAccountByPasswordRequest, res *userapi.QueryAccountByPasswordResponse) error {
	return nil
}

func (s *stubClientUserAPI) QueryProfile(ctx context.Context, userID string) (*authtypes.Profile, error) {
	return nil, nil
}

func (s *stubClientUserAPI) SetAvatarURL(ctx context.Context, localpart string, serverName spec.ServerName, avatarURL string) (*authtypes.Profile, bool, error) {
	return nil, false, nil
}

func (s *stubClientUserAPI) SetDisplayName(ctx context.Context, localpart string, serverName spec.ServerName, displayName string) (*authtypes.Profile, bool, error) {
	return nil, false, nil
}

func (s *stubClientUserAPI) QueryNumericLocalpart(ctx context.Context, req *userapi.QueryNumericLocalpartRequest, res *userapi.QueryNumericLocalpartResponse) error {
	return nil
}

func (s *stubClientUserAPI) QueryDevices(ctx context.Context, req *userapi.QueryDevicesRequest, res *userapi.QueryDevicesResponse) error {
	return nil
}

func (s *stubClientUserAPI) QueryAccountData(ctx context.Context, req *userapi.QueryAccountDataRequest, res *userapi.QueryAccountDataResponse) error {
	return nil
}

func (s *stubClientUserAPI) QueryPushers(ctx context.Context, req *userapi.QueryPushersRequest, res *userapi.QueryPushersResponse) error {
	return nil
}

func (s *stubClientUserAPI) QueryPushRules(ctx context.Context, userID string) (*pushrules.AccountRuleSets, error) {
	return nil, nil
}

func (s *stubClientUserAPI) QueryAccountAvailability(ctx context.Context, req *userapi.QueryAccountAvailabilityRequest, res *userapi.QueryAccountAvailabilityResponse) error {
	res.Available = s.accountAvailable
	return nil
}

func (s *stubClientUserAPI) PerformAdminCreateRegistrationToken(ctx context.Context, registrationToken *clientapiTypes.RegistrationToken) (bool, error) {
	s.registrationTokens = append(s.registrationTokens, *registrationToken)
	s.registrationTokenDetail = registrationToken
	return true, nil
}

func (s *stubClientUserAPI) PerformAdminListRegistrationTokens(ctx context.Context, returnAll bool, valid bool) ([]clientapiTypes.RegistrationToken, error) {
	return s.registrationTokens, nil
}

func (s *stubClientUserAPI) PerformAdminGetRegistrationToken(ctx context.Context, tokenString string) (*clientapiTypes.RegistrationToken, error) {
	return s.registrationTokenDetail, nil
}

func (s *stubClientUserAPI) PerformAdminDeleteRegistrationToken(ctx context.Context, tokenString string) error {
	return nil
}

func (s *stubClientUserAPI) PerformAdminUpdateRegistrationToken(ctx context.Context, tokenString string, newAttributes map[string]interface{}) (*clientapiTypes.RegistrationToken, error) {
	return s.registrationTokenDetail, nil
}

func (s *stubClientUserAPI) QueryAdminUsers(ctx context.Context, req *userapi.QueryAdminUsersRequest, res *userapi.QueryAdminUsersResponse) error {
	res.Users = nil
	res.Total = 0
	res.NextFrom = -1
	return nil
}

func (s *stubClientUserAPI) PerformAccountCreation(ctx context.Context, req *userapi.PerformAccountCreationRequest, res *userapi.PerformAccountCreationResponse) error {
	return nil
}

func (s *stubClientUserAPI) PerformDeviceCreation(ctx context.Context, req *userapi.PerformDeviceCreationRequest, res *userapi.PerformDeviceCreationResponse) error {
	return nil
}

func (s *stubClientUserAPI) PerformDeviceUpdate(ctx context.Context, req *userapi.PerformDeviceUpdateRequest, res *userapi.PerformDeviceUpdateResponse) error {
	return nil
}

func (s *stubClientUserAPI) PerformDeviceDeletion(ctx context.Context, req *userapi.PerformDeviceDeletionRequest, res *userapi.PerformDeviceDeletionResponse) error {
	return nil
}

func (s *stubClientUserAPI) PerformPasswordUpdate(ctx context.Context, req *userapi.PerformPasswordUpdateRequest, res *userapi.PerformPasswordUpdateResponse) error {
	res.PasswordUpdated = s.passwordUpdated
	return nil
}

func (s *stubClientUserAPI) PerformPusherDeletion(ctx context.Context, req *userapi.PerformPusherDeletionRequest, res *struct{}) error {
	return nil
}

func (s *stubClientUserAPI) PerformPusherSet(ctx context.Context, req *userapi.PerformPusherSetRequest, res *struct{}) error {
	return nil
}

func (s *stubClientUserAPI) PerformPushRulesPut(ctx context.Context, userID string, ruleSets *pushrules.AccountRuleSets) error {
	return nil
}

func (s *stubClientUserAPI) PerformAccountDeactivation(ctx context.Context, req *userapi.PerformAccountDeactivationRequest, res *userapi.PerformAccountDeactivationResponse) error {
	return nil
}

func (s *stubClientUserAPI) PerformUserDeactivation(ctx context.Context, req *userapi.PerformUserDeactivationRequest, res *userapi.PerformUserDeactivationResponse) error {
	return nil
}

func (s *stubClientUserAPI) PerformOpenIDTokenCreation(ctx context.Context, req *userapi.PerformOpenIDTokenCreationRequest, res *userapi.PerformOpenIDTokenCreationResponse) error {
	return nil
}

func (s *stubClientUserAPI) QueryNotifications(ctx context.Context, req *userapi.QueryNotificationsRequest, res *userapi.QueryNotificationsResponse) error {
	return nil
}

func (s *stubClientUserAPI) InputAccountData(ctx context.Context, req *userapi.InputAccountDataRequest, res *userapi.InputAccountDataResponse) error {
	return nil
}

func (s *stubClientUserAPI) QueryThreePIDsForLocalpart(ctx context.Context, req *userapi.QueryThreePIDsForLocalpartRequest, res *userapi.QueryThreePIDsForLocalpartResponse) error {
	return nil
}

func (s *stubClientUserAPI) QueryLocalpartForThreePID(ctx context.Context, req *userapi.QueryLocalpartForThreePIDRequest, res *userapi.QueryLocalpartForThreePIDResponse) error {
	return nil
}

func (s *stubClientUserAPI) PerformForgetThreePID(ctx context.Context, req *userapi.PerformForgetThreePIDRequest, res *struct{}) error {
	return nil
}

func (s *stubClientUserAPI) PerformSaveThreePIDAssociation(ctx context.Context, req *userapi.PerformSaveThreePIDAssociationRequest, res *struct{}) error {
	return nil
}

func (s *stubClientUserAPI) QueryKeys(ctx context.Context, req *userapi.QueryKeysRequest, res *userapi.QueryKeysResponse) {
}

func (s *stubClientUserAPI) PerformUploadKeys(ctx context.Context, req *userapi.PerformUploadKeysRequest, res *userapi.PerformUploadKeysResponse) error {
	return nil
}

func (s *stubClientUserAPI) PerformUploadDeviceSignatures(ctx context.Context, req *userapi.PerformUploadDeviceSignaturesRequest, res *userapi.PerformUploadDeviceSignaturesResponse) {
}

func (s *stubClientUserAPI) PerformClaimKeys(ctx context.Context, req *userapi.PerformClaimKeysRequest, res *userapi.PerformClaimKeysResponse) {
}

func (s *stubClientUserAPI) PerformMarkAsStaleIfNeeded(ctx context.Context, req *userapi.PerformMarkAsStaleRequest, res *struct{}) error {
	s.markAsStaleInvocationCount++
	return nil
}

func (s *stubClientUserAPI) PerformUploadDeviceKeys(ctx context.Context, req *userapi.PerformUploadDeviceKeysRequest, res *userapi.PerformUploadDeviceKeysResponse) {
}

func (s *stubClientUserAPI) DeleteKeyBackup(ctx context.Context, userID, version string) (bool, error) {
	return false, nil
}

func (s *stubClientUserAPI) PerformKeyBackup(ctx context.Context, req *userapi.PerformKeyBackupRequest) (string, error) {
	return "", nil
}

func (s *stubClientUserAPI) QueryKeyBackup(ctx context.Context, req *userapi.QueryKeyBackupRequest) (*userapi.QueryKeyBackupResponse, error) {
	return nil, nil
}

func (s *stubClientUserAPI) UpdateBackupKeyAuthData(ctx context.Context, req *userapi.PerformKeyBackupRequest) (*userapi.PerformKeyBackupResponse, error) {
	return nil, nil
}

func (s *stubClientUserAPI) QueryKeyChanges(ctx context.Context, req *userapi.QueryKeyChangesRequest, res *userapi.QueryKeyChangesResponse) error {
	return nil
}

func (s *stubClientUserAPI) QueryOneTimeKeys(ctx context.Context, req *userapi.QueryOneTimeKeysRequest, res *userapi.QueryOneTimeKeysResponse) error {
	return nil
}

func (s *stubClientUserAPI) QueryDeviceInfos(ctx context.Context, req *userapi.QueryDeviceInfosRequest, res *userapi.QueryDeviceInfosResponse) error {
	return nil
}
