package routing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"math"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/element-hq/dendrite/internal"
	"github.com/element-hq/dendrite/internal/passwordreset"
	iutil "github.com/element-hq/dendrite/internal/util"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
)

const (
	passwordResetTokenLength   = 32 // bytes
	passwordResetSessionLength = 16
	passwordResetEmailLimit    = 3
	passwordResetIPLimit       = 10
	passwordResetWindow        = time.Hour
	passwordResetMinDuration   = 200 * time.Millisecond
)

var (
	passwordResetEmailSender = sendPasswordResetEmail
	tokenHasher              = passwordreset.TokenHasher{}
	tokenGenerator           = generateSecureToken
	sessionIDGenerator       = generateSessionID
)

var passwordResetFallbackTemplate = template.Must(template.New("password_reset_fallback").Parse(`
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Reset your Matrix password</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
body { font-family: Arial, sans-serif; background: #f5f5f5; margin: 0; padding: 0; }
.container { max-width: 480px; margin: 48px auto; background: #ffffff; padding: 32px; border-radius: 12px; box-shadow: 0 6px 24px rgba(0, 0, 0, 0.08); }
h1 { font-size: 1.6rem; margin-top: 0; }
label { display: block; margin-top: 16px; font-weight: 600; }
input[type=password] { width: 100%; padding: 10px; margin-top: 8px; border-radius: 6px; border: 1px solid #c7c7c7; font-size: 1rem; }
button { margin-top: 24px; width: 100%; padding: 12px; font-size: 1rem; font-weight: 600; color: #ffffff; background: #0dbd8b; border: none; border-radius: 6px; cursor: pointer; }
button:hover { background: #0aa57a; }
.message { margin-top: 16px; font-size: 0.95rem; }
.message.error { color: #c62828; }
.message.success { color: #2e7d32; }
.message.progress { color: #1565c0; }
.checkbox { margin-top: 16px; display: flex; align-items: center; }
.checkbox input { margin-right: 8px; }
</style>
</head>
<body>
<div class="container">
<h1>Reset your password</h1>
<p>Enter a new password for your Matrix account.</p>
<noscript><div class="message error">JavaScript is required to reset your password from this page. Please enable JavaScript or use a Matrix client.</div></noscript>
<form id="reset-form">
<label for="new-password">New password</label>
<input id="new-password" name="new_password" type="password" autocomplete="new-password" required minlength="8">
<label for="confirm-password">Confirm new password</label>
<input id="confirm-password" name="confirm_password" type="password" autocomplete="new-password" required minlength="8">
<label class="checkbox"><input type="checkbox" name="logout_devices" id="logout-devices" checked>Log out of all other sessions</label>
<button type="submit">Update password</button>
</form>
<div id="message" class="message" role="status" aria-live="polite"></div>
</div>
<script>
(function() {
  const form = document.getElementById('reset-form');
  if (!form) {
    return;
  }
  const message = document.getElementById('message');
  const endpoint = {{ printf "%q" .APIEndpoint }};
  form.addEventListener('submit', async function(event) {
    event.preventDefault();
    const password = form.new_password.value.trim();
    const confirm = form.confirm_password.value.trim();
    if (!password) {
      message.textContent = 'Please enter a new password.';
      message.className = 'message error';
      return;
    }
    if (password !== confirm) {
      message.textContent = 'Passwords do not match.';
      message.className = 'message error';
      return;
    }
    message.textContent = 'Updating your password…';
    message.className = 'message progress';
    try {
      const response = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          new_password: password,
          logout_devices: document.getElementById('logout-devices').checked
        })
      });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok) {
        const errorText = payload.error || 'Unable to reset password. Please try again later.';
        throw new Error(errorText);
      }
      message.textContent = 'Your password has been updated. You can now return to your Matrix client.';
      message.className = 'message success';
      form.reset();
      const logoutCheckbox = document.getElementById('logout-devices');
      if (logoutCheckbox) {
        logoutCheckbox.checked = true;
      }
    } catch (err) {
      message.textContent = err.message || 'Unable to reset password. Please try again later.';
      message.className = 'message error';
    }
  });
})();
</script>
</body>
</html>
`))

