# Fork Changelog

This document describes the changes and enhancements in this Dendrite fork maintained by jackmaninov.

## Branch Overview

This fork maintains several branches with bug fixes and experimental features built on top of the upstream Dendrite v0.15.2 release. These branches are available for testing and community contribution.

### Bug Fix Branches

#### `fix/appservice-space-members-join`
**Status:** Stable, tested in production

Fixes an HTTP 500 error that occurred when appservice users attempted to join restricted rooms (such as spaces). This was caused by incorrect handling of membership checks for virtual appservice users.

**Files Modified:**
- Roomserver membership validation logic

#### `fix/max-depth-cap`
**Status:** Stable, tested in production

Addresses issues with rooms that have events with extremely large depth values, which could cause:
- Canonical JSON encoding failures (depths exceeding JavaScript's MAX_SAFE_INTEGER)
- Inability to send new events or leave affected rooms

**Changes:**
- Caps event depth at MAX_SAFE_INTEGER (2^53 - 1) during event creation
- Clamps depth when building new events to allow leaving problematic rooms

**Files Modified:**
- `roomserver/internal/perform/perform_leave.go`
- `roomserver/internal/helpers/helpers.go`

#### `fix/receipt-sequence-race`
**Status:** Stable, tested in production

Fixes a race condition in read receipt processing that prevented notification badges from clearing reliably. The issue occurred when receipt sequence IDs were assigned non-monotonically due to concurrent database transactions.

**Changes:**
- Ensures receipt sequence IDs are assigned monotonically
- Adds proper transaction ordering for receipt updates

**Files Modified:**
- `syncapi/storage/postgres/receipt_table.go`
- `syncapi/storage/sqlite3/receipt_table.go`

#### `fix/error-code-compliance`
**Status:** Stable, tested in production

Improves Matrix specification compliance for error codes across the codebase. Previously many errors returned generic `M_UNKNOWN`, now they use proper error codes like `M_INVALID_PARAM`, `M_TOO_LARGE`, `M_UNKNOWN_POS`, etc.

**Changes:**
- Added `MatrixErrorResponse` helper for consistent error handling
- Fixed error codes in join/leave/invite handlers
- Fixed error codes in syncapi routing handlers
- Fixed error codes in media API validation

### Matrix Specification Changes (MSCs)

#### `msc3266-room-summary`
**Status:** Stable, tested in production

Implements [MSC3266 Room Summary API](https://github.com/matrix-org/matrix-spec-proposals/pull/3266) for hierarchical room structures (spaces).

**Implementation:**
- Phase 1: Basic client API endpoints (`/_matrix/client/v1/rooms/{roomID}/hierarchy`)
- Phase 2: Federation support for fetching remote space hierarchies
- Authenticated and unauthenticated access support
- Response caching for performance
- Legacy MSC3266 path for Element X compatibility

**Features:**
- Room hierarchy traversal with pagination
- Access control based on join rules and membership
- Populates `encryption` and `room_version` fields
- Federation-aware space exploration

**Testing:**
- Tested with Element X iOS/Web clients
- Production deployment verified

#### `msc3706-faster-joins`
**Status:** Work in Progress - NOT FUNCTIONAL

Partial implementation of [MSC3706 Faster Joins](https://github.com/matrix-org/matrix-spec-proposals/pull/3706) to reduce the time required to join large rooms over federation.

**Implementation Status:**
- ✅ Partial state storage infrastructure
- ✅ Basic partial state join flow
- ✅ Partial state resync worker
- ❌ Event processing during partial state (incomplete)
- ❌ Background state resolution (not implemented)

**Known Issues:**
- Does not successfully complete joins in production testing
- State resolution conflicts during partial state
- Resync worker may not properly converge to full state

**DO NOT USE IN PRODUCTION** - This branch is experimental and does not work reliably.

#### `msc4115-membership-on-events`
**Status:** Stable, tested in production

Implements [MSC4115 Membership on Events](https://github.com/matrix-org/matrix-spec-proposals/pull/4115) for the sliding sync v2 API.

**Implementation:**
- Phase 1: Core infrastructure for membership information on events
- Phase 3: Integration with MSC3575 (Sliding Sync) v2 API
- Efficient membership state tracking for sync responses

**Features:**
- Attaches membership state to timeline events
- Optimized database queries for membership lookups
- Integrated with sliding sync `required_state` handling

### Sliding Sync Implementation

#### `sliding-sync`
**Status:** Stable, production-ready with Element X

This is the main development branch implementing [MSC3575 Sliding Sync](https://github.com/matrix-org/matrix-spec-proposals/pull/3575) (Matrix Sync v2 API).

**Implementation Status:**

Core Features:
- ✅ Sliding window sync with list-based room management
- ✅ Room subscriptions and list operations
- ✅ Timeline pagination with efficient incremental sync
- ✅ Required state delivery per room
- ✅ Room name calculation and hero members
- ✅ Notification counts (unread, highlight)
- ✅ MSC4115 membership on events integration
- ✅ Extensions framework (E2EE, account data, typing, receipts)
- ✅ Live position tracking with long-polling support

Extensions:
- ✅ E2EE extension (device lists, one-time keys, fallback keys)
- ✅ Account data extension (global and per-room)
- ✅ Typing notifications extension
- ✅ Read receipts extension (MSC4102 support)

**Testing:**
- Unit tests: `syncapi/sync/v4_incremental_test.go`
- Integration tested with Element X iOS (production deployment)
- Integration tested with Element X Web
- Long-running stability testing (multi-month deployment)

**Known Limitations:**
- Does not support all filter options from v2 sync spec
- Room list sorting may differ from Element Web's expectations in some edge cases
- Some extensions incomplete (e.g., to-device messages)

**Performance:**
- Significantly faster initial sync compared to v2 sync
- Efficient incremental updates using NATS pub/sub
- Scales well with large room counts per user

**Branches Merged:**
- `fix/appservice-space-members-join`
- `fix/max-depth-cap`
- `fix/receipt-sequence-race`
- `fix/error-code-compliance`
- `msc3266-room-summary`
- `msc3706-faster-joins` (merged but may be disabled/removed in future)
- `msc4115-membership-on-events`

## Build Configuration

All public branches use the following configuration:
- `gomatrixserverlib` dependency points to public GitHub fork: `github.com/jackmaninov/gomatrixserverlib`
- No private dependencies required
- Standard Dendrite build process applies

## Contributing

Contributions are welcome! Please:
1. Test against the `sliding-sync` branch for compatibility
2. Include unit tests where applicable
3. Verify against Element X clients when possible
4. Document any new MSC implementations

## Production Deployments

The following branches are running in production:
- `sliding-sync` - Main deployment with Element X clients
- All `fix/*` branches - Incorporated into sliding-sync

`msc3706-faster-joins` should NOT be deployed to production.

## License

This fork maintains the same license as upstream Dendrite: **AGPLv3.0-only OR LicenseRef-Element-Commercial**

See LICENSE files in the repository root for full details.
