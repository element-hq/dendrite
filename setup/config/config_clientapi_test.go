package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
)

func TestRateLimitingVerifyPerEndpointOverrides(t *testing.T) {
	rateLimiting := RateLimiting{
		Enabled:   true,
		Threshold: 5,
		CooloffMS: 500,
		PerEndpointOverrides: map[string]RateLimitEndpointOverride{
			"/_matrix/client/v3/sync": {
				Threshold: -1,
				CooloffMS: 100,
			},
		},
	}

	var configErrs ConfigErrors
	rateLimiting.Verify(&configErrs)

	// New validation produces a more comprehensive error message
	assert.Contains(t, configErrs, `client_api.rate_limiting.per_endpoint_overrides./_matrix/client/v3/sync: both 'threshold' and 'cooloff_ms' must be positive`)
}

func TestRateLimitingPerEndpointOverrideYAML(t *testing.T) {
	input := `
enabled: true
threshold: 5
cooloff_ms: 500
per_endpoint_overrides:
  "/_matrix/client/v3/sync":
    threshold: 10
    cooloff_ms: 1000
`

	var rateLimiting RateLimiting
	err := yaml.Unmarshal([]byte(input), &rateLimiting)
	assert.NoError(t, err)

	override, ok := rateLimiting.PerEndpointOverrides["/_matrix/client/v3/sync"]
	assert.True(t, ok)
	assert.Equal(t, int64(10), override.Threshold)
	assert.Equal(t, int64(1000), override.CooloffMS)
}

func TestRateLimitingVerifyExemptIPAddresses(t *testing.T) {
	rateLimiting := RateLimiting{
		Enabled:           true,
		Threshold:         5,
		CooloffMS:         500,
		ExemptIPAddresses: []string{"127.0.0.1", "192.168.1.0/24"},
	}

	var configErrs ConfigErrors
	rateLimiting.Verify(&configErrs)

	assert.Empty(t, configErrs)
}

func TestRateLimitingVerifyExemptIPAddressesInvalid(t *testing.T) {
	rateLimiting := RateLimiting{
		Enabled:           true,
		Threshold:         5,
		CooloffMS:         500,
		ExemptIPAddresses: []string{"not-an-ip"},
	}

	var configErrs ConfigErrors
	rateLimiting.Verify(&configErrs)

	assert.Contains(t, configErrs, `invalid IP address or CIDR for config key "client_api.rate_limiting.exempt_ip_addresses": not-an-ip`)
}
