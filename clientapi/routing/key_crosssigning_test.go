package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/test"
	"github.com/element-hq/dendrite/test/testrig"
	"github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/fclient"
	"github.com/matrix-org/gomatrixserverlib/spec"
)

type mockKeyAPI struct {
	t             *testing.T
	userResponses map[string]api.QueryKeysResponse
}

func (m mockKeyAPI) QueryKeys(ctx context.Context, req *api.QueryKeysRequest, res *api.QueryKeysResponse) {
	res.MasterKeys = m.userResponses[req.UserID].MasterKeys
	res.SelfSigningKeys = m.userResponses[req.UserID].SelfSigningKeys
	res.UserSigningKeys = m.userResponses[req.UserID].UserSigningKeys
	if m.t != nil {
		m.t.Logf("QueryKeys: %+v => %+v", req, res)
	}
}

func (m mockKeyAPI) PerformUploadDeviceKeys(ctx context.Context, req *api.PerformUploadDeviceKeysRequest, res *api.PerformUploadDeviceKeysResponse) {
	// Just a dummy upload which always succeeds
}

func getAccountByPassword(ctx context.Context, req *api.QueryAccountByPasswordRequest, res *api.QueryAccountByPasswordResponse) error {
	res.Exists = true
	res.Account = &api.Account{UserID: fmt.Sprintf("@%s:%s", req.Localpart, req.ServerName)}
	return nil
}

// Tests that if there is no existing master key for the user, the request is allowed
func Test_UploadCrossSigningDeviceKeys_ValidRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{
		"master_key": {"user_id": "@user:example.com", "usage": ["master"], "keys": {"ed25519:1": "key1"}},
		"self_signing_key": {"user_id": "@user:example.com", "usage": ["self_signing"], "keys": {"ed25519:2": "key2"}},
		"user_signing_key": {"user_id": "@user:example.com", "usage": ["user_signing"], "keys": {"ed25519:3": "key3"}}
	}`))
	req.Header.Set("Content-Type", "application/json")

	keyserverAPI := &mockKeyAPI{
		userResponses: map[string]api.QueryKeysResponse{
			"@user:example.com": {},
		},
	}
	device := &api.Device{UserID: "@user:example.com", ID: "device"}
	cfg := &config.ClientAPI{}

	res := UploadCrossSigningDeviceKeys(req, keyserverAPI, device, getAccountByPassword, cfg)
	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}
}

// Require UIA if there is an existing master key and there is no auth provided.
func Test_UploadCrossSigningDeviceKeys_Unauthorised(t *testing.T) {
	userID := "@user:example.com"

	// Note that there is no auth field.
	request := fclient.CrossSigningKeys{
		MasterKey: fclient.CrossSigningKey{
			Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:1": spec.Base64Bytes("key1")},
			Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeMaster},
			UserID: userID,
		},
		SelfSigningKey: fclient.CrossSigningKey{
			Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:1": spec.Base64Bytes("key2")},
			Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeSelfSigning},
			UserID: userID,
		},
		UserSigningKey: fclient.CrossSigningKey{
			Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:1": spec.Base64Bytes("key3")},
			Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeUserSigning},
			UserID: userID,
		},
	}

	b := bytes.Buffer{}
	m := json.NewEncoder(&b)
	err := m.Encode(request)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/", &b)
	req.Header.Set("Content-Type", "application/json")

	keyserverAPI := &mockKeyAPI{
		t: t,
		userResponses: map[string]api.QueryKeysResponse{
			"@user:example.com": {
				MasterKeys: map[string]fclient.CrossSigningKey{
					"@user:example.com": {UserID: "@user:example.com", Usage: []fclient.CrossSigningKeyPurpose{"master"}, Keys: map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:1": spec.Base64Bytes("key1")}},
				},
				SelfSigningKeys: nil,
				UserSigningKeys: nil,
			},
		},
	}
	device := &api.Device{UserID: "@user:example.com", ID: "device"}
	cfg := &config.ClientAPI{}

	res := UploadCrossSigningDeviceKeys(req, keyserverAPI, device, getAccountByPassword, cfg)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, res.Code)
	}
}

// Invalid JSON is rejected
func Test_UploadCrossSigningDeviceKeys_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{
		"auth": {"type": "m.login.password", "session": "session", "user": "user", "password": "password"},
		"master_key": {"user_id": "@user:example.com", "usage": ["master"], "keys": {"ed25519:1": "key1"}},
		"self_signing_key": {"user_id": "@user:example.com", "usage": ["self_signing"], "keys": {"ed25519:2": "key2"}},
		"user_signing_key": {"user_id": "@user:example.com", "usage": ["user_signing"], "keys": {"ed25519:3": "key3"}
	}`)) // Missing closing brace
	req.Header.Set("Content-Type", "application/json")

	keyserverAPI := &mockKeyAPI{}
	device := &api.Device{UserID: "@user:example.com", ID: "device"}
	cfg := &config.ClientAPI{}

	res := UploadCrossSigningDeviceKeys(req, keyserverAPI, device, getAccountByPassword, cfg)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, res.Code)
	}
}

