package config

import (
	"time"
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
	SeedPeer string `config:"seed_peer"` // default: ipfs.pinner.xyz
	RepoPath string `config:"repo_path"` // IPFS repository path
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

// Defaults implements the Defaults interface for providing default configuration values.
func (c APIConfig) Defaults() map[string]any {
	return map[string]any{
		"Timeout": 30 * time.Second,
	}
}

// Defaults implements the Defaults interface for providing default configuration values.
func (c IPFSConfig) Defaults() map[string]any {
	return map[string]any{
		"SeedPeer": "ipfs.pinner.xyz",
		"RepoPath": "./data/ipfs",
	}
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

// Defaults implements the Defaults interface for providing default configuration values.
func (c LoggingConfig) Defaults() map[string]any {
	return map[string]any{
		"Level": "info",
	}
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




