package routing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/element-hq/dendrite/internal"
	"github.com/element-hq/dendrite/internal/eventutil"
	"github.com/gorilla/mux"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"golang.org/x/exp/constraints"

	appserviceAPI "github.com/element-hq/dendrite/appservice/api"
	clientapi "github.com/element-hq/dendrite/clientapi/api"
	"github.com/element-hq/dendrite/clientapi/auth/authtypes"
	clienthttputil "github.com/element-hq/dendrite/clientapi/httputil"
	"github.com/element-hq/dendrite/clientapi/userutil"
	"github.com/element-hq/dendrite/internal/httputil"
	roomserverAPI "github.com/element-hq/dendrite/roomserver/api"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/setup/jetstream"
	"github.com/element-hq/dendrite/userapi/api"
	userapi "github.com/element-hq/dendrite/userapi/api"
	"github.com/element-hq/dendrite/userapi/storage/shared"
)

var (
	validRegistrationTokenRegex = regexp.MustCompile("^[[:ascii:][:digit:]_]*$")
	deviceDisplayName           = "OIDC-native client"
)

func AdminCreateNewRegistrationToken(req *http.Request, cfg *config.ClientAPI, userAPI userapi.ClientUserAPI) util.JSONResponse {
	if !cfg.RegistrationRequiresToken {
		return util.JSONResponse{
			Code: http.StatusForbidden,
			JSON: spec.Forbidden("Registration via tokens is not enabled on this homeserver"),
		}
	}
	request := struct {
		Token       string `json:"token"`
		UsesAllowed *int32 `json:"uses_allowed,omitempty"`
		ExpiryTime  *int64 `json:"expiry_time,omitempty"`
		Length      int32  `json:"length"`
	}{}

	if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.BadJSON(fmt.Sprintf("Failed to decode request body: %s", err)),
		}
	}

	token := request.Token
	usesAllowed := request.UsesAllowed
	expiryTime := request.ExpiryTime
	length := request.Length

	if len(token) == 0 {
		if length == 0 {
			// length not provided in request. Assign default value of 16.
			length = 16
		}
		// token not present in request body. Hence, generate a random token.
		if length <= 0 || length > 64 {
			return util.JSONResponse{
				Code: http.StatusBadRequest,
				JSON: spec.BadJSON("length must be greater than zero and not greater than 64"),
			}
		}
		token = util.RandomString(int(length))
	}

	if len(token) > 64 {
		//Token present in request body, but is too long.
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.BadJSON("token must not be longer than 64"),
		}
	}

	isTokenValid := validRegistrationTokenRegex.Match([]byte(token))
	if !isTokenValid {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.BadJSON("token must consist only of characters matched by the regex [A-Za-z0-9-_]"),
		}
	}
	// At this point, we have a valid token, either through request body or through random generation.
	if usesAllowed != nil && *usesAllowed < 0 {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.BadJSON("uses_allowed must be a non-negative integer or null"),
		}
	}
	if expiryTime != nil && spec.Timestamp(*expiryTime).Time().Before(time.Now()) {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.BadJSON("expiry_time must not be in the past"),
		}
	}
	pending := int32(0)
	completed := int32(0)
	// If usesAllowed or expiryTime is 0, it means they are not present in the request. NULL (indicating unlimited uses / no expiration will be persisted in DB)
	registrationToken := &clientapi.RegistrationToken{
		Token:       &token,
		UsesAllowed: usesAllowed,
		Pending:     &pending,
		Completed:   &completed,
		ExpiryTime:  expiryTime,
	}
	created, err := userAPI.PerformAdminCreateRegistrationToken(req.Context(), registrationToken)
	if !created {
		return util.JSONResponse{
			Code: http.StatusConflict,
			JSON: map[string]string{
				"error": fmt.Sprintf("token: %s already exists", token),
			},
		}
	}
	if err != nil {
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: err,
		}
	}
	return util.JSONResponse{
		Code: 200,
		JSON: map[string]interface{}{
			"token":        token,
			"uses_allowed": getReturnValue(usesAllowed),
			"pending":      pending,
			"completed":    completed,
			"expiry_time":  getReturnValue(expiryTime),
		},
	}
}

