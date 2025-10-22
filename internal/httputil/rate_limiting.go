package httputil

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/element-hq/dendrite/setup/config"
	userapi "github.com/element-hq/dendrite/userapi/api"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/matrix-org/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

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

var registerRateLimiterMetrics sync.Once

func init() {
	registerRateLimiterMetrics.Do(func() {
		prometheus.MustRegister(rateLimitRejections, rateLimitAllowed)
	})
}

type limiterConfig struct {
	threshold int64
	cooloff   time.Duration
}

type limiterEntry struct {
	limiter  *rate.Limiter
	config   limiterConfig
	lastSeen time.Time
}

type RateLimits struct {
	limits        map[string]*limiterEntry
	mutex         sync.RWMutex
	enabled       bool
	defaultConfig limiterConfig
	perEndpoint   map[string]limiterConfig
	exemptUserIDs map[string]struct{}
	exemptIPs     []net.IP
	exemptCIDRs   []*net.IPNet
	cleanupDone   chan struct{} // Signal channel to stop cleanup goroutine
}

func NewRateLimits(cfg *config.RateLimiting) *RateLimits {
	l := &RateLimits{
		limits:      make(map[string]*limiterEntry),
		enabled:     cfg.Enabled,
		cleanupDone: make(chan struct{}),
		defaultConfig: limiterConfig{
			threshold: cfg.Threshold,
			cooloff:   time.Duration(cfg.CooloffMS) * time.Millisecond,
		},
		perEndpoint:   make(map[string]limiterConfig),
		exemptUserIDs: map[string]struct{}{},
	}
	for _, userID := range cfg.ExemptUserIDs {
		l.exemptUserIDs[userID] = struct{}{}
	}
	for endpoint, override := range cfg.PerEndpointOverrides {
		l.perEndpoint[endpoint] = limiterConfig{
			threshold: override.Threshold,
			cooloff:   time.Duration(override.CooloffMS) * time.Millisecond,
		}
	}
	for _, ip := range cfg.ExemptIPAddresses {
		if parsedIP := net.ParseIP(ip); parsedIP != nil {
			l.exemptIPs = append(l.exemptIPs, parsedIP)
			continue
		}
		if _, network, err := net.ParseCIDR(ip); err == nil {
			l.exemptCIDRs = append(l.exemptCIDRs, network)
		}
	}
	if l.enabled {
		go l.clean()
	}
	return l
}

// clean runs periodically to remove expired rate limiter entries and prevent memory leaks.
// It uses a snapshot-based approach to minimize lock contention under high load:
// 1. Briefly acquire read lock to snapshot keys to check
// 2. Release lock to avoid blocking concurrent requests
// 3. Take individual write locks to delete expired entries
// This prevents cleaner starvation when thousands of concurrent requests hold read locks.
// The goroutine can be stopped by calling Stop() which closes the cleanupDone channel.
func (l *RateLimits) clean() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-l.cleanupDone:
			// Stop signal received, exit cleanup goroutine
			return
		case <-ticker.C:
			// Perform cleanup
			cutoff := time.Now().Add(-time.Minute)

			// Phase 1: Snapshot keys to check (brief read lock)
			l.mutex.RLock()
			keysToCheck := make([]string, 0, len(l.limits))
			for key := range l.limits {
				keysToCheck = append(keysToCheck, key)
			}
			l.mutex.RUnlock()

			// Phase 2: Check and delete expired entries (individual write locks)
			for _, key := range keysToCheck {
				l.mutex.Lock()
				entry, exists := l.limits[key]
				if exists && entry.lastSeen.Before(cutoff) {
					delete(l.limits, key)
				}
				l.mutex.Unlock()
			}
		}
	}
}

// Stop gracefully stops the cleanup goroutine. Safe to call multiple times.
// This should be called when the rate limiter is no longer needed to prevent
// goroutine leaks, especially in tests or when creating multiple instances.
func (l *RateLimits) Stop() {
	if l.enabled && l.cleanupDone != nil {
		select {
		case <-l.cleanupDone:
			// Already closed, do nothing
		default:
			close(l.cleanupDone)
		}
	}
}

