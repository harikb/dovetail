package action

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/harikb/dovetail/internal/util"
)

// Executor executes actions from an action file
type Executor struct {
	dryRun bool
}

// NewExecutor creates a new action executor
func NewExecutor(dryRun bool) *Executor {
	return &Executor{
		dryRun: dryRun,
	}
}

// ExecuteActions executes all actions in an action file
func (e *Executor) ExecuteActions(
	actionFile *ActionFile,
	leftDir, rightDir string,
) (*ExecutionSummary, []ExecutionResult, error) {
	summary := &ExecutionSummary{
		TotalActions: len(actionFile.Actions),
	}
	results := make([]ExecutionResult, 0, len(actionFile.Actions))

	for _, action := range actionFile.Actions {
		// Skip ignored actions
		if action.Action == ActionIgnore {
			continue
		}

		result := e.executeAction(action, leftDir, rightDir)
		results = append(results, result)

		// Update summary
		if result.Success {
			summary.SuccessfulActions++
			summary.BytesCopied += result.BytesCopied

			switch action.Action {
			case ActionCopyToRight, ActionCopyToLeft:
				if result.BytesCopied > 0 {
					// Check if file existed before
					if e.fileExists(action, leftDir, rightDir, action.Action) {
						summary.FilesOverwritten++
					} else {
						summary.FilesCreated++
					}
				}
			case ActionDeleteLeft, ActionDeleteRight, ActionDeleteBoth:
				if action.Action == ActionDeleteBoth {
					summary.FilesDeleted += 2
				} else {
					summary.FilesDeleted++
				}
			}
		} else {
			summary.FailedActions++
			if result.Error != nil {
				summary.Errors = append(summary.Errors, fmt.Sprintf("%s: %s", action.RelativePath, result.Error.Error()))
			}
		}
	}

	return summary, results, nil
}

// executeAction executes a single action
func (e *Executor) executeAction(action ActionItem, leftDir, rightDir string) ExecutionResult {
	result := ExecutionResult{
		Action: action,
	}

	leftPath := filepath.Join(leftDir, action.RelativePath)
	rightPath := filepath.Join(rightDir, action.RelativePath)

	switch action.Action {
	case ActionCopyToRight:
		result = e.executeCopy(leftPath, rightPath, action, "left", "right")
	case ActionCopyToLeft:
		result = e.executeCopy(rightPath, leftPath, action, "right", "left")
	case ActionDeleteLeft:
		result = e.executeDelete(leftPath, action, "left")
	case ActionDeleteRight:
		result = e.executeDelete(rightPath, action, "right")
	case ActionDeleteBoth:
		result = e.executeDeleteBoth(leftPath, rightPath, action)
	case ActionIgnore:
		result.Success = true
		result.Message = "Ignored"
	default:
		result.Success = false
		result.Error = fmt.Errorf("unknown action type: %s", action.Action.String())
		result.Message = "Failed: Unknown action"
	}

	return result
}

// executeCopy copies a file from source to destination
func (e *Executor) executeCopy(srcPath, dstPath string, action ActionItem, srcName, dstName string) ExecutionResult {
	result := ExecutionResult{
		Action: action,
	}

	if e.dryRun {
		result.Success = true
		result.Message = fmt.Sprintf("DRY RUN: Would COPY %s -> %s", srcPath, dstPath)
		return result
	}

	// Check if source exists
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		result.Error = fmt.Errorf("source file does not exist or cannot be accessed: %w", err)
		result.Message = fmt.Sprintf("Failed to copy from %s to %s", srcName, dstName)
		return result
	}

	// Create destination directory if needed
	dstDir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		result.Error = fmt.Errorf("failed to create destination directory: %w", err)
		result.Message = fmt.Sprintf("Failed to create directory for %s", dstPath)
		return result
	}

	if srcInfo.IsDir() {
		// Copy directory
		result.Message = fmt.Sprintf("Copied directory from %s to %s", srcName, dstName)
		err = e.copyDirectory(srcPath, dstPath)
	} else {
		// Copy file
		var bytesCopied int64
		bytesCopied, err = e.copyFile(srcPath, dstPath)
		result.BytesCopied = bytesCopied
		result.Message = fmt.Sprintf("Copied file from %s to %s (%s)", srcName, dstName, util.FormatSize(bytesCopied))
	}

	if err != nil {
		result.Error = err
		result.Message = fmt.Sprintf("Failed to copy from %s to %s: %s", srcName, dstName, err.Error())
		return result
	}

	result.Success = true
	return result
}

