package tui

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/harikb/dovetail/internal/action"
	"github.com/harikb/dovetail/internal/compare"
	"github.com/harikb/dovetail/internal/util"
)

// getProfilingCleanup provides access to profiling cleanup function
// This is a weak dependency to avoid import cycles
var getProfilingCleanup = func() func() { return nil }

// SetProfilingCleanup allows external packages to set the cleanup function
func SetProfilingCleanup(cleanup func()) {
	getProfilingCleanup = func() func() { return cleanup }
}

// getVisibleFileListLines calculates how many file lines can fit in the viewport
func (m Model) getVisibleFileListLines() int {
	// Reserve space for header, directories, summary, and footer
	// Approximate: Header(3) + Dirs(3) + Summary(2) + Footer(5) = 13 lines
	headerLines := 13
	if m.windowHeight <= headerLines {
		return 1 // Always show at least 1 line
	}
	return m.windowHeight - headerLines
}

// getVisibleDiffLines calculates how many diff lines can fit in the diff viewport
func (m Model) getVisibleDiffLines() int {
	// Reserve space for header and footer in diff view
	// Approximate: Header(3) + Footer(3) = 6 lines
	headerLines := 6
	if m.windowHeight <= headerLines {
		return 1 // Always show at least 1 line
	}
	return m.windowHeight - headerLines
}

// DiffHunk represents a parsed hunk from unified diff
type DiffHunk struct {
	Header     string   // "@@ -10,3 +10,4 @@"
	LeftStart  int      // Starting line number in left file
	LeftCount  int      // Number of lines in left file
	RightStart int      // Starting line number in right file
	RightCount int      // Number of lines in right file
	Lines      []string // The actual hunk content lines
	Applied    bool     // Whether this hunk has been applied
}

// App represents the main TUI application
type App struct {
	model Model
}

// NewApp creates a new TUI application
func NewApp(results []compare.ComparisonResult, summary *compare.ComparisonSummary, leftDir, rightDir string, ignoreWhitespace bool) *App {
	// Filter out identical files for the UI (focus on differences)
	var filteredResults []compare.ComparisonResult
	for _, result := range results {
		if result.Status != compare.StatusIdentical {
			filteredResults = append(filteredResults, result)
		}
	}

	// Sort results alphabetically by path for consistent ordering
	sort.Slice(filteredResults, func(i, j int) bool {
		return filteredResults[i].RelativePath < filteredResults[j].RelativePath
	})

	// Generate session ID once for this TUI session
	sessionID := time.Now().Format("20060102_150405")

	model := Model{
		results:             filteredResults,
		summary:             summary,
		leftDir:             leftDir,
		rightDir:            rightDir,
		cursor:              0,
		showingDiff:         false,
		currentDiff:         "",
		windowWidth:         80,
		windowHeight:        24,
		viewportTop:         0,
		sessionID:           sessionID,
		fileActions:         make(map[string]action.ActionType),
		hasUnsavedChanges:   false,
		hasUnappliedChanges: false,
		showingSave:         false,
		ignoreWhitespace:    ignoreWhitespace,
		detectedPatchFiles:  summary.DetectedPatchFiles,
		showingPatchCleanup: len(summary.DetectedPatchFiles) > 0, // Show cleanup prompt if patch files detected
	}

	// Initialize default actions (all ignore for safety)
	for _, result := range filteredResults {
		model.fileActions[result.RelativePath] = action.ActionIgnore
	}

	return &App{model: model}
}

// Run starts the TUI application
func (a *App) Run() error {
	p := tea.NewProgram(a.model, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// Model represents the state of the TUI
type Model struct {
	results      []compare.ComparisonResult
	summary      *compare.ComparisonSummary
	leftDir      string
	rightDir     string
	cursor       int    // Currently selected file index
	showingDiff  bool   // Whether we're showing a diff or file list
	currentDiff  string // Current diff content
	windowWidth  int
	windowHeight int
	viewportTop  int // First visible line in the viewport
	err          error

	// Session and action tracking
	sessionID           string                       // Unique session ID for this TUI session
	fileActions         map[string]action.ActionType // Track action per file path
	hasUnsavedChanges   bool                         // Whether there are unsaved modifications
	hasUnappliedChanges bool                         // Whether there's a valid action file ready to execute
	showingSave         bool                         // Whether save confirmation is shown
	saveMessage         string                       // Save result message

	// Discard changes confirmation
	showingDiscardConfirm bool   // Whether discard confirmation is shown
	discardFilePath       string // File path for discard confirmation

	// Quit confirmation with unsaved changes
	showingQuitConfirm bool // Whether quit confirmation is shown

	// Search functionality
	searchMode    bool   // Are we in search input mode?
	searchString  string // Current search term
	searchMatches []int  // Indices of matching files
	matchIndex    int    // Current match position (0-based)

	// Hunk mode functionality
	hunkMode      bool       // Are we in hunk editing mode?
	hunks         []DiffHunk // Parsed hunks from current diff
	currentHunk   int        // Currently selected hunk (0-based)
	tempDir       string     // Path to temp directory for this session
	tempLeftFile  string     // Path to temp left clone (if created)
	tempRightFile string     // Path to temp right clone (if created)
	appliedHunks  []bool     // Track which hunks have been applied (UI only)

	// Patch status for visual feedback
	leftPatchApplied  bool // Whether left side has existing patch applied
	rightPatchApplied bool // Whether right side has existing patch applied

	// Diff options
	ignoreWhitespace bool // Whether to ignore whitespace in diffs
	diffViewportTop  int  // First visible line in diff viewport
	reversedDiff     bool // Whether to show diff in reverse (RIGHT→LEFT)

	// Cleanup prompt after successful apply
	showingCleanup      bool   // Whether cleanup confirmation is shown
	cleanupOldSessionID string // Old session ID for cleanup
	cleanupActionFile   string // Action file that was applied

	// Patch file cleanup prompt
	showingPatchCleanup bool                    // Whether patch cleanup confirmation is shown
	detectedPatchFiles  []compare.PatchFileInfo // Patch files detected during scan
}

// Init initializes the model (required by bubbletea)
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the model state
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.windowWidth = msg.Width
		m.windowHeight = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case diffLoadedMsg:
		m.currentDiff = string(msg)
		m.showingDiff = true
		m.diffViewportTop = 0 // Reset scroll position for new diff
		return m, nil

	case diffErrorMsg:
		m.err = error(msg)
		m.showingDiff = true // Show the error in diff view
		return m, nil

	case dryRunCompletedMsg:
		if msg.success {
			m.saveMessage = "Dry-run completed successfully."
		} else {
			m.saveMessage = fmt.Sprintf("Dry-run failed: %v", msg.error)
		}
		return m, nil

	case applyCompletedMsg:
		if msg.success {
			m.saveMessage = "Apply completed successfully. Refreshing comparison..."
			// Reset state and refresh comparison
			return m.refreshAfterApply(msg.filename)
		} else {
			m.saveMessage = fmt.Sprintf("Apply failed: %v", msg.error)
		}
		return m, nil

	case cleanupCompletedMsg:
		if msg.success {
			m.saveMessage = "✅ Cleanup completed successfully."
		} else {
			m.saveMessage = fmt.Sprintf("Cleanup failed: %v", msg.error)
		}
		return m, nil
	}

	return m, nil
}

