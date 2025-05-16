package config

type MSCs struct {
	Matrix *Global `yaml:"-"`

	// The MSCs to enable. Supported MSCs include:
	// 'msc3861': Delegate auth to an OIDC provider - https://github.com/matrix-org/matrix-spec-proposals/pull/3861
	// 'msc2444': Peeking over federation - https://github.com/matrix-org/matrix-doc/pull/2444
	// 'msc2753': Peeking via /sync - https://github.com/matrix-org/matrix-doc/pull/2753
	// 'msc2836': Threading - https://github.com/matrix-org/matrix-doc/pull/2836
	MSCs []string `yaml:"mscs"`

	// MSC3861 contains config related to the experimental feature MSC3861.
	// It takes effect only if 'msc3861' is included in 'MSCs' array.
	MSC3861 *MSC3861 `yaml:"msc3861,omitempty"`

	Database DatabaseOptions `yaml:"database,omitempty"`
}

func (c *MSCs) Defaults(opts DefaultOpts) {
	if opts.Generate {
		if !opts.SingleDatabase {
			c.Database.ConnectionString = "file:mscs.db"
		}
	}
}

// Enabled returns true if the given msc is enabled. Should in the form 'msc12345'.
func (c *MSCs) Enabled(msc string) bool {
	for _, m := range c.MSCs {
		if m == msc {
			return true
		}
	}
	return false
}

func (c *MSCs) Verify(configErrs *ConfigErrors) {
	if c.Matrix.DatabaseOptions.ConnectionString == "" {
		checkNotEmpty(configErrs, "mscs.database.connection_string", string(c.Database.ConnectionString))
	}
	if m := c.MSC3861; m != nil && c.MSC3861Enabled() {
		m.Verify(configErrs)
	}
}

func (c *MSCs) MSC3861Enabled() bool {
	return c.Enabled("msc3861") && c.MSC3861 != nil
}

type MSC3861 struct {
	Issuer               string `yaml:"issuer"`
	ClientID             string `yaml:"client_id"`
	ClientSecret         string `yaml:"client_secret"`
	AdminToken           string `yaml:"admin_token"`
	AccountManagementURL string `yaml:"account_management_url"`
}

func (m *MSC3861) Verify(configErrs *ConfigErrors) {
	checkNotEmpty(configErrs, "mscs.msc3861.issuer", string(m.Issuer))
	checkNotEmpty(configErrs, "mscs.msc3861.client_id", string(m.ClientID))
	checkNotEmpty(configErrs, "mscs.msc3861.client_secret", string(m.ClientSecret))
	checkNotEmpty(configErrs, "mscs.msc3861.admin_token", string(m.AdminToken))
	checkNotEmpty(configErrs, "mscs.msc3861.account_management_url", string(m.AccountManagementURL))
}
