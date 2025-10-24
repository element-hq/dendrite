package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/element-hq/dendrite/internal/passwordreset"
	iutil "github.com/element-hq/dendrite/internal/util"
	"github.com/element-hq/dendrite/setup/config"
	userapi "github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
	"github.com/stretchr/testify/assert"
)

func TestRequestPasswordResetTokenSuccess(t *testing.T) {
	resetPasswordResetLimiters()

	userAPI := newStubClientUserAPI(nil)
	userAPI.threePIDLocalpart = "alice"
	userAPI.threePIDServerName = spec.ServerName("example.com")
	userAPI.threePIDStoredEmail = "alice@example.com"

	fixedToken := "token123"
	fixedSID := "session456"
	origTokenGen := tokenGenerator
	origSessionGen := sessionIDGenerator
	origEmailSender := passwordResetEmailSender
	tokenGenerator = func() (string, error) { return fixedToken, nil }
	sessionIDGenerator = func() (string, error) { return fixedSID, nil }
	var sentEmail, sentLink string
	passwordResetEmailSender = func(_ context.Context, cfg *config.ClientAPI, email, resetLink string) error {
		sentEmail = email
		sentLink = resetLink
		return nil
	}
	t.Cleanup(func() {
		tokenGenerator = origTokenGen
		sessionIDGenerator = origSessionGen
		passwordResetEmailSender = origEmailSender
	})

	cfg := &config.ClientAPI{}
	cfg.PasswordReset.Enabled = true
	cfg.PasswordReset.PublicBaseURL = "https://matrix.example"
	cfg.PasswordReset.From = "noreply@matrix.example"
	cfg.PasswordReset.TokenLifetime = time.Hour

	body := map[string]any{
		"client_secret": "secret123",
		"email":         "alice@example.com",
		"send_attempt":  1,
	}
	payload, err := json.Marshal(body)
	assert.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password/email/requestToken", bytes.NewReader(payload))

	resp := RequestPasswordResetToken(req, userAPI, cfg)
	assert.Equal(t, http.StatusOK, resp.Code)
	switch payload := resp.JSON.(type) {
	case map[string]string:
		assert.Equal(t, fixedSID, payload["sid"])
	case map[string]any:
		assert.Equal(t, fixedSID, payload["sid"])
	default:
		assert.Failf(t, "unexpected response JSON type", "%T", resp.JSON)
	}

	if assert.NotNil(t, userAPI.storedPasswordResetToken) {
		assert.Equal(t, "@alice:example.com", userAPI.storedPasswordResetToken.UserID)
		assert.Equal(t, "alice@example.com", userAPI.storedPasswordResetToken.Email)
		assert.WithinDuration(t, time.Now().Add(cfg.PasswordReset.TokenLifetime), userAPI.storedPasswordResetToken.ExpiresAt, time.Minute)
		assert.Contains(t, userAPI.storedPasswordResetToken.TokenHash, ":", "expected salt:hash format")
	}
	assert.Equal(t, passwordreset.LookupKey(fixedToken), userAPI.passwordResetTokenLookup)
	assert.Equal(t, "alice@example.com", sentEmail)
	assert.Contains(t, sentLink, "/account/password/reset?token=")
	assert.Contains(t, sentLink, fixedToken)
}

