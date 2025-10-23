# AUTONOMOUS CONTINUOUS WORK SESSION: Tasks #5-#10

**COPY THIS ENTIRE PROMPT TO YOUR AI ASSISTANT**

---

## 🎯 MISSION: Complete Remaining 5 Tasks Autonomously

You are beginning an extended autonomous work session to complete the remaining Dendrite priority tasks. You will work through **5 complete tasks** following strict TDD methodology, with quality gates between each task.

**Working Directory**: `/Users/user/src/dendrite`
**Current Branch**: `main` (all previous work merged)
**Total Estimated Time**: 5-8 weeks of work
**Your Goal**: Complete as many tasks as possible in this session
**Note**: Task #7 (Room History Purge) has been deferred to BACKLOG.md due to schema design requirements

---

## ⚠️ CRITICAL RULES

1. **ALWAYS FOLLOW TDD** - Write tests FIRST, never implementation before tests
2. **NEVER SKIP QUALITY GATES** - Each task has mandatory verification steps
3. **COMMIT FREQUENTLY** - After each working TDD cycle
4. **TEST EVERYTHING** - Run tests, race detector, linter before moving on
5. **≥80% COVERAGE REQUIRED** - Every task must meet this threshold
6. **ONE TASK AT A TIME** - Complete fully before starting next
7. **COMMIT AND CONTINUE** - After completing a task, commit your work and immediately move to next task
8. **DOCUMENT BLOCKERS** - If stuck, document clearly and move to next task
9. **NO INTERRUPTIONS** - Complete all 6 tasks without stopping for reviews or pushes

---

## 📊 COMPLETED TASKS (Reference)

✅ **Phase 0**: Admin API v1 Router Infrastructure
✅ **Task #1**: List/Search Users (`GET /_dendrite/admin/v1/users`)
✅ **Task #2**: Deactivate User (`POST /_dendrite/admin/v1/deactivate/{userID}`)
✅ **Task #3**: Enhanced Rate Limiting (per-endpoint, IP exemptions)
✅ **Task #4**: Prometheus Metrics (sync, federation, media)

All merged to main. You're starting fresh from clean `main` branch.

---

## 📋 TASK EXECUTION ORDER

**Priority-First Approach**:

1. **Task #6**: Media Quarantine (P1 - CSAM Compliance)
2. **Task #10**: Thread Notification Counts (P1 - Matrix 2.0)
3. **Task #5**: Password Reset Flow (P1 - UX)
4. **Task #9**: 3PID Email Verification (P2)
5. **Task #8**: URL Previews (P2)

**Rationale**: P1 tasks first (high impact), then P2 (nice-to-have). Task #7 (P0) deferred to BACKLOG.md.

---

# TASK #6: MEDIA QUARANTINE (P1 - CSAM COMPLIANCE)

**Priority**: P1 (High - CSAM compliance)
**Effort**: 1-2 weeks
**Branch**: `feature/admin-media-quarantine`

## Why This Matters

**CSAM compliance requirement**. Must be able to quarantine and block access to illegal media content immediately.

## Requirements

**Endpoints**:
- `POST /_dendrite/admin/v1/quarantine_media/{serverName}/{mediaID}` - Quarantine single media
- `POST /_dendrite/admin/v1/quarantine_media_in_room/{roomID}` - Quarantine all media in room
- `POST /_dendrite/admin/v1/quarantine_media_by_user/{userID}` - Quarantine all media uploaded by user

**Request Body** (optional):
```json
{
  "reason": "CSAM report",
  "quarantine_thumbnails": true
}
```

**Response**:
```json
{
  "quarantined_count": 15,
  "status": "completed"
}
```

**Database Changes**:
- Add `quarantined` boolean column to media metadata table
- Add `quarantined_at` timestamp
- Add `quarantined_by` admin user ID
- Add `quarantine_reason` text

## Implementation - TDD Cycles

### Setup
```bash
git checkout main
git pull origin main
git checkout -b feature/admin-media-quarantine
```

### Cycle 1: Database Schema (1 day)

**1. Create Migration** (`mediaapi/storage/postgres/deltas/2025-10-22-quarantine.sql`):
```sql
-- Add quarantine columns to media metadata
ALTER TABLE IF EXISTS mediaapi_media_repository
ADD COLUMN IF NOT EXISTS quarantined BOOLEAN NOT NULL DEFAULT FALSE,
ADD COLUMN IF NOT EXISTS quarantined_at BIGINT,
ADD COLUMN IF NOT EXISTS quarantined_by TEXT,
ADD COLUMN IF NOT EXISTS quarantine_reason TEXT;

-- Index for quarantine queries
CREATE INDEX IF NOT EXISTS mediaapi_media_repository_quarantined
ON mediaapi_media_repository(quarantined) WHERE quarantined = TRUE;
```

