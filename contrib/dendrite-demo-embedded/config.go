package embedded

import (
	"crypto/ed25519"
	"fmt"
	"net/http"
	"time"

	"github.com/element-hq/dendrite/setup/config"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/spec"
)

// ServerConfig contains configuration for the embedded server
type ServerConfig struct {
	// Basic server identity
	ServerName string
	KeyID      string
	PrivateKey ed25519.PrivateKey

	// Storage paths
	DatabasePath   string
	MediaStorePath string
	JetStreamPath  string

	// HTTP client configuration
	HTTPClient *http.Client

	// Feature flags
	DisableFederation bool
	EnableMetrics     bool
	MetricsUsername   string
	MetricsPassword   string

	// Cache configuration
	CacheMaxSize int64
	CacheMaxAge  time.Duration

	// Rate limiting
	RateLimitYAMLPath string

	// Custom config options
	RawDendriteConfig *config.Dendrite
}

// DefaultConfig returns a configuration with sensible defaults for an embedded server
func DefaultConfig() ServerConfig {
	return ServerConfig{
		ServerName:        "localhost",
		KeyID:             "ed25519:auto",
		DatabasePath:      "./dendrite.db",
		MediaStorePath:    "./media_store",
		JetStreamPath:     "./jetstream",
		DisableFederation: true,
		EnableMetrics:     false,
		CacheMaxSize:      64 * 1024 * 1024, // 64 MB
		CacheMaxAge:       time.Hour,
		HTTPClient:        http.DefaultClient,
	}
}

// toDendriteConfig converts the ServerConfig to a Dendrite config
func (c *ServerConfig) toDendriteConfig() (*config.Dendrite, error) {
	// If a raw config was provided, use that as the base
	if c.RawDendriteConfig != nil {
		return c.RawDendriteConfig, nil
	}

	// Create a new base config
	cfg := &config.Dendrite{}
	err := SetDefaults(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to set config defaults: %w", err)
	}

	// Set basic identity configuration
	cfg.Global.ServerName = spec.ServerName(c.ServerName)
	cfg.Global.PrivateKey = c.PrivateKey
	cfg.Global.KeyID = gomatrixserverlib.KeyID(c.KeyID)

	// Set storage paths
	cfg.Global.DatabaseOptions.ConnectionString = config.DataSource("file:" + c.DatabasePath)
	cfg.MediaAPI.BasePath = config.Path(c.MediaStorePath)
	cfg.Global.JetStream.StoragePath = config.Path(c.JetStreamPath)

	// Configure caching
	cfg.Global.Cache.EstimatedMaxSize = config.DataUnit(c.CacheMaxSize)
	cfg.Global.Cache.MaxAge = c.CacheMaxAge

	// Configure federation
	cfg.Global.DisableFederation = c.DisableFederation

	// Set up metrics
	if c.EnableMetrics {
		cfg.Global.Metrics.Enabled = true
		if c.MetricsUsername != "" && c.MetricsPassword != "" {
			cfg.Global.Metrics.BasicAuth = struct {
				Username string `yaml:"username"`
				Password string `yaml:"password"`
			}{
				Username: c.MetricsUsername,
				Password: c.MetricsPassword,
			}
		}
	}

	// Configure rate limiting
	if c.RateLimitYAMLPath != "" {
		cfg.ClientAPI.RateLimiting.Enabled = true
		// Use custom rate limiting file if provided
		// Note: This assumes the Dendrite config structure supports setting this
	}

	// Enable registration by default for embedded servers
	cfg.ClientAPI.OpenRegistrationWithoutVerificationEnabled = true
	cfg.ClientAPI.RegistrationDisabled = false

	return cfg, nil
}

// SetDefaults populates a Dendrite config with sensible default values
func SetDefaults(cfg *config.Dendrite) error {
	// Create a new config with default values if nil
	if cfg == nil {
		return fmt.Errorf("cannot set defaults on nil config")
	}

	// Global defaults
	if cfg.Global.ServerName == "" {
		cfg.Global.ServerName = "localhost"
	}
	if cfg.Global.DatabaseOptions.ConnectionString == "" {
		cfg.Global.DatabaseOptions.ConnectionString = "file:dendrite.db"
	}
	if cfg.Global.JetStream.StoragePath == "" {
		cfg.Global.JetStream.StoragePath = config.Path("jetstream")
	}

	// Cache defaults
	if cfg.Global.Cache.EstimatedMaxSize == 0 {
		cfg.Global.Cache.EstimatedMaxSize = config.DataUnit(64 * 1024 * 1024) // 64 MB
	}
	if cfg.Global.Cache.MaxAge == 0 {
		cfg.Global.Cache.MaxAge = time.Hour
	}

	// Media API defaults
	if cfg.MediaAPI.BasePath == "" {
		cfg.MediaAPI.BasePath = config.Path("media_store")
	}

	// Client API defaults
	cfg.ClientAPI.OpenRegistrationWithoutVerificationEnabled = true
	cfg.ClientAPI.RegistrationDisabled = false

	// Room server defaults
	if cfg.RoomServer.Database.ConnectionString == "" {
		cfg.RoomServer.Database.ConnectionString = cfg.Global.DatabaseOptions.ConnectionString
	}

	// Federation API defaults
	if cfg.FederationAPI.Database.ConnectionString == "" {
		cfg.FederationAPI.Database.ConnectionString = cfg.Global.DatabaseOptions.ConnectionString
	}

	// Sync API defaults
	if cfg.SyncAPI.Database.ConnectionString == "" {
		cfg.SyncAPI.Database.ConnectionString = cfg.Global.DatabaseOptions.ConnectionString
	}

	// Media API db defaults
	if cfg.MediaAPI.Database.ConnectionString == "" {
		cfg.MediaAPI.Database.ConnectionString = cfg.Global.DatabaseOptions.ConnectionString
	}

	// User API db defaults
	if cfg.UserAPI.AccountDatabase.ConnectionString == "" {
		cfg.UserAPI.AccountDatabase.ConnectionString = cfg.Global.DatabaseOptions.ConnectionString
	}

	return nil
}
