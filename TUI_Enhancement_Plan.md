# TUI Enhancement Plan

## Current State Analysis

### TUI Functionality
- **Sorting**: Currently displays results as-is from comparison engine, no directory grouping
- **Navigation**: `q` always quits, `esc` only goes back from diff view to file list  
- **Actions**: TUI is read-only, action files are generated separately and edited externally
- **Diff Colors**: Already uses `colordiff` if available, falls back to plain `diff`
- **Current Action Types**: `i`, `>`, `<`, `x-`, `-x`, `xx`

## Requirements Summary

1. **Sort Results**: Directory-aware sorting that groups files in same directory
2. **Fix Navigation**: Make `q` behave like `esc` in diff view (go back to list)
3. **Interactive Action Marking**: Allow `>`, `<`, `i`, `x` keys to mark actions, track state, save action file
4. **Colorize Diff Output**: Enhance diff display with better colors and formatting
5. **Line-by-line Copying**: Advanced feature for selective copying within files (stretch goal)

## Implementation Phases

### Phase 1: Quick Wins ✅ (Implement immediately)

#### 1.1 Directory-Aware Sorting
- **Location**: `internal/tui/app.go` - `NewApp()` function
- **Implementation**: Sort `filteredResults` by directory path, then filename
- **Logic**: Split paths by `/`, sort by directory first, then filename
- **Benefit**: Natural grouping of files in same directory

#### 1.2 Fix Navigation Behavior  
- **Location**: `internal/tui/app.go` - `handleKeyPress()` function
- **Current Issue**: `q` always quits application
- **New Behavior**: 
  - In diff view: `q` and `esc` both go back to file list
  - In file list: `q` quits application
- **Implementation**: Update key handler logic for `q` key

#### 1.3 Enhance Diff Colorization
- **Location**: `internal/tui/app.go` - `loadDiff()` function  
- **Improvements**:
  - Add `--color=always` to diff commands
  - Use better diff flags for readability
  - Ensure colors display properly in TUI context
  - Consider adding context lines with `-C3` flag

### Phase 2: Interactive Actions (Major Feature)

#### 2.1 TUI Model Extensions
```go
// Extend TUI Model:
type Model struct {
    // ... existing fields
    fileActions  map[string]ActionType  // Track action per file path
    hasChanges   bool                   // Whether any actions were modified
    actionFile   string                 // Path for saving action file
    pendingSave  bool                   // Whether save dialog is active
}
```

#### 2.2 Action Tracking System
- **New Key Handlers**:
  - `>`: ActionCopyToRight (if applicable)
  - `<`: ActionCopyToLeft (if applicable)  
  - `i`: ActionIgnore
  - `x`: ActionDelete (only for StatusOnlyLeft/StatusOnlyRight)
  - `s`: Save action file
- **Validation Logic**: Only allow valid actions based on file status
- **Visual Feedback**: Show current action in file list display

#### 2.3 Simplified Delete Actions
- **Current**: `x-` (delete left), `-x` (delete right), `xx` (delete both)
- **New**: Single `x` action, only available when one side is empty
- **Rationale**: Prevents accidental deletion when both sides exist
- **Benefit**: Simpler, safer delete logic

#### 2.4 Action File Generation from TUI
- **Save Command**: `s` key triggers save dialog/confirmation
- **Output**: Standard action file format with user's selections
- **Integration**: Reuse existing action file generation code
- **Comments**: Include metadata about TUI interaction

### Phase 3: Advanced Features (Future)

#### 3.1 Line-by-line Copying
- **New Action Type**: `[p]` for patch operations
- **Syntax**: `[p] : MODIFIED : file.txt : 45:68->70`
- **Meaning**: Copy lines 45-68 from left file to line 70 in right file
- **Requirements**:
  - Parse unified diff format
  - Track line numbers and mappings
  - Complex TUI state for line selection
  - Implement selective patching logic

#### 3.2 Enhanced Diff Navigation
- **Scroll Support**: Page up/down in diff view
- **Search**: Find text within diffs
- **Line Selection**: Visual line range selection for copying

## Technical Implementation Details

### Directory-Aware Sorting Algorithm
```go
// Sort by directory depth first, then alphabetically
sort.Slice(filteredResults, func(i, j int) bool {
    pathA := strings.Split(filteredResults[i].RelativePath, "/")
    pathB := strings.Split(filteredResults[j].RelativePath, "/")
    
    // Compare directory paths
    minLen := min(len(pathA)-1, len(pathB)-1)
    for k := 0; k < minLen; k++ {
        if pathA[k] != pathB[k] {
            return pathA[k] < pathB[k]
        }
    }
    
    // If one is in subdirectory of other, shorter path first
    if len(pathA) != len(pathB) {
        return len(pathA) < len(pathB)
    }
    
    // Same directory, sort by filename
    return pathA[len(pathA)-1] < pathB[len(pathB)-1]
})
```

### Interactive Action State Management
```go
// Initialize default actions based on file status
func (m *Model) initializeDefaultActions() {
    m.fileActions = make(map[string]ActionType)
    for _, result := range m.results {
        m.fileActions[result.RelativePath] = ActionIgnore // Safe default
    }
}

// Validate action is allowed for file status  
func (m *Model) isActionValid(action ActionType, status compare.FileStatus) bool {
    switch action {
    case ActionCopyToRight:
        return status == compare.StatusOnlyLeft || status == compare.StatusModified
    case ActionCopyToLeft:
        return status == compare.StatusOnlyRight || status == compare.StatusModified
    case ActionDelete:
        return status == compare.StatusOnlyLeft || status == compare.StatusOnlyRight
    case ActionIgnore:
        return true
    default:
        return false
    }
}
```

## Migration Strategy

### Backward Compatibility
- Existing action file format remains supported
- CLI diff/apply workflow unchanged
- TUI is additive enhancement

### Phased Rollout
1. **Phase 1**: Improve usability without breaking changes
2. **Phase 2**: Add interactive features as opt-in
3. **Phase 3**: Advanced features based on user feedback

## Success Criteria

### Phase 1
- [ ] Files grouped by directory in TUI display
- [ ] Intuitive navigation (q goes back in diff view)
- [ ] Better colored diff output

### Phase 2  
- [ ] Interactive action selection with visual feedback
- [ ] Save action file directly from TUI
- [ ] Simplified delete action logic
- [ ] Maintain backward compatibility

### Phase 3
- [ ] Line-level copying functionality
- [ ] Enhanced diff navigation and search
- [ ] Parity with VSCode DiffFolders critical features

## Timeline Estimate
- **Phase 1**: 1-2 hours (immediate implementation)
- **Phase 2**: 4-6 hours (major feature development)
- **Phase 3**: 8-12 hours (complex feature requiring careful design)