**2. Create SQLite version** (`mediaapi/storage/sqlite3/deltas/2025-10-22-quarantine.sql`):
```sql
-- Same as PostgreSQL version
```

**3. Update table interface** (`mediaapi/storage/tables/interface.go`):
```go
type MediaRepository interface {
    // ... existing methods ...
    QuarantineMedia(ctx context.Context, serverName string, mediaID string, quarantinedBy string, reason string) error
    QuarantineMediaInRoom(ctx context.Context, roomID string, quarantinedBy string, reason string) (int, error)
    QuarantineMediaByUser(ctx context.Context, userID string, quarantinedBy string, reason string) (int, error)
    IsMediaQuarantined(ctx context.Context, serverName string, mediaID string) (bool, error)
}
```

**4. Write Tests** (`mediaapi/storage/tables/media_test.go`):
```go
func TestQuarantineMedia(t *testing.T) {
    test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
        // Upload media
        // Quarantine it
        // Verify quarantined flag set
        // Verify metadata (quarantined_at, by, reason)
    })
}

func TestIsMediaQuarantined(t *testing.T) {
    // Upload media
    // Check not quarantined
    // Quarantine it
    // Check is quarantined
}

func TestQuarantineMediaInRoom(t *testing.T) {
    // Upload multiple media in room
    // Quarantine all in room
    // Verify count correct
    // Verify all marked quarantined
}
```

**5. Run Tests**: Should FAIL ❌

**6. Implement** (`mediaapi/storage/postgres/media_repository_table.go`):
```go
func (s *mediaStatements) QuarantineMedia(
    ctx context.Context,
    serverName string,
    mediaID string,
    quarantinedBy string,
    reason string,
) error {
    _, err := s.updateQuarantineStmt.ExecContext(
        ctx,
        true,
        time.Now().Unix(),
        quarantinedBy,
        reason,
        serverName,
        mediaID,
    )
    return err
}

func (s *mediaStatements) IsMediaQuarantined(
    ctx context.Context,
    serverName string,
    mediaID string,
) (bool, error) {
    var quarantined bool
    err := s.selectQuarantinedStmt.QueryRowContext(
        ctx, serverName, mediaID,
    ).Scan(&quarantined)
    return quarantined, err
}
```

**7. Implement SQLite version** (same logic)

**8. Run Tests**: Should PASS ✅

**9. Commit**:
```bash
git add .
git commit -m "Task #6 Cycle 1: Add database schema for media quarantine

- Add quarantine columns to media repository table
- Add PostgreSQL and SQLite migrations
- Implement QuarantineMedia methods
- Add IsMediaQuarantined check
- Comprehensive tests
- All tests passing

🤖 Generated with Claude Code
Co-Authored-By: Claude <noreply@anthropic.com>"
```

### Cycle 2: Download Blocking (1 day)

**1. Write Tests** (`mediaapi/routing/download_test.go`):
```go
func TestDownloadQuarantinedMedia(t *testing.T) {
    // Upload media
    // Quarantine it
    // Attempt download
    // Should receive 404 Not Found
}

func TestThumbnailQuarantinedMedia(t *testing.T) {
    // Similar to above for thumbnails
}
```

**2. Run Tests**: Should FAIL ❌

**3. Modify Download Handler** (`mediaapi/routing/download.go`):
```go
func Download(/* ... */) util.JSONResponse {
    // ... existing code ...

    // CHECK: Is media quarantined?
    quarantined, err := db.IsMediaQuarantined(req.Context(), serverName, mediaID)
    if err != nil {
        return util.ErrorResponse(err)
    }
    if quarantined {
        return util.JSONResponse{
            Code: http.StatusNotFound,
            JSON: spec.NotFound("media is unavailable"),
        }
    }

    // ... continue with download ...
}
```

**4. Run Tests**: Should PASS ✅

**5. Commit**:
```bash
git add .
git commit -m "Task #6 Cycle 2: Block downloads of quarantined media

- Add quarantine check in Download handler
- Add quarantine check in Thumbnail handler
- Return 404 for quarantined media
- Tests for blocked access
- All tests passing

🤖 Generated with Claude Code
Co-Authored-By: Claude <noreply@anthropic.com>"
```

### Cycle 3: Admin Endpoints (2-3 days)

**1. Write Tests** (`clientapi/routing/admin_test.go`):
```go
func TestAdminQuarantineMedia(t *testing.T) {
    // Upload media
    // Quarantine via admin endpoint
    // Verify response
    // Verify download blocked
    // Verify metadata recorded
}

func TestAdminQuarantineMediaInRoom(t *testing.T) {
    // Upload multiple media in room
    // Quarantine all
    // Verify count matches
    // Verify all blocked
}

func TestAdminQuarantineMediaByUser(t *testing.T) {
    // User uploads multiple media
    // Quarantine all by user
    // Verify count
    // Verify all blocked
}

func TestQuarantineNonAdmin(t *testing.T) {
    // Non-admin attempts quarantine
    // Should get 403 Forbidden
}
```

