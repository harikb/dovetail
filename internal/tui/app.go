package tui

import (
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	err          error
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
		return m, nil
	}

	return m, nil
}

// handleKeyPress processes keyboard input
func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "esc":
		if m.showingDiff {
			// Return to file list
			m.showingDiff = false
			m.currentDiff = ""
			m.err = nil
		} else {
			return m, tea.Quit
		}

	case "up", "k":
		if !m.showingDiff && m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if !m.showingDiff && m.cursor < len(m.results)-1 {
			m.cursor++
		}

	case "enter", "space":
		if !m.showingDiff && len(m.results) > 0 {
			// Load diff for selected file
			return m, m.loadDiff()
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

			// Use Unix diff command (same as the main diff functionality)
			var cmd *exec.Cmd
			if _, err := exec.LookPath("colordiff"); err == nil {
				cmd = exec.Command("colordiff", "-u", leftPath, rightPath)
			} else {
				cmd = exec.Command("diff", "-u", leftPath, rightPath)
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

			line := fmt.Sprintf("  %-12s %s", result.Status.String(), result.RelativePath)

			if i == m.cursor {
				// Highlight selected line
				selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("8")).Foreground(lipgloss.Color("15"))
				line = selectedStyle.Render(fmt.Sprintf("▶ %-12s %s", result.Status.String(), result.RelativePath))
			} else {
				line = statusStyle.Render(fmt.Sprintf("  %-12s", result.Status.String())) + " " + result.RelativePath
			}

			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	// Footer/Help
	b.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	if len(m.results) > 0 {
		b.WriteString(helpStyle.Render("↑/↓ or j/k: navigate  Enter: show diff  q: quit"))
	} else {
		b.WriteString(helpStyle.Render("q: quit"))
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
	b.WriteString(helpStyle.Render("Esc: back to file list  q: quit"))

	return b.String()
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
