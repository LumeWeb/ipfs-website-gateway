package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/multiformats/go-multiaddr"

	"github.com/Oudwins/zog"
)

// Config represents the complete application configuration.
type Config struct {
	Server        ServerConfig        `config:"server"`
	API           APIConfig           `config:"api"`
	IPFS          IPFSConfig          `config:"ipfs"`
	Cache         CacheConfig         `config:"cache"`
	Prewarm       PrewarmConfig       `config:"prewarm"`
	Logging       LoggingConfig       `config:"logging"`
	RateLimit     RateLimitConfig     `config:"rate_limit"`
	Observability ObservabilityConfig `config:"observability"`
	HealthCheck   HealthCheckConfig   `config:"health_check"`
}

// HealthCheckConfig holds configuration for periodic gateway health checks.
type HealthCheckConfig struct {
	Websites []string      `config:"websites"`
	Interval time.Duration `config:"interval"`
	Timeout  time.Duration `config:"timeout"`
}

func (c HealthCheckConfig) Defaults() map[string]any {
	return map[string]any{
		"Interval": 1 * time.Minute,
		"Timeout":  15 * time.Second,
	}
}

func (c HealthCheckConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"Websites": zog.Slice(zog.String()).Optional(),
		"Interval": zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool {
			return *valPtr >= 10*time.Second
		}),
		"Timeout": zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool {
			return *valPtr > 0
		}),
	})
}

// ServerConfig holds HTTP server configuration settings.
type ServerConfig struct {
	Port           int      `config:"port"`
	TrustedProxies []string `config:"trusted_proxies"`
	AllowedSecret  string   `config:"allowed_secret"`
	GatewayDomain  string   `config:"gateway_domain"`
}

// APIConfig holds external API configuration settings.
type APIConfig struct {
	URL     string        `config:"url"`
	Secret  string        `config:"secret"`
	Timeout time.Duration `config:"timeout"`
	SSE     SSEConfig     `config:"sse"`
}

// SSEConfig holds configuration for the portal SSE event client.
type SSEConfig struct {
	Enabled     bool              `config:"enabled"`
	Reconnect   bool              `config:"reconnect"`
	Backoff     time.Duration     `config:"backoff"`
	MaxBackoff  time.Duration     `config:"max_backoff"`
	MaxRetries  int               `config:"max_retries"`
	BrokenWatch BrokenWatchConfig `config:"broken_watch"`
}

func (c SSEConfig) Defaults() map[string]any {
	return map[string]any{
		"Enabled":     true,
		"Reconnect":   true,
		"Backoff":     1 * time.Second,
		"MaxBackoff":  30 * time.Second,
		"MaxRetries":  0, // 0 = unlimited
		"BrokenWatch": c.BrokenWatch.Defaults(),
	}
}

func (c SSEConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"Enabled":     zog.Bool().Optional(),
		"Reconnect":   zog.Bool().Optional(),
		"Backoff":     zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool { return *valPtr > 0 }),
		"MaxBackoff":  zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool { return *valPtr > 0 }),
		"MaxRetries":  zog.Int().Optional(),
		"BrokenWatch": c.BrokenWatch.Schema(),
	})
}

// BrokenWatchConfig holds configuration for the broken-site recovery watcher
// that polls sites marked broken while the SSE stream is disconnected.
type BrokenWatchConfig struct {
	Enabled  bool          `config:"enabled"`
	Interval time.Duration `config:"interval"`
}

func (c BrokenWatchConfig) Defaults() map[string]any {
	return map[string]any{
		"Enabled":  true,
		"Interval": 30 * time.Second,
	}
}

func (c BrokenWatchConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"Enabled": zog.Bool().Optional(),
		"Interval": zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool {
			return *valPtr > 0
		}, zog.Message("broken_watch interval must be greater than 0")),
	})
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
	StatusCacheTTL       time.Duration `config:"status_cache_ttl"`
	StatusCacheShortTTL  time.Duration `config:"status_cache_short_ttl"`
	StatusCacheLRUSize   int           `config:"status_cache_lru_size"`
	StatusCacheStaleTTL  time.Duration `config:"status_cache_stale_ttl"`
	ContentCachePath     string        `config:"content_cache_path"`
	ContentCacheMaxBytes int64         `config:"content_cache_max_bytes"`
	ContentCacheLRUSize  int           `config:"content_cache_lru_size"`
	IPNSCacheLRUSize     int           `config:"ipns_cache_lru_size"`
	IPNSCacheFreshTTL    time.Duration `config:"ipns_cache_fresh_ttl"`
	RedisURL             string        `config:"redis_url"`
	RedisPassword        string        `config:"redis_password"`
	RedisDB              int           `config:"redis_db"`
	RedisKeyPrefix       string        `config:"redis_key_prefix"`
	RedisInsecureTLS     bool          `config:"redis_insecure_tls"`
}

