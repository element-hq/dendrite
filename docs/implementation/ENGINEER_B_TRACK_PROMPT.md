# Engineer B Track: Independent Features Implementation

**Timeline**: 8-10 weeks (parallelizable with Engineer A's admin API track)

**Prerequisites**: None - all tasks in this track are independent of Phase 0

**Branch Strategy**: Create separate branches for each task to enable clean PRs and independent reviews

---

## Overview

Engineer B's track focuses on independent feature improvements and integrations that don't depend on the admin API infrastructure. These tasks can proceed immediately while Engineer A works on Phase 0 and the admin endpoints.

**Task Sequence**:
1. **Week 1**: Task #3 - Rate Limiting Config (2-3 days) - S
2. **Week 1-2**: Task #4 - Prometheus Metrics Expansion (3-4 days) - S
3. **Week 2-3**: Task #5 - Password Reset Flow (3-5 days) - S
4. **Week 4-5**: Task #8 - URL Previews (1-2 weeks) - M
5. **Week 6-7**: Task #9 - 3PID Email Verification (1-2 weeks) - M
6. **Week 8-10**: Task #10 - Thread Notification Counts (1-2 weeks) - M

---

## Task #3: Enhanced Rate Limiting Configuration (S - 2-3 days)

**Priority**: P0
**Files**: `setup/config/config_clientapi.go`, `clientapi/routing/ratelimit.go`

### Current State

Rate limiting config exists but is basic:
```go
// setup/config/config_clientapi.go:136-151
type RateLimiting struct {
    Enabled         bool     `yaml:"enabled"`
    Threshold       int64    `yaml:"threshold"`
    CooloffMS       int64    `yaml:"cooloff_ms"`
    ExemptUserIDs   []string `yaml:"exempt_user_ids"`
}
```

### What's Missing

- Per-endpoint rate limit overrides
- IP-based exemptions (trusted proxies, local networks)
- Per-second and burst controls
- Rate limit metrics
- Dynamic adjustment without restart

### TDD Implementation Cycles

#### Cycle 1: Add Per-Endpoint Override Config

**Test** (`setup/config/config_clientapi_test.go`):
```go
func TestRateLimitingPerEndpointOverrides(t *testing.T) {
    yaml := `
client_api:
  rate_limiting:
    enabled: true
    threshold: 10
    cooloff_ms: 500
    per_endpoint_overrides:
      /_matrix/client/v3/register:
        threshold: 3
        cooloff_ms: 1000
      /_matrix/client/v3/login:
        threshold: 5
        cooloff_ms: 2000
`
    cfg, err := loadConfig(yaml)
    require.NoError(t, err)

    assert.Equal(t, int64(3), cfg.ClientAPI.RateLimiting.PerEndpointOverrides["/_matrix/client/v3/register"].Threshold)
    assert.Equal(t, int64(1000), cfg.ClientAPI.RateLimiting.PerEndpointOverrides["/_matrix/client/v3/register"].CooloffMS)
}
```

**Implementation** (`setup/config/config_clientapi.go`):
```go
type RateLimitEndpointOverride struct {
    Threshold int64 `yaml:"threshold"`
    CooloffMS int64 `yaml:"cooloff_ms"`
}

type RateLimiting struct {
    Enabled               bool                                 `yaml:"enabled"`
    Threshold             int64                                `yaml:"threshold"`
    CooloffMS             int64                                `yaml:"cooloff_ms"`
    ExemptUserIDs         []string                             `yaml:"exempt_user_ids"`
    ExemptIPAddresses     []string                             `yaml:"exempt_ip_addresses"`
    PerEndpointOverrides  map[string]RateLimitEndpointOverride `yaml:"per_endpoint_overrides"`
}
```

**Refactor**: Add validation for IP address format in `Verify()` method.

#### Cycle 2: Implement Per-Second and Burst Controls

**Test** (`clientapi/routing/ratelimit_test.go`):
```go
func TestRateLimiterTokenBucket(t *testing.T) {
    cfg := &config.RateLimiting{
        Enabled:      true,
        Threshold:    5,  // 5 requests per second
        CooloffMS:    1000,
        BurstSize:    10, // Allow bursts up to 10
    }

    limiter := NewRateLimiter(cfg)

    // Should allow burst of 10
    for i := 0; i < 10; i++ {
        allowed, err := limiter.Check("@user:example.com", "/_matrix/client/v3/sync")
        assert.NoError(t, err)
        assert.True(t, allowed)
    }

    // 11th request should be rate limited
    allowed, err := limiter.Check("@user:example.com", "/_matrix/client/v3/sync")
    assert.NoError(t, err)
    assert.False(t, allowed)
}
```

**Implementation** (`clientapi/routing/ratelimit.go`):
```go
import "golang.org/x/time/rate"

type RateLimiter struct {
    limiters map[string]*rate.Limiter
    mu       sync.RWMutex
    cfg      *config.RateLimiting
}

func NewRateLimiter(cfg *config.RateLimiting) *RateLimiter {
    return &RateLimiter{
        limiters: make(map[string]*rate.Limiter),
        cfg:      cfg,
    }
}

func (r *RateLimiter) Check(userID, endpoint string) (bool, error) {
    // Check exemptions
    for _, exemptUser := range r.cfg.ExemptUserIDs {
        if userID == exemptUser {
            return true, nil
        }
    }

    // Get or create limiter for this user+endpoint
    key := userID + ":" + endpoint
    limiter := r.getLimiter(key, endpoint)

    return limiter.Allow(), nil
}

func (r *RateLimiter) getLimiter(key, endpoint string) *rate.Limiter {
    r.mu.Lock()
    defer r.mu.Unlock()

    limiter, exists := r.limiters[key]
    if !exists {
        // Check for endpoint override
        override, hasOverride := r.cfg.PerEndpointOverrides[endpoint]

        var limit rate.Limit
        var burst int
        if hasOverride {
            limit = rate.Limit(override.Threshold)
            burst = int(override.Threshold * 2) // Default burst = 2x threshold
        } else {
            limit = rate.Limit(r.cfg.Threshold)
            burst = int(r.cfg.Threshold * 2)
        }

        limiter = rate.NewLimiter(limit, burst)
        r.limiters[key] = limiter
    }

    return limiter
}
```

**Refactor**: Add cleanup for stale limiters (LRU cache with TTL).

#### Cycle 3: Add Rate Limit Metrics

**Test** (`clientapi/routing/ratelimit_test.go`):
```go
func TestRateLimiterMetrics(t *testing.T) {
    cfg := &config.RateLimiting{Enabled: true, Threshold: 5, CooloffMS: 1000}
    limiter := NewRateLimiter(cfg)

    // Trigger rate limits
    for i := 0; i < 20; i++ {
        limiter.Check("@user:example.com", "/_matrix/client/v3/sync")
    }

    // Check metrics were recorded
    metrics := testutil.CollectAndCompare(rateLimitRejections, strings.NewReader(`
# HELP dendrite_clientapi_rate_limit_rejections Total rate limit rejections
# TYPE dendrite_clientapi_rate_limit_rejections counter
dendrite_clientapi_rate_limit_rejections{endpoint="/_matrix/client/v3/sync"} 15
    `))
    assert.NoError(t, metrics)
}
```

**Implementation** (add to `clientapi/routing/ratelimit.go`):
```go
var (
    rateLimitRejections = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Namespace: "dendrite",
            Subsystem: "clientapi",
            Name:      "rate_limit_rejections",
            Help:      "Total number of requests rejected by rate limiting",
        },
        []string{"endpoint"},
    )

    rateLimitAllowed = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Namespace: "dendrite",
            Subsystem: "clientapi",
            Name:      "rate_limit_allowed",
            Help:      "Total number of requests allowed by rate limiting",
        },
        []string{"endpoint"},
    )
)

func init() {
    prometheus.MustRegister(rateLimitRejections, rateLimitAllowed)
}

func (r *RateLimiter) Check(userID, endpoint string) (bool, error) {
    // ... existing code ...

    allowed := limiter.Allow()
    if allowed {
        rateLimitAllowed.WithLabelValues(endpoint).Inc()
    } else {
        rateLimitRejections.WithLabelValues(endpoint).Inc()
    }

    return allowed, nil
}
```

#### Cycle 4: IP-Based Exemptions

**Test** (`clientapi/routing/ratelimit_test.go`):
```go
func TestRateLimiterIPExemptions(t *testing.T) {
    cfg := &config.RateLimiting{
        Enabled:           true,
        Threshold:         5,
        CooloffMS:         1000,
        ExemptIPAddresses: []string{"127.0.0.1", "192.168.1.0/24"},
    }
    limiter := NewRateLimiter(cfg)

    // Should allow unlimited requests from exempt IP
    for i := 0; i < 100; i++ {
        allowed, err := limiter.CheckWithIP("@user:example.com", "/_matrix/client/v3/sync", "127.0.0.1")
        assert.NoError(t, err)
        assert.True(t, allowed)
    }

    // Should rate limit non-exempt IP
    for i := 0; i < 10; i++ {
        limiter.CheckWithIP("@user:example.com", "/_matrix/client/v3/sync", "203.0.113.1")
    }
    allowed, err := limiter.CheckWithIP("@user:example.com", "/_matrix/client/v3/sync", "203.0.113.1")
    assert.NoError(t, err)
    assert.False(t, allowed)
}
```

**Implementation**: Add IP CIDR matching logic to `Check()` method.

### Acceptance Criteria

- ✅ Per-endpoint rate limit overrides work
- ✅ IP-based exemptions (CIDR notation supported)
- ✅ Token bucket algorithm with burst control
- ✅ Prometheus metrics for rejections/allowed
- ✅ Config validation for IP addresses
- ✅ ≥80% test coverage
- ✅ Backwards compatible config (old configs still work)
- ✅ Documentation in `dendrite-sample.yaml`

---

## Task #4: Prometheus Metrics Expansion (S - 3-4 days)

**Priority**: P1
**Files**: Various across `clientapi/`, `syncapi/`, `federationapi/`, `mediaapi/`

### Missing Metrics

1. `dendrite_clientapi_http_request_duration_seconds` (histogram)
2. `dendrite_syncapi_sync_duration_seconds` (histogram)
3. `dendrite_syncapi_sync_lag_seconds` (gauge) - time since last event
4. `dendrite_federationapi_send_queue_depth` (gauge)
5. `dendrite_mediaapi_thumbnail_cache_hit_ratio` (gauge)

**Note**: `dendrite_syncapi_active_sync_requests` already exists at `syncapi/sync/requestpool.go:257`

### TDD Implementation Cycles

#### Cycle 1: HTTP Request Duration Histogram

**Test** (`clientapi/routing/routing_test.go`):
```go
func TestHTTPRequestDurationMetrics(t *testing.T) {
    // Create test router with metrics middleware
    router := mux.NewRouter()
    router.Use(metricsMiddleware)
    router.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
        time.Sleep(100 * time.Millisecond)
        w.WriteHeader(http.StatusOK)
    })

    // Make request
    req := httptest.NewRequest("GET", "/test", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    // Check metric was recorded
    metrics, err := prometheus.DefaultGatherer.Gather()
    require.NoError(t, err)

    found := false
    for _, mf := range metrics {
        if mf.GetName() == "dendrite_clientapi_http_request_duration_seconds" {
            found = true
            histogram := mf.GetMetric()[0].GetHistogram()
            assert.Greater(t, histogram.GetSampleSum(), 0.1) // Should be >= 100ms
        }
    }
    assert.True(t, found, "Metric not found")
}
```

**Implementation** (`clientapi/routing/routing.go`):
```go
var (
    httpRequestDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Namespace: "dendrite",
            Subsystem: "clientapi",
            Name:      "http_request_duration_seconds",
            Help:      "Duration of HTTP requests in seconds",
            Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
        },
        []string{"method", "path", "code"},
    )
)

func init() {
    prometheus.MustRegister(httpRequestDuration)
}

func metricsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()

        // Wrap ResponseWriter to capture status code
        wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

        next.ServeHTTP(wrapped, r)

        duration := time.Since(start).Seconds()
        httpRequestDuration.WithLabelValues(
            r.Method,
            r.URL.Path,
            strconv.Itoa(wrapped.statusCode),
        ).Observe(duration)
    })
}

type responseWriter struct {
    http.ResponseWriter
    statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
    rw.statusCode = code
    rw.ResponseWriter.WriteHeader(code)
}
```

**Refactor**: Add middleware to main router in `Setup()`.

#### Cycle 2: Sync Duration and Lag Metrics

**Test** (`syncapi/sync/requestpool_test.go`):
```go
func TestSyncDurationAndLagMetrics(t *testing.T) {
    pool := NewRequestPool(...)

    req := &syncRequest{
        userID: "@alice:example.com",
        device: &userapi.Device{ID: "DEVICE1"},
    }

    // Simulate sync with some processing time
    start := time.Now()
    pool.OnIncomingSyncRequest(req, &syncResponse{})
    duration := time.Since(start).Seconds()

    // Check duration metric
    metrics := testutil.CollectAndCompare(syncDuration, strings.NewReader(`
# HELP dendrite_syncapi_sync_duration_seconds Histogram of sync request durations
# TYPE dendrite_syncapi_sync_duration_seconds histogram
dendrite_syncapi_sync_duration_seconds_count{user="@alice:example.com"} 1
    `))
    assert.NoError(t, metrics)

    // Check lag metric (time since last event)
    lagMetric := testutil.ToFloat64(syncLag.WithLabelValues("@alice:example.com"))
    assert.Greater(t, lagMetric, 0.0)
}
```

**Implementation** (`syncapi/sync/requestpool.go`):
```go
var (
    syncDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Namespace: "dendrite",
            Subsystem: "syncapi",
            Name:      "sync_duration_seconds",
            Help:      "Histogram of sync request processing durations",
            Buckets:   []float64{.01, .05, .1, .5, 1, 5, 10, 30},
        },
        []string{"user"},
    )

    syncLag = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Namespace: "dendrite",
            Subsystem: "syncapi",
            Name:      "sync_lag_seconds",
            Help:      "Time in seconds since the most recent event for this user",
        },
        []string{"user"},
    )
)

func init() {
    prometheus.MustRegister(syncDuration, syncLag)
}

func (rp *RequestPool) OnIncomingSyncRequest(req *syncRequest, res *syncResponse) {
    start := time.Now()
    defer func() {
        duration := time.Since(start).Seconds()
        syncDuration.WithLabelValues(req.userID).Observe(duration)
    }()

    // ... existing sync logic ...

    // Calculate lag (time since most recent event)
    if len(res.Rooms.Join) > 0 {
        var mostRecentTimestamp int64
        for _, room := range res.Rooms.Join {
            for _, event := range room.Timeline.Events {
                if event.OriginServerTS() > mostRecentTimestamp {
                    mostRecentTimestamp = event.OriginServerTS()
                }
            }
        }

        if mostRecentTimestamp > 0 {
            lag := float64(time.Now().UnixMilli()-mostRecentTimestamp) / 1000.0
            syncLag.WithLabelValues(req.userID).Set(lag)
        }
    }
}
```

#### Cycle 3: Federation Send Queue Depth

**Test** (`federationapi/queue/queue_test.go`):
```go
func TestFederationSendQueueDepthMetric(t *testing.T) {
    queue := NewOutgoingQueues(...)

    // Queue some events
    for i := 0; i < 10; i++ {
        queue.SendEvent(event, "destination.server")
    }

    // Check metric
    depth := testutil.ToFloat64(federationSendQueueDepth)
    assert.Equal(t, 10.0, depth)

    // Process events
    queue.processQueue()

    // Check metric decreased
    depth = testutil.ToFloat64(federationSendQueueDepth)
    assert.Equal(t, 0.0, depth)
}
```

**Implementation** (`federationapi/queue/queue.go`):
```go
var (
    federationSendQueueDepth = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Namespace: "dendrite",
            Subsystem: "federationapi",
            Name:      "send_queue_depth",
            Help:      "Number of events queued for federation sending",
        },
    )
)

func init() {
    prometheus.MustRegister(federationSendQueueDepth)
}

func (oqs *OutgoingQueues) SendEvent(event *gomatrixserverlib.HeaderedEvent, destination spec.ServerName) {
    oqs.queues[destination].push(event)
    federationSendQueueDepth.Inc()
}

func (q *destinationQueue) sendEvent(event *gomatrixserverlib.HeaderedEvent) error {
    defer federationSendQueueDepth.Dec()
    // ... existing send logic ...
}
```

#### Cycle 4: Media Thumbnail Cache Hit Ratio

**Test** (`mediaapi/thumbnailer/thumbnailer_test.go`):
```go
func TestThumbnailCacheHitRatioMetric(t *testing.T) {
    thumbnailer := NewThumbnailer(...)

    // First request (miss)
    thumbnailer.GetThumbnail("media1", 64, 64)

    // Second request (hit)
    thumbnailer.GetThumbnail("media1", 64, 64)

    // Check hit ratio = 0.5 (1 hit / 2 requests)
    hitRatio := testutil.ToFloat64(thumbnailCacheHitRatio)
    assert.InDelta(t, 0.5, hitRatio, 0.01)
}
```

**Implementation** (`mediaapi/thumbnailer/thumbnailer.go`):
```go
var (
    thumbnailCacheHits = prometheus.NewCounter(
        prometheus.CounterOpts{
            Namespace: "dendrite",
            Subsystem: "mediaapi",
            Name:      "thumbnail_cache_hits",
            Help:      "Number of thumbnail cache hits",
        },
    )

    thumbnailCacheMisses = prometheus.NewCounter(
        prometheus.CounterOpts{
            Namespace: "dendrite",
            Subsystem: "mediaapi",
            Name:      "thumbnail_cache_misses",
            Help:      "Number of thumbnail cache misses",
        },
    )

    thumbnailCacheHitRatio = prometheus.NewGaugeFunc(
        prometheus.GaugeOpts{
            Namespace: "dendrite",
            Subsystem: "mediaapi",
            Name:      "thumbnail_cache_hit_ratio",
            Help:      "Ratio of cache hits to total requests",
        },
        func() float64 {
            hits := testutil.ToFloat64(thumbnailCacheHits)
            misses := testutil.ToFloat64(thumbnailCacheMisses)
            total := hits + misses
            if total == 0 {
                return 0
            }
            return hits / total
        },
    )
)

func init() {
    prometheus.MustRegister(thumbnailCacheHits, thumbnailCacheMisses, thumbnailCacheHitRatio)
}

func (t *Thumbnailer) GetThumbnail(mediaID string, width, height int) ([]byte, error) {
    // Check cache
    if cached, found := t.cache.Get(cacheKey(mediaID, width, height)); found {
        thumbnailCacheHits.Inc()
        return cached.([]byte), nil
    }

    thumbnailCacheMisses.Inc()
    // ... generate thumbnail ...
}
```

### Acceptance Criteria

- ✅ All 5 new metrics implemented and exposed
- ✅ Metrics follow Prometheus naming conventions
- ✅ Appropriate metric types (histogram, gauge, counter)
- ✅ Labels are low-cardinality (no unbounded user_id labels except where necessary)
- ✅ ≥80% test coverage for metric recording logic
- ✅ Documentation added to `docs/metrics.md`
- ✅ Sample Grafana dashboard JSON provided

---

## Task #5: Password Reset Flow (S - 3-5 days)

**Priority**: P1
**Files**: `clientapi/routing/`, `userapi/storage/`

### Current State

No password reset endpoints exist. Users cannot reset forgotten passwords.

### Required Endpoints

1. `POST /_matrix/client/v3/account/password/email/requestToken`
2. `POST /_matrix/client/v3/account/password`

### TDD Implementation Cycles

#### Cycle 1: Database Schema for Reset Tokens

**Test** (`userapi/storage/postgres/password_reset_test.go`):
```go
func TestPasswordResetTokenStorage(t *testing.T) {
    db := mustCreateDatabase(t)

    // Create reset token
    token := util.RandomString(32)
    err := db.StorePasswordResetToken("@alice:example.com", "alice@example.com", token, time.Now().Add(1*time.Hour))
    assert.NoError(t, err)

    // Retrieve token
    userID, email, expiry, err := db.GetPasswordResetToken(token)
    assert.NoError(t, err)
    assert.Equal(t, "@alice:example.com", userID)
    assert.Equal(t, "alice@example.com", email)
    assert.True(t, expiry.After(time.Now()))

    // Consume token
    err = db.ConsumePasswordResetToken(token)
    assert.NoError(t, err)

    // Should not be retrievable again
    _, _, _, err = db.GetPasswordResetToken(token)
    assert.Error(t, err)
}
```

**Migration** (`userapi/storage/postgres/deltas/20250122_password_reset_tokens.sql`):
```sql
CREATE TABLE IF NOT EXISTS userapi_password_reset_tokens (
    token TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    email TEXT NOT NULL,
    expires_at BIGINT NOT NULL,
    consumed_at BIGINT,
    created_at BIGINT NOT NULL DEFAULT extract(epoch from now()) * 1000
);

CREATE INDEX idx_password_reset_tokens_user_id ON userapi_password_reset_tokens(user_id);
CREATE INDEX idx_password_reset_tokens_expires_at ON userapi_password_reset_tokens(expires_at);
```

**Implementation** (`userapi/storage/postgres/password_reset_table.go`):
```go
const insertPasswordResetTokenSQL = `
    INSERT INTO userapi_password_reset_tokens (token, user_id, email, expires_at)
    VALUES ($1, $2, $3, $4)
`

const selectPasswordResetTokenSQL = `
    SELECT user_id, email, expires_at FROM userapi_password_reset_tokens
    WHERE token = $1 AND consumed_at IS NULL AND expires_at > $2
`

const consumePasswordResetTokenSQL = `
    UPDATE userapi_password_reset_tokens SET consumed_at = $1 WHERE token = $2
`

func (s *passwordResetStatements) InsertPasswordResetToken(
    ctx context.Context, txn *sql.Tx,
    token, userID, email string, expiresAt time.Time,
) error {
    stmt := sqlutil.TxStmt(txn, s.insertPasswordResetTokenStmt)
    _, err := stmt.ExecContext(ctx, token, userID, email, expiresAt.UnixMilli())
    return err
}
```

#### Cycle 2: Email Request Token Endpoint

**Test** (`clientapi/routing/password_reset_test.go`):
```go
func TestPasswordResetRequestToken(t *testing.T) {
    cfg := setup.NewTestConfig()
    userAPI := userapi.NewInternalAPI(...)

    // Setup test user with email
    userAPI.PerformAccountCreation(context.Background(), &api.PerformAccountCreationRequest{
        AccountType: api.AccountTypeUser,
        Localpart:   "alice",
        Password:    "old_password",
    })

    // Add 3PID email
    userAPI.PerformSaveThreePIDAssociation(...)

    // Request reset token
    reqBody := map[string]interface{}{
        "client_secret": "test_secret",
        "email":         "alice@example.com",
        "send_attempt":  1,
    }

    req := httptest.NewRequest("POST", "/_matrix/client/v3/account/password/email/requestToken", jsonBody(reqBody))
    w := httptest.NewRecorder()

    RequestPasswordResetToken(w, req, userAPI, cfg)

    assert.Equal(t, http.StatusOK, w.Code)

    var res map[string]interface{}
    json.Unmarshal(w.Body.Bytes(), &res)

    assert.Contains(t, res, "sid") // Session ID

    // Verify email was sent (mock)
    assert.Equal(t, 1, len(mockEmailSender.SentEmails))
    assert.Contains(t, mockEmailSender.SentEmails[0].Body, "reset your password")
}
```

**Implementation** (`clientapi/routing/password_reset.go`):
```go
func RequestPasswordResetToken(
    w http.ResponseWriter, req *http.Request,
    userAPI userapi.ClientUserAPI,
    cfg *config.ClientAPI,
) util.JSONResponse {
    var r struct {
        ClientSecret string `json:"client_secret"`
        Email        string `json:"email"`
        SendAttempt  int    `json:"send_attempt"`
    }

    if err := json.NewDecoder(req.Body).Decode(&r); err != nil {
        return util.JSONResponse{Code: http.StatusBadRequest}
    }

    // Validate email format
    if !isValidEmail(r.Email) {
        return util.JSONResponse{Code: http.StatusBadRequest}
    }

    // Find user by email (3PID lookup)
    userID, err := userAPI.QueryLocalpartForThreePID(req.Context(), r.Email, "email")
    if err != nil {
        // Return success even if user not found (privacy)
        return util.JSONResponse{Code: http.StatusOK, JSON: map[string]string{"sid": "dummy"}}
    }

    // Generate reset token
    token := util.RandomString(32)
    sessionID := util.RandomString(16)

    // Store token (valid for 1 hour)
    err = userAPI.StorePasswordResetToken(req.Context(), userID, r.Email, token, time.Now().Add(1*time.Hour))
    if err != nil {
        return util.ErrorResponse(err)
    }

    // Send email
    resetLink := fmt.Sprintf("%s/_matrix/client/v3/password_reset/validate?token=%s", cfg.BaseURL, token)
    err = sendPasswordResetEmail(r.Email, resetLink, cfg)
    if err != nil {
        return util.ErrorResponse(err)
    }

    return util.JSONResponse{
        Code: http.StatusOK,
        JSON: map[string]string{"sid": sessionID},
    }
}
```

#### Cycle 3: Password Change Endpoint

**Test** (`clientapi/routing/password_reset_test.go`):
```go
func TestPasswordResetWithToken(t *testing.T) {
    cfg := setup.NewTestConfig()
    userAPI := userapi.NewInternalAPI(...)

    // Create user
    userAPI.PerformAccountCreation(...)

    // Create valid reset token
    token := "valid_reset_token_123"
    userAPI.StorePasswordResetToken(context.Background(), "@alice:example.com", "alice@example.com", token, time.Now().Add(1*time.Hour))

    // Reset password
    reqBody := map[string]interface{}{
        "auth": map[string]interface{}{
            "type":  "m.login.email.identity",
            "threepid_creds": map[string]interface{}{
                "sid":           "session_id",
                "client_secret": "test_secret",
            },
        },
        "new_password": "new_secure_password_123",
    }

    req := httptest.NewRequest("POST", "/_matrix/client/v3/account/password?token="+token, jsonBody(reqBody))
    w := httptest.NewRecorder()

    PasswordReset(w, req, userAPI, cfg)

    assert.Equal(t, http.StatusOK, w.Code)

    // Verify password was changed
    loginReq := &api.QueryAccountByPasswordRequest{
        Localpart:         "alice",
        PlaintextPassword: "new_secure_password_123",
    }
    loginRes := &api.QueryAccountByPasswordResponse{}
    userAPI.QueryAccountByPassword(context.Background(), loginReq, loginRes)

    assert.True(t, loginRes.Exists)
    assert.True(t, loginRes.PasswordMatches)
}
```

**Implementation** (`clientapi/routing/password_reset.go`):
```go
func PasswordReset(
    w http.ResponseWriter, req *http.Request,
    userAPI userapi.ClientUserAPI,
    cfg *config.ClientAPI,
) util.JSONResponse {
    var r struct {
        Auth struct {
            Type          string `json:"type"`
            ThreePIDCreds struct {
                SID          string `json:"sid"`
                ClientSecret string `json:"client_secret"`
            } `json:"threepid_creds"`
        } `json:"auth"`
        NewPassword string `json:"new_password"`
    }

    if err := json.NewDecoder(req.Body).Decode(&r); err != nil {
        return util.JSONResponse{Code: http.StatusBadRequest}
    }

    // Get token from query param
    token := req.URL.Query().Get("token")
    if token == "" {
        return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.MissingParam("token")}
    }

    // Validate token
    userID, email, _, err := userAPI.GetPasswordResetToken(req.Context(), token)
    if err != nil {
        return util.JSONResponse{Code: http.StatusForbidden, JSON: spec.Forbidden("Invalid or expired token")}
    }

    // Consume token (one-time use)
    err = userAPI.ConsumePasswordResetToken(req.Context(), token)
    if err != nil {
        return util.ErrorResponse(err)
    }

    // Update password
    err = userAPI.PerformPasswordUpdate(req.Context(), &api.PerformPasswordUpdateRequest{
        Localpart: localpartFromUserID(userID),
        Password:  r.NewPassword,
    })
    if err != nil {
        return util.ErrorResponse(err)
    }

    // Revoke all access tokens for security
    err = userAPI.PerformDeviceDeletion(req.Context(), &api.PerformDeviceDeletionRequest{
        UserID: userID,
    })
    if err != nil {
        return util.ErrorResponse(err)
    }

    return util.JSONResponse{Code: http.StatusOK, JSON: struct{}{}}
}
```

#### Cycle 4: Email Sending Integration

**Test** (`clientapi/routing/email_test.go`):
```go
func TestSendPasswordResetEmail(t *testing.T) {
    cfg := &config.ClientAPI{
        SMTP: config.SMTP{
            Host:     "smtp.example.com",
            Port:     587,
            From:     "noreply@example.com",
            Username: "smtp_user",
            Password: "smtp_pass",
        },
    }

    // Use mock SMTP server
    mockServer := smtpmock.New(t)
    defer mockServer.Close()

    cfg.SMTP.Host = mockServer.Addr()

    err := sendPasswordResetEmail("user@example.com", "https://example.com/reset?token=abc123", cfg)
    assert.NoError(t, err)

    assert.Equal(t, 1, len(mockServer.ReceivedMessages))
    msg := mockServer.ReceivedMessages[0]
    assert.Contains(t, msg.Body, "reset your password")
    assert.Contains(t, msg.Body, "https://example.com/reset?token=abc123")
}
```

**Implementation** (`clientapi/routing/email.go`):
```go
import "net/smtp"

func sendPasswordResetEmail(to, resetLink string, cfg *config.ClientAPI) error {
    subject := "Reset your Matrix password"
    body := fmt.Sprintf(`
Hello,

You requested to reset your password. Click the link below to continue:

%s

This link will expire in 1 hour.

If you did not request this, please ignore this email.
`, resetLink)

    msg := []byte(fmt.Sprintf("To: %s\r\nSubject: %s\r\n\r\n%s", to, subject, body))

    auth := smtp.PlainAuth("", cfg.SMTP.Username, cfg.SMTP.Password, cfg.SMTP.Host)

    addr := fmt.Sprintf("%s:%d", cfg.SMTP.Host, cfg.SMTP.Port)
    return smtp.SendMail(addr, auth, cfg.SMTP.From, []string{to}, msg)
}
```

### Acceptance Criteria

- ✅ Users can request password reset via email
- ✅ Reset tokens are time-limited (1 hour) and one-time use
- ✅ Password reset invalidates all existing sessions
- ✅ Email validation and proper error handling
- ✅ Privacy-preserving (doesn't leak user existence)
- ✅ ≥80% test coverage
- ✅ SMTP config in `dendrite-sample.yaml`
- ✅ Rate limiting on request token endpoint

---

## Task #8: URL Previews (M - 1-2 weeks)

**Priority**: P2
**Files**: `mediaapi/routing/`, `setup/config/`

### Current State

No URL preview functionality exists. Clients cannot display link previews.

### Required Endpoint

`GET /_matrix/media/v3/preview_url?url=<url>&ts=<timestamp>`

### TDD Implementation Cycles

#### Cycle 1: URL Preview Config and Validation

**Test** (`setup/config/config_mediaapi_test.go`):
```go
func TestURLPreviewConfig(t *testing.T) {
    yaml := `
media_api:
  url_previews:
    enabled: true
    max_page_size: 10485760  # 10MB
    allowed_domains: ["trusted.com", "*.example.org"]
    blocked_domains: ["spam.com"]
    user_agent: "Dendrite/1.0"
`
    cfg, err := loadConfig(yaml)
    require.NoError(t, err)

    assert.True(t, cfg.MediaAPI.URLPreviews.Enabled)
    assert.Equal(t, int64(10485760), cfg.MediaAPI.URLPreviews.MaxPageSize)
    assert.Contains(t, cfg.MediaAPI.URLPreviews.AllowedDomains, "trusted.com")
}
```

**Implementation** (`setup/config/config_mediaapi.go`):
```go
type URLPreviews struct {
    Enabled        bool     `yaml:"enabled"`
    MaxPageSize    int64    `yaml:"max_page_size"`
    AllowedDomains []string `yaml:"allowed_domains"`
    BlockedDomains []string `yaml:"blocked_domains"`
    UserAgent      string   `yaml:"user_agent"`
    CacheTTL       int64    `yaml:"cache_ttl_seconds"`
}

func (c *MediaAPI) Defaults(opts DefaultOpts) {
    c.URLPreviews.MaxPageSize = 10 * 1024 * 1024 // 10MB
    c.URLPreviews.UserAgent = "Dendrite"
    c.URLPreviews.CacheTTL = 3600 // 1 hour
}
```

#### Cycle 2: URL Fetching with Security Checks

**Test** (`mediaapi/routing/preview_test.go`):
```go
func TestURLPreviewSecurityChecks(t *testing.T) {
    cfg := &config.URLPreviews{
        Enabled:        true,
        MaxPageSize:    1024 * 1024,
        BlockedDomains: []string{"localhost", "127.0.0.1", "*.internal"},
    }

    tests := []struct {
        url         string
        shouldBlock bool
    }{
        {"https://example.com", false},
        {"http://localhost/admin", true},
        {"https://127.0.0.1:8080", true},
        {"http://192.168.1.1", true},  // Private IP
        {"https://169.254.169.254", true},  // Link-local (AWS metadata)
        {"file:///etc/passwd", true},  // File URLs
    }

    for _, tt := range tests {
        t.Run(tt.url, func(t *testing.T) {
            err := validateURL(tt.url, cfg)
            if tt.shouldBlock {
                assert.Error(t, err, "Should block: "+tt.url)
            } else {
                assert.NoError(t, err, "Should allow: "+tt.url)
            }
        })
    }
}
```

**Implementation** (`mediaapi/routing/preview.go`):
```go
import (
    "net"
    "net/url"
)

func validateURL(urlStr string, cfg *config.URLPreviews) error {
    u, err := url.Parse(urlStr)
    if err != nil {
        return fmt.Errorf("invalid URL: %w", err)
    }

    // Only allow HTTP(S)
    if u.Scheme != "http" && u.Scheme != "https" {
        return fmt.Errorf("unsupported scheme: %s", u.Scheme)
    }

    // Resolve hostname to IP
    ips, err := net.LookupIP(u.Hostname())
    if err != nil {
        return fmt.Errorf("DNS lookup failed: %w", err)
    }

    // Block private IPs (SSRF protection)
    for _, ip := range ips {
        if isPrivateIP(ip) {
            return fmt.Errorf("private IP blocked: %s", ip)
        }
    }

    // Check blocked domains
    for _, blocked := range cfg.BlockedDomains {
        if matchesDomain(u.Hostname(), blocked) {
            return fmt.Errorf("domain blocked: %s", u.Hostname())
        }
    }

    return nil
}

func isPrivateIP(ip net.IP) bool {
    // RFC 1918 private networks
    privateRanges := []string{
        "10.0.0.0/8",
        "172.16.0.0/12",
        "192.168.0.0/16",
        "127.0.0.0/8",     // Loopback
        "169.254.0.0/16",  // Link-local
        "::1/128",         // IPv6 loopback
        "fc00::/7",        // IPv6 private
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

#### Cycle 3: HTML Parsing and Metadata Extraction

**Test** (`mediaapi/routing/preview_test.go`):
```go
func TestExtractPreviewMetadata(t *testing.T) {
    html := `
<!DOCTYPE html>
<html>
<head>
    <title>Example Page Title</title>
    <meta property="og:title" content="Open Graph Title">
    <meta property="og:description" content="This is a description">
    <meta property="og:image" content="https://example.com/image.jpg">
    <meta name="description" content="Fallback description">
</head>
<body>
    <p>Page content</p>
</body>
</html>
`

    metadata, err := extractMetadata(strings.NewReader(html), "https://example.com")
    assert.NoError(t, err)

    assert.Equal(t, "Open Graph Title", metadata.Title)
    assert.Equal(t, "This is a description", metadata.Description)
    assert.Equal(t, "https://example.com/image.jpg", metadata.ImageURL)
}
```

**Implementation** (`mediaapi/routing/metadata.go`):
```go
import "golang.org/x/net/html"

type PreviewMetadata struct {
    Title       string `json:"og:title"`
    Description string `json:"og:description"`
    ImageURL    string `json:"og:image"`
    SiteName    string `json:"og:site_name"`
}

func extractMetadata(r io.Reader, baseURL string) (*PreviewMetadata, error) {
    doc, err := html.Parse(r)
    if err != nil {
        return nil, err
    }

    metadata := &PreviewMetadata{}

    var traverse func(*html.Node)
    traverse = func(n *html.Node) {
        if n.Type == html.ElementNode {
            switch n.Data {
            case "title":
                if n.FirstChild != nil && metadata.Title == "" {
                    metadata.Title = n.FirstChild.Data
                }
            case "meta":
                property := getAttr(n, "property")
                name := getAttr(n, "name")
                content := getAttr(n, "content")

                switch {
                case property == "og:title":
                    metadata.Title = content
                case property == "og:description":
                    metadata.Description = content
                case property == "og:image":
                    metadata.ImageURL = resolveURL(baseURL, content)
                case property == "og:site_name":
                    metadata.SiteName = content
                case name == "description" && metadata.Description == "":
                    metadata.Description = content
                }
            }
        }

        for c := n.FirstChild; c != nil; c = c.NextSibling {
            traverse(c)
        }
    }

    traverse(doc)
    return metadata, nil
}

func resolveURL(base, ref string) string {
    baseURL, _ := url.Parse(base)
    refURL, _ := url.Parse(ref)
    return baseURL.ResolveReference(refURL).String()
}
```

#### Cycle 4: Caching and Rate Limiting

**Test** (`mediaapi/routing/preview_test.go`):
```go
func TestURLPreviewCaching(t *testing.T) {
    cache := NewPreviewCache(time.Hour)

    metadata := &PreviewMetadata{Title: "Test", Description: "Test desc"}

    // Store in cache
    cache.Set("https://example.com", metadata)

    // Retrieve from cache
    cached, found := cache.Get("https://example.com")
    assert.True(t, found)
    assert.Equal(t, metadata.Title, cached.Title)

    // Expired cache
    cache2 := NewPreviewCache(1 * time.Millisecond)
    cache2.Set("https://example.com", metadata)
    time.Sleep(10 * time.Millisecond)

    _, found = cache2.Get("https://example.com")
    assert.False(t, found, "Should expire")
}
```

**Implementation** (`mediaapi/routing/preview_cache.go`):
```go
type previewCacheEntry struct {
    metadata  *PreviewMetadata
    expiresAt time.Time
}

type PreviewCache struct {
    entries map[string]*previewCacheEntry
    mu      sync.RWMutex
    ttl     time.Duration
}

func NewPreviewCache(ttl time.Duration) *PreviewCache {
    cache := &PreviewCache{
        entries: make(map[string]*previewCacheEntry),
        ttl:     ttl,
    }

    // Start cleanup goroutine
    go cache.cleanup()

    return cache
}

func (c *PreviewCache) Get(url string) (*PreviewMetadata, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()

    entry, found := c.entries[url]
    if !found || time.Now().After(entry.expiresAt) {
        return nil, false
    }

    return entry.metadata, true
}

func (c *PreviewCache) Set(url string, metadata *PreviewMetadata) {
    c.mu.Lock()
    defer c.mu.Unlock()

    c.entries[url] = &previewCacheEntry{
        metadata:  metadata,
        expiresAt: time.Now().Add(c.ttl),
    }
}

func (c *PreviewCache) cleanup() {
    ticker := time.NewTicker(10 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        c.mu.Lock()
        now := time.Now()
        for url, entry := range c.entries {
            if now.After(entry.expiresAt) {
                delete(c.entries, url)
            }
        }
        c.mu.Unlock()
    }
}
```

#### Cycle 5: Main Preview Endpoint

**Test** (`mediaapi/routing/preview_test.go`):
```go
func TestPreviewURLEndpoint(t *testing.T) {
    // Setup mock HTTP server
    mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        html := `<html><head><title>Mock Page</title></head><body></body></html>`
        w.Write([]byte(html))
    }))
    defer mockServer.Close()

    cfg := &config.MediaAPI{
        URLPreviews: config.URLPreviews{
            Enabled:     true,
            MaxPageSize: 1024 * 1024,
        },
    }

    req := httptest.NewRequest("GET", "/_matrix/media/v3/preview_url?url="+mockServer.URL, nil)
    w := httptest.NewRecorder()

    PreviewURL(w, req, cfg)

    assert.Equal(t, http.StatusOK, w.Code)

    var res PreviewMetadata
    json.Unmarshal(w.Body.Bytes(), &res)

    assert.Equal(t, "Mock Page", res.Title)
}
```

**Implementation** (`mediaapi/routing/preview.go`):
```go
func PreviewURL(
    w http.ResponseWriter, req *http.Request,
    cfg *config.MediaAPI,
) util.JSONResponse {
    if !cfg.URLPreviews.Enabled {
        return util.JSONResponse{Code: http.StatusForbidden, JSON: spec.Forbidden("URL previews disabled")}
    }

    targetURL := req.URL.Query().Get("url")
    if targetURL == "" {
        return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.MissingParam("url")}
    }

    // Security validation
    if err := validateURL(targetURL, &cfg.URLPreviews); err != nil {
        return util.JSONResponse{Code: http.StatusForbidden, JSON: spec.Forbidden(err.Error())}
    }

    // Check cache
    if metadata, found := previewCache.Get(targetURL); found {
        return util.JSONResponse{Code: http.StatusOK, JSON: metadata}
    }

    // Fetch URL
    client := &http.Client{
        Timeout: 10 * time.Second,
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
            if len(via) >= 5 {
                return fmt.Errorf("too many redirects")
            }
            return validateURL(req.URL.String(), &cfg.URLPreviews)
        },
    }

    httpReq, _ := http.NewRequest("GET", targetURL, nil)
    httpReq.Header.Set("User-Agent", cfg.URLPreviews.UserAgent)

    resp, err := client.Do(httpReq)
    if err != nil {
        return util.ErrorResponse(err)
    }
    defer resp.Body.Close()

    // Limit response size
    limitedReader := io.LimitReader(resp.Body, cfg.URLPreviews.MaxPageSize)

    // Extract metadata
    metadata, err := extractMetadata(limitedReader, targetURL)
    if err != nil {
        return util.ErrorResponse(err)
    }

    // Cache result
    previewCache.Set(targetURL, metadata)

    return util.JSONResponse{Code: http.StatusOK, JSON: metadata}
}
```

### Acceptance Criteria

- ✅ URL preview endpoint implemented and working
- ✅ SSRF protection (blocks private IPs, localhost)
- ✅ Domain allow/block lists
- ✅ HTML metadata extraction (Open Graph, meta tags)
- ✅ Response size limits
- ✅ Caching with configurable TTL
- ✅ Rate limiting per user
- ✅ ≥80% test coverage
- ✅ Security review for SSRF vulnerabilities
- ✅ Config in `dendrite-sample.yaml`

---

## Task #9: 3PID Email Verification (M - 1-2 weeks)

**Priority**: P2
**Files**: `clientapi/routing/`, `userapi/storage/`

### Required Endpoints

1. `POST /_matrix/client/v3/account/3pid/email/requestToken`
2. `POST /_matrix/client/v3/account/3pid`
3. `GET /_matrix/client/v3/account/3pid`
4. `DELETE /_matrix/client/v3/account/3pid`

### TDD Implementation Cycles

#### Cycle 1: 3PID Storage Schema

**Migration** (`userapi/storage/postgres/deltas/20250122_threepid_associations.sql`):
```sql
CREATE TABLE IF NOT EXISTS userapi_threepid_associations (
    user_id TEXT NOT NULL,
    medium TEXT NOT NULL,  -- 'email' or 'msisdn'
    address TEXT NOT NULL,
    validated_at BIGINT NOT NULL,
    added_at BIGINT NOT NULL DEFAULT extract(epoch from now()) * 1000,
    PRIMARY KEY (user_id, medium, address)
);

