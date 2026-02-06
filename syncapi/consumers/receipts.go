// Copyright 2024 New Vector Ltd.
// Copyright 2020 The Matrix.org Foundation C.I.C.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package consumers

import (
	"context"
	"strconv"

	"github.com/getsentry/sentry-go"
	"github.com/nats-io/nats.go"
	log "github.com/sirupsen/logrus"

	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/setup/jetstream"
	"github.com/element-hq/dendrite/setup/process"
	"github.com/element-hq/dendrite/syncapi/notifier"
	"github.com/element-hq/dendrite/syncapi/storage"
	"github.com/element-hq/dendrite/syncapi/streams"
	"github.com/element-hq/dendrite/syncapi/types"
	"github.com/matrix-org/gomatrixserverlib/spec"
)

// OutputReceiptEventConsumer consumes events that originated in the EDU server.
type OutputReceiptEventConsumer struct {
	ctx       context.Context
	jetstream nats.JetStreamContext
	durable   string
	topic     string
	db        storage.Database
	stream    streams.StreamProvider
	notifier  *notifier.Notifier
}

// NewOutputReceiptEventConsumer creates a new OutputReceiptEventConsumer.
// Call Start() to begin consuming from the EDU server.
func NewOutputReceiptEventConsumer(
	process *process.ProcessContext,
	cfg *config.SyncAPI,
	js nats.JetStreamContext,
	store storage.Database,
	notifier *notifier.Notifier,
	stream streams.StreamProvider,
) *OutputReceiptEventConsumer {
	return &OutputReceiptEventConsumer{
		ctx:       process.Context(),
		jetstream: js,
		topic:     cfg.Matrix.JetStream.Prefixed(jetstream.OutputReceiptEvent),
		durable:   cfg.Matrix.JetStream.Durable("SyncAPIReceiptConsumer"),
		db:        store,
		notifier:  notifier,
		stream:    stream,
	}
}

// Start consuming receipts events.
func (s *OutputReceiptEventConsumer) Start() error {
	return jetstream.JetStreamConsumer(
		s.ctx, s.jetstream, s.topic, s.durable, 1,
		s.onMessage, nats.DeliverAll(), nats.ManualAck(),
	)
}

func (s *OutputReceiptEventConsumer) onMessage(ctx context.Context, msgs []*nats.Msg) bool {
	msg := msgs[0] // Guaranteed to exist if onMessage is called
	output := types.OutputReceiptEvent{
		UserID:  msg.Header.Get(jetstream.UserID),
		RoomID:  msg.Header.Get(jetstream.RoomID),
		EventID: msg.Header.Get(jetstream.EventID),
		Type:    msg.Header.Get("type"),
	}

	log.WithFields(log.Fields{
		"user_id":  output.UserID,
		"room_id":  output.RoomID,
		"event_id": output.EventID,
		"type":     output.Type,
	}).Debug("SyncAPI receipt consumer received message")

	timestamp, err := strconv.ParseUint(msg.Header.Get("timestamp"), 10, 64)
	if err != nil {
		// If the message was invalid, log it and move on to the next message in the stream
		log.WithError(err).Errorf("output log: message parse failure")
		sentry.CaptureException(err)
		return true
	}

	output.Timestamp = spec.Timestamp(timestamp)

	streamPos, err := s.db.StoreReceipt(
		s.ctx,
		output.RoomID,
		output.Type,
		output.UserID,
		output.EventID,
		output.Timestamp,
	)
	if err != nil {
		log.WithError(err).WithFields(log.Fields{
			"user_id":  output.UserID,
			"room_id":  output.RoomID,
			"event_id": output.EventID,
		}).Error("SyncAPI receipt consumer: failed to store receipt")
		sentry.CaptureException(err)
		return true
	}

	log.WithFields(log.Fields{
		"user_id":    output.UserID,
		"room_id":    output.RoomID,
		"event_id":   output.EventID,
		"stream_pos": streamPos,
	}).Debug("SyncAPI receipt consumer: stored receipt successfully")

	// When a user posts an m.read receipt, update their notification count to 0
	// This ensures the unread badge clears immediately for v4 sync
	// IMPORTANT: Do this BEFORE calling notifiers to avoid race conditions
	var notifStreamPos types.StreamPosition
	if output.Type == "m.read" {
		log.WithFields(log.Fields{
			"user_id": output.UserID,
			"room_id": output.RoomID,
		}).Debug("SyncAPI receipt consumer: clearing notification count for m.read receipt")

		notifStreamPos, err = s.db.UpsertRoomUnreadNotificationCounts(
			s.ctx,
			output.UserID,
			output.RoomID,
			0, // Clear notification count
			0, // Clear highlight count
		)
		if err != nil {
			log.WithError(err).WithFields(log.Fields{
				"user_id": output.UserID,
				"room_id": output.RoomID,
			}).Error("SyncAPI receipt consumer: failed to clear notification counts")
			// Continue anyway - receipt was still stored successfully
		} else {
			log.WithFields(log.Fields{
				"user_id":            output.UserID,
				"room_id":            output.RoomID,
				"notif_stream_pos":   notifStreamPos,
				"receipt_stream_pos": streamPos,
			}).Debug("SyncAPI receipt consumer: cleared notification counts successfully")
		}
	}

	// Advance streams and notify AFTER all database commits are done
	// This prevents long-polling connections from waking up between commits
	s.stream.Advance(streamPos)
	s.notifier.OnNewReceipt(output.RoomID, types.StreamingToken{ReceiptPosition: streamPos})

	if output.Type == "m.read" && notifStreamPos > 0 {
		s.notifier.OnNewNotificationData(output.UserID, types.StreamingToken{NotificationDataPosition: notifStreamPos})
	}

	return true
}
