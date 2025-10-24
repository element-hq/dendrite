package config

import (
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type ClientAPI struct {
	Matrix  *Global  `yaml:"-"`
	Derived *Derived `yaml:"-"` // TODO: Nuke Derived from orbit

	// If set disables new users from registering (except via shared
	// secrets)
	RegistrationDisabled bool `yaml:"registration_disabled"`

	// If set, requires users to submit a token during registration.
	// Tokens can be managed using admin API.
	RegistrationRequiresToken bool `yaml:"registration_requires_token"`

	// Enable registration without captcha verification or shared secret.
	// This option is populated by the -really-enable-open-registration
	// command line parameter as it is not recommended.
	OpenRegistrationWithoutVerificationEnabled bool `yaml:"-"`

	// If set, allows registration by anyone who also has the shared
	// secret, even if registration is otherwise disabled.
	RegistrationSharedSecret string `yaml:"registration_shared_secret"`
	// If set, prevents guest accounts from being created. Only takes
	// effect if registration is enabled, otherwise guests registration
	// is forbidden either way.
	GuestsDisabled bool `yaml:"guests_disabled"`

	// Boolean stating whether catpcha registration is enabled
	// and required
	RecaptchaEnabled bool `yaml:"enable_registration_captcha"`
	// Recaptcha api.js Url, for compatible with hcaptcha.com, etc.
	RecaptchaApiJsUrl string `yaml:"recaptcha_api_js_url"`
	// Recaptcha div class for sitekey, for compatible with hcaptcha.com, etc.
	RecaptchaSitekeyClass string `yaml:"recaptcha_sitekey_class"`
	// Recaptcha form field, for compatible with hcaptcha.com, etc.
	RecaptchaFormField string `yaml:"recaptcha_form_field"`
	// This Home Server's ReCAPTCHA public key.
	RecaptchaPublicKey string `yaml:"recaptcha_public_key"`
	// This Home Server's ReCAPTCHA private key.
	RecaptchaPrivateKey string `yaml:"recaptcha_private_key"`
	// Secret used to bypass the captcha registration entirely
	RecaptchaBypassSecret string `yaml:"recaptcha_bypass_secret"`
	// HTTP API endpoint used to verify whether the captcha response
	// was successful
	RecaptchaSiteVerifyAPI string `yaml:"recaptcha_siteverify_api"`

	// TURN options
	TURN TURN `yaml:"turn"`

	// Rate-limiting options
	RateLimiting RateLimiting `yaml:"rate_limiting"`

	// Password reset configuration
	PasswordReset PasswordReset `yaml:"password_reset"`
	ThreePIDEmail ThreePIDEmail `yaml:"threepid_email"`

	MSCs *MSCs `yaml:"-"`
}

func (c *ClientAPI) Defaults(opts DefaultOpts) {
	c.RegistrationSharedSecret = ""
	c.RegistrationRequiresToken = false
	c.RecaptchaPublicKey = ""
	c.RecaptchaPrivateKey = ""
	c.RecaptchaEnabled = false
	c.RecaptchaBypassSecret = ""
	c.RecaptchaSiteVerifyAPI = ""
	c.RegistrationDisabled = true
	c.OpenRegistrationWithoutVerificationEnabled = false
	c.RateLimiting.Defaults()
	c.PasswordReset.Defaults()
	c.ThreePIDEmail.Defaults()
}

func (c *ClientAPI) Verify(configErrs *ConfigErrors) {
	c.TURN.Verify(configErrs)
	c.RateLimiting.Verify(configErrs)
	c.PasswordReset.Verify(configErrs)
	c.ThreePIDEmail.Verify(configErrs)
	if c.RecaptchaEnabled {
		if c.RecaptchaSiteVerifyAPI == "" {
			c.RecaptchaSiteVerifyAPI = "https://www.google.com/recaptcha/api/siteverify"
		}
		if c.RecaptchaApiJsUrl == "" {
			c.RecaptchaApiJsUrl = "https://www.google.com/recaptcha/api.js"
		}
		if c.RecaptchaFormField == "" {
			c.RecaptchaFormField = "g-recaptcha-response"
		}
		if c.RecaptchaSitekeyClass == "" {
			c.RecaptchaSitekeyClass = "g-recaptcha"
		}
		checkNotEmpty(configErrs, "client_api.recaptcha_public_key", c.RecaptchaPublicKey)
		checkNotEmpty(configErrs, "client_api.recaptcha_private_key", c.RecaptchaPrivateKey)
		checkNotEmpty(configErrs, "client_api.recaptcha_siteverify_api", c.RecaptchaSiteVerifyAPI)
		checkNotEmpty(configErrs, "client_api.recaptcha_sitekey_class", c.RecaptchaSitekeyClass)
	}
	// Ensure there is any spam counter measure when enabling registration
	if !c.RegistrationDisabled && !c.OpenRegistrationWithoutVerificationEnabled && !c.RecaptchaEnabled {
		configErrs.Add(
			"You have tried to enable open registration without any secondary verification methods " +
				"(such as reCAPTCHA). By enabling open registration, you are SIGNIFICANTLY " +
				"increasing the risk that your server will be used to send spam or abuse, and may result in " +
				"your server being banned from some rooms. If you are ABSOLUTELY CERTAIN you want to do this, " +
				"start Dendrite with the -really-enable-open-registration command line flag. Otherwise, you " +
				"should set the registration_disabled option in your Dendrite config.",
		)
	}
}

type TURN struct {
	// TODO Guest Support
	// Whether or not guests can request TURN credentials
	// AllowGuests bool `yaml:"turn_allow_guests"`
	// How long the authorization should last
	UserLifetime string `yaml:"turn_user_lifetime"`
	// The list of TURN URIs to pass to clients
	URIs []string `yaml:"turn_uris"`

	// Authorization via Shared Secret
	// The shared secret from coturn
	SharedSecret string `yaml:"turn_shared_secret"`

	// Authorization via Static Username & Password
	// Hardcoded Username and Password
	Username string `yaml:"turn_username"`
	Password string `yaml:"turn_password"`
}

func (c *TURN) Verify(configErrs *ConfigErrors) {
	value := c.UserLifetime
	if value != "" {
		if _, err := time.ParseDuration(value); err != nil {
			configErrs.Add(fmt.Sprintf("invalid duration for config key %q: %s", "client_api.turn.turn_user_lifetime", value))
		}
	}
}

type RateLimiting struct {
	// Is rate limiting enabled or disabled?
	Enabled bool `yaml:"enabled"`

	// How many "slots" a user can occupy sending requests to a rate-limited
	// endpoint before we apply rate-limiting
	Threshold int64 `yaml:"threshold"`

	// The cooloff period in milliseconds after a request before the "slot"
	// is freed again
	CooloffMS int64 `yaml:"cooloff_ms"`

	// A list of users that are exempt from rate limiting, i.e. if you want
	// to run Mjolnir or other bots.
	ExemptUserIDs []string `yaml:"exempt_user_ids"`

	// A list of IP addresses or CIDR ranges that bypass rate limiting.
	ExemptIPAddresses []string `yaml:"exempt_ip_addresses"`

	// Per-endpoint overrides allow custom thresholds and cooloff periods for specific routes.
	PerEndpointOverrides map[string]RateLimitEndpointOverride `yaml:"per_endpoint_overrides"`
}

type PasswordReset struct {
	Enabled       bool          `yaml:"enabled"`
	TokenLifetime time.Duration `yaml:"token_lifetime"`
	PublicBaseURL string        `yaml:"public_base_url"`
	From          string        `yaml:"from"`
	Subject       string        `yaml:"subject"`
	SMTP          SMTP          `yaml:"smtp"`
}

func (p *PasswordReset) Defaults() {
	if p.TokenLifetime == 0 {
		p.TokenLifetime = time.Hour
	}
	if p.Subject == "" {
		p.Subject = "Reset your Matrix password"
	}
	p.SMTP.Defaults()
}

func (p *PasswordReset) Verify(configErrs *ConfigErrors) {
	if !p.Enabled {
		return
	}
	if p.PublicBaseURL == "" {
		configErrs.Add("client_api.password_reset.public_base_url must be set when password reset is enabled")
	}
	if p.PublicBaseURL != "" {
		u, err := url.Parse(p.PublicBaseURL)
		if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
			configErrs.Add("client_api.password_reset.public_base_url must be a valid https:// URL")
		}
	}
	if p.TokenLifetime <= 0 {
		configErrs.Add("client_api.password_reset.token_lifetime must be positive")
	}
	if p.From == "" {
		configErrs.Add("client_api.password_reset.from must be set when password reset is enabled")
	}
	if p.From != "" {
		if _, err := mail.ParseAddress(p.From); err != nil {
			configErrs.Add("client_api.password_reset.from must be a valid email address")
		}
	}
	if containsHeaderInjection(p.Subject) {
		configErrs.Add("client_api.password_reset.subject must not contain control characters")
	}
	if p.SMTP.Host == "" {
		configErrs.Add("client_api.password_reset.smtp.host must be set when password reset is enabled")
	}
	if p.SMTP.Port <= 0 {
		configErrs.Add("client_api.password_reset.smtp.port must be set to a positive value when password reset is enabled")
	}
	if p.SMTP.Username != "" && p.SMTP.GetPassword() == "" {
		configErrs.Add("client_api.password_reset.smtp.username set but DENDRITE_SMTP_PASSWORD is empty")
	}
	if p.SMTP.SkipTLSVerify {
		configErrs.Add("client_api.password_reset.smtp.skip_tls_verify must remain false for password reset emails")
	}
}