// handleKeyPress processes keyboard input
func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle search mode input first
	if m.searchMode {
		return m.handleSearchInput(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		// Call profiling cleanup before quitting
		if cleanup := getProfilingCleanup(); cleanup != nil {
			cleanup()
		}
		return m, tea.Quit

	case "q":
		if m.showingDiff {
			// In diff view, q goes back to file list (same as esc)
			if m.hunkMode {
				util.DebugPrintf("Q pressed in hunk mode - calling exitHunkMode()")
				// Exit hunk mode first, then return to file list
				newModel, cmd := m.exitHunkMode()
				newModel.showingDiff = false
				newModel.currentDiff = ""
				newModel.err = nil
				newModel.diffViewportTop = 0
				newModel.reversedDiff = false // Reset revert mode when returning to file list
				newModel.saveMessage = ""     // Clear any revert mode messages
				return newModel, cmd
			}
			m.showingDiff = false
			m.currentDiff = ""
			m.err = nil
			m.diffViewportTop = 0  // Reset scroll position
			m.reversedDiff = false // Reset revert mode when returning to file list
			m.saveMessage = ""     // Clear any revert mode messages
		} else {
			// In file list, q quits the application
			if m.hasUnsavedChanges {
				// Show quit confirmation if there are unsaved changes
				m.showingQuitConfirm = true
				return m, nil
			} else {
				// Call profiling cleanup before quitting
				if cleanup := getProfilingCleanup(); cleanup != nil {
					cleanup()
				}
				return m, tea.Quit
			}
		}

	case "esc":
		if m.showingDiscardConfirm {
			// Cancel discard confirmation
			return m.handleDiscardConfirm(false)
		} else if m.showingCleanup {
			// Cancel cleanup confirmation
			return m.handleCleanupConfirm(false)
		} else if m.showingPatchCleanup {
			// Cancel patch cleanup confirmation
			return m.handlePatchCleanupConfirm(false)
		} else if m.showingQuitConfirm {
			// Cancel quit confirmation
			return m.handleQuitConfirm(false)
		} else if m.hunkMode {
			util.DebugPrintf("ESC pressed in hunk mode - calling exitHunkMode()")
			// Exit hunk mode, save patches if any changes made
			return m.exitHunkMode()
		} else if m.showingDiff {
			// Return to file list
			m.showingDiff = false
			m.currentDiff = ""
			m.err = nil
			m.diffViewportTop = 0  // Reset scroll position
			m.reversedDiff = false // Reset revert mode when returning to file list
			m.saveMessage = ""     // Clear any revert mode messages
		} else if m.searchString != "" {
			// Clear search in normal mode
			m.searchString = ""
			m.searchMatches = nil
			m.matchIndex = 0
		}
		// Note: ESC no longer quits app in normal mode - too dangerous

	case "y":
		// Handle discard confirmation
		if m.showingDiscardConfirm {
			return m.handleDiscardConfirm(true)
		}
		// Handle cleanup confirmation
		if m.showingCleanup {
			return m.handleCleanupConfirm(true)
		}
		// Handle patch cleanup confirmation
		if m.showingPatchCleanup {
			return m.handlePatchCleanupConfirm(true)
		}
		// Handle quit confirmation
		if m.showingQuitConfirm {
			return m.handleQuitConfirm(true)
		}

	case "up", "k":
		if m.showingDiff {
			// Scroll up in diff view
			if m.diffViewportTop > 0 {
				m.diffViewportTop--
			}
		} else if m.cursor > 0 {
			m.cursor--
			// Auto-scroll viewport if needed
			if m.cursor < m.viewportTop {
				m.viewportTop = m.cursor
			}
		}

	case "down", "j":
		if m.showingDiff {
			// Scroll down in diff view
			diffLines := strings.Split(m.currentDiff, "\n")
			visibleDiffLines := m.getVisibleDiffLines()
			if m.diffViewportTop+visibleDiffLines < len(diffLines) {
				m.diffViewportTop++
			}
		} else if m.cursor < len(m.results)-1 {
			m.cursor++
			// Auto-scroll viewport if needed
			visibleLines := m.getVisibleFileListLines()
			if m.cursor >= m.viewportTop+visibleLines {
				m.viewportTop = m.cursor - visibleLines + 1
			}
		}

	case "enter", "space", " ":
		if !m.showingDiff && !m.showingSave && !m.showingDiscardConfirm && !m.showingCleanup && !m.showingPatchCleanup && !m.showingQuitConfirm && len(m.results) > 0 {
			// Load diff for selected file - reset revert mode for new file
			m.reversedDiff = false
			m.saveMessage = "" // Clear any revert mode messages
			return m, m.loadDiff()
		} else if m.showingDiff && !m.hunkMode {
			// Enter hunk mode for selective editing
			return m.enterHunkMode(), nil
		}

	case "pgup", "page_up":
		if m.showingDiff {
			// Page up in diff view
			visibleDiffLines := m.getVisibleDiffLines()
			if m.diffViewportTop >= visibleDiffLines {
				m.diffViewportTop -= visibleDiffLines
			} else {
				m.diffViewportTop = 0
			}
		} else if len(m.results) > 0 {
			// Page up - jump by visible lines
			visibleLines := m.getVisibleFileListLines()
			if m.cursor >= visibleLines {
				m.cursor -= visibleLines
				m.viewportTop = m.cursor
			} else {
				m.cursor = 0
				m.viewportTop = 0
			}
		}

	case "pgdown", "page_down":
		if m.showingDiff {
			// Page down in diff view
			diffLines := strings.Split(m.currentDiff, "\n")
			visibleDiffLines := m.getVisibleDiffLines()
			if m.diffViewportTop+visibleDiffLines*2 < len(diffLines) {
				m.diffViewportTop += visibleDiffLines
			} else {
				// Go to last page
				m.diffViewportTop = len(diffLines) - visibleDiffLines
				if m.diffViewportTop < 0 {
					m.diffViewportTop = 0
				}
			}
		} else if len(m.results) > 0 {
			// Page down - jump by visible lines
			visibleLines := m.getVisibleFileListLines()
			if m.cursor+visibleLines < len(m.results) {
				m.cursor += visibleLines
				m.viewportTop = m.cursor
			} else {
				m.cursor = len(m.results) - 1
				// Adjust viewport to show last page
				m.viewportTop = len(m.results) - visibleLines
				if m.viewportTop < 0 {
					m.viewportTop = 0
				}
			}
		}

	// Interactive action keys - file list view or hunk mode
	case ">":
		// Only available in file list mode for whole-file copy operations
		if !m.showingDiff && !m.showingSave && !m.showingDiscardConfirm && !m.showingCleanup && !m.showingPatchCleanup && !m.showingQuitConfirm && len(m.results) > 0 {
			util.DebugPrintf("Setting file action COPY-TO-RIGHT")
			return m.setAction(action.ActionCopyToRight), nil
		}
	case "<":
		if m.hunkMode && len(m.hunks) > 0 {
			util.DebugPrintf("Applying visible hunk (<) - currentHunk=%d", m.currentHunk)
			// Apply current hunk as displayed (user controls direction with 'r')
			return m.applyCurrentHunk()
		} else if !m.showingDiff && !m.showingSave && !m.showingDiscardConfirm && !m.showingCleanup && !m.showingPatchCleanup && len(m.results) > 0 {
			util.DebugPrintf("Setting file action COPY-TO-LEFT")
			return m.setAction(action.ActionCopyToLeft), nil
		}
	case "i":
		if !m.showingDiff && !m.showingSave && !m.showingDiscardConfirm && !m.showingCleanup && !m.showingPatchCleanup && !m.showingQuitConfirm && len(m.results) > 0 {
			result := m.results[m.cursor]
			currentAction := m.fileActions[result.RelativePath]

			// If file has patch status, show discard confirmation
			if currentAction == action.ActionPatch {
				m.showingDiscardConfirm = true
				m.discardFilePath = result.RelativePath
				return m, nil
			} else {
				// Normal ignore action
				return m.setAction(action.ActionIgnore), nil
			}
		}
	case "x":
		if !m.showingDiff && !m.showingSave && !m.showingDiscardConfirm && !m.showingCleanup && !m.showingPatchCleanup && !m.showingQuitConfirm && len(m.results) > 0 {
			// Use simplified delete - only valid for files that exist on one side only
			result := m.results[m.cursor]
			var deleteAction action.ActionType
			switch result.Status {
			case compare.StatusOnlyLeft:
				deleteAction = action.ActionDeleteLeft
			case compare.StatusOnlyRight:
				deleteAction = action.ActionDeleteRight
			default:
				// Show error for files that exist on both sides
				m.saveMessage = fmt.Sprintf("Delete not valid for %s files (exists on both sides)",
					result.Status.String())
				return m, nil
			}
			return m.setAction(deleteAction), nil
		}
	case "s":
		if !m.showingDiff && !m.showingSave && !m.showingDiscardConfirm && !m.showingCleanup && !m.showingPatchCleanup && !m.showingQuitConfirm {
			return m.saveActionFile()
		}
	case "d":
		if !m.showingDiff && !m.showingSave && !m.showingDiscardConfirm && !m.showingCleanup && !m.showingPatchCleanup && !m.showingQuitConfirm && m.hasUnappliedChanges {
			return m.runDryRun()
		}
	case "a":
		if !m.showingDiff && !m.showingSave && !m.showingDiscardConfirm && !m.showingCleanup && !m.showingPatchCleanup && !m.showingQuitConfirm && m.hasUnappliedChanges {
			return m.runApply()
		}

	// Search functionality
	case "/":
		if !m.showingDiff && !m.showingSave && !m.showingDiscardConfirm && !m.showingCleanup && !m.showingPatchCleanup && !m.showingQuitConfirm {
			m.searchMode = true
			m.searchString = ""
		}
	case "n":
		if m.showingDiscardConfirm {
			// Handle discard confirmation (no)
			return m.handleDiscardConfirm(false)
		} else if m.showingCleanup {
			// Handle cleanup confirmation (no)
			return m.handleCleanupConfirm(false)
		} else if m.showingPatchCleanup {
			// Handle patch cleanup confirmation (no)
			return m.handlePatchCleanupConfirm(false)
		} else if m.showingQuitConfirm {
			// Handle quit confirmation (no)
			return m.handleQuitConfirm(false)
		} else if m.hunkMode && len(m.hunks) > 0 {
			// Next hunk in hunk mode
			if m.currentHunk < len(m.hunks)-1 {
				m.currentHunk++
			}
		} else if !m.showingDiff && !m.showingSave {
			if len(m.searchMatches) > 0 {
				m = m.nextSearchMatch()
			} else if m.searchString == "" {
				m.saveMessage = "No active search"
			}
		}
	case "N", "p":
		if m.hunkMode && len(m.hunks) > 0 {
			// Previous hunk in hunk mode
			if m.currentHunk > 0 {
				m.currentHunk--
			}
		} else if msg.String() == "N" && !m.showingDiff && !m.showingSave {
			if len(m.searchMatches) > 0 {
				m = m.prevSearchMatch()
			} else if m.searchString == "" {
				m.saveMessage = "No active search"
			}
		}

	case "r":
		if m.showingDiff {
			// Toggle reverse diff mode
			m.reversedDiff = !m.reversedDiff
			// Reset hunk mode state since hunks will be different in new direction
			if m.hunkMode {
				m.hunkMode = false
				m.hunks = nil
				m.currentHunk = 0
				m.appliedHunks = nil
			}
			if m.reversedDiff {
				m.saveMessage = "⚠ REVERT MODE enabled - applying changes RIGHT → LEFT"
			} else {
				m.saveMessage = "Normal mode restored - applying changes LEFT → RIGHT"
			}
			// Reload diff with new direction
			return m, m.loadDiff()
		} else {
			// In file list, just clear any error (refresh)
			m.err = nil
		}
	}

	return m, nil
}

// Custom message types for async operations
type diffLoadedMsg []byte
type diffErrorMsg error

