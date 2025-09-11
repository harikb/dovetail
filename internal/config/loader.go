package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Loader handles loading and parsing configuration files
type Loader struct {
	verboseLevel int
}

// NewLoader creates a new configuration loader
func NewLoader(verboseLevel int) *Loader {
	return &Loader{
		verboseLevel: verboseLevel,
	}
}

// Load loads configuration from all available sources and merges them
func (l *Loader) Load(explicitConfigPath string) (*Config, error) {
	config := NewDefaultConfig()
	searchPaths := GetConfigSearchPaths(explicitConfigPath)

	var loadedConfigs []string

	for _, configPath := range searchPaths {
		if _, err := os.Stat(configPath.Path); err == nil {
			// File exists, try to load it
			fileConfig, err := l.loadFromFile(configPath.Path)
			if err != nil {
				if configPath.Priority == 1 {
					// Explicit config file failed to load - this is an error
					return nil, fmt.Errorf("failed to load config file %s (from --config): %w", configPath.Path, err)
				}
				// Non-explicit config file failed - log if verbose but continue
				if l.verboseLevel >= 2 {
					fmt.Fprintf(os.Stderr, "Warning: Failed to load config from %s: %v\n", configPath.Path, err)
				}
				continue
			}

			// Merge this config into the base config
			config.MergeWith(fileConfig)
			loadedConfigs = append(loadedConfigs, fmt.Sprintf("%s (%s)", configPath.Path, configPath.Source))

			if l.verboseLevel >= 2 {
				fmt.Fprintf(os.Stderr, "Loaded config from: %s\n", configPath.Path)
			}

			// If explicit path was specified and found, stop here
			if configPath.Priority == 1 {
				break
			}
		}
	}

	if l.verboseLevel >= 1 && len(loadedConfigs) > 0 {
		fmt.Fprintf(os.Stderr, "Configuration loaded from: %s\n", loadedConfigs)
	}

	return config, nil
}

// loadFromFile loads a single configuration file
func (l *Loader) loadFromFile(path string) (*Config, error) {
	var config Config

	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse TOML
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse TOML config: %w", err)
	}

	// Validate the configuration
	if err := l.validateConfig(&config, path); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

// validateConfig validates a configuration for common issues
func (l *Loader) validateConfig(config *Config, path string) error {
	// Validate verbosity level
	if config.General.Verbose < 0 || config.General.Verbose > 3 {
		return fmt.Errorf("invalid verbose level %d in %s: must be 0-3", config.General.Verbose, path)
	}

	// Validate parallel workers
	if config.Performance.ParallelWorkers < 0 {
		return fmt.Errorf("invalid parallel_workers %d in %s: must be >= 0", config.Performance.ParallelWorkers, path)
	}

	// Validate max file size
	if config.Performance.MaxFileSize < 0 {
		return fmt.Errorf("invalid max_file_size %d in %s: must be >= 0", config.Performance.MaxFileSize, path)
	}

	// Validate exclusion paths end with / if they're meant to be directories
	for i, path := range config.Exclusions.Paths {
		// Auto-correct paths that should end with / (common mistake)
		if !strings.HasSuffix(path, "/") && !strings.Contains(path, ".") {
			config.Exclusions.Paths[i] = path + "/"
			if l.verboseLevel >= 2 {
				fmt.Fprintf(os.Stderr, "Auto-corrected exclusion path: '%s' -> '%s'\n", path, config.Exclusions.Paths[i])
			}
		}
	}

	return nil
}

// ApplyCLIOverrides applies CLI flags to override configuration values
func ApplyCLIOverrides(config *Config, cliConfig CLIConfig) {
	// Override verbosity if set via CLI
	if cliConfig.VerboseLevel > 0 {
		config.General.Verbose = cliConfig.VerboseLevel
	}

	// Override no-color if set via CLI
	if cliConfig.NoColor {
		config.General.NoColor = cliConfig.NoColor
	}

	// Append CLI exclusions to config exclusions
	config.Exclusions.Names = append(config.Exclusions.Names, cliConfig.ExcludeNames...)
	config.Exclusions.Paths = append(config.Exclusions.Paths, cliConfig.ExcludePaths...)
	config.Exclusions.Extensions = append(config.Exclusions.Extensions, cliConfig.ExcludeExtensions...)

	// Override gitignore settings if set via CLI
	if cliConfig.UseGitignore {
		config.Gitignore.Enabled = true
	}
}

// CLIConfig represents configuration values from CLI flags
type CLIConfig struct {
	VerboseLevel      int
	NoColor           bool
	ExcludeNames      []string
	ExcludePaths      []string
	ExcludeExtensions []string
	UseGitignore      bool
}
