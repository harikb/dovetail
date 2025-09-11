package util

import (
	"fmt"
	"os"
)

// VerboseCallback is a callback function for progress updates
type VerboseCallback func(level int, format string, args ...interface{})

// DefaultVerboseCallback is the default implementation that prints to stderr
func DefaultVerboseCallback(level int, format string, args ...interface{}) {
	// Print to stderr so it doesn't interfere with output redirection
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// VerbosePrintf prints a message at the specified verbosity level
func VerbosePrintf(currentLevel, requiredLevel int, format string, args ...interface{}) {
	if currentLevel >= requiredLevel {
		DefaultVerboseCallback(requiredLevel, format, args...)
	}
}

// ProgressReporter helps with progress reporting
type ProgressReporter struct {
	verboseLevel    int
	currentCount    int
	totalCount      int
	lastReportCount int
	reportInterval  int
}

// NewProgressReporter creates a new progress reporter
func NewProgressReporter(verboseLevel, totalCount int) *ProgressReporter {
	reportInterval := 100 // Report every 100 items by default
	if verboseLevel >= 3 {
		reportInterval = 1 // Report every item in debug mode
	} else if verboseLevel >= 2 {
		reportInterval = 10 // Report every 10 items in detailed mode
	}

	return &ProgressReporter{
		verboseLevel:   verboseLevel,
		totalCount:     totalCount,
		reportInterval: reportInterval,
	}
}

// Report increments the counter and reports progress if needed
func (pr *ProgressReporter) Report(format string, args ...interface{}) {
	pr.currentCount++

	// Always report in debug mode (level 3+)
	if pr.verboseLevel >= 3 {
		VerbosePrintf(pr.verboseLevel, 3, "[%d/%d] "+format, append([]interface{}{pr.currentCount, pr.totalCount}, args...)...)
		return
	}

	// Report at intervals for lower verbosity levels
	if pr.currentCount%pr.reportInterval == 0 || pr.currentCount == pr.totalCount {
		if pr.verboseLevel >= 2 {
			VerbosePrintf(pr.verboseLevel, 2, "[%d/%d] "+format, append([]interface{}{pr.currentCount, pr.totalCount}, args...)...)
		} else if pr.verboseLevel >= 1 && (pr.currentCount%1000 == 0 || pr.currentCount == pr.totalCount) {
			VerbosePrintf(pr.verboseLevel, 1, "Processed %d/%d files...", pr.currentCount, pr.totalCount)
		}
	}
}

// SetTotal updates the total count (useful when the total is not known initially)
func (pr *ProgressReporter) SetTotal(total int) {
	pr.totalCount = total
}

// Finish reports completion
func (pr *ProgressReporter) Finish() {
	if pr.verboseLevel >= 1 {
		VerbosePrintf(pr.verboseLevel, 1, "Completed processing %d files", pr.currentCount)
	}
}
