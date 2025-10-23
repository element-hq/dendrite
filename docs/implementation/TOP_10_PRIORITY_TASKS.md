# Top 10 Priority Tasks for Dendrite

**Analysis Date**: 2025-10-22
**Last Updated**: 2025-10-22 (Phase 0 completed and approved)
**Methodology**: Impact × Feasibility analysis of feature parity gaps

## ✅ Phase 0: COMPLETED

**Status**: ✅ **APPROVED FOR MERGE** (all code review issues addressed)

**What was implemented**:
- ✅ Versioned admin router: `/_dendrite/admin/v1/` structure
- ✅ Dual registration for backwards compatibility (both v1 and legacy paths work)
- ✅ Deprecation warnings for unversioned paths
- ✅ Comprehensive test coverage (100% for critical functions)
- ✅ Error handling with early panic detection
- ✅ Metrics name validation
- ✅ Full documentation

**Files modified**:
- `clientapi/routing/routing.go` - Router setup with v1 infrastructure
- `clientapi/routing/routing_admin_test.go` - Unit tests (237 lines)
- `clientapi/routing/routing_test.go` - Integration tests (864 lines)
- `clientapi/admin_test.go` - Dual-path testing

**Code review**: All 5 issues from initial review were successfully fixed and verified.

**Next**: Tasks #1, #2, #6, #7 can now proceed using the v1 admin router.

See `.claude/todos/VERSIONING_ANALYSIS.md` for migration plan and rationale.

## Selection Criteria

Tasks selected based on:
1. **User/Operator Pain Points**: Features users actively need
2. **Effort-to-Impact Ratio**: Maximum value for minimum time investment
3. **Dependency Unlocking**: Tasks that enable other features
4. **Production Readiness**: Critical for real-world deployments
5. **GDPR/Legal Compliance**: Regulatory requirements

## Impact Scoring

- 🔥 Critical - Blocks production use or legal compliance
- ⚡ High - Significantly improves UX or ops
- ✨ Medium - Nice-to-have enhancement

## Quick Wins (S-Sized: Days of Work)

### 1. List/Search Users Admin Endpoint 🔥

**Status**: 🚧 **IN PROGRESS**
**Impact**: Critical for server operators
**Effort**: S (2-4 days)
**Current State**: Missing
**Pain Point**: No way for admins to see user list or search for specific users

**Implementation**:

**Prerequisites**: ✅ Phase 0 complete - v1 router infrastructure ready

**Endpoint**:
- `GET /_dendrite/admin/v1/users` (with pagination)
- Query params: `search`, `from`, `limit`, `sort`, `deactivated`
- Location: `clientapi/routing/admin.go`
- DB queries: `userapi/storage/` - add user listing/search methods

**Note**: Using v1 path per versioning decision. See VERSIONING_ANALYSIS.md for rationale.

**Acceptance Criteria**:
- [ ] Database layer - SelectUsers/CountUsers methods (Cycle 1)
- [ ] API handler - basic listing with pagination (Cycle 2)
- [ ] Search by username/display name (Cycle 3)
- [ ] Filter by account status (active/deactivated)
- [ ] Sort by creation date, last seen (Cycle 4)
- [ ] Admin auth required (via MakeAdminAPI)
- [ ] Rate limited
- [ ] ≥80% test coverage
- [ ] Integration tests pass (Cycle 5)

**Testing**:
- Unit tests for database layer
- Unit tests for handler
- Integration tests for endpoint
- Admin permission enforcement
- Pagination correctness
- Search accuracy

