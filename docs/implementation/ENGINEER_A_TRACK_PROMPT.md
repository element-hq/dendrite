# Engineer A Track: Admin API Endpoints (Tasks #1, #2, #6, #7)

**Prerequisites**: Phase 0 complete (admin API versioning infrastructure exists)
**Timeline**: 4-5 weeks
**Methodology**: Test-Driven Development (TDD)
**Branch Strategy**: One branch per task

---

## Overview

Implement 4 admin API endpoints that depend on Phase 0's v1 router infrastructure. All endpoints use `/_dendrite/admin/v1/` paths and follow TDD methodology.

**Tasks**:
1. List/Search Users (2-4 days) - P0
2. Deactivate User (2-3 days) - P0, GDPR critical
3. Media Quarantine (1-2 weeks) - P1
4. Room History Purge (1-2 weeks) - P0, GDPR critical

**Total Effort**: 4-5 weeks for 1 engineer

---

## Prerequisites Verification

Before starting, verify Phase 0 is complete:

```bash
# Check v1 router exists
grep -r "admin/v1" clientapi/routing/routing.go

# Should show:
# adminV1Router := dendriteAdminRouter.PathPrefix("/admin/v1").Subrouter()

# Verify registerAdminHandlerDual exists
grep -A 10 "func registerAdminHandlerDual" clientapi/routing/routing.go
```

If Phase 0 not complete, **STOP** - these tasks depend on it.

---

## Task #1: List/Search Users Admin Endpoint

**Priority**: P0 (Critical)
**Effort**: 2-4 days
**Branch**: `feature/admin-list-users`

### Why This Matters

Operators have **no way** to see user list or search for users. This is essential for basic server management.

### Requirements

**Endpoint**: `GET /_dendrite/admin/v1/users`

**Query Parameters**:
- `search` (string, optional): Username or display name search
- `from` (int, optional): Pagination offset (default: 0)
- `limit` (int, optional): Results per page (default: 100, max: 1000)
- `sort` (string, optional): Sort field (`username`, `created`, `last_seen`)
- `order` (string, optional): Sort order (`asc`, `desc`)
- `deactivated` (bool, optional): Filter by account status

**Response** (JSON):
```json
{
  "users": [
    {
      "user_id": "@alice:example.com",
      "display_name": "Alice Smith",
      "avatar_url": "mxc://...",
      "created_ts": 1234567890,
      "last_seen_ts": 1234567900,
      "deactivated": false,
      "admin": true
    }
  ],
  "total": 150,
  "next_from": 100
}
```

### TDD Implementation

#### Cycle 1: Database Layer

**1. Write Tests First** (`userapi/storage/tables/users_table_test.go`):
```go
func TestSelectUsers(t *testing.T) {
    // Test selecting all users with pagination
    // Test search by username
    // Test search by display name
    // Test filtering by deactivated status
    // Test sorting by different fields
    // Test ordering (asc/desc)
}

func TestCountUsers(t *testing.T) {
    // Test total count
    // Test count with filters
}
```

**2. Run Tests**: Should FAIL ❌

**3. Implement** (`userapi/storage/postgres/users_table.go`):
```go
func (s *userStatements) SelectUsers(
    ctx context.Context,
    search string,
    offset, limit int,
    sortField, sortOrder string,
    deactivatedFilter *bool,
) ([]api.User, int, error) {
    // Build SQL query with filters
    // Support ILIKE for search (case-insensitive)
    // Support pagination
    // Support sorting
    // Return users + total count
}
```

**4. Run Tests**: Should PASS ✅

**5. Coverage**: ≥80%

---

#### Cycle 2: API Handler

**1. Write Tests First** (`clientapi/routing/admin_test.go`):
```go
func TestAdminListUsers(t *testing.T) {
    // Test list all users (no filters)
    // Test pagination (from, limit)
    // Test search by username
    // Test search by display name
    // Test filtering by deactivated status
    // Test sorting
    // Test admin auth required (401 without auth)
}

func TestAdminListUsersRateLimited(t *testing.T) {
    // Test rate limiting applies
}
```

**2. Run Tests**: Should FAIL ❌

**3. Implement** (`clientapi/routing/admin.go`):
```go
func AdminListUsers(
    req *http.Request,
    userAPI userapi.ClientUserAPI,
) util.JSONResponse {
    // Parse query parameters
    // Validate limits (max 1000)
    // Call userAPI.QueryUsers()
    // Return JSON response
    // Handle errors
}
```