**2. Run Tests**: Should FAIL ❌

**3. Implement** (`clientapi/routing/admin.go`):
```go
func AdminQuarantineMedia(
    req *http.Request,
    cfg *config.ClientAPI,
    mediaAPI mediaapi.MediaInternalAPI,
    device *userapi.Device,
) util.JSONResponse {
    vars := mux.Vars(req)
    serverName := vars["serverName"]
    mediaID := vars["mediaID"]

    // Parse request body
    var body struct {
        Reason              string `json:"reason"`
        QuarantineThumbnails bool   `json:"quarantine_thumbnails"`
    }
    json.NewDecoder(req.Body).Decode(&body)

    // Quarantine media
    err := mediaAPI.QuarantineMedia(req.Context(), &mediaapi.QuarantineMediaRequest{
        ServerName:          serverName,
        MediaID:             mediaID,
        QuarantinedBy:       device.UserID,
        Reason:              body.Reason,
        QuarantineThumbnails: body.QuarantineThumbnails,
    }, &mediaapi.QuarantineMediaResponse{})
    if err != nil {
        return util.ErrorResponse(err)
    }

    return util.JSONResponse{
        Code: http.StatusOK,
        JSON: map[string]interface{}{
            "quarantined_count": 1,
            "status":            "completed",
        },
    }
}

func AdminQuarantineMediaInRoom(/* similar */) util.JSONResponse {
    // ... implementation ...
}

func AdminQuarantineMediaByUser(/* similar */) util.JSONResponse {
    // ... implementation ...
}
```

**4. Register Endpoints** (`clientapi/routing/routing.go`):
```go
// Single media
registerAdminHandlerDual(
    dendriteRouter, adminV1Router, cfg, mediaAPI,
    "/quarantine_media", "/quarantine_media/{serverName}/{mediaID}",
    AdminQuarantineMedia, "admin_quarantine_media",
    http.MethodPost,
)

// Room media
registerAdminHandlerDual(
    dendriteRouter, adminV1Router, cfg, mediaAPI,
    "/quarantine_media_in_room", "/quarantine_media_in_room/{roomID}",
    AdminQuarantineMediaInRoom, "admin_quarantine_media_in_room",
    http.MethodPost,
)

// User media
registerAdminHandlerDual(
    dendriteRouter, adminV1Router, cfg, mediaAPI,
    "/quarantine_media_by_user", "/quarantine_media_by_user/{userID}",
    AdminQuarantineMediaByUser, "admin_quarantine_media_by_user",
    http.MethodPost,
)
```

**5. Run Tests**: Should PASS ✅

**6. Commit**:
```bash
git add .
git commit -m "Task #6 Cycle 3: Add admin quarantine endpoints

- POST /_dendrite/admin/v1/quarantine_media/{serverName}/{mediaID}
- POST /_dendrite/admin/v1/quarantine_media_in_room/{roomID}
- POST /_dendrite/admin/v1/quarantine_media_by_user/{userID}
- Support reason and thumbnail quarantine
- Admin authentication required
- Comprehensive tests
- All tests passing

🤖 Generated with Claude Code
Co-Authored-By: Claude <noreply@anthropic.com>"
```

### Quality Gate - Task #6

```bash
# 1. All tests pass
go test ./mediaapi/... ./clientapi/...

# 2. Race detector
go test -race ./mediaapi/storage/... ./mediaapi/routing/... ./clientapi/routing/...

# 3. Coverage check
go test -coverprofile=coverage.out ./mediaapi/... ./clientapi/routing/...
go tool cover -func=coverage.out | grep total
# Must show ≥80.0%

# 4. Linter
golangci-lint run ./mediaapi/... ./clientapi/...

# 5. Full project tests
go test ./...
```

**✅ Task #6 Complete - Immediately proceed to Task #10.**

---

# TASK #10: THREAD NOTIFICATION COUNTS (P1 - MATRIX 2.0)

**Priority**: P1 (High - Matrix 2.0 feature)
**Effort**: 3-5 days
**Branch**: `feature/thread-notification-counts`

## Why This Matters

**MSC3440 (Threads) completion**. Required for proper thread support in Element and other clients. Part of Matrix 2.0 spec.

## Requirements

**Endpoint Changes**: Modify `/sync` response to include thread notification counts

**New Sync Response Fields**:
```json
{
  "rooms": {
    "join": {
      "!roomid:server": {
        "unread_notifications": {
          "highlight_count": 2,
          "notification_count": 5
        },
        "unread_thread_notifications": {
          "$thread_root_event_id": {
            "highlight_count": 1,
            "notification_count": 3
          }
        }
      }
    }
  }
}
```

