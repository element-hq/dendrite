// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package internal

import (
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/sirupsen/logrus"

	"github.com/element-hq/dendrite/internal"
	roomserverAPI "github.com/element-hq/dendrite/roomserver/api"
	"github.com/element-hq/dendrite/roomserver/types"
	"github.com/element-hq/dendrite/setup/process"
)

const (
	partialStateWorkerCount = 4
	// Initial backoff delay after first failure
	partialStateMinBackoff = time.Minute * 1
	// Maximum backoff delay (cap)
	partialStateMaxBackoff = time.Hour * 1
	// Maximum number of retries before giving up on a room
	partialStateMaxRetries = 16
	// Jitter bounds for backoff calculation
	maxJitterMultiplier = 1.4
	minJitterMultiplier = 0.8
)

// roomRetryInfo tracks retry state for a single room
type roomRetryInfo struct {
	retryAt    time.Time
	retryCount uint32
}

// PartialStateWorker handles background resync of rooms with partial state from MSC3706 faster joins.
// After a partial state join, this worker fetches the full room state in the background.
type PartialStateWorker struct {
	process  *process.ProcessContext
	rsAPI    roomserverAPI.FederationRoomserverAPI
	fedAPI   *FederationInternalAPI
	workerCh chan types.RoomNID
	retryMu  sync.Mutex
	retryMap map[types.RoomNID]*roomRetryInfo
}

// NewPartialStateWorker creates a new partial state worker
func NewPartialStateWorker(
	processCtx *process.ProcessContext,
	rsAPI roomserverAPI.FederationRoomserverAPI,
	fedAPI *FederationInternalAPI,
) *PartialStateWorker {
	return &PartialStateWorker{
		process:  processCtx,
		rsAPI:    rsAPI,
		fedAPI:   fedAPI,
		workerCh: make(chan types.RoomNID, 100),
		retryMap: make(map[types.RoomNID]*roomRetryInfo),
	}
}

// backoffDuration calculates the backoff duration for a given retry count using
// exponential backoff with jitter, similar to the federation queue statistics.
func (w *PartialStateWorker) backoffDuration(retryCount uint32) time.Duration {
	// Add jitter to minimize thundering herd effects
	jitter := rand.Float64()*(maxJitterMultiplier-minJitterMultiplier) + minJitterMultiplier

	// Exponential backoff: minBackoff * 2^retryCount, capped at maxBackoff
	backoff := float64(partialStateMinBackoff) * math.Pow(2, float64(retryCount)) * jitter

	duration := time.Duration(backoff)
	if duration > partialStateMaxBackoff {
		duration = partialStateMaxBackoff
	}
	return duration
}

// Start begins the partial state worker, queuing all rooms with partial state for processing
func (w *PartialStateWorker) Start() error {
	// Start worker goroutines
	for i := 0; i < partialStateWorkerCount; i++ {
		go w.worker(i)
	}

	// Start retry goroutine
	go w.retryLoop()

	// Queue all rooms with partial state for processing
	roomNIDs, err := w.rsAPI.GetAllPartialStateRooms(w.process.Context())
	if err != nil {
		logrus.WithError(err).Error("Failed to load partial state rooms on startup")
		return err
	}

	if len(roomNIDs) > 0 {
		logrus.WithField("count", len(roomNIDs)).Info("Queuing partial state rooms for background resync")

		// Stagger the initial queue to avoid thundering herd
		offset := time.Second * 5
		step := time.Second
		if max := len(roomNIDs); max > 60 {
			step = (time.Second * 60) / time.Duration(max)
		}

		for _, roomNID := range roomNIDs {
			roomNID := roomNID
			time.AfterFunc(offset, func() {
				w.QueueRoom(roomNID)
			})
			offset += step
		}
	}

	return nil
}

