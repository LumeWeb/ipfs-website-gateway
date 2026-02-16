package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestConfigStructs(t *testing.T) {
	// Verify Config struct can be instantiated
	cfg := Config{
		Server: ServerConfig{
			Port:           8080,
			TrustedProxies: []string{"127.0.0.1"},
		},
		API: APIConfig{
			URL:     "https://api.example.com",
			Secret:  "test-secret",
			Timeout: 30 * time.Second,
		},
		IPFS: IPFSConfig{
			SeedPeer: "ipfs.pinner.xyz",
		},
		Cache: CacheConfig{
			StatusCacheTTL:       5 * time.Minute,
			StatusCacheLRUSize:   1000,
			ContentCachePath:     "/tmp/cache",
			ContentCacheMaxBytes: 1024 * 1024 * 1024, // 1GB
			ContentCacheLRUSize:  100000,
		},
		Logging: LoggingConfig{
			Level: "info",
		},
	}

	// Verify Server settings
	if cfg.Server.Port != 8080 {
		t.Errorf("Expected Port to be 8080, got %d", cfg.Server.Port)
	}
	if len(cfg.Server.TrustedProxies) != 1 || cfg.Server.TrustedProxies[0] != "127.0.0.1" {
		t.Errorf("Expected TrustedProxies to contain '127.0.0.1'")
	}

	// Verify API settings
	if cfg.API.URL != "https://api.example.com" {
		t.Errorf("Expected API URL to be 'https://api.example.com', got '%s'", cfg.API.URL)
	}
	if cfg.API.Secret != "test-secret" {
		t.Errorf("Expected API Secret to be 'test-secret', got '%s'", cfg.API.Secret)
	}
	if cfg.API.Timeout != 30*time.Second {
		t.Errorf("Expected API Timeout to be 30s, got %v", cfg.API.Timeout)
	}

	// Verify IPFS settings
	if cfg.IPFS.SeedPeer != "ipfs.pinner.xyz" {
		t.Errorf("Expected IPFS SeedPeer to be 'ipfs.pinner.xyz', got '%s'", cfg.IPFS.SeedPeer)
	}

	// Verify Cache settings
	if cfg.Cache.StatusCacheTTL != 5*time.Minute {
		t.Errorf("Expected StatusCacheTTL to be 5m, got %v", cfg.Cache.StatusCacheTTL)
	}
	if cfg.Cache.StatusCacheLRUSize != 1000 {
		t.Errorf("Expected StatusCacheLRUSize to be 1000, got %d", cfg.Cache.StatusCacheLRUSize)
	}
	if cfg.Cache.ContentCachePath != "/tmp/cache" {
		t.Errorf("Expected ContentCachePath to be '/tmp/cache', got '%s'", cfg.Cache.ContentCachePath)
	}
	if cfg.Cache.ContentCacheMaxBytes != 1024*1024*1024 {
		t.Errorf("Expected ContentCacheMaxBytes to be 1GB, got %d", cfg.Cache.ContentCacheMaxBytes)
	}
	if cfg.Cache.ContentCacheLRUSize != 100000 {
		t.Errorf("Expected ContentCacheLRUSize to be 100000, got %d", cfg.Cache.ContentCacheLRUSize)
	}

	// Verify Logging settings
	if cfg.Logging.Level != "info" {
		t.Errorf("Expected Logging Level to be 'info', got '%s'", cfg.Logging.Level)
	}
}

func TestDefaultValues(t *testing.T) {
	// Test zero values are safe
	cfg := Config{}

	if cfg.Server.Port != 0 {
		t.Errorf("Expected default Port to be 0, got %d", cfg.Server.Port)
	}
	if cfg.Logging.Level != "" {
		t.Errorf("Expected default Logging Level to be empty string, got '%s'", cfg.Logging.Level)
	}
}