**4. Register Endpoint** (`clientapi/routing/routing.go`):
```go
adminV1Router.Handle("/users",
    httputil.MakeAdminAPI("admin_list_users", userAPI, func(req *http.Request, device *userapi.Device) util.JSONResponse {
        return AdminListUsers(req, userAPI)
    }),
).Methods(http.MethodGet, http.MethodOptions)
```

**5. Run Tests**: Should PASS ✅

**6. Coverage**: ≥80%

---

#### Cycle 3: Integration Tests

**1. Write End-to-End Tests**:
```go
func TestListUsersIntegration(t *testing.T) {
    // Create test users
    // List all users
    // Verify results
    // Test pagination
    // Test search
}
```

**2. Run Tests**: Should PASS ✅

---

### Acceptance Criteria

- [ ] Database query supports pagination, search, sorting, filtering
- [ ] API endpoint returns correct JSON format
- [ ] Search works for username and display name (case-insensitive)
- [ ] Pagination works correctly (offset + limit)
- [ ] Sorting by username, created, last_seen works
- [ ] Filtering by deactivated status works
- [ ] Admin authentication required (401/403 without auth)
- [ ] Rate limiting applied
- [ ] ≥80% test coverage
- [ ] Integration tests pass

---

## Task #2: Deactivate User Endpoint

**Priority**: P0 (GDPR Critical) 🔥
**Effort**: 2-3 days
**Branch**: `feature/admin-deactivate-user`

### Why This Matters

**GDPR compliance violation** without this. Cannot deactivate/delete user accounts. EU deployments are at legal risk.

### Requirements

**Endpoint**: `POST /_dendrite/admin/v1/deactivate/{userID}`

**Request Body** (JSON):
```json
{
  "leave_rooms": true,
  "redact_messages": false
}
```

**Response** (JSON):
```json
{
  "user_id": "@alice:example.com",
  "deactivated": true,
  "rooms_left": 15,
  "tokens_revoked": 3
}
```

**Actions**:
1. Mark user as deactivated in database
2. Revoke all access tokens
3. Optionally: Leave all rooms
4. Optionally: Redact all messages (GDPR)
5. Create audit log entry

### TDD Implementation

#### Cycle 1: Deactivation Logic

**1. Write Tests First** (`userapi/internal/api_test.go`):
```go
func TestPerformUserDeactivation(t *testing.T) {
    // Test user marked as deactivated
    // Test all tokens revoked
    // Test cannot login after deactivation
    // Test leave_rooms option
    // Test redact_messages option
    // Test audit log created
}
```

**2. Run Tests**: Should FAIL ❌

**3. Implement** (`userapi/internal/api.go`):
```go
func (a *UserInternalAPI) PerformUserDeactivation(
    ctx context.Context,
    req *api.PerformUserDeactivationRequest,
    res *api.PerformUserDeactivationResponse,
) error {
    // Mark user as deactivated
    // Revoke all access tokens
    // If leave_rooms: call roomserver to leave all rooms
    // If redact_messages: queue redaction tasks
    // Create audit log entry
    // Return stats
}
```

**4. Run Tests**: Should PASS ✅

---

#### Cycle 2: API Handler

**1. Write Tests First** (`clientapi/routing/admin_test.go`):
```go
func TestAdminDeactivateUser(t *testing.T) {
    // Test deactivate user
    // Test user cannot login after
    // Test tokens revoked
    // Test leave_rooms works
    // Test admin auth required
}
```

**2. Run Tests**: Should FAIL ❌

**3. Implement** (`clientapi/routing/admin.go`):
```go
func AdminDeactivateUser(
    req *http.Request,
    userAPI userapi.ClientUserAPI,
) util.JSONResponse {
    // Parse userID from path
    // Parse request body
    // Call userAPI.PerformUserDeactivation()
    // Return response
}
```

**4. Register Endpoint**:
```go
adminV1Router.Handle("/deactivate/{userID}",
    httputil.MakeAdminAPI("admin_deactivate_user", userAPI, func(req *http.Request, device *userapi.Device) util.JSONResponse {
        return AdminDeactivateUser(req, userAPI)
    }),
).Methods(http.MethodPost, http.MethodOptions)
```

**5. Run Tests**: Should PASS ✅

---

### Acceptance Criteria

