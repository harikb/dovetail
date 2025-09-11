package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/harikb/dovetail/internal/action"
	"github.com/harikb/dovetail/internal/util"
)

// dryrunCmd represents the dry-run command
var dryrunCmd = &cobra.Command{
	Use:   "dry-run <ACTION_FILE>",
	Short: "Preview actions from an action file without executing them",
	Long: `Preview the actions that would be taken when applying an action file.
This shows exactly what files would be copied, deleted, or modified without
actually performing any operations. Use this to verify your action file
before running 'dovetail apply'.

Examples:
  dovetail dry-run actions.txt --left /path/to/source --right /path/to/target
  dovetail dry-run my_sync.txt -l ./src -r ./backup`,
	Args: cobra.ExactArgs(1),
	RunE: runDryRun,
}

var (
	dryRunLeftDir  string
	dryRunRightDir string
)

func init() {
	rootCmd.AddCommand(dryrunCmd)

	// Required directory flags
	dryrunCmd.Flags().StringVarP(&dryRunLeftDir, "left", "l", "", "left directory path (required)")
	dryrunCmd.Flags().StringVarP(&dryRunRightDir, "right", "r", "", "right directory path (required)")

	// Mark as required
	dryrunCmd.MarkFlagRequired("left")
	dryrunCmd.MarkFlagRequired("right")
}

func runDryRun(cmd *cobra.Command, args []string) error {
	actionFile := args[0]

	// Validate action file exists
	if _, err := os.Stat(actionFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("action file does not exist: %s", actionFile)
		}
		return fmt.Errorf("failed to access action file %s: %w", actionFile, err)
	}

	// Validate directories exist
	if err := validateDirectory(dryRunLeftDir); err != nil {
		return fmt.Errorf("left directory: %w", err)
	}
	if err := validateDirectory(dryRunRightDir); err != nil {
		return fmt.Errorf("right directory: %w", err)
	}

	// Convert to absolute paths
	leftDir, err := filepath.Abs(dryRunLeftDir)
	if err != nil {
		return fmt.Errorf("failed to resolve left directory path: %w", err)
	}
	rightDir, err := filepath.Abs(dryRunRightDir)
	if err != nil {
		return fmt.Errorf("failed to resolve right directory path: %w", err)
	}
	actionFile, err = filepath.Abs(actionFile)
	if err != nil {
		return fmt.Errorf("failed to resolve action file path: %w", err)
	}

	if viper.GetBool("verbose") {
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
