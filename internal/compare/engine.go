package compare

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/harikb/dovetail/internal/util"
)

// NewEngine creates a new comparison engine with the given options
func NewEngine(options ComparisonOptions) *Engine {
	// Set default values
	if options.ParallelWorkers == 0 {
		options.ParallelWorkers = runtime.NumCPU()
	}

	return &Engine{
		options:      options,
		filter:       NewFilter(options),
		verboseLevel: 0, // Default to no verbosity
	}
}

// SetVerboseLevel sets the verbosity level for progress reporting
func (e *Engine) SetVerboseLevel(level int) {
	e.verboseLevel = level
}

// Compare performs a recursive comparison of two directories
func (e *Engine) Compare(leftDir, rightDir string) ([]ComparisonResult, *ComparisonSummary, error) {
	util.VerbosePrintf(e.verboseLevel, 1, "Starting directory comparison...")

	// Collect all files from both directories
	util.VerbosePrintf(e.verboseLevel, 1, "Scanning left directory: %s", leftDir)
	leftFiles, leftPatchFiles, err := e.collectFiles(leftDir, "left")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to scan left directory: %w", err)
	}
	util.VerbosePrintf(e.verboseLevel, 1, "Found %d items in left directory", len(leftFiles))

	util.VerbosePrintf(e.verboseLevel, 1, "Scanning right directory: %s", rightDir)
	rightFiles, rightPatchFiles, err := e.collectFiles(rightDir, "right")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to scan right directory: %w", err)
	}
	util.VerbosePrintf(e.verboseLevel, 1, "Found %d items in right directory", len(rightFiles))

	// Create a set of all unique paths
	allPaths := make(map[string]bool)
	for path := range leftFiles {
		allPaths[path] = true
	}
	for path := range rightFiles {
		allPaths[path] = true
	}

	util.VerbosePrintf(e.verboseLevel, 1, "Comparing %d unique paths using %d workers...", len(allPaths), e.options.ParallelWorkers)

	// Compare files in parallel
	results := make([]ComparisonResult, 0, len(allPaths))
	summary := &ComparisonSummary{}

	// Merge patch files from both directories into summary
	allPatchFiles := append(leftPatchFiles, rightPatchFiles...)
	summary.DetectedPatchFiles = allPatchFiles
	if len(allPatchFiles) > 0 {
		util.VerbosePrintf(e.verboseLevel, 1, "Total patch files detected: %d", len(allPatchFiles))
	}

	resultsChan := make(chan ComparisonResult, len(allPaths))
	errorsChan := make(chan error, len(allPaths))

	// Create progress reporter
	progressReporter := util.NewProgressReporter(e.verboseLevel, len(allPaths))

	// Create worker pool
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, e.options.ParallelWorkers)

	for path := range allPaths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			leftInfo := leftFiles[p]
			rightInfo := rightFiles[p]

			// Report progress
			progressReporter.Report("Comparing: %s", p)

			result, err := e.compareFile(p, leftInfo, rightInfo)
			if err != nil {
				errorsChan <- fmt.Errorf("error comparing %s: %w", p, err)
				return
			}

			resultsChan <- result
		}(path)
	}

	// Close channels when all workers are done
	go func() {
		wg.Wait()
		close(resultsChan)
		close(errorsChan)
	}()

	// Collect results and errors
	for result := range resultsChan {
		results = append(results, result)
		e.updateSummary(summary, result)
	}

	for err := range errorsChan {
		summary.ErrorsEncountered = append(summary.ErrorsEncountered, err.Error())
	}

	progressReporter.Finish()
	util.VerbosePrintf(e.verboseLevel, 1, "Comparison complete!")

	return results, summary, nil
}

// patchFilePattern matches our patch file format: filename.YYYYMMDD_HHMMSS.patch
var patchFilePattern = regexp.MustCompile(`^(.+)\.(\d{8}_\d{6})\.patch$`)