func getReturnValue[t constraints.Integer](in *t) any {
	if in == nil {
		return nil
	}
	return *in
}

func AdminListRegistrationTokens(req *http.Request, cfg *config.ClientAPI, userAPI userapi.ClientUserAPI) util.JSONResponse {
	queryParams := req.URL.Query()
	returnAll := true
	valid := true
	validQuery, ok := queryParams["valid"]
	if ok {
		returnAll = false
		validValue, err := strconv.ParseBool(validQuery[0])
		if err != nil {
			return util.JSONResponse{
				Code: http.StatusBadRequest,
				JSON: spec.BadJSON("invalid 'valid' query parameter"),
			}
		}
		valid = validValue
	}
	tokens, err := userAPI.PerformAdminListRegistrationTokens(req.Context(), returnAll, valid)
	if err != nil {
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.ErrorUnknown,
		}
	}
	return util.JSONResponse{
		Code: 200,
		JSON: map[string]interface{}{
			"registration_tokens": tokens,
		},
	}
}

func AdminGetRegistrationToken(req *http.Request, cfg *config.ClientAPI, userAPI userapi.ClientUserAPI) util.JSONResponse {
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.ErrorResponse(err)
	}
	tokenText := vars["token"]
	token, err := userAPI.PerformAdminGetRegistrationToken(req.Context(), tokenText)
	if err != nil {
		return util.JSONResponse{
			Code: http.StatusNotFound,
			JSON: spec.NotFound(fmt.Sprintf("token: %s not found", tokenText)),
		}
	}
	return util.JSONResponse{
		Code: 200,
		JSON: token,
	}
}

func AdminDeleteRegistrationToken(req *http.Request, cfg *config.ClientAPI, userAPI userapi.ClientUserAPI) util.JSONResponse {
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.ErrorResponse(err)
	}
	tokenText := vars["token"]
	err = userAPI.PerformAdminDeleteRegistrationToken(req.Context(), tokenText)
	if err != nil {
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: err,
		}
	}
	return util.JSONResponse{
		Code: 200,
		JSON: map[string]interface{}{},
	}
}

func AdminUpdateRegistrationToken(req *http.Request, cfg *config.ClientAPI, userAPI userapi.ClientUserAPI) util.JSONResponse {
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.ErrorResponse(err)
	}
	tokenText := vars["token"]
	request := make(map[string]*int64)
	if err = json.NewDecoder(req.Body).Decode(&request); err != nil {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.BadJSON(fmt.Sprintf("Failed to decode request body: %s", err)),
		}
	}
	newAttributes := make(map[string]interface{})
	usesAllowed, ok := request["uses_allowed"]
	if ok {
		// Only add usesAllowed to newAtrributes if it is present and valid
		if usesAllowed != nil && *usesAllowed < 0 {
			return util.JSONResponse{
				Code: http.StatusBadRequest,
				JSON: spec.BadJSON("uses_allowed must be a non-negative integer or null"),
			}
		}
		newAttributes["usesAllowed"] = usesAllowed
	}
	expiryTime, ok := request["expiry_time"]
	if ok {
		// Only add expiryTime to newAtrributes if it is present and valid
		if expiryTime != nil && spec.Timestamp(*expiryTime).Time().Before(time.Now()) {
			return util.JSONResponse{
				Code: http.StatusBadRequest,
				JSON: spec.BadJSON("expiry_time must not be in the past"),
			}
		}
		newAttributes["expiryTime"] = expiryTime
	}
	if len(newAttributes) == 0 {
		// No attributes to update. Return existing token
		return AdminGetRegistrationToken(req, cfg, userAPI)
	}
	updatedToken, err := userAPI.PerformAdminUpdateRegistrationToken(req.Context(), tokenText, newAttributes)
	if err != nil {
		return util.JSONResponse{
			Code: http.StatusNotFound,
			JSON: spec.NotFound(fmt.Sprintf("token: %s not found", tokenText)),
		}
	}
	return util.JSONResponse{
		Code: 200,
		JSON: *updatedToken,
	}
}