- [ ] User marked as deactivated in database
- [ ] All access tokens revoked
- [ ] User cannot login after deactivation
- [ ] leave_rooms option works (calls roomserver)
- [ ] redact_messages option queues redaction jobs
- [ ] Audit log entry created (who, when, why)
- [ ] Admin authentication required
- [ ] ≥80% test coverage
- [ ] GDPR-compliant (supports data deletion)

---

## Task #6: Media Quarantine Admin Endpoint

**Priority**: P1 (High)
**Effort**: 1-2 weeks
**Branch**: `feature/admin-media-quarantine`

### Why This Matters

Cannot quarantine illegal/abusive media. **Critical for CSAM response** and content moderation. Legal compliance requirement.

### Requirements

**Endpoints**:
- `POST /_dendrite/admin/v1/media/quarantine/{serverName}/{mediaID}` - Quarantine single file
- `POST /_dendrite/admin/v1/media/quarantine/room/{roomID}` - Quarantine all media in room
- `POST /_dendrite/admin/v1/media/quarantine/user/{userID}` - Quarantine all user's media
- `DELETE /_dendrite/admin/v1/media/quarantine/{serverName}/{mediaID}` - Unquarantine

**Response** (JSON):
```json
{
  "num_quarantined": 15
}
```

**Actions**:
1. Mark media as quarantined in database
2. Return 404/403 for quarantined media requests
3. Prevent re-upload by hash
4. Optional: Delete from storage
5. Audit log entry

### TDD Implementation

#### Cycle 1: Database Schema

**1. Write Migration** (`mediaapi/storage/postgres/deltas/`):
```sql
-- Add quarantine columns
ALTER TABLE media_repository ADD COLUMN quarantined BOOLEAN DEFAULT FALSE;
ALTER TABLE media_repository ADD COLUMN quarantined_by TEXT;
ALTER TABLE media_repository ADD COLUMN quarantined_at BIGINT;
CREATE INDEX media_quarantined ON media_repository(quarantined);
```

**2. Write Tests First** (`mediaapi/storage/tables/media_table_test.go`):
```go
func TestSetMediaQuarantined(t *testing.T) {
    // Test marking media as quarantined
    // Test recording who/when
}

func TestSelectQuarantinedMedia(t *testing.T) {
    // Test finding quarantined media
}

func TestSelectMediaByRoom(t *testing.T) {
    // Test finding all media in a room
}

func TestSelectMediaByUser(t *testing.T) {
    // Test finding all media by user
}
```

**3. Run Tests**: Should FAIL ❌

**4. Implement** (`mediaapi/storage/postgres/media_table.go`):
```go
func (s *mediaStatements) SetMediaQuarantined(
    ctx context.Context,
    serverName, mediaID, quarantinedBy string,
    quarantined bool,
) error {
    // Update quarantine status
    // Record who/when
}
```

**5. Run Tests**: Should PASS ✅

---

#### Cycle 2: Quarantine Logic

**1. Write Tests First** (`mediaapi/routing/download_test.go`):
```go
func TestDownloadQuarantinedMedia(t *testing.T) {
    // Quarantine media
    // Attempt download
    // Should return 404/403
}

func TestPreventQuarantinedReupload(t *testing.T) {
    // Quarantine media by hash
    // Attempt re-upload same file
    // Should be blocked
}
```

**2. Run Tests**: Should FAIL ❌

**3. Implement** (`mediaapi/routing/download.go`):
```go
// Add quarantine check before serving media
func Download(...) {
    // Check if media quarantined
    if mediaMetadata.Quarantined {
        return util.JSONResponse{
            Code: http.StatusNotFound,
            JSON: jsonerror.NotFound("Media quarantined"),
        }
    }
    // Continue with download
}
```

**4. Run Tests**: Should PASS ✅

---

#### Cycle 3: Admin Endpoints

**1. Write Tests First** (`clientapi/routing/admin_test.go`):
```go
func TestQuarantineMedia(t *testing.T) {
    // Test quarantine single media
    // Test quarantine by room
    // Test quarantine by user
    // Test unquarantine
    // Test admin auth required
}
```

**2. Run Tests**: Should FAIL ❌