// QueueRoom adds a room to the queue for partial state processing
func (w *PartialStateWorker) QueueRoom(roomNID types.RoomNID) {
	select {
	case w.workerCh <- roomNID:
	default:
		// Channel full, add to retry map with no retry count increment
		w.retryMu.Lock()
		if _, exists := w.retryMap[roomNID]; !exists {
			w.retryMap[roomNID] = &roomRetryInfo{
				retryAt:    time.Now().Add(time.Second * 30),
				retryCount: 0,
			}
		}
		w.retryMu.Unlock()
	}
}

// worker processes rooms from the channel
func (w *PartialStateWorker) worker(workerID int) {
	for roomNID := range w.workerCh {
		select {
		case <-w.process.Context().Done():
			return
		default:
		}

		if err := w.processRoom(roomNID); err != nil {
			// Get current retry count
			w.retryMu.Lock()
			info, exists := w.retryMap[roomNID]
			if !exists {
				info = &roomRetryInfo{retryCount: 0}
			}
			info.retryCount++

			logger := logrus.WithFields(logrus.Fields{
				"room_nid":    roomNID,
				"worker_id":   workerID,
				"retry_count": info.retryCount,
			})

			// Check if we've exceeded max retries
			if info.retryCount >= partialStateMaxRetries {
				logger.WithError(err).Error("Giving up on partial state resync after max retries")
				// Remove from retry map - we're giving up
				delete(w.retryMap, roomNID)
				w.retryMu.Unlock()
				continue
			}

			// Schedule retry with exponential backoff
			backoff := w.backoffDuration(info.retryCount)
			info.retryAt = time.Now().Add(backoff)
			w.retryMap[roomNID] = info
			w.retryMu.Unlock()

			logger.WithError(err).WithField("retry_in", backoff).Warn("Failed to resync partial state room, will retry with backoff")
		} else {
			// Success - clear retry info
			w.retryMu.Lock()
			delete(w.retryMap, roomNID)
			w.retryMu.Unlock()
		}
	}
}

// retryLoop periodically retries failed rooms
func (w *PartialStateWorker) retryLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-w.process.Context().Done():
			return
		case <-ticker.C:
			w.retryMu.Lock()
			now := time.Now()
			var toRetry []types.RoomNID
			for roomNID, info := range w.retryMap {
				if now.After(info.retryAt) {
					toRetry = append(toRetry, roomNID)
				}
			}
			// Don't delete from retryMap here - the worker will update it on failure
			// or delete it on success. We only need to re-queue the room.
			w.retryMu.Unlock()

			for _, roomNID := range toRetry {
				// Send directly to channel instead of QueueRoom to preserve retry state
				select {
				case w.workerCh <- roomNID:
				default:
					// Channel full, will be picked up on next tick
				}
			}
		}
	}
}

