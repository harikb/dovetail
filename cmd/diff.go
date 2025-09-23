package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/harikb/dovetail/internal/action"
	"github.com/harikb/dovetail/internal/compare"
	"github.com/harikb/dovetail/internal/config"
)

// diffCmd represents the diff command
var diffCmd = &cobra.Command{
	Use:   "diff [LEFT_DIR] [RIGHT_DIR]",
	Short: "Compare two directories and generate action file",
	Long: `Compare two directories recursively and generate an action file that can be 
used to synchronize them. The action file will contain all differences with default
'ignore' actions, which you can then edit to specify the desired synchronization actions.

Both positional and flag formats are supported for directory specification:

Examples:
  # Positional format (easy workflow switching):
  dovetail diff /path/to/source /path/to/target -o actions.txt
  dovetail diff ./src ./backup --show-diff --ignore-whitespace
  dovetail diff dir1 dir2 --exclude-name "*.log" "*.tmp" --exclude-path "build/"
  
  # Flag format (explicit):
  dovetail diff --left /path/to/source --right /path/to/target -o actions.txt
  dovetail diff -l ./src -r ./backup --show-diff --ignore-whitespace`,
	Args: cobra.RangeArgs(0, 2), // [LEFT_DIR] [RIGHT_DIR] or use flags
	RunE: runDiff,
}

var (
	diffLeftDir       string
	diffRightDir      string
	outputFile        string
	showDiff          bool
	showDiffFile      string
	includeIdentical  bool
	ignoreWhitespace  bool
	excludeNames      []string
	excludePaths      []string
	excludeExtensions []string
	useGitignore      bool
)

func init() {
	rootCmd.AddCommand(diffCmd)

	// Optional directory flags (alternative to positional args)
	diffCmd.Flags().StringVarP(&diffLeftDir, "left", "l", "", "left directory path (use either flags or positional args)")
	diffCmd.Flags().StringVarP(&diffRightDir, "right", "r", "", "right directory path (use either flags or positional args)")

	// Output options
	diffCmd.Flags().StringVarP(&outputFile, "output", "o", "", "output action file path (required unless --show-diff)")
	diffCmd.Flags().BoolVar(&includeIdentical, "include-identical", false, "include identical files in action file (default: only show different files)")

	// Display options
	diffCmd.Flags().BoolVar(&showDiff, "show-diff", false, "display inline diffs instead of generating action file")
	diffCmd.Flags().StringVar(&showDiffFile, "show-diff-file", "", "show diff for specific file (relative path from either directory)")
	diffCmd.Flags().BoolVar(&ignoreWhitespace, "ignore-whitespace", false, "ignore whitespace differences in diffs")

	// Exclusion options
	diffCmd.Flags().StringSliceVar(&excludeNames, "exclude-name", []string{}, "exclude files/directories by name or glob pattern")
	diffCmd.Flags().StringSliceVar(&excludePaths, "exclude-path", []string{}, "exclude files/directories by relative path")
	diffCmd.Flags().StringSliceVar(&excludeExtensions, "exclude-ext", []string{}, "exclude files by extension (without dot)")
	diffCmd.Flags().BoolVar(&useGitignore, "use-gitignore", false, "read and apply .gitignore rules from both directories")

	// Note: output requirement is handled dynamically in runDiff based on other flags
}

