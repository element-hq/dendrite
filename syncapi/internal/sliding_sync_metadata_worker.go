// Copyright 2025 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package internal

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/element-hq/dendrite/setup/process"
	"github.com/element-hq/dendrite/syncapi/storage"
	"github.com/element-hq/dendrite/syncapi/storage/shared"
	"github.com/element-hq/dendrite/syncapi/storage/tables"
)

// RoomMetadataQueuer is an interface for queuing rooms for metadata recalculation.
// This is implemented by SlidingSyncMetadataWorker and can be used by consumers
// to notify the worker when room state changes.
type RoomMetadataQueuer interface {
	QueueRoom(roomID string)
}

const (
	// Number of concurrent workers processing rooms
	metadataWorkerCount = 2
	// How often to check for rooms needing recalculation
	metadataTickerInterval = time.Minute
	// Batch size for initial population
	metadataBatchSize = 100
	// Delay between processing batches to avoid overloading
	metadataBatchDelay = time.Millisecond * 100
)

// SlidingSyncMetadataWorker handles background population of sliding sync
// room metadata tables (Phase 12 optimization). It processes rooms from the
// recalculation queue and populates the joined_rooms and membership_snapshots tables.
type SlidingSyncMetadataWorker struct {
	process      *process.ProcessContext
	db           storage.Database
	roomMetadata tables.SlidingSyncRoomMetadata
	workerCh     chan string
	retryMu      sync.Mutex
	retryMap     map[string]time.Time
}

// NewSlidingSyncMetadataWorker creates a new metadata worker
func NewSlidingSyncMetadataWorker(
	processCtx *process.ProcessContext,
	db storage.Database,
) *SlidingSyncMetadataWorker {
	return &SlidingSyncMetadataWorker{
		process:      processCtx,
		db:           db,
		roomMetadata: db.GetSlidingSyncRoomMetadata(),
		workerCh:     make(chan string, 1000),
		retryMap:     make(map[string]time.Time),
	}
}

// Start begins the metadata worker. This is non-blocking - all work happens
// in background goroutines.
func (w *SlidingSyncMetadataWorker) Start() error {
	// Check if tables need initial population
	needsPopulation, err := w.checkNeedsInitialPopulation()
	if err != nil {
		logrus.WithError(err).Warn("[SLIDING_SYNC_METADATA] Failed to check if initial population needed")
		// Continue anyway - we'll populate incrementally
	}

	// Start worker goroutines
	for i := 0; i < metadataWorkerCount; i++ {
		go w.worker(i)
	}

	// Start retry/ticker loop
	go w.tickerLoop()

	if needsPopulation {
		// Queue initial population in background
		go w.queueInitialPopulation()
	}

	logrus.Info("[SLIDING_SYNC_METADATA] Worker started")
	return nil
}

// checkNeedsInitialPopulation checks if we need to do initial population
// by checking if the recalculation queue or joined_rooms table is empty
func (w *SlidingSyncMetadataWorker) checkNeedsInitialPopulation() (bool, error) {
	ctx := w.process.Context()

	// Check if SlidingSyncRoomMetadata is nil (tables not yet created)
	if w.roomMetadata == nil {
		logrus.Warn("[SLIDING_SYNC_METADATA] SlidingSyncRoomMetadata table not initialized")
		return false, nil
	}

	// Check if we have any rooms in the recalculation queue
	rooms, err := w.roomMetadata.SelectRoomsToRecalculate(ctx, nil, 1)
	if err != nil {
		// Table might not exist yet, or other error
		logrus.WithError(err).Debug("[SLIDING_SYNC_METADATA] Could not check recalculate queue")
		return true, nil // Assume we need population
	}

	// If there are rooms in the queue, we're already populating
	if len(rooms) > 0 {
		logrus.WithField("queued_rooms", len(rooms)).Info("[SLIDING_SYNC_METADATA] Rooms already queued for recalculation")
		return false, nil
	}

	// Check if joined_rooms has any entries by trying to select one
	existingRooms, err := w.roomMetadata.SelectJoinedRoomsByFilters(ctx, nil, nil, nil, nil, 1)
	if err != nil {
		logrus.WithError(err).Debug("[SLIDING_SYNC_METADATA] Could not check joined_rooms")
		return true, nil // Assume we need population
	}

	// If we have rooms cached, no need for initial population
	if len(existingRooms) > 0 {
		logrus.Info("[SLIDING_SYNC_METADATA] Joined rooms cache already populated")
		return false, nil
	}

	return true, nil
}

