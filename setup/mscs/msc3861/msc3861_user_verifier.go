package msc3861

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/element-hq/dendrite/clientapi/auth"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib/fclient"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
)

const externalAuthProvider string = "oauth-delegated"

// Scopes as defined by MSC2967
// https://github.com/matrix-org/matrix-spec-proposals/pull/2967
const (
	scopeMatrixAPI          string = "urn:matrix:org.matrix.msc2967.client:api:*"
	scopeMatrixGuest        string = "urn:matrix:org.matrix.msc2967.client:api:guest"
	scopeMatrixDevicePrefix string = "urn:matrix:org.matrix.msc2967.client:device:"
)

type errCode string

const (
	codeIntrospectionNot2xx        errCode = "introspectionIsNot2xx"
	codeInvalidClientToken         errCode = "invalidClientToken"
	codeAuthError                  errCode = "authError"
	codeMxidError                  errCode = "mxidError"
	codeOpenidConfigEndpointNon2xx errCode = "openidConfigEndpointNon2xx"
	codeOpenidConfigDecodingFailed errCode = "openidConfigDecodingFailed"
)

// MSC3861UserVerifier implements UserVerifier interface
type MSC3861UserVerifier struct {
	userAPI      api.UserInternalAPI
	serverName   spec.ServerName
	cfg          *config.MSC3861
	httpClient   *fclient.Client
	openIdConfig *OpenIDConfiguration
	allowGuest   bool
}

func newMSC3861UserVerifier(
	userAPI api.UserInternalAPI,
	serverName spec.ServerName,
	cfg *config.MSC3861,
	allowGuest bool,
	client *fclient.Client,
) (*MSC3861UserVerifier, error) {
	if cfg == nil {
		return nil, errors.New("unable to create MSC3861UserVerifier object as 'cfg' param is nil")
	}

	if client == nil {
		return nil, errors.New("unable to create MSC3861UserVerifier object as 'client' param is nil")
	}

	openIdConfig, err := fetchOpenIDConfiguration(client, cfg.Issuer)
	if err != nil {
		return nil, err
	}

	return &MSC3861UserVerifier{
		userAPI:      userAPI,
		serverName:   serverName,
		cfg:          cfg,
		openIdConfig: openIdConfig,
		allowGuest:   allowGuest,
		httpClient:   client,
	}, nil
}

type mscError struct {
	Code errCode
	Msg  string
}

func (r *mscError) Error() string {
	return fmt.Sprintf("%s: %s", r.Code, r.Msg)
}

// VerifyUserFromRequest authenticates the HTTP request, on success returns Device of the requester.
func (m *MSC3861UserVerifier) VerifyUserFromRequest(req *http.Request) (*api.Device, *util.JSONResponse) {
	util.GetLogger(req.Context()).Debug("MSC3861.VerifyUserFromRequest")
	// Try to find the Application Service user
	token, err := auth.ExtractAccessToken(req)
	if err != nil {
		return nil, &util.JSONResponse{
			Code: http.StatusUnauthorized,
			JSON: spec.MissingToken(err.Error()),
		}
	}
	// TODO: try to get appservice user first. See https://github.com/element-hq/synapse/blob/develop/synapse/api/auth/msc3861_delegated.py#L273
	userData, err := m.getUserByAccessToken(req.Context(), token)
	if err != nil {
		switch e := err.(type) {
		case (*mscError):
			switch e.Code {
			case codeIntrospectionNot2xx, codeOpenidConfigDecodingFailed, codeOpenidConfigEndpointNon2xx:
				return nil, &util.JSONResponse{
					Code: http.StatusServiceUnavailable,
					JSON: spec.Unknown(e.Error()),
				}
			case codeInvalidClientToken:
				return nil, &util.JSONResponse{
					Code: http.StatusUnauthorized,
					JSON: spec.UnknownToken(e.Error()),
				}
			case codeAuthError, codeMxidError:
				return nil, &util.JSONResponse{
					Code: http.StatusInternalServerError,
					JSON: spec.Unknown(e.Error()),
				}
			default:
				r := util.ErrorResponse(err)
				return nil, &r
			}
		default:
			r := util.ErrorResponse(err)
			return nil, &r
		}
	}

	// Do not record requests from MAS using the virtual `__oidc_admin` user.
	if token != m.cfg.AdminToken {
		// XXX: not sure which exact data we should record here. See the link for reference
		// https://github.com/element-hq/synapse/blob/develop/synapse/api/auth/base.py#L365
	}

	if !m.allowGuest && userData.IsGuest {
		return nil, &util.JSONResponse{
			Code: http.StatusUnauthorized,
			JSON: spec.Forbidden(strings.Join([]string{"Insufficient scope: ", scopeMatrixAPI}, "")),
		}
	}

	return userData.Device, nil
}

