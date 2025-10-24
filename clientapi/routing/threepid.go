// Copyright 2024 New Vector Ltd.
// Copyright 2017 Vector Creations Ltd
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package routing

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/element-hq/dendrite/clientapi/auth/authtypes"
	"github.com/element-hq/dendrite/clientapi/httputil"
	"github.com/element-hq/dendrite/clientapi/threepid"
	"github.com/element-hq/dendrite/internal/passwordreset"
	iutil "github.com/element-hq/dendrite/internal/util"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/userapi/api"
	userdb "github.com/element-hq/dendrite/userapi/storage"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/fclient"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
)

type reqTokenResponse struct {
	SID string `json:"sid"`
}

type ThreePIDsResponse struct {
	ThreePIDs []authtypes.ThreePID `json:"threepids"`
}

const (
	threePIDEmailLimit = 3
	threePIDIPLimit    = 10
	threePIDRateWindow = time.Hour
)

var errEmailTokenInvalid = errors.New("invalid or expired token")

var emailVerificationSender = sendEmailVerificationMail

var emailVerificationFallbackTemplate = template.Must(template.New("threepid_email_fallback").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Verify your email</title>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
    body { font-family: Arial, sans-serif; background: #f5f5f5; margin: 0; padding: 0; }
    .container { max-width: 420px; margin: 48px auto; background: #ffffff; padding: 32px; border-radius: 12px; box-shadow: 0 6px 24px rgba(0, 0, 0, 0.08); }
    h1 { font-size: 1.5rem; margin-top: 0; }
    p { line-height: 1.4; }
    .status { margin-top: 24px; font-weight: 600; }
  </style>
</head>
<body>
<div class="container">
  <h1>Verifying your email…</h1>
  <p>If this page does not update automatically, please copy the code from your email into your Matrix client.</p>
  <p class="status" id="status">Checking…</p>
</div>
<script>
(async function(){
  try {
    const res = await fetch("{{.PostPath}}", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({ sid: "{{.SID}}", token: "{{.Token}}" })
    });
    const data = await res.json().catch(() => ({}));
    if (res.ok) {
      document.getElementById("status").textContent = "Your email has been verified. You can close this window.";
    } else {
      const message = data.error || "We couldn’t verify your email. Please try again from your Matrix client.";
      document.getElementById("status").textContent = message;
    }
  } catch (err) {
    document.getElementById("status").textContent = "We couldn’t verify your email. Please try again from your Matrix client.";
  }
})();
</script>
</body>
</html>`))

// RequestEmailToken implements:
//
//	POST /account/3pid/email/requestToken
//	POST /register/email/requestToken
func RequestEmailToken(req *http.Request, threePIDAPI api.ClientUserAPI, cfg *config.ClientAPI, client *fclient.Client) util.JSONResponse {
	if cfg != nil && cfg.ThreePIDEmail.Enabled {
		return requestLocalEmailVerificationToken(req, threePIDAPI, cfg)
	}
	return requestIdentityServerEmailToken(req, threePIDAPI, cfg, client)
}

type emailVerificationRequest struct {
	ClientSecret string `json:"client_secret"`
	Email        string `json:"email"`
	SendAttempt  int    `json:"send_attempt"`
	NextLink     string `json:"next_link"`
}

func requestLocalEmailVerificationToken(req *http.Request, threePIDAPI api.ClientUserAPI, cfg *config.ClientAPI) util.JSONResponse {
	start := time.Now()
	defer sleepToEqualize(start)

	var body emailVerificationRequest
	if reqErr := httputil.UnmarshalJSONRequest(req, &body); reqErr != nil {
		return *reqErr
	}

	if body.ClientSecret == "" {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.MissingParam("client_secret")}
	}
	if body.SendAttempt <= 0 {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.InvalidParam("send_attempt")}
	}

	canonicalEmail := iutil.NormalizeEmail(body.Email)
	if canonicalEmail == "" {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.InvalidParam("email")}
	}

	// Prevent duplicates across accounts.
	lookupRes := &api.QueryLocalpartForThreePIDResponse{}
	if err := threePIDAPI.QueryLocalpartForThreePID(req.Context(), &api.QueryLocalpartForThreePIDRequest{
		ThreePID: canonicalEmail,
		Medium:   "email",
	}, lookupRes); err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("threePIDAPI.QueryLocalpartForThreePID failed")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}
	if lookupRes.Localpart != "" {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.MatrixError{ErrCode: spec.ErrorThreePIDInUse, Err: userdb.Err3PIDInUse.Error()},
		}
	}

	if resp := enforceThreePIDIPRateLimit(req, threePIDAPI); resp != nil {
		return *resp
	}
	if resp := enforceThreePIDEmailRateLimit(req.Context(), threePIDAPI, canonicalEmail); resp != nil {
		return *resp
	}

	sid, err := sessionIDGenerator()
	if err != nil {
		return util.ErrorResponse(fmt.Errorf("generate session id: %w", err))
	}
	token, err := tokenGenerator()
	if err != nil {
		return util.ErrorResponse(fmt.Errorf("generate token: %w", err))
	}

	tokenHash, err := tokenHasher.HashToken(token)
	if err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("failed to hash email verification token")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}
	tokenLookup := passwordreset.LookupKey(token)
	clientSecretHash := passwordreset.LookupKey(body.ClientSecret)

	validity := cfg.ThreePIDEmail.TokenLifetime
	if validity <= 0 {
		validity = 24 * time.Hour
	}
	expiresAt := time.Now().Add(validity)

	session := &api.EmailVerificationSession{
		SessionID:        sid,
		ClientSecretHash: clientSecretHash,
		Email:            canonicalEmail,
		Medium:           "email",
		TokenLookup:      tokenLookup,
		TokenHash:        tokenHash,
		SendAttempt:      body.SendAttempt,
		NextLink:         strings.TrimSpace(body.NextLink),
		ExpiresAt:        expiresAt,
	}

	storedSession, created, err := threePIDAPI.CreateOrReuseEmailVerificationSession(req.Context(), session)
	if err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("CreateOrReuseEmailVerificationSession failed")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}

	// If session already existed, do not send another email.
	if !created {
		return util.JSONResponse{Code: http.StatusOK, JSON: reqTokenResponse{SID: storedSession.SessionID}}
	}

	baseURL := strings.TrimRight(cfg.ThreePIDEmail.PublicBaseURL, "/")
	if baseURL == "" {
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{Err: "threepid email base URL not configured"}}
	}

	submitPath := "/_matrix/client/v3/account/3pid/email/submitToken"
	if strings.Contains(req.URL.Path, "/register/") {
		submitPath = "/_matrix/client/v3/register/email/submitToken"
	}
	verificationLink := fmt.Sprintf("%s%s?sid=%s&token=%s", baseURL, submitPath, url.QueryEscape(storedSession.SessionID), url.QueryEscape(token))

	if err := emailVerificationSender(req.Context(), cfg, canonicalEmail, verificationLink); err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("failed to send email verification message")
		if deleteErr := threePIDAPI.DeleteEmailVerificationSession(req.Context(), storedSession.SessionID); deleteErr != nil {
			util.GetLogger(req.Context()).WithError(deleteErr).Warn("failed to delete email verification session after send failure")
		}
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}

	return util.JSONResponse{Code: http.StatusOK, JSON: reqTokenResponse{SID: storedSession.SessionID}}
}

func sendEmailVerificationMail(ctx context.Context, cfg *config.ClientAPI, email, verificationLink string) error {
	if cfg.ThreePIDEmail.From == "" {
		return fmt.Errorf("threepid email from address not configured")
	}
	if cfg.ThreePIDEmail.SMTP.Host == "" {
		return fmt.Errorf("threepid email smtp host not configured")
	}
	if containsHeaderInjection(cfg.ThreePIDEmail.From) || containsHeaderInjection(cfg.ThreePIDEmail.Subject) {
		return fmt.Errorf("threepid email header contains invalid characters")
	}

	toAddr, err := mail.ParseAddress(email)
	if err != nil {
		return fmt.Errorf("invalid recipient email: %w", err)
	}
	fromAddr, err := mail.ParseAddress(cfg.ThreePIDEmail.From)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}

	subject := cfg.ThreePIDEmail.Subject
	if subject == "" {
		subject = "Verify your email"
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("From: %s\r\n", fromAddr.String()))
	builder.WriteString(fmt.Sprintf("To: %s\r\n", toAddr.Address))
	builder.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	builder.WriteString("MIME-Version: 1.0\r\n")
	builder.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	builder.WriteString("\r\n")
	builder.WriteString("Hello,\r\n\r\n")
	builder.WriteString("Use the link below to verify your email address for your Matrix account:\r\n\r\n")
	builder.WriteString(verificationLink)
	builder.WriteString("\r\n\r\n")
	builder.WriteString("If you did not request this, you can ignore this email.\r\n")

	return sendEmailViaSMTP(ctx, &cfg.ThreePIDEmail.SMTP, fromAddr, toAddr, builder.String())
}

func requestIdentityServerEmailToken(req *http.Request, threePIDAPI api.ClientUserAPI, cfg *config.ClientAPI, client *fclient.Client) util.JSONResponse {
	var body threepid.EmailAssociationRequest
	if reqErr := httputil.UnmarshalJSONRequest(req, &body); reqErr != nil {
		return *reqErr
	}

	var resp reqTokenResponse
	res := &api.QueryLocalpartForThreePIDResponse{}
	canonicalEmail := iutil.NormalizeEmail(body.Email)
	if canonicalEmail == "" {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.InvalidParam("email"),
		}
	}
	if err := threePIDAPI.QueryLocalpartForThreePID(req.Context(), &api.QueryLocalpartForThreePIDRequest{
		ThreePID: canonicalEmail,
		Medium:   "email",
	}, res); err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("threePIDAPI.QueryLocalpartForThreePID failed")
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}

	if len(res.Localpart) > 0 {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.MatrixError{
				ErrCode: spec.ErrorThreePIDInUse,
				Err:     userdb.Err3PIDInUse.Error(),
			},
		}
	}

	body.Email = canonicalEmail
	sid, err := threepid.CreateSession(req.Context(), body, cfg, client)
	switch err.(type) {
	case nil:
		resp.SID = sid
	case threepid.ErrNotTrusted:
		util.GetLogger(req.Context()).WithError(err).Error("threepid.CreateSession failed")
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.NotTrusted(body.IDServer),
		}
	default:
		util.GetLogger(req.Context()).WithError(err).Error("threepid.CreateSession failed")
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}

	return util.JSONResponse{Code: http.StatusOK, JSON: resp}
}

// CheckAndSave3PIDAssociation implements POST /account/3pid
func CheckAndSave3PIDAssociation(
	req *http.Request, threePIDAPI api.ClientUserAPI, device *api.Device,
	cfg *config.ClientAPI, client *fclient.Client,
) util.JSONResponse {
	if cfg != nil && cfg.ThreePIDEmail.Enabled {
		return checkAndSaveLocal3PID(req, threePIDAPI, device)
	}
	return checkAndSaveIdentity3PID(req, threePIDAPI, device, cfg, client)
}

func checkAndSaveIdentity3PID(
	req *http.Request, threePIDAPI api.ClientUserAPI, device *api.Device,
	cfg *config.ClientAPI, client *fclient.Client,
) util.JSONResponse {
	var body threepid.EmailAssociationCheckRequest
	if reqErr := httputil.UnmarshalJSONRequest(req, &body); reqErr != nil {
		return *reqErr
	}

	// Check if the association has been validated
	verified, address, medium, err := threepid.CheckAssociation(req.Context(), body.Creds, cfg, client)
	switch err.(type) {
	case nil:
	case threepid.ErrNotTrusted:
		util.GetLogger(req.Context()).WithError(err).Error("threepid.CheckAssociation failed")
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.NotTrusted(body.Creds.IDServer),
		}
	default:
		util.GetLogger(req.Context()).WithError(err).Error("threepid.CheckAssociation failed")
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}

	if !verified {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.MatrixError{
				ErrCode: spec.ErrorThreePIDAuthFailed,
				Err:     "Failed to auth 3pid",
			},
		}
	}

	if body.Bind {
		// Publish the association on the identity server if requested
		err = threepid.PublishAssociation(req.Context(), body.Creds, device.UserID, cfg, client)
		if err != nil {
			util.GetLogger(req.Context()).WithError(err).Error("threepid.PublishAssociation failed")
			return util.JSONResponse{
				Code: http.StatusInternalServerError,
				JSON: spec.InternalServerError{},
			}
		}
	}

	// Save the association in the database
	localpart, domain, err := gomatrixserverlib.SplitID('@', device.UserID)
	if err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("gomatrixserverlib.SplitID failed")
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}

	normalizedAddress := strings.TrimSpace(address)
	if strings.EqualFold(medium, "email") {
		normalizedAddress = iutil.NormalizeEmail(address)
	}
	if err = threePIDAPI.PerformSaveThreePIDAssociation(req.Context(), &api.PerformSaveThreePIDAssociationRequest{
		ThreePID:   normalizedAddress,
		Localpart:  localpart,
		ServerName: domain,
		Medium:     medium,
	}, &struct{}{}); err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("threePIDAPI.PerformSaveThreePIDAssociation failed")
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}

	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: struct{}{},
	}
}

func checkAndSaveLocal3PID(
	req *http.Request, threePIDAPI api.ClientUserAPI, device *api.Device,
) util.JSONResponse {
	var body threepid.EmailAssociationCheckRequest
	if reqErr := httputil.UnmarshalJSONRequest(req, &body); reqErr != nil {
		return *reqErr
	}

	if body.Creds.SID == "" {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.MissingParam("sid")}
	}
	if body.Creds.Secret == "" {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.MissingParam("client_secret")}
	}

	session, err := threePIDAPI.GetEmailVerificationSession(req.Context(), body.Creds.SID)
	if err != nil {
		if errors.Is(err, api.ErrEmailVerificationSessionNotFound) {
			return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.MatrixError{ErrCode: spec.ErrorThreePIDAuthFailed, Err: "invalid session"}}
		}
		util.GetLogger(req.Context()).WithError(err).Error("GetEmailVerificationSession failed")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}

	providedHash := passwordreset.LookupKey(body.Creds.Secret)
	if subtle.ConstantTimeCompare([]byte(providedHash), []byte(session.ClientSecretHash)) != 1 {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.MatrixError{ErrCode: spec.ErrorThreePIDAuthFailed, Err: "invalid client secret"}}
	}

	if session.ValidatedAt == nil {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.MatrixError{ErrCode: spec.ErrorThreePIDAuthFailed, Err: "session not validated"}}
	}
	if time.Now().After(session.ExpiresAt) {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.MatrixError{ErrCode: spec.ErrorThreePIDAuthFailed, Err: "session expired"}}
	}
	if session.ConsumedAt != nil {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.MatrixError{ErrCode: spec.ErrorThreePIDAuthFailed, Err: "session already used"}}
	}
	if !strings.EqualFold(session.Medium, "email") {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.MatrixError{ErrCode: spec.ErrorThreePIDAuthFailed, Err: "unsupported medium"}}
	}

	localpart, domain, err := gomatrixserverlib.SplitID('@', device.UserID)
	if err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("gomatrixserverlib.SplitID failed")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}

	if err = threePIDAPI.PerformSaveThreePIDAssociation(req.Context(), &api.PerformSaveThreePIDAssociationRequest{
		ThreePID:   session.Email,
		Localpart:  localpart,
		ServerName: domain,
		Medium:     session.Medium,
	}, &struct{}{}); err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("threePIDAPI.PerformSaveThreePIDAssociation failed")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}

	if err := threePIDAPI.MarkEmailVerificationSessionConsumed(req.Context(), session.SessionID, time.Now().UTC()); err != nil {
		util.GetLogger(req.Context()).WithError(err).Warn("failed to mark email verification session consumed")
	}

	return util.JSONResponse{Code: http.StatusOK, JSON: struct{}{}}
}

// GetAssociated3PIDs implements GET /account/3pid
func GetAssociated3PIDs(
	req *http.Request, threepidAPI api.ClientUserAPI, device *api.Device,
) util.JSONResponse {
	localpart, domain, err := gomatrixserverlib.SplitID('@', device.UserID)
	if err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("gomatrixserverlib.SplitID failed")
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}

	res := &api.QueryThreePIDsForLocalpartResponse{}
	err = threepidAPI.QueryThreePIDsForLocalpart(req.Context(), &api.QueryThreePIDsForLocalpartRequest{
		Localpart:  localpart,
		ServerName: domain,
	}, res)
	if err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("threepidAPI.QueryThreePIDsForLocalpart failed")
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}

	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: ThreePIDsResponse{res.ThreePIDs},
	}
}

// Forget3PID implements POST /account/3pid/delete
func Forget3PID(req *http.Request, threepidAPI api.ClientUserAPI) util.JSONResponse {
	var body authtypes.ThreePID
	if reqErr := httputil.UnmarshalJSONRequest(req, &body); reqErr != nil {
		return *reqErr
	}

	// Normalize the address to ensure case-insensitive matching
	normalizedAddress := strings.TrimSpace(body.Address)
	if strings.EqualFold(body.Medium, "email") {
		normalizedAddress = iutil.NormalizeEmail(body.Address)
	}

	if err := threepidAPI.PerformForgetThreePID(req.Context(), &api.PerformForgetThreePIDRequest{
		ThreePID: normalizedAddress,
		Medium:   body.Medium,
	}, &struct{}{}); err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("threepidAPI.PerformForgetThreePID failed")
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}

	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: struct{}{},
	}
}

type submitEmailTokenRequest struct {
	SID   string `json:"sid"`
	Token string `json:"token"`
}

type submitEmailTokenResponse struct {
	Success bool `json:"success"`
}

type emailVerificationFallbackData struct {
	SID      string
	Token    string
	PostPath string
}

func validateEmailVerificationToken(ctx context.Context, threePIDAPI api.ClientUserAPI, sid, token string) error {
	session, err := threePIDAPI.GetEmailVerificationSession(ctx, sid)
	if err != nil {
		if errors.Is(err, api.ErrEmailVerificationSessionNotFound) {
			return errEmailTokenInvalid
		}
		return err
	}

	if time.Now().After(session.ExpiresAt) {
		return errEmailTokenInvalid
	}

	valid, verifyErr := tokenHasher.VerifyToken(token, session.TokenHash)
	if verifyErr != nil {
		return verifyErr
	}
	if !valid {
		return errEmailTokenInvalid
	}

	if session.ValidatedAt == nil {
		if err := threePIDAPI.MarkEmailVerificationSessionValidated(ctx, session.SessionID, time.Now().UTC()); err != nil {
			return err
		}
	}

	return nil
}

func SubmitEmailToken(req *http.Request, threePIDAPI api.ClientUserAPI, cfg *config.ClientAPI) util.JSONResponse {
	if cfg == nil || !cfg.ThreePIDEmail.Enabled {
		return util.JSONResponse{Code: http.StatusNotFound, JSON: spec.NotFound("email verification disabled")}
	}
	start := time.Now()
	defer sleepToEqualize(start)

	var body submitEmailTokenRequest
	if reqErr := httputil.UnmarshalJSONRequest(req, &body); reqErr != nil {
		return *reqErr
	}
	if body.SID == "" {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.MissingParam("sid")}
	}
	if body.Token == "" {
		return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.MissingParam("token")}
	}

	if err := validateEmailVerificationToken(req.Context(), threePIDAPI, body.SID, body.Token); err != nil {
		if errors.Is(err, errEmailTokenInvalid) {
			return util.JSONResponse{Code: http.StatusForbidden, JSON: spec.Forbidden("invalid or expired token")}
		}
		util.GetLogger(req.Context()).WithError(err).Error("validateEmailVerificationToken failed")
		return util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}

	return util.JSONResponse{Code: http.StatusOK, JSON: submitEmailTokenResponse{Success: true}}
}

func enforceThreePIDEmailRateLimit(ctx context.Context, threePIDAPI api.ClientUserAPI, email string) *util.JSONResponse {
	if email == "" {
		return nil
	}
	key := fmt.Sprintf("threepid:email:%s", passwordreset.LookupKey(email))
	allowed, retryAfter, err := threePIDAPI.CheckEmailVerificationRateLimit(ctx, key, threePIDRateWindow, threePIDEmailLimit)
	if err != nil {
		util.GetLogger(ctx).WithError(err).Error("CheckEmailVerificationRateLimit (email) failed")
		return &util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}
	if !allowed {
		return rateLimitExceededResponse("Too many email verification requests", retryAfter)
	}
	return nil
}

func enforceThreePIDIPRateLimit(req *http.Request, threePIDAPI api.ClientUserAPI) *util.JSONResponse {
	ip := clientIPFromRequest(req)
	if ip == "" {
		ip = "unknown"
	}
	key := fmt.Sprintf("threepid:ip:%s", passwordreset.LookupKey(ip))
	allowed, retryAfter, err := threePIDAPI.CheckEmailVerificationRateLimit(req.Context(), key, threePIDRateWindow, threePIDIPLimit)
	if err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("CheckEmailVerificationRateLimit (ip) failed")
		return &util.JSONResponse{Code: http.StatusInternalServerError, JSON: spec.InternalServerError{}}
	}
	if !allowed {
		return rateLimitExceededResponse("Too many requests from this IP", retryAfter)
	}
	return nil
}

func ServeEmailTokenFallback(w http.ResponseWriter, req *http.Request, cfg *config.ClientAPI) {
	if cfg == nil || !cfg.ThreePIDEmail.Enabled {
		http.NotFound(w, req)
		return
	}
	sid := req.URL.Query().Get("sid")
	token := req.URL.Query().Get("token")
	if sid == "" || token == "" {
		http.Error(w, "missing sid or token", http.StatusBadRequest)
		return
	}

	data := emailVerificationFallbackData{
		SID:      sid,
		Token:    token,
		PostPath: req.URL.Path,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")

	if err := emailVerificationFallbackTemplate.Execute(w, data); err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("failed to render email verification fallback page")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
