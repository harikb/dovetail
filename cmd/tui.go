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
	Use:   "tui <DIR_LEFT> <DIR_RIGHT>",
	Short: "Interactive TUI for directory comparison",
	Long: `Launch an interactive terminal UI for comparing directories.
Navigate through files with arrow keys and press Enter to view diffs.

Examples:
  dovetail tui /path/to/source /path/to/target
  dovetail tui ./src ./backup --exclude-name "*.log"`,
	Args: cobra.ExactArgs(2),
	RunE: runTUI,
}

var (
	tuiExcludeNames      []string
	tuiExcludePaths      []string
	tuiExcludeExtensions []string
	tuiUseGitignore      bool
	tuiIgnoreWhitespace  bool
)

func init() {
	rootCmd.AddCommand(tuiCmd)

	// Exclusion options (same as diff command)
	tuiCmd.Flags().StringSliceVar(&tuiExcludeNames, "exclude-name", []string{}, "exclude files/directories by name or glob pattern")
	tuiCmd.Flags().StringSliceVar(&tuiExcludePaths, "exclude-path", []string{}, "exclude files/directories by relative path")
	tuiCmd.Flags().StringSliceVar(&tuiExcludeExtensions, "exclude-ext", []string{}, "exclude files by extension (without dot)")
	tuiCmd.Flags().BoolVar(&tuiUseGitignore, "use-gitignore", false, "read and apply .gitignore rules from both directories")
	tuiCmd.Flags().BoolVar(&tuiIgnoreWhitespace, "ignore-whitespace", false, "ignore whitespace differences in diffs")
}

func runTUI(cmd *cobra.Command, args []string) error {
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

	// Launch TUI with profiling cleanup
	tuiApp := tui.NewApp(results, summary, leftDir, rightDir, tuiIgnoreWhitespace)
	tui.SetProfilingCleanup(GetCleanupProfiling())
	return tuiApp.Run()
}