type passwordResetPageData struct {
	APIEndpoint string
}

type passwordResetTokenRequest struct {
	ClientSecret string `json:"client_secret"`
	Email        string `json:"email"`
	SendAttempt  int    `json:"send_attempt"`
}

type passwordResetSubmitRequest struct {
	NewPassword   string `json:"new_password"`
	LogoutDevices *bool  `json:"logout_devices"`
}

func RequestPasswordResetToken(req *http.Request, userAPI api.ClientUserAPI, cfg *config.ClientAPI) util.JSONResponse {
	start := time.Now()
	defer sleepToEqualize(start)
	if cfg == nil || !cfg.PasswordReset.Enabled {
		return util.JSONResponse{Code: http.StatusNotFound, JSON: spec.NotFound("password reset disabled")}
	}

	var body passwordResetTokenRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.BadJSON("invalid password reset request")}
	}

	canonicalEmail := iutil.NormalizeEmail(body.Email)
	if !isValidEmail(canonicalEmail) {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.InvalidParam("email")}
	}
	if body.ClientSecret == "" {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.MissingParam("client_secret")}
	}
	if body.SendAttempt <= 0 {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.InvalidParam("send_attempt")}
	}

	attempt, err := userAPI.LookupPasswordResetAttempt(req.Context(), body.ClientSecret, canonicalEmail, body.SendAttempt)
	if err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("LookupPasswordResetAttempt failed")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}
	if attempt != nil {
		return util.JSONResponse{Code: http.StatusOK, JSON: map[string]string{"sid": attempt.SessionID}}
	}

	threePIDRes := &api.QueryLocalpartForThreePIDResponse{}
	threePIDReq := &api.QueryLocalpartForThreePIDRequest{
		ThreePID: canonicalEmail,
		Medium:   "email",
	}
	if err := userAPI.QueryLocalpartForThreePID(req.Context(), threePIDReq, threePIDRes); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			util.GetLogger(req.Context()).WithError(err).Error("QueryLocalpartForThreePID failed")
			return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
		}
	}
	userExists := threePIDRes.Localpart != "" && string(threePIDRes.ServerName) != ""

	if resp := enforceIPRateLimit(req, userAPI); resp != nil {
		return *resp
	}
	if resp := enforceEmailRateLimit(req.Context(), userAPI, canonicalEmail); resp != nil {
		return *resp
	}

	sid, err := sessionIDGenerator()
	if err != nil {
		return util.ErrorResponse(fmt.Errorf("generate session: %w", err))
	}

	token, err := tokenGenerator()
	if err != nil {
		return util.ErrorResponse(fmt.Errorf("generate token: %w", err))
	}

	tokenHash, err := tokenHasher.HashToken(token)
	if err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("failed to hash password reset token")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}
	tokenLookup := passwordreset.LookupKey(token)

	validity := cfg.PasswordReset.TokenLifetime
	if validity <= 0 {
		validity = time.Hour
	}
	if cfg.PasswordReset.PublicBaseURL == "" {
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{Err: "password reset base URL not configured"}}
	}

	serverName := spec.ServerName("invalid.local")
	if cfg.Matrix != nil && cfg.Matrix.ServerName != "" {
		serverName = cfg.Matrix.ServerName
	}
	userID := fmt.Sprintf("@%s:%s", threePIDRes.Localpart, threePIDRes.ServerName)
	if !userExists {
		userID = dummyUserID(canonicalEmail, serverName)
	}
	expiresAt := time.Now().Add(validity)
	if err := userAPI.StorePasswordResetToken(req.Context(), tokenHash, tokenLookup, userID, canonicalEmail, sid, body.ClientSecret, body.SendAttempt, expiresAt); err != nil {
		if errors.Is(err, api.ErrPasswordResetAttemptExists) {
			retryAttempt, lookupErr := userAPI.LookupPasswordResetAttempt(req.Context(), body.ClientSecret, canonicalEmail, body.SendAttempt)
			if lookupErr != nil {
				util.GetLogger(req.Context()).WithError(lookupErr).Error("LookupPasswordResetAttempt failed after duplicate insert")
				return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
			}
			if retryAttempt == nil {
				util.GetLogger(req.Context()).Error("password reset attempt missing after duplicate insert")
				return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
			}
			return util.JSONResponse{Code: http.StatusOK, JSON: map[string]string{"sid": retryAttempt.SessionID}}
		}
		util.GetLogger(req.Context()).WithError(err).Error("StorePasswordResetToken failed")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}

	resetLink := buildPasswordResetLink(cfg.PasswordReset.PublicBaseURL, token)

	if err := passwordResetEmailSender(req.Context(), cfg, canonicalEmail, resetLink); err != nil {
		if delErr := userAPI.DeletePasswordResetToken(req.Context(), tokenLookup); delErr != nil {
			util.GetLogger(req.Context()).WithError(delErr).Warn("failed to delete password reset token after send failure")
		}
		util.GetLogger(req.Context()).WithError(err).Error("failed to send password reset email")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}
	return util.JSONResponse{Code: http.StatusOK, JSON: map[string]string{"sid": sid}}
}

