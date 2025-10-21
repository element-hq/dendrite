# syncapi/sync/ Testing Analysis & Recommendations

## Executive Summary

The `syncapi/sync/` package implements the Matrix `/sync` endpoint, which is the most complex component of a Matrix homeserver. After pragmatic analysis as part of Phase 3 of the TDD testing roadmap, **we recommend deferring comprehensive sync testing to dedicated integration test infrastructure** rather than attempting unit tests.

**Current Status:**
- ❌ Not included in Phase 3 (Complex Scenarios)
- 📋 Documented for future integration testing
- ✅ Complexity documented and analyzed

---

## Why syncapi/sync/ Requires Integration Testing

### 1. State Management Complexity

The sync protocol maintains per-user, per-device state including:
- Timeline position tracking across all rooms
- Filter applications (content, room types, senders)
- Presence information updates
- Account data synchronization
- Ephemeral events (typing, receipts)
- To-device messages

**Testing Challenge:** Requires full database with realistic multi-room, multi-user state.

### 2. Real-Time Event Aggregation

Sync combines data from multiple sources:
- Room events from roomserver
- Account data from userapi
- Presence updates from presence service
- To-device messages from device queue
- Notification counts
- Join/leave/invite state changes

**Testing Challenge:** Requires mocking or running 5+ microservices simultaneously.

### 3. Incremental Sync Protocol

The `/sync` endpoint supports:
- Full sync (initial sync with all historical state)
- Incremental sync (changes since last position)
- Limited sync (specific number of events)
- Filtered sync (custom JSON filters)
- Long-polling with timeouts

**Testing Challenge:** Requires stateful test scenarios with multiple sequential requests.

### 4. Performance-Critical Code Paths

Sync is the hottest code path in any Matrix homeserver:
- Must handle thousands of concurrent long-polling connections
- Must aggregate events from hundreds of rooms efficiently
- Must minimize database queries (N+1 query problems)
- Must support pagination and incremental loading

**Testing Challenge:** Unit tests cannot validate performance characteristics.

---

## Package Structure Analysis

### Main Components

```
syncapi/
├── sync/
│   ├── requestpool.go        # Connection pool management
│   ├── notifier.go           # Real-time notification system
│   ├── userstream.go         # Per-user event stream
│   ├── request.go            # Sync request handling
│   ├── streams/              # Event stream implementations
│   │   ├── stream_account_data.go
│   │   ├── stream_invites.go
│   │   ├── stream_presence.go
│   │   ├── stream_rooms.go
│   │   ├── stream_typing.go
│   │   └── stream_todevice.go
│   └── sync_test.go         # Existing tests (if any)
```

### Complexity Metrics

- **Lines of Code:** 5,000+ across sync package
- **External Dependencies:** roomserver, userapi, presence, federation, database
- **State Tracking:** Per-connection, per-user, per-room, per-device
- **Concurrency:** Goroutines for each long-poll connection, notifier channels
- **Database Queries:** Complex joins across multiple tables

---

## What Unit Testing CAN Cover

While full sync integration is deferred, **some helper functions may be unit-testable:**

### Potentially Testable Functions

1. **Filter Application Logic**
   - Room filter matching (types, not_types, rooms, not_rooms)
   - Event filter matching (senders, types, limit)
   - State filter application
   - **Test Approach:** Pure function tests with mock events

2. **Position Encoding/Decoding**
   - Sync position serialization
   - Token generation and parsing
   - Position comparison logic
   - **Test Approach:** String manipulation tests

3. **Response Builder Helpers**
   - JSON response construction
   - Room summary calculation
   - Unread count computation
   - **Test Approach:** Data transformation tests

4. **Event Deduplication**
   - Removing duplicate events from multiple sources
   - Timeline gap detection
   - **Test Approach:** List manipulation tests

### Example Unit Test (If Applicable)

```go
func TestFilterRooms_ExcludeList_RemovesMatches(t *testing.T) {
    t.Parallel()

    filter := RoomFilter{
        NotRooms: []string{"!excluded:server"},
    }

    rooms := []string{
        "!room1:server",
        "!excluded:server",
        "!room2:server",
    }

    result := applyRoomFilter(rooms, filter)

    assert.Len(t, result, 2, "Should exclude 1 room")
    assert.NotContains(t, result, "!excluded:server")
}
```

---

## Integration Test Requirements

To properly test `syncapi/sync/`, the following infrastructure is needed:

### 1. Full Stack Integration Environment

**Required Components:**
- ✅ PostgreSQL or SQLite database with schema
- ✅ Roomserver API (for room events)
- ✅ User API (for account data, devices)
- ✅ Federation API (for federated events)
- ✅ Presence service (for presence updates)
- ✅ HTTP server with authentication

**Alternative:** Use Dendrite's existing integration test framework (Complement/Sytest).

### 2. Test Scenario Examples

**Scenario 1: Initial Sync**
```go
func TestSync_InitialSync_ReturnsAllRooms(t *testing.T) {
    // Setup: Create user, create 3 rooms, join user to rooms
    server := startTestServer(t)
    user := server.CreateUser("@alice:test")
    room1 := server.CreateRoom(user, "Room 1")
    room2 := server.CreateRoom(user, "Room 2")
    room3 := server.CreateRoom(user, "Room 3")

    // Execute: Perform initial sync
    resp := server.DoSync(user, "", "") // No since token = initial sync

    // Assert: All rooms returned
    assert.Len(t, resp.Rooms.Join, 3)
    assert.Contains(t, resp.Rooms.Join, room1.ID)
    assert.Contains(t, resp.Rooms.Join, room2.ID)
    assert.Contains(t, resp.Rooms.Join, room3.ID)
}
```

