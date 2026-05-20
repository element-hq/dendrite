package internal

import (
	"context"
	"crypto/ed25519"

	"github.com/getsentry/sentry-go"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/fclient"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"

	asAPI "github.com/element-hq/dendrite/appservice/api"
	fsAPI "github.com/element-hq/dendrite/federationapi/api"
	"github.com/element-hq/dendrite/internal/caching"
	"github.com/element-hq/dendrite/roomserver/acls"
	"github.com/element-hq/dendrite/roomserver/api"
	"github.com/element-hq/dendrite/roomserver/internal/input"
	"github.com/element-hq/dendrite/roomserver/internal/perform"
	"github.com/element-hq/dendrite/roomserver/internal/query"
	"github.com/element-hq/dendrite/roomserver/producers"
	"github.com/element-hq/dendrite/roomserver/storage"
	"github.com/element-hq/dendrite/roomserver/types"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/setup/jetstream"
	"github.com/element-hq/dendrite/setup/process"
	userapi "github.com/element-hq/dendrite/userapi/api"
)

// RoomserverInternalAPI is an implementation of api.RoomserverInternalAPI
type RoomserverInternalAPI struct {
	*input.Inputer
	*query.Queryer
	*perform.Inviter
	*perform.Joiner
	*perform.Peeker
	*perform.InboundPeeker
	*perform.Unpeeker
	*perform.Leaver
	*perform.Publisher
	*perform.Backfiller
	*perform.Forgetter
	*perform.Upgrader
	*perform.Admin
	*perform.Creator
	ProcessContext         *process.ProcessContext
	DB                     storage.Database
	Cfg                    *config.Dendrite
	Cache                  caching.RoomServerCaches
	ServerName             spec.ServerName
	KeyRing                gomatrixserverlib.JSONVerifier
	ServerACLs             *acls.ServerACLs
	fsAPI                  fsAPI.RoomserverFederationAPI
	asAPI                  asAPI.AppServiceInternalAPI
	NATSClient             *nats.Conn
	JetStream              nats.JetStreamContext
	Durable                string
	InputRoomEventTopic    string // JetStream topic for new input room events
	OutputProducer         *producers.RoomEventProducer
	PerspectiveServerNames []spec.ServerName
	enableMetrics          bool
	defaultRoomVersion     gomatrixserverlib.RoomVersion
	PartialStateTracker    *PartialStateTracker
}

func NewRoomserverAPI(
	processContext *process.ProcessContext, dendriteCfg *config.Dendrite, roomserverDB storage.Database,
	js nats.JetStreamContext, nc *nats.Conn, caches caching.RoomServerCaches, enableMetrics bool,
) *RoomserverInternalAPI {
	var perspectiveServerNames []spec.ServerName
	for _, kp := range dendriteCfg.FederationAPI.KeyPerspectives {
		perspectiveServerNames = append(perspectiveServerNames, kp.ServerName)
	}

	serverACLs := acls.NewServerACLs(roomserverDB)
	producer := &producers.RoomEventProducer{
		Topic:     string(dendriteCfg.Global.JetStream.Prefixed(jetstream.OutputRoomEvent)),
		JetStream: js,
		ACLs:      serverACLs,
	}
	a := &RoomserverInternalAPI{
		ProcessContext:         processContext,
		DB:                     roomserverDB,
		Cfg:                    dendriteCfg,
		Cache:                  caches,
		ServerName:             dendriteCfg.Global.ServerName,
		PerspectiveServerNames: perspectiveServerNames,
		InputRoomEventTopic:    dendriteCfg.Global.JetStream.Prefixed(jetstream.InputRoomEvent),
		OutputProducer:         producer,
		JetStream:              js,
		NATSClient:             nc,
		Durable:                dendriteCfg.Global.JetStream.Durable("RoomserverInputConsumer"),
		ServerACLs:             serverACLs,
		enableMetrics:          enableMetrics,
		defaultRoomVersion:     dendriteCfg.RoomServer.DefaultRoomVersion,
		PartialStateTracker:    NewPartialStateTracker(),
		// perform-er structs + queryer struct get initialised when we have a federation sender to use
	}
	return a
}