func AdminEvacuateRoom(req *http.Request, rsAPI roomserverAPI.ClientRoomserverAPI) util.JSONResponse {
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.ErrorResponse(err)
	}

	affected, err := rsAPI.PerformAdminEvacuateRoom(req.Context(), vars["roomID"])
	switch err.(type) {
	case nil:
	case eventutil.ErrRoomNoExists:
		return util.JSONResponse{
			Code: http.StatusNotFound,
			JSON: spec.NotFound(err.Error()),
		}
	default:
		logrus.WithError(err).WithField("roomID", vars["roomID"]).Error("Failed to evacuate room")
		return util.ErrorResponse(err)
	}
	return util.JSONResponse{
		Code: 200,
		JSON: map[string]interface{}{
			"affected": affected,
		},
	}
}

func AdminEvacuateUser(req *http.Request, rsAPI roomserverAPI.ClientRoomserverAPI) util.JSONResponse {
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.ErrorResponse(err)
	}

	affected, err := rsAPI.PerformAdminEvacuateUser(req.Context(), vars["userID"])
	if err != nil {
		logrus.WithError(err).WithField("userID", vars["userID"]).Error("Failed to evacuate user")
		return util.MessageResponse(http.StatusBadRequest, err.Error())
	}

	return util.JSONResponse{
		Code: 200,
		JSON: map[string]interface{}{
			"affected": affected,
		},
	}
}

func AdminPurgeRoom(req *http.Request, rsAPI roomserverAPI.ClientRoomserverAPI) util.JSONResponse {
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.ErrorResponse(err)
	}

	if err = rsAPI.PerformAdminPurgeRoom(context.Background(), vars["roomID"]); err != nil {
		return util.ErrorResponse(err)
	}

	return util.JSONResponse{
		Code: 200,
		JSON: struct{}{},
	}
}

func AdminResetPassword(req *http.Request, cfg *config.ClientAPI, device *api.Device, userAPI userapi.ClientUserAPI) util.JSONResponse {
	if req.Body == nil {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.Unknown("Missing request body"),
		}
	}
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.ErrorResponse(err)
	}
	var localpart string
	userID := vars["userID"]
	localpart, serverName, err := cfg.Matrix.SplitLocalID('@', userID)
	if err != nil {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.InvalidParam(err.Error()),
		}
	}
	accAvailableResp := &api.QueryAccountAvailabilityResponse{}
	if err = userAPI.QueryAccountAvailability(req.Context(), &api.QueryAccountAvailabilityRequest{
		Localpart:  localpart,
		ServerName: serverName,
	}, accAvailableResp); err != nil {
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}
	if accAvailableResp.Available {
		return util.JSONResponse{
			Code: http.StatusNotFound,
			JSON: spec.Unknown("User does not exist"),
		}
	}
	request := struct {
		Password      string `json:"password"`
		LogoutDevices bool   `json:"logout_devices"`
	}{}
	if err = json.NewDecoder(req.Body).Decode(&request); err != nil {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.Unknown("Failed to decode request body: " + err.Error()),
		}
	}
	if request.Password == "" {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.MissingParam("Expecting non-empty password."),
		}
	}

	if err = internal.ValidatePassword(request.Password); err != nil {
		return *internal.PasswordResponse(err)
	}

	updateReq := &api.PerformPasswordUpdateRequest{
		Localpart:     localpart,
		ServerName:    serverName,
		Password:      request.Password,
		LogoutDevices: request.LogoutDevices,
	}
	updateRes := &api.PerformPasswordUpdateResponse{}
	if err := userAPI.PerformPasswordUpdate(req.Context(), updateReq, updateRes); err != nil {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.Unknown("Failed to perform password update: " + err.Error()),
		}
	}
	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: struct {
			Updated bool `json:"password_updated"`
		}{
			Updated: updateRes.PasswordUpdated,
		},
	}
}