func runDiff(cmd *cobra.Command, args []string) error {
	// Determine directory paths from either positional args or flags
	var leftDir, rightDir string

	hasPositionalDirs := len(args) == 2
	hasFlagDirs := diffLeftDir != "" && diffRightDir != ""

	if hasPositionalDirs && hasFlagDirs {
		return fmt.Errorf("cannot use both positional directories and flags - choose one format")
	}

	if hasPositionalDirs {
		// Use positional arguments: diff left/ right/
		leftDir = args[0]
		rightDir = args[1]
	} else if hasFlagDirs {
		// Use flag arguments: diff -l left/ -r right/
		leftDir = diffLeftDir
		rightDir = diffRightDir
	} else {
		return fmt.Errorf("directories must be specified either as positional args or flags:\n" +
			"  Positional: diff <LEFT_DIR> <RIGHT_DIR> [options]\n" +
			"  Flags:      diff --left <LEFT_DIR> --right <RIGHT_DIR> [options]")
	}

	// Validate directories exist
	if err := validateDirectory(leftDir); err != nil {
		return fmt.Errorf("left directory: %w", err)
	}
	if err := validateDirectory(rightDir); err != nil {
		return fmt.Errorf("right directory: %w", err)
	}

	// Convert to absolute paths
	leftDir, err := filepath.Abs(leftDir)
	if err != nil {
		return fmt.Errorf("failed to resolve left directory path: %w", err)
	}
	rightDir, err = filepath.Abs(rightDir)
	if err != nil {
		return fmt.Errorf("failed to resolve right directory path: %w", err)
	}

	// Validate output requirements
	if !showDiff && showDiffFile == "" && outputFile == "" {
		return fmt.Errorf("output file (-o) is required when not using --show-diff or --show-diff-file")
	}
	if showDiff && showDiffFile != "" {
		return fmt.Errorf("cannot use both --show-diff and --show-diff-file")
	}
	if showDiffFile != "" && outputFile != "" {
		return fmt.Errorf("cannot use both --show-diff-file and output file (-o)")
	}

	// Load configuration
	loader := config.NewLoader(GetVerboseLevel())
	cfg, err := loader.Load("")
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Apply CLI overrides
	cliConfig := config.CLIConfig{
		VerboseLevel:      GetVerboseLevel(),
		NoColor:           false, // We'll get this from viper later
		ExcludeNames:      excludeNames,
		ExcludePaths:      excludePaths,
		ExcludeExtensions: excludeExtensions,
		UseGitignore:      useGitignore,
	}
	config.ApplyCLIOverrides(cfg, cliConfig)

	// Process gitignore if enabled
	if cfg.Gitignore.Enabled {
		gitignoreParser := config.NewGitignoreParser(cfg.General.Verbose)
		gitignoreResult, err := gitignoreParser.ParseGitignoreFiles(leftDir, rightDir, cfg.Gitignore.CheckBothSides)
		if err != nil {
			return fmt.Errorf("failed to process .gitignore: %w", err)
		}

		// Add gitignore patterns to exclusions
		cfg.Exclusions.Names = append(cfg.Exclusions.Names, gitignoreResult.Names...)
		cfg.Exclusions.Paths = append(cfg.Exclusions.Paths, gitignoreResult.Paths...)
		cfg.Exclusions.Extensions = append(cfg.Exclusions.Extensions, gitignoreResult.Extensions...)
	}

	// Automatically exclude .patch files created by hunk operations
	cfg.Exclusions.Extensions = append(cfg.Exclusions.Extensions, "patch")

	if cfg.General.Verbose >= 1 {
		fmt.Printf("Comparing directories:\n")
		fmt.Printf("  Left:  %s\n", leftDir)
		fmt.Printf("  Right: %s\n", rightDir)
		if len(cfg.Exclusions.Names) > 0 {
			fmt.Printf("  Excluding names: %s\n", strings.Join(cfg.Exclusions.Names, ", "))
		}
		if len(cfg.Exclusions.Paths) > 0 {
			fmt.Printf("  Excluding paths: %s\n", strings.Join(cfg.Exclusions.Paths, ", "))
		}
		if len(cfg.Exclusions.Extensions) > 0 {
			fmt.Printf("  Excluding extensions: %s\n", strings.Join(cfg.Exclusions.Extensions, ", "))
		}
		fmt.Println()
	}

	// Create comparison options from config
	options := compare.ComparisonOptions{
		ExcludeNames:      cfg.Exclusions.Names,
		ExcludePaths:      cfg.Exclusions.Paths,
		ExcludeExtensions: cfg.Exclusions.Extensions,
		FollowSymlinks:    cfg.General.FollowSymlinks,
		IgnorePermissions: cfg.General.IgnorePermissions,
		MaxFileSize:       cfg.Performance.MaxFileSize,
		ParallelWorkers:   cfg.Performance.ParallelWorkers,
	}

	// Create comparison engine
	engine := compare.NewEngine(options)
	engine.SetVerboseLevel(cfg.General.Verbose)

	// Perform comparison
	results, summary, err := engine.Compare(leftDir, rightDir)
	if err != nil {
		return fmt.Errorf("comparison failed: %w", err)
	}

	if cfg.General.Verbose >= 1 {
		fmt.Printf("Comparison completed:\n")
		fmt.Printf("  Files - Total: %d, Identical: %d, Modified: %d, Left only: %d, Right only: %d\n",
			summary.TotalFiles, summary.IdenticalFiles, summary.ModifiedFiles,
			summary.OnlyLeftFiles, summary.OnlyRightFiles)
		fmt.Printf("  Directories - Total: %d, Identical: %d, Left only: %d, Right only: %d\n",
			summary.TotalDirs, summary.IdenticalDirs, summary.OnlyLeftDirs, summary.OnlyRightDirs)
		if len(summary.ErrorsEncountered) > 0 {
			fmt.Printf("  Errors encountered: %d\n", len(summary.ErrorsEncountered))
		}
		fmt.Println()
	}

	if showDiff {
		// Display checksum-based diffs for all modified files
		return showAllDifferences(results, leftDir, rightDir, cfg.General.NoColor, ignoreWhitespace)
	} else if showDiffFile != "" {
		// Display diff for single specific file
		return showSingleFileDiff(results, leftDir, rightDir, showDiffFile, cfg.General.NoColor, ignoreWhitespace)
	} else {
		// Generate action file
		outputFile, err := filepath.Abs(outputFile)
		if err != nil {
			return fmt.Errorf("failed to resolve output file path: %w", err)
		}

		file, err := os.Create(outputFile)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer file.Close()

		generator := action.NewGenerator(rootCmd.Version)
		if err := generator.GenerateActionFile(file, results, leftDir, rightDir, summary, includeIdentical); err != nil {
			return fmt.Errorf("failed to generate action file: %w", err)
		}

		fmt.Printf("Action file generated: %s\n", outputFile)
		fmt.Printf("Edit this file to specify the actions you want to take, then run:\n")
		fmt.Printf("  dovetail dry %s %s %s  # to preview actions\n", outputFile, leftDir, rightDir)
		fmt.Printf("  dovetail apply %s %s %s    # to execute actions\n", outputFile, leftDir, rightDir)

		return nil
	}
}

func validateDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory does not exist: %s", path)
		}
		return fmt.Errorf("failed to access directory %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", path)
	}
	return nil
}

// showAllDifferences displays checksum-based differences for all modified files
func showAllDifferences(results []compare.ComparisonResult, leftDir, rightDir string, noColor bool, ignoreWhitespace bool) error {
	if noColor {
		fmt.Printf("Comparison Results:\n")
		fmt.Printf("==================\n")
	} else {
		fmt.Printf("\033[1;36mComparison Results:\033[0m\n")
		fmt.Printf("\033[1;36m==================\033[0m\n")
	}
	fmt.Printf("Left:  %s\n", leftDir)
	fmt.Printf("Right: %s\n", rightDir)
	fmt.Printf("\n")

	modifiedCount := 0
	for _, result := range results {
		if result.Status == compare.StatusModified {
			modifiedCount++
		}
	}

	if modifiedCount == 0 {
		fmt.Printf("No modified files found.\n")
		return nil
	}

	fmt.Printf("Modified files (%d):\n\n", modifiedCount)

	// Create a slice of modified results and sort them alphabetically
	var modifiedResults []compare.ComparisonResult
	for _, result := range results {
		if result.Status == compare.StatusModified {
			modifiedResults = append(modifiedResults, result)
		}
	}
	sort.Slice(modifiedResults, func(i, j int) bool {
		return modifiedResults[i].RelativePath < modifiedResults[j].RelativePath
	})

	for _, result := range modifiedResults {
		showFileStatus(result, leftDir, rightDir, noColor, ignoreWhitespace)
	}

	return nil
}

// showSingleFileDiff displays diff for a single specific file
func showSingleFileDiff(results []compare.ComparisonResult, leftDir, rightDir, targetFile string, noColor bool, ignoreWhitespace bool) error {
	// Find the specific file in results
	var targetResult *compare.ComparisonResult
	for _, result := range results {
		if result.RelativePath == targetFile {
			targetResult = &result
			break
		}
	}

	if targetResult == nil {
		return fmt.Errorf("file not found in comparison results: %s", targetFile)
	}

	if targetResult.Status == compare.StatusIdentical {
		fmt.Printf("File '%s' is identical in both directories.\n", targetFile)
		return nil
	}

	if noColor {
		fmt.Printf("File Difference:\n")
		fmt.Printf("================\n")
	} else {
		fmt.Printf("\033[1;36mFile Difference:\033[0m\n")
		fmt.Printf("\033[1;36m================\033[0m\n")
	}

	showFileStatus(*targetResult, leftDir, rightDir, noColor, ignoreWhitespace)
	return nil
}