**Database Changes**:
- Add `thread_root_event_id` column to notifications table
- Add index on `(room_id, user_id, thread_root_event_id)`

## Implementation - TDD Cycles

### Setup
```bash
git checkout main
git pull origin main
git checkout -b feature/thread-notification-counts
```

### Cycle 1: Database Schema (1 day)

**1. Create Migration** (`syncapi/storage/postgres/deltas/2025-10-22-thread-notifications.sql`):
```sql
-- Add thread_root_event_id to notifications
ALTER TABLE IF EXISTS syncapi_notification_data
ADD COLUMN IF NOT EXISTS thread_root_event_id TEXT;

-- Index for thread notification queries
CREATE INDEX IF NOT EXISTS syncapi_notification_data_thread
ON syncapi_notification_data(room_id, user_id, thread_root_event_id)
WHERE thread_root_event_id IS NOT NULL;
```

**2. Create SQLite version** (same)

**3. Write Tests** (`syncapi/storage/tables/notification_test.go`):
```go
func TestThreadNotificationCounts(t *testing.T) {
    test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
        // Create notifications in thread
        // Query notification counts by thread
        // Verify counts match
    })
}

func TestThreadNotificationSeparation(t *testing.T) {
    // Create notifications in main timeline
    // Create notifications in thread
    // Verify counts are separate
}
```

**4. Run Tests**: Should FAIL ❌

**5. Update table interface** (`syncapi/storage/tables/notification.go`):
```go
type Notification interface {
    // ... existing methods ...
    GetThreadNotificationCounts(ctx context.Context, roomID, userID string) (map[string]*NotificationCount, error)
}

type NotificationCount struct {
    HighlightCount    int
    NotificationCount int
}
```

**6. Implement** (`syncapi/storage/postgres/notification_data_table.go`):
```go
func (s *notificationStatements) GetThreadNotificationCounts(
    ctx context.Context,
    roomID, userID string,
) (map[string]*NotificationCount, error) {
    rows, err := s.selectThreadCountsStmt.QueryContext(ctx, roomID, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    counts := make(map[string]*NotificationCount)
    for rows.Next() {
        var threadRootID string
        var highlight, notification int
        if err := rows.Scan(&threadRootID, &highlight, &notification); err != nil {
            return nil, err
        }
        counts[threadRootID] = &NotificationCount{
            HighlightCount:    highlight,
            NotificationCount: notification,
        }
    }
    return counts, rows.Err()
}
```

**7. Run Tests**: Should PASS ✅

**8. Commit**:
```bash
git add .
git commit -m "Task #10 Cycle 1: Add thread notification database schema

- Add thread_root_event_id column to notifications
- Add index for thread queries
- Implement GetThreadNotificationCounts
- Comprehensive tests
- All tests passing

🤖 Generated with Claude Code
Co-Authored-By: Claude <noreply@anthropic.com>"
```

### Cycle 2: Sync Response (2 days)

**1. Write Tests** (`syncapi/sync/requestpool_test.go`):
```go
func TestSyncThreadNotificationCounts(t *testing.T) {
    // Create room with thread
    // Send notifications to thread
    // Sync
    // Verify unread_thread_notifications in response
    // Verify counts match
}

func TestThreadNotificationsSeparateFromMain(t *testing.T) {
    // Send notification to main timeline
    // Send notification to thread
    // Sync
    // Verify both unread_notifications and unread_thread_notifications
    // Verify counts are separate
}
```

**2. Run Tests**: Should FAIL ❌

**3. Modify Sync Logic** (`syncapi/sync/requestpool.go`):
```go
func (rp *RequestPool) OnIncomingMessagesRequest(/* ... */) util.JSONResponse {
    // ... existing sync logic ...

    // Get thread notification counts
    for roomID := range joinedRooms {
        threadCounts, err := rp.db.GetThreadNotificationCounts(
            req.Context(),
            roomID,
            device.UserID,
        )
        if err != nil {
            logrus.WithError(err).Error("Failed to get thread notification counts")
            continue
        }

        // Add to sync response
        if len(threadCounts) > 0 {
            joinedRooms[roomID].UnreadThreadNotifications = threadCounts
        }
    }

    // ... rest of sync response ...
}
```

**4. Update Response Type** (`syncapi/types/types.go`):
```go
type JoinResponse struct {
    // ... existing fields ...
    UnreadThreadNotifications map[string]NotificationCount `json:"unread_thread_notifications,omitempty"`
}
```

**5. Run Tests**: Should PASS ✅

**6. Commit**:
```bash
git add .
git commit -m "Task #10 Cycle 2: Add thread notification counts to /sync

- Modify /sync to include unread_thread_notifications
- Add per-thread highlight and notification counts
- Update sync response types
- Comprehensive tests
- All tests passing
- MSC3440 (Threads) completion

🤖 Generated with Claude Code
Co-Authored-By: Claude <noreply@anthropic.com>"
```