CREATE INDEX idx_threepid_address ON userapi_threepid_associations(medium, address);
```

**Test** (`userapi/storage/postgres/threepid_test.go`):
```go
func TestThreePIDStorage(t *testing.T) {
    db := mustCreateDatabase(t)

    // Add 3PID
    err := db.SaveThreePIDAssociation("@alice:example.com", "email", "alice@example.com", time.Now())
    assert.NoError(t, err)

    // Query 3PIDs for user
    threepids, err := db.GetThreePIDsForUser("@alice:example.com")
    assert.NoError(t, err)
    assert.Len(t, threepids, 1)
    assert.Equal(t, "email", threepids[0].Medium)
    assert.Equal(t, "alice@example.com", threepids[0].Address)

    // Remove 3PID
    err = db.RemoveThreePIDAssociation("@alice:example.com", "email", "alice@example.com")
    assert.NoError(t, err)

    threepids, err = db.GetThreePIDsForUser("@alice:example.com")
    assert.NoError(t, err)
    assert.Len(t, threepids, 0)
}
```

#### Cycle 2: Email Verification Token Flow

(Similar to password reset token flow - reuse pattern from Task #5)

**Test** (`clientapi/routing/threepid_test.go`):
```go
func TestEmailVerificationRequestToken(t *testing.T) {
    // Request verification email
    reqBody := map[string]interface{}{
        "client_secret": "secret123",
        "email":         "alice@example.com",
        "send_attempt":  1,
    }

    req := httptest.NewRequest("POST", "/_matrix/client/v3/account/3pid/email/requestToken", jsonBody(reqBody))
    w := httptest.NewRecorder()

    RequestEmailVerificationToken(w, req, userAPI, cfg)

    assert.Equal(t, http.StatusOK, w.Code)

    var res map[string]string
    json.Unmarshal(w.Body.Bytes(), &res)
    assert.Contains(t, res, "sid")

    // Verify email sent
    assert.Equal(t, 1, len(mockEmailSender.SentEmails))
}
```

#### Cycle 3: Add 3PID Endpoint

**Test** (`clientapi/routing/threepid_test.go`):
```go
func TestAdd3PID(t *testing.T) {
    // Get access token
    accessToken := loginAsAlice(t)

    // Add 3PID
    reqBody := map[string]interface{}{
        "three_pid_creds": map[string]interface{}{
            "sid":           "session_id",
            "client_secret": "secret123",
        },
    }

    req := httptest.NewRequest("POST", "/_matrix/client/v3/account/3pid", jsonBody(reqBody))
    req.Header.Set("Authorization", "Bearer "+accessToken)
    w := httptest.NewRecorder()

    Add3PID(w, req, userAPI, cfg)

    assert.Equal(t, http.StatusOK, w.Code)

    // Verify 3PID was added
    threepids := getThreePIDsForUser("@alice:example.com")
    assert.Len(t, threepids, 1)
    assert.Equal(t, "alice@example.com", threepids[0].Address)
}
```

**Implementation** (`clientapi/routing/threepid.go`):
```go
func Add3PID(
    w http.ResponseWriter, req *http.Request,
    userAPI userapi.ClientUserAPI,
    device *userapi.Device,
) util.JSONResponse {
    var r struct {
        ThreePIDCreds struct {
            SID          string `json:"sid"`
            ClientSecret string `json:"client_secret"`
        } `json:"three_pid_creds"`
    }

    if err := json.NewDecoder(req.Body).Decode(&r); err != nil {
        return util.JSONResponse{Code: http.StatusBadRequest}
    }

    // Validate session (check token was verified)
    email, validated, err := userAPI.GetEmailVerificationSession(req.Context(), r.ThreePIDCreds.SID, r.ThreePIDCreds.ClientSecret)
    if err != nil || !validated {
        return util.JSONResponse{Code: http.StatusBadRequest, JSON: spec.InvalidParam("Invalid session")}
    }

    // Add 3PID association
    err = userAPI.SaveThreePIDAssociation(req.Context(), device.UserID, "email", email, time.Now())
    if err != nil {
        return util.ErrorResponse(err)
    }

    return util.JSONResponse{Code: http.StatusOK, JSON: struct{}{}}
}
```

#### Cycle 4: List and Delete 3PIDs

**Test** (`clientapi/routing/threepid_test.go`):
```go
func TestGet3PIDs(t *testing.T) {
    accessToken := loginAsAlice(t)

    // Add some 3PIDs first
    userAPI.SaveThreePIDAssociation(context.Background(), "@alice:example.com", "email", "alice@example.com", time.Now())

    req := httptest.NewRequest("GET", "/_matrix/client/v3/account/3pid", nil)
    req.Header.Set("Authorization", "Bearer "+accessToken)
    w := httptest.NewRecorder()

    Get3PIDs(w, req, userAPI, device)

    assert.Equal(t, http.StatusOK, w.Code)

    var res struct {
        ThreePIDs []struct {
            Medium      string `json:"medium"`
            Address     string `json:"address"`
            ValidatedAt int64  `json:"validated_at"`
            AddedAt     int64  `json:"added_at"`
        } `json:"threepids"`
    }
    json.Unmarshal(w.Body.Bytes(), &res)

    assert.Len(t, res.ThreePIDs, 1)
    assert.Equal(t, "email", res.ThreePIDs[0].Medium)
}