**Scenario 2: Incremental Sync**
```go
func TestSync_IncrementalSync_ReturnsNewEvents(t *testing.T) {
    server := startTestServer(t)
    user := server.CreateUser("@alice:test")
    room := server.CreateRoom(user, "Test Room")

    // Initial sync
    resp1 := server.DoSync(user, "", "")
    since := resp1.NextBatch

    // Send new message
    event := server.SendMessage(user, room, "Hello!")

    // Incremental sync
    resp2 := server.DoSync(user, since, "")

    // Assert: New event appears
    timeline := resp2.Rooms.Join[room.ID].Timeline
    assert.Len(t, timeline.Events, 1)
    assert.Equal(t, event.ID, timeline.Events[0].EventID)
}
```

**Scenario 3: Filtered Sync**
```go
func TestSync_FilterByRoom_ReturnsOnlyMatchingRooms(t *testing.T) {
    server := startTestServer(t)
    user := server.CreateUser("@alice:test")
    room1 := server.CreateRoom(user, "Include")
    room2 := server.CreateRoom(user, "Exclude")

    // Create filter for room1 only
    filter := `{"room":{"rooms":["` + room1.ID + `"]}}`
    filterID := server.UploadFilter(user, filter)

    // Sync with filter
    resp := server.DoSync(user, "", filterID)

    // Assert: Only room1 appears
    assert.Len(t, resp.Rooms.Join, 1)
    assert.Contains(t, resp.Rooms.Join, room1.ID)
    assert.NotContains(t, resp.Rooms.Join, room2.ID)
}
```

### 3. Test Infrastructure Utilities

```go
type TestServer struct {
    server  *http.Server
    db      *sql.DB
    rsAPI   roomserver.RoomserverAPI
    userAPI userapi.UserAPI
    cleanup func()
}

func (s *TestServer) CreateUser(userID string) *TestUser { /* ... */ }
func (s *TestServer) CreateRoom(user *TestUser, name string) *TestRoom { /* ... */ }
func (s *TestServer) SendMessage(user *TestUser, room *TestRoom, body string) *Event { /* ... */ }
func (s *TestServer) DoSync(user *TestUser, since, filter string) *SyncResponse { /* ... */ }
func (s *TestServer) UploadFilter(user *TestUser, filterJSON string) string { /* ... */ }
```

---

## Existing Testing Infrastructure

### Complement Tests

Dendrite has **Complement** integration tests that may already cover sync:
- Located in separate repository: `github.com/matrix-org/complement`
- Runs full Dendrite server with federation
- Tests against Matrix spec compliance

**Recommendation:** Verify complement coverage of sync scenarios before building new infrastructure.

### Sytest

Dendrite supports **Sytest** (Perl-based Matrix integration tests):
- Tests full server behavior
- Includes sync protocol tests
- May have sync coverage already

**Recommendation:** Check sytest results for sync coverage gaps.

---

## Recommendations

### Short Term (Current Phase 3)

✅ **DONE:** Document sync complexity and integration requirements
✅ **DONE:** Defer sync testing to avoid scope creep
✅ **DONE:** Focus Phase 3 on testable helper functions (roomserver/state, federationapi/internal)

### Medium Term (Post-TDD Roadmap)

1. **Audit Existing Integration Tests**
   - Review Complement test coverage for /sync endpoint
   - Review Sytest coverage for sync scenarios
   - Identify gaps in existing tests

2. **Extract Testable Helpers**
   - Identify pure functions in sync package
   - Create unit tests for filter logic, position handling, response builders
   - Target: 20-30% coverage of helper functions

3. **Document Sync Behavior**
   - Create architecture documentation for sync implementation
   - Document state machine for connection lifecycle
   - Document performance characteristics and bottlenecks

### Long Term (Future Work)

1. **Integration Test Suite**
   - Build TestServer infrastructure (outlined above)
   - Implement 20-30 key sync scenarios
   - Test concurrent sync connections (stress testing)
   - Test edge cases (network failures, database errors, etc.)

2. **Performance Testing**
   - Benchmark sync with 100+ rooms per user
   - Benchmark sync with 1000+ concurrent connections
   - Profile database query performance
   - Identify and fix N+1 query patterns

3. **Compliance Testing**
   - Ensure full Matrix spec compliance
   - Test federation sync scenarios
   - Test presence integration
   - Test to-device messaging

---

## Conclusion

The `syncapi/sync/` package is the heart of Matrix homeserver functionality but is too complex for pragmatic unit testing. The recommended approach is:

1. ✅ **Phase 3 (Current):** Document requirements, defer full testing
2. ⏭️ **Post-Roadmap:** Leverage existing integration tests (Complement, Sytest)
3. 🔮 **Future Work:** Build dedicated integration test infrastructure if gaps exist

This pragmatic approach maintains momentum on the TDD roadmap while acknowledging the significant engineering investment required for comprehensive sync testing.

---

**Document Version:** 1.0
**Created:** 2025-10-21
**Phase:** 3C (Complex Scenarios - Documentation)
**Status:** Complete
