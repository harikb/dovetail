package tui

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/harikb/dovetail/internal/action"
	"github.com/harikb/dovetail/internal/compare"
)

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
		fileActions:  make(map[string]action.ActionType),
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
	err          error

	// Interactive action tracking
	fileActions map[string]action.ActionType // Track action per file path
	hasChanges  bool                         // Whether any actions were modified
	showingSave bool                         // Whether save confirmation is shown
	saveMessage string                       // Save result message

	// Search functionality
	searchMode    bool   // Are we in search input mode?
	searchString  string // Current search term
	searchMatches []int  // Indices of matching files
	matchIndex    int    // Current match position (0-based)
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
		return m, tea.Quit

	case "q":
		if m.showingDiff {
			// In diff view, q goes back to file list (same as esc)
			m.showingDiff = false
			m.currentDiff = ""
			m.err = nil
		} else {
			// In file list, q quits the application
			return m, tea.Quit
		}

	case "esc":
		if m.showingDiff {
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
		}

	case "down", "j":
		if !m.showingDiff && m.cursor < len(m.results)-1 {
			m.cursor++
		}

	case "enter", "space":
		if !m.showingDiff && !m.showingSave && len(m.results) > 0 {
			// Load diff for selected file
			return m, m.loadDiff()
		}

	// Interactive action keys - only in file list view
	case ">":
		if !m.showingDiff && !m.showingSave && len(m.results) > 0 {
			return m.setAction(action.ActionCopyToRight), nil
		}
	case "<":
		if !m.showingDiff && !m.showingSave && len(m.results) > 0 {
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
		if !m.showingDiff && !m.showingSave {
			if len(m.searchMatches) > 0 {
				m = m.nextSearchMatch()
			} else if m.searchString == "" {
				m.saveMessage = "No active search"
			}
		}
	case "N":
		if !m.showingDiff && !m.showingSave {
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

			leftPath := fmt.Sprintf("%s/%s", m.leftDir, result.RelativePath)
			rightPath := fmt.Sprintf("%s/%s", m.rightDir, result.RelativePath)

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

		for i, result := range m.results {
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

			var line string
			if i == m.cursor {
				// Highlight selected line
				selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("8")).Foreground(lipgloss.Color("15"))
				line = selectedStyle.Render(fmt.Sprintf("▶ [%s] %-12s %s",
					currentAction.String(), result.Status.String(), filePath))
			} else {
				// Color the action and status separately
				actionPart := actionStyle.Render(fmt.Sprintf("  [%s]", currentAction.String()))
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

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))

	if m.cursor < len(m.results) {
		result := m.results[m.cursor]
		b.WriteString(headerStyle.Render(fmt.Sprintf("Diff: %s", result.RelativePath)))
		b.WriteString("\n\n")

		if m.err != nil {
			errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
			b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		} else {
			// Display diff content
			b.WriteString(m.currentDiff)
		}
	}

	// Footer
	b.WriteString("\n\n")
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	b.WriteString(helpStyle.Render("Esc/q: back to file list  Ctrl+C: quit"))

	return b.String()
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
	default:
		return lipgloss.Color("15") // White
	}
}

// handleSearchInput processes input when in search mode
func (m Model) handleSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
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