func TestDelete3PID(t *testing.T) {
    accessToken := loginAsAlice(t)

    reqBody := map[string]interface{}{
        "medium":  "email",
        "address": "alice@example.com",
    }

    req := httptest.NewRequest("DELETE", "/_matrix/client/v3/account/3pid", jsonBody(reqBody))
    req.Header.Set("Authorization", "Bearer "+accessToken)
    w := httptest.NewRecorder()

    Delete3PID(w, req, userAPI, device)

    assert.Equal(t, http.StatusOK, w.Code)

    // Verify deleted
    threepids := getThreePIDsForUser("@alice:example.com")
    assert.Len(t, threepids, 0)
}
```

### Acceptance Criteria

- ✅ Users can request email verification
- ✅ Email verification tokens are time-limited and one-time use
- ✅ Users can add verified email to account
- ✅ Users can list their 3PIDs
- ✅ Users can remove 3PIDs
- ✅ Duplicate prevention (can't add same email twice)
- ✅ Integration with password reset (Task #5)
- ✅ ≥80% test coverage
- ✅ Rate limiting on verification requests

---

## Task #10: Thread Notification Counts (M - 1-2 weeks)

**Priority**: P1
**Files**: `syncapi/sync/`, `syncapi/storage/`, `roomserver/`

### Current State

MSC3440 (Threads) has partial support - event relations exist but thread-aware sync is missing.

### Missing Features

1. `m.thread` relation aggregation
2. Thread notification counts in `/sync`
3. Thread-specific unread indicators
4. Push rule integration for threads

### TDD Implementation Cycles

#### Cycle 1: Thread Relation Aggregation

**Test** (`roomserver/storage/postgres/relations_test.go`):
```go
func TestThreadRelationAggregation(t *testing.T) {
    db := mustCreateDatabase(t)

    // Root event
    rootEvent := createEvent(t, "m.room.message", "@alice:example.com", map[string]interface{}{
        "body": "Root message",
    })

    // Thread reply 1
    reply1 := createEvent(t, "m.room.message", "@bob:example.com", map[string]interface{}{
        "body": "Reply 1",
        "m.relates_to": map[string]interface{}{
            "rel_type":  "m.thread",
            "event_id":  rootEvent.EventID(),
        },
    })

    // Thread reply 2
    reply2 := createEvent(t, "m.room.message", "@charlie:example.com", map[string]interface{}{
        "body": "Reply 2",
        "m.relates_to": map[string]interface{}{
            "rel_type":  "m.thread",
            "event_id":  rootEvent.EventID(),
        },
    })

    // Store events
    db.StoreEvent(context.Background(), rootEvent)
    db.StoreEvent(context.Background(), reply1)
    db.StoreEvent(context.Background(), reply2)

    // Get thread summary
    summary, err := db.GetThreadSummary(context.Background(), rootEvent.EventID())
    assert.NoError(t, err)

    assert.Equal(t, 2, summary.Count)
    assert.Equal(t, reply2.EventID(), summary.LatestEventID)
    assert.ElementsMatch(t, []string{"@bob:example.com", "@charlie:example.com"}, summary.Participants)
}
```

**Implementation** (`roomserver/storage/postgres/relations_table.go`):
```go
type ThreadSummary struct {
    RootEventID    string
    Count          int
    LatestEventID  string
    Participants   []string
}