type PrewarmConfig struct {
	Enabled       bool          `config:"enabled"`
	MaxConc       int           `config:"max_concurrency"`
	RetryAttempts uint          `config:"retry_attempts"`
	RetryDelay    time.Duration `config:"retry_delay"`
	DAGBatchConc  int           `config:"dag_batch_concurrency"`
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
		"GatewayDomain":  zog.String().Optional(),
	})
}

// Defaults implements the Defaults interface for providing default configuration values.
func (c APIConfig) Defaults() map[string]any {
	return map[string]any{
		"Timeout": 30 * time.Second,
		"SSE":     c.SSE.Defaults(),
	}
}

// Schema implements the ConfigSchemaProvider interface for Zog validation.
func (c APIConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"URL":    zog.String().Optional(),
		"Secret": zog.String().Optional(),
		"Timeout": zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool {
			return *valPtr > 0
		}, zog.Message("timeout must be greater than 0")),
		"SSE": c.SSE.Schema(),
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
		"Seed":             zog.String().Required(zog.Message("seed is required")),
	})
}

// Defaults implements the Defaults interface for providing default configuration values.
func (c CacheConfig) Defaults() map[string]any {
	return map[string]any{
		"StatusCacheTTL":       5 * time.Minute,
		"StatusCacheShortTTL":  30 * time.Second,
		"StatusCacheLRUSize":   1000,
		"StatusCacheStaleTTL":  10 * time.Minute,
		"ContentCachePath":     "/data/cache",
		"ContentCacheMaxBytes": int64(10) * 1024 * 1024 * 1024, // 10 GB
		"ContentCacheLRUSize":  100000,
		"IPNSCacheLRUSize":     140000,
		"IPNSCacheFreshTTL":    30 * time.Second,
		"RedisURL":             "redis://localhost:6379",
		"RedisDB":              0,
		"RedisKeyPrefix":       "gateway:",
		"RedisInsecureTLS":     false,
	}
}

// Defaults implements the Defaults interface for providing default configuration values.
func (c PrewarmConfig) Defaults() map[string]any {
	return map[string]any{
		"Enabled":       true,
		"MaxConc":       2,
		"RetryAttempts": uint(2),
		"RetryDelay":    1 * time.Second,
		"DAGBatchConc":  10,
	}
}

// Schema implements the ConfigSchemaProvider interface for Zog validation.
func (c CacheConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"StatusCacheTTL":       zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool { return *valPtr > 0 }),
		"StatusCacheShortTTL":  zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool { return *valPtr > 0 }),
		"StatusCacheLRUSize":   zog.Int().GT(0).Optional(),
		"StatusCacheStaleTTL":  zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool { return *valPtr > 0 }),
		"ContentCachePath":     zog.String().Optional(),
		"ContentCacheMaxBytes": zog.Int64().GT(0).Optional(),
		"ContentCacheLRUSize":  zog.Int().GT(0).Optional(),
		"IPNSCacheLRUSize":     zog.Int().GT(0).Optional(),
		"IPNSCacheFreshTTL":    zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool { return *valPtr > 0 }),
		"RedisURL":             zog.String().Optional(),
		"RedisPassword":        zog.String().Optional(),
		"RedisDB":              zog.Int().GTE(0).LTE(15).Optional(),
		"RedisKeyPrefix":       zog.String().Min(1).Optional(),
		"RedisInsecureTLS":     zog.Bool().Optional(),
	})
}

// Schema implements the ConfigSchemaProvider interface for Zog validation.
func (c PrewarmConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"Enabled":       zog.Bool().Optional(),
		"MaxConc":       zog.Int().GT(0).Optional(),
		"RetryAttempts": zog.Uint().GT(0).Optional(),
		"RetryDelay":    zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool { return *valPtr > 0 }),
		"DAGBatchConc":  zog.Int().GT(0).Optional(),
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
		"Enabled": zog.Bool().Optional(),
		"Rate":    zog.Float64().GT(0).LTE(1000).Optional(),
		"Burst":   zog.Int().GT(0).Optional(),
		"ExpiresIn": zog.CustomFunc(func(valPtr *time.Duration, ctx zog.Ctx) bool {
			return *valPtr > 0 && *valPtr <= 24*time.Hour
		}, zog.Message("expires_in must be between 0 and 24 hours")),
	})
}

type OTLPConfig struct {
	Endpoint  string `config:"endpoint"`
	AuthToken string `config:"auth_token"`
	Insecure  bool   `config:"insecure"`
}

