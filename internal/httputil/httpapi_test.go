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
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
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

func TestMakeHTTPAPIRecordsDurationHistogram(t *testing.T) {
	clientAPIRequestDuration.Reset()

	handler := MakeHTTPAPI("test_http_duration", nil, true, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/_matrix/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	metrics := make(chan prometheus.Metric, 10)
	clientAPIRequestDuration.Collect(metrics)
	close(metrics)

	found := false
	for metric := range metrics {
		dtoMetric := &dto.Metric{}
		require.NoError(t, metric.Write(dtoMetric))
		if dtoMetric.GetHistogram() == nil {
			continue
		}
		for _, label := range dtoMetric.GetLabel() {
			if label.GetName() == "handler" && label.GetValue() == "test_http_duration" {
				found = true
				require.Equal(t, uint64(1), dtoMetric.GetHistogram().GetSampleCount(), "expected a single observed request")
				require.Greater(t, dtoMetric.GetHistogram().GetSampleSum(), float64(0), "expected positive observed duration")
			}
		}
	}
	require.True(t, found, "expected histogram metric for handler test_http_duration")
}
