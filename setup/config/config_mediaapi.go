package config

import (
	"fmt"
	"time"
)

type MediaAPI struct {
	Matrix *Global `yaml:"-"`

	// The MediaAPI database stores information about files uploaded and downloaded
	// by local users. It is only accessed by the MediaAPI.
	Database DatabaseOptions `yaml:"database,omitempty"`

	// The base path to where the media files will be stored. May be relative or absolute.
	BasePath Path `yaml:"base_path"`

	// The absolute base path to where media files will be stored.
	AbsBasePath Path `yaml:"-"`

	// The maximum file size in bytes that is allowed to be stored on this server.
	// Note: if max_file_size_bytes is set to 0, the size is unlimited.
	// Note: if max_file_size_bytes is not set, it will default to 10485760 (10MB)
	MaxFileSizeBytes FileSizeBytes `yaml:"max_file_size_bytes,omitempty"`

	// Whether to dynamically generate thumbnails on-the-fly if the requested resolution is not already generated
	DynamicThumbnails bool `yaml:"dynamic_thumbnails"`

	// The maximum number of simultaneous thumbnail generators. default: 10
	MaxThumbnailGenerators int `yaml:"max_thumbnail_generators"`

	// A list of thumbnail sizes to be pre-generated for downloaded remote / uploaded content
	ThumbnailSizes []ThumbnailSize `yaml:"thumbnail_sizes"`

	// URL preview settings
	URLPreviews URLPreviews `yaml:"url_previews"`
}

// DefaultMaxFileSizeBytes defines the default file size allowed in transfers
var DefaultMaxFileSizeBytes = FileSizeBytes(10485760)

func (c *MediaAPI) Defaults(opts DefaultOpts) {
	c.MaxFileSizeBytes = DefaultMaxFileSizeBytes
	c.MaxThumbnailGenerators = 10
	c.URLPreviews.Defaults()
	if opts.Generate {
		c.ThumbnailSizes = []ThumbnailSize{
			{
				Width:        32,
				Height:       32,
				ResizeMethod: "crop",
			},
			{
				Width:        96,
				Height:       96,
				ResizeMethod: "crop",
			},
			{
				Width:        640,
				Height:       480,
				ResizeMethod: "scale",
			},
		}
		if !opts.SingleDatabase {
			c.Database.ConnectionString = "file:mediaapi.db"
		}
		c.BasePath = "./media_store"
	}
}

func (c *MediaAPI) Verify(configErrs *ConfigErrors) {
	checkNotEmpty(configErrs, "media_api.base_path", string(c.BasePath))
	checkPositive(configErrs, "media_api.max_file_size_bytes", int64(c.MaxFileSizeBytes))
	checkPositive(configErrs, "media_api.max_thumbnail_generators", int64(c.MaxThumbnailGenerators))

	for i, size := range c.ThumbnailSizes {
		checkPositive(configErrs, fmt.Sprintf("media_api.thumbnail_sizes[%d].width", i), int64(size.Width))
		checkPositive(configErrs, fmt.Sprintf("media_api.thumbnail_sizes[%d].height", i), int64(size.Height))
	}

	if c.Matrix.DatabaseOptions.ConnectionString == "" {
		checkNotEmpty(configErrs, "media_api.database.connection_string", string(c.Database.ConnectionString))
	}

	c.URLPreviews.Verify(configErrs)
}

type URLPreviews struct {
	Enabled        bool          `yaml:"enabled"`
	MaxPageSize    int64         `yaml:"max_page_size"`
	AllowedDomains []string      `yaml:"allowed_domains"`
	BlockedDomains []string      `yaml:"blocked_domains"`
	UserAgent      string        `yaml:"user_agent"`
	CacheTTL       time.Duration `yaml:"cache_ttl"`
	Timeout        time.Duration `yaml:"timeout"`
}

const (
	defaultPreviewMaxPageSize = int64(10 * 1024 * 1024) // 10MB
	defaultPreviewCacheTTL    = time.Hour
	defaultPreviewTimeout     = 10 * time.Second
	defaultPreviewUserAgent   = "DendriteURLPreview/1.0"
)

func (u *URLPreviews) Defaults() {
	if u.MaxPageSize == 0 {
		u.MaxPageSize = defaultPreviewMaxPageSize
	}
	if u.CacheTTL == 0 {
		u.CacheTTL = defaultPreviewCacheTTL
	}
	if u.Timeout == 0 {
		u.Timeout = defaultPreviewTimeout
	}
	if u.UserAgent == "" {
		u.UserAgent = defaultPreviewUserAgent
	}
}

func (u *URLPreviews) Verify(configErrs *ConfigErrors) {
	if !u.Enabled {
		return
	}
	checkPositive(configErrs, "media_api.url_previews.max_page_size", u.MaxPageSize)
	checkPositive(configErrs, "media_api.url_previews.cache_ttl", int64(u.CacheTTL))
	checkPositive(configErrs, "media_api.url_previews.timeout", int64(u.Timeout))
	checkNotEmpty(configErrs, "media_api.url_previews.user_agent", u.UserAgent)
}