type requester struct {
	Device  *api.Device
	UserID  *spec.UserID
	Scope   []string
	IsGuest bool
}

// nolint: gocyclo
func (m *MSC3861UserVerifier) getUserByAccessToken(ctx context.Context, token string) (*requester, error) {
	var userID *spec.UserID
	logger := util.GetLogger(ctx)

	if adminToken := m.cfg.AdminToken; adminToken != "" && token == adminToken {
		// XXX: This is a temporary solution so that the admin API can be called by
		// the OIDC provider. This will be removed once we have OIDC client
		// credentials grant support in matrix-authentication-service.
		// XXX: that user doesn't exist and won't be provisioned.
		adminUser, err := createUserID("__oidc_admin", m.serverName)
		if err != nil {
			return nil, err
		}
		return &requester{
			UserID: adminUser,
			Scope:  []string{"urn:synapse:admin:*"},
			Device: &api.Device{UserID: adminUser.Local(), AccountType: api.AccountTypeOIDCService},
		}, nil
	}

	introspectionResult, err := m.introspectToken(ctx, token)
	if err != nil {
		logger.WithError(err).Error("MSC3861UserVerifier:introspectToken")
		return nil, err
	}

	if !introspectionResult.Active {
		return nil, &mscError{Code: codeInvalidClientToken, Msg: "Token is not active"}
	}

	scopes := introspectionResult.Scopes()
	hasUserScope, hasGuestScope := slices.Contains(scopes, scopeMatrixAPI), slices.Contains(scopes, scopeMatrixGuest)
	if !hasUserScope && !hasGuestScope {
		return nil, &mscError{Code: codeInvalidClientToken, Msg: "No scope in token granting user rights"}
	}

	sub := introspectionResult.Sub
	if sub == "" {
		return nil, &mscError{Code: codeInvalidClientToken, Msg: "Invalid sub claim in the introspection result"}
	}

	localpart := ""
	{
		var rs api.QueryLocalpartExternalIDResponse
		if err = m.userAPI.QueryExternalUserIDByLocalpartAndProvider(ctx, &api.QueryLocalpartExternalIDRequest{
			ExternalID:   sub,
			AuthProvider: externalAuthProvider,
		}, &rs); err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if l := rs.LocalpartExternalID; l != nil {
			localpart = l.Localpart
		}
	}

	if localpart == "" {
		// If we could not find a user via the external_id, it either does not exist,
		// or the external_id was never recorded
		username := introspectionResult.Username
		if username == "" {
			return nil, &mscError{Code: codeAuthError, Msg: "Invalid username claim in the introspection result"}
		}
		userID, err = createUserID(username, m.serverName)
		if err != nil {
			logger.WithError(err).Error("getUserByAccessToken:createUserID")
			return nil, err
		}

		// First try to find a user from the username claim
		var account *api.Account
		{
			var rs api.QueryAccountByLocalpartResponse
			err = m.userAPI.QueryAccountByLocalpart(ctx, &api.QueryAccountByLocalpartRequest{Localpart: userID.Local(), ServerName: userID.Domain()}, &rs)
			if err != nil && err != sql.ErrNoRows {
				logger.WithError(err).Error("QueryAccountByLocalpart")
				return nil, err
			}
			account = rs.Account
		}

		if account == nil {
			// If the user does not exist, we should create it on the fly
			var rs api.PerformAccountCreationResponse
			if err = m.userAPI.PerformAccountCreation(ctx, &api.PerformAccountCreationRequest{
				AccountType: api.AccountTypeUser,
				Localpart:   userID.Local(),
				ServerName:  userID.Domain(),
			}, &rs); err != nil {
				logger.WithError(err).Error("PerformAccountCreation")
				return nil, err
			}
		}

		if err = m.userAPI.PerformLocalpartExternalUserIDCreation(ctx, &api.PerformLocalpartExternalUserIDCreationRequest{
			Localpart:    userID.Local(),
			ExternalID:   sub,
			AuthProvider: externalAuthProvider,
		}); err != nil {
			logger.WithError(err).Error("PerformLocalpartExternalUserIDCreation")
			return nil, err
		}

		localpart = userID.Local()
	}

	if userID == nil {
		userID, err = createUserID(localpart, m.serverName)
		if err != nil {
			logger.WithError(err).Error("getUserByAccessToken:createUserID")
			return nil, err
		}
	}

	deviceIDs := make([]string, 0, 1)
	for i := range scopes {
		if s := scopes[i]; strings.HasPrefix(s, scopeMatrixDevicePrefix) {
			deviceIDs = append(deviceIDs, s[len(scopeMatrixDevicePrefix):])
		}
	}

	if len(deviceIDs) != 1 {
		logger.Errorf("Invalid device IDs in scope: %+v", deviceIDs)
		return nil, &mscError{Code: codeAuthError, Msg: "Invalid device IDs in scope"}
	}

	var device *api.Device

	deviceID := deviceIDs[0]
	if len(deviceID) > 255 || len(deviceID) < 1 {
		return nil, &mscError{
			Code: codeAuthError,
			Msg:  strings.Join([]string{"Invalid device ID in scope: ", deviceID}, ""),
		}
	}

	userDeviceExists := false
	{
		var rs api.QueryDevicesResponse
		err := m.userAPI.QueryDevices(ctx, &api.QueryDevicesRequest{UserID: userID.String()}, &rs)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}

		for i := range rs.Devices {
			if d := &rs.Devices[i]; d.ID == deviceID {
				userDeviceExists = true
				device = d
				break
			}
		}
	}
	if !userDeviceExists {
		var rs api.PerformDeviceCreationResponse
		deviceDisplayName := "OIDC-native client"
		if err := m.userAPI.PerformDeviceCreation(ctx, &api.PerformDeviceCreationRequest{
			Localpart:         localpart,
			ServerName:        m.serverName,
			AccessToken:       "",
			DeviceID:          &deviceID,
			DeviceDisplayName: &deviceDisplayName,
			// TODO: Cannot add IPAddr and Useragent values here. Should we care about it here?
		}, &rs); err != nil {
			logger.WithError(err).Error("PerformDeviceCreation")
			return nil, err
		}
		device = rs.Device
		logger.Debugf("PerformDeviceCreationResponse is: %+v", rs)
	}

	return &requester{
		Device:  device,
		UserID:  userID,
		Scope:   scopes,
		IsGuest: hasGuestScope && !hasUserScope,
	}, nil
}

