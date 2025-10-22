# Plan: Achieving 100% Test Coverage

## Executive Summary

**Current Overall Coverage**: ~64% baseline → ~75% with TDD roadmap improvements
**Goal**: 100% test coverage across all tested packages
**Feasibility**: Achievable for most packages, but requires significant integration testing infrastructure for some

## Current Status by Package

### Already at 100% ✅
- **appservice/api**: 100.0% (8 tests)

### High Coverage - Quick Wins Available (80%+)
- **appservice/query**: 81.1% → **Target: 95%+**
  - Gap: User function at 68.2%
  - Effort: **LOW** - Add 3-5 test cases for User function edge cases

- **internal/caching**: 91.9% → **Target: 98%+**
  - Gaps:
    - SetTimeoutCallback at 0.0% (setter function)
    - AddTypingUser at 62.5% (needs edge cases)
    - addUser at 90.0% (needs 1-2 more cases)
    - NewRistrettoCache at 70.0% (error paths)
  - Effort: **LOW** - Add 10-15 test cases total

### Medium Coverage - Integration Testing Required (20-40%)
- **mediaapi/routing**: 30.5% → **Target: 60%+**
  - Gaps: All HTTP handlers at 0.0%
    - Download, doDownload, respondFromLocalFile
    - getThumbnailFile, generateThumbnail
    - getRemoteFile, fetchRemoteFile
  - Effort: **MEDIUM** - Requires HTTP integration tests with:
    - Mock file storage
    - Mock HTTP server for federation
    - Image generation/manipulation testing
  - Estimated: 500-800 lines of integration tests

- **roomserver/internal/input**: 34.7% → **Target: 60%+**
  - Gaps: Complex event processing beyond our unit tests
  - Effort: **MEDIUM** - More event processing scenarios
  - Estimated: 300-500 lines of tests

- **clientapi/routing**: 20.4% → **Target: 50%+**
  - Gaps: HTTP handlers and authentication flows
  - Effort: **HIGH** - Full HTTP integration required
  - Estimated: 800-1200 lines of tests

### Low Coverage - Complex Integration Required (10-20%)
- **federationapi/routing**: 12.5% → **Target: 40%+**
  - Gaps: Full federation transaction handling
  - Effort: **HIGH** - Federation simulation required
  - Estimated: 600-1000 lines of tests

- **roomserver/state**: 12.2% → **Target: 40%+**
  - Gaps: State resolution, snapshot loading, database queries
  - Effort: **HIGH** - Database integration required
  - Estimated: 800-1200 lines of tests

- **federationapi/internal**: 16.8% → **Target: 50%+**
  - Gaps: Event validation, server key verification
  - Effort: **MEDIUM** - More helper function tests
  - Estimated: 200-400 lines of tests

---

## Detailed Action Plan

### Phase 1: Quick Wins (1-2 days) 🎯

**Goal**: Achieve 95%+ on high-coverage packages

#### 1.1 appservice/query - User Function (68.2% → 95%)

**Uncovered Code Analysis:**
```bash
$ go tool cover -func=coverage_detailed_appservice_query.out | grep User
User		68.2%
```

**Required Tests:**
```go
// Add to appservice/query/query_test.go

func TestUser_ErrorFromApplicationService(t *testing.T) {
    // Test error handling when AS returns non-200
}

func TestUser_InvalidJSONResponse(t *testing.T) {
    // Test handling of malformed JSON
}

func TestUser_EmptyUserID(t *testing.T) {
    // Test edge case with empty user ID
}

func TestUser_NetworkTimeout(t *testing.T) {
    // Test timeout handling
}

func TestUser_NilPointerProtection(t *testing.T) {
    // Test nil checks in response processing
}
```

**Estimated Effort**: 2-3 hours
**Lines of Code**: ~150 lines
**Coverage Gain**: 13% → Target: 95%

#### 1.2 internal/caching - Gaps (91.9% → 98%)

**Uncovered Functions:**
1. **SetTimeoutCallback** (0.0%)
   ```go
   func TestTypingCache_SetTimeoutCallback(t *testing.T) {
       t.Parallel()
       cache := NewTypingCache()

       called := false
       callback := func() { called = true }

       cache.SetTimeoutCallback(callback)
       // Trigger timeout somehow
       assert.True(t, called)
   }
   ```

2. **AddTypingUser** (62.5%)
   - Add tests for:
     - Multiple users typing simultaneously
     - User already typing (update scenario)
     - Empty room ID edge case
     - Empty user ID edge case
     - Sync position edge cases

