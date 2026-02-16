package config

const (
	// Environment variable prefix for all gateway config
	ENV_PREFIX = "GATEWAY__"

	// Separator for nested config keys in environment variables
	ENV_SEPARATOR = "__"

	// Environment variable for custom config paths
	ENV_CONFIG_PATHS = ENV_PREFIX + "CONFIG_PATHS"

	// File extension for config files
	CONFIG_EXTENSION = ".yaml"

	// Name of the core config file
	CoreConfigFile = "gateway" + CONFIG_EXTENSION
)

// DefaultConfigPaths returns the default search paths for configuration files.
// Paths are searched in order, with the first writable file being used.
func DefaultConfigPaths() []string {
	return []string{
		"/etc/lumeweb/gateway",
		"$HOME/.lumeweb/gateway",
		"./",
	}
}