func CompletePasswordReset(req *http.Request, userAPI api.ClientUserAPI, cfg *config.ClientAPI) util.JSONResponse {
	start := time.Now()
	defer sleepToEqualize(start)
	if cfg == nil || !cfg.PasswordReset.Enabled {
		return util.JSONResponse{Code: http.StatusNotFound, JSON: spec.NotFound("password reset disabled")}
	}

	token := req.URL.Query().Get("token")
	if token == "" {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.MissingParam("token")}
	}

	var body passwordResetSubmitRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.BadJSON("invalid password reset request")}
	}
	if body.NewPassword == "" {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.MissingParam("new_password")}
	}

	if err := internal.ValidatePassword(body.NewPassword); err != nil {
		if res := internal.PasswordResponse(err); res != nil {
			return *res
		}
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.BadJSON(err.Error())}
	}

	tokenLookup := passwordreset.LookupKey(token)
	tokenInfo, err := userAPI.GetPasswordResetToken(req.Context(), tokenLookup)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return util.JSONResponse{Code: http.StatusForbidden, JSON: spec.Forbidden("invalid or expired token")}
		}
		util.GetLogger(req.Context()).WithError(err).Error("GetPasswordResetToken failed")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}

	if time.Now().After(tokenInfo.ExpiresAt) {
		return util.JSONResponse{Code: http.StatusForbidden, JSON: spec.Forbidden("invalid or expired token")}
	}

	valid, verifyErr := tokenHasher.VerifyToken(token, tokenInfo.TokenHash)
	if verifyErr != nil || !valid {
		return util.JSONResponse{Code: http.StatusForbidden, JSON: spec.Forbidden("invalid or expired token")}
	}
	canonicalEmail := iutil.NormalizeEmail(tokenInfo.Email)
	if canonicalEmail == "" {
		return util.JSONResponse{Code: http.StatusForbidden, JSON: spec.Forbidden("invalid or expired token")}
	}
	verifyRes := &api.QueryLocalpartForThreePIDResponse{}
	verifyReq := &api.QueryLocalpartForThreePIDRequest{
		ThreePID: canonicalEmail,
		Medium:   "email",
	}
	if err := userAPI.QueryLocalpartForThreePID(req.Context(), verifyReq, verifyRes); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return util.JSONResponse{Code: http.StatusForbidden, JSON: spec.Forbidden("invalid or expired token")}
		}
		util.GetLogger(req.Context()).WithError(err).Error("QueryLocalpartForThreePID verification failed")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}
	if verifyRes.Localpart == "" || string(verifyRes.ServerName) == "" {
		return util.JSONResponse{Code: http.StatusForbidden, JSON: spec.Forbidden("invalid or expired token")}
	}
	expectedUserID := fmt.Sprintf("@%s:%s", verifyRes.Localpart, verifyRes.ServerName)
	if expectedUserID != tokenInfo.UserID {
		return util.JSONResponse{Code: http.StatusForbidden, JSON: spec.Forbidden("invalid or expired token")}
	}

	claimRes, err := userAPI.ConsumePasswordResetToken(req.Context(), tokenLookup, tokenInfo.TokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return util.JSONResponse{Code: http.StatusForbidden, JSON: spec.Forbidden("invalid or expired token")}
		}
		util.GetLogger(req.Context()).WithError(err).Error("ConsumePasswordResetToken failed")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}
	if claimRes == nil || !claimRes.Claimed {
		return util.JSONResponse{Code: http.StatusForbidden, JSON: spec.Forbidden("invalid or expired token")}
	}

	localpart, domain, err := gomatrixserverlib.SplitID('@', tokenInfo.UserID)
	if err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("gomatrixserverlib.SplitID failed for password reset user")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}

	updateReq := &api.PerformPasswordUpdateRequest{
		Localpart:  localpart,
		ServerName: domain,
		Password:   body.NewPassword,
	}
	updateRes := &api.PerformPasswordUpdateResponse{}
	if err := userAPI.PerformPasswordUpdate(req.Context(), updateReq, updateRes); err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("PerformPasswordUpdate failed")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}
	if !updateRes.PasswordUpdated {
		util.GetLogger(req.Context()).Error("password reset: expected password to be updated but it was not")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}

	logoutDevices := true
	if body.LogoutDevices != nil {
		logoutDevices = *body.LogoutDevices
	}
	if logoutDevices {
		delReq := &api.PerformDeviceDeletionRequest{
			UserID: tokenInfo.UserID,
		}
		if err := userAPI.PerformDeviceDeletion(req.Context(), delReq, &api.PerformDeviceDeletionResponse{}); err != nil {
			util.GetLogger(req.Context()).WithError(err).Error("PerformDeviceDeletion failed during password reset")
			return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
		}

		pushersReq := &api.PerformPusherDeletionRequest{
			Localpart:  localpart,
			ServerName: domain,
			SessionID:  -1,
		}
		if err := userAPI.PerformPusherDeletion(req.Context(), pushersReq, &struct{}{}); err != nil {
			util.GetLogger(req.Context()).WithError(err).Error("PerformPusherDeletion failed during password reset")
			return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
		}
	}

	return util.JSONResponse{Code: http.StatusOK, JSON: struct{}{}}
}