// SetFederationInputAPI passes in a federation input API reference so that we can
// avoid the chicken-and-egg problem of both the roomserver input API and the
// federation input API being interdependent.
func (r *RoomserverInternalAPI) SetFederationAPI(fsAPI fsAPI.RoomserverFederationAPI, keyRing *gomatrixserverlib.KeyRing) {
	r.fsAPI = fsAPI
	r.KeyRing = keyRing

	r.Queryer = &query.Queryer{
		DB:                r.DB,
		Cache:             r.Cache,
		IsLocalServerName: r.Cfg.Global.IsLocalServerName,
		ServerACLs:        r.ServerACLs,
		Cfg:               r.Cfg,
		FSAPI:             fsAPI,
	}

	r.Inputer = &input.Inputer{
		Cfg:                 &r.Cfg.RoomServer,
		ProcessContext:      r.ProcessContext,
		DB:                  r.DB,
		InputRoomEventTopic: r.InputRoomEventTopic,
		OutputProducer:      r.OutputProducer,
		JetStream:           r.JetStream,
		NATSClient:          r.NATSClient,
		Durable:             nats.Durable(r.Durable),
		ServerName:          r.ServerName,
		SigningIdentity:     r.SigningIdentityFor,
		FSAPI:               fsAPI,
		RSAPI:               r,
		KeyRing:             keyRing,
		ACLs:                r.ServerACLs,
		Queryer:             r.Queryer,
		EnableMetrics:       r.enableMetrics,
	}
	r.Inviter = &perform.Inviter{
		DB:      r.DB,
		Cfg:     &r.Cfg.RoomServer,
		FSAPI:   r.fsAPI,
		RSAPI:   r,
		Inputer: r.Inputer,
	}
	r.Joiner = &perform.Joiner{
		Cfg:     &r.Cfg.RoomServer,
		DB:      r.DB,
		FSAPI:   r.fsAPI,
		RSAPI:   r,
		Inputer: r.Inputer,
		Queryer: r.Queryer,
	}
	r.Peeker = &perform.Peeker{
		ServerName: r.ServerName,
		Cfg:        &r.Cfg.RoomServer,
		DB:         r.DB,
		FSAPI:      r.fsAPI,
		Inputer:    r.Inputer,
	}
	r.InboundPeeker = &perform.InboundPeeker{
		DB:      r.DB,
		Inputer: r.Inputer,
	}
	r.Unpeeker = &perform.Unpeeker{
		ServerName: r.ServerName,
		Cfg:        &r.Cfg.RoomServer,
		FSAPI:      r.fsAPI,
		Inputer:    r.Inputer,
	}
	r.Leaver = &perform.Leaver{
		Cfg:     &r.Cfg.RoomServer,
		DB:      r.DB,
		FSAPI:   r.fsAPI,
		RSAPI:   r,
		Inputer: r.Inputer,
	}
	r.Publisher = &perform.Publisher{
		DB: r.DB,
	}
	r.Backfiller = &perform.Backfiller{
		IsLocalServerName: r.Cfg.Global.IsLocalServerName,
		DB:                r.DB,
		FSAPI:             r.fsAPI,
		Querier:           r.Queryer,
		KeyRing:           r.KeyRing,
		// Perspective servers are trusted to not lie about server keys, so we will also
		// prefer these servers when backfilling (assuming they are in the room) rather
		// than trying random servers
		PreferServers: r.PerspectiveServerNames,
	}
	r.Forgetter = &perform.Forgetter{
		DB: r.DB,
	}
	r.Upgrader = &perform.Upgrader{
		Cfg:    &r.Cfg.RoomServer,
		URSAPI: r,
	}
	r.Admin = &perform.Admin{
		DB:      r.DB,
		Cfg:     &r.Cfg.RoomServer,
		Inputer: r.Inputer,
		Queryer: r.Queryer,
		Leaver:  r.Leaver,
	}
	r.Creator = &perform.Creator{
		DB:    r.DB,
		Cfg:   &r.Cfg.RoomServer,
		RSAPI: r,
	}

	if err := r.Inputer.Start(); err != nil {
		logrus.WithError(err).Panic("failed to start roomserver input API")
	}
}

