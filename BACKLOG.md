# Deferred Tasks Backlog

This document tracks tasks that were analyzed but deferred due to architectural decisions or blockers requiring human review.

---

## Task #7: Room History Purge (P0 - GDPR Critical)

**Status:** DEFERRED
**Priority:** P0 (Critical - GDPR "right to erasure")
**Effort:** M/L (1-2 weeks depending on approach)
**Deferred Date:** 2025-10-22

### Overview

Implement selective room history purge endpoint for GDPR compliance and content moderation.

**Required Endpoint:** `POST /_dendrite/admin/v1/purge_history/{roomID}`

**Request Body:**
```json
{
  "before_ts": 1234567890,        // Purge events before this timestamp
  "user_id": "@alice:example.com", // Purge only events from this user
  "event_ids": ["$event1", "$event2"], // Purge specific event IDs
  "method": "redact",             // "redact" (keep metadata) or "delete" (hard delete)
  "reason": "GDPR erasure request"
}
```

**Response:**
```json
{
  "purged_count": 150,
  "method": "redact",
  "status": "completed",
  "warning": "Purge is local only. Remote servers may still have these events."
}
```

### Why Deferred

**Blocker:** Lacks storage-layer primitives to efficiently filter events by timestamp or sender.

**Current Database Structure:**
- `roomserver_events` table has NO `origin_server_ts` or `sender` columns
- Timestamp and sender data exists ONLY inside `roomserver_event_json.event_json` (unparsed JSON TEXT blobs)
- No indexes on these fields
- Filtering requires parsing potentially millions of JSON documents for large rooms
- This would be catastrophically slow (minutes for large rooms)

**What Already Works:**
- `event_ids` filtering via `EventsFromIDs` (can implement partial feature)
- Full room purge via existing `PurgeRoom` method

### Implementation Options

#### Option A: Proper Schema Migration (RECOMMENDED)

**Effort:** M (1-2 weeks)
**Approach:** Add indexed `origin_server_ts` and `sender` columns to `roomserver_events`

**Implementation Steps:**
1. Create PostgreSQL/SQLite migrations:
   ```sql
   ALTER TABLE roomserver_events
   ADD COLUMN origin_server_ts BIGINT,
   ADD COLUMN sender TEXT;

   CREATE INDEX roomserver_events_timestamp_idx
   ON roomserver_events(room_nid, origin_server_ts);

   CREATE INDEX roomserver_events_sender_idx
   ON roomserver_events(room_nid, sender);
   ```

2. Backfill existing events:
   ```go
   // For each room:
   //   SELECT event_nid, event_json FROM roomserver_event_json
   //   Parse JSON, extract origin_server_ts and sender
   //   UPDATE roomserver_events SET origin_server_ts=?, sender=? WHERE event_nid=?
   ```

3. Modify `StoreEvent` to populate columns on insert

4. Implement fast indexed queries:
   ```go
   func SelectEventNIDsByTimestamp(ctx, roomNID, beforeTS) ([]EventNID, error)
   func SelectEventNIDsBySender(ctx, roomNID, userID) ([]EventNID, error)
   ```

**Pros:**
- ✅ Production-grade performance (milliseconds vs minutes)
- ✅ Scalable to millions of events
- ✅ Matches how production systems implement this
- ✅ Clean, maintainable code
- ✅ Enables future features (analytics, search)

**Cons:**
- ❌ Requires schema migration approval
- ❌ Backfill could take time on existing large rooms
- ❌ More complex to implement and test

**Performance:** O(log n) indexed queries

---

#### Option B: JSON Parsing Approach (Quick & Dirty)

**Effort:** S (2-3 days)
**Approach:** Load all room events, parse JSON in memory, filter, delete

**Implementation:**
```go
func PurgeHistoryByTimestamp(roomNID, beforeTS) error {
    // 1. SELECT all event_nids for room
    eventNIDs := SelectEventNIDs(roomNID)

    // 2. Load all event JSON
    events := BulkSelectEventJSON(eventNIDs)

    // 3. Parse JSON, filter
    var toDelete []EventNID
    for _, event := range events {
        var parsed struct {
            OriginServerTS int64 `json:"origin_server_ts"`
        }
        json.Unmarshal(event.JSON, &parsed)
        if parsed.OriginServerTS < beforeTS {
            toDelete = append(toDelete, event.NID)
        }
    }

    // 4. Delete filtered events
    DeleteEvents(toDelete)
}
```

