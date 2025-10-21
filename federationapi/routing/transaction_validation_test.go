// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package routing

import (
	"testing"

	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateTransactionLimits tests the ValidateTransactionLimits function
// which enforces Matrix spec limits: max 50 PDUs and 100 EDUs per transaction.
// https://matrix.org/docs/spec/server_server/latest#transactions
func TestValidateTransactionLimits_ValidCounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pduCount  int
		eduCount  int
		shouldErr bool
	}{
		{
			name:      "zero PDUs and EDUs",
			pduCount:  0,
			eduCount:  0,
			shouldErr: false,
		},
		{
			name:      "one PDU, one EDU",
			pduCount:  1,
			eduCount:  1,
			shouldErr: false,
		},
		{
			name:      "max PDUs (50)",
			pduCount:  50,
			eduCount:  0,
			shouldErr: false,
		},
		{
			name:      "max EDUs (100)",
			pduCount:  0,
			eduCount:  100,
			shouldErr: false,
		},
		{
			name:      "max PDUs and max EDUs",
			pduCount:  50,
			eduCount:  100,
			shouldErr: false,
		},
		{
			name:      "one over max PDUs (51)",
			pduCount:  51,
			eduCount:  0,
			shouldErr: true,
		},
		{
			name:      "one over max EDUs (101)",
			pduCount:  0,
			eduCount:  101,
			shouldErr: true,
		},
		{
			name:      "both over limits",
			pduCount:  51,
			eduCount:  101,
			shouldErr: true,
		},
		{
			name:      "far over PDU limit",
			pduCount:  100,
			eduCount:  0,
			shouldErr: true,
		},
		{
			name:      "far over EDU limit",
			pduCount:  0,
			eduCount:  200,
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Call actual production function
			err := ValidateTransactionLimits(tt.pduCount, tt.eduCount)

			if tt.shouldErr {
				require.Error(t, err, "Expected validation to fail for pduCount=%d, eduCount=%d", tt.pduCount, tt.eduCount)
				assert.Contains(t, err.Error(), "exceeds limit", "Error message should mention exceeding limits")
			} else {
				require.NoError(t, err, "Expected validation to pass for pduCount=%d, eduCount=%d", tt.pduCount, tt.eduCount)
			}
		})
	}
}

// TestValidateTransactionLimits_BoundaryValues specifically tests boundary conditions
func TestValidateTransactionLimits_BoundaryValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pduCount  int
		eduCount  int
		shouldErr bool
	}{
		{
			name:      "PDU boundary - exactly 50",
			pduCount:  50,
			eduCount:  0,
			shouldErr: false,
		},
		{
			name:      "PDU boundary + 1 - exactly 51",
			pduCount:  51,
			eduCount:  0,
			shouldErr: true,
		},
		{
			name:      "EDU boundary - exactly 100",
			pduCount:  0,
			eduCount:  100,
			shouldErr: false,
		},
		{
			name:      "EDU boundary + 1 - exactly 101",
			pduCount:  0,
			eduCount:  101,
			shouldErr: true,
		},
		{
			name:      "both at boundary",
			pduCount:  50,
			eduCount:  100,
			shouldErr: false,
		},
		{
			name:      "PDUs at boundary, EDUs over",
			pduCount:  50,
			eduCount:  101,
			shouldErr: true,
		},
		{
			name:      "PDUs over, EDUs at boundary",
			pduCount:  51,
			eduCount:  100,
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Call actual production function
			err := ValidateTransactionLimits(tt.pduCount, tt.eduCount)

			if tt.shouldErr {
				require.Error(t, err, "Expected counts above boundary to fail")
			} else {
				require.NoError(t, err, "Expected counts at/below boundary to pass")
			}
		})
	}
}