func createUserID(local string, serverName spec.ServerName) (*spec.UserID, error) {
	userID, err := spec.NewUserID(strings.Join([]string{"@", local, ":", string(serverName)}, ""), false)
	if err != nil {
		return nil, &mscError{Code: codeMxidError, Msg: err.Error()}
	}
	return userID, nil
}

func (m *MSC3861UserVerifier) introspectToken(ctx context.Context, token string) (*introspectionResponse, error) {
	formBody := url.Values{"token": []string{token}}
	encoded := formBody.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.openIdConfig.IntrospectionEndpoint, strings.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(m.cfg.ClientID, m.cfg.ClientSecret)

	resp, err := m.httpClient.DoHTTPRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() // nolint: errcheck

	if c := resp.StatusCode; c/100 != 2 {
		return nil, errors.New(strings.Join([]string{"The introspection endpoint returned a '", resp.Status, "' response"}, ""))
	}
	var ir introspectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&ir); err != nil {
		return nil, err
	}
	return &ir, nil
}

type OpenIDConfiguration struct {
	Issuer                                     string   `json:"issuer"`
	AuthorizationEndpoint                      string   `json:"authorization_endpoint"`
	TokenEndpoint                              string   `json:"token_endpoint"`
	JWKsURI                                    string   `json:"jwks_uri"`
	RegistrationEndpoint                       string   `json:"registration_endpoint"`
	ScopesSupported                            []string `json:"scopes_supported"`
	ResponseTypesSupported                     []string `json:"response_types_supported"`
	ResponseModesSupported                     []string `json:"response_modes_supported"`
	GrantTypesSupported                        []string `json:"grant_types_supported"`
	TokenEndpointAuthMethodsSupported          []string `json:"token_endpoint_auth_methods_supported"`
	TokenEndpointAuthSigningAlgCaluesSupported []string `json:"token_endpoint_auth_signing_alg_values_supported"`
	RevocationEnpoint                          string   `json:"revocation_endpoint"`
	RevocationEndpointAuthMethodsSupported     []string `json:"revocation_endpoint_auth_methods_supported"`
	RevocationEndpointAuthSigningAlgValues     []string `json:"revocation_endpoint_auth_signing_alg_values_supported"`
	IntrospectionEndpoint                      string   `json:"introspection_endpoint"`
	IntrospectionEndpointAuthMethodsSupported  []string `json:"introspection_endpoint_auth_methods_supported"`
	IntrospectionEndpointAuthSigningAlgValues  []string `json:"introspection_endpoint_auth_signing_alg_values_supported"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported"`
	UserinfoEndpoint                           string   `json:"userinfo_endpoint"`
	SubjectTypesSupported                      []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported           []string `json:"id_token_signing_alg_values_supported"`
	UserinfoSigningAlgValuesSupported          []string `json:"userinfo_signing_alg_values_supported"`
	DisplayValuesSupported                     []string `json:"display_values_supported"`
	ClaimTypesSupported                        []string `json:"claim_types_supported"`
	ClaimsSupported                            []string `json:"claims_supported"`
	ClaimsParameterSupported                   bool     `json:"claims_parameter_supported"`
	RequestParameterSupported                  bool     `json:"request_parameter_supported"`
	RequestURIParameterSupported               bool     `json:"request_uri_parameter_supported"`
	PromptValuesSupported                      []string `json:"prompt_values_supported"`
	DeviceAuthorizaEndpoint                    string   `json:"device_authorization_endpoint"`
	AccountManagementURI                       string   `json:"account_management_uri"`
	AccountManagementActionsSupported          []string `json:"account_management_actions_supported"`
}

