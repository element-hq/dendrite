// Copyright 2024 New Vector Ltd.
// Copyright 2021 The Matrix.org Foundation C.I.C.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package routing

import (
	"context"
	"net/http"
	"time"

	"github.com/matrix-org/gomatrixserverlib/fclient"
	"github.com/sirupsen/logrus"

	"github.com/element-hq/dendrite/clientapi/auth"
	"github.com/element-hq/dendrite/clientapi/auth/authtypes"
	"github.com/element-hq/dendrite/clientapi/httputil"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
)

type crossSigningRequest struct {
	api.PerformUploadDeviceKeysRequest
	Auth newPasswordAuth `json:"auth"`
}

type UploadKeysAPI interface {
	QueryKeys(ctx context.Context, req *api.QueryKeysRequest, res *api.QueryKeysResponse)
	api.UploadDeviceKeysAPI
}

func UploadCrossSigningDeviceKeys(
	req *http.Request,
	keyserverAPI UploadKeysAPI, device *api.Device,
	accountAPI auth.GetAccountByPassword, cfg *config.ClientAPI,
) util.JSONResponse {
	uploadReq := &crossSigningRequest{}
	uploadRes := &api.PerformUploadDeviceKeysResponse{}

	resErr := httputil.UnmarshalJSONRequest(req, &uploadReq)
	if resErr != nil {
		return *resErr
	}

	// Query existing keys to determine if UIA is required
	keyResp := api.QueryKeysResponse{}
	keyserverAPI.QueryKeys(req.Context(), &api.QueryKeysRequest{
		UserID:        device.UserID,
		UserToDevices: map[string][]string{device.UserID: {device.ID}},
		Timeout:       time.Second * 10,
	}, &keyResp)

	if keyResp.Error != nil {
		logrus.WithError(keyResp.Error).Error("Failed to query keys")
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.Unknown(keyResp.Error.Error()),
		}
	}

	existingMasterKey, hasMasterKey := keyResp.MasterKeys[device.UserID]
	requireUIA := false
	if hasMasterKey {
		// If we have a master key, check if any of the existing keys differ. If they do,
		// we need to re-authenticate the user.
		requireUIA = keysDiffer(existingMasterKey, keyResp, uploadReq, device.UserID)
	}

	if requireUIA {
		sessionID := uploadReq.Auth.Session
		if sessionID == "" {
			sessionID = util.RandomString(sessionIDLength)
		}
		if uploadReq.Auth.Type != authtypes.LoginTypePassword {
			return util.JSONResponse{
				Code: http.StatusUnauthorized,
				JSON: newUserInteractiveResponse(
					sessionID,
					[]authtypes.Flow{
						{
							Stages: []authtypes.LoginType{authtypes.LoginTypePassword},
						},
					},
					nil,
				),
			}
		}
		typePassword := auth.LoginTypePassword{
			GetAccountByPassword: accountAPI,
			Config:               cfg,
		}
		if _, authErr := typePassword.Login(req.Context(), &uploadReq.Auth.PasswordRequest); authErr != nil {
			return *authErr
		}
		sessions.addCompletedSessionStage(sessionID, authtypes.LoginTypePassword)
	}

	uploadReq.UserID = device.UserID
	keyserverAPI.PerformUploadDeviceKeys(req.Context(), &uploadReq.PerformUploadDeviceKeysRequest, uploadRes)

	if err := uploadRes.Error; err != nil {
		switch {
		case err.IsInvalidSignature:
			return util.JSONResponse{
				Code: http.StatusBadRequest,
				JSON: spec.InvalidSignature(err.Error()),
			}
		case err.IsMissingParam:
			return util.JSONResponse{
				Code: http.StatusBadRequest,
				JSON: spec.MissingParam(err.Error()),
			}
		case err.IsInvalidParam:
			return util.JSONResponse{
				Code: http.StatusBadRequest,
				JSON: spec.InvalidParam(err.Error()),
			}
		default:
			return util.JSONResponse{
				Code: http.StatusBadRequest,
				JSON: spec.Unknown(err.Error()),
			}
		}
	}

	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: struct{}{},
	}
}

func keysDiffer(existingMasterKey fclient.CrossSigningKey, keyResp api.QueryKeysResponse, uploadReq *crossSigningRequest, userID string) bool {
	masterKeyEqual := existingMasterKey.Equal(&uploadReq.MasterKey)
	if !masterKeyEqual {
		return true
	}
	existingSelfSigningKey := keyResp.SelfSigningKeys[userID]
	selfSigningEqual := existingSelfSigningKey.Equal(&uploadReq.SelfSigningKey)
	if !selfSigningEqual {
		return true
	}
	existingUserSigningKey := keyResp.UserSigningKeys[userID]
	userSigningEqual := existingUserSigningKey.Equal(&uploadReq.UserSigningKey)
	return !userSigningEqual
}

func UploadCrossSigningDeviceSignatures(req *http.Request, keyserverAPI api.ClientKeyAPI, device *api.Device) util.JSONResponse {
	uploadReq := &api.PerformUploadDeviceSignaturesRequest{}
	uploadRes := &api.PerformUploadDeviceSignaturesResponse{}

	if err := httputil.UnmarshalJSONRequest(req, &uploadReq.Signatures); err != nil {
		return *err
	}

	uploadReq.UserID = device.UserID
	keyserverAPI.PerformUploadDeviceSignatures(req.Context(), uploadReq, uploadRes)

	if err := uploadRes.Error; err != nil {
		switch {
		case err.IsInvalidSignature:
			return util.JSONResponse{
				Code: http.StatusBadRequest,
				JSON: spec.InvalidSignature(err.Error()),
			}
		case err.IsMissingParam:
			return util.JSONResponse{
				Code: http.StatusBadRequest,
				JSON: spec.MissingParam(err.Error()),
			}
		case err.IsInvalidParam:
			return util.JSONResponse{
				Code: http.StatusBadRequest,
				JSON: spec.InvalidParam(err.Error()),
			}
		default:
			return util.JSONResponse{
				Code: http.StatusBadRequest,
				JSON: spec.Unknown(err.Error()),
			}
		}
	}

	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: struct{}{},
	}
}