type ThreePIDEmail struct {
	Enabled       bool          `yaml:"enabled"`
	TokenLifetime time.Duration `yaml:"token_lifetime"`
	PublicBaseURL string        `yaml:"public_base_url"`
	From          string        `yaml:"from"`
	Subject       string        `yaml:"subject"`
	SMTP          SMTP          `yaml:"smtp"`
}

func (t *ThreePIDEmail) Defaults() {
	if t.TokenLifetime == 0 {
		t.TokenLifetime = 24 * time.Hour
	}
	if t.Subject == "" {
		t.Subject = "Verify your email for Matrix"
	}
	t.SMTP.Defaults()
}

func (t *ThreePIDEmail) Verify(configErrs *ConfigErrors) {
	if !t.Enabled {
		return
	}
	if t.PublicBaseURL == "" {
		configErrs.Add("client_api.threepid_email.public_base_url must be set when email verification is enabled")
	}
	if t.PublicBaseURL != "" {
		u, err := url.Parse(t.PublicBaseURL)
		if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
			configErrs.Add("client_api.threepid_email.public_base_url must be a valid https:// URL")
		}
	}
	if t.TokenLifetime <= 0 {
		configErrs.Add("client_api.threepid_email.token_lifetime must be positive")
	}
	if t.From == "" {
		configErrs.Add("client_api.threepid_email.from must be set when email verification is enabled")
	}
	if t.From != "" {
		if _, err := mail.ParseAddress(t.From); err != nil {
			configErrs.Add("client_api.threepid_email.from must be a valid email address")
		}
	}
	if containsHeaderInjection(t.Subject) {
		configErrs.Add("client_api.threepid_email.subject must not contain control characters")
	}
	if t.SMTP.Host == "" {
		configErrs.Add("client_api.threepid_email.smtp.host must be set when email verification is enabled")
	}
	if t.SMTP.Port <= 0 {
		configErrs.Add("client_api.threepid_email.smtp.port must be set to a positive value when email verification is enabled")
	}
	if t.SMTP.Username != "" && t.SMTP.GetPassword() == "" {
		configErrs.Add("client_api.threepid_email.smtp.username set but DENDRITE_SMTP_PASSWORD is empty")
	}
	if t.SMTP.SkipTLSVerify {
		configErrs.Add("client_api.threepid_email.smtp.skip_tls_verify must remain false for email verification emails")
	}
}

