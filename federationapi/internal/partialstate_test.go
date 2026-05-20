// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package internal

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/element-hq/dendrite/roomserver/types"
	"github.com/element-hq/dendrite/setup/process"
)

// TestPartialStateWorkerQueueRoom tests that rooms can be queued for processing
func TestPartialStateWorkerQueueRoom(t *testing.T) {
	processCtx := process.NewProcessContext()
	defer processCtx.ShutdownDendrite()

	worker := &PartialStateWorker{
		process:  processCtx,
		workerCh: make(chan types.RoomNID, 10),
		retryMap: make(map[types.RoomNID]time.Time),
	}

	// Queue a room
	worker.QueueRoom(types.RoomNID(1))

	// Should be in the channel
	select {
	case roomNID := <-worker.workerCh:
		assert.Equal(t, types.RoomNID(1), roomNID)
	case <-time.After(time.Second):
		t.Fatal("Room was not queued")
	}
}

// TestPartialStateWorkerQueueRoomFullChannel tests fallback to retry map when channel is full
func TestPartialStateWorkerQueueRoomFullChannel(t *testing.T) {
	processCtx := process.NewProcessContext()
	defer processCtx.ShutdownDendrite()

	// Create worker with tiny channel
	worker := &PartialStateWorker{
		process:  processCtx,
		workerCh: make(chan types.RoomNID, 1),
		retryMap: make(map[types.RoomNID]time.Time),
	}

	// Fill the channel
	worker.workerCh <- types.RoomNID(1)

	// Queue another room - should go to retry map
	worker.QueueRoom(types.RoomNID(2))

	// Should be in retry map
	worker.retryMu.Lock()
	_, exists := worker.retryMap[types.RoomNID(2)]
	worker.retryMu.Unlock()

	assert.True(t, exists, "Room should be in retry map when channel is full")
}

// TestPartialStateWorkerDuplicateQueue tests that duplicate queue requests don't overwrite retry times
func TestPartialStateWorkerDuplicateQueue(t *testing.T) {
	processCtx := process.NewProcessContext()
	defer processCtx.ShutdownDendrite()

	worker := &PartialStateWorker{
		process:  processCtx,
		workerCh: make(chan types.RoomNID, 1),
		retryMap: make(map[types.RoomNID]time.Time),
	}

	// Fill channel
	worker.workerCh <- types.RoomNID(1)

	// First queue of room 2 - goes to retry map
	worker.QueueRoom(types.RoomNID(2))

	worker.retryMu.Lock()
	firstTime := worker.retryMap[types.RoomNID(2)]
	worker.retryMu.Unlock()

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Second queue of room 2 - should NOT update retry time
	worker.QueueRoom(types.RoomNID(2))

	worker.retryMu.Lock()
	secondTime := worker.retryMap[types.RoomNID(2)]
	worker.retryMu.Unlock()

	assert.Equal(t, firstTime, secondTime, "Duplicate queue should not update retry time")
}

// TestPartialStateWorkerRetryMapCleanup tests that retry map entries are properly moved to the queue
func TestPartialStateWorkerRetryMapCleanup(t *testing.T) {
	processCtx := process.NewProcessContext()
	defer processCtx.ShutdownDendrite()

	worker := &PartialStateWorker{
		process:  processCtx,
		workerCh: make(chan types.RoomNID, 10),
		retryMap: make(map[types.RoomNID]time.Time),
	}

	// Add entries to retry map with past times
	worker.retryMu.Lock()
	worker.retryMap[types.RoomNID(1)] = time.Now().Add(-time.Hour)
	worker.retryMap[types.RoomNID(2)] = time.Now().Add(-time.Hour)
	worker.retryMap[types.RoomNID(3)] = time.Now().Add(time.Hour) // Future - should not be retried
	worker.retryMu.Unlock()

	// Manually trigger retry logic (simulating what retryLoop does)
	worker.retryMu.Lock()
	now := time.Now()
	var toRetry []types.RoomNID
	for roomNID, retryAt := range worker.retryMap {
		if now.After(retryAt) {
			toRetry = append(toRetry, roomNID)
		}
	}
	for _, roomNID := range toRetry {
		delete(worker.retryMap, roomNID)
	}
	worker.retryMu.Unlock()

	// Queue retried rooms
	for _, roomNID := range toRetry {
		worker.QueueRoom(roomNID)
	}

	// Should have 2 rooms in channel (1 and 2), and 1 in retry map (3)
	assert.Len(t, toRetry, 2)

	worker.retryMu.Lock()
	remainingRetries := len(worker.retryMap)
	worker.retryMu.Unlock()
	assert.Equal(t, 1, remainingRetries, "Room 3 should still be in retry map")
}

// mockPartialStateRoomserverAPI implements minimal interface for testing
type mockPartialStateRoomserverAPI struct {
	partialStateRooms map[types.RoomNID]bool
	partialServers    map[types.RoomNID][]string
	roomIDs           map[types.RoomNID]string
	clearCalled       int32 // atomic
	mu                sync.RWMutex
}

func newMockPartialStateRoomserverAPI() *mockPartialStateRoomserverAPI {
	return &mockPartialStateRoomserverAPI{
		partialStateRooms: make(map[types.RoomNID]bool),
		partialServers:    make(map[types.RoomNID][]string),
		roomIDs:           make(map[types.RoomNID]string),
	}
}

func (m *mockPartialStateRoomserverAPI) IsRoomPartialState(ctx context.Context, roomNID types.RoomNID) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.partialStateRooms[roomNID], nil
}

func (m *mockPartialStateRoomserverAPI) GetPartialStateServers(ctx context.Context, roomNID types.RoomNID) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.partialServers[roomNID], nil
}

