# Matrix 2.0 Implementation Roadmap

**Goal**: Achieve feature parity with Synapse for Matrix 2.0 features and modern client support.

**Last Updated**: 2025-10-22

## Overview

This roadmap outlines the path to implementing Matrix 2.0 features in Dendrite, with focus on:
- Modern client support (Element X)
- Next-generation authentication (OIDC/MAS)
- Enhanced messaging features (Threads, Mentions)
- Operational maturity (Admin APIs, monitoring)

## Success Metrics

- **Element X Compatibility**: Full support with Sliding Sync
- **Auth Modernization**: OIDC/MAS support for SSO flows
- **Thread Support**: Complete thread implementation with notifications
- **Admin Parity**: Core admin/moderation APIs matching Synapse
- **Test Coverage**: ≥80% for all new features
- **Performance**: p95 sync latency <200ms for 500 MAU deployments

## Phase 1a: Critical UX Blockers (0-4 months)

**Goal**: Enable modern client support and complete core messaging features.

**Timeline**: Can be parallelized with 2+ engineers.

### Sliding Sync (MSC4186) - **XL+ Effort**

**Estimated Effort**: 3-6 months (1 engineer)

**Why Critical**: Required for Element X; dramatically improves client startup and scroll performance.

**Implementation Plan**:

1. **Storage Layer** (4-6 weeks)
   - Design tables for device list/window state
   - Room ordering indexes
   - Delta operation tracking
   - Token management for incremental updates
   - Location: `syncapi/storage/tables/sliding_sync_*.go`

2. **Core Sync Logic** (6-8 weeks)
   - New handler for sliding sync endpoint
   - List/window state management per device
   - Room ordering and filtering
   - Delta computation for incremental updates
   - Location: `syncapi/sliding/`

3. **Integration & Optimization** (2-4 weeks)
   - Reuse existing stream positions
   - In-memory caching for hot paths
   - Connection pooling and batching
   - Performance profiling and tuning

4. **Testing & Validation** (2-3 weeks)
   - Complement test scenarios
   - Element X compatibility testing
   - Load testing (multiple windows, large room counts)
   - Migration path from legacy sync

**Milestones**:
- [ ] Storage schema finalized and migrated
- [ ] Basic sliding sync endpoint working
- [ ] Element X can login and view room list
- [ ] Room ordering and filtering working
- [ ] Delta updates functioning correctly
- [ ] Performance benchmarks met (<200ms p95)
- [ ] Complement tests passing
- [ ] Element X full feature compatibility

**Dependencies**: None (can start immediately)

**Risk Mitigation**:
- Feature flag for gradual rollout
- Fallback to legacy /sync if issues occur
- Extensive performance testing before GA

---

### Threads (MSC3440 Completion) - **L Effort**

**Estimated Effort**: 1-2 months (1 engineer)

**Why Critical**: Core messaging feature; improves conversation organization.

**Current Status**: Event relations exist; missing sync filters, counts, push rules.

**Implementation Plan**:

1. **Thread-Aware Sync Filters** (2-3 weeks)
   - Add filter parameters for thread-only content
   - Implement filter logic in sync timeline
   - Location: `syncapi/sync/request.go`, `syncapi/streams/`

2. **Notification Counts** (2-3 weeks)
   - Thread-specific unread counts
   - Thread notification badges
   - Per-thread receipts
   - Location: `syncapi/internal/`, `roomserver/storage/`

3. **Push Rule Integration** (1-2 weeks)
   - Thread-aware push rule evaluation
   - Thread mention notifications
   - Location: `internal/pushrules/`

4. **Testing** (1 week)
   - Thread creation and reply tests
   - Notification accuracy tests
   - Client compatibility verification

**Milestones**:
- [ ] Thread filters working in /sync
- [ ] Thread unread counts accurate
- [ ] Thread receipts functioning
- [ ] Push notifications for thread replies
- [ ] Element client thread support verified

**Dependencies**: None (event relations already exist)

**Enhances**: Sliding Sync (thread-aware filters increase value)

---

## Phase 1b: Auth & Security (2-4 months, starts month 2)

**Goal**: Modernize authentication and strengthen E2EE support.

**Timeline**: Can parallel with Phase 1a.

### OIDC / MAS (MSC3861) - **L/XL Effort**

**Estimated Effort**: 2-4 months (1 engineer)

**Why Critical**: Required for enterprise SSO; modern auth flows; MAS compatibility.

**Implementation Plan**:

1. **OIDC Provider Configuration** (2-3 weeks)
   - Config schema for OIDC providers
   - Provider discovery and validation
   - Multiple provider support
   - Location: `setup/config/`, `userapi/oidc/`

2. **Login & Registration Flows** (4-5 weeks)
   - OIDC authorization code flow
   - Token exchange and validation
   - Account creation/linking
   - Device binding to OIDC sessions
   - Location: `clientapi/routing/login.go`, `userapi/internal/`