func (l *RateLimits) Limit(req *http.Request, device *userapi.Device) *util.JSONResponse {
	endpoint := endpointLabel(req)

	// If rate limiting is disabled then do nothing.
	if !l.enabled {
		rateLimitAllowed.WithLabelValues(endpoint).Inc()
		return nil
	}

	// Determine caller identity and request IP.
	var caller string
	var requestIPAddr net.IP
	if ip, _ := requestIP(req); ip != nil {
		requestIPAddr = ip
		caller = ip.String()
	} else if req != nil {
		caller = req.RemoteAddr
	}
	if device != nil {
		switch device.AccountType {
		case userapi.AccountTypeAdmin:
			rateLimitAllowed.WithLabelValues(endpoint).Inc()
			return nil // don't rate-limit server administrators
		case userapi.AccountTypeAppService:
			rateLimitAllowed.WithLabelValues(endpoint).Inc()
			return nil // don't rate-limit appservice users
		default:
			if _, ok := l.exemptUserIDs[device.UserID]; ok {
				// If the user is exempt from rate limiting then do nothing.
				rateLimitAllowed.WithLabelValues(endpoint).Inc()
				return nil
			}
			caller = device.UserID + device.ID
		}
	}

	if l.isIPExemptIP(requestIPAddr) {
		rateLimitAllowed.WithLabelValues(endpoint).Inc()
		return nil
	}

	cfg := l.defaultConfig
	limiterKey := caller
	if req != nil {
		if override, ok := l.perEndpoint[req.URL.Path]; ok {
			cfg = override
			limiterKey = caller + "|" + req.URL.Path
		}
	}

	limiter, block := l.getLimiter(limiterKey, cfg)
	if block {
		rateLimitRejections.WithLabelValues(endpoint).Inc()
		return &util.JSONResponse{
			Code: http.StatusTooManyRequests,
			JSON: spec.LimitExceeded("You are sending too many requests too quickly!", cfg.cooloff.Milliseconds()),
		}
	}

	if limiter == nil {
		rateLimitAllowed.WithLabelValues(endpoint).Inc()
		return nil
	}

	if limiter.Allow() {
		rateLimitAllowed.WithLabelValues(endpoint).Inc()
		return nil
	}

	rateLimitRejections.WithLabelValues(endpoint).Inc()
	return &util.JSONResponse{
		Code: http.StatusTooManyRequests,
		JSON: spec.LimitExceeded("You are sending too many requests too quickly!", cfg.cooloff.Milliseconds()),
	}
}

// getLimiter retrieves or creates a rate limiter for the given key and config.
// It uses the token bucket algorithm from golang.org/x/time/rate with the following formula:
//
// Rate Calculation:
//   requestsPerSecond = threshold × (1 second / cooloff)
//
// Example: threshold=5, cooloff=500ms
//   → rate = 5 × (1000ms / 500ms) = 10 requests/second
//   → burst = 5 requests (allows short bursts up to threshold)
//
// Token Bucket Behavior:
//   - Tokens are added at 'requestsPerSecond' rate
//   - Bucket capacity (burst) = threshold
//   - Each request consumes 1 token
//   - Request blocked when bucket empty
//   - After cooloff period, bucket refills by threshold/cooloff tokens
//
// Returns:
//   - (*rate.Limiter, false) if rate limiting should be applied
//   - (nil, true) if request should be blocked immediately (threshold <= 0)
//   - (nil, false) if rate limiting is disabled for this config (cooloff <= 0)
func (l *RateLimits) getLimiter(key string, cfg limiterConfig) (*rate.Limiter, bool) {
	if cfg.threshold <= 0 {
		return nil, true
	}

	if cfg.cooloff <= 0 {
		return nil, false
	}

	burst := int(cfg.threshold)
	if burst < 1 {
		burst = 1
	}

	requestsPerSecond := rate.Limit(float64(cfg.threshold) * float64(time.Second) / float64(cfg.cooloff))
	if requestsPerSecond <= 0 {
		requestsPerSecond = rate.Limit(1)
	}

	l.mutex.Lock()
	defer l.mutex.Unlock()

	entry, ok := l.limits[key]
	if ok && entry.config == cfg {
		entry.lastSeen = time.Now()
		return entry.limiter, false
	}

	limiter := rate.NewLimiter(requestsPerSecond, burst)
	l.limits[key] = &limiterEntry{
		limiter:  limiter,
		config:   cfg,
		lastSeen: time.Now(),
	}

	return limiter, false
}