### Quality Gate - Task #10

```bash
# Tests, race detector, coverage, linter
go test ./syncapi/...
go test -race ./syncapi/storage/... ./syncapi/sync/...
go test -coverprofile=coverage.out ./syncapi/...
go tool cover -func=coverage.out | grep total
golangci-lint run ./syncapi/...
go test ./...
```

**✅ Task #10 Complete - Immediately proceed to Task #5.**

---

# TASK #5: PASSWORD RESET FLOW (P1 - UX)

**Priority**: P1 (High - user experience)
**Effort**: 3-5 days
**Branch**: `feature/password-reset`

## Why This Matters

**Critical UX feature**. Users need to be able to reset forgotten passwords via email.

## Requirements

**Endpoints**:
- `POST /_matrix/client/v3/account/password/email/requestToken` - Request reset token
- `POST /_matrix/client/v3/account/password` - Reset password with token

**Flow**:
1. User requests password reset with email
2. Server generates token, sends email
3. User clicks link in email
4. User submits new password with token
5. Server validates token, updates password

**Database Changes**:
- Create password reset tokens table
- Store token, email, user_id, expiry

## Implementation - TDD Cycles

### Setup
```bash
git checkout main
git pull origin main
git checkout -b feature/password-reset
```

### Cycle 1: Token Generation & Storage (1-2 days)

**1. Create Migration** (`userapi/storage/postgres/deltas/2025-10-22-password-reset.sql`):
```sql
CREATE TABLE IF NOT EXISTS userapi_password_reset_tokens (
    token TEXT PRIMARY KEY,
    localpart TEXT NOT NULL,
    server_name TEXT NOT NULL,
    email TEXT NOT NULL,
    created_ts BIGINT NOT NULL,
    expires_ts BIGINT NOT NULL,
    used BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS userapi_password_reset_tokens_localpart
ON userapi_password_reset_tokens(localpart, server_name)
WHERE used = FALSE;

CREATE INDEX IF NOT EXISTS userapi_password_reset_tokens_expires
ON userapi_password_reset_tokens(expires_ts)
WHERE used = FALSE;
```

**2. Write Tests** (`userapi/storage/tables/password_reset_test.go`):
```go
func TestCreatePasswordResetToken(t *testing.T) {
    test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
        // Create token
        // Verify stored
        // Verify expiry set
    })
}

func TestValidatePasswordResetToken(t *testing.T) {
    // Create token
    // Validate it - should succeed
    // Mark as used
    // Validate again - should fail
}

func TestExpiredTokenInvalid(t *testing.T) {
    // Create token with past expiry
    // Validate - should fail
}
```

**3. Run Tests**: Should FAIL ❌

**4. Implement** (`userapi/storage/postgres/password_reset_table.go`):
```go
func (s *passwordResetStatements) CreateToken(
    ctx context.Context,
    localpart, serverName, email string,
    expiryDuration time.Duration,
) (string, error) {
    token := generateSecureToken()
    now := time.Now().Unix()
    expiry := now + int64(expiryDuration.Seconds())

    _, err := s.insertTokenStmt.ExecContext(
        ctx, token, localpart, serverName, email, now, expiry, false,
    )
    return token, err
}

func (s *passwordResetStatements) ValidateToken(
    ctx context.Context,
    token string,
) (*PasswordResetInfo, error) {
    var info PasswordResetInfo
    var used bool
    var expiresTS int64

    err := s.selectTokenStmt.QueryRowContext(ctx, token).Scan(
        &info.Localpart, &info.ServerName, &info.Email, &used, &expiresTS,
    )
    if err != nil {
        return nil, err
    }

    if used {
        return nil, errors.New("token already used")
    }

    if time.Now().Unix() > expiresTS {
        return nil, errors.New("token expired")
    }

    return &info, nil
}
```

**5. Run Tests**: Should PASS ✅

**6. Commit**:
```bash
git add .
git commit -m "Task #5 Cycle 1: Add password reset token storage

- Create password_reset_tokens table
- Implement token generation and validation
- Token expiry and one-time use
- Comprehensive tests
- All tests passing

🤖 Generated with Claude Code
Co-Authored-By: Claude <noreply@anthropic.com>"
```

### Cycle 2: Request Token Endpoint (1 day)

**1. Write Tests** (`clientapi/routing/password_test.go`):
```go
func TestRequestPasswordResetToken(t *testing.T) {
    // Request token for valid email
    // Verify token created
    // Verify email sent (mock)
    // Verify response format
}

func TestRequestTokenInvalidEmail(t *testing.T) {
    // Request token for unregistered email
    // Should fail gracefully (no user enumeration)
}
```

**2. Run Tests**: Should FAIL ❌

