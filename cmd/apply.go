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
	Use:   "apply <ACTION_FILE>",
	Short: "Execute actions from an action file",
	Long: `Execute the synchronization actions specified in an action file.
This will perform actual file operations (copy, delete, etc.) based on
the actions you've specified in the action file.

WARNING: This command will modify your filesystem. Always run 'dry-run'
first to preview the actions that will be taken.

Examples:
  dovetail apply actions.txt --left /path/to/source --right /path/to/target
  dovetail apply my_sync.txt -l ./src -r ./backup --force`,
	Args: cobra.ExactArgs(1),
	RunE: runApply,
}

var (
	applyLeftDir  string
	applyRightDir string
	forceApply    bool
)

func init() {
	rootCmd.AddCommand(applyCmd)

	// Required directory flags
	applyCmd.Flags().StringVarP(&applyLeftDir, "left", "l", "", "left directory path (required)")
	applyCmd.Flags().StringVarP(&applyRightDir, "right", "r", "", "right directory path (required)")
	applyCmd.Flags().BoolVar(&forceApply, "force", false, "skip confirmation prompt")

	// Mark as required
	applyCmd.MarkFlagRequired("left")
	applyCmd.MarkFlagRequired("right")
}

func runApply(cmd *cobra.Command, args []string) error {
	actionFile := args[0]

	// Validate action file exists
	if _, err := os.Stat(actionFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("action file does not exist: %s", actionFile)
		}
		return fmt.Errorf("failed to access action file %s: %w", actionFile, err)
	}

	// Validate directories exist
	if err := validateDirectory(applyLeftDir); err != nil {
		return fmt.Errorf("left directory: %w", err)
	}
	if err := validateDirectory(applyRightDir); err != nil {
		return fmt.Errorf("right directory: %w", err)
	}

	// Convert to absolute paths
	leftDir, err := filepath.Abs(applyLeftDir)
	if err != nil {
		return fmt.Errorf("failed to resolve left directory path: %w", err)
	}
	rightDir, err := filepath.Abs(applyRightDir)
	if err != nil {
		return fmt.Errorf("failed to resolve right directory path: %w", err)
	}
	actionFile, err = filepath.Abs(actionFile)
	if err != nil {
		return fmt.Errorf("failed to resolve action file path: %w", err)
	}

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
