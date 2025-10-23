// Copyright 2024 New Vector Ltd.
// Copyright 2017 Vector Creations Ltd
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package consumers

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/nats-io/nats.go"
	log "github.com/sirupsen/logrus"

	"github.com/element-hq/dendrite/internal/eventutil"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/setup/jetstream"
	"github.com/element-hq/dendrite/setup/process"
	"github.com/element-hq/dendrite/syncapi/notifier"
	"github.com/element-hq/dendrite/syncapi/storage"
	"github.com/element-hq/dendrite/syncapi/streams"
	"github.com/element-hq/dendrite/syncapi/types"
)

// OutputNotificationDataConsumer consumes events that originated in
// the Push server.
type OutputNotificationDataConsumer struct {
	ctx       context.Context
	jetstream nats.JetStreamContext
	durable   string
	topic     string
	db        storage.Database
	notifier  *notifier.Notifier
	stream    streams.StreamProvider
	pendingMu sync.Mutex
	pending   map[userRoomKey]*pendingThreadReset
}

type userRoomKey struct {
	UserID string
	RoomID string
}

type pendingThreadReset struct {
	threads map[string]struct{}
	timer   *time.Timer
}

const threadResetDelay = 200 * time.Millisecond

// NewOutputNotificationDataConsumer creates a new consumer. Call
// Start() to begin consuming.
func NewOutputNotificationDataConsumer(
	process *process.ProcessContext,
	cfg *config.SyncAPI,
	js nats.JetStreamContext,
	store storage.Database,
	notifier *notifier.Notifier,
	stream streams.StreamProvider,
) *OutputNotificationDataConsumer {
	s := &OutputNotificationDataConsumer{
		ctx:       process.Context(),
		jetstream: js,
		durable:   cfg.Matrix.JetStream.Durable("SyncAPINotificationDataConsumer"),
		topic:     cfg.Matrix.JetStream.Prefixed(jetstream.OutputNotificationData),
		db:        store,
		notifier:  notifier,
		stream:    stream,
		pending:   make(map[userRoomKey]*pendingThreadReset),
	}
	return s
}

// Start starts consumption.
func (s *OutputNotificationDataConsumer) Start() error {
	return jetstream.JetStreamConsumer(
		s.ctx, s.jetstream, s.topic, s.durable, 1,
		s.onMessage, nats.DeliverAll(), nats.ManualAck(),
	)
}

// onMessage is called when the Sync server receives a new event from
// the push server. It is not safe for this function to be called from
// multiple goroutines, or else the sync stream position may race and
// be incorrectly calculated.
func (s *OutputNotificationDataConsumer) onMessage(ctx context.Context, msgs []*nats.Msg) bool {
	msg := msgs[0] // Guaranteed to exist if onMessage is called
	userID := string(msg.Header.Get(jetstream.UserID))

	// Parse out the event JSON
	var data eventutil.NotificationData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		sentry.CaptureException(err)
		log.WithField("user_id", userID).WithError(err).Error("user API consumer: message parse failure")
		return true
	}

	streamPos, err := s.db.UpsertRoomUnreadNotificationCounts(ctx, userID, data.RoomID, data.ThreadRootEventID, data.UnreadNotificationCount, data.UnreadHighlightCount)
	if err != nil {
		sentry.CaptureException(err)
		log.WithFields(log.Fields{
			"user_id": userID,
			"room_id": data.RoomID,
		}).WithError(err).Error("Could not save notification counts")
		return false
	}

	s.stream.Advance(streamPos)
	s.notifier.OnNewNotificationData(userID, types.StreamingToken{NotificationDataPosition: streamPos})

	if data.ThreadRootEventID == "" {
		s.scheduleThreadReset(userID, data.RoomID)
	} else {
		s.markThreadUpdated(userID, data.RoomID, data.ThreadRootEventID)
	}

	log.WithFields(log.Fields{
		"user_id":   userID,
		"room_id":   data.RoomID,
		"streamPos": streamPos,
	}).Trace("Received notification data from user API")

	return true
}

func (s *OutputNotificationDataConsumer) scheduleThreadReset(userID, roomID string) {
	threads, err := s.db.GetUserUnreadThreadNotificationCountsForRoom(s.ctx, userID, roomID)
	if err != nil {
		sentry.CaptureException(err)
		log.WithFields(log.Fields{
			"user_id": userID,
			"room_id": roomID,
		}).WithError(err).Warn("Failed to fetch existing thread counts for reset")
		threads = nil
	}
	key := userRoomKey{UserID: userID, RoomID: roomID}
	threadSet := make(map[string]struct{})
	for threadID := range threads {
		threadSet[threadID] = struct{}{}
	}

	s.pendingMu.Lock()
	if existing, ok := s.pending[key]; ok {
		if existing.timer != nil {
			existing.timer.Stop()
		}
		for threadID := range existing.threads {
			threadSet[threadID] = struct{}{}
		}
	}
	if len(threadSet) == 0 {
		delete(s.pending, key)
		s.pendingMu.Unlock()
		return
	}
	reset := &pendingThreadReset{threads: threadSet}
	reset.timer = time.AfterFunc(threadResetDelay, func() {
		s.flushThreadReset(key)
	})
	s.pending[key] = reset
	s.pendingMu.Unlock()
}

func (s *OutputNotificationDataConsumer) markThreadUpdated(userID, roomID, threadID string) {
	key := userRoomKey{UserID: userID, RoomID: roomID}
	s.pendingMu.Lock()
	entry, ok := s.pending[key]
	if ok {
		delete(entry.threads, threadID)
		if len(entry.threads) == 0 {
			if entry.timer != nil {
				entry.timer.Stop()
			}
			delete(s.pending, key)
		}
	}
	s.pendingMu.Unlock()
}

func (s *OutputNotificationDataConsumer) flushThreadReset(key userRoomKey) {
	s.pendingMu.Lock()
	entry, ok := s.pending[key]
	if ok {
		delete(s.pending, key)
	}
	s.pendingMu.Unlock()

	if !ok {
		return
	}

	for threadID := range entry.threads {
		s.resetThreadCount(key.UserID, key.RoomID, threadID)
	}
}

func (s *OutputNotificationDataConsumer) resetThreadCount(userID, roomID, threadID string) {
	pos, err := s.db.UpsertRoomUnreadNotificationCounts(s.ctx, userID, roomID, threadID, 0, 0)
	if err != nil {
		sentry.CaptureException(err)
		log.WithFields(log.Fields{
			"user_id":   userID,
			"room_id":   roomID,
			"thread_id": threadID,
		}).WithError(err).Error("failed to reset thread notification count")
		return
	}
	s.stream.Advance(pos)
	s.notifier.OnNewNotificationData(userID, types.StreamingToken{NotificationDataPosition: pos})
}
