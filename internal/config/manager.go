package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sync"

	"go.lumeweb.com/configmanager"
	"go.lumeweb.com/configmanager/source"
	"go.uber.org/zap"
)

// FileSystem provides filesystem operations for config management.
type FileSystem interface {
	Stat(name string) (os.FileInfo, error)
	MkdirAll(path string, perm os.FileMode) error
	OpenFile(name string, flag int, perm os.FileMode) (*os.File, error)
	Create(name string) (*os.File, error)
}

// osFS provides an implementation of FileSystem using the standard os package.
type osFS struct{}

// OSFS is the default filesystem implementation.
var OSFS FileSystem = osFS{}

func (osFS) Stat(name string) (os.FileInfo, error)        { return os.Stat(name) }
func (osFS) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (osFS) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(name, flag, perm)
}
func (osFS) Create(name string) (*os.File, error) { return os.Create(name) }

// findConfigFileOptions defines options for finding configuration files.
type findConfigFileOptions struct {
	Paths           []string   // Search paths, defaults to DefaultConfigPaths if empty
	CreateIfMissing bool       // Create a default config if not found
	CheckWritable   bool       // Check if existing files are writable
	FS              FileSystem // Filesystem interface for operations
}

// findConfigFile searches for a config file in specified locations with robust handling.
func findConfigFile(options findConfigFileOptions) (string, error) {
	paths := options.Paths
	if len(paths) == 0 {
		paths = DefaultConfigPaths()
	}

	for _, _path := range paths {
		// Expand environment variables in path
		expandedPath := os.ExpandEnv(_path)

		// Append CoreConfigFile for config paths
		// All paths (defaults and custom) must be directories
		expandedPath = path.Join(expandedPath, CoreConfigFile)

		_, err := options.FS.Stat(expandedPath)
		if err == nil {
			// File exists
			if options.CheckWritable {
				file, err := options.FS.OpenFile(expandedPath, os.O_WRONLY, 0644)
				if err != nil {
					continue // Skip unwritable files
				}
				err = file.Close()
				if err != nil {
					return "", err
				}
			}
			return expandedPath, nil
		}

		if os.IsNotExist(err) && options.CreateIfMissing {
			// File doesn't exist and we should create it
			if err := CreateDefaultConfig(expandedPath, options.FS); err != nil {
				return "", fmt.Errorf("failed to create default config at %s: %w", expandedPath, err)
			}
			return expandedPath, nil
		}
	}

	return "", fmt.Errorf("no valid config file found in paths: %v", paths)
}

// CreateDefaultConfig creates an empty config file at the specified path.
func CreateDefaultConfig(path string, fs FileSystem) error {
	// Create parent directories if they don't exist
	if err := fs.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Create empty file
	file, err := fs.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	return file.Close()
}

// ManagerDefault wraps configmanager.Manager with gateway-specific functionality.
type ManagerDefault struct {
	configmanager.Manager
	logger      *zap.Logger
	configFile  string
	configDir   string
	configPaths []string
	initialized bool
	fs          FileSystem
	lock        sync.RWMutex
}

// ManagerConfig holds dependencies and options required to construct a Manager.
type ManagerConfig struct {
	ConfigManager configmanager.Manager
	Logger        *zap.Logger
	ConfigPaths   []string
	FS            FileSystem
}

// newManagerConfig creates a new ManagerConfig with defaults.
func newManagerConfig() *ManagerConfig {
	return &ManagerConfig{}
}

// ManagerOption configures a Manager.
type ManagerOption func(*ManagerConfig)

// WithConfigManager sets the ConfigManager to the provided configmanager.Manager.
func WithConfigManager(cm configmanager.Manager) ManagerOption {
	return func(c *ManagerConfig) {
		c.ConfigManager = cm
	}
}

// WithLogger sets the Logger to the provided zap.Logger.
func WithLogger(logger *zap.Logger) ManagerOption {
	return func(c *ManagerConfig) {
		c.Logger = logger
	}
}

// WithConfigPaths sets the ConfigPaths to the provided slice of configuration file paths.
func WithConfigPaths(paths []string) ManagerOption {
	return func(c *ManagerConfig) {
		c.ConfigPaths = paths
	}
}

// withFileSystem sets the FileSystem to the provided FileSystem implementation.