// loadDiff loads the diff for the currently selected file
func (m Model) loadDiff() tea.Cmd {
	if m.cursor >= len(m.results) {
		return nil
	}

	result := m.results[m.cursor]

	return func() tea.Msg {
		// Only try to diff actual files, not directories or missing files
		if result.Status == compare.StatusModified &&
			result.LeftInfo != nil && !result.LeftInfo.IsDir &&
			result.RightInfo != nil && !result.RightInfo.IsDir {

			// STEP 1: Apply any existing session patches to temp files first
			if err := m.applyExistingPatches(result); err != nil {
				return diffErrorMsg(fmt.Errorf("failed to apply existing patches: %w", err))
			}

			// Use temp files if they exist (for hunk mode), otherwise use originals
			leftPath := fmt.Sprintf("%s/%s", m.leftDir, result.RelativePath)
			if m.tempLeftFile != "" {
				leftPath = m.tempLeftFile
			}

			rightPath := fmt.Sprintf("%s/%s", m.rightDir, result.RelativePath)
			if m.tempRightFile != "" {
				rightPath = m.tempRightFile
			}

			// Use Unix diff command with enhanced colorization and formatting
			// Respect reversedDiff flag for direction
			var firstPath, secondPath string
			if m.reversedDiff {
				firstPath, secondPath = rightPath, leftPath // RIGHT → LEFT
			} else {
				firstPath, secondPath = leftPath, rightPath // LEFT → RIGHT (default)
			}

			var cmd *exec.Cmd
			args := []string{"--color=always", "-u", "-U3"}
			if m.ignoreWhitespace {
				args = append(args, "-w") // Ignore whitespace differences
			}
			args = append(args, firstPath, secondPath)

			if _, err := exec.LookPath("colordiff"); err == nil {
				// Use colordiff with color output and unified format with 3 lines of context
				cmd = exec.Command("colordiff", args...)
			} else {
				// Fall back to regular diff with unified format and 3 lines of context
				// Remove --color=always for regular diff
				regularArgs := []string{"-u", "-U3"}
				if m.ignoreWhitespace {
					regularArgs = append(regularArgs, "-w")
				}
				regularArgs = append(regularArgs, firstPath, secondPath)
				cmd = exec.Command("diff", regularArgs...)
			}

			output, err := cmd.Output()
			if err != nil {
				// diff returns exit code 1 when files differ (normal case)
				if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
					return diffLoadedMsg(output)
				}
				return diffErrorMsg(fmt.Errorf("failed to generate diff: %w", err))
			}

			return diffLoadedMsg(output)
		}

		// For non-diff-able items, show file contents or basic info
		var info string
		var filePath string

		switch result.Status {
		case compare.StatusOnlyLeft:
			if result.LeftInfo != nil && !result.LeftInfo.IsDir {
				// Show actual file contents for ONLY_LEFT files
				filePath = filepath.Join(m.leftDir, result.RelativePath)
				content, err := os.ReadFile(filePath)
				if err != nil {
					info = fmt.Sprintf("File: %s\nStatus: %s\n\nError reading file: %v", result.RelativePath, result.Status.String(), err)
				} else {
					info = fmt.Sprintf("File: %s\nStatus: Only in LEFT directory\nSize: %d bytes\n\n--- Content ---\n%s",
						result.RelativePath, result.LeftInfo.Size, string(content))
				}
			} else {
				// Directory or error case
				info = fmt.Sprintf("File: %s\nStatus: %s\n\nOnly exists in LEFT directory", result.RelativePath, result.Status.String())
				if result.LeftInfo != nil && result.LeftInfo.IsDir {
					info += "\nType: Directory"
				}
			}
		case compare.StatusOnlyRight:
			if result.RightInfo != nil && !result.RightInfo.IsDir {
				// Show actual file contents for ONLY_RIGHT files
				filePath = filepath.Join(m.rightDir, result.RelativePath)
				content, err := os.ReadFile(filePath)
				if err != nil {
					info = fmt.Sprintf("File: %s\nStatus: %s\n\nError reading file: %v", result.RelativePath, result.Status.String(), err)
				} else {
					info = fmt.Sprintf("File: %s\nStatus: Only in RIGHT directory\nSize: %d bytes\n\n--- Content ---\n%s",
						result.RelativePath, result.RightInfo.Size, string(content))
				}
			} else {
				// Directory or error case
				info = fmt.Sprintf("File: %s\nStatus: %s\n\nOnly exists in RIGHT directory", result.RelativePath, result.Status.String())
				if result.RightInfo != nil && result.RightInfo.IsDir {
					info += "\nType: Directory"
				}
			}
		default:
			// Other statuses - show basic info
			info = fmt.Sprintf("File: %s\nStatus: %s", result.RelativePath, result.Status.String())
		}

		return diffLoadedMsg([]byte(info))
	}
}

// View renders the current state of the UI
func (m Model) View() string {
	if m.showingDiff {
		return m.viewDiff()
	}
	return m.viewFileList()
}

// viewFileList renders the file list view
func (m Model) viewFileList() string {
	var b strings.Builder

	// Clear screen for clean display (especially when returning from diff view)
	b.WriteString("\033[2J") // Clear entire screen
	b.WriteString("\033[H")  // Move cursor to top-left corner

	// Header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	b.WriteString(headerStyle.Render("Dovetail Directory Comparison"))
	b.WriteString("\n\n")

	// Directory info
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	b.WriteString(infoStyle.Render(fmt.Sprintf("Left:  %s", m.leftDir)))
	b.WriteString("\n")
	b.WriteString(infoStyle.Render(fmt.Sprintf("Right: %s", m.rightDir)))
	b.WriteString("\n\n")

	// Summary
	if m.summary != nil {
		b.WriteString(infoStyle.Render(fmt.Sprintf("Files: %d total (%d different, %d identical)",
			m.summary.TotalFiles,
			m.summary.ModifiedFiles+m.summary.OnlyLeftFiles+m.summary.OnlyRightFiles,
			m.summary.IdenticalFiles)))
		b.WriteString("\n\n")
	}

	// File list
	if len(m.results) == 0 {
		b.WriteString(infoStyle.Render("No differences found."))
	} else {
		b.WriteString(lipgloss.NewStyle().Bold(true).Render("Files with differences:"))
		b.WriteString("\n\n")

		// Calculate viewport boundaries for performance with large lists
		visibleLines := m.getVisibleFileListLines()
		viewportEnd := m.viewportTop + visibleLines
		if viewportEnd > len(m.results) {
			viewportEnd = len(m.results)
		}

		// Show viewport info for large lists
		if len(m.results) > visibleLines {
			viewportInfo := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
			b.WriteString(viewportInfo.Render(fmt.Sprintf("Showing %d-%d of %d files",
				m.viewportTop+1, viewportEnd, len(m.results))))
			b.WriteString("\n\n")
		}

		// Only render visible items (CRITICAL for performance)
		for i := m.viewportTop; i < viewportEnd; i++ {
			result := m.results[i]
			statusColor := getStatusColor(result.Status)
			statusStyle := lipgloss.NewStyle().Foreground(statusColor)

			// Get current action for this file
			currentAction := m.fileActions[result.RelativePath]
			actionColor := getActionColor(currentAction)
			actionStyle := lipgloss.NewStyle().Foreground(actionColor)

			// Format: [ACTION] STATUS file_path with search highlighting
			filePath := result.RelativePath
			if m.searchString != "" {
				filePath = highlightSearch(result.RelativePath, m.searchString)
			}

			// Get action display string
			actionStr := currentAction.String()
			if currentAction == action.ActionPatch {
				// Just show "p" like in action file
				actionStr = "p"
			}

			var line string
			if i == m.cursor {
				// Highlight selected line
				selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("8")).Foreground(lipgloss.Color("15"))
				line = selectedStyle.Render(fmt.Sprintf("▶ [%s] %-12s %s",
					actionStr, result.Status.String(), filePath))
			} else {
				// Color the action and status separately
				actionPart := actionStyle.Render(fmt.Sprintf("  [%s]", actionStr))
				statusPart := statusStyle.Render(fmt.Sprintf(" %-12s", result.Status.String()))
				line = actionPart + statusPart + " " + filePath
			}

			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	// Footer/Help with interactive commands
	b.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	if len(m.results) > 0 {
		if m.searchMode {
			// Show search prompt
			searchStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
			b.WriteString(searchStyle.Render(fmt.Sprintf("Search: %s", m.searchString)))
			b.WriteString("\n")
			b.WriteString(helpStyle.Render("Enter: search  Esc: cancel"))
		} else {
			// Normal help with search commands
			b.WriteString(helpStyle.Render("↑/↓: navigate  Enter: diff  <: copy←  >: copy→  i: ignore  x: delete  /: search  s: save  d: dry-run  a: apply  q: quit  Ctrl+C: force quit"))
			if m.searchString != "" {
				b.WriteString("\n")
				b.WriteString(helpStyle.Render("n: next match  N: prev match  Esc: clear search"))
			}
		}
	} else {
		b.WriteString(helpStyle.Render("q: quit"))
	}

	// Show save message if any
	if m.saveMessage != "" {
		b.WriteString("\n")
		messageStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
		if strings.Contains(m.saveMessage, "Error") {
			messageStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		}
		b.WriteString(messageStyle.Render(m.saveMessage))
	}

	// Show changes indicator
	if m.hasUnsavedChanges {
		b.WriteString("\n")
		changesStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
		b.WriteString(changesStyle.Render("● Unsaved changes"))
	} else if m.hasUnappliedChanges {
		b.WriteString("\n")
		readyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
		b.WriteString(readyStyle.Render("● Ready to execute"))
	}

	// Show discard confirmation dialog
	if m.showingDiscardConfirm {
		b.WriteString("\n\n")
		confirmStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("11")).
			Padding(1).
			Margin(1)

		confirmText := fmt.Sprintf("Discard staged changes for %s?\n\nThis will delete any patch files for this file in the current session.\nThis action cannot be undone.\n\n[y] Yes, discard  [n] No, cancel", m.discardFilePath)
		b.WriteString(confirmStyle.Render(confirmText))
	}

	// Show cleanup confirmation dialog
	if m.showingCleanup {
		b.WriteString("\n\n")
		confirmStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("10")). // Green border for positive action
			Padding(1).
			Margin(1)

		confirmText := fmt.Sprintf("Clean up old files?\n\nApply completed successfully! Clean up:\n• Action file: %s\n• All patch files from session: %s\n• All other dovetail temporary files\n\nThis will help keep your directories clean.\n\n[y] Yes, clean up  [n] No, keep files", m.cleanupActionFile, m.cleanupOldSessionID)
		b.WriteString(confirmStyle.Render(confirmText))
	}

	// Show patch cleanup confirmation dialog
	if m.showingPatchCleanup {
		b.WriteString("\n\n")
		confirmStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("11")). // Yellow border for attention
			Padding(1).
			Margin(1)

		// Count patch files by directory
		leftCount, rightCount := 0, 0
		for _, pf := range m.detectedPatchFiles {
			if pf.Side == "left" {
				leftCount++
			} else {
				rightCount++
			}
		}

		confirmText := fmt.Sprintf("Clean up patch files from previous runs?\n\nFound %d patch files:\n• Left directory: %d files\n• Right directory: %d files\n\nThese are leftover .patch files from previous dovetail hunk operations.\nThey are safe to remove.\n\n[y] Yes, clean up  [n] No, keep files", len(m.detectedPatchFiles), leftCount, rightCount)
		b.WriteString(confirmStyle.Render(confirmText))
	}

	// Show quit confirmation dialog
	if m.showingQuitConfirm {
		b.WriteString("\n\n")
		confirmStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("9")). // Red border for warning
			Padding(1).
			Margin(1)

		confirmText := "Quit with unsaved changes?\n\nYou have unsaved changes that will be lost if you quit now.\nConsider saving first (press 's') to preserve your work.\n\n[y] Yes, quit anyway  [n] No, go back"
		b.WriteString(confirmStyle.Render(confirmText))
	}

	return b.String()
}