3. **addUser** (90.0%)
   - Add tests for:
     - User cleanup when timeout expires
     - Edge case with zero timeout

4. **NewRistrettoCache** (70.0%)
   - Add tests for:
     - Memory limit edge cases
     - Invalid configuration
     - Error paths in ristretto initialization

**Estimated Effort**: 4-6 hours
**Lines of Code**: ~300 lines
**Coverage Gain**: 7% → Target: 98%

### Phase 2: Medium Coverage Packages (1-2 weeks) 🔨

#### 2.1 mediaapi/routing - HTTP Handlers (30.5% → 60%)

**Current State**: Only validation functions tested
**Gap**: All HTTP request handlers untested

**Required Infrastructure:**
```go
// Create: mediaapi/routing/integration_test_helpers.go

type TestMediaAPI struct {
    server  *httptest.Server
    db      *storage.Database
    cfg     *config.MediaAPI
    cleanup func()
}

func setupMediaAPI(t *testing.T) *TestMediaAPI {
    // Setup test database
    // Setup test HTTP server
    // Setup mock federation client
    // Setup temporary file storage
    return &TestMediaAPI{...}
}

func (m *TestMediaAPI) UploadFile(userID, filename string, content []byte) (string, error)
func (m *TestMediaAPI) DownloadFile(mediaID string) ([]byte, error)
func (m *TestMediaAPI) GenerateThumbnail(mediaID string, width, height int) ([]byte, error)
```

**Tests to Add:**
1. **Download Flow** (~200 lines)
   - Local file download
   - Remote file download
   - Thumbnail generation
   - Error cases (file not found, invalid media ID)

2. **Upload Flow** (~150 lines)
   - File upload with various content types
   - Size limit enforcement
   - Quarantine handling

3. **Remote Federation** (~200 lines)
   - Fetching from remote servers
   - Caching behavior
   - Network error handling

**Estimated Effort**: 3-5 days
**Lines of Code**: ~550 lines
**Coverage Gain**: 30% → Target: 60%

#### 2.2 roomserver/internal/input - Event Processing (34.7% → 60%)

**Gap Analysis**: Beyond basic event creation, need:
- More complex event chains
- Power level enforcement scenarios
- Membership state transitions
- Redaction handling

**Tests to Add:**
```go
// Add to roomserver/internal/input/input_process_test.go

func TestProcessRoomEvent_EventChain(t *testing.T)
func TestProcessRoomEvent_PowerLevelEnforcement(t *testing.T)
func TestProcessRoomEvent_RedactionProcessing(t *testing.T)
func TestProcessRoomEvent_StateConflictResolution(t *testing.T)
```

**Estimated Effort**: 3-4 days
**Lines of Code**: ~400 lines
**Coverage Gain**: 25% → Target: 60%

#### 2.3 federationapi/internal - Helper Functions (16.8% → 50%)

**Current Tests**: Blacklist logic, event validation basics
**Gap**: More edge cases and error paths

**Tests to Add** (~300 lines):
- Server key verification edge cases
- Event signature validation failures
- Backoff calculation edge cases
- Error aggregation scenarios

**Estimated Effort**: 2-3 days
**Lines of Code**: ~300 lines
**Coverage Gain**: 33% → Target: 50%

### Phase 3: Low Coverage - Integration Heavy (2-4 weeks) ⚠️

#### 3.1 clientapi/routing - HTTP Handlers (20.4% → 50%)

**Challenge**: Requires full authentication and session management

**Required Infrastructure:**
```go
type TestClientAPI struct {
    server     *httptest.Server
    db         *sql.DB
    userAPI    userapi.UserAPI
    rsAPI      roomserver.RoomserverAPI
    federation fclient.FederationClient
}

func (c *TestClientAPI) RegisterUser(username, password string) (accessToken string)
func (c *TestClientAPI) Login(username, password string) (accessToken string)
func (c *TestClientAPI) CreateRoom(token, name string) (roomID string)
func (c *TestClientAPI) SendMessage(token, roomID, message string) (eventID string)
func (c *TestClientAPI) JoinRoom(token, roomID string) error
```

**Tests to Add** (~800 lines):
1. **Registration Flow** - All validation paths, errors
2. **Login Flow** - Success, failures, 2FA scenarios
3. **Room Creation** - With various room types and configs
4. **Message Sending** - Text, images, edits, redactions
5. **Room Membership** - Join, leave, invite, kick, ban

