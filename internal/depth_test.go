// Copyright 2025 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package internal

import (
	"testing"
)

func TestClampDepth(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected int64
	}{
		{
			name:     "normal depth",
			input:    100,
			expected: 100,
		},
		{
			name:     "zero depth",
			input:    0,
			expected: 0,
		},
		{
			name:     "negative depth",
			input:    -1,
			expected: 0,
		},
		{
			name:     "large negative depth",
			input:    -9007199254740991,
			expected: 0,
		},
		{
			name:     "max depth exactly",
			input:    MaxDepth,
			expected: MaxDepth,
		},
		{
			name:     "one over max depth (overflow case)",
			input:    MaxDepth + 1,
			expected: MaxDepth,
		},
		{
			name:     "large overflow",
			input:    MaxDepth + 1000000,
			expected: MaxDepth,
		},
		{
			name:     "near max depth (valid)",
			input:    MaxDepth - 1,
			expected: MaxDepth - 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClampDepth(tt.input)
			if result != tt.expected {
				t.Errorf("ClampDepth(%d) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMaxDepthConstant(t *testing.T) {
	// Verify MaxDepth is JavaScript's Number.MAX_SAFE_INTEGER (2^53 - 1)
	expectedMaxDepth := int64(9007199254740991)
	if MaxDepth != expectedMaxDepth {
		t.Errorf("MaxDepth = %d, want %d (2^53 - 1)", MaxDepth, expectedMaxDepth)
	}
}