func AdminReindex(req *http.Request, cfg *config.ClientAPI, device *api.Device, natsClient *nats.Conn) util.JSONResponse {
	_, err := natsClient.RequestMsg(nats.NewMsg(cfg.Matrix.JetStream.Prefixed(jetstream.InputFulltextReindex)), time.Second*10)
	if err != nil {
		logrus.WithError(err).Error("failed to publish nats message")
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

func AdminMarkAsStale(req *http.Request, cfg *config.ClientAPI, keyAPI userapi.ClientKeyAPI) util.JSONResponse {
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.ErrorResponse(err)
	}
	userID := vars["userID"]

	_, domain, err := gomatrixserverlib.SplitID('@', userID)
	if err != nil {
		return util.MessageResponse(http.StatusBadRequest, err.Error())
	}
	if cfg.Matrix.IsLocalServerName(domain) {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.InvalidParam("Can not mark local device list as stale"),
		}
	}

	err = keyAPI.PerformMarkAsStaleIfNeeded(req.Context(), &api.PerformMarkAsStaleRequest{
		UserID: userID,
		Domain: domain,
	}, &struct{}{})
	if err != nil {
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.Unknown(fmt.Sprintf("Failed to mark device list as stale: %s", err)),
		}
	}
	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: struct{}{},
	}
}

func AdminDownloadState(req *http.Request, device *api.Device, rsAPI roomserverAPI.ClientRoomserverAPI) util.JSONResponse {
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.ErrorResponse(err)
	}
	roomID, ok := vars["roomID"]
	if !ok {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.MissingParam("Expecting room ID."),
		}
	}
	serverName, ok := vars["serverName"]
	if !ok {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.MissingParam("Expecting remote server name."),
		}
	}
	if err = rsAPI.PerformAdminDownloadState(req.Context(), roomID, device.UserID, spec.ServerName(serverName)); err != nil {
		if errors.Is(err, eventutil.ErrRoomNoExists{}) {
			return util.JSONResponse{
				Code: 200,
				JSON: spec.NotFound(err.Error()),
			}
		}
		logrus.WithError(err).WithFields(logrus.Fields{
			"userID":     device.UserID,
			"serverName": serverName,
			"roomID":     roomID,
		}).Error("failed to download state")
		return util.ErrorResponse(err)
	}
	return util.JSONResponse{
		Code: 200,
		JSON: struct{}{},
	}
}

func AdminCheckUsernameAvailable(
	req *http.Request,
	userAPI userapi.ClientUserAPI,
	cfg *config.ClientAPI,
) util.JSONResponse {
	username := req.URL.Query().Get("username")
	if username == "" {
		return util.MessageResponse(http.StatusBadRequest, "Query parameter 'username' is missing or empty")
	}
	rq := userapi.QueryAccountAvailabilityRequest{Localpart: username, ServerName: cfg.Matrix.ServerName}
	rs := userapi.QueryAccountAvailabilityResponse{}
	if err := userAPI.QueryAccountAvailability(req.Context(), &rq, &rs); err != nil {
		return util.ErrorResponse(err)
	}

	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: map[string]bool{"available": rs.Available},
	}
}

