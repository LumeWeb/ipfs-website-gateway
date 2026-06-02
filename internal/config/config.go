package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/multiformats/go-multiaddr"

	"github.com/Oudwins/zog"
)

// Config represents the complete application configuration.
type Config struct {
	Server    ServerConfig    `config:"server"`
	API       APIConfig       `config:"api"`
	IPFS      IPFSConfig      `config:"ipfs"`
	Cache     CacheConfig     `config:"cache"`
	Prewarm   PrewarmConfig   `config:"prewarm"`
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
	SeedPeer         string        `config:"seed_peer"`
	ConnectTimeout   time.Duration `config:"connect_timeout"`
	RetrievalTimeout time.Duration `config:"retrieval_timeout"`
	PubsubEnabled    bool          `config:"pubsub_enabled"`
	Seed             string        `config:"seed"`
}

func (c IPFSConfig) RoutingEndpoint() string {
	if c.SeedPeer == "" {
		return ""
	}

	host := c.SeedPeer

	if strings.HasPrefix(c.SeedPeer, "/") {
		ma, err := multiaddr.NewMultiaddr(c.SeedPeer)
		if err != nil {
			return ""
		}
		for _, code := range []int{multiaddr.P_DNSADDR, multiaddr.P_DNS4, multiaddr.P_DNS6, multiaddr.P_DNS} {
			if val, err := ma.ValueForProtocol(code); err == nil {
				host = val
				break
			}
		}
	}

	if u, err := url.Parse("//" + host); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}

	return fmt.Sprintf("https://%s/routing/v1", host)
}

// CacheConfig holds caching configuration settings for both status and content.
type CacheConfig struct {
	StatusCacheTTL         time.Duration `config:"status_cache_ttl"`
	StatusCacheShortTTL    time.Duration `config:"status_cache_short_ttl"`
	StatusCacheLRUSize     int           `config:"status_cache_lru_size"`
	ContentCachePath     string        `config:"content_cache_path"`
	ContentCacheMaxBytes int64         `config:"content_cache_max_bytes"`
	ContentCacheLRUSize  int           `config:"content_cache_lru_size"`
	IPNSCacheLRUSize     int           `config:"ipns_cache_lru_size"`
	IPNSCacheFreshTTL    time.Duration `config:"ipns_cache_fresh_ttl"`
	IPNSCachePath        string        `config:"ipns_cache_path"`
}

type PrewarmConfig struct {
	Enabled       bool          `config:"enabled"`
	MaxConc       int           `config:"max_concurrency"`
	RetryAttempts uint          `config:"retry_attempts"`
	RetryDelay    time.Duration `config:"retry_delay"`
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
		"SeedPeer":         "ipfs.pinner.xyz",
		"ConnectTimeout":   30 * time.Second,
		"RetrievalTimeout": 30 * time.Second,
		"PubsubEnabled":    true,
	}
}

func (c IPFSConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"SeedPeer":         zog.String().Optional(),
		"ConnectTimeout":   zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool { return *valPtr > 0 }),
		"RetrievalTimeout": zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool { return *valPtr > 0 }),
		"Seed": zog.String().Required(zog.Message("seed is required")),
	})
}

// Defaults implements the Defaults interface for providing default configuration values.
func (c CacheConfig) Defaults() map[string]any {
	return map[string]any{
		"StatusCacheTTL":         5 * time.Minute,
		"StatusCacheShortTTL":    30 * time.Second,
		"StatusCacheLRUSize":     1000,
		"ContentCachePath":     "/data/cache",
		"ContentCacheMaxBytes": int64(10) * 1024 * 1024 * 1024, // 10 GB
		"ContentCacheLRUSize":  100000,
		"IPNSCacheLRUSize":     140000,
		"IPNSCacheFreshTTL":    30 * time.Second,
		"IPNSCachePath":        "/data/ipns",
	}
}

// Defaults implements the Defaults interface for providing default configuration values.
func (c PrewarmConfig) Defaults() map[string]any {
	return map[string]any{
		"Enabled":       true,
		"MaxConc":       2,
		"RetryAttempts": uint(2),
		"RetryDelay":    1 * time.Second,
	}
}

// Schema implements the ConfigSchemaProvider interface for Zog validation.
func (c CacheConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"StatusCacheTTL":         zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool { return *valPtr > 0 }),
		"StatusCacheShortTTL":    zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool { return *valPtr > 0 }),
		"StatusCacheLRUSize":     zog.Int().GT(0).Optional(),
		"ContentCachePath":     zog.String().Optional(),
		"ContentCacheMaxBytes": zog.Int64().GT(0).Optional(),
		"ContentCacheLRUSize":  zog.Int().GT(0).Optional(),
		"IPNSCacheLRUSize":     zog.Int().GT(0).Optional(),
		"IPNSCacheFreshTTL":    zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool { return *valPtr > 0 }),
		"IPNSCachePath":        zog.String().Optional(),
	})
}

// Schema implements the ConfigSchemaProvider interface for Zog validation.
func (c PrewarmConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"Enabled":       zog.Bool().Optional(),
		"MaxConc":       zog.Int().GT(0).Optional(),
		"RetryAttempts": zog.Uint().GT(0).Optional(),
		"RetryDelay":    zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool { return *valPtr > 0 }),
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