**3. Implement** (`clientapi/routing/admin.go`):
```go
func AdminQuarantineMedia(req *http.Request, mediaAPI mediaapi.MediaAPI) util.JSONResponse {
    // Parse serverName/mediaID from path
    // Call mediaAPI.QuarantineMedia()
    // Return count
}

func AdminQuarantineRoom(req *http.Request, mediaAPI mediaapi.MediaAPI, rsAPI roomserver.API) util.JSONResponse {
    // Get all media in room
    // Quarantine each
    // Return count
}

func AdminQuarantineUser(req *http.Request, mediaAPI mediaapi.MediaAPI) util.JSONResponse {
    // Get all media by user
    // Quarantine each
    // Return count
}

func AdminUnquarantineMedia(req *http.Request, mediaAPI mediaapi.MediaAPI) util.JSONResponse {
    // Remove quarantine flag
}
```

**4. Register Endpoints**:
```go
adminV1Router.Handle("/media/quarantine/{serverName}/{mediaID}",
    httputil.MakeAdminAPI("admin_quarantine_media", userAPI, func(req *http.Request, device *userapi.Device) util.JSONResponse {
        return AdminQuarantineMedia(req, mediaAPI)
    }),
).Methods(http.MethodPost, http.MethodOptions)

adminV1Router.Handle("/media/quarantine/room/{roomID}",
    httputil.MakeAdminAPI("admin_quarantine_room", userAPI, func(req *http.Request, device *userapi.Device) util.JSONResponse {
        return AdminQuarantineRoom(req, mediaAPI, rsAPI)
    }),
).Methods(http.MethodPost, http.MethodOptions)

adminV1Router.Handle("/media/quarantine/user/{userID}",
    httputil.MakeAdminAPI("admin_quarantine_user", userAPI, func(req *http.Request, device *userapi.Device) util.JSONResponse {
        return AdminQuarantineUser(req, mediaAPI)
    }),
).Methods(http.MethodPost, http.MethodOptions)

adminV1Router.Handle("/media/quarantine/{serverName}/{mediaID}",
    httputil.MakeAdminAPI("admin_unquarantine_media", userAPI, func(req *http.Request, device *userapi.Device) util.JSONResponse {
        return AdminUnquarantineMedia(req, mediaAPI)
    }),
).Methods(http.MethodDelete, http.MethodOptions)
```

**5. Run Tests**: Should PASS ✅

---

### Acceptance Criteria

- [ ] Database migration adds quarantine columns
- [ ] Single media can be quarantined
- [ ] All media in room can be quarantined
- [ ] All media by user can be quarantined
- [ ] Media can be unquarantined
- [ ] Quarantined media returns 404 on download
- [ ] Re-upload by hash prevented
- [ ] Audit log entries created
- [ ] Admin authentication required
- [ ] ≥80% test coverage
- [ ] CSAM compliance support

---

## Task #7: Room History Purge (Targeted)

**Priority**: P0 (GDPR Critical) 🔥
**Effort**: 1-2 weeks
**Branch**: `feature/admin-room-purge`

### Why This Matters

Cannot selectively purge messages. Current implementation only allows full room purge (nuclear option). **Required for GDPR "right to erasure"** requests and illegal content removal.

### Requirements

**Endpoint**: `POST /_dendrite/admin/v1/purge_history/{roomID}`

**Request Body** (JSON):
```json
{
  "before_ts": 1234567890,
  "user_id": "@alice:example.com",
  "event_ids": ["$event1", "$event2"],
  "method": "redact"
}
```

**Options**:
- `before_ts`: Delete all events before timestamp
- `user_id`: Delete all events from specific user
- `event_ids`: Delete specific event IDs
- `method`: `"redact"` (keeps metadata) or `"delete"` (hard delete)

**Response** (JSON):
```json
{
  "purged_count": 150,
  "status": "completed"
}
```

### TDD Implementation

#### Cycle 1: Purge Logic

**1. Write Tests First** (`roomserver/internal/purge_test.go`):
```go
func TestPurgeByTimestamp(t *testing.T) {
    // Create events before/after timestamp
    // Purge events before timestamp
    // Verify only old events purged
    // Verify state events protected
}

func TestPurgeByUser(t *testing.T) {
    // Create events from multiple users
    // Purge events from specific user
    // Verify only that user's events purged
}

func TestPurgeByEventIDs(t *testing.T) {
    // Purge specific event IDs
    // Verify only those events purged
}

func TestPurgeMethodRedact(t *testing.T) {
    // Redact events (keep metadata)
    // Verify content removed, metadata remains
}

func TestPurgeMethodDelete(t *testing.T) {
    // Hard delete events
    // Verify completely removed from DB
}

func TestPurgeProtectsStateEvents(t *testing.T) {
    // Attempt to purge state events
    // Should be blocked/skipped
}
```

