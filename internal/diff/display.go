package diff

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/fatih/color"
	"github.com/harikb/dovetail/internal/compare"
	"github.com/harikb/dovetail/internal/util"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// DisplayOptions contains options for diff display
type DisplayOptions struct {
	IgnoreWhitespace bool  // Ignore whitespace differences
	Context          int   // Number of context lines (default 3)
	NoColor          bool  // Disable colored output
	MaxFileSize      int64 // Maximum file size to diff (default 1MB)
}

// Display handles diff display functionality
type Display struct {
	options DisplayOptions
	dmp     *diffmatchpatch.DiffMatchPatch
}

// NewDisplay creates a new diff display handler
func NewDisplay(options DisplayOptions) *Display {
	// Set defaults
	if options.Context == 0 {
		options.Context = 3
	}
	if options.MaxFileSize == 0 {
		options.MaxFileSize = 1024 * 1024 // 1MB
	}

	return &Display{
		options: options,
		dmp:     diffmatchpatch.New(),
	}
}

// ShowDifferences displays differences between directories
func (d *Display) ShowDifferences(
	results []compare.ComparisonResult,
	leftDir, rightDir string,
	writer io.Writer,
) error {
	if d.options.NoColor {
		color.NoColor = true
	}

	// Setup colors
	headerColor := color.New(color.FgCyan, color.Bold)
	pathColor := color.New(color.FgYellow)
	addedColor := color.New(color.FgGreen)
	removedColor := color.New(color.FgRed)
	contextColor := color.New(color.FgWhite)

	modifiedCount := 0
	for _, result := range results {
		if result.Status == compare.StatusModified {
			modifiedCount++
		}
	}

	fmt.Fprintf(writer, "Comparison Results:\n")
	fmt.Fprintf(writer, "==================\n")
	fmt.Fprintf(writer, "Left:  %s\n", leftDir)
	fmt.Fprintf(writer, "Right: %s\n", rightDir)
	fmt.Fprintf(writer, "\n")

	if modifiedCount == 0 {
		fmt.Fprintf(writer, "No modified files found.\n")
		return nil
	}

	fmt.Fprintf(writer, "Modified files (%d):\n\n", modifiedCount)

	for _, result := range results {
		if result.Status != compare.StatusModified {
			continue
		}

		// Skip if both are directories (no content to diff)
		if result.LeftInfo != nil && result.RightInfo != nil &&
			result.LeftInfo.IsDir && result.RightInfo.IsDir {
			continue
		}

		// Skip if one is directory and one is file
		if (result.LeftInfo != nil && result.LeftInfo.IsDir) ||
			(result.RightInfo != nil && result.RightInfo.IsDir) {
			headerColor.Fprintf(writer, "=== %s ===\n", result.RelativePath)
			fmt.Fprintf(writer, "Type mismatch: one is directory, one is file\n\n")
			continue
		}

		// Show file diff
		if err := d.showFileDiff(result, leftDir, rightDir, writer,
			headerColor, pathColor, addedColor, removedColor, contextColor); err != nil {
			fmt.Fprintf(writer, "Error showing diff for %s: %v\n\n", result.RelativePath, err)
		}
	}

	return nil
}