func AdminUserDeviceRetrieveCreate(
	req *http.Request,
	userAPI userapi.ClientUserAPI,
	cfg *config.ClientAPI,
) util.JSONResponse {
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.MessageResponse(http.StatusBadRequest, err.Error())
	}
	userID := vars["userID"]
	local, domain, err := userutil.ParseUsernameParam(userID, cfg.Matrix)
	if err != nil {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.InvalidParam(err.Error()),
		}
	}
	logger := util.GetLogger(req.Context())

	switch req.Method {
	case http.MethodPost:
		var payload struct {
			DeviceID string `json:"device_id"`
		}
		if resErr := clienthttputil.UnmarshalJSONRequest(req, &payload); resErr != nil {
			return *resErr
		}

		userDeviceExists := false
		{
			var rs api.QueryDevicesResponse
			if err = userAPI.QueryDevices(req.Context(), &api.QueryDevicesRequest{UserID: userID}, &rs); err != nil {
				logger.WithError(err).Error("QueryDevices")
				return util.JSONResponse{
					Code: http.StatusInternalServerError,
					JSON: spec.InternalServerError{},
				}
			}
			if !rs.UserExists {
				return util.JSONResponse{
					Code: http.StatusNotFound,
					JSON: spec.NotFound("Given user ID does not exist"),
				}
			}
			for i := range rs.Devices {
				if d := rs.Devices[i]; d.ID == payload.DeviceID && d.UserID == userID {
					userDeviceExists = true
					break
				}
			}
		}

		if !userDeviceExists {
			var rs userapi.PerformDeviceCreationResponse
			if err = userAPI.PerformDeviceCreation(req.Context(), &userapi.PerformDeviceCreationRequest{
				Localpart:          local,
				ServerName:         domain,
				DeviceID:           &payload.DeviceID,
				DeviceDisplayName:  &deviceDisplayName,
				IPAddr:             "",
				UserAgent:          req.UserAgent(),
				NoDeviceListUpdate: false,
				FromRegistration:   false,
			}, &rs); err != nil {
				logger.WithError(err).Error("PerformDeviceCreation")
				return util.JSONResponse{
					Code: http.StatusInternalServerError,
					JSON: spec.InternalServerError{},
				}
			}
			logger.WithError(err).Debug("PerformDeviceCreation succeeded")
		}
		return util.JSONResponse{
			Code: http.StatusCreated,
			JSON: struct{}{},
		}
	case http.MethodGet:
		var res userapi.QueryDevicesResponse
		if err := userAPI.QueryDevices(req.Context(), &userapi.QueryDevicesRequest{UserID: userID}, &res); err != nil {
			return util.MessageResponse(http.StatusBadRequest, err.Error())
		}

		jsonDevices := make([]deviceJSON, 0, len(res.Devices))
		for i := range res.Devices {
			d := &res.Devices[i]
			jsonDevices = append(jsonDevices, deviceJSON{
				DeviceID:    d.ID,
				DisplayName: d.DisplayName,
				LastSeenIP:  d.LastSeenIP,
				LastSeenTS:  d.LastSeenTS,
			})
		}

		return util.JSONResponse{
			Code: http.StatusOK,
			JSON: struct {
				Devices []deviceJSON `json:"devices"`
				Total   int          `json:"total"`
			}{
				Devices: jsonDevices,
				Total:   len(res.Devices),
			},
		}
	default:
		return util.JSONResponse{
			Code: http.StatusMethodNotAllowed,
			JSON: struct{}{},
		}
	}
}

func AdminUserDeviceDelete(
	req *http.Request,
	userAPI userapi.ClientUserAPI,
	cfg *config.ClientAPI,
) util.JSONResponse {
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.MessageResponse(http.StatusBadRequest, err.Error())
	}
	userID := vars["userID"]
	deviceID := vars["deviceID"]
	logger := util.GetLogger(req.Context())

	// XXX: we probably have to delete session from the sessions dict
	// like we do in DeleteDeviceById. If so, we have to fi
	var device *api.Device
	{
		var rs api.QueryDevicesResponse
		if err := userAPI.QueryDevices(req.Context(), &api.QueryDevicesRequest{UserID: userID}, &rs); err != nil {
			logger.WithError(err).Error("userAPI.QueryDevices failed")
			return util.JSONResponse{
				Code: http.StatusInternalServerError,
				JSON: spec.InternalServerError{},
			}
		}
		if !rs.UserExists {
			return util.JSONResponse{
				Code: http.StatusNotFound,
				JSON: spec.NotFound("Given user ID does not exist"),
			}
		}
		for i := range rs.Devices {
			if d := rs.Devices[i]; d.ID == deviceID && d.UserID == userID {
				device = &d
				break
			}
		}
	}

	if device != nil {
		// XXX: this response struct can completely removed everywhere as it doesn't
		// have any functional purpose
		var res api.PerformDeviceDeletionResponse
		if err := userAPI.PerformDeviceDeletion(req.Context(), &api.PerformDeviceDeletionRequest{
			UserID:    device.UserID,
			DeviceIDs: []string{device.ID},
		}, &res); err != nil {
			logger.WithError(err).Error("userAPI.PerformDeviceDeletion failed")
			return util.JSONResponse{
				Code: http.StatusInternalServerError,
				JSON: spec.InternalServerError{},
			}
		}
	}

	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: struct{}{},
	}
}

