package passwordreset

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/scrypt"
)

// Scrypt parameters tuned for password reset token hashing; trading off latency vs brute-force resistance.
const (
	scryptN       = 32768
	scryptR       = 8
	scryptP       = 1
	scryptKeyLen  = 32
	scryptSaltLen = 16
)

// TokenHasher provides hashing and verification for password reset tokens.
type TokenHasher struct{}

// HashToken hashes the provided token using scrypt and returns a salt:hash string.
func (TokenHasher) HashToken(token string) (string, error) {
	salt := make([]byte, scryptSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	derived, err := scrypt.Key([]byte(token), salt, scryptN, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return "", fmt.Errorf("scrypt hash: %w", err)
	}

	return fmt.Sprintf("%s:%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived),
	), nil
}

// VerifyToken checks the provided token against the stored salt:hash.
func (TokenHasher) VerifyToken(token, storedHash string) (bool, error) {
	parts := strings.Split(storedHash, ":")
	if len(parts) != 2 {
		return false, fmt.Errorf("invalid hash format")
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil {
		return false, fmt.Errorf("decode salt: %w", err)
	}
	hashBytes, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		return false, fmt.Errorf("decode hash: %w", err)
	}

	derived, err := scrypt.Key([]byte(token), salt, scryptN, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return false, fmt.Errorf("scrypt hash: %w", err)
	}

	return subtle.ConstantTimeCompare(derived, hashBytes) == 1, nil
}

// LookupKey returns a deterministic, URL-safe digest for locating password reset tokens.
func LookupKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawStdEncoding.EncodeToString(sum[:])
}
