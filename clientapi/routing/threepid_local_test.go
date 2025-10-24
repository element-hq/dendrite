package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/element-hq/dendrite/internal/passwordreset"
	"github.com/element-hq/dendrite/setup/config"
	userapi "github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/stretchr/testify/assert"
)

func newThreePIDTestConfig() *config.ClientAPI {
	cfg := newTestClientAPIConfig()
	cfg.ThreePIDEmail.Enabled = true
	cfg.ThreePIDEmail.PublicBaseURL = "https://example.com"
	cfg.ThreePIDEmail.From = "noreply@example.com"
	cfg.ThreePIDEmail.Subject = "Verify"
	cfg.ThreePIDEmail.SMTP.Host = "smtp.example.com"
	return cfg
}

func TestRequestEmailVerificationToken_LocalFlow(t *testing.T) {
	cfg := newThreePIDTestConfig()
	origSender := emailVerificationSender
	var sentEmail, sentLink string
	emailVerificationSender = func(ctx context.Context, cfg *config.ClientAPI, email, link string) error {
		sentEmail = email
		sentLink = link
		return nil
	}
	t.Cleanup(func() {
		emailVerificationSender = origSender
	})

	stub := newStubClientUserAPI(&userapi.Device{UserID: "@alice:test"})

	body := map[string]any{
		"client_secret": "shh",
		"email":         "Alice@Example.com",
		"send_attempt":  1,
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/3pid/email/requestToken", bytes.NewReader(payload))

	resp := RequestEmailToken(req, stub, cfg, nil)
	assert.Equal(t, http.StatusOK, resp.Code)

	data, _ := json.Marshal(resp.JSON)
	var parsed map[string]string
	_ = json.Unmarshal(data, &parsed)
	sid := parsed["sid"]
	if assert.NotEmpty(t, sid) {
		session, ok := stub.emailVerificationSessions[sid]
		if assert.True(t, ok, "session stored") {
			assert.Equal(t, "alice@example.com", session.Email)
			assert.Empty(t, session.ValidatedAt)
		}
	}
	assert.Equal(t, "alice@example.com", sentEmail)
	assert.Contains(t, sentLink, "sid=")
	assert.Contains(t, sentLink, "token=")
}

func TestSubmitEmailToken_LocalFlow(t *testing.T) {
	cfg := newThreePIDTestConfig()
	stub := newStubClientUserAPI(nil)

	token := "verification-token"
	tokenHash, err := tokenHasher.HashToken(token)
	assert.NoError(t, err)

	session := &userapi.EmailVerificationSession{
		SessionID:        "sid123",
		ClientSecretHash: passwordreset.LookupKey("secret"),
		Email:            "alice@example.com",
		Medium:           "email",
		TokenLookup:      passwordreset.LookupKey(token),
		TokenHash:        tokenHash,
		SendAttempt:      1,
		ExpiresAt:        time.Now().Add(time.Hour),
	}
	stub.emailVerificationSessions[session.SessionID] = session

	body := map[string]string{
		"sid":   session.SessionID,
		"token": token,
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/3pid/email/submitToken", bytes.NewReader(payload))

	resp := SubmitEmailToken(req, stub, cfg)
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.NotNil(t, stub.emailVerificationSessions[session.SessionID].ValidatedAt)

	body["token"] = "wrong"
	payload, _ = json.Marshal(body)
	req = httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/3pid/email/submitToken", bytes.NewReader(payload))
	resp = SubmitEmailToken(req, stub, cfg)
	assert.Equal(t, http.StatusForbidden, resp.Code)
}

func TestRequestEmailVerificationToken_RetryAfterSendFailure(t *testing.T) {
	cfg := newThreePIDTestConfig()
	stub := newStubClientUserAPI(&userapi.Device{UserID: "@alice:test"})

	origSender := emailVerificationSender
	defer func() { emailVerificationSender = origSender }()

	callCount := 0
	emailVerificationSender = func(ctx context.Context, cfg *config.ClientAPI, email, link string) error {
		callCount++
		if callCount == 1 {
			return fmt.Errorf("smtp unavailable")
		}
		return nil
	}

	body := map[string]any{
		"client_secret": "retry-secret",
		"email":         "bob@example.com",
		"send_attempt":  3,
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/3pid/email/requestToken", bytes.NewReader(payload))

	resp := RequestEmailToken(req, stub, cfg, nil)
	assert.Equal(t, http.StatusInternalServerError, resp.Code)
	assert.Empty(t, stub.emailVerificationSessions)

	// Second attempt with same payload should succeed and send email again.
	req = httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/3pid/email/requestToken", bytes.NewReader(payload))
	resp = RequestEmailToken(req, stub, cfg, nil)
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, 2, callCount)
	if assert.Len(t, stub.emailVerificationSessions, 1) {
		for _, session := range stub.emailVerificationSessions {
			assert.Equal(t, "bob@example.com", session.Email)
			assert.Equal(t, 3, session.SendAttempt)
		}
	}
}

func TestCheckAndSave3PIDAssociation_LocalFlow(t *testing.T) {
	cfg := newThreePIDTestConfig()
	device := &userapi.Device{UserID: "@alice:test"}
	stub := newStubClientUserAPI(device)

	secret := "super-secret"
	session := &userapi.EmailVerificationSession{
		SessionID:        "sid456",
		ClientSecretHash: passwordreset.LookupKey(secret),
		Email:            "alice@example.com",
		Medium:           "email",
		TokenHash:        "hash",
		SendAttempt:      1,
		ExpiresAt:        time.Now().Add(time.Hour),
		ValidatedAt:      ptrTime(time.Now()),
	}
	stub.emailVerificationSessions[session.SessionID] = session

	reqBody := map[string]any{
		"three_pid_creds": map[string]string{
			"sid":           session.SessionID,
			"client_secret": secret,
		},
	}
	payload, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/account/3pid", bytes.NewReader(payload))

	resp := CheckAndSave3PIDAssociation(req, stub, device, cfg, nil)
	assert.Equal(t, http.StatusOK, resp.Code)
	if assert.NotEmpty(t, stub.savedThreePIDAssociations) {
		saved := stub.savedThreePIDAssociations[len(stub.savedThreePIDAssociations)-1]
		assert.Equal(t, "alice@example.com", saved.ThreePID)
		assert.Equal(t, "email", saved.Medium)
		assert.Equal(t, spec.ServerName("test"), saved.ServerName)
	}
	assert.NotNil(t, stub.emailVerificationSessions[session.SessionID].ConsumedAt)
}
