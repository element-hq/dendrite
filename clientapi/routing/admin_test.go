package routing_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/element-hq/dendrite/clientapi/routing"
	"github.com/element-hq/dendrite/setup/config"
	userapi "github.com/element-hq/dendrite/userapi/api"
)

func makeTestConfig() *config.ClientAPI {
	cfg := &config.ClientAPI{
		Matrix: &config.Global{},
	}
	cfg.Matrix.ServerName = "test"
	return cfg
}

type mockUserAPI struct {
	userapi.ClientUserAPI
	deactivationCalls []userapi.PerformUserDeactivationRequest
	deactivationErr   error
}

func (m *mockUserAPI) PerformUserDeactivation(ctx context.Context, req *userapi.PerformUserDeactivationRequest, res *userapi.PerformUserDeactivationResponse) error {
	m.deactivationCalls = append(m.deactivationCalls, *req)
	if m.deactivationErr != nil {
		return m.deactivationErr
	}
	res.UserID = req.UserID
	res.Deactivated = true
	res.TokensRevoked = 3
	res.RoomsLeft = 5
	res.RedactionQueued = req.RedactMessages
	res.RedactionJobID = 123
	return nil
}

func TestAdminDeactivateUser_Success(t *testing.T) {
	mock := &mockUserAPI{}
	cfg := makeTestConfig()
	device := &userapi.Device{
		UserID: "@admin:test",
	}

	reqBody := map[string]interface{}{
		"leave_rooms":     true,
		"redact_messages": true,
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/_dendrite/admin/v1/deactivate/@alice:test", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"userID": "@alice:test"})

	w := httptest.NewRecorder()
	resp := routing.AdminDeactivateUser(req, cfg, device, mock)
	w.WriteHeader(resp.Code); json.NewEncoder(w).Encode(resp.JSON)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "@alice:test", response["user_id"])
	assert.Equal(t, true, response["deactivated"])
	assert.Equal(t, float64(3), response["tokens_revoked"])
	assert.Equal(t, float64(5), response["rooms_left"])

	// Verify redaction fields are present (GDPR compliance)
	assert.Contains(t, response, "redaction_queued", "response missing redaction_queued field")
	assert.Contains(t, response, "redaction_job_id", "response missing redaction_job_id field")
	assert.Equal(t, true, response["redaction_queued"], "redaction should be queued")
	assert.Equal(t, float64(123), response["redaction_job_id"], "redaction job ID should match mock")

	// Verify the userAPI was called correctly
	require.Len(t, mock.deactivationCalls, 1)
	call := mock.deactivationCalls[0]
	assert.Equal(t, "@alice:test", call.UserID)
	assert.Equal(t, "@admin:test", call.RequestedBy, "RequestedBy should be admin user ID")
	assert.True(t, call.LeaveRooms)
	assert.True(t, call.RedactMessages)
}

func TestAdminDeactivateUser_OptionsOff(t *testing.T) {
	mock := &mockUserAPI{}
	cfg := makeTestConfig()

	reqBody := map[string]interface{}{
		"leave_rooms":     false,
		"redact_messages": false,
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/_dendrite/admin/v1/deactivate/@bob:test", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"userID": "@bob:test"})

	w := httptest.NewRecorder()
	resp := routing.AdminDeactivateUser(req, cfg, &userapi.Device{UserID: "@admin:test"}, mock)
	w.WriteHeader(resp.Code); json.NewEncoder(w).Encode(resp.JSON)

	assert.Equal(t, http.StatusOK, w.Code)

	require.Len(t, mock.deactivationCalls, 1)
	call := mock.deactivationCalls[0]
	assert.Equal(t, "@bob:test", call.UserID)
	assert.False(t, call.LeaveRooms)
	assert.False(t, call.RedactMessages)
}

func TestAdminDeactivateUser_InvalidUserID(t *testing.T) {
	mock := &mockUserAPI{}
	cfg := makeTestConfig()

	reqBody := map[string]interface{}{
		"leave_rooms": true,
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/_dendrite/admin/v1/deactivate/invalid-user-id", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"userID": "invalid-user-id"})

	w := httptest.NewRecorder()
	resp := routing.AdminDeactivateUser(req, cfg, &userapi.Device{UserID: "@admin:test"}, mock)
	w.WriteHeader(resp.Code); json.NewEncoder(w).Encode(resp.JSON)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response["error"], "user ID")
}

func TestAdminDeactivateUser_MissingUserID(t *testing.T) {
	mock := &mockUserAPI{}
	cfg := makeTestConfig()

	reqBody := map[string]interface{}{
		"leave_rooms": true,
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/_dendrite/admin/v1/deactivate/", bytes.NewReader(body))
	// No URL vars set - simulating missing userID

	w := httptest.NewRecorder()
	resp := routing.AdminDeactivateUser(req, cfg, &userapi.Device{UserID: "@admin:test"}, mock)
	w.WriteHeader(resp.Code); json.NewEncoder(w).Encode(resp.JSON)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAdminDeactivateUser_InvalidRequestBody(t *testing.T) {
	mock := &mockUserAPI{}
	cfg := makeTestConfig()

	req := httptest.NewRequest(http.MethodPost, "/_dendrite/admin/v1/deactivate/@alice:test", bytes.NewReader([]byte("invalid json")))
	req = mux.SetURLVars(req, map[string]string{"userID": "@alice:test"})

	w := httptest.NewRecorder()
	resp := routing.AdminDeactivateUser(req, cfg, &userapi.Device{UserID: "@admin:test"}, mock)
	w.WriteHeader(resp.Code); json.NewEncoder(w).Encode(resp.JSON)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAdminDeactivateUser_DeactivationError(t *testing.T) {
	mock := &mockUserAPI{
		deactivationErr: fmt.Errorf("database connection failed"),
	}
	cfg := makeTestConfig()

	reqBody := map[string]interface{}{
		"leave_rooms": true,
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/_dendrite/admin/v1/deactivate/@alice:test", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"userID": "@alice:test"})

	w := httptest.NewRecorder()
	resp := routing.AdminDeactivateUser(req, cfg, &userapi.Device{UserID: "@admin:test"}, mock)
	w.WriteHeader(resp.Code); json.NewEncoder(w).Encode(resp.JSON)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAdminDeactivateUser_DefaultValues(t *testing.T) {
	mock := &mockUserAPI{}
	cfg := makeTestConfig()

	// Empty body - should use default values (false for both)
	reqBody := map[string]interface{}{}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/_dendrite/admin/v1/deactivate/@carol:test", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"userID": "@carol:test"})

	w := httptest.NewRecorder()
	resp := routing.AdminDeactivateUser(req, cfg, &userapi.Device{UserID: "@admin:test"}, mock)
	w.WriteHeader(resp.Code); json.NewEncoder(w).Encode(resp.JSON)

	assert.Equal(t, http.StatusOK, w.Code)

	require.Len(t, mock.deactivationCalls, 1)
	call := mock.deactivationCalls[0]
	assert.False(t, call.LeaveRooms, "leave_rooms should default to false")
	assert.False(t, call.RedactMessages, "redact_messages should default to false")
}