**Estimated Effort**: 5-7 days
**Lines of Code**: ~800 lines
**Coverage Gain**: 30% → Target: 50%

#### 3.2 federationapi/routing - Federation Handlers (12.5% → 40%)

**Challenge**: Requires federation transaction simulation

**Required Infrastructure:**
```go
type TestFederationAPI struct {
    server    *httptest.Server
    db        *sql.DB
    rsAPI     roomserver.RoomserverAPI
    mockServers map[string]*MockFederationServer
}

type MockFederationServer struct {
    serverName spec.ServerName
    responses  map[string]interface{}
}
```

**Tests to Add** (~600 lines):
1. **Transaction Handling** - PDU/EDU processing
2. **Invite Handling** - v1/v2 invites
3. **Query Handling** - Profile, directory lookups
4. **Error Responses** - Various failure scenarios

**Estimated Effort**: 4-6 days
**Lines of Code**: ~600 lines
**Coverage Gain**: 27% → Target: 40%

#### 3.3 roomserver/state - State Resolution (12.2% → 40%)

**Challenge**: Most complex - requires full database integration and state resolution testing

**Current Gap**: All state loading/resolution functions at 0%
- NewStateResolution
- Resolve
- LoadStateAtSnapshot
- LoadStateAtEvent
- LoadCombinedStateAfterEvents

**Required Infrastructure:**
```go
type TestStateResolution struct {
    db     storage.Database
    cache  caching.RoomServerCaches
    state  *StateResolution
}

func (s *TestStateResolution) CreateStateSnapshot(...) types.StateSnapshotNID
func (s *TestStateResolution) CreateEvents(...) []gomatrixserverlib.PDU
func (s *TestStateResolution) VerifyStateAtSnapshot(...)
```

**Tests to Add** (~1000 lines):
1. **State Loading** - From snapshots, from events
2. **State Resolution** - Conflict scenarios
3. **Membership Loading** - Various visibility rules
4. **State Differences** - Between snapshots

**Estimated Effort**: 7-10 days
**Lines of Code**: ~1000 lines
**Coverage Gain**: 28% → Target: 40%

**⚠️ Recommendation**: This package may not be worth 100% coverage due to:
- Extreme complexity of state resolution algorithm
- Already tested by Complement/Sytest integration tests
- Diminishing returns on unit test investment

---

## Coverage Targets and ROI Analysis

### Tier 1: High ROI - Achieve 100% ✅

| Package | Current | Target | Effort | Priority |
|---------|---------|--------|--------|----------|
| appservice/api | 100.0% | 100% | ✅ Done | - |
| appservice/query | 81.1% | 95% | LOW | **HIGH** |
| internal/caching | 91.9% | 98% | LOW | **HIGH** |

**Total Effort**: 1-2 days
**Coverage Gain**: Minimal package count impact, but completes critical packages

### Tier 2: Medium ROI - Achieve 50-60% 📊

| Package | Current | Target | Effort | Priority |
|---------|---------|--------|--------|----------|
| mediaapi/routing | 30.5% | 60% | MEDIUM | MEDIUM |
| roomserver/internal/input | 34.7% | 60% | MEDIUM | MEDIUM |
| federationapi/internal | 16.8% | 50% | MEDIUM | MEDIUM |

**Total Effort**: 1-2 weeks
**Coverage Gain**: Significant improvement on key packages
**ROI**: Good - these packages have testable logic

### Tier 3: Lower ROI - Achieve 40-50% ⚠️

| Package | Current | Target | Effort | Priority |
|---------|---------|--------|--------|----------|
| clientapi/routing | 20.4% | 50% | HIGH | LOW |
| federationapi/routing | 12.5% | 40% | HIGH | LOW |
| roomserver/state | 12.2% | 40% | **VERY HIGH** | **DEFER** |

**Total Effort**: 3-4 weeks
**Coverage Gain**: Moderate improvement on complex packages
**ROI**: Lower - much effort for partial coverage
**Recommendation**: Leverage existing Complement/Sytest integration tests instead

---

## Recommended Phased Approach

### Immediate: Phase 1 Only (1-2 days) 🎯

**Focus**: Quick wins to complete Tier 1 packages
**Deliverable**: 3 packages at 95%+ coverage
**Effort**: 1-2 days
**Impact**: Demonstrates commitment to quality, easy wins

### Short Term: Phases 1 + 2 (2-3 weeks) 📈