// showFileStatus displays the status of a single file with checksum information
func showFileStatus(result compare.ComparisonResult, leftDir, rightDir string, noColor bool, ignoreWhitespace bool) {
	if noColor {
		fmt.Printf("=== %s ===\n", result.RelativePath)
	} else {
		fmt.Printf("\033[1;33m=== %s ===\033[0m\n", result.RelativePath)
	}

	switch result.Status {
	case compare.StatusModified:
		if result.LeftInfo != nil && result.RightInfo != nil {
			if result.LeftInfo.IsDir && result.RightInfo.IsDir {
				fmt.Printf("Type: Directory (both sides)\n")
				fmt.Printf("Status: Directory structure differs\n")
			} else if result.LeftInfo.IsDir || result.RightInfo.IsDir {
				fmt.Printf("Type mismatch: ")
				if result.LeftInfo.IsDir {
					fmt.Printf("Directory (left) vs File (right)\n")
				} else {
					fmt.Printf("File (left) vs Directory (right)\n")
				}
			} else {
				// Both are files with different content - show Unix diff
				leftPath := filepath.Join(leftDir, result.RelativePath)
				rightPath := filepath.Join(rightDir, result.RelativePath)

				fmt.Printf("Type: File\n")
				fmt.Printf("Status: Content differs (checksum mismatch)\n")
				fmt.Printf("Left:  %s  Size: %s  Hash: %s\n",
					leftPath,
					formatBytes(result.LeftInfo.Size),
					result.LeftInfo.Hash[:8]+"...")
				fmt.Printf("Right: %s  Size: %s  Hash: %s\n",
					rightPath,
					formatBytes(result.RightInfo.Size),
					result.RightInfo.Hash[:8]+"...")
				fmt.Printf("\nDifferences:\n")

				// Use Unix diff to show actual content differences
				if err := showUnixDiff(leftPath, rightPath, result.RelativePath, noColor, ignoreWhitespace); err != nil {
					fmt.Printf("Error generating diff: %v\n", err)
				}
			}
		}
	case compare.StatusOnlyLeft:
		fmt.Printf("Status: Only exists in left directory\n")
		if result.LeftInfo != nil {
			if result.LeftInfo.IsDir {
				fmt.Printf("Type: Directory\n")
			} else {
				fmt.Printf("Type: File  Size: %s  Hash: %s\n",
					formatBytes(result.LeftInfo.Size),
					result.LeftInfo.Hash[:8]+"...")
			}
		}
	case compare.StatusOnlyRight:
		fmt.Printf("Status: Only exists in right directory\n")
		if result.RightInfo != nil {
			if result.RightInfo.IsDir {
				fmt.Printf("Type: Directory\n")
			} else {
				fmt.Printf("Type: File  Size: %s  Hash: %s\n",
					formatBytes(result.RightInfo.Size),
					result.RightInfo.Hash[:8]+"...")
			}
		}
	}

	fmt.Printf("\n")
}

// formatBytes formats bytes in human-readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// showUnixDiff uses the Unix diff command to show actual line-by-line differences
func showUnixDiff(leftPath, rightPath, relativePath string, noColor bool, ignoreWhitespace bool) error {
	// Check if diff command exists
	if _, err := exec.LookPath("diff"); err != nil {
		fmt.Printf("Unix 'diff' command not available: %v\n", err)
		return nil
	}

	// Prepare diff command with unified format
	var cmd *exec.Cmd
	args := []string{"-u"}
	if ignoreWhitespace {
		args = append(args, "-w") // Ignore whitespace differences
	}
	args = append(args, leftPath, rightPath)

	if noColor {
		// Standard unified diff
		cmd = exec.Command("diff", args...)
	} else {
		// Try to use colordiff if available, fallback to regular diff
		if _, err := exec.LookPath("colordiff"); err == nil {
			cmd = exec.Command("colordiff", args...)
		} else {
			cmd = exec.Command("diff", args...)
		}
	}

	// Execute diff command
	output, err := cmd.Output()

	// diff returns exit code 1 when files differ (which is normal)
	// Only treat it as an error if exit code is 2 or higher
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				// Files differ (normal case) - output is valid
				err = nil
			} else {
				// Real error (exit code 2+)
				return fmt.Errorf("diff command failed: %v", err)
			}
		} else {
			return fmt.Errorf("failed to execute diff: %v", err)
		}
	}

	// Print the diff output
	if len(output) > 0 {
		fmt.Printf("```diff\n")
		fmt.Print(string(output))
		fmt.Printf("```\n")
	} else {
		fmt.Printf("Files are identical (unexpected - checksum difference detected)\n")
	}

	return nil
}
