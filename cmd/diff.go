package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/harikb/dovetail/internal/action"
	"github.com/harikb/dovetail/internal/compare"
	"github.com/harikb/dovetail/internal/diff"
)

// diffCmd represents the diff command
var diffCmd = &cobra.Command{
	Use:   "diff <DIR_LEFT> <DIR_RIGHT>",
	Short: "Compare two directories and generate action file",
	Long: `Compare two directories recursively and generate an action file that can be 
used to synchronize them. The action file will contain all differences with default
'ignore' actions, which you can then edit to specify the desired synchronization actions.

Examples:
  dovetail diff /path/to/source /path/to/target -o actions.txt
  dovetail diff ./src ./backup --show-diff --ignore-whitespace
  dovetail diff dir1 dir2 --exclude-name "*.log" "*.tmp" --exclude-path "build/"`,
	Args: cobra.ExactArgs(2),
	RunE: runDiff,
}

var (
	outputFile        string
	showDiff          bool
	ignoreWhitespace  bool
	excludeNames      []string
	excludePaths      []string
	excludeExtensions []string
)

func init() {
	rootCmd.AddCommand(diffCmd)

	// Output options
	diffCmd.Flags().StringVarP(&outputFile, "output", "o", "", "output action file path (required unless --show-diff)")

	// Display options
	diffCmd.Flags().BoolVar(&showDiff, "show-diff", false, "display inline diffs instead of generating action file")
	diffCmd.Flags().BoolVar(&ignoreWhitespace, "ignore-whitespace", false, "ignore whitespace differences in diffs")

	// Exclusion options
	diffCmd.Flags().StringSliceVar(&excludeNames, "exclude-name", []string{}, "exclude files/directories by name or glob pattern")
	diffCmd.Flags().StringSliceVar(&excludePaths, "exclude-path", []string{}, "exclude files/directories by relative path")
	diffCmd.Flags().StringSliceVar(&excludeExtensions, "exclude-ext", []string{}, "exclude files by extension (without dot)")

	// Mark output as required when not showing diff
	diffCmd.MarkFlagRequired("output")
}

func runDiff(cmd *cobra.Command, args []string) error {
	leftDir := args[0]
	rightDir := args[1]

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
	if !showDiff && outputFile == "" {
		return fmt.Errorf("output file (-o) is required when not using --show-diff")
	}

	if GetVerboseLevel() >= 1 {
		fmt.Printf("Comparing directories:\n")
		fmt.Printf("  Left:  %s\n", leftDir)
		fmt.Printf("  Right: %s\n", rightDir)
		if len(excludeNames) > 0 {
			fmt.Printf("  Excluding names: %s\n", strings.Join(excludeNames, ", "))
		}
		if len(excludePaths) > 0 {
			fmt.Printf("  Excluding paths: %s\n", strings.Join(excludePaths, ", "))
		}
		if len(excludeExtensions) > 0 {
			fmt.Printf("  Excluding extensions: %s\n", strings.Join(excludeExtensions, ", "))
		}
		fmt.Println()
	}

	// Create comparison options
	options := compare.ComparisonOptions{
		ExcludeNames:      excludeNames,
		ExcludePaths:      excludePaths,
		ExcludeExtensions: excludeExtensions,
	}

	// Create comparison engine
	engine := compare.NewEngine(options)
	engine.SetVerboseLevel(GetVerboseLevel())

	// Perform comparison
	results, summary, err := engine.Compare(leftDir, rightDir)
	if err != nil {
		return fmt.Errorf("comparison failed: %w", err)
	}

	if GetVerboseLevel() >= 1 {
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
		// Display diffs
		diffOptions := diff.DisplayOptions{
			IgnoreWhitespace: ignoreWhitespace,
			NoColor:          viper.GetBool("no-color"),
		}

		diffDisplay := diff.NewDisplay(diffOptions)
		return diffDisplay.ShowDifferences(results, leftDir, rightDir, os.Stdout)
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
		if err := generator.GenerateActionFile(file, results, leftDir, rightDir, summary); err != nil {
			return fmt.Errorf("failed to generate action file: %w", err)
		}

		fmt.Printf("Action file generated: %s\n", outputFile)
		fmt.Printf("Edit this file to specify the actions you want to take, then run:\n")
		fmt.Printf("  dovetail dry-run %s -l %s -r %s  # to preview actions\n", outputFile, leftDir, rightDir)
		fmt.Printf("  dovetail apply %s -l %s -r %s    # to execute actions\n", outputFile, leftDir, rightDir)

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
