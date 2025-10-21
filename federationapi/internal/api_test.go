// Copyright 2024 New Vector Ltd.
// Copyright 2022 The Matrix.org Foundation C.I.C.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package internal

import (
	"testing"
	"time"

	"github.com/element-hq/dendrite/federationapi/statistics"
	"github.com/element-hq/dendrite/test"
	"github.com/matrix-org/gomatrix"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/stretchr/testify/assert"
)

// Test failBlacklistableError with nil error
func TestFailBlacklistableError_NilError_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	testDB := test.NewInMemoryFederationDatabase()
	stats := statistics.NewStatistics(testDB, FailuresUntilBlacklist, FailuresUntilAssumedOffline, false)
	serverStats := stats.ForServer(spec.ServerName("test"))

	until, blacklisted := failBlacklistableError(nil, serverStats)

	assert.Equal(t, time.Time{}, until, "nil error should return zero time")
	assert.False(t, blacklisted, "nil error should not blacklist")
}

// Test failBlacklistableError with HTTP 401 error
func TestFailBlacklistableError_HTTP401_ReturnsFailure(t *testing.T) {
	t.Parallel()

	testDB := test.NewInMemoryFederationDatabase()
	stats := statistics.NewStatistics(testDB, FailuresUntilBlacklist, FailuresUntilAssumedOffline, false)
	serverStats := stats.ForServer(spec.ServerName("test"))

	err := gomatrix.HTTPError{
		Code:    401,
		Message: "Unauthorized",
	}

	until, blacklisted := failBlacklistableError(err, serverStats)

	// Should trigger backoff but not immediate blacklist
	assert.False(t, blacklisted, "single 401 should not immediately blacklist")
	// The until time should be set (backoff started)
	assert.False(t, until.IsZero(), "failure should return a valid backoff time")
	assert.True(t, until.After(time.Now()), "backoff time should be in the future")
}

// Test failBlacklistableError with HTTP 500 error
func TestFailBlacklistableError_HTTP500_ReturnsFailure(t *testing.T) {
	t.Parallel()

	testDB := test.NewInMemoryFederationDatabase()
	stats := statistics.NewStatistics(testDB, FailuresUntilBlacklist, FailuresUntilAssumedOffline, false)
	serverStats := stats.ForServer(spec.ServerName("test"))

	err := gomatrix.HTTPError{
		Code:    500,
		Message: "Internal Server Error",
	}

	until, blacklisted := failBlacklistableError(err, serverStats)

	assert.False(t, blacklisted, "single 500 should not immediately blacklist")
	assert.False(t, until.IsZero(), "failure should return a valid backoff time")
	assert.True(t, until.After(time.Now()), "backoff time should be in the future")
}

// Test failBlacklistableError with various 5xx errors
func TestFailBlacklistableError_HTTP5xx_ReturnsFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		httpCode int
	}{
		{"500 Internal Server Error", 500},
		{"502 Bad Gateway", 502},
		{"503 Service Unavailable", 503},
		{"504 Gateway Timeout", 504},
		{"599 Custom 5xx", 599},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			testDB := test.NewInMemoryFederationDatabase()
			stats := statistics.NewStatistics(testDB, FailuresUntilBlacklist, FailuresUntilAssumedOffline, false)
			serverStats := stats.ForServer(spec.ServerName("test_" + tt.name))

			err := gomatrix.HTTPError{
				Code:    tt.httpCode,
				Message: tt.name,
			}

			until, blacklisted := failBlacklistableError(err, serverStats)

			assert.False(t, blacklisted, "single error should not immediately blacklist")
			assert.False(t, until.IsZero(), "failure should return a valid backoff time")
			assert.True(t, until.After(time.Now()), "backoff time should be in the future")
		})
	}
}

// Test failBlacklistableError with HTTP 200 wrapped in error (defensive behavior)
func TestFailBlacklistableError_HTTP200WrappedError_IsIgnored(t *testing.T) {
	t.Parallel()

	testDB := test.NewInMemoryFederationDatabase()
	stats := statistics.NewStatistics(testDB, FailuresUntilBlacklist, FailuresUntilAssumedOffline, false)
	serverStats := stats.ForServer(spec.ServerName("test"))

	// Test defensive behavior: even if HTTP 200 is incorrectly wrapped as error,
	// failBlacklistableError should not treat it as a failure
	err := gomatrix.HTTPError{
		Code:    200,
		Message: "OK",
	}

	until, blacklisted := failBlacklistableError(err, serverStats)

	// HTTP 200 is not a failure condition
	assert.Equal(t, time.Time{}, until, "200 should not trigger backoff")
	assert.False(t, blacklisted, "200 should not blacklist")
}

// Test failBlacklistableError with HTTP 4xx errors (except 401)
func TestFailBlacklistableError_HTTP4xx_DoesNotFail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		httpCode int
	}{
		{"400 Bad Request", 400},
		{"403 Forbidden", 403},
		{"404 Not Found", 404},
		{"429 Too Many Requests", 429},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			testDB := test.NewInMemoryFederationDatabase()
			stats := statistics.NewStatistics(testDB, FailuresUntilBlacklist, FailuresUntilAssumedOffline, false)
			serverStats := stats.ForServer(spec.ServerName("test_" + tt.name))

			err := gomatrix.HTTPError{
				Code:    tt.httpCode,
				Message: tt.name,
			}

			until, blacklisted := failBlacklistableError(err, serverStats)

			// 4xx errors (except 401) should not trigger failure
			assert.Equal(t, time.Time{}, until, tt.name+" should not trigger backoff")
			assert.False(t, blacklisted, tt.name+" should not blacklist")
		})
	}
}

// Test failBlacklistableError with non-HTTP error
func TestFailBlacklistableError_NonHTTPError_ReturnsFailure(t *testing.T) {
	t.Parallel()

	testDB := test.NewInMemoryFederationDatabase()
	stats := statistics.NewStatistics(testDB, FailuresUntilBlacklist, FailuresUntilAssumedOffline, false)
	serverStats := stats.ForServer(spec.ServerName("test"))

	// Use a standard error (not an HTTP error)
	err := assert.AnError

	until, blacklisted := failBlacklistableError(err, serverStats)

	// Non-HTTP errors should trigger failure
	assert.False(t, blacklisted, "single non-HTTP error should not immediately blacklist")
	assert.False(t, until.IsZero(), "failure should return a valid backoff time")
	assert.True(t, until.After(time.Now()), "backoff time should be in the future")
}