func (r *RoomserverInternalAPI) SetUserAPI(userAPI userapi.RoomserverUserAPI) {
	r.Leaver.UserAPI = userAPI
	r.Inputer.UserAPI = userAPI
}

func (r *RoomserverInternalAPI) SetAppserviceAPI(asAPI asAPI.AppServiceInternalAPI) {
	r.asAPI = asAPI
}

func (r *RoomserverInternalAPI) DefaultRoomVersion() gomatrixserverlib.RoomVersion {
	return r.defaultRoomVersion
}

func (r *RoomserverInternalAPI) IsKnownRoom(ctx context.Context, roomID spec.RoomID) (bool, error) {
	return r.Inviter.IsKnownRoom(ctx, roomID)
}

func (r *RoomserverInternalAPI) StateQuerier() gomatrixserverlib.StateQuerier {
	return r.Inviter.StateQuerier()
}

func (r *RoomserverInternalAPI) HandleInvite(
	ctx context.Context, inviteEvent *types.HeaderedEvent,
) error {
	outputEvents, err := r.Inviter.ProcessInviteMembership(ctx, inviteEvent)
	if err != nil {
		return err
	}
	return r.OutputProducer.ProduceRoomEvents(inviteEvent.RoomID().String(), outputEvents)
}

func (r *RoomserverInternalAPI) PerformCreateRoom(
	ctx context.Context, userID spec.UserID, roomID spec.RoomID, createRequest *api.PerformCreateRoomRequest,
) (string, *util.JSONResponse) {
	return r.Creator.PerformCreateRoom(ctx, userID, roomID, createRequest)
}

func (r *RoomserverInternalAPI) PerformInvite(
	ctx context.Context,
	req *api.PerformInviteRequest,
) error {
	return r.Inviter.PerformInvite(ctx, req)
}

func (r *RoomserverInternalAPI) PerformLeave(
	ctx context.Context,
	req *api.PerformLeaveRequest,
	res *api.PerformLeaveResponse,
) error {
	outputEvents, err := r.Leaver.PerformLeave(ctx, req, res)
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	if len(outputEvents) == 0 {
		return nil
	}
	return r.OutputProducer.ProduceRoomEvents(req.RoomID, outputEvents)
}

func (r *RoomserverInternalAPI) PerformForget(
	ctx context.Context,
	req *api.PerformForgetRequest,
	resp *api.PerformForgetResponse,
) error {
	return r.Forgetter.PerformForget(ctx, req, resp)
}

// GetOrCreateUserRoomPrivateKey gets the user room key for the specified user. If no key exists yet, a new one is created.
func (r *RoomserverInternalAPI) GetOrCreateUserRoomPrivateKey(ctx context.Context, userID spec.UserID, roomID spec.RoomID) (ed25519.PrivateKey, error) {
	key, err := r.DB.SelectUserRoomPrivateKey(ctx, userID, roomID)
	if err != nil {
		return nil, err
	}
	// no key found, create one
	if len(key) == 0 {
		_, key, err = ed25519.GenerateKey(nil)
		if err != nil {
			return nil, err
		}
		key, err = r.DB.InsertUserRoomPrivatePublicKey(ctx, userID, roomID, key)
		if err != nil {
			return nil, err
		}
	}
	return key, nil
}

func (r *RoomserverInternalAPI) StoreUserRoomPublicKey(ctx context.Context, senderID spec.SenderID, userID spec.UserID, roomID spec.RoomID) error {
	pubKeyBytes, err := senderID.RawBytes()
	if err != nil {
		return err
	}
	_, err = r.DB.InsertUserRoomPublicKey(ctx, userID, roomID, ed25519.PublicKey(pubKeyBytes))
	return err
}

