package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/harikb/dovetail/internal/compare"
	"github.com/harikb/dovetail/internal/config"
	"github.com/harikb/dovetail/internal/tui"
	"github.com/harikb/dovetail/internal/util"
)

// tuiCmd represents the tui command
var tuiCmd = &cobra.Command{
	Use:   "tui [LEFT_DIR] [RIGHT_DIR]",
	Short: "Interactive TUI for directory comparison",
	Long: `Launch an interactive terminal UI for comparing directories.
Navigate through files with arrow keys and press Enter to view diffs.

Both positional and flag formats are supported for directory specification:

Examples:
  # Positional format (easy workflow switching):
  dovetail tui /path/to/source /path/to/target
  dovetail tui ./src ./backup --exclude-name "*.log"
  
  # Flag format (explicit):
  dovetail tui --left /path/to/source --right /path/to/target
  dovetail tui -l ./src -r ./backup --exclude-name "*.log"`,
	Args: cobra.RangeArgs(0, 2), // [LEFT_DIR] [RIGHT_DIR] or use flags
	RunE: runTUI,
}

var (
	tuiLeftDir           string
	tuiRightDir          string
	tuiExcludeNames      []string
	tuiExcludePaths      []string
	tuiExcludeExtensions []string
	tuiUseGitignore      bool
	tuiIgnoreWhitespace  bool
)

func init() {
	rootCmd.AddCommand(tuiCmd)

	// Optional directory flags (alternative to positional args)
	tuiCmd.Flags().StringVarP(&tuiLeftDir, "left", "l", "", "left directory path (use either flags or positional args)")
	tuiCmd.Flags().StringVarP(&tuiRightDir, "right", "r", "", "right directory path (use either flags or positional args)")

	// Exclusion options (same as diff command)
	tuiCmd.Flags().StringSliceVar(&tuiExcludeNames, "exclude-name", []string{}, "exclude files/directories by name or glob pattern")
	tuiCmd.Flags().StringSliceVar(&tuiExcludePaths, "exclude-path", []string{}, "exclude files/directories by relative path")
	tuiCmd.Flags().StringSliceVar(&tuiExcludeExtensions, "exclude-ext", []string{}, "exclude files by extension (without dot)")
	tuiCmd.Flags().BoolVar(&tuiUseGitignore, "use-gitignore", false, "read and apply .gitignore rules from both directories")
	tuiCmd.Flags().BoolVar(&tuiIgnoreWhitespace, "ignore-whitespace", false, "ignore whitespace differences in diffs")
}

func runTUI(cmd *cobra.Command, args []string) error {
	// Determine directory paths from either positional args or flags
	var leftDir, rightDir string

	hasPositionalDirs := len(args) == 2
	hasFlagDirs := tuiLeftDir != "" && tuiRightDir != ""

	if hasPositionalDirs && hasFlagDirs {
		return fmt.Errorf("cannot use both positional directories and flags - choose one format")
	}

	if hasPositionalDirs {
		// Use positional arguments: tui left/ right/
		leftDir = args[0]
		rightDir = args[1]
	} else if hasFlagDirs {
		// Use flag arguments: tui -l left/ -r right/
		leftDir = tuiLeftDir
		rightDir = tuiRightDir
	} else {
		return fmt.Errorf("directories must be specified either as positional args or flags:\n" +
			"  Positional: tui <LEFT_DIR> <RIGHT_DIR>\n" +
			"  Flags:      tui --left <LEFT_DIR> --right <RIGHT_DIR>")
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

	// Load configuration
	loader := config.NewLoader(GetVerboseLevel())
	cfg, err := loader.Load("")
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Apply CLI overrides
	cliConfig := config.CLIConfig{
		VerboseLevel:      GetVerboseLevel(),
		ExcludeNames:      tuiExcludeNames,
		ExcludePaths:      tuiExcludePaths,
		ExcludeExtensions: tuiExcludeExtensions,
		UseGitignore:      tuiUseGitignore,
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

	// Show loading message
	util.LogProgress("Scanning directories...")

	// Perform comparison
	results, summary, err := engine.Compare(leftDir, rightDir)
	if err != nil {
		return fmt.Errorf("comparison failed: %w", err)
	}

	// Configure logger for TUI mode (file-only, no stderr output)
	if err := util.SetTUIMode(); err != nil {
		return fmt.Errorf("failed to configure TUI logging: %w", err)
	}

	// Launch TUI with profiling cleanup
	tuiApp := tui.NewApp(results, summary, leftDir, rightDir, tuiIgnoreWhitespace)
	tui.SetProfilingCleanup(GetCleanupProfiling())
	return tuiApp.Run()
}
