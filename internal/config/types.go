package config

import (
	"path/filepath"
)

// Config represents the complete configuration for dovetail
type Config struct {
	General     GeneralConfig     `toml:"general"`
	Performance PerformanceConfig `toml:"performance"`
	Exclusions  ExclusionsConfig  `toml:"exclusions"`
	Gitignore   GitignoreConfig   `toml:"gitignore"`
}

// GeneralConfig contains general application settings
type GeneralConfig struct {
	Verbose           int  `toml:"verbose"`            // Verbosity level (0-3)
	NoColor           bool `toml:"no_color"`           // Disable colored output
	FollowSymlinks    bool `toml:"follow_symlinks"`    // Follow symbolic links
	IgnorePermissions bool `toml:"ignore_permissions"` // Ignore file permission differences
}

// PerformanceConfig contains performance-related settings
type PerformanceConfig struct {
	ParallelWorkers int   `toml:"parallel_workers"` // Number of parallel workers (0 = auto)
	MaxFileSize     int64 `toml:"max_file_size"`    // Maximum file size to hash in bytes (0 = no limit)
}

// ExclusionsConfig contains file/directory exclusion patterns
type ExclusionsConfig struct {
	Names      []string `toml:"names"`      // File/directory names or glob patterns to exclude
	Paths      []string `toml:"paths"`      // Relative paths to exclude
	Extensions []string `toml:"extensions"` // File extensions to exclude (without dot)
}

// GitignoreConfig contains gitignore-related settings
type GitignoreConfig struct {
	Enabled        bool `toml:"enabled"`          // Whether to read and apply .gitignore rules
	CheckBothSides bool `toml:"check_both_sides"` // Look for .gitignore in both directories
}

// NewDefaultConfig creates a new configuration with sensible defaults
func NewDefaultConfig() *Config {
	return &Config{
		General: GeneralConfig{
			Verbose:           0, // Quiet by default
			NoColor:           false,
			FollowSymlinks:    false,
			IgnorePermissions: false,
		},
		Performance: PerformanceConfig{
			ParallelWorkers: 0,       // Auto-detect CPU cores
			MaxFileSize:     1048576, // 1MB default
		},
		Exclusions: ExclusionsConfig{
			Names:      []string{},
			Paths:      []string{},
			Extensions: []string{},
		},
		Gitignore: GitignoreConfig{
			Enabled:        false,
			CheckBothSides: true,
		},
	}
}

// MergeWith merges another config into this one, with the other config taking precedence
func (c *Config) MergeWith(other *Config) {
	if other == nil {
		return
	}

	// Merge general settings (only if explicitly set)
	if other.General.Verbose != 0 {
		c.General.Verbose = other.General.Verbose
	}
	// NoColor is a boolean, so we need special handling
	if other.General.NoColor {
		c.General.NoColor = other.General.NoColor
	}
	if other.General.FollowSymlinks {
		c.General.FollowSymlinks = other.General.FollowSymlinks
	}
	if other.General.IgnorePermissions {
		c.General.IgnorePermissions = other.General.IgnorePermissions
	}

	// Merge performance settings
	if other.Performance.ParallelWorkers != 0 {
		c.Performance.ParallelWorkers = other.Performance.ParallelWorkers
	}
	if other.Performance.MaxFileSize != 0 {
		c.Performance.MaxFileSize = other.Performance.MaxFileSize
	}

	// Merge exclusions (append, don't replace)
	c.Exclusions.Names = append(c.Exclusions.Names, other.Exclusions.Names...)
	c.Exclusions.Paths = append(c.Exclusions.Paths, other.Exclusions.Paths...)
	c.Exclusions.Extensions = append(c.Exclusions.Extensions, other.Exclusions.Extensions...)

	// Merge gitignore settings
	if other.Gitignore.Enabled {
		c.Gitignore.Enabled = other.Gitignore.Enabled
	}
	if !other.Gitignore.CheckBothSides {
		c.Gitignore.CheckBothSides = other.Gitignore.CheckBothSides
	}
}

// ToComparisonOptions converts config to comparison options
func (c *Config) ToComparisonOptions() ComparisonOptions {
	return ComparisonOptions{
		ExcludeNames:      c.Exclusions.Names,
		ExcludePaths:      c.Exclusions.Paths,
		ExcludeExtensions: c.Exclusions.Extensions,
		FollowSymlinks:    c.General.FollowSymlinks,
		IgnorePermissions: c.General.IgnorePermissions,
		MaxFileSize:       c.Performance.MaxFileSize,
		ParallelWorkers:   c.Performance.ParallelWorkers,
	}
}

// ComparisonOptions represents options for directory comparison
// This duplicates the type from internal/compare/types.go for now
// TODO: Refactor to use a shared types package
type ComparisonOptions struct {
	ExcludeNames      []string
	ExcludePaths      []string
	ExcludeExtensions []string
	FollowSymlinks    bool
	IgnorePermissions bool
	MaxFileSize       int64
	ParallelWorkers   int
}

// ConfigPath represents a configuration file path and its priority
type ConfigPath struct {
	Path     string
	Priority int // Lower numbers = higher priority
	Source   string
}

// GetConfigSearchPaths returns the paths to search for config files in priority order
func GetConfigSearchPaths(explicitPath string) []ConfigPath {
	var paths []ConfigPath

	// 1. Explicit path from --config flag (highest priority)
	if explicitPath != "" {
		paths = append(paths, ConfigPath{
			Path:     explicitPath,
			Priority: 1,
			Source:   "command line --config",
		})
	}

	// 2. Current directory .dovetail.toml
	if cwd, err := filepath.Abs("."); err == nil {
		paths = append(paths, ConfigPath{
			Path:     filepath.Join(cwd, ".dovetail.toml"),
			Priority: 2,
			Source:   "current directory",
		})
	}

	// 3. Walk up parent directories looking for .dovetail.toml
	if cwd, err := filepath.Abs("."); err == nil {
		dir := cwd
		priority := 3
		for {
			parent := filepath.Dir(dir)
			if parent == dir {
				break // Reached root
			}
			dir = parent
			paths = append(paths, ConfigPath{
				Path:     filepath.Join(dir, ".dovetail.toml"),
				Priority: priority,
				Source:   "parent directory",
			})
			priority++
		}
	}

	// 4. Home directory ~/.dovetail.toml (lowest priority)
	if homeDir, err := filepath.Abs("~"); err == nil {
		paths = append(paths, ConfigPath{
			Path:     filepath.Join(homeDir, ".dovetail.toml"),
			Priority: 100,
			Source:   "home directory",
		})
	}

	return paths
}