// viewDiff renders the diff view
func (m Model) viewDiff() string {
	var b strings.Builder

	// Clear entire screen to prevent display corruption
	b.WriteString("\033[2J") // Clear entire screen
	b.WriteString("\033[H")  // Move cursor to top-left corner

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))

	if m.cursor < len(m.results) {
		result := m.results[m.cursor]

		// Show different header for hunk mode and direction
		var directionText string
		var directionStyle lipgloss.Style
		if m.reversedDiff {
			// REVERT mode - bright yellow background with black text for high visibility
			directionStyle = lipgloss.NewStyle().Background(lipgloss.Color("11")).Foreground(lipgloss.Color("0")).Bold(true)
			directionText = directionStyle.Render("⚠ REVERT MODE: RIGHT → LEFT")
		} else {
			// Normal mode - subtle gray text
			directionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
			directionText = directionStyle.Render("LEFT → RIGHT")
		}

		if m.hunkMode {
			b.WriteString(headerStyle.Render(fmt.Sprintf("Hunk Mode: %s", result.RelativePath)))
			b.WriteString(" ")
			b.WriteString(directionText)
			b.WriteString(headerStyle.Render(fmt.Sprintf(" (Hunk %d of %d)", m.currentHunk+1, len(m.hunks))))
		} else {
			b.WriteString(headerStyle.Render(fmt.Sprintf("Diff: %s", result.RelativePath)))
			b.WriteString(" ")
			b.WriteString(directionText)
		}
		b.WriteString("\n")

		// Show patch status indicators
		if m.leftPatchApplied || m.rightPatchApplied {
			patchedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // Yellow
			var statusParts []string
			if m.leftPatchApplied {
				statusParts = append(statusParts, "LEFT (patched)")
			}
			if m.rightPatchApplied {
				statusParts = append(statusParts, "RIGHT (patched)")
			}
			b.WriteString(patchedStyle.Render(fmt.Sprintf("Applied patches: %s", strings.Join(statusParts, ", "))))
			b.WriteString("\n")
		}
		b.WriteString("\n")

		if m.err != nil {
			errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
			b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		} else {
			// Display diff content with scrolling support
			diffContent := ""
			if m.hunkMode && len(m.hunks) > 0 {
				diffContent = m.renderDiffWithHunkHighlight()
			} else {
				diffContent = m.currentDiff
			}

			// Apply scrolling to diff content
			diffLines := strings.Split(diffContent, "\n")
			visibleLines := m.getVisibleDiffLines()

			// Calculate viewport boundaries
			startLine := m.diffViewportTop
			endLine := startLine + visibleLines
			if endLine > len(diffLines) {
				endLine = len(diffLines)
			}
			if startLine >= len(diffLines) {
				startLine = len(diffLines) - 1
				if startLine < 0 {
					startLine = 0
				}
			}

			// Show scrollable diff content
			if len(diffLines) > 0 {
				visibleDiff := strings.Join(diffLines[startLine:endLine], "\n")
				b.WriteString(visibleDiff)

				// Show scroll indicators if needed
				if len(diffLines) > visibleLines {
					b.WriteString("\n\n")
					scrollInfo := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
					b.WriteString(scrollInfo.Render(fmt.Sprintf("Lines %d-%d of %d",
						startLine+1, endLine, len(diffLines))))
				}
			}
		}
	}

	// Footer - different help for hunk mode
	b.WriteString("\n\n")
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	if m.hunkMode {
		appliedCount := 0
		for _, applied := range m.appliedHunks {
			if applied {
				appliedCount++
			}
		}
		b.WriteString(helpStyle.Render("n/p: next/prev hunk  <: apply hunk  r: toggle revert mode  ESC: exit hunk mode"))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render(fmt.Sprintf("Applied: %d hunks", appliedCount)))
	} else {
		b.WriteString(helpStyle.Render("↑/↓: scroll  PgUp/PgDn: page  r: toggle revert mode  SPACE: enter hunk mode  Esc/q: back to file list"))
	}

	return b.String()
}

// renderDiffWithHunkHighlight renders diff content with current hunk highlighted
func (m Model) renderDiffWithHunkHighlight() string {
	if len(m.hunks) == 0 {
		return m.currentDiff
	}

	lines := strings.Split(m.currentDiff, "\n")
	var result strings.Builder
	currentHunkLines := make(map[string]bool)

	// Mark lines that belong to current hunk
	if m.currentHunk < len(m.hunks) {
		hunk := m.hunks[m.currentHunk]
		for _, line := range hunk.Lines {
			currentHunkLines[line] = true
		}
	}

	// Render with highlighting
	hunkStyle := lipgloss.NewStyle().Background(lipgloss.Color("8")).Foreground(lipgloss.Color("15"))
	appliedStyle := lipgloss.NewStyle().Background(lipgloss.Color("10")).Foreground(lipgloss.Color("0"))

	for i, line := range lines {
		if currentHunkLines[line] {
			// Highlight current hunk
			result.WriteString(hunkStyle.Render(fmt.Sprintf(">>> %s", line)))
		} else if m.isLineFromAppliedHunk(line) {
			// Mark applied hunks differently
			result.WriteString(appliedStyle.Render(fmt.Sprintf("✓   %s", line)))
		} else {
			result.WriteString(fmt.Sprintf("    %s", line))
		}

		if i < len(lines)-1 {
			result.WriteString("\n")
		}
	}

	return result.String()
}

// isLineFromAppliedHunk checks if a line belongs to an applied hunk
func (m Model) isLineFromAppliedHunk(line string) bool {
	for i, applied := range m.appliedHunks {
		if applied && i < len(m.hunks) {
			for _, hunkLine := range m.hunks[i].Lines {
				if hunkLine == line {
					return true
				}
			}
		}
	}
	return false
}

// setAction sets the action for the currently selected file with validation
// handleDiscardConfirm handles the discard staged changes confirmation
func (m Model) handleDiscardConfirm(confirm bool) (tea.Model, tea.Cmd) {
	if confirm {
		// User confirmed - discard staged changes
		result := m.results[m.cursor]

		// Delete patch files for this file in current session
		leftPath := filepath.Join(m.leftDir, m.discardFilePath)
		rightPath := filepath.Join(m.rightDir, m.discardFilePath)
		leftPatchPath := leftPath + "." + m.sessionID + ".patch"
		rightPatchPath := rightPath + "." + m.sessionID + ".patch"

		var deletedFiles []string
		if _, err := os.Stat(leftPatchPath); err == nil {
			if err := os.Remove(leftPatchPath); err == nil {
				deletedFiles = append(deletedFiles, "LEFT")
				util.DebugPrintf("Deleted left patch file: %s", leftPatchPath)
			}
		}
		if _, err := os.Stat(rightPatchPath); err == nil {
			if err := os.Remove(rightPatchPath); err == nil {
				deletedFiles = append(deletedFiles, "RIGHT")
				util.DebugPrintf("Deleted right patch file: %s", rightPatchPath)
			}
		}

		// Set action to ignore and clear flags
		m.fileActions[result.RelativePath] = action.ActionIgnore
		m.showingDiscardConfirm = false
		m.discardFilePath = ""
		m.hasUnsavedChanges = true   // Need to save this change
		m.hasUnappliedChanges = true // Will be ready to execute after save

		if len(deletedFiles) > 0 {
			m.saveMessage = fmt.Sprintf("Discarded staged changes: %s patches deleted", strings.Join(deletedFiles, " and "))
		} else {
			m.saveMessage = "No patch files found to discard"
		}
	} else {
		// User cancelled - just hide the dialog
		m.showingDiscardConfirm = false
		m.discardFilePath = ""
		m.saveMessage = "Discard cancelled"
	}

	return m, nil
}

// handleCleanupConfirm handles the cleanup confirmation
func (m Model) handleCleanupConfirm(confirm bool) (Model, tea.Cmd) {
	if confirm {
		// User confirmed - run cleanup
		executable, err := os.Executable()
		if err != nil {
			m.saveMessage = fmt.Sprintf("Error finding executable: %v", err)
			m.showingCleanup = false
			return m, nil
		}

		// Run cleanup command for old session
		cmd := tea.ExecProcess(
			&exec.Cmd{
				Path: executable,
				Args: []string{executable, "cleanup", "--force"},
			},
			func(err error) tea.Msg {
				if err != nil {
					return cleanupCompletedMsg{success: false, error: err}
				}
				return cleanupCompletedMsg{success: true, error: nil}
			},
		)

		m.showingCleanup = false
		m.saveMessage = "Running cleanup..."
		return m, cmd
	} else {
		// User cancelled cleanup
		m.showingCleanup = false
		m.cleanupOldSessionID = ""
		m.cleanupActionFile = ""
		m.saveMessage = "Cleanup skipped"
	}

	return m, nil
}

// handlePatchCleanupConfirm handles the patch cleanup confirmation
func (m Model) handlePatchCleanupConfirm(confirm bool) (Model, tea.Cmd) {
	if confirm {
		// User confirmed - clean up patch files
		var successCount, errorCount int
		var errorMessages []string

		for _, patchFile := range m.detectedPatchFiles {
			// Construct absolute path to patch file
			var patchFilePath string
			if patchFile.Side == "left" {
				patchFilePath = filepath.Join(m.leftDir, patchFile.PatchPath)
			} else {
				patchFilePath = filepath.Join(m.rightDir, patchFile.PatchPath)
			}

			// Attempt to remove the patch file
			if err := os.Remove(patchFilePath); err != nil {
				errorCount++
				errorMessages = append(errorMessages, fmt.Sprintf("%s: %v", patchFile.PatchPath, err))
				util.DebugPrintf("Failed to remove patch file %s: %v", patchFilePath, err)
			} else {
				successCount++
				util.DebugPrintf("Successfully removed patch file: %s", patchFilePath)
			}
		}

		// Update UI state
		m.showingPatchCleanup = false

		// Show result message
		if successCount > 0 && errorCount == 0 {
			m.saveMessage = fmt.Sprintf("✅ Successfully cleaned up %d patch files", successCount)
		} else if successCount > 0 && errorCount > 0 {
			m.saveMessage = fmt.Sprintf("⚠ Cleaned up %d patch files, %d failed", successCount, errorCount)
		} else {
			m.saveMessage = fmt.Sprintf("❌ Failed to clean up patch files: %s", strings.Join(errorMessages, "; "))
		}
	} else {
		// User cancelled cleanup
		m.showingPatchCleanup = false
		m.saveMessage = "Patch cleanup skipped"
	}

	return m, nil
}

