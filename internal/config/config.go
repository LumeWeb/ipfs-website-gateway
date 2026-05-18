package config

import (
	"time"

	"github.com/Oudwins/zog"
)

// Config represents the complete application configuration.
type Config struct {
	Server    ServerConfig    `config:"server"`
	API       APIConfig       `config:"api"`
	IPFS      IPFSConfig      `config:"ipfs"`
	Cache     CacheConfig     `config:"cache"`
	Logging   LoggingConfig   `config:"logging"`
	RateLimit RateLimitConfig `config:"rate_limit"`
}

// ServerConfig holds HTTP server configuration settings.
type ServerConfig struct {
	Port          int      `config:"port"`
	TrustedProxies []string `config:"trusted_proxies"`
	AllowedSecret string   `config:"allowed_secret"`
}

// APIConfig holds external API configuration settings.
type APIConfig struct {
	URL     string        `config:"url"`
	Secret  string        `config:"secret"`
	Timeout time.Duration `config:"timeout"`
}

// IPFSConfig holds IPFS network configuration settings.
type IPFSConfig struct {
	SeedPeer        string        `config:"seed_peer"`
	ConnectTimeout  time.Duration `config:"connect_timeout"`
	RetrievalTimeout time.Duration `config:"retrieval_timeout"`
}

// CacheConfig holds caching configuration settings for both status and content.
type CacheConfig struct {
	StatusCacheTTL       time.Duration `config:"status_cache_ttl"`
	StatusCacheLRUSize   int           `config:"status_cache_lru_size"`
	ContentCachePath     string        `config:"content_cache_path"`
	ContentCacheMaxBytes int64         `config:"content_cache_max_bytes"`
	ContentCacheLRUSize  int           `config:"content_cache_lru_size"`
}

// LoggingConfig holds logging configuration settings.
type LoggingConfig struct {
	Level string `config:"level"` // debug, info, warn, error
}

// RateLimitConfig holds rate limiting configuration for the /allowed endpoint.
type RateLimitConfig struct {
	Enabled   bool          `config:"enabled"`
	Rate      float64       `config:"rate"`       // requests per second
	Burst     int           `config:"burst"`      // max concurrent requests
	ExpiresIn time.Duration `config:"expires_in"` // cleanup interval
}

// Defaults implements the Defaults interface for providing default configuration values.
func (c ServerConfig) Defaults() map[string]any {
	return map[string]any{
		"Port": 8080,
	}
}

// Schema implements the ConfigSchemaProvider interface for Zog validation.
func (c ServerConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"Port":           zog.Int().GTE(1).LTE(65535).Optional(),
		"TrustedProxies": zog.Slice(zog.String()).Optional(),
		"AllowedSecret":  zog.String().Optional(),
	})
}

// Defaults implements the Defaults interface for providing default configuration values.
func (c APIConfig) Defaults() map[string]any {
	return map[string]any{
		"Timeout": 30 * time.Second,
	}
}

// Schema implements the ConfigSchemaProvider interface for Zog validation.
func (c APIConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"URL":     zog.String().Optional(),
		"Secret":  zog.String().Optional(),
		"Timeout": zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool {
			return *valPtr > 0
		}, zog.Message("timeout must be greater than 0")),
	})
}

// Defaults implements the Defaults interface for providing default configuration values.
func (c IPFSConfig) Defaults() map[string]any {
	return map[string]any{
		"SeedPeer":        "ipfs.pinner.xyz",
		"ConnectTimeout":  30 * time.Second,
		"RetrievalTimeout": 30 * time.Second,
	}
}

func (c IPFSConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"SeedPeer":         zog.String().Optional(),
		"ConnectTimeout":   zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool { return *valPtr > 0 }),
		"RetrievalTimeout": zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool { return *valPtr > 0 }),
	})
}

// Defaults implements the Defaults interface for providing default configuration values.
func (c CacheConfig) Defaults() map[string]any {
	return map[string]any{
		"StatusCacheTTL":       5 * time.Minute,
		"StatusCacheLRUSize":   1000,
		"ContentCachePath":     "/tmp/ipfs-cache",
		"ContentCacheMaxBytes": int64(10) * 1024 * 1024 * 1024, // 10 GB
		"ContentCacheLRUSize":  100000,
	}
}

// Schema implements the ConfigSchemaProvider interface for Zog validation.
func (c CacheConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"StatusCacheTTL":       zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool { return *valPtr > 0 }),
		"StatusCacheLRUSize":   zog.Int().GT(0).Optional(),
		"ContentCachePath":     zog.String().Optional(),
		"ContentCacheMaxBytes": zog.Int64().GT(0).Optional(),
		"ContentCacheLRUSize":  zog.Int().GT(0).Optional(),
	})
}

// Defaults implements the Defaults interface for providing default configuration values.
func (c LoggingConfig) Defaults() map[string]any {
	return map[string]any{
		"Level": "info",
	}
}

// Schema implements the ConfigSchemaProvider interface for Zog validation.
func (c LoggingConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"Level": zog.String().OneOf([]string{"debug", "info", "warn", "error"}).Optional(),
	})
}

// Defaults implements the Defaults interface for providing default configuration values.
func (c RateLimitConfig) Defaults() map[string]any {
	return map[string]any{
		"Enabled":   false,
		"Rate":      0.167,
		"Burst":     10,
		"ExpiresIn": 5 * time.Minute,
	}
}

// Schema implements the ConfigSchemaProvider interface for Zog validation.
// When rate limiting is enabled, Rate must be (0,1000], Burst must be >0,
// and ExpiresIn must be (0,24h].
func (c RateLimitConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"Enabled":   zog.Bool().Optional(),
		"Rate":      zog.Float64().GT(0).LTE(1000).Optional(),
		"Burst":     zog.Int().GT(0).Optional(),
		"ExpiresIn": zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool {
			return *valPtr > 0 && *valPtr <= 24*time.Hour
		}, zog.Message("expires_in must be between 0 and 24 hours")),
	})
}