// NewManager creates a new Manager instance with the provided options.
func NewManager(opts ...ManagerOption) (*ManagerDefault, error) {
	// Create config with defaults and apply options
	config := newManagerConfig()
	for _, opt := range opts {
		opt(config)
	}

	// Use provided config manager or create default one
	var cm configmanager.Manager
	if config.ConfigManager != nil {
		cm = config.ConfigManager
	} else {
		var err error
		cm, err = createDefaultConfigManager()
		if err != nil {
			return nil, err
		}
	}

	// Initialize ManagerDefault with config manager
	m := &ManagerDefault{
		Manager: cm,
		fs:      OSFS,
		lock:    sync.RWMutex{},
	}

	// Apply additional configuration
	if config.Logger != nil {
		m.logger = config.Logger
	}
	if config.ConfigPaths != nil {
		m.configPaths = config.ConfigPaths
	}
	if config.FS != nil {
		m.fs = config.FS
	}

	// Determine config file and directory
	// Get paths - use custom paths if set, otherwise check env var, then fall back to defaults
	var paths []string
	if len(m.configPaths) > 0 {
		paths = m.configPaths
	} else if customPaths := os.Getenv(ENV_CONFIG_PATHS); customPaths != "" {
		paths = filepath.SplitList(customPaths)
	} else {
		paths = DefaultConfigPaths()
	}

	configFile, err := findConfigFile(findConfigFileOptions{
		Paths:           paths,
		CreateIfMissing: true,
		CheckWritable:   true,
		FS:              m.fs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find or create config file: %w", err)
	}

	// Set final config paths
	m.configFile = configFile
	m.configDir = filepath.Dir(configFile)

	// Register sources in priority order (last loaded wins):
	// 1. Default source (lowest priority)
	// 2. File source (medium priority)
	// 3. Env source (highest priority)
	
	// Register default config source with defaults from Config struct
	defaultSource := source.NewDefaultConfigSource(m, source.WithDefaultSourceGlobal())
	m.Manager.RegisterSource(defaultSource) //nolint:staticcheck // QF1008: explicit selector needed for nested resolution

	// Register the file source
	fileSource := source.NewFileSource(configFile)
	m.Manager.RegisterSource(fileSource)      //nolint:staticcheck // QF1008: explicit selector needed for nested resolution
	m.Manager.RegisterNamespace("", fileSource) //nolint:staticcheck // QF1008: explicit selector needed for nested resolution

	// Register env source (must be last to override file and defaults)
	envSource := source.NewEnvConfigSource(ENV_PREFIX, ENV_SEPARATOR, source.WithEnvSourceGlobal())
	m.Manager.RegisterSource(envSource) //nolint:staticcheck // QF1008: explicit selector needed for nested resolution

	// Register nested config structs first (for defaults processing)
	err = m.Manager.RegisterStruct("server", ServerConfig{}) //nolint:staticcheck // QF1008
	if err != nil {
		return nil, fmt.Errorf("failed to register server config: %w", err)
	}
	err = m.Manager.RegisterStruct("api", APIConfig{}) //nolint:staticcheck // QF1008
	if err != nil {
		return nil, fmt.Errorf("failed to register api config: %w", err)
	}
	err = m.Manager.RegisterStruct("ipfs", IPFSConfig{}) //nolint:staticcheck // QF1008
	if err != nil {
		return nil, fmt.Errorf("failed to register ipfs config: %w", err)
	}
	err = m.Manager.RegisterStruct("cache", CacheConfig{}) //nolint:staticcheck // QF1008
	if err != nil {
		return nil, fmt.Errorf("failed to register cache config: %w", err)
	}
	err = m.Manager.RegisterStruct("logging", LoggingConfig{}) //nolint:staticcheck // QF1008
	if err != nil {
		return nil, fmt.Errorf("failed to register logging config: %w", err)
	}
	err = m.Manager.RegisterStruct("rate_limit", RateLimitConfig{}) //nolint:staticcheck // QF1008
	if err != nil {
		return nil, fmt.Errorf("failed to register rate_limit config: %w", err)
	}
	err = m.Manager.RegisterStruct("prewarm", PrewarmConfig{}) //nolint:staticcheck // QF1008
	if err != nil {
		return nil, fmt.Errorf("failed to register prewarm config: %w", err)
	}

	// Register parent Config struct (must be after nested structs)
	err = m.Manager.RegisterStruct("", Config{}) //nolint:staticcheck // QF1008
	if err != nil {
		return nil, fmt.Errorf("failed to register config: %w", err)
	}

	return m, nil
}

// createDefaultConfigManager creates a default config manager with basic setup.
func createDefaultConfigManager() (configmanager.Manager, error) {
	cm, err := configmanager.NewConfigManager(
		[]source.ConfigSource{}, // Don't register env source here, will register later
		configmanager.WithLogger(zap.NewNop()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create configmanager: %w", err)
	}

	return cm, nil
}

// Init initializes the manager by loading configuration from all sources.
func (m *ManagerDefault) Init() error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.initialized {
		return nil
	}

	if err := m.Load(); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	m.initialized = true
	return nil
}

// SetLogger sets the logger for the manager.
func (m *ManagerDefault) SetLogger(logger *zap.Logger) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.logger = logger
}

// Config returns the current configuration as a Config struct.
func (m *ManagerDefault) Config() *Config {
	// Use Root to decode the entire config struct
	decoded, err := m.Root(nil)
	if err != nil {
		if m.logger != nil {
			m.logger.Error("failed to get config", zap.Error(err))
		}
		return &Config{}
	}
	
	if decoded != nil {
		if cfg, ok := decoded.(*Config); ok {
			return cfg
		}
	}
	
	return &Config{}
}
