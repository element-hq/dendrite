package internal_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/element-hq/dendrite/internal/sqlutil"
	rsapi "github.com/element-hq/dendrite/roomserver/api"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/test"
	"github.com/element-hq/dendrite/test/testrig"
	userapi "github.com/element-hq/dendrite/userapi/api"
	"github.com/element-hq/dendrite/userapi/internal"
	"github.com/element-hq/dendrite/userapi/producers"
	"github.com/element-hq/dendrite/userapi/storage"
	"github.com/element-hq/dendrite/userapi/types"
	"github.com/matrix-org/gomatrixserverlib/spec"
)

type userDeactivationHarness struct {
	t         *testing.T
	userAPI   *internal.UserInternalAPI
	accountDB storage.UserDatabase
	keyDB     storage.KeyDatabase
	server    spec.ServerName
	close     func()
}

func newUserDeactivationHarness(t *testing.T, dbType test.DBType) *userDeactivationHarness {
	t.Helper()

	cfg, processCtx, close := testrig.CreateConfig(t, dbType)

	cm := sqlutil.NewConnectionManager(processCtx, cfg.Global.DatabaseOptions)

	accountDB, err := storage.NewUserDatabase(
		processCtx.Context(),
		cm,
		&cfg.UserAPI.AccountDatabase,
		cfg.Global.ServerName,
		bcrypt.MinCost,
		config.DefaultOpenIDTokenLifetimeMS,
		userapi.DefaultLoginTokenLifetime,
		"", // serverNoticesLocalpart
	)
	require.NoError(t, err)

	keyDB, err := storage.NewKeyDatabase(cm, &cfg.KeyServer.Database)
	require.NoError(t, err)

	// Create a KeyChange producer for testing
	keyChangeProducer := &producers.KeyChange{
		Topic:     "test",
		JetStream: &stubJetStreamPublisher{},
		DB:        keyDB,
	}

	userAPI := &internal.UserInternalAPI{
		DB:                accountDB,
		KeyDatabase:       keyDB,
		Config:            &cfg.UserAPI,
		KeyChangeProducer: keyChangeProducer,
	}

	return &userDeactivationHarness{
		t:         t,
		userAPI:   userAPI,
		accountDB: accountDB,
		keyDB:     keyDB,
		server:    cfg.Global.ServerName,
		close:     close,
	}
}

func (h *userDeactivationHarness) tearDown() {
	h.close()
}

type stubJetStreamPublisher struct{}

func (s *stubJetStreamPublisher) PublishMsg(msg *nats.Msg, opts ...nats.PubOpt) (*nats.PubAck, error) {
	return &nats.PubAck{}, nil
}

type stubRoomserverAPI struct{
	rsapi.UserRoomserverAPI
	rooms []string
	calls int
	err   error
}

func (s *stubRoomserverAPI) PerformAdminEvacuateUser(ctx context.Context, userID string) ([]string, error) {
	s.calls++
	return s.rooms, s.err
}

