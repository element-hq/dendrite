package util

import "strings"

// NormalizeEmail trims surrounding whitespace and lowercases the address so it
// can be compared and stored consistently. Email local-parts are technically
// case-sensitive per RFC, but Dendrite treats email addresses case-insensitively
// which matches user expectations and the Matrix spec guidance.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