**3. Implement** (`clientapi/routing/password.go`):
```go
func RequestPasswordResetToken(
    req *http.Request,
    userAPI userapi.ClientUserAPI,
    cfg *config.ClientAPI,
) util.JSONResponse {
    var body struct {
        Email      string `json:"email"`
        ClientSecret string `json:"client_secret"`
        SendAttempt  int    `json:"send_attempt"`
    }
    if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
        return util.JSONResponse{
            Code: http.StatusBadRequest,
            JSON: spec.BadJSON("invalid request"),
        }
    }

    // Look up user by email (via 3PID table)
    userID, err := userAPI.GetUserIDByThreePID(req.Context(), body.Email, "email")
    if err != nil {
        // Don't reveal if email exists (prevent user enumeration)
        return util.JSONResponse{
            Code: http.StatusOK,
            JSON: map[string]string{
                "sid": generateSessionID(),
            },
        }
    }

    // Generate token
    token, err := userAPI.CreatePasswordResetToken(
        req.Context(),
        userID,
        body.Email,
        time.Hour, // 1 hour expiry
    )
    if err != nil {
        return util.ErrorResponse(err)
    }

    // Send email
    resetLink := fmt.Sprintf("%s/_matrix/client/v3/password/reset?token=%s", cfg.Matrix.ServerName, token)
    err = sendPasswordResetEmail(body.Email, resetLink)
    if err != nil {
        logrus.WithError(err).Error("Failed to send password reset email")
    }

    return util.JSONResponse{
        Code: http.StatusOK,
        JSON: map[string]string{
            "sid": generateSessionID(),
        },
    }
}
```

**4. Run Tests**: Should PASS ✅

**5. Commit**:
```bash
git add .
git commit -m "Task #5 Cycle 2: Add request password reset token endpoint

- POST /_matrix/client/v3/account/password/email/requestToken
- Token generation and email sending
- User enumeration protection
- Comprehensive tests
- All tests passing

🤖 Generated with Claude Code
Co-Authored-By: Claude <noreply@anthropic.com>"
```

### Cycle 3: Reset Password Endpoint (1 day)

**1. Write Tests**:
```go
func TestResetPasswordWithToken(t *testing.T) {
    // Create user
    // Request reset token
    // Reset password with valid token
    // Verify password changed
    // Verify can login with new password
}

func TestResetPasswordInvalidToken(t *testing.T) {
    // Attempt reset with invalid token
    // Should fail
}

func TestResetPasswordExpiredToken(t *testing.T) {
    // Create expired token
    // Attempt reset
    // Should fail
}
```

**2. Run Tests**: Should FAIL ❌

**3. Implement**:
```go
func ResetPassword(
    req *http.Request,
    userAPI userapi.ClientUserAPI,
) util.JSONResponse {
    var body struct {
        Auth struct {
            Type  string `json:"type"`
            Token string `json:"token"`
        } `json:"auth"`
        NewPassword string `json:"new_password"`
    }
    if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
        return util.JSONResponse{
            Code: http.StatusBadRequest,
            JSON: spec.BadJSON("invalid request"),
        }
    }

    // Validate token
    tokenInfo, err := userAPI.ValidatePasswordResetToken(req.Context(), body.Auth.Token)
    if err != nil {
        return util.JSONResponse{
            Code: http.StatusUnauthorized,
            JSON: spec.Forbidden("invalid or expired token"),
        }
    }

    // Update password
    err = userAPI.SetPassword(req.Context(), tokenInfo.UserID, body.NewPassword)
    if err != nil {
        return util.ErrorResponse(err)
    }

    // Mark token as used
    err = userAPI.MarkPasswordResetTokenUsed(req.Context(), body.Auth.Token)
    if err != nil {
        logrus.WithError(err).Error("Failed to mark token as used")
    }

    return util.JSONResponse{
        Code: http.StatusOK,
        JSON: map[string]interface{}{},
    }
}
```

**4. Run Tests**: Should PASS ✅

**5. Commit**:
```bash
git add .
git commit -m "Task #5 Cycle 3: Add reset password endpoint

- POST /_matrix/client/v3/account/password
- Token validation and password update
- One-time token use enforcement
- Comprehensive tests
- All tests passing

🤖 Generated with Claude Code
Co-Authored-By: Claude <noreply@anthropic.com>"
```

### Quality Gate - Task #5

```bash
go test ./userapi/... ./clientapi/...
go test -race ./userapi/storage/... ./clientapi/routing/...
go test -coverprofile=coverage.out ./userapi/... ./clientapi/...
go tool cover -func=coverage.out | grep total
golangci-lint run ./userapi/... ./clientapi/...
go test ./...
```

**✅ Task #5 Complete - Immediately proceed to Task #9.**

---

# TASK #9: 3PID EMAIL VERIFICATION (P2)

**Priority**: P2 (Medium - auth enhancement)
**Effort**: 3-5 days
**Branch**: `feature/3pid-email-verification`

## Why This Matters

