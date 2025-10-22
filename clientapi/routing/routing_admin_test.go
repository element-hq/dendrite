package routing

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/matrix-org/util"
	"github.com/stretchr/testify/assert"

	userapi "github.com/element-hq/dendrite/userapi/api"
)

// TestCreateAdminV1Router_Success ensures the versioned subrouter wires handlers correctly.
func TestCreateAdminV1Router_Success(t *testing.T) {
	parent := mux.NewRouter()
	dendriteRouter := parent.PathPrefix("/_dendrite").Subrouter()

	v1Router := createAdminV1Router(dendriteRouter)
	if v1Router == nil {
		t.Fatalf("expected v1 router to be created")
	}

	v1Router.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	req := httptest.NewRequest(http.MethodGet, "/_dendrite/admin/v1/test", nil)
	rec := httptest.NewRecorder()
	parent.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTeapot, rec.Code)
}

// TestCreateAdminV1Router_NilRouter_Panics verifies misconfiguration is caught early.
func TestCreateAdminV1Router_NilRouter_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "createAdminV1Router: dendriteAdminRouter must not be nil - check router initialization order", func() {
		createAdminV1Router(nil)
	})
}

func TestDeriveAdminMetricsName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "simple path", path: "/admin/users", want: "admin_users"},
		{name: "path with variable", path: "/admin/users/{userID}", want: "admin_users_userID"},
		{name: "nested path", path: "/admin/rooms/{roomID}/members", want: "admin_rooms_roomID_members"},
		{name: "root admin path", path: "/admin", want: "admin_admin"},
		{name: "admin with trailing slash", path: "/admin/", want: "admin"},
		{name: "path with multiple slashes", path: "/admin//users/", want: "admin_users"},
		{name: "path with special chars", path: "/admin/registration-tokens", want: "admin_registration-tokens"},
		{name: "path with dots", path: "/admin/server.version", want: "admin_server.version"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := deriveAdminMetricsName(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRegisterAdminHandlerDual_Success(t *testing.T) {
	parent := mux.NewRouter()
	dendriteRouter := parent.PathPrefix("/_dendrite").Subrouter()
	v1Router := createAdminV1Router(dendriteRouter)

	handler := func(req *http.Request, device *userapi.Device) util.JSONResponse {
		return util.JSONResponse{Code: http.StatusOK}
	}

	registerAdminHandlerDual(
		dendriteRouter,
		v1Router,
		nil,
		"admin_test",
		"/admin/test",
		"/test",
		handler,
		http.MethodGet,
	)

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "versioned", path: "/_dendrite/admin/v1/test"},
		{name: "legacy", path: "/_dendrite/admin/test"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			parent.ServeHTTP(rec, req)

			assert.Contains(t,
				[]int{http.StatusOK, http.StatusUnauthorized, http.StatusForbidden},
				rec.Code,
				"expected route %s to be registered", tc.path,
			)
		})
	}
}

func TestRegisterAdminHandlerDual_NilDendriteRouter_Panics(t *testing.T) {
	v1Router := mux.NewRouter()
	handler := func(req *http.Request, device *userapi.Device) util.JSONResponse {
		return util.JSONResponse{Code: http.StatusOK}
	}

	assert.PanicsWithValue(t,
		"registerAdminHandlerDual: dendriteRouter must not be nil",
		func() {
			registerAdminHandlerDual(
				nil,
				v1Router,
				nil,
				"admin_test",
				"/admin/test",
				"/test",
				handler,
				http.MethodGet,
			)
		},
	)
}

func TestRegisterAdminHandlerDual_NilV1Router_Panics(t *testing.T) {
	dendriteRouter := mux.NewRouter()
	handler := func(req *http.Request, device *userapi.Device) util.JSONResponse {
		return util.JSONResponse{Code: http.StatusOK}
	}

	assert.PanicsWithValue(t,
		"registerAdminHandlerDual: adminV1Router must not be nil",
		func() {
			registerAdminHandlerDual(
				dendriteRouter,
				nil,
				nil,
				"admin_test",
				"/admin/test",
				"/test",
				handler,
				http.MethodGet,
			)
		},
	)
}

func TestRegisterAdminHandlerDual_NilHandler_Panics(t *testing.T) {
	dendriteRouter := mux.NewRouter()
	v1Router := mux.NewRouter()

	assert.PanicsWithValue(t,
		"registerAdminHandlerDual: handler must not be nil",
		func() {
			registerAdminHandlerDual(
				dendriteRouter,
				v1Router,
				nil,
				"admin_test",
				"/admin/test",
				"/test",
				nil,
				http.MethodGet,
			)
		},
	)
}

func TestRegisterAdminHandlerDual_EmptyMetricsName_DerivesFallback(t *testing.T) {
	parent := mux.NewRouter()
	dendriteRouter := parent.PathPrefix("/_dendrite").Subrouter()
	v1Router := createAdminV1Router(dendriteRouter)

	handler := func(req *http.Request, device *userapi.Device) util.JSONResponse {
		return util.JSONResponse{Code: http.StatusOK}
	}

	assert.NotPanics(t, func() {
		registerAdminHandlerDual(
			dendriteRouter,
			v1Router,
			nil,
			"",
			"/admin/derived",
			"/derived",
			handler,
			http.MethodGet,
		)
	})
}

func TestRegisterAdminHandlerDual_InvalidDerivedMetricsName_Panics(t *testing.T) {
	dendriteRouter := mux.NewRouter()
	v1Router := mux.NewRouter()

	handler := func(req *http.Request, device *userapi.Device) util.JSONResponse {
		return util.JSONResponse{Code: http.StatusOK}
	}

	tests := []struct {
		name            string
		unversionedPath string
	}{
		{name: "empty path", unversionedPath: ""},
		{name: "root path", unversionedPath: "/"},
		{name: "admin trailing slash", unversionedPath: "/admin/"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			assert.PanicsWithValue(t, "registerAdminHandlerDual: invalid metrics name \"admin\" derived from path \""+tt.unversionedPath+"\" - provide explicit metricsName", func() {
				registerAdminHandlerDual(
					dendriteRouter,
					v1Router,
					nil,
					"",
					tt.unversionedPath,
					"/test",
					handler,
					http.MethodGet,
				)
			})
		})
	}
}
