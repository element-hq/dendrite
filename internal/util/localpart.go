package util

import "strings"

// NormalizeLocalpart trims whitespace and lowercases a user localpart for consistent storage and lookup.
func NormalizeLocalpart(localpart string) string {
	return strings.ToLower(strings.TrimSpace(localpart))
}
