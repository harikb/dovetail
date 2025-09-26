package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/harikb/dovetail/internal/action"
	"github.com/harikb/dovetail/internal/util"
)

// applyCmd represents the apply command
var applyCmd = &cobra.Command{
	Use:   "apply <ACTION_FILE> [LEFT_DIR] [RIGHT_DIR]",
	Short: "Execute actions from an action file",
	Long: `Execute the synchronization actions specified in an action file.
This will perform actual file operations (copy, delete, etc.) based on
the actions you've specified in the action file.

WARNING: This command will modify your filesystem. Always run 'dry'
first to preview the actions that will be taken.

Both positional and flag formats are supported for directory specification:

Examples:
  # Positional format (easy workflow switching):
  dovetail apply actions.txt /path/to/source /path/to/target
  dovetail apply my_sync.txt ./src ./backup --force
  
  # Flag format (explicit):
  dovetail apply actions.txt --left /path/to/source --right /path/to/target
  dovetail apply my_sync.txt -l ./src -r ./backup --force`,
	Args: cobra.RangeArgs(1, 3), // ACTION_FILE [LEFT_DIR] [RIGHT_DIR]
	RunE: runApply,
}

var (
	applyLeftDir  string
	applyRightDir string
	forceApply    bool
)

func init() {
	rootCmd.AddCommand(applyCmd)

	// Optional directory flags (alternative to positional args)
	applyCmd.Flags().StringVarP(&applyLeftDir, "left", "l", "", "left directory path (use either flags or positional args)")
	applyCmd.Flags().StringVarP(&applyRightDir, "right", "r", "", "right directory path (use either flags or positional args)")
	applyCmd.Flags().BoolVar(&forceApply, "force", false, "skip confirmation prompt")

	// Note: flags are no longer required - either flags OR positional args must be provided
}

func runApply(cmd *cobra.Command, args []string) error {
	// Log extensive debugging information
	util.LogInfo("=== APPLY COMMAND STARTED ===")
	util.LogInfo("Full command line: %v", os.Args)
	util.LogInfo("Cobra args received: %v", args)
	util.LogInfo("Command flags - applyLeftDir: %q, applyRightDir: %q, forceApply: %t", applyLeftDir, applyRightDir, forceApply)
	util.LogInfo("Number of args: %d", len(args))

	if len(args) == 0 {
		util.LogInfo("ERROR: No arguments provided to apply command")
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
	hasFlagDirs := applyLeftDir != "" && applyRightDir != ""

	util.LogInfo("Directory detection - hasPositionalDirs: %t, hasFlagDirs: %t", hasPositionalDirs, hasFlagDirs)

	if hasPositionalDirs && hasFlagDirs {
		util.LogInfo("ERROR: Both positional and flag directories provided")
		return fmt.Errorf("cannot use both positional directories and flags - choose one format")
	}

	if hasPositionalDirs {
		// Use positional arguments: apply actions.txt left/ right/
		leftDir = args[1]
		rightDir = args[2]
		util.LogInfo("Using positional directories - leftDir: %q, rightDir: %q", leftDir, rightDir)
	} else if hasFlagDirs {
		// Use flag arguments: apply actions.txt -l left/ -r right/
		leftDir = applyLeftDir
		rightDir = applyRightDir
		util.LogInfo("Using flag directories - leftDir: %q, rightDir: %q", leftDir, rightDir)
	} else {
		util.LogInfo("ERROR: No directories specified in either positional args or flags")
		return fmt.Errorf("directories must be specified either as positional args or flags:\n"+
			"  Positional: apply %s <LEFT_DIR> <RIGHT_DIR>\n"+
			"  Flags:      apply %s --left <LEFT_DIR> --right <RIGHT_DIR>", actionFile, actionFile)
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

	// Safety confirmation unless --force is used
	if !forceApply {
		fmt.Printf("WARNING: This will execute file operations that may modify or delete files.\n")
		fmt.Printf("Action file: %s\n", actionFile)
		fmt.Printf("Left dir:    %s\n", leftDir)
		fmt.Printf("Right dir:   %s\n", rightDir)
		fmt.Printf("\nDo you want to continue? [y/N]: ")

		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" && response != "yes" {
			fmt.Println("Operation cancelled.")
			return nil
		}
	}

	if GetVerboseLevel() >= 1 {
		fmt.Printf("Executing actions:\n")
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

	// Execute actions
	executor := action.NewExecutor(false) // false for real execution
	summary, results, err := executor.ExecuteActions(actionFileData, leftDir, rightDir)
	if err != nil {
		return fmt.Errorf("execution failed: %w", err)
	}

	// Display results
	fmt.Printf("EXECUTION COMPLETE\n")
	fmt.Printf("==================\n")
	fmt.Printf("Action file: %s\n", actionFile)
	fmt.Printf("Left dir:    %s\n", leftDir)
	fmt.Printf("Right dir:   %s\n", rightDir)
	fmt.Printf("\n")

	if len(results) == 0 {
		fmt.Printf("No actions were performed (all actions were set to ignore).\n")
		return nil
	}

	// Show detailed results
	successCount := 0
	for _, result := range results {
		if result.Success {
			if GetVerboseLevel() >= 1 {
				fmt.Printf("✓ %s\n", result.Message)
			}
			successCount++
		} else {
			fmt.Printf("✗ %s\n", result.Message)
			if result.Error != nil {
				fmt.Printf("  Error: %s\n", result.Error.Error())
			}
		}
	}

	fmt.Printf("\nExecution Summary:\n")
	fmt.Printf("==================\n")
	fmt.Printf("Total actions attempted: %d\n", len(results))
	fmt.Printf("Successful actions: %d\n", successCount)
	fmt.Printf("Failed actions: %d\n", len(results)-successCount)

	if summary.FilesCreated > 0 {
		fmt.Printf("Files created: %d\n", summary.FilesCreated)
	}
	if summary.FilesOverwritten > 0 {
		fmt.Printf("Files overwritten: %d\n", summary.FilesOverwritten)
	}
	if summary.FilesDeleted > 0 {
		fmt.Printf("Files deleted: %d\n", summary.FilesDeleted)
	}
	if summary.BytesCopied > 0 {
		fmt.Printf("Data copied: %s\n", util.FormatSize(summary.BytesCopied))
	}

	if len(summary.Errors) > 0 {
		fmt.Printf("\nErrors encountered:\n")
		for _, errMsg := range summary.Errors {
			fmt.Printf("  %s\n", errMsg)
		}
		return fmt.Errorf("execution completed with %d errors", len(summary.Errors))
	}

	fmt.Printf("\nExecution completed successfully!\n")
	return nil
}