**Implementation Guide**: See `.claude/todos/ENGINEER_A_TRACK_PROMPT.md` (Task #1)

**Value**: Enables basic user management for operators

---

### 2. Deactivate User Endpoint 🔥

**Impact**: Critical (GDPR compliance)
**Effort**: S (2-3 days)
**Current State**: Missing
**Pain Point**: Cannot deactivate/delete user accounts (GDPR violation)

**Implementation**:

**Prerequisites**: Requires Phase 0 router setup (admin API versioning)

**Endpoint**:
- `POST /_dendrite/admin/v1/deactivate/{userID}`
- Mark account as deactivated (prevent login)
- Optional: Leave all rooms
- Optional: Delete personal data
- Location: `clientapi/routing/admin.go`, `userapi/internal/`

**Acceptance Criteria**:
- [ ] Mark user as deactivated in database
- [ ] Revoke all access tokens
- [ ] Prevent future logins
- [ ] Option to leave all rooms
- [ ] Option to redact messages (GDPR)
- [ ] Audit log entry

**Testing**:
- User cannot login after deactivation
- Access tokens invalidated
- Room memberships handled correctly

**Legal Note**: Required for GDPR "right to be forgotten"

**Value**: Enables GDPR compliance; essential for EU deployments

---

### 3. Rate Limiting Configuration Knobs ⚡

**Impact**: High (prevents abuse)
**Effort**: S (2-3 days)
**Current State**: Hardcoded/limited
**Pain Point**: Cannot tune rate limits for specific deployments

**Implementation**:

**Current State**: Basic rate limiting config exists (`setup/config/config_clientapi.go:136-151`)
- `enabled`: bool
- `threshold`: int64 (slots before rate limiting)
- `cooloff_ms`: int64 (cooloff period)
- `exempt_user_ids`: []string (user exemptions)

**What's Missing** (needs extension):
- Per-endpoint overrides (login vs register vs send)
- IP exemptions (currently only user ID exemptions)
- More granular per-second/burst controls
- Location: Extend `setup/config/config_clientapi.go`, apply in `clientapi/routing/`

**Configuration Example**:
```yaml
client_api:
  rate_limiting:
    enabled: true
    per_second: 10
    burst: 20
    exemptions:
      - "127.0.0.1"
    endpoints:
      login:
        per_second: 1
        burst: 3
      register:
        per_second: 0.1
        burst: 1
```

**Acceptance Criteria**:
- [ ] Config schema with sensible defaults
- [ ] Per-endpoint override capability
- [ ] IP exemption list
- [ ] Prometheus metrics for rate limit hits
- [ ] 429 responses with Retry-After header

**Value**: Prevents spam, abuse, and DoS; essential for public servers

---

### 4. Prometheus Metrics Expansion ⚡

**Impact**: High (operational visibility)
**Effort**: S (3-4 days)
**Current State**: Basic metrics only
**Pain Point**: Insufficient visibility into system health

**New Metrics to Add**:

**Sync API**:
- ✅ `dendrite_syncapi_active_sync_requests` - **Already exists** (`syncapi/sync/requestpool.go:257`)
- `dendrite_syncapi_sync_duration_seconds` - Histogram of sync latency (**NEW**)
- `dendrite_syncapi_sync_lag_seconds` - Time between event and sync delivery (**NEW**)

**Federation API**:
- `dendrite_federationapi_queue_depth` - Per-destination queue size
- `dendrite_federationapi_retry_count` - Retry attempts per destination
- `dendrite_federationapi_send_duration_seconds` - Outbound send latency
- `dendrite_federationapi_blacklisted_servers` - Count of blocked servers

**JetStream**:
- `dendrite_jetstream_subject_lag` - Per-subject consumer lag
- `dendrite_jetstream_pending_messages` - Backlog per stream

**Roomserver**:
- `dendrite_roomserver_state_resolution_duration_seconds` - State res timing
- `dendrite_roomserver_event_processing_duration_seconds` - Event processing time

**Implementation**:
- Location: Each component's `internal/` package
- Use existing Prometheus client in codebase
- Add to component initialization

**Acceptance Criteria**:
- [ ] All metrics registered at startup
- [ ] Metrics documented in `docs/metrics.md`
- [ ] Grafana dashboard examples
- [ ] No performance impact

**Value**: Enables proactive monitoring and troubleshooting

---

### 5. Password Reset via Email ⚡

**Impact**: High (basic user expectation)
**Effort**: S (3-5 days)
**Current State**: Missing
**Pain Point**: Users cannot recover lost passwords

**Implementation**:

**Phase 1: Token Generation**
- Endpoint: `POST /_matrix/client/v3/account/password/email/requestToken`
- Generate secure reset token (crypto/rand)
- Store token with expiry (15 minutes)
- Location: `userapi/storage/tables/password_reset.go`

**Phase 2: Email Delivery**
- SMTP configuration in `dendrite.yaml`
- Email template with reset link
- Token validation endpoint
- Location: `userapi/threepid/email.go`

**Phase 3: Password Update**
- Validate token
- Update password hash
- Invalidate all existing sessions
- Location: `clientapi/routing/password.go`

**Configuration**:
```yaml
global:
  smtp:
    host: smtp.example.com
    port: 587
    username: noreply@example.com
    password: ${SMTP_PASSWORD}
    from: "Dendrite <noreply@example.com>"
```

**Acceptance Criteria**:
- [ ] Request token endpoint working
- [ ] Email sent with reset link
- [ ] Token expires after 15 minutes
- [ ] Password successfully reset
- [ ] All sessions invalidated
- [ ] Rate limiting (prevent abuse)

**Security Considerations**:
- Tokens single-use only
- Short expiry (15 min)
- Rate limit to prevent enumeration
- Don't reveal if email exists

**Value**: Basic account recovery; reduces support burden

---

## Medium Wins (M-Sized: 1-2 Weeks of Work)

### 6. Media Quarantine Admin Endpoint ⚡

**Impact**: High (moderation essential)
**Effort**: M (1-2 weeks)
**Current State**: Missing
**Pain Point**: Cannot quarantine illegal/abusive media

**Implementation**:

**Quarantine Actions**:
1. Mark media as quarantined in database
2. Return 404/403 for quarantined media requests
3. Prevent new uploads of same hash
4. Optional: Delete from storage
5. Admin-only reverse quarantine

**Prerequisites**: Requires Phase 0 router setup (admin API versioning)

**Endpoints**:
- `POST /_dendrite/admin/v1/media/quarantine/{serverName}/{mediaID}`
- `POST /_dendrite/admin/v1/media/quarantine/room/{roomID}` - All media in room
- `POST /_dendrite/admin/v1/media/quarantine/user/{userID}` - All user's media
- `DELETE /_dendrite/admin/v1/media/quarantine/{serverName}/{mediaID}` - Unquarantine

**Database**:
- Add `quarantined BOOLEAN` column to media table
- Add `quarantined_by` and `quarantined_at` for audit trail
- Location: `mediaapi/storage/tables/media.go`

**Acceptance Criteria**:
- [ ] Quarantine single media file
- [ ] Quarantine all media in room
- [ ] Quarantine all media by user
- [ ] Quarantined media returns 404
- [ ] Audit log for quarantine actions
- [ ] Optional storage deletion
- [ ] Block re-upload by hash

**Legal**: Required for CSAM/illegal content response

**Value**: Enables content moderation; legal compliance

---

### 7. Room History Purge (Targeted) 🔥

**Impact**: Critical (GDPR + moderation)
**Effort**: M (1-2 weeks)
**Current State**: Full room purge only (nuclear option)
**Pain Point**: Cannot selectively purge messages (GDPR requests, illegal content)

**Implementation**:

**Purge Options**:
1. **Time-based**: Delete all events before timestamp
2. **User-based**: Delete all events from specific user
3. **Event-based**: Delete specific event IDs
4. **Redaction**: Mark as redacted (keeps metadata)
5. **Hard delete**: Remove from database entirely

**Prerequisites**: Requires Phase 0 router setup (admin API versioning)

**Endpoints**:
- `POST /_dendrite/admin/v1/purge_history/{roomID}`
  - Body: `{before_ts, user_id, event_ids, method: "redact"|"delete"}`

**Database Operations**:
- Mark events as purged/redacted
- Remove event content
- Update state snapshots if needed
- Location: `roomserver/storage/tables/events.go`

**Considerations**:
- **Federation**: Purge is local only; remote servers may still have events
- **State events**: Cannot delete without breaking room state
- **Audit trail**: Log what was purged and by whom

**Acceptance Criteria**:
- [ ] Time-based purge working
- [ ] User-based purge working
- [ ] Event-specific purge working
- [ ] Choice of redact vs hard delete
- [ ] State events protected
- [ ] Audit logging
- [ ] Progress tracking for large purges

**GDPR Note**: Required for "right to erasure" requests

**Value**: Enables GDPR compliance and content moderation

---

### 8. URL Preview MVP ✨

**Impact**: Medium (core UX feature)
**Effort**: M (1-2 weeks)
**Current State**: Missing
**Pain Point**: No link previews in messages

**Implementation**:

**Phase 1: Basic Fetch & Parse**
- Endpoint: `GET /_matrix/media/v3/preview_url?url=<url>`
- Fetch URL content (with timeout)
- Parse OpenGraph tags (`og:title`, `og:description`, `og:image`)
- Return JSON response
- Location: `mediaapi/preview/`

**Security (CRITICAL)**:
- IP allowlist/denylist (prevent SSRF)
- Deny private IP ranges (RFC1918, loopback, link-local)
- DNS rebinding protection
- Size limit (1 MB initial, 10 MB for images)
- Timeout (5 seconds)
- Content-Type validation
- User-Agent string

**Caching**:
- Cache previews for 24 hours
- Store in `mediaapi/storage/`
- TTL-based invalidation

**Configuration**:
```yaml
media_api:
  url_preview:
    enabled: true
    max_page_size: 1048576  # 1 MB
    timeout_seconds: 5
    allowed_ip_ranges:
      - "0.0.0.0/0"  # Allow all
    denied_ip_ranges:
      - "127.0.0.0/8"
      - "10.0.0.0/8"
      - "172.16.0.0/12"
      - "192.168.0.0/16"
    user_agent: "Dendrite URL Preview Bot"
```

**Acceptance Criteria**:
- [ ] Fetch and parse OpenGraph metadata
- [ ] SSRF protections in place
- [ ] Caching working (24h TTL)
- [ ] Timeout handling
- [ ] Size limits enforced
- [ ] Security audit passed

**Security Review**: Mandatory (SSRF is a critical risk)

**Value**: Improves message UX; expected feature in modern clients

---

### 9. 3PID Email Verification for Registration ✨

**Impact**: Medium (enables registration flows)
**Effort**: M (1-2 weeks)
**Current State**: Missing
**Pain Point**: Cannot verify email during registration

**Implementation**:

**Flow**:
1. User submits email during registration
2. Server generates validation token
3. Email sent with verification link
4. User clicks link to validate
5. Registration completes with verified email

**Endpoints**:
- `POST /_matrix/client/v3/register/email/requestToken`
- `POST /_matrix/client/v3/register/email/submitToken` (or validate via URL)

**Database**:
- Table: `threepid_validation_tokens`
- Columns: token, email, user_id, expires_at, validated_at
- Location: `userapi/storage/tables/threepid.go`

**Integration with Identity Service** (optional phase 2):
- Hash email for privacy
- Lookup via identity server
- 3PID invites

**Acceptance Criteria**:
- [ ] Email verification during registration
- [ ] Token generation and validation
- [ ] Email delivery working
- [ ] Token expiry (24 hours)
- [ ] Rate limiting (prevent abuse)
- [ ] Bind email to account on success

**Value**: Enables anti-spam measures; required for some deployments

---

### 10. Thread Notification Counts ⚡

**Impact**: High (completes thread support)
**Effort**: M (1-2 weeks)
**Current State**: Thread relations exist, counts missing
**Pain Point**: Incomplete thread implementation; poor UX in Element

**Implementation**:

**What's Needed**:
1. **Per-thread unread counts** in /sync response
2. **Thread-specific notification counts**
3. **Thread receipts** (read markers per thread)
4. **Push rule integration** for thread notifications

**Database**:
- Add thread_unread_counts table
- Track last_read_event per thread per user
- Location: `syncapi/storage/tables/thread_counts.go`

**Sync Response Changes**:
```json
{
  "rooms": {
    "join": {
      "!roomid": {
        "unread_thread_notifications": {
          "$thread_root_event_id": {
            "notification_count": 5,
            "highlight_count": 1
          }
        }
      }
    }
  }
}
```

**Push Rules**:
- Evaluate thread context for push notifications
- Respect thread-specific mute settings
- Location: `internal/pushrules/`

**Acceptance Criteria**:
- [ ] Thread unread counts in /sync
- [ ] Per-thread notification counts
- [ ] Thread read receipts working
- [ ] Push rules honor thread context
- [ ] Element client displays correctly
- [ ] Performance acceptable (<50ms overhead)

**Dependencies**: MSC3440 (thread relations already exist)

**Value**: Completes thread feature; significantly improves UX

---

## Implementation Priority Order

**Week 1-2 (Quick Wins Sprint)**:
1. List Users Endpoint
2. Deactivate User Endpoint
3. Rate Limiting Configuration

**Week 3-4 (Ops & Monitoring)**:
4. Prometheus Metrics Expansion
5. Password Reset via Email

**Week 5-7 (Moderation)**:
6. Media Quarantine
7. Room History Purge

**Week 8-10 (UX Features)**:
8. URL Preview MVP
9. 3PID Email Verification
10. Thread Notification Counts

**Total Estimated Time**: 10-12 weeks (2.5-3 months) for 1 engineer

---

## Success Metrics

**After Completion**:
- ✅ GDPR compliant (user deactivation, data purge)
- ✅ Spam/abuse controls (rate limiting, quarantine)
- ✅ Operator-friendly (user management, metrics)
- ✅ Better UX (password reset, URL previews, thread counts)
- ✅ Production-ready for small-medium deployments (100-500 users)

---

## Resources Required

**Engineering**:
- 1 backend engineer (full-time, 10-12 weeks)
- OR 2 engineers (parallel tracks, 6-8 weeks)

**Infrastructure**:
- SMTP server for email delivery
- Test OIDC provider (for future auth work)
- Monitoring stack (Prometheus + Grafana)

**Testing**:
- Complement test scenarios for new features
- Manual testing with Element clients
- Security audit for URL preview SSRF protections

---

## Risk Mitigation

**SSRF Risk (URL Preview)**:
- Comprehensive security review before merge
- IP allowlist/denylist strictly enforced
- Fuzz testing with malicious URLs

**GDPR Compliance**:
- Legal review of purge/deactivation flows
- Ensure audit trails
- Document data retention policies

**Performance**:
- Benchmark thread counts with large rooms
- Load test rate limiting under abuse scenarios
- Profile metrics collection overhead

---

## Next Actions

1. **Prioritize top 3** for immediate sprint
2. **Create GitHub issues** for each task with detailed specs
3. **Assign engineer(s)** to quick wins sprint
4. **Set up monitoring** before starting (to track improvements)
5. **Document as you go** (operator guides, API docs)

---

## Notes

- These tasks are independent and can be parallelized
- All require ≥80% test coverage per project standards
- Admin endpoints need comprehensive security reviews
- Email features require SMTP configuration documentation
- Success unlocks Phase 2 of Matrix 2.0 roadmap
