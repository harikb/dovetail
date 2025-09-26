package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/harikb/dovetail/internal/action"
	"github.com/harikb/dovetail/internal/util"
)

// dryCmd represents the dry command
var dryCmd = &cobra.Command{
	Use:   "dry <ACTION_FILE> [LEFT_DIR] [RIGHT_DIR]",
	Short: "Preview actions from an action file without executing them",
	Long: `Preview the actions that would be taken when applying an action file.
This shows exactly what files would be copied, deleted, or modified without
actually performing any operations. Use this to verify your action file
before running 'dovetail apply'.

Both positional and flag formats are supported for directory specification:

Examples:
  # Positional format (easy workflow switching):
  dovetail dry actions.txt /path/to/source /path/to/target
  dovetail dry my_sync.txt ./src ./backup
  
  # Flag format (explicit):
  dovetail dry actions.txt --left /path/to/source --right /path/to/target
  dovetail dry my_sync.txt -l ./src -r ./backup`,
	Args: cobra.RangeArgs(1, 3), // ACTION_FILE [LEFT_DIR] [RIGHT_DIR]
	RunE: runDryRun,
}

var (
	dryRunLeftDir  string
	dryRunRightDir string
)

func init() {
	rootCmd.AddCommand(dryCmd)

	// Optional directory flags (alternative to positional args)
	dryCmd.Flags().StringVarP(&dryRunLeftDir, "left", "l", "", "left directory path (use either flags or positional args)")
	dryCmd.Flags().StringVarP(&dryRunRightDir, "right", "r", "", "right directory path (use either flags or positional args)")

	// Note: flags are no longer required - either flags OR positional args must be provided
}