func (r *RoomserverInternalAPI) SigningIdentityFor(ctx context.Context, roomID spec.RoomID, senderID spec.UserID) (fclient.SigningIdentity, error) {
	roomVersion, ok := r.Cache.GetRoomVersion(roomID.String())
	if !ok {
		roomInfo, err := r.DB.RoomInfo(ctx, roomID.String())
		if err != nil {
			return fclient.SigningIdentity{}, err
		}
		if roomInfo != nil {
			roomVersion = roomInfo.RoomVersion
		}
	}
	if roomVersion == gomatrixserverlib.RoomVersionPseudoIDs {
		privKey, err := r.GetOrCreateUserRoomPrivateKey(ctx, senderID, roomID)
		if err != nil {
			return fclient.SigningIdentity{}, err
		}
		return fclient.SigningIdentity{
			PrivateKey: privKey,
			KeyID:      "ed25519:1",
			ServerName: spec.ServerName(spec.SenderIDFromPseudoIDKey(privKey)),
		}, nil
	}
	identity, err := r.Cfg.Global.SigningIdentityFor(senderID.Domain())
	if err != nil {
		return fclient.SigningIdentity{}, err
	}
	return *identity, err
}

func (r *RoomserverInternalAPI) AssignRoomNID(ctx context.Context, roomID spec.RoomID, roomVersion gomatrixserverlib.RoomVersion) (roomNID types.RoomNID, err error) {
	return r.DB.AssignRoomNID(ctx, roomID, roomVersion)
}

func (r *RoomserverInternalAPI) InsertReportedEvent(
	ctx context.Context,
	roomID, eventID, reportingUserID, reason string,
	score int64,
) (int64, error) {
	return r.DB.InsertReportedEvent(ctx, roomID, eventID, reportingUserID, reason, score)
}

// MSC3706 Partial State Join methods

// SetRoomPartialState marks a room as having partial state after a faster join
func (r *RoomserverInternalAPI) SetRoomPartialState(ctx context.Context, roomNID types.RoomNID, joinEventNID types.EventNID, joinedVia string, serversInRoom []string, deviceListStreamID int64) error {
	return r.DB.SetRoomPartialState(ctx, roomNID, joinEventNID, joinedVia, serversInRoom, deviceListStreamID)
}

// IsRoomPartialState returns true if the room has partial state
func (r *RoomserverInternalAPI) IsRoomPartialState(ctx context.Context, roomNID types.RoomNID) (bool, error) {
	return r.DB.IsRoomPartialState(ctx, roomNID)
}

// ClearRoomPartialState removes the partial state flag from a room
// Returns the device list stream ID that was stored at join time for device list replay
func (r *RoomserverInternalAPI) ClearRoomPartialState(ctx context.Context, roomNID types.RoomNID) (int64, error) {
	return r.DB.ClearRoomPartialState(ctx, roomNID)
}

// GetPartialStateServers returns servers known to be in a partial state room
func (r *RoomserverInternalAPI) GetPartialStateServers(ctx context.Context, roomNID types.RoomNID) ([]string, error) {
	return r.DB.GetPartialStateServers(ctx, roomNID)
}

// GetPartialStateDeviceListStreamID returns the device list stream ID for a partial state room
func (r *RoomserverInternalAPI) GetPartialStateDeviceListStreamID(ctx context.Context, roomNID types.RoomNID) (int64, error) {
	return r.DB.GetPartialStateDeviceListStreamID(ctx, roomNID)
}

// GetAllPartialStateRooms returns all rooms with partial state
func (r *RoomserverInternalAPI) GetAllPartialStateRooms(ctx context.Context) ([]types.RoomNID, error) {
	return r.DB.GetAllPartialStateRooms(ctx)
}

// RoomInfoByNID returns room information for the given room NID
func (r *RoomserverInternalAPI) RoomInfoByNID(ctx context.Context, roomNID types.RoomNID) (*types.RoomInfo, error) {
	return r.DB.RoomInfoByNID(ctx, roomNID)
}

