package util

import "strings"

// NormalizeRoomAlias trims surrounding whitespace and lowercases the alias so it can be
// compared and stored consistently. Room aliases are treated case-insensitively by Dendrite.
func NormalizeRoomAlias(alias string) string {
	return strings.ToLower(strings.TrimSpace(alias))
}