func runDryRun(cmd *cobra.Command, args []string) error {
	// Log extensive debugging information
	util.LogInfo("=== DRY RUN COMMAND STARTED ===")
	util.LogInfo("Full command line: %v", os.Args)
	util.LogInfo("Cobra args received: %v", args)
	util.LogInfo("Command flags - dryRunLeftDir: %q, dryRunRightDir: %q", dryRunLeftDir, dryRunRightDir)
	util.LogInfo("Number of args: %d", len(args))

	if len(args) == 0 {
		util.LogInfo("ERROR: No arguments provided to dry command")
		return fmt.Errorf("no action file specified")
	}

	actionFile := args[0]
	util.LogInfo("Action file from args[0]: %q", actionFile)

	// Validate action file exists
	util.LogInfo("Checking if action file exists: %q", actionFile)
	if _, err := os.Stat(actionFile); err != nil {
		util.LogInfo("ERROR: Action file stat failed: %v", err)
		if os.IsNotExist(err) {
			return fmt.Errorf("action file does not exist: %s", actionFile)
		}
		return fmt.Errorf("failed to access action file %s: %w", actionFile, err)
	}
	util.LogInfo("Action file exists and is accessible")

	// Determine directory paths from either positional args or flags
	var leftDir, rightDir string

	hasPositionalDirs := len(args) == 3
	hasFlagDirs := dryRunLeftDir != "" && dryRunRightDir != ""

	util.LogInfo("Directory detection - hasPositionalDirs: %t, hasFlagDirs: %t", hasPositionalDirs, hasFlagDirs)

	if hasPositionalDirs && hasFlagDirs {
		util.LogInfo("ERROR: Both positional and flag directories provided")
		return fmt.Errorf("cannot use both positional directories and flags - choose one format")
	}

	if hasPositionalDirs {
		// Use positional arguments: dry actions.txt left/ right/
		leftDir = args[1]
		rightDir = args[2]
		util.LogInfo("Using positional directories - leftDir: %q, rightDir: %q", leftDir, rightDir)
	} else if hasFlagDirs {
		// Use flag arguments: dry actions.txt -l left/ -r right/
		leftDir = dryRunLeftDir
		rightDir = dryRunRightDir
		util.LogInfo("Using flag directories - leftDir: %q, rightDir: %q", leftDir, rightDir)
	} else {
		util.LogInfo("ERROR: No directories specified in either positional args or flags")
		return fmt.Errorf("directories must be specified either as positional args or flags:\n"+
			"  Positional: dry %s <LEFT_DIR> <RIGHT_DIR>\n"+
			"  Flags:      dry %s --left <LEFT_DIR> --right <RIGHT_DIR>", actionFile, actionFile)
	}

	// Validate directories exist
	util.LogInfo("Validating left directory: %q", leftDir)
	if err := validateDirectory(leftDir); err != nil {
		util.LogInfo("ERROR: Left directory validation failed: %v", err)
		return fmt.Errorf("left directory: %w", err)
	}
	util.LogInfo("Left directory validation passed")

	util.LogInfo("Validating right directory: %q", rightDir)
	if err := validateDirectory(rightDir); err != nil {
		util.LogInfo("ERROR: Right directory validation failed: %v", err)
		return fmt.Errorf("right directory: %w", err)
	}
	util.LogInfo("Right directory validation passed")

	// Convert to absolute paths
	util.LogInfo("Converting paths to absolute - leftDir: %q", leftDir)
	leftDir, err := filepath.Abs(leftDir)
	if err != nil {
		util.LogInfo("ERROR: Failed to resolve left directory to absolute path: %v", err)
		return fmt.Errorf("failed to resolve left directory path: %w", err)
	}
	util.LogInfo("Left directory absolute path: %q", leftDir)

	util.LogInfo("Converting paths to absolute - rightDir: %q", rightDir)
	rightDir, err = filepath.Abs(rightDir)
	if err != nil {
		util.LogInfo("ERROR: Failed to resolve right directory to absolute path: %v", err)
		return fmt.Errorf("failed to resolve right directory path: %w", err)
	}
	util.LogInfo("Right directory absolute path: %q", rightDir)

	util.LogInfo("Converting paths to absolute - actionFile: %q", actionFile)
	actionFile, err = filepath.Abs(actionFile)
	if err != nil {
		util.LogInfo("ERROR: Failed to resolve action file to absolute path: %v", err)
		return fmt.Errorf("failed to resolve action file path: %w", err)
	}
	util.LogInfo("Action file absolute path: %q", actionFile)

	if GetVerboseLevel() >= 1 {
		fmt.Printf("Dry run preview:\n")
		fmt.Printf("  Action file: %s\n", actionFile)
		fmt.Printf("  Left dir:    %s\n", leftDir)
		fmt.Printf("  Right dir:   %s\n", rightDir)
		fmt.Println()
	}

	// Parse action file
	file, err := os.Open(actionFile)
	if err != nil {
		return fmt.Errorf("failed to open action file: %w", err)
	}
	defer file.Close()

	parser := action.NewParser()
	actionFileData, err := parser.ParseActionFile(file)
	if err != nil {
		return fmt.Errorf("failed to parse action file: %w", err)
	}

	// Validate action file
	validationErrors := parser.ValidateActionFile(actionFileData, leftDir, rightDir)
	if len(validationErrors) > 0 {
		fmt.Printf("Validation errors found:\n")
		for _, err := range validationErrors {
			fmt.Printf("  %s\n", err.Error())
		}
		return fmt.Errorf("action file contains validation errors")
	}

	// Execute in dry-run mode
	executor := action.NewExecutor(true) // true for dry-run mode
	summary, results, err := executor.ExecuteActions(actionFileData, leftDir, rightDir)
	if err != nil {
		return fmt.Errorf("dry-run execution failed: %w", err)
	}

	// Display results
	fmt.Printf("DRY RUN PREVIEW\n")
	fmt.Printf("===============\n")
	fmt.Printf("Action file: %s\n", actionFile)
	fmt.Printf("Left dir:    %s\n", leftDir)
	fmt.Printf("Right dir:   %s\n", rightDir)
	fmt.Printf("\n")

	if len(results) == 0 {
		fmt.Printf("No actions to perform (all actions are set to ignore).\n")
		return nil
	}

	fmt.Printf("Actions to be performed:\n")
	fmt.Printf("========================\n")
	for _, result := range results {
		fmt.Printf("%s\n", result.Message)
	}

	fmt.Printf("\nSummary:\n")
	fmt.Printf("--------\n")
	fmt.Printf("Total actions: %d\n", len(results))
	if summary.FilesCreated > 0 {
		fmt.Printf("Files to be created: %d\n", summary.FilesCreated)
	}
	if summary.FilesOverwritten > 0 {
		fmt.Printf("Files to be overwritten: %d\n", summary.FilesOverwritten)
	}
	if summary.FilesDeleted > 0 {
		fmt.Printf("Files to be deleted: %d\n", summary.FilesDeleted)
	}
	if summary.BytesCopied > 0 {
		fmt.Printf("Data to be copied: %s\n", util.FormatSize(summary.BytesCopied))
	}

	fmt.Printf("\nTo execute these actions, run:\n")
	fmt.Printf("  dovetail apply %s -l %s -r %s\n", actionFile, leftDir, rightDir)

	return nil
}