const selectThreadSummarySQL = `
    SELECT
        COUNT(*) as count,
        MAX(event_nid) as latest_event_nid,
        array_agg(DISTINCT sender_nid) as participant_nids
    FROM roomserver_events
    WHERE relates_to_event_id = $1 AND relation_type = 'm.thread'
`

func (s *relationsStatements) SelectThreadSummary(
    ctx context.Context, txn *sql.Tx, rootEventID string,
) (*ThreadSummary, error) {
    stmt := sqlutil.TxStmt(txn, s.selectThreadSummaryStmt)

    var count int
    var latestEventNID int64
    var participantNIDs pq.Int64Array

    err := stmt.QueryRowContext(ctx, rootEventID).Scan(&count, &latestEventNID, &participantNIDs)
    if err != nil {
        return nil, err
    }

    // Convert NIDs to user IDs
    participants, err := s.getUserIDsFromNIDs(ctx, txn, participantNIDs)
    if err != nil {
        return nil, err
    }

    latestEventID, err := s.getEventIDFromNID(ctx, txn, latestEventNID)
    if err != nil {
        return nil, err
    }

    return &ThreadSummary{
        RootEventID:   rootEventID,
        Count:         count,
        LatestEventID: latestEventID,
        Participants:  participants,
    }, nil
}
```

#### Cycle 2: Thread Notification Counts in Sync

**Test** (`syncapi/sync/requestpool_test.go`):
```go
func TestSyncThreadNotificationCounts(t *testing.T) {
    alice := "@alice:example.com"
    room := "!room:example.com"

    // Create room with thread
    rootEvent := sendMessage(t, alice, room, "Root message")

    // Bob replies in thread (should notify Alice)
    replyEvent := sendMessage(t, "@bob:example.com", room, "Reply", map[string]interface{}{
        "m.relates_to": map[string]interface{}{
            "rel_type": "m.thread",
            "event_id": rootEvent.EventID(),
        },
    })

    // Alice syncs
    syncReq := &syncRequest{userID: alice}
    syncRes := &syncResponse{}

    pool.OnIncomingSyncRequest(syncReq, syncRes)

    // Check thread notification count
    joinedRoom := syncRes.Rooms.Join[room]
    assert.Equal(t, 1, joinedRoom.UnreadThreadNotifications[rootEvent.EventID()].NotificationCount)
    assert.Equal(t, 0, joinedRoom.UnreadThreadNotifications[rootEvent.EventID()].HighlightCount)
}
```

**Implementation** (`syncapi/sync/notifier.go`):
```go
type ThreadNotificationCounts struct {
    NotificationCount int `json:"notification_count"`
    HighlightCount    int `json:"highlight_count"`
}

