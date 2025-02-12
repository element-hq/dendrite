// Copyright 2024 New Vector Ltd.
// Copyright 2020 The Matrix.org Foundation C.I.C.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package httputil

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matrix-org/util"
)

func TestWrapHandlerInBasicAuth(t *testing.T) {
	type args struct {
		h http.Handler
		b BasicAuth
	}

	dummyHandler := http.HandlerFunc(func(h http.ResponseWriter, r *http.Request) {
		h.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name    string
		args    args
		want    int
		reqAuth bool
	}{
		{
			name:    "no user or password setup",
			args:    args{h: dummyHandler},
			want:    http.StatusOK,
			reqAuth: false,
		},
		{
			name: "only user set",
			args: args{
				h: dummyHandler,
				b: BasicAuth{Username: "test"}, // no basic auth
			},
			want:    http.StatusOK,
			reqAuth: false,
		},
		{
			name: "only pass set",
			args: args{
				h: dummyHandler,
				b: BasicAuth{Password: "test"}, // no basic auth
			},
			want:    http.StatusOK,
			reqAuth: false,
		},
		{
			name: "credentials correct",
			args: args{
				h: dummyHandler,
				b: BasicAuth{Username: "test", Password: "test"}, // basic auth enabled
			},
			want:    http.StatusOK,
			reqAuth: true,
		},
		{
			name: "credentials wrong",
			args: args{
				h: dummyHandler,
				b: BasicAuth{Username: "test1", Password: "test"}, // basic auth enabled
			},
			want:    http.StatusForbidden,
			reqAuth: true,
		},
		{
			name: "no basic auth in request",
			args: args{
				h: dummyHandler,
				b: BasicAuth{Username: "test", Password: "test"}, // basic auth enabled
			},
			want:    http.StatusForbidden,
			reqAuth: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baHandler := WrapHandlerInBasicAuth(tt.args.h, tt.args.b)

			req := httptest.NewRequest("GET", "http://localhost/metrics", nil)
			if tt.reqAuth {
				req.SetBasicAuth("test", "test")
			}

			w := httptest.NewRecorder()
			baHandler(w, req)
			resp := w.Result()

			if resp.StatusCode != tt.want {
				t.Errorf("Expected status code %d, got %d", resp.StatusCode, tt.want)
			}
		})
	}
}

func TestMakeServiceAdminAPI(t *testing.T) {
	serviceToken := "valid_secret_token"
	type args struct {
		f            func(*http.Request) util.JSONResponse
		serviceToken string
	}

	f := func(*http.Request) util.JSONResponse {
		return util.JSONResponse{Code: http.StatusOK}
	}

	tests := []struct {
		name    string
		args    args
		want    int
		reqAuth bool
	}{
		{
			name: "service token valid",
			args: args{
				f:            f,
				serviceToken: serviceToken,
			},
			want:    http.StatusOK,
			reqAuth: true,
		},
		{
			name: "service token invalid",
			args: args{
				f:            f,
				serviceToken: "invalid_service_token",
			},
			want:    http.StatusForbidden,
			reqAuth: true,
		},
		{
			name: "service token is missing",
			args: args{
				f:            f,
				serviceToken: "",
			},
			want:    http.StatusUnauthorized,
			reqAuth: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := MakeServiceAdminAPI("metrics", serviceToken, tt.args.f)

			req := httptest.NewRequest("GET", "http://localhost/admin/v1/username_available", nil)
			if tt.reqAuth {
				req.Header.Add("Authorization", "Bearer "+tt.args.serviceToken)
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			resp := w.Result()

			if resp.StatusCode != tt.want {
				t.Errorf("Expected status code %d, got %d", resp.StatusCode, tt.want)
			}
		})
	}
}