// queueInitialPopulation queries all existing rooms and queues them for processing
func (w *SlidingSyncMetadataWorker) queueInitialPopulation() {
	ctx := w.process.Context()

	logrus.Info("[SLIDING_SYNC_METADATA] Starting initial population - queuing all rooms")

	// Get all room IDs from current_room_state (using AllJoinedUsersInRooms which returns map[roomID][]userID)
	snapshot, err := w.db.NewDatabaseSnapshot(ctx)
	if err != nil {
		logrus.WithError(err).Error("[SLIDING_SYNC_METADATA] Failed to get snapshot for initial population")
		return
	}
	defer snapshot.Rollback()

	// Query all rooms that have joined users
	roomsWithUsers, err := snapshot.AllJoinedUsersInRooms(ctx)
	if err != nil {
		logrus.WithError(err).Error("[SLIDING_SYNC_METADATA] Failed to query rooms for initial population")
		return
	}

	// Extract room IDs from the map keys
	roomIDs := make([]string, 0, len(roomsWithUsers))
	for roomID := range roomsWithUsers {
		roomIDs = append(roomIDs, roomID)
	}

	logrus.WithField("room_count", len(roomIDs)).Info("[SLIDING_SYNC_METADATA] Queuing rooms for initial population")

	// Add all rooms to the recalculation queue
	for i, roomID := range roomIDs {
		select {
		case <-ctx.Done():
			logrus.Info("[SLIDING_SYNC_METADATA] Shutting down during initial population")
			return
		default:
		}

		// Insert into recalculate queue
		if err := w.roomMetadata.InsertRoomToRecalculate(ctx, nil, roomID); err != nil {
			logrus.WithError(err).WithField("room_id", roomID).Warn("[SLIDING_SYNC_METADATA] Failed to queue room")
			continue
		}

		// Also queue for immediate processing
		w.QueueRoom(roomID)

		// Log progress periodically
		if (i+1)%1000 == 0 {
			logrus.WithField("progress", i+1).Info("[SLIDING_SYNC_METADATA] Initial population progress")
		}

		// Small delay to avoid overwhelming the system
		if (i+1)%metadataBatchSize == 0 {
			time.Sleep(metadataBatchDelay)
		}
	}

	logrus.WithField("room_count", len(roomIDs)).Info("[SLIDING_SYNC_METADATA] Initial population queuing complete")
}

// QueueRoom adds a room to the processing queue
func (w *SlidingSyncMetadataWorker) QueueRoom(roomID string) {
	select {
	case w.workerCh <- roomID:
	default:
		// Channel full, add to retry map
		w.retryMu.Lock()
		if _, exists := w.retryMap[roomID]; !exists {
			w.retryMap[roomID] = time.Now().Add(time.Second * 30)
		}
		w.retryMu.Unlock()
	}
}

// worker processes rooms from the channel
func (w *SlidingSyncMetadataWorker) worker(workerID int) {
	for roomID := range w.workerCh {
		select {
		case <-w.process.Context().Done():
			return
		default:
		}

		if err := w.processRoom(roomID); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"room_id":   roomID,
				"worker_id": workerID,
			}).Warn("[SLIDING_SYNC_METADATA] Failed to process room, will retry")

			// Schedule retry
			w.retryMu.Lock()
			w.retryMap[roomID] = time.Now().Add(time.Minute * 5)
			w.retryMu.Unlock()
		}
	}
}

// tickerLoop periodically checks for rooms needing recalculation and retries failed rooms
func (w *SlidingSyncMetadataWorker) tickerLoop() {
	ticker := time.NewTicker(metadataTickerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.process.Context().Done():
			return
		case <-ticker.C:
			w.processRetries()
			w.checkRecalculateQueue()
		}
	}
}

// processRetries moves due items from retryMap back to workerCh
func (w *SlidingSyncMetadataWorker) processRetries() {
	w.retryMu.Lock()
	now := time.Now()
	var toRetry []string
	for roomID, retryAt := range w.retryMap {
		if now.After(retryAt) {
			toRetry = append(toRetry, roomID)
		}
	}
	for _, roomID := range toRetry {
		delete(w.retryMap, roomID)
	}
	w.retryMu.Unlock()

	for _, roomID := range toRetry {
		w.QueueRoom(roomID)
	}
}

// checkRecalculateQueue checks the database queue for rooms needing recalculation
func (w *SlidingSyncMetadataWorker) checkRecalculateQueue() {
	ctx := w.process.Context()

	rooms, err := w.roomMetadata.SelectRoomsToRecalculate(ctx, nil, metadataBatchSize)
	if err != nil {
		logrus.WithError(err).Warn("[SLIDING_SYNC_METADATA] Failed to check recalculate queue")
		return
	}

	for _, roomID := range rooms {
		w.QueueRoom(roomID)
	}
}