// collectFiles recursively collects all files from a directory
func (e *Engine) collectFiles(dir string, side string) (map[string]*FileInfo, []PatchFileInfo, error) {
	files := make(map[string]*FileInfo)
	var patchFiles []PatchFileInfo
	fileCount := 0

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip files we can't access rather than failing completely
			util.VerbosePrintf(e.verboseLevel, 2, "Skipping inaccessible path (%s): %s", side, path)
			return nil
		}

		// Calculate relative path
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		// Skip root directory
		if relPath == "." {
			return nil
		}

		// Report current directory being scanned
		if info.IsDir() {
			util.VerbosePrintf(e.verboseLevel, 2, "Scanning directory (%s): %s", side, relPath)
		}

		// Check for patch files from previous dovetail runs (BEFORE applying filters)
		if !info.IsDir() {
			filename := filepath.Base(relPath)
			if matches := patchFilePattern.FindStringSubmatch(filename); matches != nil {
				timestamp := matches[2]

				// Calculate base file path (patch file path without the timestamp and .patch extension)
				basePath := strings.TrimSuffix(relPath, "."+timestamp+".patch")

				// Check if base file exists in same directory
				baseFileAbsPath := filepath.Join(dir, basePath)
				if _, err := os.Stat(baseFileAbsPath); err == nil {
					// Base file exists - this is "our" patch file
					patchInfo := PatchFileInfo{
						PatchPath:    relPath,
						BaseFilePath: basePath,
						Timestamp:    timestamp,
						Side:         side,
					}
					patchFiles = append(patchFiles, patchInfo)
					util.VerbosePrintf(e.verboseLevel, 2, "Detected patch file (%s): %s -> %s", side, relPath, basePath)
				} else {
					util.VerbosePrintf(e.verboseLevel, 3, "Ignoring patch file (%s): %s (base file %s not found)", side, relPath, basePath)
				}
			}
		}

		// Apply filters
		if e.filter.ShouldExclude(relPath, info) {
			util.VerbosePrintf(e.verboseLevel, 3, "Excluding (%s): %s", side, relPath)
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Report file being processed
		if !info.IsDir() {
			fileCount++
			if e.verboseLevel >= 3 {
				util.VerbosePrintf(e.verboseLevel, 3, "Found file (%s): %s", side, relPath)
			} else if e.verboseLevel >= 2 && fileCount%100 == 0 {
				util.VerbosePrintf(e.verboseLevel, 2, "Scanned %d files in %s...", fileCount, side)
			}
		}

		// Create FileInfo
		fileInfo := &FileInfo{
			Path:        relPath,
			Size:        info.Size(),
			ModTime:     info.ModTime(),
			IsDir:       info.IsDir(),
			Permissions: info.Mode().String(),
		}

		// Calculate hash for files (not directories)
		if !info.IsDir() {
			util.VerbosePrintf(e.verboseLevel, 3, "Calculating hash (%s): %s", side, relPath)
			hash, err := e.calculateHash(path)
			if err != nil {
				// Log error but don't fail - we'll mark as different
				util.VerbosePrintf(e.verboseLevel, 2, "Hash calculation failed (%s): %s - %v", side, relPath, err)
				fileInfo.Hash = "ERROR_CALCULATING_HASH"
			} else {
				fileInfo.Hash = hash
			}
		}

		files[relPath] = fileInfo
		return nil
	})

	if e.verboseLevel >= 2 {
		util.VerbosePrintf(e.verboseLevel, 2, "Completed scan of %s: %d files found", side, fileCount)
		if len(patchFiles) > 0 {
			util.VerbosePrintf(e.verboseLevel, 1, "Found %d patch files from previous runs (%s)", len(patchFiles), side)
		}
	}

	return files, patchFiles, err
}