func AdminUserDevicesDelete(
	req *http.Request,
	userAPI userapi.ClientUserAPI,
	cfg *config.ClientAPI,
) util.JSONResponse {
	logger := util.GetLogger(req.Context())
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.MessageResponse(http.StatusBadRequest, err.Error())
	}
	userID := vars["userID"]

	if req.Body == nil {
		return util.MessageResponse(http.StatusBadRequest, "body is required")
	}
	var payload struct {
		Devices []string `json:"devices"`
	}

	if err = json.NewDecoder(req.Body).Decode(&payload); err != nil {
		logger.WithError(err).Error("unable to decode device deletion request")
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}
	defer req.Body.Close() // nolint: errcheck

	{
		// XXX: this response struct can completely removed everywhere as it doesn't
		// have any functional purpose
		var rs api.PerformDeviceDeletionResponse
		if err := userAPI.PerformDeviceDeletion(req.Context(), &api.PerformDeviceDeletionRequest{
			UserID:    userID,
			DeviceIDs: payload.Devices,
		}, &rs); err != nil {
			logger.WithError(err).Error("userAPI.PerformDeviceDeletion failed")
			return util.JSONResponse{
				Code: http.StatusInternalServerError,
				JSON: spec.InternalServerError{},
			}
		}
	}

	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: struct{}{},
	}
}

func AdminDeactivateAccount(
	req *http.Request,
	userAPI userapi.ClientUserAPI,
	cfg *config.ClientAPI,
) util.JSONResponse {
	logger := util.GetLogger(req.Context())
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.MessageResponse(http.StatusBadRequest, err.Error())
	}
	userID := vars["userID"]
	local, domain, err := userutil.ParseUsernameParam(userID, cfg.Matrix)
	if err != nil {
		return util.MessageResponse(http.StatusBadRequest, err.Error())
	}

	// TODO: "erase" field must also be processed here
	// see https://github.com/element-hq/synapse/blob/develop/docs/admin_api/user_admin_api.md#deactivate-account

	var rs api.PerformAccountDeactivationResponse
	if err := userAPI.PerformAccountDeactivation(req.Context(), &api.PerformAccountDeactivationRequest{
		Localpart: local, ServerName: domain,
	}, &rs); err != nil {
		logger.WithError(err).Error("userAPI.PerformDeviceDeletion failed")
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

func AdminAllowCrossSigningReplacementWithoutUIA(
	req *http.Request,
	userAPI userapi.ClientUserAPI,
) util.JSONResponse {
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.MessageResponse(http.StatusBadRequest, err.Error())
	}
	userIDstr, ok := vars["userID"]
	userID, err := spec.NewUserID(userIDstr, false)
	if !ok || err != nil {
		return util.JSONResponse{
			Code: http.StatusNotFound,
			JSON: spec.MissingParam("User not found."),
		}
	}

	var rs api.QueryAccountByLocalpartResponse
	err = userAPI.QueryAccountByLocalpart(req.Context(), &api.QueryAccountByLocalpartRequest{
		Localpart:  userID.Local(),
		ServerName: userID.Domain(),
	}, &rs)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		util.GetLogger(req.Context()).WithError(err).Error("userAPI.QueryAccountByLocalpart")
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.Unknown(err.Error()),
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		return util.JSONResponse{
			Code: http.StatusNotFound,
			JSON: spec.NotFound("User not found."),
		}
	}
	switch req.Method {
	case http.MethodPost:
		ts := sessions.allowCrossSigningKeysReplacement(userID.String())
		return util.JSONResponse{
			Code: http.StatusOK,
			JSON: map[string]int64{"updatable_without_uia_before_ms": ts},
		}
	default:
		return util.JSONResponse{
			Code: http.StatusMethodNotAllowed,
			JSON: spec.Unknown("Method not allowed."),
		}
	}

}