func TestRequestPasswordResetTokenNormalizesEmail(t *testing.T) {
	resetPasswordResetLimiters()

	userAPI := newStubClientUserAPI(nil)
	userAPI.threePIDLocalpart = "alice"
	userAPI.threePIDServerName = spec.ServerName("example.com")

	cfg := &config.ClientAPI{}
	cfg.PasswordReset.Enabled = true
	cfg.PasswordReset.PublicBaseURL = "https://matrix.example"
	cfg.PasswordReset.From = "noreply@example.com"
	cfg.PasswordReset.TokenLifetime = time.Hour

	body := map[string]any{
		"client_secret": "secret123",
		"email":         "Alice@Example.com",
		"send_attempt":  1,
	}
	payload, err := json.Marshal(body)
	assert.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password/email/requestToken", bytes.NewReader(payload))

	origEmailSender := passwordResetEmailSender
	var sentEmail string
	passwordResetEmailSender = func(_ context.Context, _ *config.ClientAPI, email, _ string) error {
		sentEmail = email
		return nil
	}
	defer func() { passwordResetEmailSender = origEmailSender }()

	resp := RequestPasswordResetToken(req, userAPI, cfg)
	assert.Equal(t, http.StatusOK, resp.Code)
	if assert.NotNil(t, userAPI.storedPasswordResetToken) {
		assert.Equal(t, "alice@example.com", userAPI.storedPasswordResetToken.Email)
		assert.Equal(t, "@alice:example.com", userAPI.storedPasswordResetToken.UserID)
	}
	assert.Equal(t, "alice@example.com", sentEmail)
}

func TestRequestPasswordResetTokenIdempotentSendAttempt(t *testing.T) {
	resetPasswordResetLimiters()

	userAPI := newStubClientUserAPI(nil)
	userAPI.threePIDLocalpart = "alice"
	userAPI.threePIDServerName = spec.ServerName("example.com")

	cfg := &config.ClientAPI{}
	cfg.PasswordReset.Enabled = true
	cfg.PasswordReset.PublicBaseURL = "https://matrix.example"
	cfg.PasswordReset.From = "noreply@example.com"
	cfg.PasswordReset.TokenLifetime = time.Hour

	tokenValues := []string{"token-one", "token-two"}
	sessionValues := []string{"sid-one", "sid-two"}

	origTokenGen := tokenGenerator
	tokenGenerator = func() (string, error) {
		if len(tokenValues) == 0 {
			return "", fmt.Errorf("no tokens left")
		}
		val := tokenValues[0]
		tokenValues = tokenValues[1:]
		return val, nil
	}
	origSessionGen := sessionIDGenerator
	sessionIDGenerator = func() (string, error) {
		if len(sessionValues) == 0 {
			return "", fmt.Errorf("no sessions left")
		}
		val := sessionValues[0]
		sessionValues = sessionValues[1:]
		return val, nil
	}
	emailCount := 0
	origEmailSender := passwordResetEmailSender
	passwordResetEmailSender = func(_ context.Context, _ *config.ClientAPI, _, _ string) error {
		emailCount++
		return nil
	}
	t.Cleanup(func() {
		tokenGenerator = origTokenGen
		sessionIDGenerator = origSessionGen
		passwordResetEmailSender = origEmailSender
	})

	makeRequest := func(sendAttempt int) util.JSONResponse {
		body := map[string]any{
			"client_secret": "shared-secret",
			"email":         "alice@example.com",
			"send_attempt":  sendAttempt,
		}
		payload, err := json.Marshal(body)
		assert.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password/email/requestToken", bytes.NewReader(payload))
		return RequestPasswordResetToken(req, userAPI, cfg)
	}

	getSID := func(resp util.JSONResponse) string {
		switch payload := resp.JSON.(type) {
		case map[string]string:
			return payload["sid"]
		case map[string]any:
			if sid, ok := payload["sid"].(string); ok {
				return sid
			}
		}
		t.Fatalf("unexpected response JSON type %T", resp.JSON)
		return ""
	}

	firstResp := makeRequest(1)
	assert.Equal(t, http.StatusOK, firstResp.Code)
	firstSID := getSID(firstResp)
	assert.Equal(t, "sid-one", firstSID)
	assert.Equal(t, 1, emailCount)

	secondResp := makeRequest(1)
	assert.Equal(t, http.StatusOK, secondResp.Code)
	secondSID := getSID(secondResp)
	assert.Equal(t, firstSID, secondSID)
	assert.Equal(t, 1, emailCount, "duplicate send_attempt should not send another email")
	assert.Equal(t, 1, len(sessionValues), "session generator should not be invoked for repeated attempt")
	assert.Equal(t, passwordreset.LookupKey("token-one"), userAPI.passwordResetTokenLookup)

	thirdResp := makeRequest(2)
	assert.Equal(t, http.StatusOK, thirdResp.Code)
	thirdSID := getSID(thirdResp)
	assert.Equal(t, "sid-two", thirdSID)
	assert.Equal(t, 2, emailCount)
}

