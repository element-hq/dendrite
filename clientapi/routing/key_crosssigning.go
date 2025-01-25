// Copyright 2024 New Vector Ltd.
// Copyright 2021 The Matrix.org Foundation C.I.C.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package routing

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/element-hq/dendrite/clientapi/auth"
	"github.com/element-hq/dendrite/clientapi/auth/authtypes"
	"github.com/element-hq/dendrite/clientapi/httputil"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib/fclient"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
)

const CrossSigningResetStage = "org.matrix.cross_signing_reset"

type crossSigningRequest struct {
	api.PerformUploadDeviceKeysRequest
	Auth newPasswordAuth `json:"auth"`
}

type UploadKeysAPI interface {
	QueryKeys(ctx context.Context, req *api.QueryKeysRequest, res *api.QueryKeysResponse)
	QueryMasterKeys(ctx context.Context, req *api.QueryMasterKeysRequest, res *api.QueryMasterKeysResponse)
	api.UploadDeviceKeysAPI
}

func UploadCrossSigningDeviceKeys(
	req *http.Request,
	keyserverAPI UploadKeysAPI, device *api.Device,
	accountAPI auth.GetAccountByPassword, cfg *config.ClientAPI,
) util.JSONResponse {
	logger := util.GetLogger(req.Context())
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
		logger.WithError(keyResp.Error).Error("Failed to query keys")
		return convertKeyError(keyResp.Error)
	}

	existingMasterKey, hasMasterKey := keyResp.MasterKeys[device.UserID]

	if hasMasterKey {
		if !keysDiffer(existingMasterKey, keyResp, uploadReq, device.UserID) {
			// If we have a master key, check if any of the existing keys differ. If they don't
			// we return 200 as keys are still valid and there's nothing to do.
			return util.JSONResponse{
				Code: http.StatusOK,
				JSON: struct{}{},
			}
		}

		// With MSC3861, UIA is not possible. Instead, the auth service has to explicitly mark the master key as replaceable.
		if cfg.MSCs.MSC3861Enabled() {
			masterKeyResp := api.QueryMasterKeysResponse{}
			keyserverAPI.QueryMasterKeys(req.Context(), &api.QueryMasterKeysRequest{UserID: device.UserID}, &masterKeyResp)

			if masterKeyResp.Error != nil {
				logger.WithError(masterKeyResp.Error).Error("Failed to query master key")
				return convertKeyError(masterKeyResp.Error)
			}

			requireUIA := !sessions.isCrossSigningKeysReplacementAllowed(device.UserID) && masterKeyResp.Key != nil
			if requireUIA {
				url := ""
				if m := cfg.MSCs.MSC3861; m.AccountManagementURL != "" {
					url = strings.Join([]string{m.AccountManagementURL, "?action=", CrossSigningResetStage}, "")
				} else {
					url = m.Issuer
				}
				return util.JSONResponse{
					Code: http.StatusUnauthorized,
					JSON: newUserInteractiveResponse(
						"dummy",
						[]authtypes.Flow{
							{
								Stages: []authtypes.LoginType{CrossSigningResetStage},
							},
						},
						map[string]interface{}{
							CrossSigningResetStage: map[string]string{
								"url": url,
							},
						},
						strings.Join([]string{
							"To reset your end-to-end encryption cross-signing identity, you first need to approve it at",
							url,
							"and then try again.",
						}, " "),
					),
				}
			}
			sessions.restrictCrossSigningKeysReplacement(device.UserID)
		} else {
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
						"",
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
	}

	uploadReq.UserID = device.UserID
	keyserverAPI.PerformUploadDeviceKeys(req.Context(), &uploadReq.PerformUploadDeviceKeysRequest, uploadRes)

	if err := uploadRes.Error; err != nil {
		return convertKeyError(err)
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
		return convertKeyError(err)
	}

	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: struct{}{},
	}
}

func convertKeyError(err *api.KeyError) util.JSONResponse {
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