func buildPasswordResetLink(baseURL, token string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	return fmt.Sprintf("%s/_matrix/client/v3/account/password/reset?token=%s", trimmed, url.QueryEscape(token))
}

func servePasswordResetFallback(w http.ResponseWriter, req *http.Request) {
	token := req.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}

	endpoint := fmt.Sprintf("/_matrix/client/v3/account/password?token=%s", url.QueryEscape(token))
	data := passwordResetPageData{
		APIEndpoint: endpoint,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")

	if err := passwordResetFallbackTemplate.Execute(w, data); err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("failed to render password reset fallback page")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func isValidEmail(email string) bool {
	if email == "" {
		return false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return false
	}
	return addr.Address == email
}

func sendPasswordResetEmail(ctx context.Context, cfg *config.ClientAPI, email, resetLink string) error {
	if cfg.PasswordReset.From == "" {
		return fmt.Errorf("password reset from address not configured")
	}
	smtpCfg := cfg.PasswordReset.SMTP
	if smtpCfg.Host == "" {
		return fmt.Errorf("password reset SMTP host not configured")
	}
	if containsHeaderInjection(cfg.PasswordReset.From) || containsHeaderInjection(cfg.PasswordReset.Subject) {
		return fmt.Errorf("password reset header contains invalid characters")
	}
	toAddr, err := mail.ParseAddress(email)
	if err != nil {
		return fmt.Errorf("invalid recipient email: %w", err)
	}
	fromAddr, err := mail.ParseAddress(cfg.PasswordReset.From)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}

	subject := cfg.PasswordReset.Subject
	if subject == "" {
		subject = "Reset your Matrix password"
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("From: %s\r\n", fromAddr.String()))
	builder.WriteString(fmt.Sprintf("To: %s\r\n", toAddr.Address))
	builder.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	builder.WriteString("MIME-Version: 1.0\r\n")
	builder.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	builder.WriteString("\r\n")
	builder.WriteString("Hello,\r\n\r\n")
	builder.WriteString("A request was made to reset the password for your Matrix account. If this was you, use the link below to continue:\r\n\r\n")
	builder.WriteString(resetLink)
	builder.WriteString("\r\n\r\n")
	builder.WriteString("If you did not request this reset, you can ignore this email.\r\n")

	message := builder.String()
	if err := sendEmailViaSMTP(ctx, &smtpCfg, fromAddr, toAddr, message); err != nil {
		return err
	}
	return nil
}