// handleQuitConfirm handles the quit confirmation when there are unsaved changes
func (m Model) handleQuitConfirm(confirm bool) (tea.Model, tea.Cmd) {
	if confirm {
		// User confirmed quit - call profiling cleanup and quit
		if cleanup := getProfilingCleanup(); cleanup != nil {
			cleanup()
		}
		return m, tea.Quit
	} else {
		// User cancelled quit
		m.showingQuitConfirm = false
		m.saveMessage = "Quit cancelled"
	}

	return m, nil
}

func (m Model) setAction(newAction action.ActionType) Model {
	if m.cursor >= len(m.results) {
		return m
	}

	result := m.results[m.cursor]

	// Validate action is allowed for this file status
	if !m.isActionValid(newAction, result.Status) {
		// Show error message briefly
		m.saveMessage = fmt.Sprintf("Action '%s' not valid for %s files",
			newAction.String(), result.Status.String())
		return m
	}

	// Set the action
	m.fileActions[result.RelativePath] = newAction
	m.hasUnsavedChanges = true   // Need to save changes
	m.hasUnappliedChanges = true // Will be ready to execute after save
	m.saveMessage = ""           // Clear any previous message

	// Auto-advance to next file for better UX
	if m.cursor < len(m.results)-1 {
		m.cursor++
	}

	return m
}

// isActionValid checks if an action is valid for a given file status
func (m Model) isActionValid(act action.ActionType, status compare.FileStatus) bool {
	switch act {
	case action.ActionIgnore:
		return true // Always valid
	case action.ActionCopyToRight:
		return status == compare.StatusModified || status == compare.StatusOnlyLeft
	case action.ActionCopyToLeft:
		return status == compare.StatusModified || status == compare.StatusOnlyRight
	case action.ActionDeleteLeft, action.ActionDeleteRight:
		// Simplified delete logic - only when file exists on one side only
		if act == action.ActionDeleteLeft {
			return status == compare.StatusOnlyLeft
		} else {
			return status == compare.StatusOnlyRight
		}
	case action.ActionDeleteBoth:
		// Delete both not supported in simplified TUI logic
		return false
	default:
		return false
	}
}

// saveActionFileWithState handles file writing and state management
func (m Model) saveActionFileWithState(filename string, successMessage string) (Model, error) {
	if err := m.writeActionFile(filename); err != nil {
		m.saveMessage = fmt.Sprintf("Error saving: %v", err)
		return m, err
	}

	// Update state after successful save
	m.hasUnsavedChanges = false  // Changes are now saved
	m.hasUnappliedChanges = true // Ready to execute
	m.saveMessage = successMessage

	return m, nil
}

// saveActionFile initiates the manual save process
func (m Model) saveActionFile() (Model, tea.Cmd) {
	if !m.hasUnsavedChanges {
		m.saveMessage = "No changes to save"
		return m, nil
	}

	// Generate action file with current actions (use sessionID to match patch files)
	filename := fmt.Sprintf("dovetail_actions_%s.txt", m.sessionID)
	successMessage := fmt.Sprintf("✅ Saved to %s", filename)

	updatedModel, _ := m.saveActionFileWithState(filename, successMessage)
	return updatedModel, nil
}

// writeActionFile writes the current actions to a file
func (m Model) writeActionFile(filename string) error {
	// Create action file
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write header
	header := action.ActionFileHeader{
		GeneratedAt: time.Now().Format("2006-01-02 15:04:05"),
		LeftDir:     m.leftDir,
		RightDir:    m.rightDir,
		Version:     "TUI",
	}

	// Write custom action file with our selected actions
	if err := m.writeCustomActionFile(file, header); err != nil {
		return err
	}

	return nil
}

// writeCustomActionFile writes action file with selected actions
func (m Model) writeCustomActionFile(file *os.File, header action.ActionFileHeader) error {
	// Write header
	lines := []string{
		fmt.Sprintf("# Action File generated on %s", header.GeneratedAt),
		fmt.Sprintf("# Generated by dovetail TUI version %s", header.Version),
		fmt.Sprintf("# Left:  %s", header.LeftDir),
		fmt.Sprintf("# Right: %s", header.RightDir),
		"#",
		"# INSTRUCTIONS:",
		"# Actions were selected interactively in the TUI.",
		"#",
		"# Available Actions:",
		"#   i  : Ignore this difference, do nothing",
		"#   >  : Copy file from Left to Right (overwrite)",
		"#   <  : Copy file from Right to Left (overwrite)",
		"#   x- : Delete file from Left",
		"#   -x : Delete file from Right",
		"#   xx : Delete file from both Left and Right",
		"#",
		"# FORMAT: [ACTION] : STATUS : RELATIVE_PATH",
		"#",
	}

	for _, line := range lines {
		if _, err := fmt.Fprintf(file, "%s\n", line); err != nil {
			return err
		}
	}

	// Write action items with selected actions
	for _, result := range m.results {
		selectedAction := m.fileActions[result.RelativePath]
		line := fmt.Sprintf("[%s] : %-12s : %s",
			selectedAction.String(),
			result.Status.String(),
			result.RelativePath)

		if _, err := fmt.Fprintf(file, "%s\n", line); err != nil {
			return err
		}
	}

	return nil
}

// getStatusColor returns the appropriate color for a file status
func getStatusColor(status compare.FileStatus) lipgloss.Color {
	switch status {
	case compare.StatusModified:
		return lipgloss.Color("11") // Yellow
	case compare.StatusOnlyLeft:
		return lipgloss.Color("9") // Red
	case compare.StatusOnlyRight:
		return lipgloss.Color("10") // Green
	case compare.StatusIdentical:
		return lipgloss.Color("8") // Gray
	default:
		return lipgloss.Color("15") // White
	}
}

// getActionColor returns the appropriate color for an action
func getActionColor(act action.ActionType) lipgloss.Color {
	switch act {
	case action.ActionIgnore:
		return lipgloss.Color("240") // Very dark gray - dimmed for ignored files
	case action.ActionCopyToRight:
		return lipgloss.Color("12") // Blue
	case action.ActionCopyToLeft:
		return lipgloss.Color("13") // Magenta
	case action.ActionDeleteLeft, action.ActionDeleteRight, action.ActionDeleteBoth:
		return lipgloss.Color("9") // Red
	case action.ActionPatch:
		return lipgloss.Color("11") // Yellow for patches
	default:
		return lipgloss.Color("15") // White
	}
}

// handleSearchInput processes input when in search mode
func (m Model) handleSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		// Call profiling cleanup before quitting
		if cleanup := getProfilingCleanup(); cleanup != nil {
			cleanup()
		}
		return m, tea.Quit
	case "esc":
		// Cancel search
		m.searchMode = false
		m.searchString = ""
	case "enter":
		// Execute search
		m.searchMode = false
		if m.searchString != "" {
			m.executeSearch()
		}
	case "backspace":
		// Remove last character
		if len(m.searchString) > 0 {
			m.searchString = m.searchString[:len(m.searchString)-1]
		}
	default:
		// Add printable characters to search string
		if len(msg.String()) == 1 && unicode.IsPrint(rune(msg.String()[0])) {
			m.searchString += msg.String()
		}
	}
	return m, nil
}

// executeSearch performs the search and updates match list
func (m *Model) executeSearch() {
	if m.searchString == "" {
		return
	}

	searchLower := strings.ToLower(m.searchString)
	m.searchMatches = nil

	// Search in file paths (case insensitive)
	for i, result := range m.results {
		pathLower := strings.ToLower(result.RelativePath)
		if strings.Contains(pathLower, searchLower) {
			m.searchMatches = append(m.searchMatches, i)
		}
	}

	// Jump to first match if any
	if len(m.searchMatches) > 0 {
		m.matchIndex = 0
		m.cursor = m.searchMatches[0]
		m.saveMessage = fmt.Sprintf("Found %d matches - jumped to first", len(m.searchMatches))
	} else {
		m.saveMessage = fmt.Sprintf("'%s' not found", m.searchString)
	}
}

// nextSearchMatch moves to next search match
func (m Model) nextSearchMatch() Model {
	if len(m.searchMatches) == 0 {
		return m
	}

	m.matchIndex = (m.matchIndex + 1) % len(m.searchMatches)
	m.cursor = m.searchMatches[m.matchIndex]
	m.saveMessage = fmt.Sprintf("Match %d of %d", m.matchIndex+1, len(m.searchMatches))
	return m
}

// prevSearchMatch moves to previous search match
func (m Model) prevSearchMatch() Model {
	if len(m.searchMatches) == 0 {
		return m
	}

	m.matchIndex = (m.matchIndex - 1 + len(m.searchMatches)) % len(m.searchMatches)
	m.cursor = m.searchMatches[m.matchIndex]
	m.saveMessage = fmt.Sprintf("Match %d of %d", m.matchIndex+1, len(m.searchMatches))
	return m
}

// highlightSearch highlights search terms in text
func highlightSearch(text, search string) string {
	if search == "" {
		return text
	}

	// Case insensitive highlighting
	lowerText := strings.ToLower(text)
	lowerSearch := strings.ToLower(search)

	highlightStyle := lipgloss.NewStyle().Background(lipgloss.Color("11")).Foreground(lipgloss.Color("0"))

	result := ""
	searchLen := len(search)
	i := 0

	for i < len(text) {
		// Find next occurrence
		pos := strings.Index(lowerText[i:], lowerSearch)
		if pos == -1 {
			// No more matches, append rest of text
			result += text[i:]
			break
		}

		// Add text before match
		actualPos := i + pos
		result += text[i:actualPos]

		// Add highlighted match (preserve original case)
		match := text[actualPos : actualPos+searchLen]
		result += highlightStyle.Render(match)

		i = actualPos + searchLen
	}

	return result
}

// enterHunkMode parses current diff into hunks and enters hunk editing mode
func (m Model) enterHunkMode() Model {
	if m.cursor >= len(m.results) {
		return m
	}

	// Parse diff content into hunks
	hunks, err := parseDiffIntoHunks(m.currentDiff)
	if err != nil {
		m.saveMessage = fmt.Sprintf("Error parsing diff: %v", err)
		return m
	}

	if len(hunks) == 0 {
		m.saveMessage = "No editable hunks found in diff"
		return m
	}

	// Initialize hunk mode state
	m.hunkMode = true
	m.hunks = hunks
	m.currentHunk = 0
	m.appliedHunks = make([]bool, len(hunks))
	m.saveMessage = fmt.Sprintf("Hunk mode: %d hunks available", len(hunks))

	return m
}