type adminCreateOrModifyAccountRequest struct {
	DisplayName string `json:"displayname"`
	AvatarURL   string `json:"avatar_url"`
	ThreePIDs   []struct {
		Medium  string `json:"medium"`
		Address string `json:"address"`
	} `json:"threepids"`
	// TODO: the following fields are not used by dendrite, but they are used in Synapse.
	// Password      string            `json:"password"`
	// LogoutDevices bool              `json:"logout_devices"`
	// ExternalIDs   []struct{
	// 	AuthProvider string `json:"auth_provider"`
	// 	ExternalID   string `json:"external_id"`
	// } `json:"external_ids"`
	// Admin         bool              `json:"admin"`
	// Deactivated   bool              `json:"deactivated"`
	// Locked        bool              `json:"locked"`
}

func AdminCreateOrModifyAccount(req *http.Request, userAPI userapi.ClientUserAPI, cfg *config.ClientAPI) util.JSONResponse {
	logger := util.GetLogger(req.Context())
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.MessageResponse(http.StatusBadRequest, err.Error())
	}
	userID := vars["userID"]
	local, domain, err := userutil.ParseUsernameParam(userID, cfg.Matrix)
	if err != nil {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.InvalidParam(userID),
		}
	}
	var r adminCreateOrModifyAccountRequest
	if resErr := clienthttputil.UnmarshalJSONRequest(req, &r); resErr != nil {
		logger.Debugf("UnmarshalJSONRequest failed: %+v", *resErr)
		return *resErr
	}
	logger.Debugf("adminCreateOrModifyAccountRequest is: %#v", r)
	statusCode := http.StatusOK

	// TODO: Ideally, the following commands should be executed in one transaction.
	// can we propagate the tx object and pass it in context?
	var res userapi.PerformAccountCreationResponse
	err = userAPI.PerformAccountCreation(req.Context(), &userapi.PerformAccountCreationRequest{
		AccountType: userapi.AccountTypeUser,
		Localpart:   local,
		ServerName:  domain,
		OnConflict:  api.ConflictUpdate,
		AvatarURL:   r.AvatarURL,
		DisplayName: r.DisplayName,
	}, &res)
	if err != nil {
		logger.WithError(err).Error("userAPI.PerformAccountCreation")
		return util.MessageResponse(http.StatusBadRequest, err.Error())
	}
	if res.AccountCreated {
		statusCode = http.StatusCreated
	}

	if l := len(r.ThreePIDs); l > 0 {
		logger.Debugf("Trying to bulk save 3PID associations: %+v", r.ThreePIDs)
		threePIDs := make([]authtypes.ThreePID, 0, len(r.ThreePIDs))
		for i := range r.ThreePIDs {
			tpid := &r.ThreePIDs[i]
			threePIDs = append(threePIDs, authtypes.ThreePID{Medium: tpid.Medium, Address: tpid.Address})
		}
		err = userAPI.PerformBulkSaveThreePIDAssociation(req.Context(), &userapi.PerformBulkSaveThreePIDAssociationRequest{
			ThreePIDs:  threePIDs,
			Localpart:  local,
			ServerName: domain,
		}, &struct{}{})
		if err == shared.Err3PIDInUse {
			return util.MessageResponse(http.StatusBadRequest, err.Error())
		} else if err != nil {
			logger.WithError(err).Error("userAPI.PerformSaveThreePIDAssociation")
			return util.ErrorResponse(err)
		}
	}

	return util.JSONResponse{
		Code: statusCode,
		JSON: nil,
	}
}