// processRoom fetches the full state for a room with partial state
func (w *PartialStateWorker) processRoom(roomNID types.RoomNID) error {
	// Create a root span for tracing the entire partial state resync
	trace, ctx := internal.StartTask(w.process.Context(), "PartialStateWorker.processRoom")
	defer trace.EndTask()
	trace.SetTag("room_nid", roomNID)

	// MSC3706: Trace resync timing for diagnostics
	resyncStartTime := time.Now()
	logger := logrus.WithFields(logrus.Fields{
		"room_nid": roomNID,
		"trace":    "join_timing",
	})

	// Check if room still has partial state
	hasPartialState, err := w.rsAPI.IsRoomPartialState(ctx, roomNID)
	if err != nil {
		return err
	}
	if !hasPartialState {
		logger.Debug("Room no longer has partial state, skipping")
		return nil
	}

	// Get servers in the room
	servers, err := w.rsAPI.GetPartialStateServers(ctx, roomNID)
	if err != nil {
		return err
	}
	if len(servers) == 0 {
		logger.Warn("No servers found for partial state room")
		return nil
	}

	// Get room ID from room NID
	roomID, err := w.rsAPI.RoomIDFromNID(ctx, roomNID)
	if err != nil {
		logger.WithError(err).Warn("Room not found for partial state room")
		// Clear partial state since room doesn't exist
		_, err = w.rsAPI.ClearRoomPartialState(ctx, roomNID)
		return err
	}

	// Get room info for version
	roomInfo, err := w.rsAPI.RoomInfoByNID(ctx, roomNID)
	if err != nil {
		return err
	}
	if roomInfo == nil {
		logger.Warn("Room info not found for partial state room")
		_, err = w.rsAPI.ClearRoomPartialState(ctx, roomNID)
		return err
	}

	logger = logger.WithField("room_id", roomID)
	trace.SetTag("room_id", roomID)
	logger.Info("Starting partial state resync")

	// Pre-filter servers that are blacklisted or backing off
	// This avoids unnecessary iterations and log noise for known-dead servers
	var availableServers []string
	for _, serverStr := range servers {
		serverName := spec.ServerName(serverStr)
		_, err := w.fedAPI.IsBlacklistedOrBackingOff(serverName)
		if err == nil {
			availableServers = append(availableServers, serverStr)
		} else {
			logger.WithFields(logrus.Fields{
				"server": serverName,
			}).Debug("Skipping server for partial state resync (blacklisted or backing off)")
		}
	}

	if len(availableServers) == 0 {
		logger.Warn("No available servers for partial state resync (all blacklisted or backing off)")
		return nil // Will be retried later via the retry mechanism
	}

	// Try each available server until we succeed
	var lastErr error
	for _, serverStr := range availableServers {
		serverName := spec.ServerName(serverStr)

		// Get the latest events so we can fetch state at that point
		latestEventIDs, _, _, err := w.rsAPI.LatestEventIDs(ctx, roomNID)
		if err != nil {
			lastErr = err
			continue
		}
		if len(latestEventIDs) == 0 {
			logger.Warn("No latest events found")
			continue
		}

		// Fetch state from the remote server
		// We use the first latest event to get state at that point
		lookupStateRegion, _ := internal.StartRegion(ctx, "LookupState")
		lookupStateRegion.SetTag("server", string(serverName))
		lookupStateStartTime := time.Now()
		stateResponse, err := w.fedAPI.LookupState(
			ctx,
			w.fedAPI.cfg.Matrix.ServerName,
			serverName,
			roomID,
			latestEventIDs[0],
			roomInfo.RoomVersion,
		)
		lookupStateRegion.EndRegion()
		if err != nil {
			logger.WithError(err).WithFields(logrus.Fields{
				"server":          serverName,
				"lookup_state_ms": time.Since(lookupStateStartTime).Milliseconds(),
			}).Warn("Failed to fetch state from server")
			lastErr = err
			continue
		}
		logger.WithFields(logrus.Fields{
			"lookup_state_ms": time.Since(lookupStateStartTime).Milliseconds(),
			"server":          serverName,
		}).Debug("LookupState completed for partial state resync")

		// Process the state - the events include member events we were missing
		stateEvents := stateResponse.GetStateEvents()
		authEvents := stateResponse.GetAuthEvents()

		logger.WithFields(logrus.Fields{
			"state_events": len(stateEvents.UntrustedEvents(roomInfo.RoomVersion)),
			"auth_events":  len(authEvents.UntrustedEvents(roomInfo.RoomVersion)),
			"server":       serverName,
		}).Debug("Fetched full state for partial state room")

		// Send the state events to the roomserver as outliers
		// MSC3706: We use SendStateAsOutliers since we're completing a partial state resync
		// and don't have a new event to add, just filling in missing state.
		sendStateRegion, _ := internal.StartRegion(ctx, "SendStateAsOutliers")
		sendStateRegion.SetTag("state_events", len(stateEvents.UntrustedEvents(roomInfo.RoomVersion)))
		sendStateRegion.SetTag("auth_events", len(authEvents.UntrustedEvents(roomInfo.RoomVersion)))
		sendStateStartTime := time.Now()
		if err := roomserverAPI.SendStateAsOutliers(
			ctx,
			w.rsAPI,
			w.fedAPI.cfg.Matrix.ServerName,
			roomID,
			roomInfo.RoomVersion,
			stateResponse,
			serverName,
			nil,
			false,
		); err != nil {
			sendStateRegion.EndRegion()
			logger.WithError(err).WithFields(logrus.Fields{
				"send_state_ms": time.Since(sendStateStartTime).Milliseconds(),
			}).Warn("Failed to send state to roomserver")
			lastErr = err
			continue
		}
		sendStateRegion.EndRegion()
		logger.WithFields(logrus.Fields{
			"send_state_ms": time.Since(sendStateStartTime).Milliseconds(),
		}).Debug("SendStateAsOutliers completed for partial state resync")

		// MSC3706: Update current state and memberships after storing outliers
		// This is the critical step that updates the membership table based on the new state
		updateStateRegion, _ := internal.StartRegion(ctx, "UpdateCurrentStateAfterResync")
		updateStateStartTime := time.Now()
		stateEventsList := stateEvents.UntrustedEvents(roomInfo.RoomVersion)
		stateEventIDs := make([]string, 0, len(stateEventsList))
		for _, ev := range stateEventsList {
			stateEventIDs = append(stateEventIDs, ev.EventID())
		}
		updateStateRegion.SetTag("state_event_count", len(stateEventIDs))
		if err := w.rsAPI.UpdateCurrentStateAfterResync(ctx, roomID, stateEventIDs); err != nil {
			updateStateRegion.EndRegion()
			logger.WithError(err).WithFields(logrus.Fields{
				"update_state_ms": time.Since(updateStateStartTime).Milliseconds(),
			}).Warn("Failed to update current state after resync")
			lastErr = err
			continue
		}
		updateStateRegion.EndRegion()
		logger.WithFields(logrus.Fields{
			"update_state_ms":   time.Since(updateStateStartTime).Milliseconds(),
			"state_event_count": len(stateEventIDs),
		}).Debug("UpdateCurrentStateAfterResync completed")

		// Clear partial state flag - we've successfully synced
		// The returned deviceListStreamID can be used for device list replay (MSC3902)
		clearStateRegion, _ := internal.StartRegion(ctx, "ClearRoomPartialState")
		clearStateStartTime := time.Now()
		deviceListStreamID, err := w.rsAPI.ClearRoomPartialState(ctx, roomNID)
		clearStateRegion.EndRegion()
		if err != nil {
			logger.WithError(err).Error("Failed to clear partial state flag")
			return err
		}

		logger.WithFields(logrus.Fields{
			"device_list_stream_id": deviceListStreamID,
			"clear_state_ms":        time.Since(clearStateStartTime).Milliseconds(),
			"total_resync_ms":       time.Since(resyncStartTime).Milliseconds(),
		}).Debug("Successfully completed partial state resync")

		// Notify observers that this room is no longer in partial state (MSC3706)
		w.rsAPI.NotifyUnPartialStated(roomID)

		// MSC3902 Device List Replay:
		// During partial state, our server may have missed device list updates for users in the room.
		// The deviceListStreamID was recorded when we entered partial state, and now that we've
		// completed the state sync, we should:
		//
		// 1. Query all users who have had device list changes since deviceListStreamID
		//    (using userapi.QueryKeyChanges with Offset=deviceListStreamID)
		// 2. For each user in the room who had device changes:
		//    a. For remote users: Mark their device lists as stale to trigger re-fetch
		//       (using userapi.PerformMarkAsStaleIfNeeded)
		//    b. For local users: Device list changes should already be in the sync stream
		// 3. This ensures clients get accurate device lists for E2EE
		//
		// For now, this is a placeholder - the full implementation requires:
		// - Adding userapi.SyncKeyAPI to the PartialStateWorker
		// - Querying room members to identify which users are in the room
		// - Filtering device changes to only users in this room
		//
		// TODO(MSC3902): Implement full device list replay
		if deviceListStreamID > 0 {
			logger.Debug("Device list replay would use changes since stream position (not yet implemented)")
		}

		return nil
	}

	return lastErr
}