// exitHunkMode exits hunk editing mode and handles patch generation
func (m Model) exitHunkMode() (Model, tea.Cmd) {
	util.DebugPrintf("=== exitHunkMode ENTRY ===")
	if !m.hunkMode {
		util.DebugPrintf("Not in hunk mode, returning")
		return m, nil
	}

	// Always check if temp files differ from originals (filesystem-based approach)
	// Don't rely on appliedHunks state which can be lost during diff regeneration
	util.DebugPrintf("Checking if temp files differ from originals...")

	// Clean up hunk mode state first
	m.hunkMode = false
	m.hunks = nil
	m.currentHunk = 0
	m.appliedHunks = nil

	// Check if we have any temp files that differ from originals
	if m.tempLeftFile != "" || m.tempRightFile != "" {
		util.DebugPrintf("Found temp files, checking for differences...")
		// Generate patch file - it will check for actual differences
		return m.generatePatchFile()
	}

	// No temp files created - no changes made
	util.DebugPrintf("No temp files found - no changes made")
	m.cleanupTempFiles()
	m.saveMessage = "Exited hunk mode - no changes made"
	return m, nil
}

// applyCurrentHunk applies the currently selected hunk LEFT→RIGHT (only direction)
func (m Model) applyCurrentHunk() (Model, tea.Cmd) {
	util.DebugPrintf("applyCurrentHunk called, hunkMode=%t, currentHunk=%d/%d",
		m.hunkMode, m.currentHunk, len(m.hunks))

	if !m.hunkMode || m.currentHunk >= len(m.hunks) {
		util.DebugPrintf("Invalid state - returning")
		return m, nil
	}

	if m.appliedHunks[m.currentHunk] {
		util.DebugPrintf("Hunk already applied")
		m.saveMessage = fmt.Sprintf("Hunk %d already applied", m.currentHunk+1)
		return m, nil
	}

	util.DebugPrintf("Creating temp target file...")
	// Create temp file for target side based on current diff direction
	if err := m.ensureTempTargetFile(); err != nil {
		util.DebugPrintf("Error creating temp files: %v", err)
		m.saveMessage = fmt.Sprintf("Error creating temp files: %v", err)
		return m, nil
	}

	util.DebugPrintf("Applying hunk to target temp file...")
	// Apply the hunk to the first file in current diff direction
	hunk := m.hunks[m.currentHunk]
	if err := m.applyHunkToTargetFile(hunk); err != nil {
		util.DebugPrintf("Error applying hunk: %v", err)
		m.saveMessage = fmt.Sprintf("Error applying hunk: %v", err)
		return m, nil
	}

	util.DebugPrintf("Marking hunk as applied...")
	// Mark hunk as applied
	m.appliedHunks[m.currentHunk] = true
	appliedCount := 0
	for _, applied := range m.appliedHunks {
		if applied {
			appliedCount++
		}
	}

	// Show which direction the diff is currently in
	directionStr := "left→right"
	if m.reversedDiff {
		directionStr = "revert right→left"
	}
	m.saveMessage = fmt.Sprintf("Applied hunk %d/%d (%s)", m.currentHunk+1, len(m.hunks), directionStr)
	util.DebugPrintf("Hunk applied successfully, regenerating diff...")

	// Regenerate diff with updated temp files - this will cause immediate refresh
	newModel, cmd := m.regenerateDiff()
	util.DebugPrintf("Regeneration complete, returning updated model")
	return newModel, cmd
}

// stripAnsiCodes removes ANSI escape sequences from a string
func stripAnsiCodes(s string) string {
	// Remove all ANSI escape sequences (more comprehensive than just SGR codes)
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	return ansiRegex.ReplaceAllString(s, "")
}

// parseDiffIntoHunks parses unified diff content into individual hunks
func parseDiffIntoHunks(diffContent string) ([]DiffHunk, error) {
	// Strip ANSI color codes that break parsing
	cleanDiff := stripAnsiCodes(diffContent)
	lines := strings.Split(cleanDiff, "\n")
	var hunks []DiffHunk
	var currentHunk *DiffHunk

	// Regex for parsing hunk headers: @@ -10,3 +10,4 @@
	hunkHeaderRegex := regexp.MustCompile(`^@@\s+-(\d+)(?:,(\d+))?\s+\+(\d+)(?:,(\d+))?\s+@@`)

	for _, line := range lines {
		// Check for hunk header
		if matches := hunkHeaderRegex.FindStringSubmatch(line); matches != nil {
			// Save previous hunk if any
			if currentHunk != nil {
				hunks = append(hunks, *currentHunk)
			}

			// Parse new hunk header
			leftStart, _ := strconv.Atoi(matches[1])
			leftCount := 1
			if matches[2] != "" {
				leftCount, _ = strconv.Atoi(matches[2])
			}
			rightStart, _ := strconv.Atoi(matches[3])
			rightCount := 1
			if matches[4] != "" {
				rightCount, _ = strconv.Atoi(matches[4])
			}

			currentHunk = &DiffHunk{
				Header:     line,
				LeftStart:  leftStart,
				LeftCount:  leftCount,
				RightStart: rightStart,
				RightCount: rightCount,
				Lines:      []string{line},
				Applied:    false,
			}
		} else if currentHunk != nil {
			// Add line to current hunk (context, additions, deletions)
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
				currentHunk.Lines = append(currentHunk.Lines, line)
			}
		}
	}

	// Don't forget the last hunk
	if currentHunk != nil {
		hunks = append(hunks, *currentHunk)
	}

	return hunks, nil
}

// applyExistingPatches checks for and applies existing session patches to temp files
func (m *Model) applyExistingPatches(result compare.ComparisonResult) error {
	util.DebugPrintf("=== applyExistingPatches ENTRY for %s ===", result.RelativePath)

	// Reset patch status indicators
	m.leftPatchApplied = false
	m.rightPatchApplied = false

	// Check for existing patch files
	leftOrigPath := filepath.Join(m.leftDir, result.RelativePath)
	rightOrigPath := filepath.Join(m.rightDir, result.RelativePath)
	leftPatchPath := leftOrigPath + "." + m.sessionID + ".patch"
	rightPatchPath := rightOrigPath + "." + m.sessionID + ".patch"

	var leftPatchExists, rightPatchExists bool
	if _, err := os.Stat(leftPatchPath); err == nil {
		leftPatchExists = true
		util.DebugPrintf("Found existing left patch: %s", leftPatchPath)
	}
	if _, err := os.Stat(rightPatchPath); err == nil {
		rightPatchExists = true
		util.DebugPrintf("Found existing right patch: %s", rightPatchPath)
	}

	if !leftPatchExists && !rightPatchExists {
		util.DebugPrintf("No existing patches found for %s", result.RelativePath)
		return nil
	}

	// Create temp directory if not already done
	if m.tempDir == "" {
		tempDir, err := ioutil.TempDir("", "dovetail_hunks_")
		if err != nil {
			return fmt.Errorf("failed to create temp directory: %w", err)
		}
		m.tempDir = tempDir
		util.DebugPrintf("Created temp directory: %s", m.tempDir)
	}

	// Apply left patch if it exists
	if leftPatchExists {
		if m.tempLeftFile == "" {
			// Create temp left file and apply patch
			leftTempName := "left_" + strings.ReplaceAll(filepath.Base(result.RelativePath), "/", "_")
			m.tempLeftFile = filepath.Join(m.tempDir, leftTempName)

			// Copy original to temp
			if err := copyFile(leftOrigPath, m.tempLeftFile); err != nil {
				return fmt.Errorf("failed to copy left file to temp: %w", err)
			}
			util.DebugPrintf("Copied left file to temp: %s", m.tempLeftFile)
		}

		// Apply existing patch to temp file
		if err := action.ApplyPatchToFile(leftPatchPath, m.tempLeftFile); err != nil {
			return fmt.Errorf("failed to apply existing left patch: %w", err)
		}
		m.leftPatchApplied = true
		util.DebugPrintf("Applied existing left patch to temp file")
	}

	// Apply right patch if it exists
	if rightPatchExists {
		if m.tempRightFile == "" {
			// Create temp right file and apply patch
			rightTempName := "right_" + strings.ReplaceAll(filepath.Base(result.RelativePath), "/", "_")
			m.tempRightFile = filepath.Join(m.tempDir, rightTempName)

			// Copy original to temp
			if err := copyFile(rightOrigPath, m.tempRightFile); err != nil {
				return fmt.Errorf("failed to copy right file to temp: %w", err)
			}
			util.DebugPrintf("Copied right file to temp: %s", m.tempRightFile)
		}

		// Apply existing patch to temp file
		if err := action.ApplyPatchToFile(rightPatchPath, m.tempRightFile); err != nil {
			return fmt.Errorf("failed to apply existing right patch: %w", err)
		}
		m.rightPatchApplied = true
		util.DebugPrintf("Applied existing right patch to temp file")
	}

	util.DebugPrintf("=== applyExistingPatches SUCCESS ===")
	return nil
}

