package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/harikb/dovetail/internal/util"
)

// cleanupCmd represents the cleanup command
var cleanupCmd = &cobra.Command{
	Use:   "cleanup [LEFT_DIR] [RIGHT_DIR]",
	Short: "Remove dovetail action and patch files",
	Long: `Clean up dovetail-generated files including action files and patch files.
This removes:
- Action files matching pattern: dovetail_actions_*.txt
- Patch files matching pattern: *.*.patch (e.g., file.go.20241224_143022.patch)

By default, searches current directory and specified directories.
Use --force to skip confirmation prompts.

Examples:
  # Clean current directory
  dovetail cleanup
  
  # Clean specific directories
  dovetail cleanup /path/to/left /path/to/right
  
  # Clean with force (no prompts)
  dovetail cleanup --force`,
	Args: cobra.RangeArgs(0, 2), // [LEFT_DIR] [RIGHT_DIR]
	RunE: runCleanup,
}

var (
	cleanupForce bool
)

func init() {
	rootCmd.AddCommand(cleanupCmd)

	cleanupCmd.Flags().BoolVar(&cleanupForce, "force", false, "skip confirmation prompts")
}

func runCleanup(cmd *cobra.Command, args []string) error {
	// Determine directories to search
	var searchDirs []string

	// Always search current directory
	currentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	searchDirs = append(searchDirs, currentDir)

	// Add provided directories
	for _, dir := range args {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			util.LogWarning("Invalid directory path %s: %v", dir, err)
			continue
		}
		if _, err := os.Stat(absDir); os.IsNotExist(err) {
			util.LogWarning("Directory does not exist: %s", absDir)
			continue
		}
		searchDirs = append(searchDirs, absDir)
	}

	util.LogInfo("Searching for cleanup files in %d directories", len(searchDirs))

	// Find files to clean
	actionFiles, patchFiles, err := findCleanupFiles(searchDirs)
	if err != nil {
		return fmt.Errorf("failed to find cleanup files: %w", err)
	}

	totalFiles := len(actionFiles) + len(patchFiles)
	if totalFiles == 0 {
		util.LogInfo("No dovetail files found to clean up.")
		return nil
	}

	// Show what will be cleaned
	fmt.Printf("Found %d files to clean:\n\n", totalFiles)

	if len(actionFiles) > 0 {
		fmt.Printf("Action files (%d):\n", len(actionFiles))
		for _, file := range actionFiles {
			fmt.Printf("  %s\n", file)
		}
		fmt.Println()
	}

	if len(patchFiles) > 0 {
		fmt.Printf("Patch files (%d):\n", len(patchFiles))
		for _, file := range patchFiles {
			fmt.Printf("  %s\n", file)
		}
		fmt.Println()
	}

	// Confirmation prompt (unless --force)
	if !cleanupForce {
		fmt.Printf("Delete all %d files? [y/N]: ", totalFiles)
		var response string
		fmt.Scanln(&response)
		response = strings.ToLower(strings.TrimSpace(response))
		if response != "y" && response != "yes" {
			fmt.Println("Cleanup cancelled.")
			return nil
		}
	}

	// Perform cleanup
	deleted := 0
	errors := 0

	for _, file := range actionFiles {
		if err := os.Remove(file); err != nil {
			util.LogError("Failed to delete action file %s: %v", file, err)
			errors++
		} else {
			util.LogInfo("Deleted action file: %s", file)
			deleted++
		}
	}

	for _, file := range patchFiles {
		if err := os.Remove(file); err != nil {
			util.LogError("Failed to delete patch file %s: %v", file, err)
			errors++
		} else {
			util.LogInfo("Deleted patch file: %s", file)
			deleted++
		}
	}

	// Summary
	if errors == 0 {
		util.LogInfo("✅ Cleanup complete. Deleted %d files.", deleted)
	} else {
		util.LogWarning("⚠ Cleanup finished with %d errors. Deleted %d files.", errors, deleted)
		return fmt.Errorf("cleanup completed with %d errors", errors)
	}

	return nil
}

// findCleanupFiles searches for action and patch files in the given directories
func findCleanupFiles(searchDirs []string) ([]string, []string, error) {
	var actionFiles []string
	var patchFiles []string

	// Regex patterns
	actionPattern := regexp.MustCompile(`^dovetail_actions_\d{8}_\d{6}\.txt$`)
	patchPattern := regexp.MustCompile(`^.+\.\d{8}_\d{6}\.patch$`)

	util.LogInfo("Starting search in directories: %v", searchDirs)
	for _, dir := range searchDirs {
		util.LogInfo("Walking directory: %s", dir)
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				util.LogWarning("Error accessing %s: %v", path, err)
				return nil // Continue walking
			}

			// Skip directories (but allow recursion)
			if info.IsDir() {
				return nil
			}

			fileName := info.Name()
			util.LogInfo("Examining file: %s (name: %s)", path, fileName)

			// Check for action files
			if actionPattern.MatchString(fileName) {
				util.LogInfo("Found action file: %s", path)
				actionFiles = append(actionFiles, path)
				return nil
			}

			// Check for patch files
			if patchPattern.MatchString(fileName) {
				util.LogInfo("Found patch file: %s", path)
				patchFiles = append(patchFiles, path)
				return nil
			}

			return nil
		})

		if err != nil {
			return nil, nil, fmt.Errorf("failed to walk directory %s: %w", dir, err)
		}
	}

	return actionFiles, patchFiles, nil
}
