package routing

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	iutil "github.com/element-hq/dendrite/internal/util"
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
			registerAdminRoutes(routers.DendriteAdmin, adminV1Router, cfg, roomAPI, userAPI, nil, natsClient)

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
			registerAdminRoutes(routers.DendriteAdmin, adminV1Router, cfg, roomAPI, userAPI, nil, natsClient)

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

	registerAdminRoutes(routers.DendriteAdmin, adminV1Router, cfg, roomAPI, userAPI, nil, nilConn)

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

	registerAdminRoutes(routers.DendriteAdmin, adminV1Router, cfg, roomAPI, userAPI, nil, natsClient)

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
			registerAdminRoutes(routers.DendriteAdmin, adminV1Router, cfg, roomAPI, userAPI, nil, natsClient)

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
	passwordUpdateCalls        int
	registrationTokens         []clientapiTypes.RegistrationToken
	registrationTokenDetail    *clientapiTypes.RegistrationToken
	markAsStaleInvocationCount int
	storedPasswordResetToken   *userapi.PasswordResetToken
	passwordResetTokenLookup   string
	rateLimitBehavior          map[string][]bool
	threePIDLocalpart          string
	threePIDServerName         spec.ServerName
	threePIDLookupErr          error
	threePIDStoredEmail        string
	deviceDeletionRequests     []*userapi.PerformDeviceDeletionRequest
	pusherDeletionRequests     []*userapi.PerformPusherDeletionRequest
	forget3PIDRequests         []*userapi.PerformForgetThreePIDRequest
	passwordResetAttempts      map[string]*stubPasswordResetAttempt
	emailVerificationSessions  map[string]*userapi.EmailVerificationSession
	savedThreePIDAssociations  []*userapi.PerformSaveThreePIDAssociationRequest
}

type stubPasswordResetAttempt struct {
	SessionID   string
	TokenLookup string
	ExpiresAt   time.Time
	Consumed    bool
}

func ptrTime(t time.Time) *time.Time {
	copy := t
	return &copy
}