**Pros:**
- ✅ No schema changes needed
- ✅ Fast to implement
- ✅ Works immediately

**Cons:**
- ❌ EXTREMELY slow for large rooms (10,000+ events = minutes)
- ❌ High memory usage (load all events into RAM)
- ❌ Not production-ready
- ❌ Poor user experience for operators
- ❌ Could timeout or OOM on very large rooms

**Performance:** O(n) full table scan + JSON parsing

---

#### Option C: MVP with event_ids Only (Partial Implementation)

**Effort:** S (2-3 days)
**Approach:** Implement only `event_ids` filtering, defer timestamp/sender

**Implementation:**
- API endpoint accepts only `event_ids[]` parameter (reject others)
- Use existing `EventsFromIDs` + deletion logic
- Document limitations in API docs and warning message

**Pros:**
- ✅ Delivers partial GDPR compliance (can delete specific events)
- ✅ No schema changes
- ✅ Fast implementation
- ✅ Can extend later with Option A or D

**Cons:**
- ❌ Incomplete feature (70% solution)
- ❌ Can't purge "all events before date"
- ❌ Can't purge "all events from user X" (major GDPR gap)
- ❌ Operators must identify event IDs manually

**Performance:** O(k) where k = number of event IDs provided

---

#### Option D: Hybrid (Timestamp Column Only)

**Effort:** S/M (1 week)
**Approach:** Add ONLY `origin_server_ts` column, use JSON parsing for sender

**Implementation:**
1. Add `origin_server_ts BIGINT` column + index
2. Backfill from existing event JSON
3. Fast indexed timestamp filtering (80% use case)
4. Slow JSON parsing for sender filtering (acceptable - rare operation)

**Migration:**
```sql
ALTER TABLE roomserver_events ADD COLUMN origin_server_ts BIGINT;
CREATE INDEX roomserver_events_timestamp_idx ON roomserver_events(room_nid, origin_server_ts);
```

**Pros:**
- ✅ Solves 80% use case (purge before date)
- ✅ Smaller migration than Option A
- ✅ Acceptable performance for sender (rare operation)
- ✅ Can add sender column later if needed

**Cons:**
- ❌ Still requires schema migration
- ❌ Inconsistent performance (fast for time, slow for sender)
- ❌ Sender filtering still problematic for large rooms

**Performance:**
- By timestamp: O(log n) - fast
- By sender: O(n) - slow but acceptable

---

### Recommendation

**Implement Option A (Proper Schema Migration)** when ready to tackle this task.

**Rationale:**
- Task #7 is P0 (GDPR Critical) - must be done right
- Production deployments need good performance
- Options B/C/D create technical debt
- One-time migration cost is worth long-term benefits
- Enables future features (event search, analytics)

**Alternative:** If schema migrations are blocked, implement Option C as interim solution and plan Option A for later.

### Prerequisites Before Implementation

1. **Architectural approval** for schema changes
2. **Migration strategy** for existing large rooms
3. **Backfill performance testing** on development instance
4. **Downtime requirements** (if any) for migration

### Effort Breakdown (Option A)

- **Schema design & migration files:** 1 day
- **Backfill logic & testing:** 2 days
- **Storage layer queries:** 1 day
- **TDD Cycle 1 - Roomserver logic:** 2 days
- **TDD Cycle 2 - API handler:** 1 day
- **Quality gate & testing:** 1 day
- **Total:** 8-10 days

### Related Tasks

- Similar to how Task #10 (Thread Notifications) adds `thread_root_event_id` column
- Can reuse patterns from Task #6 (Media Quarantine) for selective deletion

### References

- Existing `PurgeRoom` implementation: `roomserver/storage/postgres/purge_statements.go`
- Storage interface: `roomserver/storage/interface.go:171`
- Feature parity doc: `/Users/user/Downloads/dendrite-synapse-feature-parity-pack-v2/feature_parity_markdown_v2/09a-Admin-APIs.md`

---

---

## Task #6b: Room-Level Media Quarantine (Enhancement)

