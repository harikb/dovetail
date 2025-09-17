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
func NewApp(results []compare.ComparisonResult, summary *compare.ComparisonSummary, leftDir, rightDir string) *App {
	// Filter out identical files for the UI (focus on differences)
	var filteredResults []compare.ComparisonResult
	for _, result := range results {
		if result.Status != compare.StatusIdentical {
			filteredResults = append(filteredResults, result)
		}
	}

	// Sort results with directory-aware sorting for better organization
	sortResultsByDirectory(filteredResults)

	model := Model{
		results:      filteredResults,
		summary:      summary,
		leftDir:      leftDir,
		rightDir:     rightDir,
		cursor:       0,
		showingDiff:  false,
		currentDiff:  "",
		windowWidth:  80,
		windowHeight: 24,
		viewportTop:  0,
		fileActions:  make(map[string]action.ActionType),
		patchTimes:   make(map[string]string),
		hasChanges:   false,
		showingSave:  false,
	}

	// Initialize default actions (all ignore for safety)
	for _, result := range filteredResults {
		model.fileActions[result.RelativePath] = action.ActionIgnore
	}

	return &App{model: model}
}

// sortResultsByDirectory sorts comparison results with directory-aware grouping
// Files in the same directory will be grouped together, with directories sorted alphabetically
func sortResultsByDirectory(results []compare.ComparisonResult) {
	sort.Slice(results, func(i, j int) bool {
		pathA := strings.Split(results[i].RelativePath, "/")
		pathB := strings.Split(results[j].RelativePath, "/")

		// Compare directory paths element by element
		minLen := len(pathA) - 1 // Don't include filename in directory comparison
		if len(pathB)-1 < minLen {
			minLen = len(pathB) - 1
		}

		for k := 0; k < minLen; k++ {
			if pathA[k] != pathB[k] {
				return pathA[k] < pathB[k]
			}
		}

		// If one path is a subdirectory of the other, shorter path (parent directory) comes first
		if len(pathA) != len(pathB) {
			return len(pathA) < len(pathB)
		}

		// Same directory depth, sort by filename
		return pathA[len(pathA)-1] < pathB[len(pathB)-1]
	})
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

	// Interactive action tracking
	fileActions map[string]action.ActionType // Track action per file path
	patchTimes  map[string]string            // Track patch timestamps for display
	hasChanges  bool                         // Whether any actions were modified
	showingSave bool                         // Whether save confirmation is shown
	saveMessage string                       // Save result message

	// Search functionality
	searchMode    bool   // Are we in search input mode?
	searchString  string // Current search term
	searchMatches []int  // Indices of matching files
	matchIndex    int    // Current match position (0-based)

	// Hunk mode functionality
	hunkMode      bool       // Are we in hunk editing mode?
	hunks         []DiffHunk // Parsed hunks from current diff
	currentHunk   int        // Currently selected hunk (0-based)
	tempLeftFile  string     // Path to temp left clone (if created)
	tempRightFile string     // Path to temp right clone (if created)
	appliedHunks  []bool     // Track which hunks have been applied (UI only)
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
		return m, nil

	case diffErrorMsg:
		m.err = error(msg)
		m.showingDiff = true // Show the error in diff view
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
			m.showingDiff = false
			m.currentDiff = ""
			m.err = nil
		} else {
			// In file list, q quits the application
			// Call profiling cleanup before quitting
			if cleanup := getProfilingCleanup(); cleanup != nil {
				cleanup()
			}
			return m, tea.Quit
		}

	case "esc":
		if m.hunkMode {
			// Exit hunk mode, save patches if any changes made
			return m.exitHunkMode()
		} else if m.showingDiff {
			// Return to file list
			m.showingDiff = false
			m.currentDiff = ""
			m.err = nil
		} else if m.searchString != "" {
			// Clear search in normal mode
			m.searchString = ""
			m.searchMatches = nil
			m.matchIndex = 0
		}
		// Note: ESC no longer quits app in normal mode - too dangerous

	case "up", "k":
		if !m.showingDiff && m.cursor > 0 {
			m.cursor--
			// Auto-scroll viewport if needed
			if m.cursor < m.viewportTop {
				m.viewportTop = m.cursor
			}
		}

	case "down", "j":
		if !m.showingDiff && m.cursor < len(m.results)-1 {
			m.cursor++
			// Auto-scroll viewport if needed
			visibleLines := m.getVisibleFileListLines()
			if m.cursor >= m.viewportTop+visibleLines {
				m.viewportTop = m.cursor - visibleLines + 1
			}
		}

	case "enter", "space", " ":
		if !m.showingDiff && !m.showingSave && len(m.results) > 0 {
			// Load diff for selected file
			return m, m.loadDiff()
		} else if m.showingDiff && !m.hunkMode {
			// Enter hunk mode for selective editing
			return m.enterHunkMode(), nil
		}

	case "pgup", "page_up":
		if !m.showingDiff && len(m.results) > 0 {
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
		if !m.showingDiff && len(m.results) > 0 {
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
		if m.hunkMode && len(m.hunks) > 0 {
			// Apply current hunk left→right
			return m.applyCurrentHunk("left-to-right")
		} else if !m.showingDiff && !m.showingSave && len(m.results) > 0 {
			return m.setAction(action.ActionCopyToRight), nil
		}
	case "<":
		if m.hunkMode && len(m.hunks) > 0 {
			// Apply current hunk right→left
			return m.applyCurrentHunk("right-to-left")
		} else if !m.showingDiff && !m.showingSave && len(m.results) > 0 {
			return m.setAction(action.ActionCopyToLeft), nil
		}
	case "i":
		if !m.showingDiff && !m.showingSave && len(m.results) > 0 {
			return m.setAction(action.ActionIgnore), nil
		}
	case "x":
		if !m.showingDiff && !m.showingSave && len(m.results) > 0 {
			// Use simplified delete - only valid for files that exist on one side only
			result := m.results[m.cursor]
			var deleteAction action.ActionType
			if result.Status == compare.StatusOnlyLeft {
				deleteAction = action.ActionDeleteLeft
			} else if result.Status == compare.StatusOnlyRight {
				deleteAction = action.ActionDeleteRight
			} else {
				// Show error for files that exist on both sides
				m.saveMessage = fmt.Sprintf("Delete not valid for %s files (exists on both sides)",
					result.Status.String())
				return m, nil
			}
			return m.setAction(deleteAction), nil
		}
	case "s":
		if !m.showingDiff && !m.showingSave {
			return m.saveActionFile()
		}

	// Search functionality
	case "/":
		if !m.showingDiff && !m.showingSave {
			m.searchMode = true
			m.searchString = ""
		}
	case "n":
		if m.hunkMode && len(m.hunks) > 0 {
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
		// Refresh/reload (future feature)
		// For now just clear any error
		m.err = nil
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
			var cmd *exec.Cmd
			if _, err := exec.LookPath("colordiff"); err == nil {
				// Use colordiff with color output and unified format with 3 lines of context
				cmd = exec.Command("colordiff", "--color=always", "-u", "-U3", leftPath, rightPath)
			} else {
				// Fall back to regular diff with unified format and 3 lines of context
				cmd = exec.Command("diff", "-u", "-U3", leftPath, rightPath)
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

		// For non-diff-able items, show basic info
		info := fmt.Sprintf("File: %s\nStatus: %s\n\n", result.RelativePath, result.Status.String())

		switch result.Status {
		case compare.StatusOnlyLeft:
			if result.LeftInfo != nil {
				info += fmt.Sprintf("Only exists in LEFT directory\nSize: %d bytes\n", result.LeftInfo.Size)
				if !result.LeftInfo.IsDir {
					info += fmt.Sprintf("Hash: %s\n", result.LeftInfo.Hash)
				}
			}
		case compare.StatusOnlyRight:
			if result.RightInfo != nil {
				info += fmt.Sprintf("Only exists in RIGHT directory\nSize: %d bytes\n", result.RightInfo.Size)
				if !result.RightInfo.IsDir {
					info += fmt.Sprintf("Hash: %s\n", result.RightInfo.Hash)
				}
			}
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
			if currentAction == action.ActionType(999) { // Patch marker
				if timestamp, hasTime := m.patchTimes[result.RelativePath]; hasTime {
					actionStr = "p=" + timestamp[len(timestamp)-6:] // Last 6 digits
				} else {
					actionStr = "p"
				}
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
			b.WriteString(helpStyle.Render("↑/↓: navigate  Enter: diff  >: copy→  <: copy←  i: ignore  x: delete  /: search  s: save  q: quit  Ctrl+C: force quit"))
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
	if m.hasChanges {
		b.WriteString("\n")
		changesStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
		b.WriteString(changesStyle.Render("● Unsaved changes"))
	}

	return b.String()
}

// viewDiff renders the diff view
func (m Model) viewDiff() string {
	var b strings.Builder

	// Clear screen to prevent corruption
	b.WriteString(strings.Repeat(" ", m.windowWidth))
	b.WriteString("\n")
	b.WriteString("\033[H") // Move cursor to top

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))

	if m.cursor < len(m.results) {
		result := m.results[m.cursor]

		// Show different header for hunk mode
		if m.hunkMode {
			b.WriteString(headerStyle.Render(fmt.Sprintf("Hunk Mode: %s (Hunk %d of %d)",
				result.RelativePath, m.currentHunk+1, len(m.hunks))))
		} else {
			b.WriteString(headerStyle.Render(fmt.Sprintf("Diff: %s", result.RelativePath)))
		}
		b.WriteString("\n\n")

		if m.err != nil {
			errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
			b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		} else {
			// Display diff content with hunk highlighting if in hunk mode
			if m.hunkMode && len(m.hunks) > 0 {
				b.WriteString(m.renderDiffWithHunkHighlight())
			} else {
				b.WriteString(m.currentDiff)
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
		b.WriteString(helpStyle.Render(fmt.Sprintf("n/p: next/prev hunk  >: apply left→right  <: apply right→left  ESC: exit hunk mode")))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render(fmt.Sprintf("Applied: %d hunks", appliedCount)))
	} else {
		b.WriteString(helpStyle.Render("SPACE: enter hunk mode  Esc/q: back to file list  Ctrl+C: quit"))
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
	m.hasChanges = true
	m.saveMessage = "" // Clear any previous message

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

// saveActionFile initiates the save process
func (m Model) saveActionFile() (Model, tea.Cmd) {
	if !m.hasChanges {
		m.saveMessage = "No changes to save"
		return m, nil
	}

	// Generate action file with current actions
	filename := fmt.Sprintf("dovetail_actions_%s.txt",
		time.Now().Format("20060102_150405"))

	if err := m.writeActionFile(filename); err != nil {
		m.saveMessage = fmt.Sprintf("Error saving: %v", err)
	} else {
		m.saveMessage = fmt.Sprintf("✅ Saved to %s", filename)
		m.hasChanges = false
	}

	return m, nil
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
	case action.ActionType(999): // Patch marker
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
	if !m.hunkMode {
		return m, nil
	}

	// Check if any hunks were applied
	anyApplied := false
	for _, applied := range m.appliedHunks {
		if applied {
			anyApplied = true
			break
		}
	}

	// Clean up state
	m.hunkMode = false
	m.hunks = nil
	m.currentHunk = 0
	m.appliedHunks = nil

	if anyApplied {
		// Generate patch file and update action
		return m.generatePatchFile()
	}

	// Clean up temp files if no changes made
	m.cleanupTempFiles()
	m.saveMessage = "Exited hunk mode - no changes made"
	return m, nil
}

// applyCurrentHunk applies the currently selected hunk in the specified direction
func (m Model) applyCurrentHunk(direction string) (Model, tea.Cmd) {
	fmt.Fprintf(os.Stderr, "DEBUG: applyCurrentHunk called, direction=%s, hunkMode=%t, currentHunk=%d/%d\n",
		direction, m.hunkMode, m.currentHunk, len(m.hunks))

	if !m.hunkMode || m.currentHunk >= len(m.hunks) {
		fmt.Fprintf(os.Stderr, "DEBUG: Invalid state - returning\n")
		return m, nil
	}

	if m.appliedHunks[m.currentHunk] {
		fmt.Fprintf(os.Stderr, "DEBUG: Hunk already applied\n")
		m.saveMessage = fmt.Sprintf("Hunk %d already applied", m.currentHunk+1)
		return m, nil
	}

	fmt.Fprintf(os.Stderr, "DEBUG: Creating temp files...\n")
	// Create temp files if not already created
	if err := m.ensureTempFiles(); err != nil {
		fmt.Fprintf(os.Stderr, "DEBUG: Error creating temp files: %v\n", err)
		m.saveMessage = fmt.Sprintf("Error creating temp files: %v", err)
		return m, nil
	}

	fmt.Fprintf(os.Stderr, "DEBUG: Applying hunk to temp file...\n")
	// Apply the hunk
	hunk := m.hunks[m.currentHunk]
	if err := m.applyHunkToTempFile(hunk, direction); err != nil {
		fmt.Fprintf(os.Stderr, "DEBUG: Error applying hunk: %v\n", err)
		m.saveMessage = fmt.Sprintf("Error applying hunk: %v", err)
		return m, nil
	}

	fmt.Fprintf(os.Stderr, "DEBUG: Marking hunk as applied...\n")
	// Mark hunk as applied
	m.appliedHunks[m.currentHunk] = true
	appliedCount := 0
	for _, applied := range m.appliedHunks {
		if applied {
			appliedCount++
		}
	}

	m.saveMessage = fmt.Sprintf("Applied hunk %d/%d (%s)", m.currentHunk+1, len(m.hunks), direction)
	fmt.Fprintf(os.Stderr, "DEBUG: Hunk applied successfully, regenerating diff...\n")

	// Regenerate diff with updated temp files
	return m, m.loadDiff()
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

// ensureTempFiles creates temp clone files if they don't exist
func (m *Model) ensureTempFiles() error {
	if m.cursor >= len(m.results) {
		return fmt.Errorf("invalid cursor position")
	}

	result := m.results[m.cursor]

	// Create temp directory if needed
	tempDir, err := ioutil.TempDir("", "dovetail_hunks_")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Create temp left file if needed and file exists
	if result.LeftInfo != nil && !result.LeftInfo.IsDir && m.tempLeftFile == "" {
		leftPath := filepath.Join(m.leftDir, result.RelativePath)
		m.tempLeftFile = filepath.Join(tempDir, "left_"+filepath.Base(result.RelativePath))

		if err := copyFile(leftPath, m.tempLeftFile); err != nil {
			return fmt.Errorf("failed to copy left file: %w", err)
		}
	}

	// Create temp right file if needed and file exists
	if result.RightInfo != nil && !result.RightInfo.IsDir && m.tempRightFile == "" {
		rightPath := filepath.Join(m.rightDir, result.RelativePath)
		m.tempRightFile = filepath.Join(tempDir, "right_"+filepath.Base(result.RelativePath))

		if err := copyFile(rightPath, m.tempRightFile); err != nil {
			return fmt.Errorf("failed to copy right file: %w", err)
		}
	}

	return nil
}

// applyHunkToTempFile applies a hunk to the appropriate temp file
func (m *Model) applyHunkToTempFile(hunk DiffHunk, direction string) error {
	// Create a temporary patch file with just this hunk
	patchContent := strings.Join(hunk.Lines, "\n") + "\n"

	tempPatch, err := ioutil.TempFile("", "hunk_*.patch")
	if err != nil {
		return fmt.Errorf("failed to create temp patch: %w", err)
	}
	defer os.Remove(tempPatch.Name())
	defer tempPatch.Close()

	if _, err := tempPatch.WriteString(patchContent); err != nil {
		return fmt.Errorf("failed to write patch content: %w", err)
	}
	tempPatch.Close()

	// Apply patch to appropriate temp file
	var targetFile string
	if direction == "left-to-right" {
		targetFile = m.tempRightFile
	} else {
		targetFile = m.tempLeftFile
	}

	// Use patch command to apply the hunk
	cmd := exec.Command("patch", targetFile)
	cmd.Stdin = strings.NewReader(patchContent)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("patch failed: %w, output: %s", err, string(output))
	}

	return nil
}

// regenerateDiff regenerates the diff using updated temp files
func (m *Model) regenerateDiff() (Model, tea.Cmd) {
	if m.cursor >= len(m.results) {
		return *m, nil
	}

	// Generate new diff between temp files (or original if no temp file)
	result := m.results[m.cursor]

	leftPath := filepath.Join(m.leftDir, result.RelativePath)
	if m.tempLeftFile != "" {
		leftPath = m.tempLeftFile
	}

	rightPath := filepath.Join(m.rightDir, result.RelativePath)
	if m.tempRightFile != "" {
		rightPath = m.tempRightFile
	}

	// Run diff command
	var cmd *exec.Cmd
	if _, err := exec.LookPath("colordiff"); err == nil {
		cmd = exec.Command("colordiff", "--color=always", "-u", "-U3", leftPath, rightPath)
	} else {
		cmd = exec.Command("diff", "-u", "-U3", leftPath, rightPath)
	}

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// Normal case - files differ
			m.currentDiff = string(output)
		} else {
			m.saveMessage = fmt.Sprintf("Error regenerating diff: %v", err)
			return *m, nil
		}
	} else {
		m.currentDiff = string(output)
	}

	// Re-parse hunks
	hunks, err := parseDiffIntoHunks(m.currentDiff)
	if err != nil {
		m.saveMessage = fmt.Sprintf("Error re-parsing hunks: %v", err)
		return *m, nil
	}

	// Update hunk state
	m.hunks = hunks
	m.appliedHunks = make([]bool, len(hunks))

	// Reset current hunk if out of bounds
	if m.currentHunk >= len(hunks) {
		m.currentHunk = 0
	}

	return *m, nil
}

// generatePatchFile generates the final patch file from original to temp files
func (m *Model) generatePatchFile() (Model, tea.Cmd) {
	if m.cursor >= len(m.results) {
		return *m, nil
	}

	result := m.results[m.cursor]
	timestamp := time.Now().Format("20060102_150405")

	// Generate patch from original to final temp file
	originalLeft := filepath.Join(m.leftDir, result.RelativePath)
	originalRight := filepath.Join(m.rightDir, result.RelativePath)

	var patchContent string
	var patchSide string

	// Determine which side was modified and generate appropriate patch
	if m.tempLeftFile != "" {
		// Left side was modified
		fmt.Fprintf(os.Stderr, "DEBUG: Generating patch for left side: %s vs %s\n", originalLeft, m.tempLeftFile)
		cmd := exec.Command("diff", "-u", originalLeft, m.tempLeftFile)
		output, err := cmd.Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				// Exit code 1 means differences found - this is what we want!
				patchContent = string(output)
				patchSide = "left"
				fmt.Fprintf(os.Stderr, "DEBUG: Left patch generated, %d bytes\n", len(patchContent))
			} else {
				fmt.Fprintf(os.Stderr, "DEBUG: Diff error: %v\n", err)
			}
		} else {
			// No differences found
			fmt.Fprintf(os.Stderr, "DEBUG: No differences in left side\n")
		}
	} else if m.tempRightFile != "" {
		// Right side was modified
		fmt.Fprintf(os.Stderr, "DEBUG: Generating patch for right side: %s vs %s\n", originalRight, m.tempRightFile)
		cmd := exec.Command("diff", "-u", originalRight, m.tempRightFile)
		output, err := cmd.Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				// Exit code 1 means differences found - this is what we want!
				patchContent = string(output)
				patchSide = "right"
				fmt.Fprintf(os.Stderr, "DEBUG: Right patch generated, %d bytes\n", len(patchContent))
			} else {
				fmt.Fprintf(os.Stderr, "DEBUG: Diff error: %v\n", err)
			}
		} else {
			// No differences found
			fmt.Fprintf(os.Stderr, "DEBUG: No differences in right side\n")
		}
	}

	if patchContent == "" {
		m.saveMessage = "No changes to save"
		m.cleanupTempFiles()
		return *m, nil
	}

	// Save patch file as sibling to original file
	var patchDir string
	if patchSide == "left" {
		patchDir = filepath.Dir(filepath.Join(m.leftDir, result.RelativePath))
	} else {
		patchDir = filepath.Dir(filepath.Join(m.rightDir, result.RelativePath))
	}

	patchFilename := filepath.Base(result.RelativePath) + "." + timestamp + ".patch"
	patchPath := filepath.Join(patchDir, patchFilename)

	if err := ioutil.WriteFile(patchPath, []byte(patchContent), 0644); err != nil {
		m.saveMessage = fmt.Sprintf("Error saving patch: %v", err)
		m.cleanupTempFiles()
		return *m, nil
	}

	// Update action to patch type
	// Use a special marker value and store timestamp separately
	m.fileActions[result.RelativePath] = action.ActionType(999) // Patch marker
	m.patchTimes[result.RelativePath] = timestamp
	m.hasChanges = true
	m.saveMessage = fmt.Sprintf("Patch saved: %s (timestamp: %s)", patchPath, timestamp)

	// Store timestamp for display purposes
	fmt.Fprintf(os.Stderr, "DEBUG: Generated patch with timestamp %s\n", timestamp)

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
