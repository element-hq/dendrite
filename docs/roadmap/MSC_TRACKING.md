# MSC Implementation Tracking

This document tracks the implementation status of Matrix Spec Changes (MSCs) in Dendrite, with version tracking, test coverage, and verification dates.

**Last Updated**: 2025-10-22

## Status Legend

- ✅ **Complete**: Fully implemented and tested
- 🟡 **Partial**: Some functionality exists, but incomplete
- ❌ **Missing**: Not implemented
- 🔍 **TBD**: Needs verification

## Priority MSCs for Feature Parity

### Critical Priority (Blocks Modern Clients)

| MSC | Title | Status | First Version | Test Coverage | Last Verified | Priority | Effort |
|-----|-------|--------|---------------|---------------|---------------|----------|--------|
| [MSC4186](https://github.com/matrix-org/matrix-spec-proposals/pull/4186) | Sliding Sync | ❌ Missing | - | - | 2025-10-22 | Critical | XL+ (multi-quarter) |
| [MSC3861](https://github.com/matrix-org/matrix-spec-proposals/pull/3861) | OIDC / MAS | ❌ Missing | - | - | 2025-10-22 | High | L/XL |

**MSC4186 (Sliding Sync)**
- **Spec Status**: Stable/Latest in Matrix spec
- **Synapse Status**: ✅ Complete
- **Dendrite Status**: ❌ Missing
- **Implementation Areas**:
  - `syncapi/` - New sync subsystem
  - New storage tables for lists/windows, device state
  - Ordering indexes and delta operations
  - Token management for incremental updates
- **Required For**: Element X, modern client performance
- **Blockers**: None (can start immediately)
- **Testing Strategy**:
  - Complement test suite for sliding sync scenarios
  - Element X compatibility testing
  - Performance benchmarks (startup time, memory usage)
  - Load testing with multiple concurrent windows

**MSC3861 (OIDC / Matrix Authentication Service)**
- **Spec Status**: Stable
- **Synapse Status**: ✅ Complete
- **Dendrite Status**: ❌ Missing
- **Implementation Areas**:
  - `userapi/` - OIDC provider configuration
  - Token management (access + refresh)
  - Device binding to OIDC sessions
  - Account linking flows
- **Required For**: Modern SSO flows, enterprise auth
- **Blockers**: None (can parallel with Sliding Sync)
- **Testing Strategy**:
  - Integration tests with test OIDC providers
  - Token refresh flow tests
  - Multi-provider scenarios
  - Migration from password auth

### High Priority (Core Features)

| MSC | Title | Status | First Version | Test Coverage | Last Verified | Priority | Effort |
|-----|-------|--------|---------------|---------------|---------------|----------|--------|
| [MSC3440](https://github.com/matrix-org/matrix-spec-proposals/pull/3440) | Threads | 🟡 Partial | - | ~40% | 2025-10-22 | High | L |
| [MSC1212](https://github.com/matrix-org/matrix-spec-proposals/pull/1212) | Key Backup | 🟡 Partial | - | ~30% | 2025-10-22 | High | M/L |
| [MSC3952](https://github.com/matrix-org/matrix-spec-proposals/pull/3952) | Intentional Mentions | ❌ Missing | - | - | 2025-10-22 | Medium | M |

**MSC3440 (Threads)**
- **Spec Status**: Stable
- **Synapse Status**: ✅ Complete
- **Dendrite Status**: 🟡 Partial
- **What Works**: Event relations (MSC2836) for thread structure
- **What's Missing**:
  - Thread-aware `/sync` filters
  - Thread-specific unread/notification counts
  - Thread receipts
  - Push rule integration for threads
- **Implementation Areas**:
  - `syncapi/` - Thread filters and counts
  - `clientapi/` - Thread-aware notification logic
  - Push rules evaluation for threaded messages
- **Blockers**: None
- **Dependencies**: Enhances Sliding Sync value
- **Testing Strategy**:
  - Thread creation and reply tests
  - Notification count accuracy
  - Client compatibility (Element, others)

**MSC1212 (Key Backup)**
- **Spec Status**: Stable
- **Synapse Status**: ✅ Complete
- **Dendrite Status**: 🟡 Partial
- **What Works**: Basic E2EE device management
- **What's Missing**:
  - Server-side encrypted backup storage
  - Backup version management
  - Restore flows
  - Recovery key handling
- **Implementation Areas**:
  - `userapi/storage/` - Backup tables (versions, encrypted data)
  - E2EE backup/restore endpoints
  - Recovery key generation and validation
- **Blockers**: Cross-signing reliability improvements needed first
- **Testing Strategy**:
  - Backup creation and encryption tests
  - Multi-device restore scenarios
  - Recovery key workflows

**MSC3952 (Intentional Mentions)**
- **Spec Status**: Stable
- **Synapse Status**: ✅ Complete
- **Dendrite Status**: ❌ Missing
- **Implementation Areas**:
  - `clientapi/` - Event content mention flags
  - `syncapi/` - Mention-aware notifications
  - Push rules engine updates
- **Blockers**: None
- **Testing Strategy**:
  - Mention flag parsing
  - Notification delivery for mentions
  - Push rule evaluation

### Medium Priority (Federation & UX)

| MSC | Title | Status | First Version | Test Coverage | Last Verified | Priority | Effort |
|-----|-------|--------|---------------|---------------|---------------|----------|--------|
| [MSC3083](https://github.com/matrix-org/matrix-spec-proposals/pull/3083) | Restricted Rooms | 🟡 Partial | - | ~50% | 2025-10-22 | Medium | M |
| [MSC3266](https://github.com/matrix-org/matrix-spec-proposals/pull/3266) | Room Knocking | ❌ Missing | - | - | 2025-10-22 | Medium | M |
| [MSC3874](https://github.com/matrix-org/matrix-spec-proposals/pull/3874) | Thread Filters | ❌ Missing | - | - | 2025-10-22 | Medium | S/M |
| [MSC3391](https://github.com/matrix-org/matrix-spec-proposals/pull/3391) | Remove Deprecated APIs | 🟡 Partial | - | - | 2025-10-22 | Medium | S |

**MSC3083 (Restricted Rooms)**
- **Spec Status**: Stable
- **Synapse Status**: ✅ Complete
- **Dendrite Status**: 🟡 Partial
- **What Works**: Basic join rule handling
- **What's Missing**: Full create/enforce of restricted join rules
- **Implementation Areas**:
  - `roomserver/` - Join rule enforcement
  - `federationapi/` - `make_join`/`send_join` restricted paths
- **Testing Strategy**: Sytest scenarios for restricted room joins

**MSC3266 (Room Knocking)**
- **Spec Status**: Stable
- **Synapse Status**: ✅ Complete
- **Dendrite Status**: ❌ Missing
- **Implementation Areas**:
  - `roomserver/` - Knock membership state transitions
  - `clientapi/` - Knock endpoints
  - `federationapi/` - Federation knock endpoints
- **Testing Strategy**: Knock lifecycle tests (knock, accept, deny)

**MSC3874 (Thread Filters)**
- **Spec Status**: Draft/Stable
- **Synapse Status**: ✅ Complete
- **Dendrite Status**: ❌ Missing
- **Implementation Areas**: `syncapi/` - Filter parameters for thread-only content
- **Dependencies**: Requires MSC3440 base implementation
- **Testing Strategy**: Sync filter tests with thread parameters

**MSC3391 (Remove Deprecated APIs)**
- **Spec Status**: Stable
- **Synapse Status**: ✅ Complete
- **Dendrite Status**: 🟡 Partial
- **Implementation Areas**: Audit across `clientapi/` for deprecated endpoints
- **Testing Strategy**: Ensure deprecated endpoints return proper errors

## Implementation Status by Component

### syncapi/
- ❌ Sliding Sync (MSC4186) - Critical missing feature
- 🟡 Threads (MSC3440) - Relations exist, filters/counts missing
- ❌ Thread Filters (MSC3874) - Not implemented

### userapi/
- ❌ OIDC/MAS (MSC3861) - Not implemented
- 🟡 Key Backup (MSC1212) - Storage missing, flows incomplete

### roomserver/
- 🟡 Restricted Rooms (MSC3083) - Partial enforcement
- ❌ Knock (MSC3266) - Not implemented

### clientapi/
- ❌ Intentional Mentions (MSC3952) - Not implemented
- 🟡 Deprecated API removal (MSC3391) - Partial cleanup

### federationapi/
- 🟡 Restricted Rooms (MSC3083) - Federation paths incomplete
- ❌ Knock (MSC3266) - Federation endpoints missing

## Testing Coverage Goals

For each MSC implementation:

1. **Unit Tests**: ≥80% coverage for new code
2. **Integration Tests**: Complement test scenarios
3. **Sytest Coverage**: Relevant sytest cases passing
4. **Client Compatibility**: Verification with major clients
5. **Performance Tests**: Benchmarks for performance-critical MSCs

## Verification Process

To verify MSC status:

1. Check spec status at https://spec.matrix.org/
2. Review Synapse implementation (reference implementation)
3. Search Dendrite codebase for MSC references
4. Run relevant test suites (Complement, Sytest)
5. Test with Matrix clients (Element, etc.)
6. Update this document with findings

## Useful Resources

- **Matrix Spec**: https://spec.matrix.org/
- **MSC Proposals**: https://github.com/matrix-org/matrix-spec-proposals
- **Complement Tests**: https://github.com/matrix-org/complement
- **Sytest**: https://github.com/matrix-org/sytest
- **Synapse Reference**: https://github.com/element-hq/synapse