func fetchOpenIDConfiguration(httpClient *fclient.Client, authHostURL string) (*OpenIDConfiguration, error) {
	u, err := url.Parse(authHostURL)
	if err != nil {
		return nil, err
	}
	u = u.JoinPath(".well-known/openid-configuration")
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.DoHTTPRequest(context.Background(), req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() // nolint: errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, &mscError{Code: codeOpenidConfigEndpointNon2xx, Msg: ".well-known/openid-configuration endpoint returned non-200 response"}
	}
	var oic OpenIDConfiguration
	if err := json.NewDecoder(resp.Body).Decode(&oic); err != nil {
		return nil, &mscError{Code: codeOpenidConfigDecodingFailed, Msg: err.Error()}
	}
	return &oic, nil
}

// introspectionResponse as described in the RFC https://datatracker.ietf.org/doc/html/rfc7662#section-2.2
type introspectionResponse struct {
	Active    bool   `json:"active"`     // required
	Scope     string `json:"scope"`      // optional
	Username  string `json:"username"`   // optional
	TokenType string `json:"token_type"` // optional
	Exp       *int64 `json:"exp"`        // optional
	Iat       *int64 `json:"iat"`        // optional
	Nfb       *int64 `json:"nfb"`        // optional
	Sub       string `json:"sub"`        // optional
	Jti       string `json:"jti"`        // optional
	Aud       string `json:"aud"`        // optional
	Iss       string `json:"iss"`        // optional
}

func (i *introspectionResponse) Scopes() []string {
	return strings.Split(i.Scope, " ")
}