// ensureTempTargetFile creates temp clone file for the target file based on current diff direction
// Normal view: LEFT is first → create temp LEFT file
// Reversed view: RIGHT is first → create temp RIGHT file
func (m *Model) ensureTempTargetFile() error {
	util.DebugPrintf("=== ensureTempTargetFile ENTRY (reversedDiff=%t) ===", m.reversedDiff)

	if m.cursor >= len(m.results) {
		util.DebugPrintf("ERROR: invalid cursor position %d >= %d", m.cursor, len(m.results))
		return fmt.Errorf("invalid cursor position")
	}

	result := m.results[m.cursor]
	util.DebugPrintf("Processing file: %s, Status: %s", result.RelativePath, result.Status)

	// Create temp directory if needed
	var tempDir string
	if m.tempLeftFile == "" && m.tempRightFile == "" {
		var err error
		tempDir, err = ioutil.TempDir("", "dovetail_hunks_")
		if err != nil {
			util.DebugPrintf("ERROR: failed to create temp directory: %v", err)
			return fmt.Errorf("failed to create temp directory: %w", err)
		}
		util.DebugPrintf("Created temp directory: %s", tempDir)
	}

	// Create temp file for the "first file" in current diff direction
	if !m.reversedDiff {
		// Normal view: diff -u LEFT RIGHT → LEFT is first → create temp LEFT file
		if result.LeftInfo != nil && !result.LeftInfo.IsDir && m.tempLeftFile == "" {
			if tempDir == "" {
				// Find existing temp directory from other temp file
				if m.tempRightFile != "" {
					tempDir = filepath.Dir(m.tempRightFile)
				}
			}
			leftPath := filepath.Join(m.leftDir, result.RelativePath)
			m.tempLeftFile = filepath.Join(tempDir, "left_"+filepath.Base(result.RelativePath))
			util.DebugPrintf("Creating temp LEFT file (normal view target): %s -> %s", leftPath, m.tempLeftFile)

			if err := copyFile(leftPath, m.tempLeftFile); err != nil {
				util.DebugPrintf("ERROR: failed to copy left file: %v", err)
				return fmt.Errorf("failed to copy left file: %w", err)
			}
			util.DebugPrintf("SUCCESS: copied left file to %s", m.tempLeftFile)
		} else if m.tempLeftFile != "" {
			util.DebugPrintf("Using existing temp left file: %s", m.tempLeftFile)
		}
	} else {
		// Reversed view: diff -u RIGHT LEFT → RIGHT is first → create temp RIGHT file
		if result.RightInfo != nil && !result.RightInfo.IsDir && m.tempRightFile == "" {
			if tempDir == "" {
				// Find existing temp directory from other temp file
				if m.tempLeftFile != "" {
					tempDir = filepath.Dir(m.tempLeftFile)
				}
			}
			rightPath := filepath.Join(m.rightDir, result.RelativePath)
			m.tempRightFile = filepath.Join(tempDir, "right_"+filepath.Base(result.RelativePath))
			util.DebugPrintf("Creating temp RIGHT file (reversed view target): %s -> %s", rightPath, m.tempRightFile)

			if err := copyFile(rightPath, m.tempRightFile); err != nil {
				util.DebugPrintf("ERROR: failed to copy right file: %v", err)
				return fmt.Errorf("failed to copy right file: %w", err)
			}
			util.DebugPrintf("SUCCESS: copied right file to %s", m.tempRightFile)
		} else if m.tempRightFile != "" {
			util.DebugPrintf("Using existing temp right file: %s", m.tempRightFile)
		}
	}

	util.DebugPrintf("=== ensureTempTargetFile SUCCESS ===")
	return nil
}

// applyHunkToTargetFile applies a hunk to the target temp file based on current diff direction
func (m *Model) applyHunkToTargetFile(hunk DiffHunk) error {
	util.DebugPrintf("=== applyHunkToTargetFile ENTRY (reversedDiff=%t) ===", m.reversedDiff)
	util.DebugPrintf("Hunk header: %s", hunk.Header)
	util.DebugPrintf("Hunk lines count: %d", len(hunk.Lines))

	// Create a temporary patch file with just this hunk
	patchContent := strings.Join(hunk.Lines, "\n") + "\n"
	util.DebugPrintf("Patch content preview (first 200 chars): %.200s", patchContent)

	tempPatch, err := ioutil.TempFile("", "hunk_*.patch")
	if err != nil {
		util.DebugPrintf("ERROR: failed to create temp patch file: %v", err)
		return fmt.Errorf("failed to create temp patch: %w", err)
	}
	patchFilePath := tempPatch.Name()
	util.DebugPrintf("Created temp patch file: %s", patchFilePath)
	defer os.Remove(patchFilePath)
	defer tempPatch.Close()

	if _, err := tempPatch.WriteString(patchContent); err != nil {
		util.DebugPrintf("ERROR: failed to write patch content: %v", err)
		return fmt.Errorf("failed to write patch content: %w", err)
	}
	tempPatch.Close()
	util.DebugPrintf("Successfully wrote patch content to file")

	// Apply patch to the "first file" in the current diff direction
	var targetFile string
	if !m.reversedDiff {
		// Normal view: diff -u LEFT RIGHT → apply to LEFT file (first file)
		targetFile = m.tempLeftFile
		util.DebugPrintf("Applying hunk to LEFT file (normal view): %s", targetFile)
	} else {
		// Reversed view: diff -u RIGHT LEFT → apply to RIGHT file (first file)
		targetFile = m.tempRightFile
		util.DebugPrintf("Applying hunk to RIGHT file (reversed view): %s", targetFile)
	}

	// Check if target file exists
	if _, err := os.Stat(targetFile); err != nil {
		util.DebugPrintf("ERROR: target file doesn't exist: %s, error: %v", targetFile, err)
		return fmt.Errorf("target file doesn't exist: %s", targetFile)
	}
	util.DebugPrintf("Target file exists: %s", targetFile)

	// Use patch command to apply the hunk
	cmd := exec.Command("patch", targetFile)
	cmd.Stdin = strings.NewReader(patchContent)
	util.DebugPrintf("Running patch command: patch %s", targetFile)

	output, err := cmd.CombinedOutput()
	util.DebugPrintf("Patch command output: %s", string(output))

	if err != nil {
		util.DebugPrintf("ERROR: patch command failed: %v", err)
		util.DebugPrintf("Full patch command output: %s", string(output))
		return fmt.Errorf("patch failed: %w, output: %s", err, string(output))
	}

	util.DebugPrintf("SUCCESS: patch applied successfully")
	util.DebugPrintf("=== applyHunkToTargetFile SUCCESS ===")
	return nil
}

// regenerateDiff regenerates the diff using updated temp files
func (m *Model) regenerateDiff() (Model, tea.Cmd) {
	util.DebugPrintf("=== regenerateDiff ENTRY ===")

	if m.cursor >= len(m.results) {
		util.DebugPrintf("ERROR: invalid cursor position in regenerateDiff")
		return *m, nil
	}

	// Generate new diff between temp files (or original if no temp file)
	result := m.results[m.cursor]
	util.DebugPrintf("Regenerating diff for: %s", result.RelativePath)

	leftPath := filepath.Join(m.leftDir, result.RelativePath)
	if m.tempLeftFile != "" {
		leftPath = m.tempLeftFile
		util.DebugPrintf("Using temp left file: %s", leftPath)
	} else {
		util.DebugPrintf("Using original left file: %s", leftPath)
	}

	rightPath := filepath.Join(m.rightDir, result.RelativePath)
	if m.tempRightFile != "" {
		rightPath = m.tempRightFile
		util.DebugPrintf("Using temp right file: %s", rightPath)
	} else {
		util.DebugPrintf("Using original right file: %s", rightPath)
	}

	// Run diff command
	var cmd *exec.Cmd
	args := []string{"--color=always", "-u", "-U3"}
	if m.ignoreWhitespace {
		args = append(args, "-w") // Ignore whitespace differences
	}
	args = append(args, leftPath, rightPath)

	if _, err := exec.LookPath("colordiff"); err == nil {
		cmd = exec.Command("colordiff", args...)
	} else {
		// Fall back to regular diff with unified format and 3 lines of context
		// Remove --color=always for regular diff
		regularArgs := []string{"-u", "-U3"}
		if m.ignoreWhitespace {
			regularArgs = append(regularArgs, "-w")
		}
		regularArgs = append(regularArgs, leftPath, rightPath)
		cmd = exec.Command("diff", regularArgs...)
	}
	util.DebugPrintf("Running diff command: %s", cmd.String())

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// Normal case - files differ
			m.currentDiff = string(output)
			util.DebugPrintf("Diff command completed (exit code 1), output length: %d", len(output))
		} else {
			util.DebugPrintf("ERROR: diff command failed: %v", err)
			m.saveMessage = fmt.Sprintf("Error regenerating diff: %v", err)
			return *m, nil
		}
	} else {
		// Files are identical
		m.currentDiff = string(output)
		util.DebugPrintf("Diff command completed (exit code 0), files identical, output length: %d", len(output))
	}

	util.DebugPrintf("Updated currentDiff, preview (first 200 chars): %.200s", m.currentDiff)

	// Re-parse hunks
	hunks, err := parseDiffIntoHunks(m.currentDiff)
	if err != nil {
		m.saveMessage = fmt.Sprintf("Error re-parsing hunks: %v", err)
		return *m, nil
	}

	// Update hunk state - preserve applied hunk tracking
	oldAppliedHunks := m.appliedHunks
	m.hunks = hunks
	m.appliedHunks = make([]bool, len(hunks))

	// Try to preserve as many applied states as possible
	preserved := 0
	for i := 0; i < len(m.appliedHunks) && i < len(oldAppliedHunks); i++ {
		m.appliedHunks[i] = oldAppliedHunks[i]
		if oldAppliedHunks[i] {
			preserved++
		}
	}
	util.DebugPrintf("Preserved %d applied hunk states, new total: %d hunks", preserved, len(hunks))

	// No auto-exit logic - let user explicitly exit with ESC/q

	// Reset current hunk if out of bounds
	if m.currentHunk >= len(hunks) {
		m.currentHunk = 0
		util.DebugPrintf("Reset current hunk to 0 (was out of bounds)")
	}

	util.DebugPrintf("=== regenerateDiff SUCCESS ===")
	return *m, nil
}