type JoinResponse struct {
    // ... existing fields ...
    UnreadThreadNotifications map[string]ThreadNotificationCounts `json:"org.matrix.msc3773.unread_thread_notifications"`
}

func (n *Notifier) calculateThreadNotifications(
    ctx context.Context,
    userID, roomID string,
    since types.StreamPosition,
) (map[string]ThreadNotificationCounts, error) {
    // Get thread events since last sync
    threadEvents, err := n.db.GetThreadEventsSince(ctx, roomID, since)
    if err != nil {
        return nil, err
    }

    counts := make(map[string]ThreadNotificationCounts)

    for _, event := range threadEvents {
        // Skip user's own messages
        if event.Sender() == userID {
            continue
        }

        // Get root event ID from relation
        relatesTo := event.Content()["m.relates_to"].(map[string]interface{})
        rootEventID := relatesTo["event_id"].(string)

        // Check push rules
        action := n.evaluatePushRules(ctx, userID, event)

        threadCounts := counts[rootEventID]

        if action.Notify {
            threadCounts.NotificationCount++
        }
        if action.Highlight {
            threadCounts.HighlightCount++
        }

        counts[rootEventID] = threadCounts
    }

    return counts, nil
}
```

#### Cycle 3: Push Rules for Threads

**Test** (`userapi/pushnotifications/pushnotifications_test.go`):
```go
func TestThreadPushRules(t *testing.T) {
    evaluator := NewPushRuleEvaluator(...)

    // Thread reply that mentions user
    event := createEvent(t, "m.room.message", "@bob:example.com", map[string]interface{}{
        "body": "Hey @alice, check this out",
        "m.relates_to": map[string]interface{}{
            "rel_type": "m.thread",
            "event_id": "$root_event",
            "is_falling_back": true,
        },
    })

    actions := evaluator.Evaluate(event, "@alice:example.com", 10)

    assert.True(t, actions.Notify)
    assert.True(t, actions.Highlight) // Mention should highlight
}
```

**Implementation** (`userapi/pushnotifications/evaluator.go`):
```go
func (e *PushRuleEvaluator) Evaluate(event *gomatrixserverlib.Event, userID string, memberCount int) *PushActions {
    // Check if event is in thread
    relatesTo, _ := event.Content()["m.relates_to"].(map[string]interface{})
    isThread := relatesTo["rel_type"] == "m.thread"

    for _, rule := range e.getRules(userID) {
        // Thread-specific rule matching
        if isThread {
            // Check "is_user_mention" condition (MSC3952)
            if e.hasUserMention(event, userID) {
                return &PushActions{Notify: true, Highlight: true}
            }

            // Check "is_room_mention" condition
            if e.hasRoomMention(event) {
                return &PushActions{Notify: true, Highlight: false}
            }
        }

        // ... existing rule evaluation ...
    }

    return &PushActions{Notify: false, Highlight: false}
}