// TestValidateTransactionLimits_ErrorMessages verifies error messages contain useful information
func TestValidateTransactionLimits_ErrorMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		pduCount        int
		eduCount        int
		expectedInError string
	}{
		{
			name:            "PDU limit exceeded",
			pduCount:        51,
			eduCount:        0,
			expectedInError: "PDU",
		},
		{
			name:            "EDU limit exceeded",
			pduCount:        0,
			eduCount:        101,
			expectedInError: "EDU",
		},
		{
			name:            "both limits exceeded - should fail on PDU first",
			pduCount:        100,
			eduCount:        200,
			expectedInError: "PDU",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateTransactionLimits(tt.pduCount, tt.eduCount)

			require.Error(t, err, "Expected validation to fail")
			assert.Contains(t, err.Error(), tt.expectedInError,
				"Error message should mention which limit was exceeded")
		})
	}
}

// TestGenerateTransactionKey tests the transaction key generation for deduplication
func TestGenerateTransactionKey_Format(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		origin spec.ServerName
		txnID  gomatrixserverlib.TransactionID
		want   string
	}{
		{
			name:   "basic transaction",
			origin: "server.com",
			txnID:  "txn123",
			want:   "server.com\000txn123",
		},
		{
			name:   "different server same txn",
			origin: "other.com",
			txnID:  "txn123",
			want:   "other.com\000txn123",
		},
		{
			name:   "same server different txn",
			origin: "server.com",
			txnID:  "txn456",
			want:   "server.com\000txn456",
		},
		{
			name:   "empty txn ID",
			origin: "server.com",
			txnID:  "",
			want:   "server.com\000",
		},
		{
			name:   "long transaction ID",
			origin: "example.org",
			txnID:  "very-long-transaction-id-12345678901234567890",
			want:   "example.org\000very-long-transaction-id-12345678901234567890",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Call actual production function
			key := GenerateTransactionKey(tt.origin, tt.txnID)

			assert.Equal(t, tt.want, key, "Transaction key should match expected format")
		})
	}
}

// TestGenerateTransactionKey_Uniqueness verifies that different origin/txnID combinations
// produce unique keys (important for deduplication)
func TestGenerateTransactionKey_Uniqueness(t *testing.T) {
	t.Parallel()

	indices := make(map[string]bool)

	testCases := []struct {
		origin spec.ServerName
		txnID  gomatrixserverlib.TransactionID
	}{
		{"server1.com", "txn1"},
		{"server1.com", "txn2"},
		{"server2.com", "txn1"},
		{"server2.com", "txn2"},
	}

	for _, tc := range testCases {
		// Call actual production function
		key := GenerateTransactionKey(tc.origin, tc.txnID)

		_, exists := indices[key]
		assert.False(t, exists, "Key should be unique for origin=%s, txnID=%s", tc.origin, tc.txnID)

		indices[key] = true
	}

	// Verify we have the expected number of unique keys
	assert.Equal(t, 4, len(indices), "Should have 4 unique transaction keys")
}

// TestGenerateTransactionKey_CollisionResistance ensures the null byte separator
// prevents collisions between similar origin/txnID combinations
func TestGenerateTransactionKey_CollisionResistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		origin1 spec.ServerName
		txnID1  gomatrixserverlib.TransactionID
		origin2 spec.ServerName
		txnID2  gomatrixserverlib.TransactionID
	}{
		{
			name:    "similar concatenation",
			origin1: "server",
			txnID1:  ".comtxn1",
			origin2: "server.com",
			txnID2:  "txn1",
		},
		{
			name:    "empty origin vs prefix",
			origin1: "",
			txnID1:  "txn1",
			origin2: "txn",
			txnID2:  "1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Call actual production function
			key1 := GenerateTransactionKey(tt.origin1, tt.txnID1)
			key2 := GenerateTransactionKey(tt.origin2, tt.txnID2)

			// The null byte separator should ensure these are different
			assert.NotEqual(t, key1, key2, "Keys should be different despite similar components")
		})
	}
}

// TODO: Add integration test for full /send transaction endpoint
// This unit test covers only validation and key generation; full handler testing requires:
// - HTTP server setup with federation request signing
// - Signature verification
// - Roomserver API mocking
// - Database transaction handling
// - Complex state resolution scenarios
