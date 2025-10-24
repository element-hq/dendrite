package util

import (
	"strings"

	"github.com/matrix-org/gomatrixserverlib/spec"
)

// NormalizeServerName trims whitespace and lowercases a server name so that
// comparisons and lookups remain case-insensitive. Domain names are defined as
// case-insensitive by RFC 1035, so this canonical form is safe to store.
func NormalizeServerName(name spec.ServerName) spec.ServerName {
	return spec.ServerName(strings.ToLower(strings.TrimSpace(string(name))))
}
