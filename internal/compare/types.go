package compare

import (
	"time"
)

// FileStatus represents the comparison status of a file/directory
type FileStatus int

const (
	StatusIdentical FileStatus = iota
	StatusModified
	StatusOnlyLeft
	StatusOnlyRight
)

func (s FileStatus) String() string {
	switch s {
	case StatusIdentical:
		return "IDENTICAL"
	case StatusModified:
		return "MODIFIED"
	case StatusOnlyLeft:
		return "ONLY_IN_LEFT"
	case StatusOnlyRight:
		return "ONLY_IN_RIGHT"
	default:
		return "UNKNOWN"
	}
}

// FileInfo contains information about a file for comparison
type FileInfo struct {
	Path        string    // Relative path from root
	Size        int64     // File size in bytes
	ModTime     time.Time // Modification time
	IsDir       bool      // Whether this is a directory
	Hash        string    // SHA-256 hash for files (empty for directories)
	Permissions string    // File permissions (for display/debugging)
}

// ComparisonResult represents the result of comparing a single file/directory
type ComparisonResult struct {
	RelativePath string     // Path relative to comparison root
	Status       FileStatus // Comparison status
	LeftInfo     *FileInfo  // Info from left directory (nil if not present)
	RightInfo    *FileInfo  // Info from right directory (nil if not present)
}

// ComparisonOptions contains options for directory comparison
type ComparisonOptions struct {
	// Filtering options
	ExcludeNames      []string // File/directory names or glob patterns to exclude
	ExcludePaths      []string // Relative paths to exclude
	ExcludeExtensions []string // File extensions to exclude (without dot)

	// Comparison options
	IgnorePermissions bool // Whether to ignore permission differences
	FollowSymlinks    bool // Whether to follow symbolic links

	// Performance options
	MaxFileSize     int64 // Maximum file size to hash (0 = no limit)
	ParallelWorkers int   // Number of parallel workers for hashing (0 = auto)
}

// Engine represents the directory comparison engine
type Engine struct {
	options ComparisonOptions
	filter  *Filter
}

// ComparisonSummary contains statistics about the comparison
type ComparisonSummary struct {
	TotalFiles        int
	IdenticalFiles    int
	ModifiedFiles     int
	OnlyLeftFiles     int
	OnlyRightFiles    int
	TotalDirs         int
	IdenticalDirs     int
	OnlyLeftDirs      int
	OnlyRightDirs     int
	ErrorsEncountered []string
}
