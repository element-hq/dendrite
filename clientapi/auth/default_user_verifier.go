// Copyright 2025 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package auth

import (
	"net/http"
	"strings"

	"github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
)

// DefaultUserVerifier implements UserVerifier interface
type DefaultUserVerifier struct {
	UserAPI api.QueryAccessTokenAPI
}

// VerifyUserFromRequest authenticates the HTTP request,
// on success returns Device of the requester.
// Finds local user or an application service user.
// Note: For an AS user, AS dummy device is returned.
// On failure returns an JSON error response which can be sent to the client.
func (d *DefaultUserVerifier) VerifyUserFromRequest(req *http.Request) (*api.Device, *util.JSONResponse) {
	ctx := req.Context()
	util.GetLogger(ctx).Debug("Default VerifyUserFromRequest")
	// Try to find the Application Service user
	token, err := ExtractAccessToken(req)
	if err != nil {
		return nil, &util.JSONResponse{
			Code: http.StatusUnauthorized,
			JSON: spec.MissingToken(err.Error()),
		}
	}
	var res api.QueryAccessTokenResponse
	err = d.UserAPI.QueryAccessToken(ctx, &api.QueryAccessTokenRequest{
		AccessToken:      token,
		AppServiceUserID: req.URL.Query().Get("user_id"),
	}, &res)
	if err != nil {
		util.GetLogger(ctx).WithError(err).Error("userAPI.QueryAccessToken failed")
		return nil, &util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}
	if res.Err != "" {
		if strings.HasPrefix(strings.ToLower(res.Err), "forbidden:") { // TODO: use actual error and no string comparison
			return nil, &util.JSONResponse{
				Code: http.StatusForbidden,
				JSON: spec.Forbidden(res.Err),
			}
		}
	}
	if res.Device == nil {
		return nil, &util.JSONResponse{
			Code: http.StatusUnauthorized,
			JSON: spec.UnknownToken("Unknown token"),
		}
	}
	return res.Device, nil
}
