// Copyright 2025 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package msc3861

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"errors"

	"github.com/element-hq/dendrite/federationapi/statistics"
	"github.com/element-hq/dendrite/internal/caching"
	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/roomserver"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/setup/jetstream"
	"github.com/element-hq/dendrite/test"
	"github.com/element-hq/dendrite/test/testrig"
	"github.com/element-hq/dendrite/userapi"
	uapi "github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/fclient"
	"github.com/matrix-org/gomatrixserverlib/spec"
)

var testIsBlacklistedOrBackingOff = func(s spec.ServerName) (*statistics.ServerStatistics, error) {
	return &statistics.ServerStatistics{}, nil
}

type roundTripper struct {
	roundTrip func(request *http.Request) (*http.Response, error)
}

func (rt *roundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return rt.roundTrip(request)
}

func TestVerifyUserFromRequest(t *testing.T) {
	aliceUser := test.NewUser(t, test.WithAccountType(uapi.AccountTypeUser))
	bobUser := test.NewUser(t, test.WithAccountType(uapi.AccountTypeUser))

	roundTrip := func(request *http.Request) (*http.Response, error) {
		var (
			respBody   string
			statusCode int
		)

		switch request.URL.String() {
		case "https://mas.example.com/.well-known/openid-configuration":
			respBody = `{"introspection_endpoint": "https://mas.example.com/oauth2/introspect"}`
			statusCode = http.StatusOK
		case "https://mas.example.com/oauth2/introspect":
			_ = request.ParseForm()

			switch request.Form.Get("token") {
			case "validTokenUserExistsTokenActive":
				statusCode = http.StatusOK
				resp := introspectionResponse{
					Active:   true,
					Scope:    "urn:matrix:org.matrix.msc2967.client:device:devAlice urn:matrix:org.matrix.msc2967.client:api:*",
					Sub:      "111111111111111111",
					Username: aliceUser.Localpart,
				}
				b, _ := json.Marshal(resp)
				respBody = string(b)
			case "validTokenUserDoesNotExistTokenActive":
				statusCode = http.StatusOK
				resp := introspectionResponse{
					Active:   true,
					Scope:    "urn:matrix:org.matrix.msc2967.client:device:devBob urn:matrix:org.matrix.msc2967.client:api:*",
					Sub:      "222222222222222222",
					Username: bobUser.Localpart,
				}
				b, _ := json.Marshal(resp)
				respBody = string(b)
			case "validTokenUserExistsTokenInactive":
				statusCode = http.StatusOK
				resp := introspectionResponse{Active: false}
				b, _ := json.Marshal(resp)
				respBody = string(b)
			default:
				return nil, errors.New("Request URL not supported by stub")
			}
		}

		respReader := io.NopCloser(strings.NewReader(respBody))
		resp := http.Response{
			StatusCode:    statusCode,
			Body:          respReader,
			ContentLength: int64(len(respBody)),
			Header:        map[string][]string{"Content-Type": {"application/json"}},
		}
		return &resp, nil
	}

	httpClient := fclient.NewClient(
		fclient.WithTransport(&roundTripper{roundTrip: roundTrip}),
	)

	ctx := context.Background()
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		cfg, processCtx, close := testrig.CreateConfig(t, dbType)
		defer close()
		cfg.ClientAPI.MSCs.MSC3861 = &config.MSC3861{
			Issuer: "https://mas.example.com",
		}
		cfg.ClientAPI.RateLimiting.Enabled = false
		natsInstance := jetstream.NATSInstance{}
		// add a vhost
		cfg.Global.VirtualHosts = append(cfg.Global.VirtualHosts, &config.VirtualHost{
			SigningIdentity: fclient.SigningIdentity{ServerName: "vh1"},
		})
		caches := caching.NewRistrettoCache(128*1024*1024, time.Hour, caching.DisableMetrics)
		cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)
		rsAPI := roomserver.NewInternalAPI(processCtx, cfg, cm, &natsInstance, caches, caching.DisableMetrics)
		rsAPI.SetFederationAPI(nil, nil)
		// Needed for /login
		userAPI := userapi.NewInternalAPI(processCtx, cfg, cm, &natsInstance, rsAPI, nil, caching.DisableMetrics, testIsBlacklistedOrBackingOff)
		userVerifier, err := newMSC3861UserVerifier(
			userAPI,
			cfg.Global.ServerName,
			cfg.MSCs.MSC3861,
			false,
			httpClient,
		)
		if err != nil {
			t.Fatal(err.Error())
		}
		u, _ := url.Parse("https://example.com/something")

		t.Run("existing user and active token", func(t *testing.T) {
			localpart, serverName, _ := gomatrixserverlib.SplitID('@', aliceUser.ID)
			userRes := &uapi.PerformAccountCreationResponse{}
			if err := userAPI.PerformAccountCreation(ctx, &uapi.PerformAccountCreationRequest{
				AccountType: aliceUser.AccountType,
				Localpart:   localpart,
				ServerName:  serverName,
			}, userRes); err != nil {
				t.Errorf("failed to create account: %s", err)
			}
			if !userRes.AccountCreated {
				t.Fatalf("account not created")
			}
			httpReq := http.Request{
				URL: u,
				Header: map[string][]string{
					"Content-Type":  {"application/json"},
					"Authorization": {"Bearer validTokenUserExistsTokenActive"},
				},
			}
			device, jsonResp := userVerifier.VerifyUserFromRequest(&httpReq)
			if jsonResp != nil {
				t.Fatalf("JSONResponse is not expected: %+v", jsonResp)
			}
			deviceRes := uapi.QueryDevicesResponse{}
			if err := userAPI.QueryDevices(ctx, &uapi.QueryDevicesRequest{
				UserID: aliceUser.ID,
			}, &deviceRes); err != nil {
				t.Errorf("failed to query user devices")
			}
			if !deviceRes.UserExists {
				t.Fatalf("user does not exist")
			}
			if l := len(deviceRes.Devices); l != 1 {
				t.Fatalf("Incorrect number of user devices. Got %d, want 1", l)
			}
			if device.ID != deviceRes.Devices[0].ID {
				t.Fatalf("Device IDs do not match: %s != %s", device.ID, deviceRes.Devices[0].ID)
			}
		})

		t.Run("inactive token", func(t *testing.T) {
			httpReq := http.Request{
				URL: u,
				Header: map[string][]string{
					"Content-Type":  {"application/json"},
					"Authorization": {"Bearer validTokenUserExistsTokenInactive"},
				},
			}
			device, jsonResp := userVerifier.VerifyUserFromRequest(&httpReq)
			if jsonResp == nil {
				t.Fatal("JSONResponse is expected to be nil")
			}
			if device != nil {
				t.Fatalf("Device is not nil: %+v", device)
			}
			if jsonResp.Code != http.StatusUnauthorized {
				t.Fatalf("Incorrect status code: want=401, got=%d", jsonResp.Code)
			}
			mErr, _ := jsonResp.JSON.(spec.MatrixError)
			if mErr.ErrCode != spec.ErrorUnknownToken {
				t.Fatalf("Unexpected error code: want=%s, got=%s", spec.ErrorUnknownToken, mErr.ErrCode)
			}
		})

		t.Run("non-existing user", func(t *testing.T) {
			httpReq := http.Request{
				URL: u,
				Header: map[string][]string{
					"Content-Type":  {"application/json"},
					"Authorization": {"Bearer validTokenUserDoesNotExistTokenActive"},
				},
			}
			device, jsonResp := userVerifier.VerifyUserFromRequest(&httpReq)
			if jsonResp != nil {
				t.Fatalf("JSONResponse is not expected: %+v", jsonResp)
			}
			deviceRes := uapi.QueryDevicesResponse{}
			if err := userAPI.QueryDevices(ctx, &uapi.QueryDevicesRequest{
				UserID: bobUser.ID,
			}, &deviceRes); err != nil {
				t.Errorf("failed to query user devices")
			}
			if !deviceRes.UserExists {
				t.Fatalf("user does not exist")
			}
			if l := len(deviceRes.Devices); l != 1 {
				t.Fatalf("Incorrect number of user devices. Got %d, want 1", l)
			}
			if device.ID != deviceRes.Devices[0].ID {
				t.Fatalf("Device IDs do not match: %s != %s", device.ID, deviceRes.Devices[0].ID)
			}
		})
	})
}