// showFileDiff shows the diff for a single file
func (d *Display) showFileDiff(
	result compare.ComparisonResult,
	leftDir, rightDir string,
	writer io.Writer,
	headerColor, pathColor, addedColor, removedColor, contextColor *color.Color,
) error {
	leftPath := filepath.Join(leftDir, result.RelativePath)
	rightPath := filepath.Join(rightDir, result.RelativePath)

	// Check file sizes
	if result.LeftInfo != nil && result.LeftInfo.Size > d.options.MaxFileSize {
		headerColor.Fprintf(writer, "=== %s ===\n", result.RelativePath)
		fmt.Fprintf(writer, "Left file too large to diff (%s > %s)\n\n",
			util.FormatSize(result.LeftInfo.Size), util.FormatSize(d.options.MaxFileSize))
		return nil
	}
	if result.RightInfo != nil && result.RightInfo.Size > d.options.MaxFileSize {
		headerColor.Fprintf(writer, "=== %s ===\n", result.RelativePath)
		fmt.Fprintf(writer, "Right file too large to diff (%s > %s)\n\n",
			util.FormatSize(result.RightInfo.Size), util.FormatSize(d.options.MaxFileSize))
		return nil
	}

	// Read file contents
	leftContent, err := d.readFileContent(leftPath)
	if err != nil {
		return fmt.Errorf("failed to read left file: %w", err)
	}

	rightContent, err := d.readFileContent(rightPath)
	if err != nil {
		return fmt.Errorf("failed to read right file: %w", err)
	}

	// Apply whitespace normalization if requested
	if d.options.IgnoreWhitespace {
		leftContent = d.normalizeWhitespace(leftContent)
		rightContent = d.normalizeWhitespace(rightContent)
	}

	// Check if files are binary
	if d.isBinary(leftContent) || d.isBinary(rightContent) {
		headerColor.Fprintf(writer, "=== %s ===\n", result.RelativePath)
		fmt.Fprintf(writer, "Binary files differ\n")
		fmt.Fprintf(writer, "Left:  %s (%s)\n", leftPath, util.FormatSize(result.LeftInfo.Size))
		fmt.Fprintf(writer, "Right: %s (%s)\n\n", rightPath, util.FormatSize(result.RightInfo.Size))
		return nil
	}

	// Generate unified diff
	diffs := d.dmp.DiffMain(leftContent, rightContent, false)
	if len(diffs) == 0 {
		return nil // No differences (shouldn't happen for modified files)
	}

	// Display diff header
	headerColor.Fprintf(writer, "=== %s ===\n", result.RelativePath)
	pathColor.Fprintf(writer, "--- %s\n", leftPath)
	pathColor.Fprintf(writer, "+++ %s\n", rightPath)

	// Convert to line-based diff for better display
	if err := d.displayUnifiedDiff(leftContent, rightContent, writer,
		addedColor, removedColor, contextColor); err != nil {
		return err
	}

	fmt.Fprintf(writer, "\n")
	return nil
}

// displayUnifiedDiff displays a unified diff format
func (d *Display) displayUnifiedDiff(
	leftContent, rightContent string,
	writer io.Writer,
	addedColor, removedColor, contextColor *color.Color,
) error {
	leftLines := strings.Split(leftContent, "\n")
	rightLines := strings.Split(rightContent, "\n")

	// Use a simple LCS-based diff algorithm for line-by-line comparison
	hunks := d.generateHunks(leftLines, rightLines)

	for _, hunk := range hunks {
		// Display hunk header
		fmt.Fprintf(writer, "@@ -%d,%d +%d,%d @@\n",
			hunk.LeftStart+1, hunk.LeftCount,
			hunk.RightStart+1, hunk.RightCount)

		// Display hunk lines
		for _, line := range hunk.Lines {
			switch line.Type {
			case DiffLineContext:
				contextColor.Fprintf(writer, " %s\n", line.Content)
			case DiffLineDeleted:
				removedColor.Fprintf(writer, "-%s\n", line.Content)
			case DiffLineAdded:
				addedColor.Fprintf(writer, "+%s\n", line.Content)
			}
		}
	}

	return nil
}

// DiffLineType represents the type of line in a diff
type DiffLineType int

const (
	DiffLineContext DiffLineType = iota
	DiffLineDeleted
	DiffLineAdded
)

// DiffLine represents a single line in a diff
type DiffLine struct {
	Type    DiffLineType
	Content string
}

// DiffHunk represents a hunk in a unified diff
type DiffHunk struct {
	LeftStart  int
	LeftCount  int
	RightStart int
	RightCount int
	Lines      []DiffLine
}