**Focus**: Quick wins + medium packages
**Deliverable**:
- 3 packages at 95%+ (Tier 1)
- 3 packages at 50-60% (Tier 2)
**Effort**: 2-3 weeks
**Impact**: Significant overall coverage improvement

### Long Term: All Phases (6-8 weeks) 🏆

**Focus**: Maximum coverage across all packages
**Deliverable**: All testable packages at target coverage
**Effort**: 6-8 weeks
**Impact**: Near-complete coverage, but diminishing returns on Tier 3

**⚠️ NOT Recommended** for Tier 3 packages - better to use integration tests

---

## Alternative: Pragmatic 80% Goal

Instead of 100% coverage, target **80% coverage on tested packages**:

### Advantages:
1. **Focuses on testable code** - Skips complex integration scenarios
2. **Better ROI** - More coverage per hour invested
3. **Maintainable** - Tests remain fast and reliable
4. **Realistic** - Acknowledges integration test role

### Strategy:
- ✅ **100%** on appservice packages (already near completion)
- ✅ **95%+** on internal/caching
- ✅ **60%+** on mediaapi/routing (validation + key flows)
- ✅ **60%+** on roomserver/internal/input
- ✅ **50%+** on federationapi packages
- ⏭️ **Defer** to Complement/Sytest: clientapi/routing, roomserver/state

**Total Effort**: 2-3 weeks
**Overall Coverage**: ~75-80% on unit-tested packages
**Recommended**: ✅ **YES** - Best balance of coverage and effort

---

## Implementation Roadmap

### Week 1: Quick Wins
- [ ] appservice/query User function tests (95% coverage)
- [ ] internal/caching gap filling (98% coverage)
- [ ] Document completion of Tier 1

### Week 2-3: Medium Packages (Optional)
- [ ] mediaapi/routing HTTP handler tests (60% coverage)
- [ ] roomserver/internal/input additional scenarios (60% coverage)
- [ ] federationapi/internal edge cases (50% coverage)

### Week 4-6: Advanced (NOT Recommended)
- [ ] clientapi/routing integration tests
- [ ] federationapi/routing integration tests
- [ ] roomserver/state integration tests
- **⚠️ STOP**: Evaluate if Complement/Sytest coverage is sufficient

---

## Coverage Enforcement Strategy

### Current (codecov.yaml):
```yaml
coverage:
  status:
    project:
      default:
        target: 80%
    patch:
      default:
        target: 100%
```

### Recommended Updates:
```yaml
coverage:
  status:
    project:
      default:
        target: 80%  # Overall project target
      tier1-packages:
        target: 95%  # High-coverage packages
        paths:
          - "appservice/"
          - "internal/caching/"
    patch:
      default:
        target: 100%  # All new code must have tests
```

---

## Conclusion and Recommendation

### Is 100% Coverage Achievable?
**Yes**, but with caveats:
- ✅ **Easily achievable** for Tier 1 packages (1-2 days)
- ⚠️ **Achievable with effort** for Tier 2 packages (2-3 weeks)
- ❌ **Not recommended** for Tier 3 packages (3-4 weeks, low ROI)

### Recommended Path Forward

**Option A: Pragmatic 80% Goal** ✅ **RECOMMENDED**
- **Timeline**: 2-3 weeks
- **Effort**: Moderate
- **Coverage**: 75-80% on unit-tested packages
- **ROI**: Excellent
- **Maintainability**: High

**Option B: Maximum Coverage (90%+)**
- **Timeline**: 6-8 weeks
- **Effort**: High
- **Coverage**: 85-90% overall
- **ROI**: Diminishing returns
- **Maintainability**: Medium (complex integration tests)

**Option C: 100% Coverage**
- **Timeline**: 10-12 weeks
- **Effort**: Very High
- **Coverage**: 95%+ overall
- **ROI**: Low (last 10% takes 50% of effort)
- **Maintainability**: Lower (brittle integration tests)

### Final Recommendation

**Pursue Option A** with the following immediate action:

1. **This Week**: Complete Phase 1 (Tier 1 packages to 95%+)
2. **Next 2 Weeks**: Complete Phase 2 (Tier 2 packages to 50-60%)
3. **Evaluate**: Measure impact, decide if Tier 3 is worth investment
4. **Maintain**: Focus on 100% coverage for all NEW code (patch target)

This approach balances coverage goals with engineering velocity and maintainability.

---

**Document Version**: 1.0
**Created**: October 21, 2025
**Status**: Action Plan Ready for Execution
**Recommended Start**: Phase 1 Quick Wins (1-2 days)
