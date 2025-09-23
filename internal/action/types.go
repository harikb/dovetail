package action

import (
	"fmt"

	"github.com/harikb/dovetail/internal/compare"
)

// ActionType represents the type of action to perform
type ActionType int

const (
	ActionIgnore      ActionType = iota // [i] - Do nothing
	ActionCopyToRight                   // [>] - Copy from left to right
	ActionCopyToLeft                    // [<] - Copy from right to left
	ActionDeleteLeft                    // [x-] - Delete from left
	ActionDeleteRight                   // [-x] - Delete from right
	ActionDeleteBoth                    // [xx] - Delete from both
	ActionPatch                         // [p] - Apply patch from session
)

func (a ActionType) String() string {
	switch a {
	case ActionIgnore:
		return "i"
	case ActionCopyToRight:
		return ">"
	case ActionCopyToLeft:
		return "<"
	case ActionDeleteLeft:
		return "x-"
	case ActionDeleteRight:
		return "-x"
	case ActionDeleteBoth:
		return "xx"
	case ActionPatch:
		return "p"
	default:
		return "?"
	}
}

func (a ActionType) Description() string {
	switch a {
	case ActionIgnore:
		return "Ignore this difference, do nothing"
	case ActionCopyToRight:
		return "Copy file from Left to Right (overwrite)"
	case ActionCopyToLeft:
		return "Copy file from Right to Left (overwrite)"
	case ActionDeleteLeft:
		return "Delete file from Left"
	case ActionDeleteRight:
		return "Delete file from Right"
	case ActionDeleteBoth:
		return "Delete file from both Left and Right"
	case ActionPatch:
		return "Apply patch file generated from hunk session"
	default:
		return "Unknown action"
	}
}

// ParseActionType parses an action string into an ActionType
func ParseActionType(s string) (ActionType, bool) {
	switch s {
	case "i":
		return ActionIgnore, true
	case ">":
		return ActionCopyToRight, true
	case "<":
		return ActionCopyToLeft, true
	case "x-":
		return ActionDeleteLeft, true
	case "-x":
		return ActionDeleteRight, true
	case "xx":
		return ActionDeleteBoth, true
	case "p":
		return ActionPatch, true
	default:
		return ActionIgnore, false
	}
}

// ActionItem represents a single action to be performed
type ActionItem struct {
	Action       ActionType         // The action to perform
	Status       compare.FileStatus // The comparison status that led to this action
	RelativePath string             // Path relative to the root directories
	LeftInfo     *compare.FileInfo  // File info from left directory (may be nil)
	RightInfo    *compare.FileInfo  // File info from right directory (may be nil)
	LineNumber   int                // Line number in the action file (for error reporting)
}

// ActionFile represents a complete action file
type ActionFile struct {
	Header   ActionFileHeader // Header information
	Actions  []ActionItem     // List of actions
	Comments []string         // Additional comments
}

// ActionFileHeader contains metadata about the action file
type ActionFileHeader struct {
	GeneratedAt string // Timestamp when file was generated
	LeftDir     string // Left directory path
	RightDir    string // Right directory path
	Version     string // Tool version
}

// ExecutionResult represents the result of executing an action
type ExecutionResult struct {
	Action      ActionItem // The action that was executed
	Success     bool       // Whether the action succeeded
	Error       error      // Error if action failed
	BytesCopied int64      // Number of bytes copied (for copy operations)
	Message     string     // Human-readable message about what happened
}

// ExecutionSummary contains statistics about action execution
type ExecutionSummary struct {
	TotalActions      int
	SuccessfulActions int
	FailedActions     int
	BytesCopied       int64
	FilesCreated      int
	FilesDeleted      int
	FilesOverwritten  int
	Errors            []string
}

// ValidationError represents an error in action file validation
type ValidationError struct {
	LineNumber int
	Message    string
	Action     string
}

func (ve ValidationError) Error() string {
	return fmt.Sprintf("line %d: %s (action: %s)", ve.LineNumber, ve.Message, ve.Action)
}

// ActionFileError represents an error related to action file processing
type ActionFileError struct {
	Type    string // "parse", "validate", "execute"
	Line    int
	Path    string
	Message string
	Err     error
}

func (afe ActionFileError) Error() string {
	if afe.Line > 0 {
		return fmt.Sprintf("%s error at line %d in %s: %s", afe.Type, afe.Line, afe.Path, afe.Message)
	}
	return fmt.Sprintf("%s error in %s: %s", afe.Type, afe.Path, afe.Message)
}

func (afe ActionFileError) Unwrap() error {
	return afe.Err
}