func (o OTLPConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"Endpoint":  zog.String(),
		"AuthToken": zog.String(),
		"Insecure":  zog.Bool(),
	}).TestFunc(func(data any, ctx zog.Ctx) bool {
		c, ok := data.(*OTLPConfig)
		if !ok {
			return true
		}

		if c.Endpoint != "" {
			endpoint := c.Endpoint
			if _, _, err := net.SplitHostPort(endpoint); err != nil {
				endpoint = endpoint + ":4317"
				if _, _, err2 := net.SplitHostPort(endpoint); err2 != nil {
					ctx.AddIssue(ctx.Issue().SetMessage("endpoint format is invalid"))
					return false
				}
				c.Endpoint = endpoint
			}
		}

		return true
	})
}

func (o OTLPConfig) Defaults() map[string]any {
	return map[string]any{
		"Endpoint":  "",
		"AuthToken": "",
		"Insecure":  false,
	}
}

type ObservabilityConfig struct {
	Enabled     bool              `config:"enabled"`
	ServiceName string            `config:"service_name"`
	OTLP        OTLPConfig        `config:"otlp"`
	Tracing     TracingConfig     `config:"tracing"`
	Logging     OTelLoggingConfig `config:"logging"`
	Metrics     MetricsConfig     `config:"metrics"`
	Pprof       PprofConfig       `config:"pprof"`
}

// PprofConfig holds configuration for the Go pprof debugging endpoints.
type PprofConfig struct {
	Enabled bool   `config:"enabled"`
	Path    string `config:"path"`
}

type TracingConfig struct {
	Enabled     bool    `config:"enabled"`
	SampleRatio float64 `config:"sample_ratio"`
}

type OTelLoggingConfig struct {
	Enabled bool   `config:"enabled"`
	Level   string `config:"level"`
}

type MetricsConfig struct {
	Enabled           bool   `config:"enabled"`
	Path              string `config:"path"`
	BasicAuthPassword string `config:"basic_auth_password"`
}

func (o ObservabilityConfig) Defaults() map[string]any {
	return map[string]any{
		"Enabled":     false,
		"ServiceName": "ipfs-website-gateway",
	}
}

func (o ObservabilityConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"Enabled":     zog.Bool().Optional(),
		"ServiceName": zog.String().Optional(),
		"OTLP":        o.OTLP.Schema(),
		"Pprof":       o.Pprof.Schema(),
	}).TestFunc(func(data any, ctx zog.Ctx) bool {
		c, ok := data.(*ObservabilityConfig)
		if !ok {
			return true
		}

		if c.Enabled && c.OTLP.Endpoint == "" {
			ctx.AddIssue(ctx.Issue().SetMessage("otlp.endpoint is required when observability is enabled"))
			return false
		}

		return true
	})
}

func (o ObservabilityConfig) IsTracingEnabled() bool { return o.Enabled && o.Tracing.Enabled }
func (o ObservabilityConfig) IsLoggingEnabled() bool { return o.Enabled && o.Logging.Enabled }
func (o ObservabilityConfig) IsMetricsEnabled() bool { return o.Enabled && o.Metrics.Enabled }
func (m MetricsConfig) IsBasicAuthEnabled() bool     { return m.BasicAuthPassword != "" }

func (o ObservabilityConfig) IsPprofEnabled() bool { return o.Pprof.Enabled }

func (p PprofConfig) Defaults() map[string]any {
	return map[string]any{
		"Enabled": false,
		"Path":    "/debug/pprof",
	}
}

func (p PprofConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"Enabled": zog.Bool().Optional(),
		"Path":    zog.String().Min(1).Optional(),
	})
}

func (t TracingConfig) Defaults() map[string]any {
	return map[string]any{
		"Enabled":     true,
		"SampleRatio": 1.0,
	}
}

func (t TracingConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"Enabled":     zog.Bool().Optional(),
		"SampleRatio": zog.Float64().GTE(0.0).LTE(1.0).Optional(),
	})
}

func (l OTelLoggingConfig) Defaults() map[string]any {
	return map[string]any{
		"Enabled": true,
		"Level":   "info",
	}
}

func (l OTelLoggingConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"Enabled": zog.Bool().Optional(),
		"Level":   zog.String().OneOf([]string{"debug", "info", "warn", "error"}).Optional(),
	})
}

func (m MetricsConfig) Defaults() map[string]any {
	return map[string]any{
		"Enabled":           true,
		"Path":              "/metrics",
		"BasicAuthPassword": "",
	}
}

func (m MetricsConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"Enabled":           zog.Bool().Optional(),
		"Path":              zog.String().Min(1).Optional(),
		"BasicAuthPassword": zog.String().Optional(),
	})
}