**2. Run Tests**: Should FAIL ❌

**3. Implement** (`roomserver/internal/purge.go`):
```go
func (r *RoomserverInternalAPI) PerformPurgeHistory(
    ctx context.Context,
    req *api.PerformPurgeHistoryRequest,
    res *api.PerformPurgeHistoryResponse,
) error {
    // Validate request
    // Find events matching criteria
    // Protect state events from deletion
    // Apply purge method (redact or delete)
    // Update state snapshots if needed
    // Create audit log
    // Return count
}
```

**4. Run Tests**: Should PASS ✅

---

#### Cycle 2: API Handler

**1. Write Tests First** (`clientapi/routing/admin_test.go`):
```go
func TestAdminPurgeHistory(t *testing.T) {
    // Test purge by timestamp
    // Test purge by user
    // Test purge by event IDs
    // Test redact vs delete methods
    // Test state events protected
    // Test admin auth required
}
```

**2. Run Tests**: Should FAIL ❌

**3. Implement** (`clientapi/routing/admin.go`):
```go
func AdminPurgeHistory(
    req *http.Request,
    rsAPI roomserver.API,
) util.JSONResponse {
    // Parse roomID from path
    // Parse request body
    // Validate parameters
    // Call rsAPI.PerformPurgeHistory()
    // Return response
}
```

**4. Register Endpoint**:
```go
adminV1Router.Handle("/purge_history/{roomID}",
    httputil.MakeAdminAPI("admin_purge_history", userAPI, func(req *http.Request, device *userapi.Device) util.JSONResponse {
        return AdminPurgeHistory(req, rsAPI)
    }),
).Methods(http.MethodPost, http.MethodOptions)
```

**5. Run Tests**: Should PASS ✅

---

#### Cycle 3: Federation Considerations

**1. Write Tests**:
```go
func TestPurgeIsLocalOnly(t *testing.T) {
    // Verify purge is local only
    // Remote servers still have events
    // Document in response
}
```

**2. Add Documentation**:
```go
// Note in response JSON:
{
  "purged_count": 150,
  "status": "completed",
  "warning": "Purge is local only. Remote servers may still have these events."
}
```

---

### Acceptance Criteria

- [ ] Purge by timestamp works
- [ ] Purge by user works
- [ ] Purge by event IDs works
- [ ] Redact method keeps metadata, removes content
- [ ] Delete method removes completely
- [ ] State events protected from deletion
- [ ] Audit log entries created
- [ ] Progress tracking for large purges
- [ ] Admin authentication required
- [ ] ≥80% test coverage
- [ ] GDPR "right to erasure" compliant
- [ ] Federation caveat documented (local only)

---

## Implementation Order

Follow this sequence:

```
Week 1:      Task #1 (List Users)
Week 2:      Task #2 (Deactivate User)
Week 3-4:    Task #6 (Media Quarantine)
Week 5-6:    Task #7 (Room History Purge)
```

Each task builds on Phase 0's v1 router infrastructure.

---

## General TDD Workflow

For EVERY feature in EVERY task:

1. 🔴 **RED**: Write failing test
2. 🟢 **GREEN**: Write minimum code to pass
3. 🔵 **REFACTOR**: Clean up (tests stay green)
4. 📊 **COVERAGE**: Verify ≥80%
5. ➡️ **NEXT**: Move to next feature

**Never write implementation code before tests exist.**

---

## Testing Commands

```bash
# For each task, run tests with coverage
go test -coverprofile=coverage.out ./clientapi/routing/... ./userapi/... ./mediaapi/... ./roomserver/...

# View coverage summary
go tool cover -func=coverage.out | grep total
# Must show ≥80.0%

# Run with race detector
go test -race ./clientapi/routing/... ./userapi/... ./mediaapi/... ./roomserver/...

# Run all project tests (verify no regressions)
go test ./...
```

---

## Common Patterns Across All Tasks

### Admin Authentication
All endpoints use:
```go
httputil.MakeAdminAPI("admin_<action>", userAPI, handler)
```

This automatically:
- Requires valid access token
- Checks user is admin
- Returns 401/403 if not authorized