func TestDefaultsInterface(t *testing.T) {
	// Test that nested config structs implement Defaults interface correctly

	// Test ServerConfig defaults
	serverCfg := ServerConfig{}
	serverDefaults := serverCfg.Defaults()
	if serverDefaults["Port"] != 8080 {
		t.Errorf("Expected default Server.Port to be 8080, got %v", serverDefaults["Port"])
	}

	// Test APIConfig defaults
	apiCfg := APIConfig{}
	apiDefaults := apiCfg.Defaults()
	if apiDefaults["Timeout"] != 30*time.Second {
		t.Errorf("Expected default API.Timeout to be 30s, got %v", apiDefaults["Timeout"])
	}

	// Test IPFSConfig defaults
	ipfsCfg := IPFSConfig{}
	ipfsDefaults := ipfsCfg.Defaults()
	if ipfsDefaults["SeedPeer"] != "ipfs.pinner.xyz" {
		t.Errorf("Expected default IPFS.SeedPeer to be 'ipfs.pinner.xyz', got %v", ipfsDefaults["SeedPeer"])
	}
	if ipfsDefaults["RepoPath"] != "./data/ipfs" {
		t.Errorf("Expected default IPFS.RepoPath to be './data/ipfs', got %v", ipfsDefaults["RepoPath"])
	}

	// Test CacheConfig defaults
	cacheCfg := CacheConfig{}
	cacheDefaults := cacheCfg.Defaults()
	if cacheDefaults["StatusCacheTTL"] != 5*time.Minute {
		t.Errorf("Expected default Cache.StatusCacheTTL to be 5m, got %v", cacheDefaults["StatusCacheTTL"])
	}
	if cacheDefaults["StatusCacheLRUSize"] != 1000 {
		t.Errorf("Expected default Cache.StatusCacheLRUSize to be 1000, got %v", cacheDefaults["StatusCacheLRUSize"])
	}
	if cacheDefaults["ContentCachePath"] != "/tmp/ipfs-cache" {
		t.Errorf("Expected default Cache.ContentCachePath to be '/tmp/ipfs-cache', got %v", cacheDefaults["ContentCachePath"])
	}
	if cacheDefaults["ContentCacheMaxBytes"] != int64(10)*1024*1024*1024 {
		t.Errorf("Expected default Cache.ContentCacheMaxBytes to be 10GB, got %v", cacheDefaults["ContentCacheMaxBytes"])
	}
	if cacheDefaults["ContentCacheLRUSize"] != 100000 {
		t.Errorf("Expected default Cache.ContentCacheLRUSize to be 100000, got %v", cacheDefaults["ContentCacheLRUSize"])
	}

	// Test LoggingConfig defaults
	loggingCfg := LoggingConfig{}
	loggingDefaults := loggingCfg.Defaults()
	if loggingDefaults["Level"] != "info" {
		t.Errorf("Expected default Logging.Level to be 'info', got %v", loggingDefaults["Level"])
	}
}

func TestManagerWithDefaults(t *testing.T) {
	logger := zap.NewNop()

	// Create manager with default config
	mgr, err := NewManager(WithLogger(logger))
	if err != nil {
		t.Fatalf("NewManager() failed: %v", err)
	}

	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	cfg := mgr.Config()

	// Verify default values are applied
	if cfg.Server.Port != 8080 {
		t.Errorf("Expected default Port to be 8080, got %d", cfg.Server.Port)
	}
	if len(cfg.Server.TrustedProxies) != 0 {
		t.Errorf("Expected default TrustedProxies to be empty, got %v", cfg.Server.TrustedProxies)
	}
	if cfg.API.Timeout != 30*time.Second {
		t.Errorf("Expected default API Timeout to be 30s, got %v", cfg.API.Timeout)
	}
	if cfg.IPFS.SeedPeer != "ipfs.pinner.xyz" {
		t.Errorf("Expected default SeedPeer to be 'ipfs.pinner.xyz', got '%s'", cfg.IPFS.SeedPeer)
	}
	if cfg.Cache.StatusCacheTTL != 5*time.Minute {
		t.Errorf("Expected default StatusCacheTTL to be 5m, got %v", cfg.Cache.StatusCacheTTL)
	}
	if cfg.Cache.StatusCacheLRUSize != 1000 {
		t.Errorf("Expected default StatusCacheLRUSize to be 1000, got %d", cfg.Cache.StatusCacheLRUSize)
	}
	if cfg.Cache.ContentCachePath != "/tmp/ipfs-cache" {
		t.Errorf("Expected default ContentCachePath to be '/tmp/ipfs-cache', got '%s'", cfg.Cache.ContentCachePath)
	}
	if cfg.Cache.ContentCacheMaxBytes != int64(10)*1024*1024*1024 {
		t.Errorf("Expected default ContentCacheMaxBytes to be 10GB, got %d", cfg.Cache.ContentCacheMaxBytes)
	}
	if cfg.Cache.ContentCacheLRUSize != 100000 {
		t.Errorf("Expected default ContentCacheLRUSize to be 100000, got %d", cfg.Cache.ContentCacheLRUSize)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Expected default Logging Level to be 'info', got '%s'", cfg.Logging.Level)
	}
}