// generateHunks generates diff hunks from two sets of lines
func (d *Display) generateHunks(leftLines, rightLines []string) []DiffHunk {
	// This is a simplified diff algorithm
	// In a production system, you might want to use a more sophisticated algorithm

	var hunks []DiffHunk
	leftIdx, rightIdx := 0, 0

	for leftIdx < len(leftLines) || rightIdx < len(rightLines) {
		hunk := DiffHunk{
			LeftStart:  leftIdx,
			RightStart: rightIdx,
		}

		// Find the next difference
		contextStart := leftIdx
		for leftIdx < len(leftLines) && rightIdx < len(rightLines) &&
			leftLines[leftIdx] == rightLines[rightIdx] {
			leftIdx++
			rightIdx++
		}

		// Add context before the difference
		contextEnd := leftIdx
		if contextEnd-contextStart > d.options.Context*2 {
			// Too much context, trim it
			if len(hunks) > 0 {
				// Skip some context at the beginning
				contextStart = contextEnd - d.options.Context
			} else {
				// For the first hunk, show more context at the beginning
				contextStart = max(0, contextEnd-d.options.Context)
			}
		}

		for i := contextStart; i < contextEnd; i++ {
			hunk.Lines = append(hunk.Lines, DiffLine{
				Type:    DiffLineContext,
				Content: leftLines[i],
			})
		}

		// Handle the difference
		diffStartLeft := leftIdx
		diffStartRight := rightIdx

		// Find end of difference (simplified algorithm)
		for leftIdx < len(leftLines) && rightIdx < len(rightLines) {
			if leftLines[leftIdx] == rightLines[rightIdx] {
				break
			}

			// Simple heuristic: advance both
			leftIdx++
			rightIdx++
		}

		// Add deleted lines
		for i := diffStartLeft; i < leftIdx && i < len(leftLines); i++ {
			hunk.Lines = append(hunk.Lines, DiffLine{
				Type:    DiffLineDeleted,
				Content: leftLines[i],
			})
		}

		// Add added lines
		for i := diffStartRight; i < rightIdx && i < len(rightLines); i++ {
			hunk.Lines = append(hunk.Lines, DiffLine{
				Type:    DiffLineAdded,
				Content: rightLines[i],
			})
		}

		// Add context after the difference
		contextAfter := min(leftIdx+d.options.Context, len(leftLines))
		for i := leftIdx; i < contextAfter; i++ {
			if i < len(leftLines) {
				hunk.Lines = append(hunk.Lines, DiffLine{
					Type:    DiffLineContext,
					Content: leftLines[i],
				})
			}
		}

		hunk.LeftCount = leftIdx - hunk.LeftStart
		hunk.RightCount = rightIdx - hunk.RightStart

		if len(hunk.Lines) > 0 {
			hunks = append(hunks, hunk)
		}

		// Move past the matched section
		for leftIdx < len(leftLines) && rightIdx < len(rightLines) &&
			leftLines[leftIdx] == rightLines[rightIdx] {
			leftIdx++
			rightIdx++
		}

		// If we've reached the end of both files, break
		if leftIdx >= len(leftLines) && rightIdx >= len(rightLines) {
			break
		}
	}

	return hunks
}

// readFileContent reads the content of a file
func (d *Display) readFileContent(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var content strings.Builder
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		content.WriteString(scanner.Text())
		content.WriteString("\n")
	}

	return content.String(), scanner.Err()
}

// normalizeWhitespace normalizes whitespace in content
func (d *Display) normalizeWhitespace(content string) string {
	lines := strings.Split(content, "\n")
	var normalized []string

	for _, line := range lines {
		// Trim leading and trailing whitespace
		trimmed := strings.TrimSpace(line)

		// Normalize internal whitespace (multiple spaces/tabs to single space)
		var result strings.Builder
		inWhitespace := false
		for _, r := range trimmed {
			if unicode.IsSpace(r) {
				if !inWhitespace {
					result.WriteRune(' ')
					inWhitespace = true
				}
			} else {
				result.WriteRune(r)
				inWhitespace = false
			}
		}

		normalized = append(normalized, result.String())
	}

	return strings.Join(normalized, "\n")
}

// isBinary checks if content appears to be binary
func (d *Display) isBinary(content string) bool {
	// Simple heuristic: if there are null bytes or too many non-printable characters
	nullBytes := strings.Count(content, "\x00")
	if nullBytes > 0 {
		return true
	}

	nonPrintable := 0
	for _, r := range content {
		if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			nonPrintable++
		}
	}

	// If more than 30% of characters are non-printable, consider it binary
	if len(content) > 0 && float64(nonPrintable)/float64(len(content)) > 0.3 {
		return true
	}

	return false
}

// Helper functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