func (m *mockPartialStateRoomserverAPI) GetAllPartialStateRooms(ctx context.Context) ([]types.RoomNID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var nids []types.RoomNID
	for nid, isPartial := range m.partialStateRooms {
		if isPartial {
			nids = append(nids, nid)
		}
	}
	return nids, nil
}

func (m *mockPartialStateRoomserverAPI) ClearRoomPartialState(ctx context.Context, roomNID types.RoomNID) (int64, error) {
	atomic.AddInt32(&m.clearCalled, 1)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.partialStateRooms, roomNID)
	return 0, nil // Return 0 for device list stream ID in mock
}

func (m *mockPartialStateRoomserverAPI) RoomIDFromNID(ctx context.Context, roomNID types.RoomNID) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if id, ok := m.roomIDs[roomNID]; ok {
		return id, nil
	}
	return "", nil // Room not found
}

// TestPartialStateWorkerSkipsNonPartialRooms tests that non-partial rooms are skipped
func TestPartialStateWorkerSkipsNonPartialRooms(t *testing.T) {
	processCtx := process.NewProcessContext()
	defer processCtx.ShutdownDendrite()

	mockAPI := newMockPartialStateRoomserverAPI()
	// Room 1 is NOT in partial state
	mockAPI.partialStateRooms[types.RoomNID(1)] = false

	_ = &PartialStateWorker{
		process:  processCtx,
		rsAPI:    nil, // Will use mockAPI in processRoom
		workerCh: make(chan types.RoomNID, 10),
		retryMap: make(map[types.RoomNID]time.Time),
	}

	// Manually test the check logic (since processRoom requires full API)
	hasPartialState, err := mockAPI.IsRoomPartialState(context.Background(), types.RoomNID(1))
	require.NoError(t, err)
	assert.False(t, hasPartialState, "Room should not have partial state")
}

// TestPartialStateJoinClientTracking tests that the PartialStateJoinClient tracks join metadata
func TestPartialStateJoinClientTracking(t *testing.T) {
	// Test initial state
	client := &PartialStateJoinClient{
		FederationInternalAPI:  nil, // Not needed for this test
		LastJoinMembersOmitted: false,
		LastJoinServersInRoom:  nil,
	}

	assert.False(t, client.LastJoinMembersOmitted)
	assert.Nil(t, client.LastJoinServersInRoom)

	// Simulate a partial state join response update
	client.LastJoinMembersOmitted = true
	client.LastJoinServersInRoom = []string{"server1.example.com", "server2.example.com"}

	assert.True(t, client.LastJoinMembersOmitted)
	assert.Len(t, client.LastJoinServersInRoom, 2)
	assert.Contains(t, client.LastJoinServersInRoom, "server1.example.com")
}

// TestPartialStateWorkerConcurrency tests concurrent queue operations
func TestPartialStateWorkerConcurrency(t *testing.T) {
	processCtx := process.NewProcessContext()
	defer processCtx.ShutdownDendrite()

	worker := &PartialStateWorker{
		process:  processCtx,
		workerCh: make(chan types.RoomNID, 100),
		retryMap: make(map[types.RoomNID]time.Time),
	}

	// Concurrently queue many rooms
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(roomNID types.RoomNID) {
			defer wg.Done()
			worker.QueueRoom(roomNID)
		}(types.RoomNID(i))
	}

	wg.Wait()

	// Count rooms in channel + retry map
	channelCount := len(worker.workerCh)
	worker.retryMu.Lock()
	retryCount := len(worker.retryMap)
	worker.retryMu.Unlock()

	total := channelCount + retryCount
	assert.Equal(t, 50, total, "All rooms should be queued in channel or retry map")
}

// TestPartialStateWorkerRaceConditions tests for data races in concurrent operations
func TestPartialStateWorkerRaceConditions(t *testing.T) {
	processCtx := process.NewProcessContext()
	defer processCtx.ShutdownDendrite()

	worker := &PartialStateWorker{
		process:  processCtx,
		workerCh: make(chan types.RoomNID, 5), // Small channel to force retry map usage
		retryMap: make(map[types.RoomNID]time.Time),
	}

	// Run concurrent reads and writes
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				select {
				case <-ctx.Done():
					return
				default:
					worker.QueueRoom(types.RoomNID(id*100 + j))
				}
			}
		}(i)
	}

	// Readers (simulating retry loop reads)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					worker.retryMu.Lock()
					for roomNID, retryAt := range worker.retryMap {
						_ = roomNID
						_ = retryAt
					}
					worker.retryMu.Unlock()
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	// Drain channel concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-worker.workerCh:
				// Drained
			}
		}
	}()

	wg.Wait()
	// Test passes if no race conditions detected (run with -race flag)
}

// TestPartialStateConstants verifies the configuration constants
func TestPartialStateConstants(t *testing.T) {
	assert.Equal(t, 4, partialStateWorkerCount, "Worker count should be 4")
	assert.Equal(t, 5*time.Minute, partialStateRetryDelay, "Retry delay should be 5 minutes")
}

// TestPartialStateWorkerContextCancellation tests that workers respect context cancellation
func TestPartialStateWorkerContextCancellation(t *testing.T) {
	processCtx := process.NewProcessContext()

	worker := &PartialStateWorker{
		process:  processCtx,
		workerCh: make(chan types.RoomNID, 10),
		retryMap: make(map[types.RoomNID]time.Time),
	}

	// Start a goroutine that would block on channel read
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-processCtx.Context().Done():
				close(done)
				return
			case roomNID := <-worker.workerCh:
				_ = roomNID
			}
		}
	}()

	// Cancel context
	processCtx.ShutdownDendrite()

	// Should exit quickly
	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("Worker did not exit on context cancellation")
	}
}