func TestManagerWithConfigFile(t *testing.T) {
	logger := zap.NewNop()

	// Create a temporary config file
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	configPath := filepath.Join(configDir, CoreConfigFile)

	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config directory: %v", err)
	}

	configContent := `server:
  port: 9090
  trusted_proxies:
    - 10.0.0.1
    - 10.0.0.2
ipfs:
  seed_peer: custom.seed.peer
logging:
  level: debug
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Create manager with custom config path
	mgr, err := NewManager(WithLogger(logger), WithConfigPaths([]string{configDir}))
	if err != nil {
		t.Fatalf("NewManager() failed: %v", err)
	}

	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	cfg := mgr.Config()

	// Verify config file values override defaults
	if cfg.Server.Port != 9090 {
		t.Errorf("Expected Port to be 9090 from config file, got %d", cfg.Server.Port)
	}
	if len(cfg.Server.TrustedProxies) != 2 {
		t.Errorf("Expected 2 TrustedProxies, got %d", len(cfg.Server.TrustedProxies))
	}
	if cfg.Server.TrustedProxies[0] != "10.0.0.1" {
		t.Errorf("Expected TrustedProxies[0] to be '10.0.0.1', got '%s'", cfg.Server.TrustedProxies[0])
	}
	if cfg.IPFS.SeedPeer != "custom.seed.peer" {
		t.Errorf("Expected SeedPeer to be 'custom.seed.peer' from config file, got '%s'", cfg.IPFS.SeedPeer)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Expected Logging Level to be 'debug' from config file, got '%s'", cfg.Logging.Level)
	}

	// Verify defaults still apply for unspecified values
	if cfg.API.Timeout != 30*time.Second {
		t.Errorf("Expected default API Timeout to be 30s, got %v", cfg.API.Timeout)
	}
}

func TestManagerWithEnvironmentVariables(t *testing.T) {
	logger := zap.NewNop()

	// Set environment variables
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{"Server Port", "GATEWAY__SERVER__PORT", "9999"},
		{"Logging Level", "GATEWAY__LOGGING__LEVEL", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set env var
			t.Setenv(tt.key, tt.value)

			mgr, err := NewManager(WithLogger(logger))
			if err != nil {
				t.Fatalf("NewManager() failed: %v", err)
			}

			if err := mgr.Init(); err != nil {
				t.Fatalf("Init() failed: %v", err)
			}

			cfg := mgr.Config()

			// Verify env var overrides defaults
			if tt.key == "GATEWAY__SERVER__PORT" && cfg.Server.Port != 9999 {
				t.Errorf("Expected Port to be 9999 from env var, got %d", cfg.Server.Port)
			}
			if tt.key == "GATEWAY__LOGGING__LEVEL" && cfg.Logging.Level != "error" {
				t.Errorf("Expected Logging Level to be 'error' from env var, got '%s'", cfg.Logging.Level)
			}
		})
	}
}

func TestManagerEnvOverridesFile(t *testing.T) {
	logger := zap.NewNop()

	// Create a temporary config file
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	configPath := filepath.Join(configDir, CoreConfigFile)

	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config directory: %v", err)
	}

	configContent := `server:
  port: 8080
logging:
  level: info
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Set env vars that should override config file
	t.Setenv("GATEWAY__SERVER__PORT", "7777")
	t.Setenv("GATEWAY__LOGGING__LEVEL", "warn")

	mgr, err := NewManager(WithLogger(logger), WithConfigPaths([]string{configDir}))
	if err != nil {
		t.Fatalf("NewManager() failed: %v", err)
	}

	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	cfg := mgr.Config()

	// Verify env vars override config file values
	if cfg.Server.Port != 7777 {
		t.Errorf("Expected Port to be 7777 from env var (overriding config file), got %d", cfg.Server.Port)
	}
	if cfg.Logging.Level != "warn" {
		t.Errorf("Expected Logging Level to be 'warn' from env var (overriding config file), got '%s'", cfg.Logging.Level)
	}
}

func TestManagerInitIdempotency(t *testing.T) {
	logger := zap.NewNop()

	mgr, err := NewManager(WithLogger(logger))
	if err != nil {
		t.Fatalf("NewManager() failed: %v", err)
	}

	// First Init
	if err := mgr.Init(); err != nil {
		t.Fatalf("First Init() failed: %v", err)
	}

	cfg1 := mgr.Config()

	// Second Init should be idempotent
	if err := mgr.Init(); err != nil {
		t.Fatalf("Second Init() failed: %v", err)
	}

	cfg2 := mgr.Config()

	// Config should be the same
	if cfg1.Server.Port != cfg2.Server.Port {
		t.Errorf("Config changed after second Init()")
	}
	if cfg1.Logging.Level != cfg2.Logging.Level {
		t.Errorf("Config changed after second Init()")
	}
}

func TestManagerSetLogger(t *testing.T) {
	logger1 := zap.NewNop()
	logger2 := zap.NewNop()

	mgr, err := NewManager(WithLogger(logger1))
	if err != nil {
		t.Fatalf("NewManager() failed: %v", err)
	}

	// Verify initial logger
	if mgr.logger != logger1 {
		t.Error("Initial logger not set correctly")
	}

	// Change logger
	mgr.SetLogger(logger2)

	if mgr.logger != logger2 {
		t.Error("Logger not changed after SetLogger()")
	}
}