**Account security**. Users need to verify email addresses for password recovery and account protection.

## Requirements

**Endpoints**:
- `POST /_matrix/client/v3/account/3pid/email/requestToken` - Request verification
- `POST /_matrix/client/v3/account/3pid` - Add verified email

**Flow**:
1. User requests email verification
2. Server generates token, sends verification email
3. User clicks verification link
4. Email marked as verified
5. User can add verified email to account

## Implementation - TDD Cycles

### Setup
```bash
git checkout main
git pull origin main
git checkout -b feature/3pid-email-verification
```

### Cycle 1: Email Verification Tokens (1-2 days)

**Similar structure to Task #5, but for 3PID verification instead of password reset**

**1. Create database table for verification tokens**
**2. Write tests for token generation and validation**
**3. Implement token storage and validation**
**4. Commit**

### Cycle 2: Request Verification Endpoint (1 day)

**1. Write tests**
**2. Implement POST /_matrix/client/v3/account/3pid/email/requestToken**
**3. Send verification email**
**4. Commit**

### Cycle 3: Add 3PID Endpoint (1 day)

**1. Write tests**
**2. Implement POST /_matrix/client/v3/account/3pid**
**3. Verify token and add email to user account**
**4. Commit**

### Quality Gate & PR

```bash
# Same verification steps as previous tasks
go test ./...
go test -race ./userapi/... ./clientapi/...
go test -coverprofile=coverage.out ./userapi/... ./clientapi/...
golangci-lint run ./userapi/... ./clientapi/...
```

**✅ Task #9 Complete - Immediately proceed to Task #8.**

---

# TASK #8: URL PREVIEWS (P2)

**Priority**: P2 (Medium - nice-to-have feature)
**Effort**: 1-2 weeks
**Branch**: `feature/url-previews`

## Why This Matters

**UX enhancement**. Users expect link previews in chat. Common in modern messaging apps.

## Requirements

**Endpoint**: `GET /_matrix/media/v3/preview_url`

**Query Parameters**:
- `url` (required): URL to preview
- `ts` (optional): Timestamp for cache

**Response**:
```json
{
  "og:title": "Page Title",
  "og:description": "Description",
  "og:image": "mxc://server/mediaId",
  "og:url": "https://example.com"
}
```

**Security Requirements** (CRITICAL):
- ✅ SSRF protection (no internal IPs)
- ✅ Size limits (max download size)
- ✅ Timeout enforcement
- ✅ Content-Type validation
- ✅ Rate limiting per domain

## Implementation - TDD Cycles

### Setup
```bash
git checkout main
git pull origin main
git checkout -b feature/url-previews
```

### Cycle 1: SSRF Protection (2 days - CRITICAL)

**1. Write Tests** (`mediaapi/routing/ssrf_test.go`):
```go
func TestSSRFProtection(t *testing.T) {
    tests := []struct {
        url      string
        shouldBlock bool
    }{
        {"http://localhost", true},
        {"http://127.0.0.1", true},
        {"http://192.168.1.1", true},
        {"http://10.0.0.1", true},
        {"http://169.254.169.254", true}, // AWS metadata
        {"http://[::1]", true}, // IPv6 localhost
        {"http://example.com", false},
        {"https://matrix.org", false},
    }

    for _, tt := range tests {
        t.Run(tt.url, func(t *testing.T) {
            blocked := isURLBlocked(tt.url)
            assert.Equal(t, tt.shouldBlock, blocked)
        })
    }
}

func TestSSRFRedirectFollow(t *testing.T) {
    // URL redirects to localhost
    // Should be blocked even after redirect
}
```

**2. Run Tests**: Should FAIL ❌

**3. Implement** (`mediaapi/routing/ssrf.go`):
```go
func isURLBlocked(urlStr string) bool {
    u, err := url.Parse(urlStr)
    if err != nil {
        return true
    }

    // Block file:// and other non-HTTP schemes
    if u.Scheme != "http" && u.Scheme != "https" {
        return true
    }

    // Resolve hostname to IP
    ips, err := net.LookupIP(u.Hostname())
    if err != nil {
        return true
    }

    // Check if any resolved IP is private/internal
    for _, ip := range ips {
        if isPrivateIP(ip) {
            return true
        }
    }

    return false
}

func isPrivateIP(ip net.IP) bool {
    // Check for private ranges
    privateRanges := []string{
        "127.0.0.0/8",    // Loopback
        "10.0.0.0/8",     // Private
        "172.16.0.0/12",  // Private
        "192.168.0.0/16", // Private
        "169.254.0.0/16", // Link-local
        "::1/128",        // IPv6 loopback
        "fc00::/7",       // IPv6 private
    }

    for _, cidr := range privateRanges {
        _, network, _ := net.ParseCIDR(cidr)
        if network.Contains(ip) {
            return true
        }
    }

    return false
}
```