func AdminRetrieveAccount(req *http.Request, cfg *config.ClientAPI, userAPI userapi.ClientUserAPI) util.JSONResponse {
	logger := util.GetLogger(req.Context())
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.MessageResponse(http.StatusBadRequest, err.Error())
	}
	userID, ok := vars["userID"]
	if !ok {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.MissingParam("Expecting user ID."),
		}
	}
	local, domain, err := userutil.ParseUsernameParam(userID, cfg.Matrix)
	if err != nil {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.InvalidParam(err.Error()),
		}
	}

	body := struct {
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		Deactivated bool   `json:"deactivated"`
	}{}

	var rs api.QueryAccountByLocalpartResponse
	err = userAPI.QueryAccountByLocalpart(req.Context(), &api.QueryAccountByLocalpartRequest{Localpart: local, ServerName: domain}, &rs)
	if err == sql.ErrNoRows {
		return util.JSONResponse{
			Code: http.StatusNotFound,
			JSON: spec.NotFound(fmt.Sprintf("User '%s' not found", userID)),
		}
	} else if err != nil {
		logger.WithError(err).Error("userAPI.QueryAccountByLocalpart")
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.Unknown(err.Error()),
		}
	}
	body.Deactivated = rs.Account.Deactivated

	profile, err := userAPI.QueryProfile(req.Context(), userID)
	if err != nil {
		if err == appserviceAPI.ErrProfileNotExists {
			return util.JSONResponse{
				Code: http.StatusNotFound,
				JSON: spec.NotFound(err.Error()),
			}
		}
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.Unknown(err.Error()),
		}
	}
	body.AvatarURL = profile.AvatarURL
	body.DisplayName = profile.DisplayName

	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: body,
	}
}

// GetEventReports returns reported events for a given user/room.
func GetEventReports(
	req *http.Request,
	rsAPI roomserverAPI.ClientRoomserverAPI,
	from, limit uint64,
	backwards bool,
	userID, roomID string,
) util.JSONResponse {

	eventReports, count, err := rsAPI.QueryAdminEventReports(req.Context(), from, limit, backwards, userID, roomID)
	if err != nil {
		logrus.WithError(err).Error("failed to query event reports")
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}

	resp := map[string]any{
		"event_reports": eventReports,
		"total":         count,
	}

	// Add a next_token if there are still reports
	if int64(from+limit) < count {
		resp["next_token"] = int(from) + len(eventReports)
	}

	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: resp,
	}
}

func GetEventReport(req *http.Request, rsAPI roomserverAPI.ClientRoomserverAPI, reportID string) util.JSONResponse {
	parsedReportID, err := strconv.ParseUint(reportID, 10, 64)
	if err != nil {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			// Given this is an admin endpoint, let them know what didn't work.
			JSON: spec.InvalidParam(err.Error()),
		}
	}

	report, err := rsAPI.QueryAdminEventReport(req.Context(), parsedReportID)
	if err != nil {
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.Unknown(err.Error()),
		}
	}

	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: report,
	}
}

func DeleteEventReport(req *http.Request, rsAPI roomserverAPI.ClientRoomserverAPI, reportID string) util.JSONResponse {
	parsedReportID, err := strconv.ParseUint(reportID, 10, 64)
	if err != nil {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			// Given this is an admin endpoint, let them know what didn't work.
			JSON: spec.InvalidParam(err.Error()),
		}
	}

	err = rsAPI.PerformAdminDeleteEventReport(req.Context(), parsedReportID)
	if err != nil {
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.Unknown(err.Error()),
		}
	}

	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: struct{}{},
	}
}

func parseUint64OrDefault(input string, defaultValue uint64) uint64 {
	v, err := strconv.ParseUint(input, 10, 64)
	if err != nil {
		return defaultValue
	}
	return v
}