### Error Handling
```go
if err != nil {
    return util.JSONResponse{
        Code: http.StatusInternalServerError,
        JSON: jsonerror.InternalServerError(),
    }
}
```

### Audit Logging
For all admin actions:
```go
logrus.WithFields(logrus.Fields{
    "admin_user": device.UserID,
    "action": "deactivate_user",
    "target": targetUserID,
}).Info("Admin action performed")
```

### Pagination Pattern
```go
// Parse query params
from := req.URL.Query().Get("from")
limit := req.URL.Query().Get("limit")

// Apply defaults
fromInt := 0
limitInt := 100
if from != "" {
    fromInt, _ = strconv.Atoi(from)
}
if limit != "" {
    limitInt, _ = strconv.Atoi(limit)
    if limitInt > 1000 {
        limitInt = 1000 // Max limit
    }
}
```

---

## Branch Management

Create separate branches for clean PRs:

```bash
# Task #1
git checkout -b feature/admin-list-users
# Implement, test, PR
git push origin feature/admin-list-users

# Task #2
git checkout main
git pull
git checkout -b feature/admin-deactivate-user
# Implement, test, PR
git push origin feature/admin-deactivate-user

# Task #6
git checkout main
git pull
git checkout -b feature/admin-media-quarantine
# Implement, test, PR
git push origin feature/admin-media-quarantine

# Task #7
git checkout main
git pull
git checkout -b feature/admin-room-purge
# Implement, test, PR
git push origin feature/admin-room-purge
```

---

## PR Template

Use this for each task:

```markdown
## Task #X: [Task Name]

### Summary
Brief description of what this implements.

### Implementation
- Database changes (if any)
- API endpoint(s) added
- TDD approach used (tests first)

### Testing
- ✅ Unit tests: XX% coverage (≥80% required)
- ✅ Integration tests pass
- ✅ Race detector clean
- ✅ No test regressions

### Security
- Admin authentication required
- Rate limiting applied
- Audit logging included

### GDPR Compliance (if applicable)
- Supports data deletion
- Audit trail maintained
- Complies with "right to erasure"

### Documentation
- API endpoint documented
- Request/response format specified
- Error codes documented

### Related
- Depends on: Phase 0 (PR #XXX)
- Part of: Engineer A Track (Tasks #1, #2, #6, #7)
- See: `.claude/todos/TOP_10_PRIORITY_TASKS.md`

### Checklist
- [ ] Tests written before implementation (TDD)
- [ ] ≥80% test coverage
- [ ] Race detector passes
- [ ] All existing tests pass
- [ ] Documentation updated
- [ ] PR description complete
```

---

## Success Criteria for All 4 Tasks

**Week 2 Complete** (Tasks #1, #2):
- ✅ List/search users working
- ✅ Deactivate user working
- ✅ GDPR compliant for user data

**Week 4 Complete** (Task #6):
- ✅ Media quarantine working
- ✅ CSAM response capability
- ✅ Content moderation enabled

**Week 6 Complete** (Task #7):
- ✅ Room history purge working
- ✅ GDPR "right to erasure" compliant
- ✅ All 4 admin endpoints production-ready

---

## Coordination with Engineer B

While you work on these tasks, Engineer B is working on:
- Week 1: Tasks #3, #4 (Rate Limiting, Metrics)
- Week 2: Task #5 (Password Reset)
- Week 3-4: Task #8 (URL Previews)
- Week 5-6: Task #10 (Thread Counts)

**No coordination needed during implementation** - different files/components.

**Coordination points**:
- PR reviews (review each other's code)
- Integration testing (Week 7-8)

---

## References

- **Task specifications**: `.claude/todos/TOP_10_PRIORITY_TASKS.md`
- **Phase 0 details**: `.claude/todos/PHASE0_IMPLEMENTATION_PROMPT.md`
- **TDD workflow**: `docs/development/test-coverage-workflow.md`
- **Parallelization plan**: `.claude/todos/QUICK_PARALLEL_GUIDE.md`

---

## Final Notes

- All 4 tasks follow same TDD pattern: tests first, then implementation
- All use v1 admin router from Phase 0
- All require ≥80% test coverage
- Tasks #2 and #7 are GDPR-critical (must be correct)
- Task #6 is legal-critical (CSAM compliance)
- Quality > Speed (these are foundational admin APIs)

**Remember: Tests first, always. 🧪**

**Ready to implement? Start with Task #1 (List Users)!** 🚀
