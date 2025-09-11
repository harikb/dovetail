package compare

import (
	"os"
	"path/filepath"
	"strings"
)

// Filter handles file and directory filtering during comparison
type Filter struct {
	excludeNames      []string
	excludePaths      []string
	excludeExtensions []string
}

// NewFilter creates a new filter with the given options
func NewFilter(options ComparisonOptions) *Filter {
	return &Filter{
		excludeNames:      options.ExcludeNames,
		excludePaths:      options.ExcludePaths,
		excludeExtensions: options.ExcludeExtensions,
	}
}

// ShouldExclude determines if a file or directory should be excluded from comparison
func (f *Filter) ShouldExclude(relPath string, info os.FileInfo) bool {
	// Check by name/glob patterns
	if f.matchesExcludeName(filepath.Base(relPath)) {
		return true
	}

	// Check by relative path
	if f.matchesExcludePath(relPath) {
		return true
	}

	// Check by extension (only for files)
	if !info.IsDir() && f.matchesExcludeExtension(relPath) {
		return true
	}

	return false
}

// matchesExcludeName checks if a filename matches any exclude name patterns
func (f *Filter) matchesExcludeName(name string) bool {
	for _, pattern := range f.excludeNames {
		// Try exact match first
		if name == pattern {
			return true
		}

		// Try glob match
		if matched, err := filepath.Match(pattern, name); err == nil && matched {
			return true
		}

		// Handle common patterns manually if glob fails
		if strings.Contains(pattern, "*") {
			if f.simpleGlobMatch(pattern, name) {
				return true
			}
		}
	}
	return false
}

// matchesExcludePath checks if a relative path matches any exclude path patterns
func (f *Filter) matchesExcludePath(relPath string) bool {
	// Normalize path separators
	normalizedPath := filepath.ToSlash(relPath)

	for _, excludePath := range f.excludePaths {
		normalizedExclude := filepath.ToSlash(excludePath)

		// Exact match
		if normalizedPath == normalizedExclude {
			return true
		}

		// Prefix match (for directory exclusion)
		if strings.HasPrefix(normalizedPath, normalizedExclude+"/") {
			return true
		}

		// Suffix match (for file exclusion in any directory)
		if strings.HasSuffix(normalizedPath, "/"+normalizedExclude) {
			return true
		}
	}
	return false
}

// matchesExcludeExtension checks if a file extension matches any exclude extensions
func (f *Filter) matchesExcludeExtension(relPath string) bool {
	if len(f.excludeExtensions) == 0 {
		return false
	}

	ext := strings.ToLower(filepath.Ext(relPath))
	if ext == "" {
		return false
	}

	// Remove the leading dot
	ext = ext[1:]

	for _, excludeExt := range f.excludeExtensions {
		if strings.ToLower(excludeExt) == ext {
			return true
		}
	}
	return false
}

// simpleGlobMatch provides basic glob matching for common patterns
func (f *Filter) simpleGlobMatch(pattern, name string) bool {
	// Handle simple cases like "*.txt", "test*", "*test*"
	if pattern == "*" {
		return true
	}

	if strings.HasPrefix(pattern, "*.") {
		// Pattern like "*.txt"
		ext := pattern[2:]
		return strings.HasSuffix(strings.ToLower(name), "."+strings.ToLower(ext))
	}

	if strings.HasSuffix(pattern, "*") && !strings.HasPrefix(pattern, "*") {
		// Pattern like "test*"
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix))
	}

	if strings.HasPrefix(pattern, "*") && !strings.HasSuffix(pattern, "*") {
		// Pattern like "*test"
		suffix := pattern[1:]
		return strings.HasSuffix(strings.ToLower(name), strings.ToLower(suffix))
	}

	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		// Pattern like "*test*"
		middle := pattern[1 : len(pattern)-1]
		return strings.Contains(strings.ToLower(name), strings.ToLower(middle))
	}

	return false
}