// processRoom calculates and stores metadata for a single room
func (w *SlidingSyncMetadataWorker) processRoom(roomID string) error {
	ctx := w.process.Context()

	snapshot, err := w.db.NewDatabaseSnapshot(ctx)
	if err != nil {
		return err
	}
	defer snapshot.Rollback()

	// Get room metadata from current state
	joinedRoom, err := w.extractRoomMetadata(ctx, snapshot, roomID)
	if err != nil {
		return err
	}

	// Upsert into joined_rooms table
	if err := w.roomMetadata.UpsertJoinedRoom(ctx, nil, joinedRoom); err != nil {
		return err
	}

	// Get all members and create membership snapshots
	if err := w.updateMembershipSnapshots(ctx, snapshot, roomID, joinedRoom); err != nil {
		return err
	}

	// Remove from recalculate queue
	if err := w.roomMetadata.DeleteRoomToRecalculate(ctx, nil, roomID); err != nil {
		logrus.WithError(err).WithField("room_id", roomID).Warn("[SLIDING_SYNC_METADATA] Failed to remove from recalculate queue")
		// Not fatal, continue
	}

	return nil
}

// extractRoomMetadata extracts room metadata from current state events
func (w *SlidingSyncMetadataWorker) extractRoomMetadata(
	ctx context.Context,
	snapshot *shared.DatabaseTransaction,
	roomID string,
) (*tables.SlidingSyncJoinedRoom, error) {
	room := &tables.SlidingSyncJoinedRoom{
		RoomID: roomID,
	}

	// Get latest stream position for this room
	positions, err := snapshot.MaxStreamPositionsForRooms(ctx, []string{roomID})
	if err != nil {
		return nil, err
	}
	if pos, ok := positions[roomID]; ok {
		room.EventStreamOrdering = int64(pos)
		// For now, bump_stamp equals event_stream_ordering
		// TODO: Filter to only "bump" event types (messages, etc.)
		bumpStamp := int64(pos)
		room.BumpStamp = &bumpStamp
	}

	// Get m.room.create for room type
	createEvent, err := snapshot.GetStateEvent(ctx, roomID, "m.room.create", "")
	if err == nil && createEvent != nil {
		var content struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(createEvent.Content(), &content); err == nil {
			room.RoomType = content.Type
		}
	}

	// Get m.room.name
	nameEvent, err := snapshot.GetStateEvent(ctx, roomID, "m.room.name", "")
	if err == nil && nameEvent != nil {
		var content struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(nameEvent.Content(), &content); err == nil {
			room.RoomName = content.Name
		}
	}

	// Check m.room.encryption
	encEvent, err := snapshot.GetStateEvent(ctx, roomID, "m.room.encryption", "")
	room.IsEncrypted = err == nil && encEvent != nil

	// Get m.room.tombstone for successor
	tombstoneEvent, err := snapshot.GetStateEvent(ctx, roomID, "m.room.tombstone", "")
	if err == nil && tombstoneEvent != nil {
		var content struct {
			ReplacementRoom string `json:"replacement_room"`
		}
		if err := json.Unmarshal(tombstoneEvent.Content(), &content); err == nil {
			room.TombstoneSuccessorRoomID = content.ReplacementRoom
		}
	}

	return room, nil
}

// updateMembershipSnapshots updates membership snapshots for all members in a room
func (w *SlidingSyncMetadataWorker) updateMembershipSnapshots(
	ctx context.Context,
	snapshot *shared.DatabaseTransaction,
	roomID string,
	roomMeta *tables.SlidingSyncJoinedRoom,
) error {
	// Get all joined users for this room
	joinedUsers, err := snapshot.AllJoinedUsersInRoom(ctx, []string{roomID})
	if err != nil {
		return err
	}

	users := joinedUsers[roomID]
	for _, userID := range users {
		// Get the member event for this user
		memberEvent, err := snapshot.GetStateEvent(ctx, roomID, "m.room.member", userID)
		if err != nil || memberEvent == nil {
			continue
		}

		var content struct {
			Membership string `json:"membership"`
		}
		if err := json.Unmarshal(memberEvent.Content(), &content); err != nil {
			continue
		}

		membershipSnapshot := &tables.SlidingSyncMembershipSnapshot{
			RoomID:                   roomID,
			UserID:                   userID,
			Sender:                   string(memberEvent.SenderID()),
			MembershipEventID:        memberEvent.EventID(),
			Membership:               content.Membership,
			Forgotten:                false,
			EventStreamOrdering:      roomMeta.EventStreamOrdering,
			HasKnownState:            true,
			RoomType:                 roomMeta.RoomType,
			RoomName:                 roomMeta.RoomName,
			IsEncrypted:              roomMeta.IsEncrypted,
			TombstoneSuccessorRoomID: roomMeta.TombstoneSuccessorRoomID,
		}

		if err := w.roomMetadata.UpsertMembershipSnapshot(ctx, nil, membershipSnapshot); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"room_id": roomID,
				"user_id": userID,
			}).Warn("[SLIDING_SYNC_METADATA] Failed to upsert membership snapshot")
			// Continue with other users
		}
	}

	return nil
}