func TestPerformUserDeactivation_FullFlow(t *testing.T) {
	ctx := context.Background()

	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		h := newUserDeactivationHarness(t, dbType)
		defer h.tearDown()

		localpart := "deactivate-me"
		userID := fmt.Sprintf("@%s:%s", localpart, h.server)
		password := "S3cretPass!"

		_, err := h.accountDB.CreateAccount(ctx, localpart, h.server, password, "", userapi.AccountTypeUser)
		require.NoError(t, err)

		for i := 0; i < 3; i++ {
			deviceID := fmt.Sprintf("DEV%d", i)
			token := fmt.Sprintf("tok%d", i)
			_, err := h.accountDB.CreateDevice(ctx, localpart, h.server, &deviceID, token, nil, "", "")
			require.NoError(t, err)
		}

		devicesBefore, err := h.accountDB.GetDevicesByLocalpart(ctx, localpart, h.server)
		require.NoError(t, err)
		require.Len(t, devicesBefore, 3)

		rsStub := &stubRoomserverAPI{
			rooms: []string{"!abc:test", "!def:test"},
		}
		h.userAPI.RSAPI = rsStub

		hook := logrustest.NewGlobal()
		defer hook.Reset()

		req := &userapi.PerformUserDeactivationRequest{
			UserID:         userID,
			LeaveRooms:     true,
			RedactMessages: true,
			RequestedBy:    "@admin:test",
		}
		res := &userapi.PerformUserDeactivationResponse{}

		err = h.userAPI.PerformUserDeactivation(ctx, req, res)
		require.NoError(t, err)

		require.True(t, res.Deactivated, "expected user to be marked deactivated")
		require.Equal(t, len(rsStub.rooms), res.RoomsLeft, "unexpected rooms_left count")
		require.Equal(t, len(devicesBefore), res.TokensRevoked, "unexpected tokens_revoked count")

		devicesAfter, err := h.accountDB.GetDevicesByLocalpart(ctx, localpart, h.server)
		require.NoError(t, err)
		require.Len(t, devicesAfter, 0, "expected all devices to be removed")

		loginRes := &userapi.QueryAccountByPasswordResponse{}
		err = h.userAPI.QueryAccountByPassword(ctx, &userapi.QueryAccountByPasswordRequest{
			Localpart:         localpart,
			ServerName:        h.server,
			PlaintextPassword: password,
		}, loginRes)
		require.NoError(t, err)
		require.False(t, loginRes.Exists, "expected login to fail for deactivated user")

		jobs, err := h.accountDB.GetUserRedactionJobs(ctx, userID)
		require.NoError(t, err)
		require.Len(t, jobs, 1, "expected redaction job queued")

		job := jobs[0]
		require.Equal(t, userID, job.UserID)
		require.Equal(t, req.RequestedBy, job.RequestedBy)
		require.Equal(t, types.RedactionJobStatusPending, job.Status)
		require.True(t, job.RedactMessages, "expected redact_messages option stored")

		// Find the admin deactivation audit log entry
		var adminLogEntry *logrus.Entry
		for _, entry := range hook.AllEntries() {
			if entry.Message == "Admin deactivating user" {
				adminLogEntry = entry
				break
			}
		}
		require.NotNil(t, adminLogEntry, "expected audit log entry for admin deactivation")
		require.Equal(t, req.RequestedBy, adminLogEntry.Data["admin_user"])
		require.Equal(t, userID, adminLogEntry.Data["target_user"])
		require.Equal(t, req.LeaveRooms, adminLogEntry.Data["leave_rooms"])
		require.Equal(t, req.RedactMessages, adminLogEntry.Data["redact_messages"])
	})
}

func TestPerformUserDeactivation_OptionsOff(t *testing.T) {
	ctx := context.Background()

	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		h := newUserDeactivationHarness(t, dbType)
		defer h.tearDown()

		localpart := "no-opts"
		userID := fmt.Sprintf("@%s:%s", localpart, h.server)

		_, err := h.accountDB.CreateAccount(ctx, localpart, h.server, "s3cret", "", userapi.AccountTypeUser)
		require.NoError(t, err)

		deviceID := "ONLYDEV"
		_, err = h.accountDB.CreateDevice(ctx, localpart, h.server, &deviceID, "single", nil, "", "")
		require.NoError(t, err)

		rsStub := &stubRoomserverAPI{
			rooms: []string{"!unused:test"},
		}
		h.userAPI.RSAPI = rsStub

		req := &userapi.PerformUserDeactivationRequest{
			UserID:         userID,
			LeaveRooms:     false,
			RedactMessages: false,
			RequestedBy:    "@auditor:test",
		}
		res := &userapi.PerformUserDeactivationResponse{}

		err = h.userAPI.PerformUserDeactivation(ctx, req, res)
		require.NoError(t, err)

		require.Equal(t, 0, rsStub.calls, "expected roomserver not invoked when leave_rooms=false")
		require.Equal(t, 0, res.RoomsLeft)

		jobs, err := h.accountDB.GetUserRedactionJobs(ctx, userID)
		require.NoError(t, err)
		require.Len(t, jobs, 0, "expected no redaction jobs when redact_messages=false")
	})
}