func newStubClientUserAPI(device *userapi.Device) *stubClientUserAPI {
	tokenValue := "sample"
	return &stubClientUserAPI{
		device:                    device,
		accountAvailable:          false,
		passwordUpdated:           true,
		registrationTokens:        []clientapiTypes.RegistrationToken{{Token: strPtr(tokenValue)}},
		registrationTokenDetail:   &clientapiTypes.RegistrationToken{Token: strPtr(tokenValue)},
		rateLimitBehavior:         make(map[string][]bool),
		pusherDeletionRequests:    []*userapi.PerformPusherDeletionRequest{},
		passwordResetAttempts:     make(map[string]*stubPasswordResetAttempt),
		emailVerificationSessions: make(map[string]*userapi.EmailVerificationSession),
		savedThreePIDAssociations: []*userapi.PerformSaveThreePIDAssociationRequest{},
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
	s.deviceDeletionRequests = append(s.deviceDeletionRequests, req)
	return nil
}

func (s *stubClientUserAPI) PerformPasswordUpdate(ctx context.Context, req *userapi.PerformPasswordUpdateRequest, res *userapi.PerformPasswordUpdateResponse) error {
	res.PasswordUpdated = s.passwordUpdated
	s.passwordUpdateCalls++
	return nil
}

func (s *stubClientUserAPI) PerformPusherDeletion(ctx context.Context, req *userapi.PerformPusherDeletionRequest, res *struct{}) error {
	s.pusherDeletionRequests = append(s.pusherDeletionRequests, req)
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

func (s *stubClientUserAPI) passwordResetAttemptKey(clientSecret, email string, sendAttempt int) string {
	return fmt.Sprintf("%s|%s|%d", clientSecret, iutil.NormalizeEmail(email), sendAttempt)
}

func (s *stubClientUserAPI) StorePasswordResetToken(ctx context.Context, tokenHash, tokenLookup, userID, email, sessionID, clientSecret string, sendAttempt int, expiresAt time.Time) error {
	key := s.passwordResetAttemptKey(clientSecret, email, sendAttempt)
	if attempt, ok := s.passwordResetAttempts[key]; ok && !attempt.Consumed && time.Now().Before(attempt.ExpiresAt) {
		return userapi.ErrPasswordResetAttemptExists
	}

	s.storedPasswordResetToken = &userapi.PasswordResetToken{
		TokenHash: tokenHash,
		UserID:    userID,
		Email:     email,
		ExpiresAt: expiresAt,
	}
	s.passwordResetTokenLookup = tokenLookup
	s.passwordResetAttempts[key] = &stubPasswordResetAttempt{
		SessionID:   sessionID,
		TokenLookup: tokenLookup,
		ExpiresAt:   expiresAt,
	}
	return nil
}

func (s *stubClientUserAPI) LookupPasswordResetAttempt(ctx context.Context, clientSecret, email string, sendAttempt int) (*userapi.PasswordResetAttempt, error) {
	key := s.passwordResetAttemptKey(clientSecret, email, sendAttempt)
	if attempt, ok := s.passwordResetAttempts[key]; ok {
		if attempt.Consumed {
			return nil, nil
		}
		if time.Now().After(attempt.ExpiresAt) {
			return nil, nil
		}
		return &userapi.PasswordResetAttempt{
			TokenLookup: attempt.TokenLookup,
			SessionID:   attempt.SessionID,
			ExpiresAt:   attempt.ExpiresAt,
		}, nil
	}
	return nil, nil
}

func (s *stubClientUserAPI) GetPasswordResetToken(ctx context.Context, tokenLookup string) (*userapi.PasswordResetToken, error) {
	if s.storedPasswordResetToken != nil && tokenLookup == s.passwordResetTokenLookup {
		return s.storedPasswordResetToken, nil
	}
	return nil, sql.ErrNoRows
}

func (s *stubClientUserAPI) ConsumePasswordResetToken(ctx context.Context, tokenLookup, tokenHash string) (*userapi.ConsumePasswordResetTokenResponse, error) {
	if s.storedPasswordResetToken != nil && tokenLookup == s.passwordResetTokenLookup && tokenHash == s.storedPasswordResetToken.TokenHash {
		s.passwordResetTokenLookup = ""
		s.storedPasswordResetToken = nil
		for key, attempt := range s.passwordResetAttempts {
			if attempt.TokenLookup == tokenLookup {
				attempt.Consumed = true
				s.passwordResetAttempts[key] = attempt
			}
		}
		return &userapi.ConsumePasswordResetTokenResponse{Claimed: true}, nil
	}
	return nil, sql.ErrNoRows
}

func (s *stubClientUserAPI) CheckPasswordResetRateLimit(ctx context.Context, key string, window time.Duration, limit int) (bool, time.Duration, error) {
	if s.rateLimitBehavior == nil {
		return true, window, nil
	}
	if sequence, ok := s.rateLimitBehavior[key]; ok && len(sequence) > 0 {
		allowed := sequence[0]
		s.rateLimitBehavior[key] = sequence[1:]
		if !allowed {
			return false, window, nil
		}
		return true, window, nil
	}
	return true, window, nil
}

func (s *stubClientUserAPI) DeletePasswordResetToken(ctx context.Context, tokenLookup string) error {
	if s.storedPasswordResetToken != nil && s.passwordResetTokenLookup == tokenLookup {
		s.storedPasswordResetToken = nil
		s.passwordResetTokenLookup = ""
	}
	for key, attempt := range s.passwordResetAttempts {
		if attempt.TokenLookup == tokenLookup {
			delete(s.passwordResetAttempts, key)
			break
		}
	}
	return nil
}

func (s *stubClientUserAPI) CreateOrReuseEmailVerificationSession(ctx context.Context, session *userapi.EmailVerificationSession) (*userapi.EmailVerificationSession, bool, error) {
	for _, existing := range s.emailVerificationSessions {
		if existing.ClientSecretHash == session.ClientSecretHash && existing.Email == session.Email && existing.SendAttempt == session.SendAttempt {
			return existing, false, nil
		}
	}
	copy := *session
	s.emailVerificationSessions[session.SessionID] = &copy
	return &copy, true, nil
}

func (s *stubClientUserAPI) GetEmailVerificationSession(ctx context.Context, sessionID string) (*userapi.EmailVerificationSession, error) {
	session, ok := s.emailVerificationSessions[sessionID]
	if !ok {
		return nil, userapi.ErrEmailVerificationSessionNotFound
	}
	return session, nil
}

func (s *stubClientUserAPI) MarkEmailVerificationSessionValidated(ctx context.Context, sessionID string, validatedAt time.Time) error {
	if session, ok := s.emailVerificationSessions[sessionID]; ok {
		session.ValidatedAt = ptrTime(validatedAt)
	}
	return nil
}

func (s *stubClientUserAPI) MarkEmailVerificationSessionConsumed(ctx context.Context, sessionID string, consumedAt time.Time) error {
	if session, ok := s.emailVerificationSessions[sessionID]; ok {
		session.ConsumedAt = ptrTime(consumedAt)
	}
	return nil
}

func (s *stubClientUserAPI) DeleteExpiredEmailVerificationSessions(ctx context.Context, now time.Time) error {
	for id, session := range s.emailVerificationSessions {
		if now.After(session.ExpiresAt) {
			delete(s.emailVerificationSessions, id)
		}
	}
	return nil
}

func (s *stubClientUserAPI) DeleteEmailVerificationSession(ctx context.Context, sessionID string) error {
	delete(s.emailVerificationSessions, sessionID)
	return nil
}

func (s *stubClientUserAPI) CheckEmailVerificationRateLimit(ctx context.Context, key string, window time.Duration, limit int) (bool, time.Duration, error) {
	return s.CheckPasswordResetRateLimit(ctx, key, window, limit)
}

func (s *stubClientUserAPI) PurgeEmailVerificationLimits(ctx context.Context, olderThan time.Time) error {
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
	if s.threePIDLookupErr != nil {
		return s.threePIDLookupErr
	}
	if strings.EqualFold(req.Medium, "email") && s.threePIDStoredEmail != "" {
		if iutil.NormalizeEmail(req.ThreePID) != iutil.NormalizeEmail(s.threePIDStoredEmail) {
			res.Localpart = ""
			res.ServerName = ""
			return nil
		}
	}
	if s.threePIDLocalpart == "" {
		res.Localpart = ""
		res.ServerName = ""
		return nil
	}
	res.Localpart = s.threePIDLocalpart
	res.ServerName = s.threePIDServerName
	return nil
}

func (s *stubClientUserAPI) PerformForgetThreePID(ctx context.Context, req *userapi.PerformForgetThreePIDRequest, res *struct{}) error {
	s.forget3PIDRequests = append(s.forget3PIDRequests, req)
	return nil
}

func (s *stubClientUserAPI) PerformSaveThreePIDAssociation(ctx context.Context, req *userapi.PerformSaveThreePIDAssociationRequest, res *struct{}) error {
	s.savedThreePIDAssociations = append(s.savedThreePIDAssociations, &userapi.PerformSaveThreePIDAssociationRequest{
		ThreePID:   req.ThreePID,
		Localpart:  req.Localpart,
		ServerName: req.ServerName,
		Medium:     req.Medium,
	})
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