func (e *PushRuleEvaluator) hasUserMention(event *gomatrixserverlib.Event, userID string) bool {
    mentions, ok := event.Content()["m.mentions"].(map[string]interface{})
    if !ok {
        return false
    }

    userMentions, ok := mentions["user_ids"].([]interface{})
    if !ok {
        return false
    }

    for _, mentioned := range userMentions {
        if mentioned == userID {
            return true
        }
    }

    return false
}
```

#### Cycle 4: Read Receipts for Threads

**Test** (`syncapi/sync/receipt_test.go`):
```go
func TestThreadReadReceipts(t *testing.T) {
    alice := "@alice:example.com"
    room := "!room:example.com"

    rootEvent := sendMessage(t, alice, room, "Root")
    replyEvent := sendMessage(t, "@bob:example.com", room, "Reply", threadRelation(rootEvent))

    // Alice sends read receipt for thread
    err := sendReadReceipt(alice, room, replyEvent.EventID(), map[string]interface{}{
        "thread_id": rootEvent.EventID(),
    })
    assert.NoError(t, err)

    // Check notification count is cleared for that thread
    syncRes := performSync(alice)
    joinedRoom := syncRes.Rooms.Join[room]

    threadCounts, exists := joinedRoom.UnreadThreadNotifications[rootEvent.EventID()]
    assert.False(t, exists || threadCounts.NotificationCount > 0)
}
```

**Implementation** (`syncapi/sync/receipts.go`):
```go
func (s *ReceiptDatabase) UpdateReadReceipt(
    ctx context.Context,
    roomID, userID, eventID string,
    threadID *string,
) error {
    if threadID != nil {
        // Thread-specific receipt
        return s.updateThreadReadReceipt(ctx, roomID, userID, eventID, *threadID)
    }

    // Main timeline receipt
    return s.updateMainReadReceipt(ctx, roomID, userID, eventID)
}