// executeDelete deletes a file or directory
func (e *Executor) executeDelete(path string, action ActionItem, location string) ExecutionResult {
	result := ExecutionResult{
		Action: action,
	}

	if e.dryRun {
		result.Success = true
		result.Message = fmt.Sprintf("DRY RUN: Would DELETE %s", path)
		return result
	}

	// Check if file exists
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, consider it a success
			result.Success = true
			result.Message = fmt.Sprintf("File already doesn't exist in %s", location)
			return result
		}
		result.Error = fmt.Errorf("cannot access file: %w", err)
		result.Message = fmt.Sprintf("Failed to delete from %s", location)
		return result
	}

	// Delete the file or directory
	if err := os.RemoveAll(path); err != nil {
		result.Error = err
		result.Message = fmt.Sprintf("Failed to delete from %s: %s", location, err.Error())
		return result
	}

	result.Success = true
	if info.IsDir() {
		result.Message = fmt.Sprintf("Deleted directory from %s", location)
	} else {
		result.Message = fmt.Sprintf("Deleted file from %s (%s)", location, util.FormatSize(info.Size()))
	}

	return result
}

// executeDeleteBoth deletes from both locations
func (e *Executor) executeDeleteBoth(leftPath, rightPath string, action ActionItem) ExecutionResult {
	result := ExecutionResult{
		Action: action,
	}

	if e.dryRun {
		result.Success = true
		result.Message = fmt.Sprintf("DRY RUN: Would DELETE %s AND %s", leftPath, rightPath)
		return result
	}

	var errors []string

	// Delete from left
	if err := os.RemoveAll(leftPath); err != nil && !os.IsNotExist(err) {
		errors = append(errors, fmt.Sprintf("left: %s", err.Error()))
	}

	// Delete from right
	if err := os.RemoveAll(rightPath); err != nil && !os.IsNotExist(err) {
		errors = append(errors, fmt.Sprintf("right: %s", err.Error()))
	}

	if len(errors) > 0 {
		result.Error = fmt.Errorf("deletion errors: %s", errors)
		result.Message = fmt.Sprintf("Failed to delete from both locations: %v", errors)
		return result
	}

	result.Success = true
	result.Message = "Deleted from both locations"
	return result
}

// copyFile copies a single file
func (e *Executor) copyFile(srcPath, dstPath string) (int64, error) {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return 0, err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dstPath)
	if err != nil {
		return 0, err
	}
	defer dstFile.Close()

	// Copy file contents
	bytesCopied, err := io.Copy(dstFile, srcFile)
	if err != nil {
		return bytesCopied, err
	}

	// Copy file permissions
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return bytesCopied, nil // File copied, but couldn't preserve permissions
	}

	if err := os.Chmod(dstPath, srcInfo.Mode()); err != nil {
		return bytesCopied, nil // File copied, but couldn't preserve permissions
	}

	return bytesCopied, nil
}

// copyDirectory recursively copies a directory
func (e *Executor) copyDirectory(srcPath, dstPath string) error {
	return filepath.Walk(srcPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Calculate destination path
		relPath, err := filepath.Rel(srcPath, path)
		if err != nil {
			return err
		}
		dstFilePath := filepath.Join(dstPath, relPath)

		if info.IsDir() {
			// Create directory
			return os.MkdirAll(dstFilePath, info.Mode())
		} else {
			// Create directory for file if needed
			dstDir := filepath.Dir(dstFilePath)
			if err := os.MkdirAll(dstDir, 0755); err != nil {
				return err
			}

			// Copy file
			_, err := e.copyFile(path, dstFilePath)
			return err
		}
	})
}

// fileExists checks if a file exists at the target location for the given action
func (e *Executor) fileExists(action ActionItem, leftDir, rightDir string, actionType ActionType) bool {
	var targetPath string

	switch actionType {
	case ActionCopyToRight:
		targetPath = filepath.Join(rightDir, action.RelativePath)
	case ActionCopyToLeft:
		targetPath = filepath.Join(leftDir, action.RelativePath)
	default:
		return false
	}

	_, err := os.Stat(targetPath)
	return err == nil
}