// generatePatchFile generates the final patch file from original to temp files
func (m *Model) generatePatchFile() (Model, tea.Cmd) {
	util.DebugPrintf("=== generatePatchFile ENTRY ===")
	if m.cursor >= len(m.results) {
		util.DebugPrintf("Invalid cursor position")
		return *m, nil
	}

	result := m.results[m.cursor]
	util.DebugPrintf("Generating patch for file: %s", result.RelativePath)

	// Generate patch from original to final temp file
	originalLeft := filepath.Join(m.leftDir, result.RelativePath)
	originalRight := filepath.Join(m.rightDir, result.RelativePath)
	util.DebugPrintf("Original left: %s", originalLeft)
	util.DebugPrintf("Original right: %s", originalRight)
	util.DebugPrintf("Temp left file: %s", m.tempLeftFile)
	util.DebugPrintf("Temp right file: %s", m.tempRightFile)

	// Generate patches for all modified sides (can be both left and right)
	var leftPatchContent, rightPatchContent string
	var leftPatchPath, rightPatchPath string
	patchesGenerated := 0

	// Check left side for modifications
	if m.tempLeftFile != "" {
		util.DebugPrintf("Generating patch for left side: %s vs %s", originalLeft, m.tempLeftFile)
		cmd := exec.Command("diff", "-u", originalLeft, m.tempLeftFile)
		output, err := cmd.Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				// Exit code 1 means differences found - this is what we want!
				leftPatchContent = string(output)
				patchDir := filepath.Dir(filepath.Join(m.leftDir, result.RelativePath))
				patchFilename := filepath.Base(result.RelativePath) + "." + m.sessionID + ".patch"
				leftPatchPath = filepath.Join(patchDir, patchFilename)
				util.DebugPrintf("Left patch generated, %d bytes", len(leftPatchContent))
			} else {
				util.DebugPrintf("Left diff error: %v", err)
			}
		} else {
			util.DebugPrintf("No differences in left side")
		}
	}

	// Check right side for modifications
	if m.tempRightFile != "" {
		util.DebugPrintf("Generating patch for right side: %s vs %s", originalRight, m.tempRightFile)
		cmd := exec.Command("diff", "-u", originalRight, m.tempRightFile)
		output, err := cmd.Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				// Exit code 1 means differences found - this is what we want!
				rightPatchContent = string(output)
				patchDir := filepath.Dir(filepath.Join(m.rightDir, result.RelativePath))
				patchFilename := filepath.Base(result.RelativePath) + "." + m.sessionID + ".patch"
				rightPatchPath = filepath.Join(patchDir, patchFilename)
				util.DebugPrintf("Right patch generated, %d bytes", len(rightPatchContent))
			} else {
				util.DebugPrintf("Diff error: %v", err)
			}
		} else {
			// No differences found
			util.DebugPrintf("No differences in right side")
		}
	}

	// Save all generated patches
	if leftPatchContent != "" {
		util.DebugPrintf("Attempting to write left patch to: %s", leftPatchPath)
		util.DebugPrintf("Left patch content length: %d bytes", len(leftPatchContent))
		if err := ioutil.WriteFile(leftPatchPath, []byte(leftPatchContent), 0644); err != nil {
			util.DebugPrintf("ERROR writing left patch file: %v", err)
			m.saveMessage = fmt.Sprintf("Error saving left patch: %v", err)
			m.cleanupTempFiles()
			return *m, nil
		}
		util.DebugPrintf("SUCCESS: Saved left patch: %s", leftPatchPath)
		patchesGenerated++
	} else {
		util.DebugPrintf("No left patch content to save")
	}

	if rightPatchContent != "" {
		util.DebugPrintf("Attempting to write right patch to: %s", rightPatchPath)
		util.DebugPrintf("Right patch content length: %d bytes", len(rightPatchContent))
		if err := ioutil.WriteFile(rightPatchPath, []byte(rightPatchContent), 0644); err != nil {
			util.DebugPrintf("ERROR writing right patch file: %v", err)
			m.saveMessage = fmt.Sprintf("Error saving right patch: %v", err)
			m.cleanupTempFiles()
			return *m, nil
		}
		util.DebugPrintf("SUCCESS: Saved right patch: %s", rightPatchPath)
		patchesGenerated++
	} else {
		util.DebugPrintf("No right patch content to save")
	}

	if patchesGenerated == 0 {
		m.saveMessage = "No changes to save"
		m.cleanupTempFiles()
		return *m, nil
	}

	// Update action to patch type
	m.fileActions[result.RelativePath] = action.ActionPatch
	m.hasUnsavedChanges = true   // Need to save changes
	m.hasUnappliedChanges = true // Will be ready to execute after save

	// Show appropriate success message
	if patchesGenerated == 1 {
		if leftPatchContent != "" {
			m.saveMessage = fmt.Sprintf("Left patch saved: %s", leftPatchPath)
		} else {
			m.saveMessage = fmt.Sprintf("Right patch saved: %s", rightPatchPath)
		}
	} else {
		m.saveMessage = fmt.Sprintf("Both patches saved: %s, %s", leftPatchPath, rightPatchPath)
	}

	util.DebugPrintf("Generated %d patch files successfully", patchesGenerated)
	util.DebugPrintf("=== generatePatchFile SUCCESS ===")

	// Clean up temp files
	m.cleanupTempFiles()

	return *m, nil
}

// cleanupTempFiles removes temporary files
func (m *Model) cleanupTempFiles() {
	if m.tempLeftFile != "" {
		os.Remove(m.tempLeftFile)
		m.tempLeftFile = ""
	}
	if m.tempRightFile != "" {
		os.Remove(m.tempRightFile)
		m.tempRightFile = ""
	}
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = destFile.ReadFrom(sourceFile)
	return err
}

// runDryRun executes dry-run command in external process with pager
func (m Model) runDryRun() (Model, tea.Cmd) {
	// Save action file first using consolidated function
	filename := fmt.Sprintf("dovetail_actions_%s.txt", m.sessionID)
	util.LogInfo("=== TUI DRY RUN INVOCATION ===")
	util.LogInfo("Action file: %q", filename)

	updatedModel, err := m.saveActionFileWithState(filename, "Launching dry-run...")
	if err != nil {
		util.LogInfo("ERROR: Failed to write action file: %v", err)
		return updatedModel, nil
	}
	util.LogInfo("Action file written successfully")
	m = updatedModel

	// Get executable path
	executable, err := os.Executable()
	if err != nil {
		util.LogInfo("ERROR: Failed to get executable path: %v", err)
		m.saveMessage = fmt.Sprintf("Error finding executable: %v", err)
		return m, nil
	}
	util.LogInfo("Executable path: %q", executable)
	util.LogInfo("Left directory: %q", m.leftDir)
	util.LogInfo("Right directory: %q", m.rightDir)

	// Construct full command with required directories
	fullCommand := fmt.Sprintf("%s dry %s %s %s | less", executable, filename, m.leftDir, m.rightDir)
	util.LogInfo("Full command to execute: %q", fullCommand)
	util.LogInfo("Shell command: [/bin/sh, -c, %q]", fullCommand)

	// Run dry-run with pager
	cmd := tea.ExecProcess(
		&exec.Cmd{
			Path: "/bin/sh",
			Args: []string{"/bin/sh", "-c", fullCommand},
		},
		func(err error) tea.Msg {
			if err != nil {
				util.LogInfo("DRY RUN COMPLETED WITH ERROR: %v", err)
				return dryRunCompletedMsg{success: false, error: err}
			}
			util.LogInfo("DRY RUN COMPLETED SUCCESSFULLY")
			return dryRunCompletedMsg{success: true, error: nil}
		},
	)

	return m, cmd
}

// runApply executes apply command in external process
func (m Model) runApply() (Model, tea.Cmd) {
	// Save action file first using consolidated function
	filename := fmt.Sprintf("dovetail_actions_%s.txt", m.sessionID)
	util.LogInfo("=== TUI APPLY INVOCATION ===")
	util.LogInfo("Action file: %q", filename)

	updatedModel, err := m.saveActionFileWithState(filename, "Launching apply...")
	if err != nil {
		util.LogInfo("ERROR: Failed to write action file: %v", err)
		return updatedModel, nil
	}
	util.LogInfo("Action file written successfully")
	m = updatedModel

	// Get executable path
	executable, err := os.Executable()
	if err != nil {
		util.LogInfo("ERROR: Failed to get executable path: %v", err)
		m.saveMessage = fmt.Sprintf("Error finding executable: %v", err)
		return m, nil
	}
	util.LogInfo("Executable path: %q", executable)
	util.LogInfo("Left directory: %q", m.leftDir)
	util.LogInfo("Right directory: %q", m.rightDir)

	// Construct command arguments with required directories
	args := []string{executable, "apply", filename, m.leftDir, m.rightDir}
	util.LogInfo("Command args: %v", args)

	// Run apply command
	cmd := tea.ExecProcess(
		&exec.Cmd{
			Path: executable,
			Args: args,
		},
		func(err error) tea.Msg {
			if err != nil {
				util.LogInfo("APPLY COMPLETED WITH ERROR: %v", err)
				return applyCompletedMsg{success: false, error: err, filename: filename}
			}
			util.LogInfo("APPLY COMPLETED SUCCESSFULLY")
			return applyCompletedMsg{success: true, error: nil, filename: filename}
		},
	)

	return m, cmd
}

// Custom message types for external process completion
type dryRunCompletedMsg struct {
	success bool
	error   error
}

type applyCompletedMsg struct {
	success  bool
	error    error
	filename string
}

type cleanupCompletedMsg struct {
	success bool
	error   error
}

// refreshAfterApply handles post-apply state reset and comparison refresh
func (m Model) refreshAfterApply(appliedActionFile string) (Model, tea.Cmd) {
	util.DebugPrintf("=== refreshAfterApply ENTRY ===")

	// Store old session info for cleanup
	oldSessionID := m.sessionID

	// Generate new session ID
	m.sessionID = time.Now().Format("20060102_150405")
	util.DebugPrintf("New session ID: %s (old: %s)", m.sessionID, oldSessionID)

	// Reset TUI state - clean slate after successful apply
	m.hasUnsavedChanges = false
	m.hasUnappliedChanges = false // Applied successfully, nothing left to execute
	m.showingDiff = false
	m.currentDiff = ""
	m.err = nil
	m.cursor = 0
	m.viewportTop = 0
	m.reversedDiff = false
	m.hunkMode = false
	m.hunks = nil
	m.currentHunk = 0
	m.appliedHunks = nil
	m.cleanupTempFiles()

	// Re-run comparison to get fresh results
	results, summary, err := m.performFreshComparison()
	if err != nil {
		m.saveMessage = fmt.Sprintf("Error refreshing comparison: %v", err)
		return m, nil
	}

	// Update model with fresh results
	m.results = results
	m.summary = summary

	// Initialize actions for new results (all ignore by default)
	m.fileActions = make(map[string]action.ActionType)
	for _, result := range m.results {
		m.fileActions[result.RelativePath] = action.ActionIgnore
	}

	// Set up cleanup prompt
	m.showingCleanup = true
	m.cleanupOldSessionID = oldSessionID
	m.cleanupActionFile = appliedActionFile

	util.DebugPrintf("Fresh comparison complete. Found %d different files", len(results))
	m.saveMessage = "Apply succeeded! Clean up old action and patch files?"

	return m, nil
}

// performFreshComparison re-runs the directory comparison
func (m Model) performFreshComparison() ([]compare.ComparisonResult, *compare.ComparisonSummary, error) {
	// Create comparison engine with default options
	engine := compare.NewEngine(compare.ComparisonOptions{})

	results, summary, err := engine.Compare(m.leftDir, m.rightDir)
	if err != nil {
		return nil, nil, fmt.Errorf("comparison failed: %w", err)
	}

	// Filter out identical files for UI
	var filteredResults []compare.ComparisonResult
	for _, result := range results {
		if result.Status != compare.StatusIdentical {
			filteredResults = append(filteredResults, result)
		}
	}

	// Sort results alphabetically
	sort.Slice(filteredResults, func(i, j int) bool {
		return filteredResults[i].RelativePath < filteredResults[j].RelativePath
	})

	return filteredResults, summary, nil
}