3. **Token Management** (3-4 weeks)
   - Access token storage and validation
   - Refresh token handling
   - Token revocation
   - Session management
   - Location: `userapi/storage/tables/oidc_*.go`

4. **MAS Integration** (2-3 weeks)
   - MAS compatibility layer
   - Admin API for provider management
   - Migration from password auth

5. **Testing** (2 weeks)
   - Integration with test OIDC providers (Keycloak, Auth0)
   - Multi-provider scenarios
   - Token refresh flows
   - MAS compatibility tests

**Milestones**:
- [ ] OIDC config schema complete
- [ ] Login via OIDC working
- [ ] Token refresh working
- [ ] Multi-provider support
- [ ] MAS compatibility verified
- [ ] Migration tooling for password→OIDC

**Dependencies**: None

**Relates To**: 3PID flows (account recovery interplay)

---

### Key Backup (MSC1212) - **M/L Effort**

**Estimated Effort**: 6-10 weeks (1 engineer)

**Why Important**: Critical for E2EE UX; allows key recovery across devices.

**Prerequisites**: Cross-signing reliability improvements.

**Implementation Plan**:

1. **Storage Layer** (2-3 weeks)
   - Backup version tables
   - Encrypted key data storage
   - Signature validation
   - Location: `userapi/storage/tables/key_backup_*.go`

2. **Backup Endpoints** (3-4 weeks)
   - Create backup version
   - Upload encrypted keys
   - Download backup
   - Delete backup
   - Location: `clientapi/routing/key_backup.go`

3. **Recovery Key Handling** (2-3 weeks)
   - Recovery key generation
   - Recovery key validation
   - Backup decryption flows

4. **Testing** (1-2 weeks)
   - Backup creation tests
   - Multi-device restore scenarios
   - Recovery key workflows

**Milestones**:
- [ ] Storage schema complete
- [ ] Backup creation working
- [ ] Key upload/download working
- [ ] Recovery key flows working
- [ ] Multi-device restore verified

**Dependencies**: Cross-signing stability improvements

---

### 3PID Flows (Email/Phone) - **M Effort**

**Estimated Effort**: 4-6 weeks (1 engineer)

**Why Important**: Account recovery; user discovery; registration flows.

**Implementation Plan**:

1. **Email Verification** (2-3 weeks)
   - SMTP configuration
   - Token generation and validation
   - Email templates
   - Location: `userapi/threepid/`, `clientapi/routing/register.go`

2. **Password Reset** (1-2 weeks)
   - Reset token generation
   - Email delivery
   - Token validation and password update

3. **3PID Registration** (1-2 weeks)
   - Email/phone during registration
   - Identity service integration
   - 3PID binding

4. **Discovery** (1 week)
   - 3PID lookup endpoints
   - Privacy controls

**Milestones**:
- [ ] Email verification working
- [ ] Password reset via email
- [ ] 3PID registration flows
- [ ] Identity service integration

**Dependencies**: SMTP server configuration

---

## Phase 2: Operability & Bridges (3-6 months)

**Goal**: Production-ready admin tooling and bridge support.

### Admin & Moderation APIs - **S/M per endpoint**

**Estimated Effort**: 4-8 weeks total (1 engineer)

**Priority Endpoints**:

1. **User Management** (1-2 weeks)
   - List/search users
   - Deactivate user (GDPR compliance)
   - Edit user attributes
   - 3PID management per user

2. **Room Administration** (2-3 weeks)
   - Purge room history (targeted)
   - Admin ban/kick
   - Room state inspection
   - Room deletion

3. **Media Management** (1-2 weeks)
   - Quarantine media
   - Remote media purge
   - Media usage reports

4. **Moderation Tools** (1-2 weeks)
   - Shadow-banning
   - Rate limit inspection/override
   - Spam module hooks

**Milestones**:
- [ ] List/search users endpoint
- [ ] Deactivate user endpoint (GDPR-compliant)
- [ ] Room history purge
- [ ] Media quarantine
- [ ] Shadow-ban support

**Testing**: Admin API integration tests, GDPR compliance verification

---

### URL Previews - **M Effort**

**Estimated Effort**: 4-6 weeks (1 engineer)

**Why Important**: Core UX feature; link previews in messages.

**Implementation Plan**:

1. **Fetch & Parse** (2-3 weeks)
   - SSRF protections (IP allowlist, DNS rebinding prevention)
   - OpenGraph metadata extraction
   - Timeout and size limits
   - Location: `mediaapi/preview/`

2. **Caching** (1-2 weeks)
   - Preview metadata storage
   - TTL management
   - Cache invalidation

3. **Thumbnail Generation** (1 week)
   - OG image thumbnailing
   - Storage integration

4. **Security** (1 week)
   - Content-type validation
   - Malicious content filtering
   - Rate limiting per user