**4. Run Tests**: Should PASS ✅

**5. Commit**:
```bash
git add .
git commit -m "Task #8 Cycle 1: Add SSRF protection for URL previews

- Block private IP ranges
- Block localhost and link-local addresses
- IPv4 and IPv6 support
- Redirect following protection
- Comprehensive tests
- All tests passing

SECURITY: Critical SSRF protection

🤖 Generated with Claude Code
Co-Authored-By: Claude <noreply@anthropic.com>"
```

### Cycle 2: URL Fetching & Parsing (2-3 days)

**1. Write tests for HTML parsing**
**2. Implement HTTP client with timeouts and size limits**
**3. Parse Open Graph tags**
**4. Download and store preview images**
**5. Commit**

### Cycle 3: Caching (1 day)

**1. Write tests for cache behavior**
**2. Implement cache storage (database or Redis)**
**3. Cache expiry logic**
**4. Commit**

### Cycle 4: API Endpoint (1 day)

**1. Write tests**
**2. Implement GET /_matrix/media/v3/preview_url**
**3. Rate limiting per domain**
**4. Commit**

### Quality Gate

```bash
go test ./mediaapi/...
go test -race ./mediaapi/routing/...
go test -coverprofile=coverage.out ./mediaapi/...
golangci-lint run ./mediaapi/...
go test ./...
```

**✅ Task #8 Complete - All 5 tasks finished! Review PROGRESS.md for summary.**

---

## 🏁 SESSION COMPLETION CRITERIA

You have completed this autonomous session when:

- [ ] All 5 tasks implemented following TDD (Task #7 deferred to BACKLOG.md)
- [ ] All quality gates passed for each task
- [ ] All work committed to local branches
- [ ] Coverage ≥80% for all modified packages
- [ ] No failing tests in entire project
- [ ] All race conditions resolved
- [ ] All linter warnings fixed
- [ ] PROGRESS.md file updated with completion status

**If you encounter blockers**:
1. Document the blocker clearly in PROGRESS.md
2. Commit the work-in-progress
3. Skip to next task
4. Create a summary at end listing all blockers

---

## 📝 PROGRESS TRACKING

Create a `PROGRESS.md` file to track your work:

```markdown
# Autonomous Session Progress

## Session Start
- Date: [timestamp]
- Starting from: main branch (Tasks #1-#4 complete, Task #7 deferred)

## Task #6: Media Quarantine
- [ ] Cycle 1: Database schema
- [ ] Cycle 2: Download blocking
- [ ] Cycle 3: Admin endpoints
- [ ] Quality gate passed
- [ ] Work committed to: feature/admin-media-quarantine
- [ ] Blockers: [none/list them]

## Task #10: Thread Notifications
- [ ] Cycle 1: Database schema
- [ ] Cycle 2: Sync response
- [ ] Quality gate passed
- [ ] Work committed to: feature/thread-notification-counts
- [ ] Blockers: [none/list them]

## Task #5: Password Reset
- [ ] Cycle 1: Token storage
- [ ] Cycle 2: Request token
- [ ] Cycle 3: Reset password
- [ ] Quality gate passed
- [ ] Work committed to: feature/password-reset
- [ ] Blockers: [none/list them]

## Task #9: 3PID Email Verification
- [ ] Cycle 1: Verification tokens
- [ ] Cycle 2: Request verification
- [ ] Cycle 3: Add 3PID
- [ ] Quality gate passed
- [ ] Work committed to: feature/3pid-email-verification
- [ ] Blockers: [none/list them]

## Task #8: URL Previews
- [ ] Cycle 1: SSRF protection
- [ ] Cycle 2: URL fetching
- [ ] Cycle 3: Caching
- [ ] Cycle 4: API endpoint
- [ ] Quality gate passed
- [ ] Work committed to: feature/url-previews
- [ ] Blockers: [none/list them]

## Session Summary
- Tasks in scope: 5 (Task #7 deferred to BACKLOG.md)
- Tasks completed: ___/5
- Total time: ___ hours
- Branches with completed work: ___/5
- Deferred: 1 (Task #7 - schema design required)

## Next Steps
After this session, you will need to manually:
1. Push each branch to remote
2. Create PRs for review:
   - feature/admin-media-quarantine
   - feature/thread-notification-counts
   - feature/password-reset
   - feature/3pid-email-verification
   - feature/url-previews
3. (Optional) Address Task #7 separately - see BACKLOG.md
```

---

## 🚀 BEGIN NOW

Start with Task #6 (Media Quarantine). Follow TDD strictly. Write tests FIRST, always.

**Estimated total time**: 5-8 weeks of work compressed into continuous autonomous execution.

**Note**: Task #7 (Room History Purge) has been deferred to BACKLOG.md due to schema design requirements.

**Remember**: Quality over speed. Every test must pass. Every task must meet ≥80% coverage.

GO! 🤖