func (r *RateLimiting) Verify(configErrs *ConfigErrors) {
	if r.Enabled {
		// Validate that both threshold and cooloff are positive when rate limiting is enabled
		if r.Threshold <= 0 || r.CooloffMS <= 0 {
			configErrs.Add(
				"client_api.rate_limiting: both 'threshold' and 'cooloff_ms' must be positive when rate limiting is enabled. " +
					"Set 'enabled: false' to disable rate limiting, or provide valid positive values for both parameters.",
			)
		} else {
			checkPositive(configErrs, "client_api.rate_limiting.threshold", r.Threshold)
			checkPositive(configErrs, "client_api.rate_limiting.cooloff_ms", r.CooloffMS)
		}

		// Validate per-endpoint overrides
		for name, override := range r.PerEndpointOverrides {
			if override.Threshold <= 0 || override.CooloffMS <= 0 {
				configErrs.Add(
					fmt.Sprintf("client_api.rate_limiting.per_endpoint_overrides.%s: both 'threshold' and 'cooloff_ms' must be positive", name),
				)
			} else {
				checkPositive(
					configErrs,
					fmt.Sprintf("client_api.rate_limiting.per_endpoint_overrides.%s.threshold", name),
					override.Threshold,
				)
				checkPositive(
					configErrs,
					fmt.Sprintf("client_api.rate_limiting.per_endpoint_overrides.%s.cooloff_ms", name),
					override.CooloffMS,
				)
			}
		}

		// Validate IP exemptions
		for _, ip := range r.ExemptIPAddresses {
			if _, _, err := net.ParseCIDR(ip); err != nil {
				if parsedIP := net.ParseIP(ip); parsedIP == nil {
					configErrs.Add(fmt.Sprintf("invalid IP address or CIDR for config key %q: %s", "client_api.rate_limiting.exempt_ip_addresses", ip))
				}
			}
		}
	}
}

func (r *RateLimiting) Defaults() {
	// Default to disabled to maintain backward compatibility with existing deployments.
	// Administrators should explicitly enable rate limiting in their configuration.
	r.Enabled = false
	r.Threshold = 5
	r.CooloffMS = 500
	if r.PerEndpointOverrides == nil {
		r.PerEndpointOverrides = make(map[string]RateLimitEndpointOverride)
	}
}

type RateLimitEndpointOverride struct {
	// Threshold defines how many concurrent slots the override allows.
	Threshold int64 `yaml:"threshold"`
	// CooloffMS controls how long in milliseconds before a slot is released.
	CooloffMS int64 `yaml:"cooloff_ms"`
}

type SMTP struct {
	Host          string `yaml:"host"`
	Port          int    `yaml:"port"`
	Username      string `yaml:"username"`
	RequireTLS    bool   `yaml:"require_tls"`
	SkipTLSVerify bool   `yaml:"skip_tls_verify"`
	passwordOnce  sync.Once
	password      string
}

func (s *SMTP) Defaults() {
	if s.Port == 0 {
		s.Port = 587
	}
	if !s.RequireTLS {
		s.RequireTLS = true
	}
}

func (s *SMTP) GetPassword() string {
	s.passwordOnce.Do(func() {
		s.password = os.Getenv("DENDRITE_SMTP_PASSWORD")
	})
	return s.password
}

func containsHeaderInjection(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}