// LatestEventIDs returns the latest event IDs and state snapshot for a room
func (r *RoomserverInternalAPI) LatestEventIDs(ctx context.Context, roomNID types.RoomNID) ([]string, types.StateSnapshotNID, int64, error) {
	return r.DB.LatestEventIDs(ctx, roomNID)
}

// RoomIDFromNID returns the room ID for a given room NID
func (r *RoomserverInternalAPI) RoomIDFromNID(ctx context.Context, roomNID types.RoomNID) (string, error) {
	return r.DB.RoomIDFromNID(ctx, roomNID)
}

// GetPartialStateRoomIDs returns the room IDs of all rooms currently in partial state (MSC3706 faster joins).
// This is used by sync to filter rooms that may have incomplete state.
func (r *RoomserverInternalAPI) GetPartialStateRoomIDs(ctx context.Context) ([]string, error) {
	roomNIDs, err := r.DB.GetAllPartialStateRooms(ctx)
	if err != nil {
		return nil, err
	}
	roomIDs := make([]string, 0, len(roomNIDs))
	for _, nid := range roomNIDs {
		roomID, err := r.DB.RoomIDFromNID(ctx, nid)
		if err != nil {
			// Skip rooms we can't look up
			continue
		}
		roomIDs = append(roomIDs, roomID)
	}
	return roomIDs, nil
}

// NotifyUnPartialStated notifies observers that a room has completed its partial state resync.
// This wakes up any callers waiting in AwaitFullState for this room and emits an output
// event to notify downstream components (like syncapi) about the completion.
func (r *RoomserverInternalAPI) NotifyUnPartialStated(roomID string) {
	// Wake up any callers waiting for full state
	if r.PartialStateTracker != nil {
		r.PartialStateTracker.NotifyUnPartialStated(roomID)
	}

	// Query local joined members to notify downstream components
	ctx := context.Background()
	membershipsReq := &api.QueryMembershipsForRoomRequest{
		RoomID:    roomID,
		JoinedOnly: true,
		LocalOnly:  true,
	}
	membershipsRes := &api.QueryMembershipsForRoomResponse{}
	if err := r.Queryer.QueryMembershipsForRoom(ctx, membershipsReq, membershipsRes); err != nil {
		logrus.WithError(err).WithField("room_id", roomID).Error("Failed to query memberships for un-partial-stated room")
		return
	}

	// Extract user IDs from membership events
	var joinedUserIDs []string
	for _, ev := range membershipsRes.JoinEvents {
		if ev.StateKey != nil && *ev.StateKey != "" {
			joinedUserIDs = append(joinedUserIDs, *ev.StateKey)
		}
	}

	if len(joinedUserIDs) == 0 {
		logrus.WithField("room_id", roomID).Debug("No local members in un-partial-stated room, skipping output event")
		return
	}

	// Emit output event to notify downstream components
	outputEvent := api.OutputEvent{
		Type: api.OutputTypeUnPartialStatedRoom,
		UnPartialStatedRoom: &api.OutputUnPartialStatedRoom{
			RoomID:        roomID,
			JoinedUserIDs: joinedUserIDs,
		},
	}

	if err := r.OutputProducer.ProduceRoomEvents(roomID, []api.OutputEvent{outputEvent}); err != nil {
		logrus.WithError(err).WithField("room_id", roomID).Error("Failed to produce un-partial-stated room event")
		return
	}

	logrus.WithFields(logrus.Fields{
		"room_id":     roomID,
		"user_count":  len(joinedUserIDs),
	}).Info("Room completed partial state resync, notified downstream components")
}

// UpdateCurrentStateAfterResync updates the current state and memberships after a partial state resync.
// This is called after state events have been stored as outliers via SendStateAsOutliers.
// It creates a new state snapshot from the stored events, calculates the state delta,
// updates the membership table, and notifies downstream components (syncapi).
func (r *RoomserverInternalAPI) UpdateCurrentStateAfterResync(ctx context.Context, roomID string, stateEventIDs []string) error {
	return r.Inputer.UpdateStateAfterResync(ctx, roomID, stateEventIDs)
}