func TestRequestPasswordResetInvalidEmail(t *testing.T) {
	resetPasswordResetLimiters()

	userAPI := newStubClientUserAPI(nil)
	userAPI.threePIDLocalpart = "alice"
	userAPI.threePIDServerName = spec.ServerName("example.com")
	cfg := &config.ClientAPI{}
	cfg.PasswordReset.Enabled = true
	cfg.PasswordReset.PublicBaseURL = "https://matrix.example"
	cfg.PasswordReset.From = "noreply@matrix.example"
	cfg.PasswordReset.TokenLifetime = time.Hour

	body := map[string]any{
		"client_secret": "secret123",
		"email":         "not-an-email",
		"send_attempt":  1,
	}
	payload, err := json.Marshal(body)
	assert.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password/email/requestToken", bytes.NewReader(payload))

	resp := RequestPasswordResetToken(req, userAPI, cfg)
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestRequestPasswordResetUnknownEmail(t *testing.T) {
	resetPasswordResetLimiters()

	userAPI := newStubClientUserAPI(nil)
	userAPI.threePIDLocalpart = "alice"
	userAPI.threePIDServerName = spec.ServerName("example.com")
	userAPI.threePIDLocalpart = ""

	cfg := &config.ClientAPI{}
	cfg.PasswordReset.Enabled = true
	cfg.PasswordReset.PublicBaseURL = "https://matrix.example"
	cfg.PasswordReset.From = "noreply@matrix.example"
	cfg.PasswordReset.TokenLifetime = time.Hour

	body := map[string]any{
		"client_secret": "secret123",
		"email":         "unknown@example.com",
		"send_attempt":  1,
	}
	payload, err := json.Marshal(body)
	assert.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password/email/requestToken", bytes.NewReader(payload))

	origEmailSender := passwordResetEmailSender
	passwordResetEmailSender = func(context.Context, *config.ClientAPI, string, string) error { return nil }
	defer func() { passwordResetEmailSender = origEmailSender }()

	resp := RequestPasswordResetToken(req, userAPI, cfg)
	assert.Equal(t, http.StatusOK, resp.Code)
	if assert.NotNil(t, userAPI.storedPasswordResetToken) {
		assert.True(t, strings.HasPrefix(userAPI.storedPasswordResetToken.UserID, "@_reset_"))
	}
}

func TestPasswordResetWithTokenSuccess(t *testing.T) {
	resetPasswordResetLimiters()

	userAPI := newStubClientUserAPI(nil)
	userAPI.threePIDLocalpart = "alice"
	userAPI.threePIDServerName = spec.ServerName("example.com")
	userAPI.threePIDStoredEmail = "alice@example.com"
	hasher := passwordreset.TokenHasher{}
	hash, err := hasher.HashToken("token123")
	assert.NoError(t, err)
	userAPI.passwordResetTokenLookup = passwordreset.LookupKey("token123")
	userAPI.storedPasswordResetToken = &userapi.PasswordResetToken{
		TokenHash: hash,
		UserID:    "@alice:example.com",
		Email:     "alice@example.com",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	cfg := &config.ClientAPI{}
	cfg.PasswordReset.Enabled = true
	cfg.PasswordReset.PublicBaseURL = "https://matrix.example"
	cfg.PasswordReset.From = "noreply@matrix.example"
	cfg.PasswordReset.TokenLifetime = time.Hour

	body := map[string]any{
		"new_password": "supersecurepassword",
	}
	payload, err := json.Marshal(body)
	assert.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password?token=token123", bytes.NewReader(payload))

	resp := CompletePasswordReset(req, userAPI, cfg)
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "", userAPI.passwordResetTokenLookup)
	assert.Equal(t, 1, userAPI.passwordUpdateCalls)
	if assert.Len(t, userAPI.deviceDeletionRequests, 1) {
		assert.Equal(t, "@alice:example.com", userAPI.deviceDeletionRequests[0].UserID)
	}
	if assert.Len(t, userAPI.pusherDeletionRequests, 1) {
		assert.Equal(t, "alice", userAPI.pusherDeletionRequests[0].Localpart)
		assert.Equal(t, spec.ServerName("example.com"), userAPI.pusherDeletionRequests[0].ServerName)
		assert.Equal(t, int64(-1), userAPI.pusherDeletionRequests[0].SessionID)
	}
}

func TestPasswordResetWithTokenUppercaseEmail(t *testing.T) {
	resetPasswordResetLimiters()

	userAPI := newStubClientUserAPI(nil)
	userAPI.threePIDLocalpart = "alice"
	userAPI.threePIDServerName = spec.ServerName("example.com")
	userAPI.threePIDStoredEmail = "alice@example.com"
	hasher := passwordreset.TokenHasher{}
	hash, err := hasher.HashToken("token123")
	assert.NoError(t, err)
	userAPI.passwordResetTokenLookup = passwordreset.LookupKey("token123")
	userAPI.storedPasswordResetToken = &userapi.PasswordResetToken{
		TokenHash: hash,
		UserID:    "@alice:example.com",
		Email:     "Alice@Example.com",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	cfg := &config.ClientAPI{}
	cfg.PasswordReset.Enabled = true

	body := map[string]any{
		"new_password": "supersecurepassword",
	}
	payload, err := json.Marshal(body)
	assert.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password?token=token123", bytes.NewReader(payload))

	resp := CompletePasswordReset(req, userAPI, cfg)
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "", userAPI.passwordResetTokenLookup)
	assert.Equal(t, 1, userAPI.passwordUpdateCalls)
}

func TestPasswordResetWithTokenSkipLogout(t *testing.T) {
	resetPasswordResetLimiters()

	userAPI := newStubClientUserAPI(nil)
	userAPI.threePIDLocalpart = "alice"
	userAPI.threePIDServerName = spec.ServerName("example.com")
	hasher := passwordreset.TokenHasher{}
	hash, err := hasher.HashToken("token123")
	assert.NoError(t, err)
	userAPI.passwordResetTokenLookup = passwordreset.LookupKey("token123")
	userAPI.storedPasswordResetToken = &userapi.PasswordResetToken{
		TokenHash: hash,
		UserID:    "@alice:example.com",
		Email:     "alice@example.com",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	cfg := &config.ClientAPI{}
	cfg.PasswordReset.Enabled = true
	cfg.PasswordReset.PublicBaseURL = "https://matrix.example"
	cfg.PasswordReset.From = "noreply@matrix.example"
	cfg.PasswordReset.TokenLifetime = time.Hour

	logout := false
	body := map[string]any{
		"new_password":   "supersecurepassword",
		"logout_devices": logout,
	}
	payload, err := json.Marshal(body)
	assert.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password?token=token123", bytes.NewReader(payload))

	resp := CompletePasswordReset(req, userAPI, cfg)
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "", userAPI.passwordResetTokenLookup)
	assert.Equal(t, 1, userAPI.passwordUpdateCalls)
	assert.Empty(t, userAPI.deviceDeletionRequests)
	assert.Empty(t, userAPI.pusherDeletionRequests)
}

func TestPasswordResetWithTokenInvalidToken(t *testing.T) {
	resetPasswordResetLimiters()

	userAPI := newStubClientUserAPI(nil)
	cfg := &config.ClientAPI{}
	cfg.PasswordReset.Enabled = true
	cfg.PasswordReset.PublicBaseURL = "https://matrix.example"
	cfg.PasswordReset.From = "noreply@matrix.example"
	cfg.PasswordReset.TokenLifetime = time.Hour

	body := map[string]any{
		"new_password": "supersecurepassword",
	}
	payload, err := json.Marshal(body)
	assert.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password?token=doesnotexist", bytes.NewReader(payload))

	resp := CompletePasswordReset(req, userAPI, cfg)
	assert.Equal(t, http.StatusForbidden, resp.Code)
}

func TestPasswordResetWithTokenWeakPassword(t *testing.T) {
	resetPasswordResetLimiters()

	userAPI := newStubClientUserAPI(nil)
	hasher := passwordreset.TokenHasher{}
	hash, err := hasher.HashToken("token123")
	assert.NoError(t, err)
	userAPI.threePIDStoredEmail = "alice@example.com"
	userAPI.passwordResetTokenLookup = passwordreset.LookupKey("token123")
	userAPI.storedPasswordResetToken = &userapi.PasswordResetToken{
		TokenHash: hash,
		UserID:    "@alice:example.com",
		Email:     "alice@example.com",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	cfg := &config.ClientAPI{}
	cfg.PasswordReset.Enabled = true
	cfg.PasswordReset.PublicBaseURL = "https://matrix.example"
	cfg.PasswordReset.From = "noreply@matrix.example"
	cfg.PasswordReset.TokenLifetime = time.Hour

	body := map[string]any{
		"new_password": "short",
	}
	payload, err := json.Marshal(body)
	assert.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password?token=token123", bytes.NewReader(payload))

	resp := CompletePasswordReset(req, userAPI, cfg)
	assert.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Equal(t, passwordreset.LookupKey("token123"), userAPI.passwordResetTokenLookup)
}

func TestRequestPasswordResetEmailRateLimit(t *testing.T) {
	resetPasswordResetLimiters()

	userAPI := newStubClientUserAPI(nil)
	userAPI.threePIDLocalpart = "alice"
	userAPI.threePIDServerName = spec.ServerName("example.com")

	cfg := &config.ClientAPI{}
	cfg.PasswordReset.Enabled = true
	cfg.PasswordReset.PublicBaseURL = "https://matrix.example"
	cfg.PasswordReset.From = "noreply@matrix.example"
	cfg.PasswordReset.TokenLifetime = time.Hour

	body := map[string]any{
		"client_secret": "secret123",
		"email":         "alice@example.com",
	}
	payload, err := json.Marshal(body)
	assert.NoError(t, err)
	probeReq := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password/email/requestToken", bytes.NewReader(payload))
	ip, _, _ := net.SplitHostPort(probeReq.RemoteAddr)
	userAPI.rateLimitBehavior["ip:"+passwordreset.LookupKey(ip)] = []bool{true, true, true, true, true}
	userAPI.rateLimitBehavior["email:"+passwordreset.LookupKey(iutil.NormalizeEmail("alice@example.com"))] = []bool{true, true, true, false}

	origEmailSender := passwordResetEmailSender
	passwordResetEmailSender = func(context.Context, *config.ClientAPI, string, string) error {
		return nil
	}
	t.Cleanup(func() {
		passwordResetEmailSender = origEmailSender
	})

	for i := 0; i < passwordResetEmailLimit; i++ {
		body["send_attempt"] = i + 1
		payload, err = json.Marshal(body)
		assert.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password/email/requestToken", bytes.NewReader(payload))
		resp := RequestPasswordResetToken(req, userAPI, cfg)
		assert.Equal(t, http.StatusOK, resp.Code)
	}

	body["send_attempt"] = passwordResetEmailLimit + 1
	payload, err = json.Marshal(body)
	assert.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password/email/requestToken", bytes.NewReader(payload))
	resp := RequestPasswordResetToken(req, userAPI, cfg)
	assert.Equal(t, http.StatusTooManyRequests, resp.Code)
}

func TestRequestPasswordResetTokenEmailFailureRetry(t *testing.T) {
	resetPasswordResetLimiters()

	userAPI := newStubClientUserAPI(nil)
	userAPI.threePIDLocalpart = "alice"
	userAPI.threePIDServerName = spec.ServerName("example.com")
	userAPI.threePIDStoredEmail = "alice@example.com"

	tokens := []string{"token-one", "token-two"}
	sessions := []string{"sid-one", "sid-two"}
	callCount := 0

	origTokenGen := tokenGenerator
	origSessionGen := sessionIDGenerator
	origEmailSender := passwordResetEmailSender

	tokenGenerator = func() (string, error) {
		if len(tokens) == 0 {
			return "fallback-token", nil
		}
		tok := tokens[0]
		tokens = tokens[1:]
		return tok, nil
	}
	sessionIDGenerator = func() (string, error) {
		if len(sessions) == 0 {
			return "fallback-sid", nil
		}
		sid := sessions[0]
		sessions = sessions[1:]
		return sid, nil
	}
	passwordResetEmailSender = func(context.Context, *config.ClientAPI, string, string) error {
		callCount++
		if callCount == 1 {
			return fmt.Errorf("smtp timeout")
		}
		return nil
	}
	t.Cleanup(func() {
		tokenGenerator = origTokenGen
		sessionIDGenerator = origSessionGen
		passwordResetEmailSender = origEmailSender
	})

	cfg := &config.ClientAPI{}
	cfg.PasswordReset.Enabled = true
	cfg.PasswordReset.PublicBaseURL = "https://matrix.example"
	cfg.PasswordReset.From = "noreply@matrix.example"
	cfg.PasswordReset.TokenLifetime = time.Hour

	body := map[string]any{
		"client_secret": "secret123",
		"email":         "alice@example.com",
		"send_attempt":  1,
	}
	payload, err := json.Marshal(body)
	assert.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password/email/requestToken", bytes.NewReader(payload))
	resp := RequestPasswordResetToken(req, userAPI, cfg)
	assert.Equal(t, http.StatusInternalServerError, resp.Code)
	assert.Nil(t, userAPI.storedPasswordResetToken)
	assert.Empty(t, userAPI.passwordResetTokenLookup)

	payload, err = json.Marshal(body)
	assert.NoError(t, err)
	req = httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password/email/requestToken", bytes.NewReader(payload))
	resp = RequestPasswordResetToken(req, userAPI, cfg)
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, 2, callCount)
	if assert.NotNil(t, userAPI.storedPasswordResetToken) {
		assert.Equal(t, passwordreset.LookupKey("token-two"), userAPI.passwordResetTokenLookup)
	}
}

func TestPasswordResetTokenHashing(t *testing.T) {
	resetPasswordResetLimiters()

	userAPI := newStubClientUserAPI(nil)
	userAPI.threePIDLocalpart = "alice"
	userAPI.threePIDServerName = spec.ServerName("example.com")

	fixedToken := "token-abc"
	origTokenGen := tokenGenerator
	origSessionGen := sessionIDGenerator
	origEmailSender := passwordResetEmailSender
	tokenGenerator = func() (string, error) { return fixedToken, nil }
	sessionIDGenerator = func() (string, error) { return "sid", nil }
	passwordResetEmailSender = func(context.Context, *config.ClientAPI, string, string) error { return nil }
	t.Cleanup(func() {
		tokenGenerator = origTokenGen
		sessionIDGenerator = origSessionGen
		passwordResetEmailSender = origEmailSender
	})

	cfg := &config.ClientAPI{}
	cfg.PasswordReset.Enabled = true
	cfg.PasswordReset.PublicBaseURL = "https://matrix.example"
	cfg.PasswordReset.From = "noreply@matrix.example"
	cfg.PasswordReset.TokenLifetime = time.Hour

	body := map[string]any{
		"client_secret": "secret",
		"email":         "alice@example.com",
		"send_attempt":  1,
	}
	payload, err := json.Marshal(body)
	assert.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password/email/requestToken", bytes.NewReader(payload))

	resp := RequestPasswordResetToken(req, userAPI, cfg)
	assert.Equal(t, http.StatusOK, resp.Code)
	if assert.NotNil(t, userAPI.storedPasswordResetToken) {
		hash := userAPI.storedPasswordResetToken.TokenHash
		assert.NotEmpty(t, hash)
		assert.NotEqual(t, fixedToken, hash)
		valid, err := tokenHasher.VerifyToken(fixedToken, hash)
		assert.NoError(t, err)
		assert.True(t, valid)
		valid, err = tokenHasher.VerifyToken("wrong-token", hash)
		assert.NoError(t, err)
		assert.False(t, valid)
	}
}

func TestPasswordResetTokenVerificationFlow(t *testing.T) {
	resetPasswordResetLimiters()

	token := "token-verify"
	hasher := passwordreset.TokenHasher{}
	tokenHash, err := hasher.HashToken(token)
	assert.NoError(t, err)
	lookup := passwordreset.LookupKey(token)

	userAPI := newStubClientUserAPI(nil)
	userAPI.threePIDLocalpart = "alice"
	userAPI.threePIDServerName = spec.ServerName("example.com")
	userAPI.passwordResetTokenLookup = lookup
	userAPI.storedPasswordResetToken = &userapi.PasswordResetToken{
		TokenHash: tokenHash,
		UserID:    "@alice:example.com",
		Email:     "alice@example.com",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	cfg := &config.ClientAPI{}
	cfg.PasswordReset.Enabled = true

	body := map[string]any{"new_password": "securepass123"}
	payload, err := json.Marshal(body)
	assert.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/_matrix/client/v3/account/password?token=%s", token), bytes.NewReader(payload))
	resp := CompletePasswordReset(req, userAPI, cfg)
	assert.Equal(t, http.StatusOK, resp.Code)

	userAPI.passwordResetTokenLookup = lookup
	userAPI.storedPasswordResetToken = &userapi.PasswordResetToken{
		TokenHash: tokenHash,
		UserID:    "@alice:example.com",
		Email:     "alice@example.com",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	req = httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/password?token=wrong-token", bytes.NewReader(payload))
	resp = CompletePasswordReset(req, userAPI, cfg)
	assert.Equal(t, http.StatusForbidden, resp.Code)
}

func TestPasswordResetFallbackPageRenders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/account/password/reset?token=abc123", nil)
	rec := httptest.NewRecorder()

	servePasswordResetFallback(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Reset your password")
	assert.Contains(t, body, "account/password?token=abc123")
}

func TestPasswordResetFallbackMissingToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/account/password/reset", nil)
	rec := httptest.NewRecorder()

	servePasswordResetFallback(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestForget3PIDEmailCaseNormalization(t *testing.T) {
	userAPI := newStubClientUserAPI(nil)

	// Simulate a registered 3PID with lowercase email
	body := map[string]any{
		"medium":  "email",
		"address": "Alice@Example.com", // Mixed case input
	}
	payload, err := json.Marshal(body)
	assert.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/3pid/delete", bytes.NewReader(payload))

	resp := Forget3PID(req, userAPI)
	assert.Equal(t, http.StatusOK, resp.Code)

	// Verify that the email was normalized before being passed to the API
	if assert.Len(t, userAPI.forget3PIDRequests, 1) {
		assert.Equal(t, "alice@example.com", userAPI.forget3PIDRequests[0].ThreePID,
			"Email should be normalized to lowercase")
		assert.Equal(t, "email", userAPI.forget3PIDRequests[0].Medium)
	}
}

func TestForget3PIDEmailWithWhitespace(t *testing.T) {
	userAPI := newStubClientUserAPI(nil)

	// Email with leading/trailing whitespace
	body := map[string]any{
		"medium":  "email",
		"address": "  Bob@Example.com  ", // Whitespace + mixed case
	}
	payload, err := json.Marshal(body)
	assert.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/3pid/delete", bytes.NewReader(payload))

	resp := Forget3PID(req, userAPI)
	assert.Equal(t, http.StatusOK, resp.Code)

	// Verify that the email was trimmed and normalized
	if assert.Len(t, userAPI.forget3PIDRequests, 1) {
		assert.Equal(t, "bob@example.com", userAPI.forget3PIDRequests[0].ThreePID,
			"Email should be trimmed and normalized to lowercase")
	}
}

func resetPasswordResetLimiters() {}