func endpointLabel(req *http.Request) string {
	if req == nil || req.URL == nil {
		return "unknown"
	}
	return req.URL.Path
}

// requestIP extracts the client IP address from the HTTP request.
//
// Security Model:
// X-Forwarded-For is ONLY trusted when req.RemoteAddr is a loopback address
// (127.0.0.1/::1), indicating the request came through a local reverse proxy
// like nginx, Caddy, or HAProxy running on the same machine.
//
// X-Forwarded-For Format: "client, proxy1, proxy2, ourProxy"
// - Left-most IP: Original client (can be spoofed by client)
// - Right-most IP: Last proxy that added to the header (most trustworthy)
//
// When Behind Reverse Proxy:
// Only trust X-Forwarded-For if RemoteAddr is loopback (127.0.0.1 or ::1).
// In this case, parse the FIRST valid non-private IP from left to right, as:
// - Our reverse proxy added the real client IP to the left
// - Any prior hops in the header came from outside our infrastructure
//
// Direct Connections:
// Use req.RemoteAddr directly when not proxied (RemoteAddr is not loopback).
// X-Forwarded-For is ignored to prevent client IP spoofing.
//
// Multi-Proxy Scenario:
// If you have multiple layers (e.g., CDN → reverse proxy → Dendrite), ensure:
// 1. Only the final reverse proxy (on localhost) talks to Dendrite
// 2. That proxy sets X-Forwarded-For with the client's real IP
// 3. Configure your proxy to override X-Forwarded-For, not append to it
//
// Returns:
//   (IP, true) if we trust the extracted IP (from X-Forwarded-For)
//   (IP, false) if using RemoteAddr directly (not behind trusted proxy)
//   (nil, false) if IP extraction fails
func requestIP(req *http.Request) (net.IP, bool) {
	if req == nil {
		return nil, false
	}

	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	host = strings.TrimSpace(host)
	remoteIP := net.ParseIP(host)
	if remoteIP == nil {
		return nil, false
	}

	// Only trust X-Forwarded-For if the direct connection is from loopback.
	// This indicates we're behind a local reverse proxy.
	forwardedFor := req.Header.Get("X-Forwarded-For")
	if forwardedFor != "" {
		if !remoteIP.IsLoopback() {
			// Log at debug level to help diagnose proxy misconfiguration or spoofing attempts.
			// This is not logged at higher levels to prevent spam in production logs.
			logrus.WithFields(logrus.Fields{
				"remote_addr":      remoteIP.String(),
				"x_forwarded_for":  forwardedFor,
				"request_path":     req.URL.Path,
			}).Debug("Ignoring X-Forwarded-For from non-loopback connection (potential IP spoofing or misconfigured proxy)")
			return remoteIP, false
		}

		// Parse IPs from left to right, taking the first valid public IP.
		// This assumes the local reverse proxy added the real client IP on the left.
		parts := strings.Split(forwardedFor, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if ip := net.ParseIP(part); ip != nil && !ip.IsLoopback() {
				// Found a valid, non-loopback IP. Use it.
				// Note: You may want to also filter out private IPs (RFC1918) depending on your setup.
				return ip, true
			}
		}
	}

	// Either no X-Forwarded-For header, or RemoteAddr is not loopback, or all IPs were invalid.
	// Use the direct connection IP.
	return remoteIP, false
}

func (l *RateLimits) isIPExemptIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, exemptIP := range l.exemptIPs {
		if exemptIP.Equal(ip) {
			return true
		}
	}
	for _, network := range l.exemptCIDRs {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