**Status:** DEFERRED
**Priority:** P2 (Enhancement to completed Task #6)
**Effort:** M (1 week)
**Deferred Date:** 2025-10-22

### Overview

Implement room-level media quarantine endpoint to quarantine all media referenced in a room's event history.

**Required Endpoint:** `POST /_dendrite/admin/v1/media/quarantine/room/{roomID}`

**Request Body:**
```json
{
  "reason": "CSAM content reported in room"
}
```

**Response:**
```json
{
  "quarantined_count": 47,
  "media_ids": ["$mediaId1", "$mediaId2", "..."],
  "status": "completed"
}
```

**Use case:** Quickly quarantine all media in a room after discovering illegal content or abuse.

### Why Deferred

**Blocker:** No efficient way to discover which media files are referenced in a room's events.

**Current limitation:**
- Media metadata has no `room_id` column in database
- Media URLs (`mxc://server/mediaID`) exist only in parsed event JSON content
- Would require loading all room events and parsing JSON (slow for large rooms)
- For a room with 10,000 events, this could take minutes instead of milliseconds

**What already works (90% of Task #6 complete):**
- ✅ Single media quarantine (by media ID): `POST /_dendrite/admin/v1/media/quarantine/{server}/{mediaID}`
- ✅ User-level quarantine (all media from a user): `POST /_dendrite/admin/v1/media/quarantine/user/{userID}`
- ✅ Unquarantine: `DELETE /_dendrite/admin/v1/media/quarantine/{server}/{mediaID}`
- ✅ Download blocking: Quarantined media returns 404
- ✅ Audit trail: `quarantined_by`, `quarantined_at` timestamps

**Current endpoint behavior:**
- Returns `501 Not Implemented` with helpful error message
- Suggests workarounds (user-level quarantine or individual media quarantine)

### Implementation Options

#### Option A: Event Content Indexing (RECOMMENDED)

**Effort:** M (1 week)

**Approach:** Add media→room mapping during event processing

**Implementation Steps:**

1. **Schema migration** - Add media-room reference table:
   ```sql
   CREATE TABLE media_room_references (
       media_id TEXT NOT NULL,
       server_name TEXT NOT NULL,
       room_id TEXT NOT NULL,
       event_id TEXT NOT NULL,
       sender TEXT NOT NULL,
       created_at BIGINT NOT NULL,
       PRIMARY KEY (media_id, server_name, room_id),
       INDEX media_room_idx (room_id)
   );
   ```

2. **Event processing hook** - Extract media URLs when storing events:
   ```go
   // In roomserver/internal/input/input_events.go
   func extractMediaReferences(event gomatrixserverlib.PDU) []MediaRef {
       // Parse event content for mxc:// URLs
       // Extract server_name and media_id from mxc://server/mediaID
       // Return list of references
   }
   ```

3. **Populate on insert** - Store references during event processing:
   ```go
   func StoreMediaReferences(ctx, roomID, eventID, sender, refs)
   ```

4. **Fast indexed queries** for quarantine:
   ```go
   func SelectMediaByRoom(ctx, roomID) ([]MediaID, error) {
       // SELECT media_id, server_name FROM media_room_references WHERE room_id = ?
   }
   ```

5. **Backfill existing rooms** (optional, for historical data):
   ```go
   // Batch job to parse existing events and populate table
   ```

**Pros:**
- ✅ Production-grade performance (milliseconds, not minutes)
- ✅ Scalable to millions of events
- ✅ Matches how production systems implement this (Synapse uses similar approach)
- ✅ Clean, maintainable code
- ✅ Enables future features (media analytics, usage reports, orphan cleanup)
- ✅ Can query "which rooms use this media?" (useful for moderation)

**Cons:**
- ❌ Requires schema migration approval
- ❌ Backfill could take time on existing large rooms (can be done async)
- ❌ More complex to implement and test
- ❌ Adds storage overhead (small - one row per media per room)

**Performance:** O(log n) indexed queries

---

#### Option B: On-Demand Event Parsing (Quick & Dirty)

**Effort:** S (2-3 days)

**Approach:** Parse room events at quarantine time (no schema changes)

**Implementation:**
```go
func QuarantineMediaInRoom(ctx, roomID, quarantinedBy) (int, error) {
    // 1. Load all events in room from roomserver
    events := roomserverAPI.QueryEventsInRoom(ctx, roomID)

    // 2. Parse each event's JSON content
    var mediaIDs []MediaID
    for _, event := range events {
        // Regex to find mxc:// URLs in event content
        matches := regexp.FindAll(`mxc://([^/]+)/([^"\\s]+)`, event.JSON)
        for _, match := range matches {
            mediaIDs = append(mediaIDs, MediaID{
                ServerName: match[1],
                MediaID: match[2],
            })
        }
    }

    // 3. Quarantine each found media ID
    for _, media := range mediaIDs {
        QuarantineMedia(ctx, media.ServerName, media.MediaID, quarantinedBy)
    }

    return len(mediaIDs), nil
}
```

**Pros:**
- ✅ No schema changes needed
- ✅ Fast to implement (2-3 days)
- ✅ Works immediately

**Cons:**
- ❌ EXTREMELY slow for large rooms (10,000+ events = minutes)
- ❌ High memory usage (load all events into RAM)
- ❌ Not production-ready for busy servers
- ❌ Poor user experience for operators (timeout risk)
- ❌ Could timeout or OOM on very large rooms
- ❌ Scales poorly (O(n) full scan of all events)

**Performance:** O(n) full table scan + JSON parsing

---

#### Option C: Hybrid (Cache Recent Rooms)

**Effort:** M (4-5 days)

**Approach:** Combine on-demand parsing with caching for recently active rooms

**Implementation:**
1. For small rooms (<1000 events): Use Option B (parse on demand)
2. For large rooms: Use cached media reference index (maintained for active rooms only)
3. TTL-based cache eviction for inactive rooms

**Pros:**
- ✅ Balances performance and complexity
- ✅ Works well for most use cases (abuse typically in active rooms)
- ✅ No permanent schema changes

**Cons:**
- ❌ Complex cache invalidation logic
- ❌ Inconsistent performance (fast for active, slow for archived rooms)
- ❌ Still has worst-case slowness issue
- ❌ More code to maintain

**Performance:**
- Active rooms: O(1) cache lookup
- Inactive rooms: O(n) full scan

---

### Recommendation

**Implement Option A (Event Content Indexing)** when ready to tackle this task.

**Rationale:**
- Production deployments need reliable, fast performance
- One-time migration cost is worth long-term benefits
- Enables future features (media analytics, orphan cleanup)
- Matches how mature Matrix servers (Synapse) implement this
- Clean architecture, no technical debt

**Alternative:** If schema migrations are blocked indefinitely, implement Option B as interim solution with clear warnings about performance on large rooms.

### Prerequisites Before Implementation

1. ✅ **Task #6 core features complete** (single/user-level quarantine working)
2. **Architectural approval** for schema changes and media indexing
3. **Migration strategy** for backfilling existing rooms (can be async)
4. **Performance testing** on development instance with large rooms
5. **Decision on backfill scope** (all rooms vs. active rooms vs. opt-in)

### Effort Breakdown (Option A)

- **Schema design & migration files:** 1 day
- **Event processing hook (media extraction):** 1 day
- **Storage layer queries:** 1 day
- **Backfill logic (optional):** 1 day
- **TDD Cycle - Room quarantine endpoint:** 1 day
- **Quality gate & testing:** 1 day
- **Total:** 5-6 days

### Testing Requirements

- Unit tests for media URL extraction (regex accuracy)
- Unit tests for storage layer queries
- Integration tests for endpoint (small/large rooms)
- Performance tests with 10,000+ event rooms
- Test backfill logic (if implemented)
- Verify audit trail correctness

### Related Tasks

- Similar indexing approach to Task #7 (Room History Purge) which needs `origin_server_ts` indexing
- Could share event content parsing infrastructure
- Both tasks benefit from extracting structured data from event JSON

### Workarounds Until Implemented

**For operators who need to quarantine room media now:**

1. **User-level quarantine** (covers most abuse scenarios):
   ```bash
   POST /_dendrite/admin/v1/media/quarantine/user/@abusive_user:server.com
   ```

2. **Manual media ID extraction** (for targeted response):
   ```bash
   # Query room events via roomserver API
   # Parse JSON content for mxc:// URLs
   # Quarantine each media ID individually:
   POST /_dendrite/admin/v1/media/quarantine/server.com/mediaID123
   ```

3. **Client-side tooling** (build admin script):
   - Use Matrix client SDK to fetch room timeline
   - Extract media events (m.image, m.file, m.video, m.audio)
   - Call quarantine endpoint for each media ID

### References

- Existing implementation: `clientapi/routing/admin.go:404-414` (501 response)
- Media storage: `mediaapi/storage/tables/interface.go`
- Event processing: `roomserver/internal/input/input_events.go`
- Synapse implementation: https://matrix-org.github.io/synapse/latest/admin_api/media_admin_api.html#quarantine-media-in-a-room

---

## Future Tasks

*Additional deferred tasks will be documented here as they arise.*