func (s *ReceiptDatabase) updateThreadReadReceipt(
    ctx context.Context,
    roomID, userID, eventID, threadID string,
) error {
    // Store separate receipt for thread
    _, err := s.db.ExecContext(ctx, `
        INSERT INTO syncapi_thread_receipts (room_id, user_id, thread_id, event_id, ts)
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (room_id, user_id, thread_id)
        DO UPDATE SET event_id = $4, ts = $5
    `, roomID, userID, threadID, eventID, time.Now().UnixMilli())

    return err
}
```

### Acceptance Criteria

- ✅ Thread relations aggregated correctly
- ✅ Thread notification counts in `/sync` response
- ✅ Separate unread counts per thread
- ✅ Push rules evaluate thread mentions (MSC3952)
- ✅ Thread-specific read receipts
- ✅ Backwards compatible with clients not supporting threads
- ✅ ≥80% test coverage
- ✅ Performance testing with large threads (>1000 messages)

---

## Testing Strategy

### Unit Tests
- Write tests FIRST for each cycle (TDD)
- Achieve ≥80% coverage for all new code
- Use table-driven tests where appropriate

### Integration Tests
- Test endpoint interactions end-to-end
- Use `httptest` for HTTP handlers
- Mock external dependencies (SMTP, HTTP fetches)

### Race Detection
```bash
go test -race ./...
```

### Coverage Reports
```bash
go test -coverprofile=coverage.out ./clientapi/routing
go tool cover -html=coverage.out
```

---

## Coordination with Engineer A

**No blocking dependencies** - all Engineer B tasks can proceed immediately while Engineer A works on Phase 0 and admin endpoints.

**Optional collaboration points**:
- Week 3-4: Share rate limiting implementation (Task #3 → Phase 0 admin routes can reuse)
- Week 5: Share metrics patterns (Task #4 → Engineer A can add admin metrics)
- Week 6: Code review swap for fresh perspective

---

## Branch and PR Strategy

Create separate branches for each task:
```bash
git checkout -b feature/rate-limiting-config
git checkout -b feature/prometheus-metrics
git checkout -b feature/password-reset
git checkout -b feature/url-previews
git checkout -b feature/3pid-email
git checkout -b feature/thread-notifications
```

**PR Titles**:
- `[Quick Win] Enhanced Rate Limiting Configuration`
- `[Quick Win] Prometheus Metrics Expansion`
- `[Quick Win] Password Reset Flow`
- `[Feature] URL Previews with SSRF Protection`
- `[Feature] 3PID Email Verification`
- `[Feature] Thread Notification Counts (MSC3440)`

---

## Final Checklist

Before marking each task complete:

- [ ] All TDD cycles completed (Red → Green → Refactor)
- [ ] ≥80% test coverage verified
- [ ] Race detector clean (`go test -race`)
- [ ] Linter clean (`golangci-lint run`)
- [ ] Manual testing performed
- [ ] Documentation updated (config, API docs)
- [ ] PR created with description and test plan
- [ ] No regressions in existing tests

---

**Estimated Total Time**: 8-10 weeks (can overlap with Engineer A's 6-week track)

**End Result**: 6 production-ready features enhancing Dendrite's operability, security, and Matrix 2.0 compliance.
