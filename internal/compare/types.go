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

// ComparisonMethod represents how the comparison was performed
type ComparisonMethod int

const (
	ComparisonHash      ComparisonMethod = iota // Full hash comparison
	ComparisonSize                              // Size-only comparison
	ComparisonError                             // Error during comparison
	ComparisonExistence                         // File exists on one side only (no content comparison)
)

func (cm ComparisonMethod) String() string {
	switch cm {
	case ComparisonHash:
		return "H"
	case ComparisonSize:
		return "S"
	case ComparisonError:
		return "E"
	case ComparisonExistence:
		return "-"
	default:
		return "?"
	}
}

// SizeComparison represents relative file sizes
type SizeComparison int

const (
	SizeEqual         SizeComparison = iota // Files are same size
	SizeLeftSmaller                         // Left file is smaller
	SizeLeftBigger                          // Left file is bigger
	SizeNotApplicable                       // File exists on one side only
)

func (sc SizeComparison) String() string {
	switch sc {
	case SizeEqual:
		return "L=R"
	case SizeLeftSmaller:
		return "L<R"
	case SizeLeftBigger:
		return "L>R"
	case SizeNotApplicable:
		return "---"
	default:
		return "???"
	}
}

// TimeComparison represents relative modification times
type TimeComparison int

const (
	TimeEqual         TimeComparison = iota // Files have same modification time
	TimeLeftOlder                           // Left file is older
	TimeLeftNewer                           // Left file is newer
	TimeNotApplicable                       // File exists on one side only
)

func (tc TimeComparison) String() string {
	switch tc {
	case TimeEqual:
		return "T="
	case TimeLeftOlder:
		return "T<"
	case TimeLeftNewer:
		return "T>"
	case TimeNotApplicable:
		return "--"
	default:
		return "??"
	}
}

// ComparisonResult represents the result of comparing a single file/directory
type ComparisonResult struct {
	RelativePath     string           // Path relative to comparison root
	Status           FileStatus       // Comparison status
	LeftInfo         *FileInfo        // Info from left directory (nil if not present)
	RightInfo        *FileInfo        // Info from right directory (nil if not present)
	ComparisonMethod ComparisonMethod // How the comparison was performed
	SizeComparison   SizeComparison   // Relative file sizes
	TimeComparison   TimeComparison   // Relative modification times
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
	options      ComparisonOptions
	filter       *Filter
	verboseLevel int
}

// PatchFileInfo represents a detected patch file from previous runs
type PatchFileInfo struct {
	PatchPath    string // Path to the patch file (e.g., "src/main.go.20240924_151230.patch")
	BaseFilePath string // Path to the base file (e.g., "src/main.go")
	Timestamp    string // Extracted timestamp (e.g., "20240924_151230")
	Side         string // Which directory it was found in ("left" or "right")
}

// ComparisonSummary contains statistics about the comparison
type ComparisonSummary struct {
	TotalFiles         int
	IdenticalFiles     int
	ModifiedFiles      int
	OnlyLeftFiles      int
	OnlyRightFiles     int
	TotalDirs          int
	IdenticalDirs      int
	OnlyLeftDirs       int
	OnlyRightDirs      int
	ErrorsEncountered  []string
	DetectedPatchFiles []PatchFileInfo // Patch files from previous dovetail runs
}