// compareFile compares a single file between left and right directories
func (e *Engine) compareFile(relPath string, leftInfo, rightInfo *FileInfo) (ComparisonResult, error) {
	result := ComparisonResult{
		RelativePath: relPath,
		LeftInfo:     leftInfo,
		RightInfo:    rightInfo,
	}

	// Determine status and metadata
	if leftInfo == nil && rightInfo == nil {
		return result, fmt.Errorf("both files are nil for path: %s", relPath)
	} else if leftInfo == nil {
		result.Status = StatusOnlyRight
		result.SizeComparison = SizeNotApplicable
		result.TimeComparison = TimeNotApplicable
		result.ComparisonMethod = ComparisonExistence // File exists on right side only
	} else if rightInfo == nil {
		result.Status = StatusOnlyLeft
		result.SizeComparison = SizeNotApplicable
		result.TimeComparison = TimeNotApplicable
		result.ComparisonMethod = ComparisonExistence // File exists on left side only
	} else {
		// Both exist, compare them and calculate metadata
		
		// Calculate size comparison for files
		if !leftInfo.IsDir && !rightInfo.IsDir {
			if leftInfo.Size == rightInfo.Size {
				result.SizeComparison = SizeEqual
			} else if leftInfo.Size < rightInfo.Size {
				result.SizeComparison = SizeLeftSmaller
			} else {
				result.SizeComparison = SizeLeftBigger
			}
		} else {
			result.SizeComparison = SizeNotApplicable // Directories
		}
		
		// Calculate time comparison
		if leftInfo.ModTime.Equal(rightInfo.ModTime) {
			result.TimeComparison = TimeEqual
		} else if leftInfo.ModTime.Before(rightInfo.ModTime) {
			result.TimeComparison = TimeLeftOlder
		} else {
			result.TimeComparison = TimeLeftNewer
		}

		// Determine comparison method and status
		if leftInfo.IsDir && rightInfo.IsDir {
			// Both are directories - they're identical as directories
			result.Status = StatusIdentical
			result.ComparisonMethod = ComparisonHash // Directory comparison
		} else if leftInfo.IsDir != rightInfo.IsDir {
			// One is directory, one is file - they're different
			result.Status = StatusModified
			result.ComparisonMethod = ComparisonHash // Type mismatch
		} else {
			// Both are files - compare content
			hasHashError := leftInfo.Hash == "ERROR_CALCULATING_HASH" || rightInfo.Hash == "ERROR_CALCULATING_HASH"
			
			if hasHashError {
				result.ComparisonMethod = ComparisonError
				result.Status = StatusModified // Assume different when hash failed
			} else if leftInfo.Size != rightInfo.Size {
				// Size-only comparison (optimization: different sizes = different files)
				result.ComparisonMethod = ComparisonSize
				result.Status = StatusModified
			} else if leftInfo.Hash == rightInfo.Hash {
				// Hash comparison: identical
				result.ComparisonMethod = ComparisonHash
				result.Status = StatusIdentical
			} else {
				// Hash comparison: different
				result.ComparisonMethod = ComparisonHash
				result.Status = StatusModified
			}
		}
	}

	return result, nil
}

// calculateHash calculates SHA-256 hash of a file
func (e *Engine) calculateHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Check file size limit
	if e.options.MaxFileSize > 0 {
		if info, err := file.Stat(); err == nil && info.Size() > e.options.MaxFileSize {
			// For very large files, just use size + modtime as "hash"
			return fmt.Sprintf("LARGE_FILE_%d_%d", info.Size(), info.ModTime().Unix()), nil
		}
	}

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// updateSummary updates the comparison summary with a result
func (e *Engine) updateSummary(summary *ComparisonSummary, result ComparisonResult) {
	if result.LeftInfo != nil && result.LeftInfo.IsDir {
		summary.TotalDirs++
		switch result.Status {
		case StatusIdentical:
			summary.IdenticalDirs++
		case StatusOnlyLeft:
			summary.OnlyLeftDirs++
		}
	} else if result.RightInfo != nil && result.RightInfo.IsDir {
		summary.TotalDirs++
		if result.Status == StatusOnlyRight {
			summary.OnlyRightDirs++
		}
	} else {
		// It's a file
		summary.TotalFiles++
		switch result.Status {
		case StatusIdentical:
			summary.IdenticalFiles++
		case StatusModified:
			summary.ModifiedFiles++
		case StatusOnlyLeft:
			summary.OnlyLeftFiles++
		case StatusOnlyRight:
			summary.OnlyRightFiles++
		}
	}
}
