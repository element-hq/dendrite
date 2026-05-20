// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package internal

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// DefaultAwaitTimeout is the default timeout for awaiting full state
const DefaultAwaitTimeout = 5 * time.Minute

// PartialStateTracker tracks rooms in partial state and allows callers to wait
// for a room to complete its partial state resync. This is used by operations
// that require full room state (MSC3706).
type PartialStateTracker struct {
	// roomObservers tracks waiting channels for each room
	// map[roomID][]chan struct{}
	roomObservers map[string][]chan struct{}
	mu            sync.Mutex
}

// NewPartialStateTracker creates a new PartialStateTracker
func NewPartialStateTracker() *PartialStateTracker {
	return &PartialStateTracker{
		roomObservers: make(map[string][]chan struct{}),
	}
}

// AwaitFullState blocks until the room has full state or the context is cancelled.
// If the room is not in partial state, this returns immediately.
// Returns an error if the context is cancelled or times out.
func (t *PartialStateTracker) AwaitFullState(ctx context.Context, roomID string) error {
	// Create a channel to wait on
	ch := make(chan struct{})

	t.mu.Lock()
	t.roomObservers[roomID] = append(t.roomObservers[roomID], ch)
	t.mu.Unlock()

	// Ensure we clean up on exit
	defer func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		observers := t.roomObservers[roomID]
		for i, observer := range observers {
			if observer == ch {
				// Remove this observer
				t.roomObservers[roomID] = append(observers[:i], observers[i+1:]...)
				break
			}
		}
		// Clean up empty observer lists
		if len(t.roomObservers[roomID]) == 0 {
			delete(t.roomObservers, roomID)
		}
	}()

	logrus.WithField("room_id", roomID).Debug("Awaiting full state for room")

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		logrus.WithField("room_id", roomID).Debug("Room full state complete")
		return nil
	}
}

// AwaitFullStateWithTimeout is a convenience wrapper that adds a timeout to the context
func (t *PartialStateTracker) AwaitFullStateWithTimeout(ctx context.Context, roomID string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return t.AwaitFullState(ctx, roomID)
}

// NotifyUnPartialStated is called when a room completes its partial state resync.
// This wakes up all callers waiting in AwaitFullState for this room.
func (t *PartialStateTracker) NotifyUnPartialStated(roomID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	observers, ok := t.roomObservers[roomID]
	if !ok || len(observers) == 0 {
		return
	}

	logrus.WithFields(logrus.Fields{
		"room_id":       roomID,
		"observer_count": len(observers),
	}).Debug("Notifying observers that room is no longer partial state")

	// Close all waiting channels to wake up waiters
	for _, ch := range observers {
		close(ch)
	}

	// Clear the observers list
	delete(t.roomObservers, roomID)
}

// PendingRoomCount returns the number of rooms with pending observers
func (t *PartialStateTracker) PendingRoomCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.roomObservers)
}

// HasObservers returns true if there are any observers waiting for this room
func (t *PartialStateTracker) HasObservers(roomID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.roomObservers[roomID]) > 0
}
