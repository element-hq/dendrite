package passwordreset

import (
	"strings"
	"testing"
)

func TestTokenHasherHashToken(t *testing.T) {
	hasher := TokenHasher{}
	token := "test-token-12345"

	hash, err := hasher.HashToken(token)
	if err != nil {
		t.Fatalf("HashToken failed: %v", err)
	}
	if hash == "" || hash == token {
		t.Fatal("HashToken returned invalid hash")
	}
	if len(strings.Split(hash, ":")) != 2 {
		t.Fatalf("invalid hash format: %s", hash)
	}
}

func TestTokenHasherVerifyToken(t *testing.T) {
	hasher := TokenHasher{}
	token := "test-token-12345"

	hash, err := hasher.HashToken(token)
	if err != nil {
		t.Fatalf("HashToken failed: %v", err)
	}

	valid, err := hasher.VerifyToken(token, hash)
	if err != nil {
		t.Fatalf("VerifyToken returned error: %v", err)
	}
	if !valid {
		t.Fatal("expected token to verify")
	}

	valid, err = hasher.VerifyToken("wrong-token", hash)
	if err != nil {
		t.Fatalf("VerifyToken returned error: %v", err)
	}
	if valid {
		t.Fatal("expected wrong token to fail verification")
	}
}

func TestTokenHasherUniqueHashes(t *testing.T) {
	hasher := TokenHasher{}
	token := "test-token-12345"

	hash1, err := hasher.HashToken(token)
	if err != nil {
		t.Fatalf("HashToken failed: %v", err)
	}
	hash2, err := hasher.HashToken(token)
	if err != nil {
		t.Fatalf("HashToken failed: %v", err)
	}

	if hash1 == hash2 {
		t.Fatal("expected unique hashes per invocation")
	}

	if ok, _ := hasher.VerifyToken(token, hash1); !ok {
		t.Fatal("hash1 should verify")
	}
	if ok, _ := hasher.VerifyToken(token, hash2); !ok {
		t.Fatal("hash2 should verify")
	}
}