func containsHeaderInjection(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}

func dummyUserID(email string, serverName spec.ServerName) string {
	sum := sha256.Sum256([]byte(email))
	localpart := fmt.Sprintf("_reset_%x", sum[:6])
	domain := "invalid.local"
	if serverName != "" {
		domain = string(serverName)
	}
	return fmt.Sprintf("@%s:%s", localpart, domain)
}

func generateSecureToken() (string, error) {
	buf := make([]byte, passwordResetTokenLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func generateSessionID() (string, error) {
	buf := make([]byte, passwordResetSessionLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func clientIPFromRequest(req *http.Request) string {
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" && requestFromTrustedProxy(req.RemoteAddr) {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			if ip := strings.TrimSpace(parts[0]); ip != "" {
				return ip
			}
		}
	}
	if req.RemoteAddr != "" {
		if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
			return host
		}
		return req.RemoteAddr
	}
	return ""
}

func requestFromTrustedProxy(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

func enforceIPRateLimit(req *http.Request, userAPI api.ClientUserAPI) *util.JSONResponse {
	ip := clientIPFromRequest(req)
	if ip == "" {
		ip = "unknown"
	}
	key := fmt.Sprintf("ip:%s", passwordreset.LookupKey(ip))
	allowed, retryAfter, err := userAPI.CheckPasswordResetRateLimit(req.Context(), key, passwordResetWindow, passwordResetIPLimit)
	if err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("CheckPasswordResetRateLimit (ip) failed")
		return &util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}
	if !allowed {
		return rateLimitExceededResponse("Too many requests from this IP", retryAfter)
	}
	return nil
}

func enforceEmailRateLimit(ctx context.Context, userAPI api.ClientUserAPI, email string) *util.JSONResponse {
	if email == "" {
		return nil
	}
	key := fmt.Sprintf("email:%s", passwordreset.LookupKey(email))
	allowed, retryAfter, err := userAPI.CheckPasswordResetRateLimit(ctx, key, passwordResetWindow, passwordResetEmailLimit)
	if err != nil {
		util.GetLogger(ctx).WithError(err).Error("CheckPasswordResetRateLimit (email) failed")
		return &util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}
	if !allowed {
		return rateLimitExceededResponse("Too many password reset requests", retryAfter)
	}
	return nil
}

func rateLimitExceededResponse(message string, retryAfter time.Duration) *util.JSONResponse {
	seconds := int(math.Ceil(retryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	retryHeader := strconv.Itoa(seconds)
	return &util.JSONResponse{
		Code:    http.StatusTooManyRequests,
		JSON:    spec.LimitExceeded(message, retryAfter.Milliseconds()),
		Headers: map[string]string{"Retry-After": retryHeader},
	}
}

func sleepToEqualize(start time.Time) {
	elapsed := time.Since(start)
	if elapsed < passwordResetMinDuration {
		time.Sleep(passwordResetMinDuration - elapsed)
	}
}
