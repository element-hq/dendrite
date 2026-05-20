// Copyright 2024 New Vector Ltd.
// Copyright 2017 Vector Creations Ltd
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package httputil

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
)

// UnmarshalJSONRequest into the given interface pointer. Returns an error JSON response if
// there was a problem unmarshalling. Calling this function consumes the request body.
func UnmarshalJSONRequest(req *http.Request, iface interface{}) *util.JSONResponse {
	// encoding/json allows invalid utf-8, matrix does not
	// https://matrix.org/docs/spec/client_server/r0.6.1#api-standards
	body, err := io.ReadAll(req.Body)
	if err != nil {
		util.GetLogger(req.Context()).WithError(err).Error("io.ReadAll failed")
		return &util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: spec.InternalServerError{},
		}
	}

	return UnmarshalJSON(body, iface)
}

func UnmarshalJSON(body []byte, iface interface{}) *util.JSONResponse {
	if !utf8.Valid(body) {
		return &util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.NotJSON("Body contains invalid UTF-8"),
		}
	}

	if err := json.Unmarshal(body, iface); err != nil {
		// TODO: We may want to suppress the Error() return in production? It's useful when
		// debugging because an error will be produced for both invalid/malformed JSON AND
		// valid JSON with incorrect types for values.
		return &util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: spec.BadJSON("The request body could not be decoded into valid JSON. " + err.Error()),
		}
	}
	return nil
}

// MatrixErrorResponse converts a spec.MatrixError to a util.JSONResponse with the
// appropriate HTTP status code based on the error code. This helper prevents error
// codes from being lost when errors are wrapped or passed through handler chains.
//
// If the error is not a spec.MatrixError, it returns nil (caller should handle as internal error).
//
// HTTP status code mapping follows the Matrix spec:
//   - M_FORBIDDEN, M_UNABLE_TO_AUTHORISE_JOIN -> 403
//   - M_NOT_FOUND, M_UNRECOGNIZED -> 404
//   - All other Matrix errors -> 400 (bad request)
func MatrixErrorResponse(err error) *util.JSONResponse {
	var matrixErr spec.MatrixError
	if !errors.As(err, &matrixErr) {
		return nil
	}

	var httpCode int
	switch matrixErr.ErrCode {
	case spec.ErrorForbidden, spec.ErrorUnableToAuthoriseJoin:
		httpCode = http.StatusForbidden
	case spec.ErrorNotFound, spec.ErrorUnrecognized:
		httpCode = http.StatusNotFound
	default:
		// Most Matrix errors are client errors (bad request)
		httpCode = http.StatusBadRequest
	}

	return &util.JSONResponse{
		Code: httpCode,
		JSON: matrixErr,
	}
}