// Require UIA if an existing master key is present and the keys differ.
func Test_UploadCrossSigningDeviceKeys_ExistingKeysMismatch(t *testing.T) {
	// Again, no auth provided
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{
		"master_key": {"user_id": "@user:example.com", "usage": ["master"], "keys": {"ed25519:1": "key1"}},
		"self_signing_key": {"user_id": "@user:example.com", "usage": ["self_signing"], "keys": {"ed25519:2": "key2"}},
		"user_signing_key": {"user_id": "@user:example.com", "usage": ["user_signing"], "keys": {"ed25519:3": "key3"}}
	}`))
	req.Header.Set("Content-Type", "application/json")

	keyserverAPI := &mockKeyAPI{
		userResponses: map[string]api.QueryKeysResponse{
			"@user:example.com": {
				MasterKeys: map[string]fclient.CrossSigningKey{
					"@user:example.com": {UserID: "@user:example.com", Usage: []fclient.CrossSigningKeyPurpose{"master"}, Keys: map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:1": spec.Base64Bytes("different_key")}},
				},
			},
		},
	}
	device := &api.Device{UserID: "@user:example.com", ID: "device"}

	cfg, _, _ := testrig.CreateConfig(t, test.DBTypeSQLite)
	cfg.Global.ServerName = "example.com"

	res := UploadCrossSigningDeviceKeys(req, keyserverAPI, device, getAccountByPassword, &cfg.ClientAPI)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, res.Code)
	}
}

func Test_KeysDiffer_MasterKeyMismatch(t *testing.T) {
	existingMasterKey := fclient.CrossSigningKey{
		UserID: "@user:example.com",
		Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeMaster},
		Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:1": spec.Base64Bytes("existing_key")},
	}
	keyResp := api.QueryKeysResponse{}
	uploadReq := &crossSigningRequest{
		PerformUploadDeviceKeysRequest: api.PerformUploadDeviceKeysRequest{
			CrossSigningKeys: fclient.CrossSigningKeys{
				MasterKey: fclient.CrossSigningKey{
					UserID: "@user:example.com",
					Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeMaster},
					Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:1": spec.Base64Bytes("new_key")},
				},
			},
		},
	}
	userID := "@user:example.com"

	result := keysDiffer(existingMasterKey, keyResp, uploadReq, userID)
	if !result {
		t.Fatalf("expected keys to differ, but they did not")
	}
}

func Test_KeysDiffer_SelfSigningKeyMismatch(t *testing.T) {
	existingMasterKey := fclient.CrossSigningKey{
		UserID: "@user:example.com",
		Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeMaster},
		Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:1": spec.Base64Bytes("key")},
	}
	keyResp := api.QueryKeysResponse{
		SelfSigningKeys: map[string]fclient.CrossSigningKey{
			"@user:example.com": {
				UserID: "@user:example.com",
				Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeSelfSigning},
				Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:2": spec.Base64Bytes("existing_key")},
			},
		},
	}
	uploadReq := &crossSigningRequest{
		PerformUploadDeviceKeysRequest: api.PerformUploadDeviceKeysRequest{
			CrossSigningKeys: fclient.CrossSigningKeys{
				SelfSigningKey: fclient.CrossSigningKey{
					UserID: "@user:example.com",
					Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeSelfSigning},
					Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:2": spec.Base64Bytes("new_key")},
				},
			},
		},
	}
	userID := "@user:example.com"

	result := keysDiffer(existingMasterKey, keyResp, uploadReq, userID)
	if !result {
		t.Fatalf("expected keys to differ, but they did not")
	}
}

func Test_KeysDiffer_UserSigningKeyMismatch(t *testing.T) {
	existingMasterKey := fclient.CrossSigningKey{
		UserID: "@user:example.com",
		Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeMaster},
		Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:1": spec.Base64Bytes("key")},
	}
	keyResp := api.QueryKeysResponse{
		UserSigningKeys: map[string]fclient.CrossSigningKey{
			"@user:example.com": {
				UserID: "@user:example.com",
				Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeUserSigning},
				Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:3": spec.Base64Bytes("existing_key")},
			},
		},
	}
	uploadReq := &crossSigningRequest{
		PerformUploadDeviceKeysRequest: api.PerformUploadDeviceKeysRequest{
			CrossSigningKeys: fclient.CrossSigningKeys{
				UserSigningKey: fclient.CrossSigningKey{
					UserID: "@user:example.com",
					Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeUserSigning},
					Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:3": spec.Base64Bytes("new_key")},
				},
			},
		},
	}
	userID := "@user:example.com"

	result := keysDiffer(existingMasterKey, keyResp, uploadReq, userID)
	if !result {
		t.Fatalf("expected keys to differ, but they did not")
	}
}

func Test_KeysDiffer_AllKeysMatch(t *testing.T) {
	existingMasterKey := fclient.CrossSigningKey{
		UserID: "@user:example.com",
		Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeMaster},
		Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:1": spec.Base64Bytes("key")},
	}
	keyResp := api.QueryKeysResponse{
		SelfSigningKeys: map[string]fclient.CrossSigningKey{
			"@user:example.com": {
				UserID: "@user:example.com",
				Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeSelfSigning},
				Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:2": spec.Base64Bytes("key")},
			},
		},
		UserSigningKeys: map[string]fclient.CrossSigningKey{
			"@user:example.com": {
				UserID: "@user:example.com",
				Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeUserSigning},
				Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:3": spec.Base64Bytes("key")},
			},
		},
	}
	uploadReq := &crossSigningRequest{
		PerformUploadDeviceKeysRequest: api.PerformUploadDeviceKeysRequest{
			CrossSigningKeys: fclient.CrossSigningKeys{
				MasterKey: fclient.CrossSigningKey{
					UserID: "@user:example.com",
					Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeMaster},
					Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:1": spec.Base64Bytes("key")},
				},
				SelfSigningKey: fclient.CrossSigningKey{
					UserID: "@user:example.com",
					Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeSelfSigning},
					Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:2": spec.Base64Bytes("key")},
				},
				UserSigningKey: fclient.CrossSigningKey{
					UserID: "@user:example.com",
					Usage:  []fclient.CrossSigningKeyPurpose{fclient.CrossSigningKeyPurposeUserSigning},
					Keys:   map[gomatrixserverlib.KeyID]spec.Base64Bytes{"ed25519:3": spec.Base64Bytes("key")},
				},
			},
		},
	}
	userID := "@user:example.com"

	result := keysDiffer(existingMasterKey, keyResp, uploadReq, userID)
	if result {
		t.Fatalf("expected keys to match, but they did not")
	}
}