**Milestones**:
- [ ] Basic URL fetch working
- [ ] SSRF protections in place
- [ ] OpenGraph parsing
- [ ] Preview caching
- [ ] Thumbnail generation
- [ ] Security audit passed

**Security Review Required**: Yes (SSRF risks)

---

### Rate Limiting & Spam Hooks - **S/M Effort**

**Estimated Effort**: 2-4 weeks (1 engineer)

**Implementation Plan**:
- Configurable rate limit policies
- Per-endpoint rate limiting
- Spam checker module hooks
- Admin override capabilities

**Milestones**:
- [ ] Rate limit configuration schema
- [ ] Per-endpoint enforcement
- [ ] Spam module hook framework
- [ ] Admin override endpoints

---

## Phase 3: Scale & Hardening (6-12+ months, ongoing)

**Goal**: Production scale and operational excellence.

### Background Maintenance Jobs - **S/M per job**

**Jobs to Implement**:
- Event extremity pruning (prevent DAG bloat)
- Media retention policies
- Stale device cleanup
- Database vacuuming automation

**Estimated Effort**: 1-2 weeks per job

---

### Performance Optimization - **M Effort**

**Focus Areas**:
- Targeted caching (current state, device data, recent events)
- State resolution memoization
- Database query optimization
- JetStream tuning (batching, retention)

**Estimated Effort**: 4-8 weeks (ongoing)

**Metrics to Track**:
- Sync latency p95/p99
- Federation queue depth
- Database query times
- Memory usage under load

---

### Multi-Process Hardening - **XL Effort**

**Estimated Effort**: 6-12+ months (major architectural work)

**Scope**:
- Support multiple `syncapi` instances behind load balancer
- Federation sender parallelization
- State resolution worker pools
- Connection pooling across instances

**Prerequisites**: Phase 1 & 2 complete; significant production deployment experience

---

## Testing Strategy by Phase

### Phase 1a/1b: Feature Correctness
- Unit tests: ≥80% coverage
- Complement scenarios for each MSC
- Client compatibility matrix (Element Web, iOS, Android, X)
- Migration path testing

### Phase 2: Operational Readiness
- Admin API integration tests
- GDPR compliance verification
- Security audit (URL previews, spam filtering)
- Load testing with realistic traffic patterns

### Phase 3: Scale Testing
- Sustained load tests (500+ MAU)
- Failure injection and recovery
- Multi-instance coordination
- Performance regression tracking

---

## Dependencies & Sequencing

```
Sliding Sync (MSC4186) ─────┐
                            ├─> Combined better UX
Threads (MSC3440) ──────────┘

OIDC/MAS (MSC3861) ─────────┐
                            ├─> Account lifecycle
3PID flows ─────────────────┘

Cross-signing fixes ────────> Key Backup (MSC1212)

Admin APIs ─────────────────> Phase 3 Scale Operations

Phases 1a/1b ───────────────> Phase 2 ─────> Phase 3
```

---

## Resource Planning

**Minimum Team Size by Phase**:
- **Phase 1a**: 2 engineers (Sliding Sync + Threads in parallel)
- **Phase 1b**: 1-2 engineers (can overlap with 1a)
- **Phase 2**: 1-2 engineers
- **Phase 3**: 1-2 engineers (ongoing)

**Realistic Timeline**:
- **Phases 1a/1b**: 4-6 months (parallel work)
- **Phase 2**: 3-4 months (after 1a/1b core complete)
- **Phase 3**: 12+ months (continuous improvement)

**Total to Feature Parity**: ~12-18 months with 2-3 dedicated engineers

---

## Current Status (2025-10-22)

- ✅ Planning complete
- ⏳ Ready to begin Phase 1a
- 📋 GitHub issues created for Phase 1a/1b
- 📊 Project board tracking in place

---

## Next Steps

1. **Immediate**:
   - Create detailed technical design for Sliding Sync storage
   - Set up development environment for Sliding Sync work
   - Begin Threads completion (independent workstream)

2. **Week 2-4**:
   - Sliding Sync storage implementation
   - Threads sync filter implementation
   - OIDC config schema design

3. **Month 2**:
   - Sliding Sync core logic
   - Threads notification counts
   - OIDC login flow implementation

4. **Month 3-4**:
   - Sliding Sync Element X testing
   - Threads completion & testing
   - OIDC token management

---

## Success Criteria

Phase 1a/1b is complete when:
- [ ] Element X works fully with Dendrite (Sliding Sync)
- [ ] Thread support matches Element client expectations
- [ ] OIDC login works with major providers (Keycloak, Auth0, etc.)
- [ ] Key Backup functional across devices
- [ ] All Complement tests passing for implemented MSCs
- [ ] Performance benchmarks met (p95 sync <200ms)

---

For detailed MSC status, see [MSC_TRACKING.md](MSC_TRACKING.md).
For feature parity details, see [FEATURE_PARITY_STATUS.md](FEATURE_PARITY_STATUS.md).
